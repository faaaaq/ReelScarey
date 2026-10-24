package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/automation"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/httpapi"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/subtitle"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"github.com/faaaaq/ReelScarey/web"
)

var version = "0.12.0-dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Fprintln(os.Stdout, version)
		return nil
	}
	if len(os.Args) == 2 && (os.Args[1] == "help" || os.Args[1] == "--help") {
		fmt.Fprintln(os.Stdout, usage)
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return err
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 {
		return command(ctx, cfg, os.Args[1:])
	}
	ownership, err := instance.Acquire(cfg.ConfigDir)
	if err != nil {
		return err
	}
	defer ownership.Close()
	guard, err := instance.Guard(cfg.ConfigDir, true)
	if err != nil {
		return err
	}
	defer guard.Close()
	if err = instance.CheckStart(cfg.ConfigDir); err != nil {
		return err
	}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := database.Open(startup, cfg.ConfigDir)
	if err != nil {
		return err
	}
	defer db.Close()
	authService, err := auth.New(startup, db, cfg.ConfigDir)
	if err != nil {
		return err
	}
	secretStore, err := securestore.New(startup, db, cfg.ConfigDir)
	if err != nil {
		return err
	}
	cancel()
	metadataService := metadata.New(secretStore, metadata.NewTMDb())
	transferService := transfer.New(db, secretStore, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	automationService := automation.New(db, transferService)
	subtitleService := subtitle.New(db, secretStore, cfg, subtitle.NewOpenSubtitles())
	assets, err := web.Assets()
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.ListenAddr, Handler: httpapi.New(db, cfg, logger, assets, version, httpapi.Dependencies{Auth: authService, Metadata: metadataService, Transfer: transferService, Automation: automationService, Subtitle: subtitleService}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	workerCtx, workerCancel := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			transferService.Run(workerCtx, logger)
		}
	}()
	automationDone := make(chan struct{})
	go func() {
		defer close(automationDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			automationService.Run(workerCtx, logger)
		}
	}()
	subtitleDone := make(chan struct{})
	go func() {
		defer close(subtitleDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			subtitleService.Run(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-subtitleDone }()
	defer func() { workerCancel(); <-automationDone }()
	defer func() { workerCancel(); <-workerDone }()
	failed := make(chan error, 1)
	go func() { failed <- server.Serve(listener) }()
	logger.Info("ReelScarey started", "address", listener.Addr().String(), "version", version)
	select {
	case err := <-failed:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}

func waitRecovery(ctx context.Context, dir string) bool {
	for instance.RecoveryPending(dir) {
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	return ctx.Err() == nil
}

func healthcheck(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/readyz")
	if err != nil {
		return fmt.Errorf("healthcheck failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("healthcheck returned HTTP %d", resp.StatusCode)
	}
	return nil
}
