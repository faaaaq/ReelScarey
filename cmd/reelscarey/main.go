package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/httpapi"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"github.com/faaaaq/ReelScarey/web"
)

var version = "0.4.0-dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(os.Args) > 1 {
		if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
			return healthcheck(cfg.ListenAddr)
		}
		if len(os.Args) != 2 || (os.Args[1] != "setup-token" && os.Args[1] != "reset-password") {
			return fmt.Errorf("usage: reelscarey [healthcheck|setup-token|reset-password]")
		}
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return err
	}
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	db, err := database.Open(startup, cfg.ConfigDir)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()
	authService, err := auth.New(ctx, db, cfg.ConfigDir)
	if err != nil {
		return err
	}
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "setup-token":
			configured, err := authService.Configured(ctx)
			if err != nil {
				return err
			}
			if configured {
				return fmt.Errorf("the administrator is already configured")
			}
			secret, err := securestore.SecretFile(filepath.Join(cfg.ConfigDir, "setup-token"), 32, false)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(os.Stdout, base64.RawURLEncoding.EncodeToString(secret))
			return err
		case "reset-password":
			body, err := io.ReadAll(io.LimitReader(os.Stdin, 259))
			if err != nil {
				return fmt.Errorf("read the new password from standard input")
			}
			if len(body) > 258 {
				return auth.ErrPassword
			}
			password := strings.TrimSuffix(strings.TrimSuffix(string(body), "\n"), "\r")
			if err := authService.ResetPassword(ctx, password); err != nil {
				return err
			}
			_, err = fmt.Fprintln(os.Stdout, "Password updated. All sessions have been revoked.")
			return err
		}
	}
	secretStore, err := securestore.New(ctx, db, cfg.ConfigDir)
	if err != nil {
		return err
	}
	metadataService := metadata.New(secretStore, metadata.NewTMDb())
	transferService := transfer.New(db, secretStore, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	assets, err := web.Assets()
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.ListenAddr, Handler: httpapi.New(db, cfg, logger, assets, version, httpapi.Dependencies{Auth: authService, Metadata: metadataService, Transfer: transferService}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	workerCtx, workerCancel := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); transferService.Run(workerCtx, logger) }()
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
