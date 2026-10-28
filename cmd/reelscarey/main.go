package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/automation"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/discordbot"
	"github.com/faaaaq/ReelScarey/internal/emby"
	"github.com/faaaaq/ReelScarey/internal/httpapi"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/requests"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/subtitle"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"github.com/faaaaq/ReelScarey/internal/youtube"
	"github.com/faaaaq/ReelScarey/web"
)

var version = "0.20.0-dev"

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
	organizationService := organization.New(db, cfg)
	organizationService.SetProbe(func(ctx context.Context, f *os.File) (string, error) {
		result, err := (transfer.FFprobe{}).Probe(ctx, f)
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(result)
		return string(data), err
	})
	botService := discordbot.New(db, secretStore, metadataService, transferService)
	embyMonitor := emby.NewMonitor(db, emby.New(secretStore))
	youtubePath := os.Getenv("REELSCAREY_YTDLP_PATH")
	if youtubePath == "" {
		youtubePath = "yt-dlp"
	}
	youtubeInspector, _ := youtube.NewInspector(youtubePath)
	youtubeStaging := os.Getenv("REELSCAREY_YOUTUBE_STAGING_DIR")
	if youtubeStaging == "" {
		youtubeStaging = filepath.Join(cfg.DownloadsDir, ".reelscarey-youtube")
	}
	var httpYouTubeInspector httpapi.YouTubeInspector
	if youtubeInspector != nil {
		httpYouTubeInspector = youtubeInspector
	}
	assets, err := web.Assets()
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.ListenAddr, Handler: httpapi.New(db, cfg, logger, assets, version, httpapi.Dependencies{Auth: authService, Metadata: metadataService, Transfer: transferService, Automation: automationService, Subtitle: subtitleService, YouTubeInspector: httpYouTubeInspector, Secrets: secretStore, Bot: botService}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	workerCtx, workerCancel := context.WithCancel(ctx)
	botDone := make(chan struct{})
	go func() {
		defer close(botDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			botService.Run(workerCtx)
		}
	}()
	defer func() { workerCancel(); <-botDone }()
	listsDone := make(chan struct{})
	go func() {
		defer close(listsDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			requests.NewLists(db, secretStore).Run(workerCtx)
		}
	}()
	defer func() { workerCancel(); <-listsDone }()
	discordDone := make(chan struct{})
	go func() {
		defer close(discordDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			requests.NewDiscord(db, secretStore).Run(workerCtx)
		}
	}()
	defer func() { workerCancel(); <-discordDone }()
	youtubeDone := make(chan struct{})
	go func() {
		defer close(youtubeDone)
		if youtubeInspector == nil || !waitRecovery(workerCtx, cfg.ConfigDir) {
			return
		}
		worker := &youtube.Worker{Store: youtube.NewStore(db), Downloader: youtubeInspector, Prober: transfer.FFprobe{}, StagingRoot: youtubeStaging, MediaRoot: cfg.MediaDir, ConfigRoot: cfg.ConfigDir}
		if e := worker.CleanupCommitted(workerCtx, cfg); e != nil && workerCtx.Err() == nil {
			logger.Warn("YouTube staging cleanup deferred", "error", e)
		}
		for workerCtx.Err() == nil {
			imported, e := worker.ProcessImport(workerCtx, cfg)
			if e != nil && workerCtx.Err() == nil {
				logger.Warn("YouTube import needs attention", "error", e)
			}
			acquired, e := worker.ProcessOne(workerCtx)
			if e != nil && workerCtx.Err() == nil {
				logger.Warn("YouTube acquisition failed", "error", e)
			}
			if !imported && !acquired {
				select {
				case <-workerCtx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			}
		}
	}()
	defer func() { workerCancel(); <-youtubeDone }()
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
	embyDone := make(chan struct{})
	go func() {
		defer close(embyDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			embyMonitor.Run(workerCtx)
		}
	}()
	defer func() { workerCancel(); <-embyDone }()
	organizationDone := make(chan struct{})
	go func() {
		defer close(organizationDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			organizationService.Run(workerCtx)
		}
	}()
	defer func() { workerCancel(); <-organizationDone }()
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
