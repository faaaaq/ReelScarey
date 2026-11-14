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
	"runtime/debug"
	"syscall"
	"time"

	"github.com/faaaaq/ReelScarey/internal/announce"
	"github.com/faaaaq/ReelScarey/internal/artworkexport"
	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/automation"
	"github.com/faaaaq/ReelScarey/internal/backups"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/discordbot"
	"github.com/faaaaq/ReelScarey/internal/emby"
	"github.com/faaaaq/ReelScarey/internal/httpapi"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/jellyfin"
	"github.com/faaaaq/ReelScarey/internal/mediaanalysis"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/nfoexport"
	"github.com/faaaaq/ReelScarey/internal/notify"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/plex"
	"github.com/faaaaq/ReelScarey/internal/requests"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/subtitle"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"github.com/faaaaq/ReelScarey/internal/vpn"
	"github.com/faaaaq/ReelScarey/internal/youtube"
	"github.com/faaaaq/ReelScarey/web"
)

var version = "0.54.11"

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
	metadataService.SetCooldownDB(db)
	musicService := music.NewService(db, cfg.MediaDir)
	musicService.SetDownloadsDir(cfg.DownloadsDir)
	musicService.SetArtworkDir(filepath.Join(cfg.ConfigDir, "artwork"))
	musicService.SetSecrets(secretStore)
	musicBrainzCtx, musicBrainzCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer musicBrainzCancel()
	if err := musicService.LoadMusicBrainzBase(musicBrainzCtx); err != nil {
		return false, err
	}
	if err := musicService.RecoverScans(ctx); err != nil {
		return false, err
	}
	transferService := transfer.New(db, secretStore, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	transferService.SetLogger(logger)
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
	notifyService := notify.New(db, secretStore)
	embyService := emby.New(secretStore)
	embyMonitor := emby.NewMonitor(db, embyService)
	jellyfinService := jellyfin.New(secretStore)
	plexService := plex.New(secretStore)
	youtubePath := os.Getenv("REELSCAREY_YTDLP_PATH")
	if youtubePath == "" {
		youtubePath = "yt-dlp"
	}
	youtubeInspector, youtubeInspectorErr := youtube.NewInspector(youtubePath)
	if youtubeInspectorErr != nil {
		// The YouTube subsystem failing to start (for example a missing
		// yt-dlp executable) must be visible, not silently swallowed. The
		// executable path is local configuration, not a secret; the API layer
		// already answers YouTube requests with a 503 youtube_unavailable
		// notice while the inspector is nil.
		logger.Warn("YouTube subsystem failed to start; YouTube sourcing is unavailable",
			"error", youtubeInspectorErr,
			"executable", youtubePath)
	}
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
	server := &http.Server{Addr: cfg.ListenAddr, Handler: httpapi.New(db, cfg, logger, assets, version, httpapi.Dependencies{Auth: authService, Metadata: metadataService, Music: musicService, Transfer: transferService, Automation: automationService, Subtitle: subtitleService, YouTubeInspector: httpYouTubeInspector, Secrets: secretStore, Notify: notifyService, TOTP: totpStore, Emby: embyService, EmbyMonitor: embyMonitor, Jellyfin: jellyfinService, Plex: plexService, VPN: vpnService, Announce: announceService, Bot: botService, Backups: backupService, MetadataRefresh: metadataRefresh}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}
	workerCtx, workerCancel := context.WithCancel(ctx)
	workers := newWorkerDrain(logger, workerCancel)
	defer workers.report()

	notifyDone := make(chan struct{})
	go guardWorker(logger, "notify", notifyDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			notifyService.Run(workerCtx, logger)
		}
	})
	defer workers.join(notifyDone, "notify")

	musicImportsDone := make(chan struct{})
	go guardWorker(logger, "musicImports", musicImportsDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			musicService.RunImports(workerCtx, logger)
		}
	})
	defer workers.join(musicImportsDone, "musicImports")
	musicAcquisitionsDone := make(chan struct{})
	go guardWorker(logger, "musicAcquisitions", musicAcquisitionsDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			musicService.RunAcquisitions(workerCtx, logger)
		}
	})
	defer workers.join(musicAcquisitionsDone, "musicAcquisitions")
	musicMonitorDone := make(chan struct{})
	go guardWorker(logger, "musicMonitor", musicMonitorDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			musicService.RunMonitorLoop(workerCtx, logger)
		}
	})
	defer workers.join(musicMonitorDone, "musicMonitor")
	musicScheduleDone := make(chan struct{})
	go guardWorker(logger, "musicSchedule", musicScheduleDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			musicService.RunScheduleLoop(workerCtx)
		}
	})
	defer workers.join(musicScheduleDone, "musicSchedule")
	musicWorkflowDone := make(chan struct{})
	go guardWorker(logger, "musicWorkflow", musicWorkflowDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			musicService.RunWorkflow(workerCtx, logger)
		}
	})
	defer workers.join(musicWorkflowDone, "musicWorkflow")
	subscriptionsDone := make(chan struct{})
	go guardWorker(logger, "subscriptions", subscriptionsDone, func() {
		if youtubeInspector != nil && waitRecovery(workerCtx, cfg.ConfigDir) {
			youtube.NewStore(db).RunSubscriptions(workerCtx, youtubeInspector, logger)
		}
	})
	defer workers.join(subscriptionsDone, "subscriptions")
	seriesSourcesDone := make(chan struct{})
	go guardWorker(logger, "seriesSources", seriesSourcesDone, func() {
		if youtubeInspector != nil && waitRecovery(workerCtx, cfg.ConfigDir) {
			youtube.NewStore(db).RunSeriesSources(workerCtx, youtubeInspector, logger)
		}
	})
	defer workers.join(seriesSourcesDone, "seriesSources")
	metadataDone := make(chan struct{})
	go guardWorker(logger, "metadata", metadataDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			metadataRefresh.Run(workerCtx, logger)
		}
	})
	defer workers.join(metadataDone, "metadata")
	backupDone := make(chan struct{})
	go guardWorker(logger, "backup", backupDone, func() { backupService.Run(workerCtx, logger) })
	defer workers.join(backupDone, "backup")
	botDone := make(chan struct{})
	go guardWorker(logger, "bot", botDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			botService.Run(workerCtx)
		}
	})
	defer workers.join(botDone, "bot")
	listsDone := make(chan struct{})
	go guardWorker(logger, "lists", listsDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			requests.NewLists(db, secretStore).Run(workerCtx)
		}
	})
	defer workers.join(listsDone, "lists")
	discordDone := make(chan struct{})
	go guardWorker(logger, "discord", discordDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			requests.NewDiscord(db, secretStore).Run(workerCtx)
		}
	})
	defer workers.join(discordDone, "discord")
	youtubeDone := make(chan struct{})
	go guardWorker(logger, "youtube", youtubeDone, func() {
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
	})
	defer workers.join(youtubeDone, "youtube")
	workerDone := make(chan struct{})
	go guardWorker(logger, "worker", workerDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			transferService.Run(workerCtx, logger)
		}
	})
	analysisDone := make(chan struct{})
	go guardWorker(logger, "analysis", analysisDone, func() {
		if !waitRecovery(workerCtx, cfg.ConfigDir) {
			return
		}
		probe := os.Getenv("REELSCAREY_FFPROBE_PATH")
		if probe == "" {
			probe = "ffprobe"
		}
		worker := &mediaanalysis.Worker{DB: db, MediaRoot: cfg.MediaDir, Binary: probe}
		worker.Run(workerCtx, logger)
	})
	defer workers.join(analysisDone, "analysis")
	nfoDone := make(chan struct{})
	go guardWorker(logger, "nfo", nfoDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			(&nfoexport.Worker{DB: db, MediaRoot: cfg.MediaDir}).Run(workerCtx, logger)
		}
	})
	defer workers.join(nfoDone, "nfo")
	artworkDone := make(chan struct{})
	go guardWorker(logger, "artwork", artworkDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			(&artworkexport.Worker{DB: db, MediaRoot: cfg.MediaDir}).Run(workerCtx, logger)
		}
	})
	defer workers.join(artworkDone, "artwork")
	bundledDone := make(chan struct{})
	go guardWorker(logger, "bundled", bundledDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			transferService.SuperviseBundled(workerCtx, logger)
		}
	})
	defer workers.join(bundledDone, "bundled")
	usenetBuiltinDone := make(chan struct{})
	go guardWorker(logger, "usenetBuiltin", usenetBuiltinDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			transferService.SuperviseBundledUsenet(workerCtx, logger)
		}
	})
	defer workers.join(usenetBuiltinDone, "usenetBuiltin")
	seedingDone := make(chan struct{})
	go guardWorker(logger, "seeding", seedingDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			transferService.SuperviseTorrentPolicies(workerCtx, logger)
		}
	})
	defer workers.join(seedingDone, "seeding")
	automationDone := make(chan struct{})
	go guardWorker(logger, "automation", automationDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			automationService.Run(workerCtx, logger)
		}
	})
	rssDone := make(chan struct{})
	go guardWorker(logger, "rss", rssDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			automationService.RunRSS(workerCtx, logger)
		}
	})
	defer workers.join(rssDone, "rss")
	announceAutomationDone := make(chan struct{})
	go guardWorker(logger, "announceAutomation", announceAutomationDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			automationService.RunAnnounces(workerCtx, logger)
		}
	})
	defer workers.join(announceAutomationDone, "announceAutomation")
	rssDeferralDone := make(chan struct{})
	go guardWorker(logger, "rssDeferrals", rssDeferralDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			automationService.RunRSSDeferrals(workerCtx, logger)
		}
	})
	defer workers.join(rssDeferralDone, "rssDeferrals")
	subtitleDone := make(chan struct{})
	go guardWorker(logger, "subtitle", subtitleDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			subtitleService.Run(workerCtx, logger)
		}
	})
	vpnDone := make(chan struct{})
	go guardWorker(logger, "vpn", vpnDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			vpnService.Supervise(workerCtx, logger)
		}
	})
	defer workers.join(subtitleDone, "subtitle")
	defer workers.join(vpnDone, "vpn")
	announceDone := make(chan struct{})
	go guardWorker(logger, "announce", announceDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			announceService.Run(workerCtx)
		}
	})
	defer workers.join(announceDone, "announce")

	embyDone := make(chan struct{})
	go guardWorker(logger, "emby", embyDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			embyMonitor.Run(workerCtx)
		}
	})
	defer workers.join(embyDone, "emby")
	organizationDone := make(chan struct{})
	go guardWorker(logger, "organization", organizationDone, func() {
		if waitRecovery(workerCtx, cfg.ConfigDir) {
			organizationService.Run(workerCtx)
		}
	})
	defer workers.join(organizationDone, "organization")
	defer workers.join(automationDone, "automation")
	defer workers.join(workerDone, "worker")
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

// guardWorker contains a panic in one boot worker so a defect in a single
// background feature cannot take the HTTP server and every other worker down
// with it. The panic is logged with the worker name and the goroutine stack,
// the done channel is still closed so the shutdown drain joins cleanly, and
// the worker stays stopped: recovery bounds the blast radius, it does not
// resume the failed feature or conceal the defect. Durable leases and
// recovery journals reconcile the interrupted work on the next start.
func guardWorker(logger *slog.Logger, name string, done chan struct{}, run func()) {
	defer close(done)
	defer func() {
		if r := recover(); r != nil {
			logger.Error("background worker panicked and stopped; its durable state will be reconciled on the next start", "worker", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	run()
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
