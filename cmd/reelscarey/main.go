package main

import (
	"context"
	"database/sql"
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

	"github.com/faaaaq/ReelScarey/internal/announce"
	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/automation"
	"github.com/faaaaq/ReelScarey/internal/backups"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/discordbot"
	"github.com/faaaaq/ReelScarey/internal/emby"
	"github.com/faaaaq/ReelScarey/internal/httpapi"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/mediaanalysis"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/nfoexport"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/requests"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/subtitle"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"github.com/faaaaq/ReelScarey/internal/vpn"
	"github.com/faaaaq/ReelScarey/internal/youtube"
	"github.com/faaaaq/ReelScarey/web"
)

var version = "0.51.2"

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
	guard, err := instance.Guard(cfg.ConfigDir, false)
	if err != nil {
		return err
	}
	defer func() { guard.Close() }()
	for {
		apply, done := context.WithTimeout(context.Background(), 10*time.Minute)
		err = backups.ApplyPending(apply, cfg, version)
		done()
		if err != nil {
			return err
		}
		if err = instance.CheckStart(cfg.ConfigDir); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		guard.Close()
		guard, err = instance.Guard(cfg.ConfigDir, true)
		if err != nil {
			return err
		}
		restart, err := serve(ctx, cfg, logger)
		if err != nil || !restart {
			return err
		}
		guard.Close()
		guard, err = instance.Guard(cfg.ConfigDir, false)
		if err != nil {
			return err
		}
	}
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) (bool, error) {
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := database.Open(startup, cfg.ConfigDir)
	if err != nil {
		return false, err
	}
	defer db.Close()
	secretStore, err := securestore.New(startup, db, cfg.ConfigDir)
	if err != nil {
		return false, err
	}
	authService, err := auth.New(startup, db, cfg.ConfigDir)
	if err != nil {
		return false, err
	}
	totpStore, err := auth.NewTOTPStore(db, secretStore)
	if err != nil {
		return false, err
	}
	authService.SetTOTP(totpStore)
	cancel()
	metadataService := metadata.New(secretStore, metadata.NewTMDb())
	metadataService.SetDefaultToken(cfg.TMDbToken)
	musicService := music.NewService(db, cfg.MediaDir)
	musicService.SetDownloadsDir(cfg.DownloadsDir)
	musicService.SetArtworkDir(filepath.Join(cfg.ConfigDir, "artwork"))
	musicService.SetSecrets(secretStore)
	if err := musicService.LoadMusicBrainzBase(context.Background()); err != nil {
		return false, err
	}
	if err := musicService.RecoverScans(ctx); err != nil {
		return false, err
	}
	transferService := transfer.New(db, secretStore, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	transferService.SetAutomaticMetadata(metadataService)
	transferService.SetMusicImporter(musicService)
	musicService.SetScheduleRunner(func(ctx context.Context, schedule music.MusicSchedule, _ music.MusicPreferences, albums []music.WantedAlbum) error {
		var failures error
		for _, album := range albums {
			current, err := musicService.GetSchedule(ctx)
			if err != nil {
				return errors.Join(failures, err)
			}
			if current.Revision != schedule.Revision {
				break
			}
			releases, err := transferService.SearchMusicAlbum(ctx, album.ID, "")
			if err != nil {
				failures = errors.Join(failures, fmt.Errorf("album %d: %w", album.ID, err))
				continue
			}
			albumPrefs, err := musicService.AlbumPreferences(ctx, album.ID)
			if err != nil {
				failures = errors.Join(failures, fmt.Errorf("album %d: %w", album.ID, err))
				continue
			}
			if schedule.Mode != "download" {
				continue
			}
			for _, candidate := range releases {
				if !music.CandidateAllowed(albumPrefs.Preferences, candidate.Format, candidate.Bitrate, candidate.Lossless) {
					continue
				}
				_, err := transferService.QueueMusicGuarded(ctx, album.ID, candidate.ID, "Music", func(ctx context.Context, tx *sql.Tx) error {
					return music.CheckScheduledDownload(ctx, tx, album.ID, schedule, albumPrefs)
				})
				if err != nil && !errors.Is(err, music.ErrRevisionConflict) {
					failures = errors.Join(failures, fmt.Errorf("album %d: %w", album.ID, err))
				}
				break
			}
		}
		return failures
	})
	automationService := automation.New(db, transferService)
	metadataRefresh := automation.NewMetadataRefresher(db, metadataService)
	subtitleService := subtitle.New(db, secretStore, cfg, subtitle.NewOpenSubtitles())
	vpnService := vpn.New(db, secretStore, cfg)
	announceService := announce.New(db, secretStore, logger)
	transferService.SetTunnelState(vpnService.TorrentState)
	organizationService := organization.New(db, cfg)
	organizationService.SetProbe(func(ctx context.Context, f *os.File) (string, error) {
		result, err := (transfer.FFprobe{}).Probe(ctx, f)
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(result)
		return string(data), err
	})
	organizationService.SetTagger(func(ctx context.Context, source, stage *os.File, extension, title string) (string, error) {
		result, err := transfer.RewriteTitleVerified(ctx, source, stage, extension, title)
		if err != nil {
			var problem *transfer.Error
			if errors.As(err, &problem) {
				return "", &organization.Error{Code: problem.Code, Message: problem.Message}
			}
			return "", err
		}
		data, err := json.Marshal(result)
		return string(data), err
	})
	botService := discordbot.New(db, secretStore, metadataService, transferService)
	embyService := emby.New(secretStore)
	embyMonitor := emby.NewMonitor(db, embyService)
	youtubePath := os.Getenv("REELSCAREY_YTDLP_PATH")
	if youtubePath == "" {
		youtubePath = "yt-dlp"
	}
	youtubeInspector, _ := youtube.NewInspector(youtubePath)
	if youtubeInspector != nil {
		musicService.SetAudioSource(youtubeAudioSource{inspector: youtubeInspector})
	}
	youtubeStaging := os.Getenv("REELSCAREY_YOUTUBE_STAGING_DIR")
	if youtubeStaging == "" {
		youtubeStaging = filepath.Join(filepath.Dir(cfg.ConfigDir), "youtube-staging", "work")
	}
	var httpYouTubeInspector httpapi.YouTubeInspector
	if youtubeInspector != nil {
		httpYouTubeInspector = youtubeInspector
	}
	assets, err := web.Assets()
	if err != nil {
		return false, err
	}
	restart := make(chan struct{}, 1)
	backupService, err := backups.New(ctx, db, cfg, version, func() {
		select {
		case restart <- struct{}{}:
		default:
		}
	})
	if err != nil {
		return false, err
	}
	server := &http.Server{Addr: cfg.ListenAddr, Handler: httpapi.New(db, cfg, logger, assets, version, httpapi.Dependencies{Auth: authService, Metadata: metadataService, Music: musicService, Transfer: transferService, Automation: automationService, Subtitle: subtitleService, YouTubeInspector: httpYouTubeInspector, Secrets: secretStore, TOTP: totpStore, Emby: embyService, EmbyMonitor: embyMonitor, VPN: vpnService, Announce: announceService, Bot: botService, Backups: backupService, MetadataRefresh: metadataRefresh}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}
	workerCtx, workerCancel := context.WithCancel(ctx)

	musicImportsDone := make(chan struct{})
	go func() {
		defer close(musicImportsDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			musicService.RunImports(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-musicImportsDone }()
	musicAcquisitionsDone := make(chan struct{})
	go func() {
		defer close(musicAcquisitionsDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			musicService.RunAcquisitions(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-musicAcquisitionsDone }()
	musicMonitorDone := make(chan struct{})
	go func() {
		defer close(musicMonitorDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			musicService.RunMonitorLoop(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-musicMonitorDone }()
	musicScheduleDone := make(chan struct{})
	go func() {
		defer close(musicScheduleDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			musicService.RunScheduleLoop(workerCtx)
		}
	}()
	defer func() { workerCancel(); <-musicScheduleDone }()
	musicWorkflowDone := make(chan struct{})
	go func() {
		defer close(musicWorkflowDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			musicService.RunWorkflow(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-musicWorkflowDone }()
	subscriptionsDone := make(chan struct{})
	go func() {
		defer close(subscriptionsDone)
		if youtubeInspector != nil && waitRecovery(workerCtx, cfg.ConfigDir) {
			youtube.NewStore(db).RunSubscriptions(workerCtx, youtubeInspector, logger)
		}
	}()
	defer func() { workerCancel(); <-subscriptionsDone }()
	seriesSourcesDone := make(chan struct{})
	go func() {
		defer close(seriesSourcesDone)
		if youtubeInspector != nil && waitRecovery(workerCtx, cfg.ConfigDir) {
			youtube.NewStore(db).RunSeriesSources(workerCtx, youtubeInspector, logger)
		}
	}()
	defer func() { workerCancel(); <-seriesSourcesDone }()
	metadataDone := make(chan struct{})
	go func() {
		defer close(metadataDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			metadataRefresh.Run(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-metadataDone }()
	backupDone := make(chan struct{})
	go func() { defer close(backupDone); backupService.Run(workerCtx, logger) }()
	defer func() { workerCancel(); <-backupDone }()
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
		worker := &youtube.Worker{Store: youtube.NewStore(db), Downloader: youtubeInspector, Prober: transfer.FFprobe{}, StagingRoot: youtubeStaging, MediaRoot: cfg.MediaDir, ConfigRoot: cfg.ConfigDir, Config: cfg}
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
	analysisDone := make(chan struct{})
	go func() {
		defer close(analysisDone)
		if !waitRecovery(workerCtx, cfg.ConfigDir) {
			return
		}
		probe := os.Getenv("REELSCAREY_FFPROBE_PATH")
		if probe == "" {
			probe = "ffprobe"
		}
		worker := &mediaanalysis.Worker{DB: db, MediaRoot: cfg.MediaDir, Binary: probe}
		worker.Run(workerCtx, logger)
	}()
	defer func() { workerCancel(); <-analysisDone }()
	nfoDone := make(chan struct{})
	go func() {
		defer close(nfoDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			(&nfoexport.Worker{DB: db, MediaRoot: cfg.MediaDir}).Run(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-nfoDone }()
	go func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			transferService.SuperviseBundled(workerCtx, logger)
		}
	}()
	usenetBuiltinDone := make(chan struct{})
	go func() {
		defer close(usenetBuiltinDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			transferService.SuperviseBundledUsenet(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-usenetBuiltinDone }()
	seedingDone := make(chan struct{})
	go func() {
		defer close(seedingDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			transferService.SuperviseTorrentPolicies(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-seedingDone }()
	automationDone := make(chan struct{})
	go func() {
		defer close(automationDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			automationService.Run(workerCtx, logger)
		}
	}()
	rssDone := make(chan struct{})
	go func() {
		defer close(rssDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			automationService.RunRSS(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-rssDone }()
	subtitleDone := make(chan struct{})
	go func() {
		defer close(subtitleDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			subtitleService.Run(workerCtx, logger)
		}
	}()
	vpnDone := make(chan struct{})
	go func() {
		defer close(vpnDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			vpnService.Supervise(workerCtx, logger)
		}
	}()
	defer func() { workerCancel(); <-subtitleDone }()
	defer func() { workerCancel(); <-vpnDone }()
	announceDone := make(chan struct{})
	go func() {
		defer close(announceDone)
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			announceService.Run(workerCtx)
		}
	}()
	defer func() { workerCancel(); <-announceDone }()

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
			return false, nil
		}
		return false, err
	case <-restart:
		logger.Info("closing active work for reviewed configuration restore")
		workerCancel()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return false, err
		}
		return true, nil
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return false, err
		}
		return false, nil
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
