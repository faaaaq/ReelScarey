package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/announce"
	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/automation"
	"github.com/faaaaq/ReelScarey/internal/backups"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/discordbot"
	"github.com/faaaaq/ReelScarey/internal/emby"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/operations"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/requests"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/settings"
	"github.com/faaaaq/ReelScarey/internal/subtitle"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"github.com/faaaaq/ReelScarey/internal/vpn"
	"github.com/faaaaq/ReelScarey/internal/youtube"
)

type Server struct {
	db               *sql.DB
	config           config.Config
	logger           *slog.Logger
	settings         *settings.Store
	library          *library.Store
	version          string
	started          time.Time
	auth             *auth.Service
	metadata         *metadata.Service
	music            MusicService
	transfer         *transfer.Service
	automation       *automation.Service
	subtitle         *subtitle.Service
	operations       *operations.Service
	organization     *organization.Service
	requests         *requests.Store
	discord          *requests.Discord
	requestLists     *requests.Lists
	bot              *discordbot.Service
	emby             *emby.Service
	embyMonitor      *emby.Monitor
	vpn              *vpn.Service
	announce         *announce.Service
	franchises       metadata.FranchiseProvider
	animeProvider    metadata.AnimeProvider
	kitsuProvider    metadata.AnimeProvider
	youtube          *youtube.Store
	youtubeInspector YouTubeInspector
	backups          *backups.Service
	metadataRefresh  *automation.MetadataRefresher
	totp             *auth.TOTPStore
}

type Dependencies struct {
	Auth             *auth.Service
	Metadata         *metadata.Service
	Music            MusicService
	Transfer         *transfer.Service
	Automation       *automation.Service
	Subtitle         *subtitle.Service
	AnimeProvider    metadata.AnimeProvider
	KitsuProvider    metadata.AnimeProvider
	YouTubeInspector YouTubeInspector
	Secrets          *securestore.Store
	Emby             *emby.Service
	EmbyMonitor      *emby.Monitor
	VPN              *vpn.Service
	Announce         *announce.Service
	Bot              *discordbot.Service
	Franchises       metadata.FranchiseProvider
	Backups          *backups.Service
	MetadataRefresh  *automation.MetadataRefresher
	TOTP             *auth.TOTPStore
}

type YouTubeInspector interface {
	Inspect(context.Context, string) (youtube.Inspection, error)
}

type MusicService interface {
	SearchArtists(context.Context, string, int) (music.SearchResults, error)
	AddArtist(context.Context, string, bool) (music.Artist, bool, error)
	ListArtists(context.Context, string, int) (music.ArtistPage, error)
	Artist(context.Context, int64) (music.ArtistDetails, error)
	ArtistTracks(context.Context, int64, int, int) (music.ArtistTrackPage, error)
	ListAlbums(context.Context, string, int, int) (music.AlbumPage, error)
	ListTracks(context.Context, string, int, int) (music.ArtistTrackPage, error)
	ArtistTopTracks(context.Context, int64, int) (music.TopTrackPage, error)
	ArtistDiscography(context.Context, int64, int) (music.DiscographyPage, error)
	ChartTracks(context.Context, int) (music.TopTrackPage, error)
	ResolveLink(context.Context, string) (music.LinkPreview, error)
	StartAlbumAcquisition(context.Context, int64, string, string) (music.AcquisitionJob, error)
	StartProviderAlbumAcquisition(context.Context, string, string, string) (music.AcquisitionJob, error)
	StartAcquisition(context.Context, string, string, string, string, string, []music.AcquisitionTrackInput) (music.AcquisitionJob, error)
	Acquisition(context.Context, int64) (music.AcquisitionJob, error)
	ListAcquisitions(context.Context, int) (music.AcquisitionJobList, error)
	MonitorSettings(context.Context) (music.MusicMonitorSettings, error)
	SaveMonitorSettings(context.Context, bool, int, int) (music.MusicMonitorSettings, error)
	ListMonitorSources(context.Context) (music.MusicMonitorSourceList, error)
	AddMonitorSource(context.Context, string) (music.MusicMonitorSource, error)
	SetMonitorSourceEnabled(context.Context, int64, bool) (music.MusicMonitorSource, error)
	DeleteMonitorSource(context.Context, int64) error
	TriggerMonitor(context.Context) (music.MusicMonitorSettings, error)
	OpenAudioFile(context.Context, int64) (music.AudioStream, error)
	ArtistImage(context.Context, int64) (music.Artwork, error)
	AlbumImage(context.Context, int64) (music.Artwork, error)
	Album(context.Context, int64) (music.AlbumDetails, error)
	ServerAlbum(context.Context, int64) (music.AlbumDetails, error)
	ServerSong(context.Context, int64) (music.ServerSong, error)
	ServerSongs(context.Context, string, int) ([]music.ServerSong, error)
	ServerRandomSongs(context.Context, int) ([]music.ServerSong, error)
	ServerAlbums(context.Context, string, int, int) ([]music.Album, error)
	ServerSearchAlbums(context.Context, string, int) ([]music.Album, error)
	MusicServer(context.Context) (music.MusicServerStatus, error)
	SaveMusicServer(context.Context, bool, int) (music.MusicServerStatus, error)
	GenerateMusicServerToken(context.Context) (string, music.MusicServerStatus, error)
	DeleteMusicServerToken(context.Context) (music.MusicServerStatus, error)
	VerifyMusicServerToken(context.Context, string, string, string, string) (bool, error)
	StartScan(context.Context, string) (music.Scan, error)
	StartScanRoots(context.Context, []string) (music.Scan, error)
	StartScanRootsFull(context.Context, []string, bool) (music.Scan, error)
	ListScans(context.Context, int) (music.ScanList, error)
	Scan(context.Context, int64) (music.Scan, error)
	ScanPage(context.Context, int64, int) (music.ScanPage, error)
	ScanPageFiltered(context.Context, int64, int, string) (music.ScanPage, error)
	RegisterScan(context.Context, int64, []int64) (int, error)
	DiscoverScan(context.Context, int64, []int64, bool) (music.DiscoverResult, error)
	FingerprintScan(context.Context, int64, []int64, bool) (music.DiscoverResult, error)
	RequestMatchAll(context.Context, int64, bool) (music.Scan, error)
	RequestFinalize(context.Context, int64, bool) (music.Scan, error)
	AcoustIDStatus(context.Context) (music.AcoustIDStatus, error)
	SaveAcoustIDKey(context.Context, string) error
	DeleteAcoustIDKey(context.Context) error
	MusicBrainzStatus(context.Context) (music.MusicBrainzStatus, error)
	SaveMusicBrainzBaseURL(context.Context, string) (music.MusicBrainzStatus, error)
	DeleteMusicBrainzBaseURL(context.Context) (music.MusicBrainzStatus, error)
	CancelScan(context.Context, int64) error
	SkipScanFile(context.Context, int64, int64) error
	ListFiles(context.Context, int) (music.MusicFilePage, error)
	PreviewEdits(context.Context, []int64) (music.EditPlan, error)
	ApplyEdits(context.Context, []int64, string, bool) (music.EditJob, error)
	EditJob(context.Context, int64) (music.EditJob, error)
	ListEditJobs(context.Context, int) (music.EditJobList, error)
	RollbackEdits(context.Context, int64, bool) (music.EditJob, error)
	StartImport(context.Context, int64, string, string) (music.ImportJob, error)
	ImportJob(context.Context, int64) (music.ImportJob, error)
	ListImports(context.Context, int) (music.ImportJobList, error)
}

func New(db *sql.DB, cfg config.Config, logger *slog.Logger, assets fs.FS, version string, deps Dependencies) http.Handler {
	s := &Server{db: db, config: cfg, logger: logger, settings: settings.New(db), library: library.New(db), version: version, started: time.Now(), auth: deps.Auth, metadata: deps.Metadata, transfer: deps.Transfer, automation: deps.Automation, subtitle: deps.Subtitle, totp: deps.TOTP}
	s.music = deps.Music
	if s.music == nil {
		service := music.NewService(db, cfg.MediaDir)
		service.SetDownloadsDir(cfg.DownloadsDir)
		if deps.Secrets != nil {
			service.SetSecrets(deps.Secrets)
		}
		s.music = service
	}
	s.youtube = youtube.NewStore(db)
	s.backups = deps.Backups
	s.vpn = deps.VPN
	s.announce = deps.Announce
	s.metadataRefresh = deps.MetadataRefresh
	if s.metadataRefresh == nil && deps.Metadata != nil {
		s.metadataRefresh = automation.NewMetadataRefresher(db, deps.Metadata)
	}
	s.youtubeInspector = deps.YouTubeInspector
	s.bot = deps.Bot
	s.franchises = deps.Franchises
	if s.franchises == nil {
		s.franchises = metadata.NewWikidataFranchises()
	}
	s.operations = operations.New(db, cfg)
	s.organization = organization.New(db, cfg)
	s.requests = requests.New(db)
	if deps.Secrets != nil {
		s.emby = deps.Emby
		if s.emby == nil {
			s.emby = emby.New(deps.Secrets)
		}
		s.embyMonitor = deps.EmbyMonitor
		if s.embyMonitor == nil {
			s.embyMonitor = emby.NewMonitor(db, s.emby)
		}
		s.discord = requests.NewDiscord(db, deps.Secrets)
		s.requestLists = requests.NewLists(db, deps.Secrets)
	}
	s.animeProvider = deps.AnimeProvider
	s.kitsuProvider = deps.KitsuProvider
	if s.kitsuProvider == nil {
		s.kitsuProvider = metadata.NewKitsu()
	}
	if s.animeProvider == nil {
		s.animeProvider = metadata.NewAniList()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", method(http.MethodGet, s.health))
	mux.HandleFunc("/readyz", method(http.MethodGet, s.ready))
	mux.HandleFunc("/api/v1/system", method(http.MethodGet, s.system))
	mux.HandleFunc("/api/v1/setup", method(http.MethodGet, s.setupGuide))
	mux.HandleFunc("/api/v1/system/checks", s.installationChecks)
	mux.HandleFunc("/api/v1/system/storage", method(http.MethodGet, s.storageBrowser))
	mux.HandleFunc("/api/v1/download-clients", s.downloadClients)
	mux.HandleFunc("/api/v1/download-clients/defaults", method(http.MethodPut, s.downloadClientDefaults))
	mux.HandleFunc("/api/v1/download-clients/builtin", s.downloadClientBuiltin)
	mux.HandleFunc("/api/v1/download-clients/{id}", s.downloadClient)
	mux.HandleFunc("/api/v1/download-clients/{id}/test", method(http.MethodPost, s.testDownloadClient))
	mux.HandleFunc("/api/v1/usenet", s.usenetOverview)
	mux.HandleFunc("/api/v1/usenet/news", s.usenetNews)
	mux.HandleFunc("/api/v1/usenet/news/test", method(http.MethodPost, s.usenetNewsTest))
	mux.HandleFunc("/api/v1/usenet/queue", s.usenetQueue)
	mux.HandleFunc("/api/v1/usenet/queue/actions", method(http.MethodPost, s.usenetQueueActions))
	mux.HandleFunc("/api/v1/usenet/speed-limits", method(http.MethodPost, s.usenetSpeedLimit))
	mux.HandleFunc("/api/v1/system/storage/migration", method(http.MethodGet, s.storageMigration))
	mux.HandleFunc("/api/v1/system/metadata-refresh", s.metadataRefreshSettings)
	mux.HandleFunc("/api/v1/library/series/{id}/subtitles", s.showSubtitlePreferences)
	mux.HandleFunc("/api/v1/subtitles/files/{id}/edits", s.subtitleEdits)
	mux.HandleFunc("/api/v1/subtitles/edits/{id}/{action}", method(http.MethodPost, s.subtitleEditAction))
	mux.HandleFunc("/api/v1/system/metadata-refresh/run", method(http.MethodPost, s.metadataRefreshRun))
	mux.HandleFunc("/api/v1/system/recovery/resume", method(http.MethodPost, s.resumeRecovery))
	mux.HandleFunc("/api/v1/system/backups", s.backupCollection)
	mux.HandleFunc("/api/v1/system/backups/schedule", method(http.MethodPut, s.backupSchedule))
	mux.HandleFunc("/api/v1/system/backups/upload", method(http.MethodPost, s.backupUpload))
	mux.HandleFunc("/api/v1/system/backups/{id}", method(http.MethodDelete, s.backupDelete))
	mux.HandleFunc("/api/v1/system/backups/{id}/download", method(http.MethodGet, s.backupDownload))
	mux.HandleFunc("/api/v1/system/backups/{id}/preview", method(http.MethodPost, s.backupPreview))
	mux.HandleFunc("/api/v1/system/backups/{id}/restore", method(http.MethodPost, s.backupRestore))
	mux.HandleFunc("/api/v1/dashboard", method(http.MethodGet, s.dashboard))
	mux.HandleFunc("/api/v1/events", method(http.MethodGet, s.activityEvents))
	mux.HandleFunc("/api/v1/wanted", method(http.MethodGet, s.planning))
	mux.HandleFunc("/api/v1/calendar", method(http.MethodGet, s.planning))
	mux.HandleFunc("/api/v1/library", method(http.MethodGet, s.listLibrary))
	mux.HandleFunc("/api/v1/library/refresh", method(http.MethodPost, s.refreshLibrary))
	mux.HandleFunc("/api/v1/library/monitoring/preview", method(http.MethodPost, s.reviewMonitoring))
	mux.HandleFunc("/api/v1/library/monitoring", method(http.MethodPut, s.applyMonitoring))
	mux.HandleFunc("/api/v1/library/profiles/preview", method(http.MethodPost, s.reviewProfiles))
	mux.HandleFunc("/api/v1/library/profiles", method(http.MethodPut, s.applyProfiles))
	mux.HandleFunc("/api/v1/library/imports", s.existingCollection)
	mux.HandleFunc("/api/v1/library/roots", s.libraryRoots)
	mux.HandleFunc("/api/v1/library/import/backup/preview", method(http.MethodPost, s.arrBackupPreview))
	mux.HandleFunc("/api/v1/library/import/backup/apply", method(http.MethodPost, s.arrBackupApply))
	mux.HandleFunc("/api/v1/library/imports/catalog", method(http.MethodGet, s.existingCatalog))
	mux.HandleFunc("/api/v1/library/imports/{id}", s.existingScan)
	mux.HandleFunc("/api/v1/library/imports/{id}/matches/automatic", method(http.MethodPost, s.existingAutomaticMatching))
	mux.HandleFunc("/api/v1/library/imports/{id}/matches/suggest", method(http.MethodPost, s.bulkMatchSuggestions))
	mux.HandleFunc("/api/v1/library/imports/{id}/matches/review", method(http.MethodPost, s.bulkMatchReview))
	mux.HandleFunc("/api/v1/library/imports/{id}/matches/apply", method(http.MethodPost, s.bulkMatchApply))
	mux.HandleFunc("/api/v1/library/imports/{id}/cancel", method(http.MethodPost, s.existingCancel))
	mux.HandleFunc("/api/v1/library/imports/{id}/duplicates", method(http.MethodGet, s.existingDuplicates))
	mux.HandleFunc("/api/v1/library/imports/{id}/duplicates/resolve", method(http.MethodPost, s.existingDuplicateResolve))
	mux.HandleFunc("/api/v1/library/import-files/{id}/register", method(http.MethodPost, s.existingRegister))
	mux.HandleFunc("/api/v1/library/import-files/{id}/skip", method(http.MethodPost, s.existingSkip))
	mux.HandleFunc("/api/v1/library/items/{id}", method(http.MethodGet, s.itemDetails))
	mux.HandleFunc("/api/v1/library/files/{id}/inspect", method(http.MethodPost, s.inspectMediaFile))
	mux.HandleFunc("/api/v1/library/items/{id}/anime", s.animeSettings)
	mux.HandleFunc("/api/v1/library/items/{id}/anime-search", s.animeSearchSettings)
	mux.HandleFunc("/api/v1/library/anime/preferences", s.animePreferences)
	mux.HandleFunc("/api/v1/library/anime/dub-test", method(http.MethodPost, s.animeDubTest))
	mux.HandleFunc("/api/v1/library/anime/english-dub", method(http.MethodPost, s.animeEnglishDubAll))
	mux.HandleFunc("/api/v1/metadata/anime", method(http.MethodGet, s.searchAnimeIdentities))
	mux.HandleFunc("/api/v1/library/items/{id}/anime-order", s.animeOrderSettings)
	mux.HandleFunc("/api/v1/library/items/{id}/anime-order/preview", method(http.MethodPost, s.reviewAnimeOrder))
	mux.HandleFunc("/api/v1/library/items/{id}/anime-order/refresh", method(http.MethodPost, s.refreshAnimeIdentities))
	mux.HandleFunc("/api/v1/library/items/{id}/anime-order/sources", method(http.MethodGet, s.animeOrderSources))
	mux.HandleFunc("/api/v1/library/items/{id}/anime-order/propose", method(http.MethodPost, s.proposeAnimeOrder))
	mux.HandleFunc("/api/v1/releases/anime-pack", method(http.MethodPost, s.searchAnimePack))
	mux.HandleFunc("/api/v1/library/items/{id}/metadata", method(http.MethodPost, s.refreshInformation))
	mux.HandleFunc("/api/v1/indexers", s.indexers)
	mux.HandleFunc("/api/v1/indexers/status", method(http.MethodGet, s.indexerStatus))
	mux.HandleFunc("/api/v1/indexers/{id}", s.indexerItem)
	mux.HandleFunc("/api/v1/indexers/{id}/test", method(http.MethodPost, s.indexerTest))
	mux.HandleFunc("/api/v1/indexers/{id}/enabled", method(http.MethodPut, s.indexerEnabled))
	mux.HandleFunc("/api/v1/settings", s.handleSettings)
	mux.HandleFunc("/api/v1/emby", s.embySettings)
	mux.HandleFunc("/api/v1/emby/activity", method(http.MethodGet, s.embyActivity))
	mux.HandleFunc("/api/v1/emby/monitor", s.embyMonitorSettings)
	mux.HandleFunc("/api/v1/emby/history", method(http.MethodGet, s.embyHistory))
	mux.HandleFunc("/api/v1/organization", s.organizationSettings)
	mux.HandleFunc("/api/v1/organization/files", method(http.MethodGet, s.organizationFiles))
	mux.HandleFunc("/api/v1/organization/files/{id}/health", method(http.MethodPost, s.organizationFileHealth))
	mux.HandleFunc("/api/v1/organization/plans", s.organizationPlans)
	mux.HandleFunc("/api/v1/organization/removal", method(http.MethodPost, s.organizationRemoval))
	mux.HandleFunc("/api/v1/organization/acquisition", method(http.MethodPost, s.organizationAcquisition))
	mux.HandleFunc("/api/v1/organization/replacements", s.organizationReplacement)
	mux.HandleFunc("/api/v1/organization/embedded-title", method(http.MethodPost, s.organizationEmbeddedTitle))
	mux.HandleFunc("/api/v1/organization/plans/{id}/rollback", method(http.MethodPost, s.organizationRollback))
	mux.HandleFunc("/api/v1/organization/plans/{id}/recover-replacement", method(http.MethodPost, s.organizationRecoverReplacement))
	mux.HandleFunc("/api/v1/organization/plans/{id}", method(http.MethodGet, s.organizationPlan))
	mux.HandleFunc("/api/v1/organization/plans/{id}/apply", method(http.MethodPost, s.organizationApply))
	mux.HandleFunc("/api/v1/organization/plans/{id}/cancel", method(http.MethodPost, s.organizationCancel))
	mux.HandleFunc("/api/v1/library/items/{id}/organization", s.organizationAssignment)
	mux.HandleFunc("/api/v1/library/items/{id}/create-folder", method(http.MethodPost, s.createTitleFolder))
	mux.HandleFunc("/api/v1/library/items/{id}/collection", method(http.MethodGet, s.movieCollection))
	mux.HandleFunc("/api/v1/library/items/{id}/related", s.relatedTitles)
	mux.HandleFunc("/api/v1/library/items/{id}/franchise", method(http.MethodGet, s.franchiseTitles))
	mux.HandleFunc("/api/v1/subtitles/preferences", s.subtitlePreferences)
	mux.HandleFunc("/api/v1/providers/opensubtitles", s.subtitleProvider)
	mux.HandleFunc("/api/v1/providers/opensubtitles/test", method(http.MethodPost, s.subtitleProviderTest))
	mux.HandleFunc("/api/v1/subtitles/files", method(http.MethodGet, s.subtitleFiles))
	mux.HandleFunc("/api/v1/subtitles/files/{id}", method(http.MethodGet, s.subtitleFile))
	mux.HandleFunc("/api/v1/subtitles/files/{id}/{action}", method(http.MethodPost, s.subtitleFileAction))
	mux.HandleFunc("/api/v1/subtitles/candidates/{id}/download", method(http.MethodPost, s.subtitleDownload))
	mux.HandleFunc("/api/v1/automation", s.automationSettings)
	mux.HandleFunc("/api/v1/torrents", s.torrents)
	mux.HandleFunc("/api/v1/torrents/actions", method(http.MethodPost, s.torrentBulkAction))
	mux.HandleFunc("/api/v1/torrents/queue-limits", method(http.MethodPost, s.torrentQueueLimits))
	mux.HandleFunc("/api/v1/torrents/speed-limits", method(http.MethodPost, s.torrentSpeedLimits))
	mux.HandleFunc("/api/v1/torrents/share-policy", s.torrentSharePolicy)
	mux.HandleFunc("/api/v1/torrents/share-policy/run", method(http.MethodPost, s.torrentSharePolicyRun))
	mux.HandleFunc("/api/v1/torrents/public-pause", s.torrentPublicPause)
	mux.HandleFunc("/api/v1/torrents/{hash}", method(http.MethodGet, s.torrentDetail))
	mux.HandleFunc("/api/v1/torrents/{hash}/files", method(http.MethodPost, s.torrentFilePriority))
	mux.HandleFunc("/api/v1/torrents/{hash}/{action}", method(http.MethodPost, s.torrentAction))
	mux.HandleFunc("/api/v1/youtube/inspect", method(http.MethodPost, s.youtubeInspect))
	mux.HandleFunc("/api/v1/youtube/playlist/preview", method(http.MethodPost, s.youtubePlaylistPreview))
	mux.HandleFunc("/api/v1/youtube/playlist/queue", method(http.MethodPost, s.youtubePlaylistQueue))
	mux.HandleFunc("/api/v1/youtube/bulk/review", method(http.MethodPost, s.youtubeBulkReview))
	mux.HandleFunc("/api/v1/youtube/bulk/queue", method(http.MethodPost, s.youtubeBulkQueue))
	mux.HandleFunc("/api/v1/youtube/subscriptions", s.youtubeSubscriptions)
	mux.HandleFunc("/api/v1/youtube/subscriptions/{id}", s.youtubeSubscription)
	mux.HandleFunc("/api/v1/youtube/collections", s.youtubeCollections)
	mux.HandleFunc("/api/v1/youtube/reviews", method(http.MethodPost, s.youtubeReview))
	mux.HandleFunc("/api/v1/youtube/reviews/{id}/queue", method(http.MethodPost, s.youtubeQueue))
	mux.HandleFunc("/api/v1/youtube/jobs", method(http.MethodGet, s.youtubeJobs))
	mux.HandleFunc("/api/v1/youtube/jobs/{id}/import-review", method(http.MethodGet, s.youtubeImportReview))
	mux.HandleFunc("/api/v1/youtube/jobs/{id}/import", method(http.MethodPost, s.youtubeImport))
	mux.HandleFunc("/api/v1/youtube/jobs/{id}/cancel", method(http.MethodPost, s.youtubeCancel))
	mux.HandleFunc("/api/v1/youtube/files", method(http.MethodGet, s.youtubeFiles))
	mux.HandleFunc("/api/v1/automation/rss", s.rssSettings)
	mux.HandleFunc("/api/v1/automation/rss/run", method(http.MethodPost, s.rssRequest))
	mux.HandleFunc("/api/v1/automation/run", method(http.MethodPost, s.automationRequest))
	mux.HandleFunc("/api/v1/automation/search-item", method(http.MethodPost, s.automationSearchItem))
	mux.HandleFunc("/api/v1/automation/search-bulk", method(http.MethodPost, s.automationSearchBulk))
	mux.HandleFunc("/api/v1/automation/item-results", method(http.MethodGet, s.automationItemResults))
	mux.HandleFunc("/api/v1/automation/runs", method(http.MethodGet, s.automationRuns))
	mux.HandleFunc("/api/v1/automation/runs/{id}", method(http.MethodGet, s.automationResults))
	mux.HandleFunc("/api/v1/automation/items", method(http.MethodGet, s.automationItems))
	mux.HandleFunc("/api/v1/automation/items/{id}", method(http.MethodPut, s.automationAssign))
	mux.HandleFunc("/api/v1/library/series/{id}/release-rules", s.seriesReleaseRules)
	mux.HandleFunc("/api/v1/library/series/{id}/youtube-sources", s.seriesYouTubeSources)
	mux.HandleFunc("/api/v1/library/series/{id}/youtube-sources/preview", method(http.MethodPost, s.seriesYouTubePreview))
	mux.HandleFunc("/api/v1/library/series/{id}/youtube-sources/{source}", s.seriesYouTubeSource)
	mux.HandleFunc("/api/v1/quality-profiles", s.qualityProfiles)
	mux.HandleFunc("/api/v1/quality-profiles/{id}", method(http.MethodPut, s.qualityProfileUpdate))
	mux.HandleFunc("/api/v1/auth/session", method(http.MethodGet, s.authSession))
	mux.HandleFunc("/api/v1/auth/setup", method(http.MethodPost, s.authSetup))
	mux.HandleFunc("/api/v1/auth/login", method(http.MethodPost, s.authLogin))
	mux.HandleFunc("/api/v1/auth/logout", method(http.MethodPost, s.authLogout))
	mux.HandleFunc("/api/v1/auth/totp", method(http.MethodGet, s.totpStatus))
	mux.HandleFunc("/api/v1/auth/totp/provision", method(http.MethodPost, s.totpProvision))
	mux.HandleFunc("/api/v1/auth/totp/confirm", method(http.MethodPost, s.totpConfirm))
	mux.HandleFunc("/api/v1/auth/totp/disable", method(http.MethodPost, s.totpDisable))
	mux.HandleFunc("/api/v1/request-accounts", s.requestAccounts)
	mux.HandleFunc("/api/v1/request-accounts/{id}", method(http.MethodPut, s.requestAccountState))
	mux.HandleFunc("/api/v1/request-accounts/{id}/discord", method(http.MethodPut, s.requestAccountDiscord))
	mux.HandleFunc("/api/v1/discord-bot", s.discordBotSettings)
	mux.HandleFunc("/api/v1/discord-bot/register", method(http.MethodPost, s.discordBotRegister))
	mux.HandleFunc("/api/v1/discord-bot/notices/{id}/retry", method(http.MethodPost, s.discordBotRetryNotice))
	mux.HandleFunc("/api/v1/discord/interactions", s.discordInteraction)
	mux.HandleFunc("/api/v1/requester/search/movies", method(http.MethodGet, s.searchMovies))
	mux.HandleFunc("/api/v1/requester/search/series", method(http.MethodGet, s.searchSeries))
	mux.HandleFunc("/api/v1/requester/requests", s.requesterRequests)
	mux.HandleFunc("/api/v1/requester/discord", s.requesterDiscord)
	mux.HandleFunc("/api/v1/requester/password", method(http.MethodPut, s.requesterPassword))
	mux.HandleFunc("/api/v1/requester/lists", s.requesterLists)
	mux.HandleFunc("/api/v1/requester/lists/{id}", method(http.MethodDelete, s.requesterDeleteList))
	mux.HandleFunc("/api/v1/requester/lists/{id}/refresh", method(http.MethodPost, s.requesterRefreshList))
	mux.HandleFunc("/api/v1/requester/imdb-csv", method(http.MethodPost, s.requesterIMDbCSV))
	mux.HandleFunc("/api/v1/requester/requests/{id}/notification/retry", method(http.MethodPost, s.requesterRetryNotification))
	mux.HandleFunc("/api/v1/requests", method(http.MethodGet, s.adminRequests))
	mux.HandleFunc("/api/v1/requests/{id}/{action}", method(http.MethodPost, s.requestAction))
	mux.HandleFunc("/api/v1/providers/tmdb", s.tmdbSettings)
	mux.HandleFunc("/api/v1/providers/tmdb/test", method(http.MethodPost, s.tmdbTest))
	mux.HandleFunc("/api/v1/metadata/movies", method(http.MethodGet, s.searchMovies))
	mux.HandleFunc("/api/v1/metadata/movies/{id}", method(http.MethodGet, s.movieDetails))
	mux.HandleFunc("/api/v1/metadata/people/{id}/credits", method(http.MethodGet, s.personCredits))
	mux.HandleFunc("/api/v1/library/movies", method(http.MethodPost, s.addMovie))
	mux.HandleFunc("/api/v1/library/movies/{id}", method(http.MethodGet, s.catalogMovie))
	mux.HandleFunc("/api/v1/metadata/series", method(http.MethodGet, s.searchSeries))
	mux.HandleFunc("/api/v1/metadata/series/{id}", method(http.MethodGet, s.seriesDetails))
	mux.HandleFunc("/api/v1/library/series", method(http.MethodPost, s.addSeries))
	mux.HandleFunc("/api/v1/library/series/{id}", method(http.MethodGet, s.catalogSeries))
	mux.HandleFunc("/api/v1/metadata/music/artists", method(http.MethodGet, s.searchMusicArtists))
	mux.HandleFunc("/api/v1/library/music/artists", s.musicArtists)
	mux.HandleFunc("/api/v1/library/music/artists/{id}", method(http.MethodGet, s.musicArtist))
	mux.HandleFunc("/api/v1/library/music/artists/{id}/tracks", method(http.MethodGet, s.musicArtistTracks))
	mux.HandleFunc("/api/v1/library/music/artists/{id}/top-tracks", method(http.MethodGet, s.musicArtistTopTracks))
	mux.HandleFunc("/api/v1/library/music/artists/{id}/discography", method(http.MethodGet, s.musicArtistDiscography))
	mux.HandleFunc("/api/v1/library/music/artists/{id}/image", method(http.MethodGet, s.musicArtistImage))
	mux.HandleFunc("/api/v1/library/music/acquisitions", s.musicAcquisitions)
	mux.HandleFunc("/api/v1/library/music/acquisitions/{id}", method(http.MethodGet, s.musicAcquisition))
	mux.HandleFunc("/api/v1/library/music/links/preview", method(http.MethodPost, s.musicLinkPreview))
	mux.HandleFunc("/api/v1/library/music/links", method(http.MethodPost, s.musicLinkAcquire))
	mux.HandleFunc("/api/v1/library/music/charts", method(http.MethodGet, s.musicCharts))
	mux.HandleFunc("/api/v1/library/music/monitor", s.musicMonitor)
	mux.HandleFunc("/api/v1/library/music/monitor/sources", s.musicMonitorSources)
	mux.HandleFunc("/api/v1/library/music/monitor/sources/{id}", s.musicMonitorSource)
	mux.HandleFunc("/api/v1/library/music/monitor/run", method(http.MethodPost, s.musicMonitorRun))
	mux.HandleFunc("/api/v1/library/music/albums", method(http.MethodGet, s.musicAlbums))
	mux.HandleFunc("/api/v1/library/music/albums/{id}", method(http.MethodGet, s.musicAlbum))
	mux.HandleFunc("/api/v1/library/music/albums/{id}/cover", method(http.MethodGet, s.musicAlbumCover))
	mux.HandleFunc("/api/v1/library/music/albums/{id}/acquire", method(http.MethodPost, s.musicAlbumAcquire))
	mux.HandleFunc("/api/v1/library/music/provider-albums/{id}/acquire", method(http.MethodPost, s.musicProviderAlbumAcquire))
	mux.HandleFunc("/api/v1/library/music/tracks", method(http.MethodGet, s.musicTracks))
	mux.HandleFunc("/api/v1/library/music/server", s.musicServer)
	mux.HandleFunc("/api/v1/library/music/server/token", s.musicServerToken)
	mux.HandleFunc("/rest/", s.subsonic)
	mux.HandleFunc("/api/v1/library/music/scans", s.musicScans)
	mux.HandleFunc("/api/v1/library/music/acoustid", s.musicAcoustID)
	mux.HandleFunc("/api/v1/library/music/musicbrainz", s.musicMusicBrainz)
	mux.HandleFunc("/api/v1/library/music/scans/{id}", method(http.MethodGet, s.musicScan))
	mux.HandleFunc("/api/v1/library/music/scans/{id}/register", method(http.MethodPost, s.musicScanRegister))
	mux.HandleFunc("/api/v1/library/music/scans/{id}/discover", method(http.MethodPost, s.musicScanDiscover))
	mux.HandleFunc("/api/v1/library/music/scans/{id}/fingerprint", method(http.MethodPost, s.musicScanFingerprint))
	mux.HandleFunc("/api/v1/library/music/scans/{id}/match-all", method(http.MethodPost, s.musicScanMatchAll))
	mux.HandleFunc("/api/v1/library/music/scans/{id}/finalize", method(http.MethodPost, s.musicScanFinalize))
	mux.HandleFunc("/api/v1/library/music/scans/{id}/cancel", method(http.MethodPost, s.musicScanCancel))
	mux.HandleFunc("/api/v1/library/music/scans/{id}/skip", method(http.MethodPost, s.musicScanSkip))
	mux.HandleFunc("/api/v1/library/music/files", method(http.MethodGet, s.musicFiles))
	mux.HandleFunc("/api/v1/library/music/files/{id}/stream", method(http.MethodGet, s.musicFileStream))
	mux.HandleFunc("/api/v1/library/music/edits", method(http.MethodGet, s.musicEdits))
	mux.HandleFunc("/api/v1/library/music/edits/preview", method(http.MethodPost, s.musicEditPreview))
	mux.HandleFunc("/api/v1/library/music/edits/apply", method(http.MethodPost, s.musicEditApply))
	mux.HandleFunc("/api/v1/library/music/edits/{id}", method(http.MethodGet, s.musicEdit))
	mux.HandleFunc("/api/v1/library/music/edits/{id}/rollback", method(http.MethodPost, s.musicEditRollback))
	mux.HandleFunc("/api/v1/library/music/albums/{id}/releases", method(http.MethodPost, s.musicAlbumReleases))
	mux.HandleFunc("/api/v1/library/music/albums/{id}/import", method(http.MethodPost, s.musicAlbumImport))
	mux.HandleFunc("/api/v1/library/music/imports", method(http.MethodGet, s.musicImports))
	mux.HandleFunc("/api/v1/library/music/imports/{id}", method(http.MethodGet, s.musicImport))
	mux.HandleFunc("/api/v1/library/music/preferences", s.musicPreferences)
	mux.HandleFunc("/api/v1/library/music/wanted", method(http.MethodGet, s.musicWantedAlbums))
	mux.HandleFunc("/api/v1/library/music/schedule", s.musicSchedule)
	mux.HandleFunc("/api/v1/library/music/schedule/run", method(http.MethodPost, s.musicScheduleRun))
	mux.HandleFunc("/api/v1/library/music/albums/{id}/download", method(http.MethodPost, s.musicAlbumDownload))
	mux.HandleFunc("/api/v1/library/music/albums/{id}/preferences", s.musicAlbumPreferences)
	mux.HandleFunc("/api/v1/library/music/upgrades", method(http.MethodGet, s.musicUpgrades))
	mux.HandleFunc("/api/v1/library/music/downloads", method(http.MethodGet, s.musicDownloads))
	mux.HandleFunc("/api/v1/library/music/downloads/{id}", method(http.MethodGet, s.musicDownload))
	mux.HandleFunc("/api/v1/library/music/downloads/{id}/cancel", method(http.MethodPost, s.musicDownloadCancel))
	mux.HandleFunc("/api/v1/library/music/downloads/{id}/retry", method(http.MethodPost, s.musicDownloadRetry))
	mux.HandleFunc("/api/v1/library/music/downloads/{id}/files", method(http.MethodGet, s.musicDownloadFiles))
	mux.HandleFunc("/api/v1/library/music/downloads/{id}/mapping", method(http.MethodPost, s.musicDownloadMapping))
	mux.HandleFunc("/api/v1/library/series/{id}/refresh", method(http.MethodPost, s.refreshTV))
	mux.HandleFunc("/api/v1/library/series/{id}/monitor", method(http.MethodPut, s.monitorTV))
	mux.HandleFunc("/api/v1/library/series/{id}/monitor-all", method(http.MethodPut, s.monitorAllEpisodes))
	mux.HandleFunc("/api/v1/library/series/{id}/episodes/{episode}/remove-file", method(http.MethodPost, s.unlinkEpisodeFile))
	mux.HandleFunc("/api/v1/library/monitor-all-episodes", method(http.MethodPost, s.monitorAllShows))
	mux.HandleFunc("/api/v1/library/series/{id}/seasons/monitor", method(http.MethodPut, s.monitorSeasons))
	mux.HandleFunc("/api/v1/library/series/{id}/seasons/{number}", method(http.MethodGet, s.catalogSeason))
	mux.HandleFunc("/api/v1/library/series/{id}/seasons/{number}/refresh", method(http.MethodPost, s.refreshTV))
	mux.HandleFunc("/api/v1/library/series/{id}/seasons/{number}/monitor", method(http.MethodPut, s.monitorTV))
	mux.HandleFunc("/api/v1/library/series/{id}/seasons/{number}/episodes/monitor", method(http.MethodPut, s.monitorEpisodes))
	mux.HandleFunc("/api/v1/library/series/{id}/seasons/{number}/episodes/{episode}/monitor", method(http.MethodPut, s.monitorTV))
	mux.HandleFunc("/api/v1/integrations/{name}", s.integrationSettings)
	mux.HandleFunc("/api/v1/integrations/{name}/test", method(http.MethodPost, s.integrationTest))
	mux.HandleFunc("/api/v1/releases", method(http.MethodGet, s.searchReleases))
	mux.HandleFunc("/api/v1/releases/blacklist", method(http.MethodGet, s.releaseBlacklist))
	mux.HandleFunc("/api/v1/releases/blacklist/{id}", method(http.MethodDelete, s.releaseBlacklistEntry))
	mux.HandleFunc("/api/v1/duplicates", method(http.MethodGet, s.duplicateReview))
	mux.HandleFunc("/api/v1/metrics", method(http.MethodGet, s.prometheusMetrics))
	mux.HandleFunc("/api/v1/library/series/{id}/seasons/{number}/releases", method(http.MethodGet, s.searchTVReleases))
	mux.HandleFunc("/api/v1/library/series/{id}/manual-torrent", method(http.MethodPost, s.manualSeriesTorrent))
	mux.HandleFunc("/api/v1/library/movies/{id}/manual-torrent", method(http.MethodPost, s.manualMovieTorrent))
	mux.HandleFunc("/api/v1/vpn/mullvad", s.vpnMullvad)
	mux.HandleFunc("/api/v1/announce-sources", s.announceSources)
	mux.HandleFunc("/api/v1/announce-sources/{id}", s.announceSource)
	mux.HandleFunc("/api/v1/announces", method(http.MethodGet, s.announceEvents))
	mux.HandleFunc("/api/v1/downloads", s.downloadCollection)
	mux.HandleFunc("/api/v1/library/movies/{id}/versions", s.movieVersions)
	mux.HandleFunc("/api/v1/downloads/{id}", method(http.MethodGet, s.downloadDetail))
	mux.HandleFunc("/api/v1/downloads/{id}/{action}", method(http.MethodPost, s.downloadAction))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, 404, "not_found", "This API endpoint does not exist.")
	})
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		fail(w, 404, "not_found", "This API endpoint does not exist.")
	})
	mux.HandleFunc("/", frontend(assets))
	return s.middleware(s.security(mux))
}

func method(verb string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != verb {
			w.Header().Set("Allow", verb)
			fail(w, 405, "method_not_allowed", "This method is not supported.")
			return
		}
		next(w, r)
	}
}

func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, code, message string) {
	send(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (s *Server) internal(w http.ResponseWriter, operation string, err error) {
	s.logger.Error("request failed", "operation", operation, "error", err)
	fail(w, 500, "internal_error", "The request could not be completed. Check the application logs and try again.")
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	send(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		fail(w, 503, "database_unavailable", "The database is not ready.")
		return
	}
	send(w, 200, map[string]string{"status": "ready"})
}

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	version, err := database.Version(r.Context(), s.db)
	if err != nil {
		s.internal(w, "system", err)
		return
	}
	send(w, 200, map[string]any{
		"name": "ReelScarey", "version": s.version, "milestone": "hardening",
		"database": "connected", "schema_version": version, "uptime_seconds": int(time.Since(s.started).Seconds()),
		"paths":        map[string]string{"config": s.config.ConfigDir, "media": s.config.MediaDir, "downloads": s.config.DownloadsDir},
		"capabilities": map[string]bool{"authentication": true, "library_settings": true, "metadata_search": true, "movie_add": true, "series_catalog": s.metadata != nil && s.metadata.SupportsSeries(), "episode_monitoring": true, "tv_downloads": s.transfer != nil, "torznab_search": s.transfer != nil, "downloads": s.transfer != nil, "import": s.transfer != nil, "persistent_jobs": s.transfer != nil, "subtitles": s.subtitle != nil, "automation": s.automation != nil, "quality_profiles": true, "music": s.music != nil},
	})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	counts, err := s.library.Counts(r.Context())
	if err != nil {
		s.internal(w, "library_counts", err)
		return
	}
	prefs, err := s.settings.Get(r.Context())
	if err != nil {
		s.internal(w, "dashboard_settings", err)
		return
	}
	send(w, 200, map[string]any{"library_name": prefs.LibraryName, "counts": counts, "settings_revision": prefs.Revision, "settings_updated_at": prefs.UpdatedAt})
}

func (s *Server) listLibrary(w http.ResponseWriter, r *http.Request) {
	values, parseErr := url.ParseQuery(r.URL.RawQuery)
	allowed := map[string]bool{"kind": true, "page": true, "query": true, "status": true, "monitored": true, "sort": true, "profile": true}
	if parseErr != nil {
		fail(w, 400, "invalid_library", library.ErrBrowseInput.Error())
		return
	}
	for k, v := range values {
		if !allowed[k] || len(v) != 1 {
			fail(w, 400, "invalid_library", library.ErrBrowseInput.Error())
			return
		}
	}
	kind := values.Get("kind")
	if kind != "movie" && kind != "series" && kind != "anime" {
		fail(w, 400, "invalid_kind", "Choose kind=movie, kind=series or kind=anime.")
		return
	}
	all := values.Get("page") == "all"
	page := 1
	if !all {
		var valid bool
		page, valid = pageNumber(r, 100000)
		if !valid {
			fail(w, 400, "invalid_page", "Choose all or a page between 1 and 100000.")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	items, err := s.library.Browse(ctx, library.BrowseFilter{Kind: kind, Page: page, All: all, Query: values.Get("query"), Status: values.Get("status"), Monitored: values.Get("monitored"), Sort: values.Get("sort"), Profile: values.Get("profile")})
	if errors.Is(err, library.ErrBrowseAllLimit) {
		fail(w, 400, "library_too_large", err.Error())
		return
	}
	if errors.Is(err, library.ErrBrowseInput) {
		fail(w, 400, "invalid_library", err.Error())
		return
	}
	if err != nil {
		s.internal(w, "library_list", err)
		return
	}
	send(w, 200, items)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		prefs, err := s.settings.Get(r.Context())
		if err != nil {
			s.internal(w, "settings_get", err)
			return
		}
		send(w, 200, prefs)
	case http.MethodPut:
		if !s.sameOrigin(r) {
			fail(w, 403, "cross_origin", "Settings must be saved from the ReelScarey application origin.")
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			fail(w, 415, "unsupported_media_type", "Use application/json.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var in settings.Update
		if err := decoder.Decode(&in); err != nil {
			invalidJSON(w, err)
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			invalidJSON(w, err)
			return
		}
		out, err := s.settings.Update(r.Context(), in)
		if errors.Is(err, settings.ErrConflict) {
			fail(w, 409, "revision_conflict", err.Error())
			return
		}
		var invalid *settings.ValidationError
		if errors.As(err, &invalid) {
			fail(w, 400, "validation_error", invalid.Error())
			return
		}
		if err != nil {
			s.internal(w, "settings_update", err)
			return
		}
		send(w, 200, out)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT for settings.")
	}
}

func invalidJSON(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(w, 413, "body_too_large", "The request body must be at most 16 KiB.")
		return
	}
	fail(w, 400, "invalid_json", "Send one valid JSON object with only the documented fields.")
}

func (s *Server) sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	} // Non-browser clients still require JSON.
	if s.config.PublicURL != "" {
		return strings.EqualFold(origin, s.config.PublicURL)
	}
	u, err := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && u.Scheme == scheme && strings.EqualFold(u.Host, r.Host) && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

func frontend(assets fs.FS) http.HandlerFunc {
	files := http.FileServer(http.FS(assets))
	routes := map[string]bool{"/": true, "/login": true, "/movies": true, "/series": true, "/anime": true, "/youtube": true, "/organization": true, "/search": true, "/releases": true, "/activity": true, "/timeline": true, "/watch-activity": true, "/automation": true, "/subtitles": true, "/settings": true, "/setup": true, "/system": true, "/indexers": true, "/import": true, "/wanted": true, "/calendar": true, "/requests": true}
	routes["/backups"] = true
	routes["/storage"] = true
	routes["/download-clients"] = true
	routes["/metadata-refresh"] = true
	routes["/youtube/subscriptions"] = true
	routes["/announces"] = true
	routes["/torrents"] = true
	routes["/usenet"] = true
	routes["/music"] = true
	routes["/music/import"] = true
	routes["/music/server"] = true
	routes["/music/discover"] = true
	routes["/music/organize"] = true
	routes["/music/wanted"] = true
	serveIndex := func(w http.ResponseWriter, r *http.Request, status int) {
		body, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			fail(w, 503, "frontend_unavailable", "Build the frontend before starting the application.")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(status)
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			fail(w, 405, "method_not_allowed", "Use GET or HEAD.")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		seriesID, seriesErr := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/series/"), 10, 64)
		movieID, movieErr := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/movies/"), 10, 64)
		musicArtistID, musicArtistErr := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/music/artists/"), 10, 64)
		musicAlbumID, musicAlbumErr := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/music/albums/"), 10, 64)
		if routes[r.URL.Path] || (strings.HasPrefix(r.URL.Path, "/series/") && seriesErr == nil && seriesID > 0) || (strings.HasPrefix(r.URL.Path, "/movies/") && movieErr == nil && movieID > 0) || (strings.HasPrefix(r.URL.Path, "/music/artists/") && musicArtistErr == nil && musicArtistID > 0) || (strings.HasPrefix(r.URL.Path, "/music/albums/") && musicAlbumErr == nil && musicAlbumID > 0) {
			serveIndex(w, r, http.StatusOK)
			return
		}
		info, err := fs.Stat(assets, name)
		if err == nil && !info.IsDir() && !strings.HasPrefix(filepath.Base(name), ".") {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		// Unknown extensionless paths are client-side routes. Serve the
		// application with a 404 status so its own not-found page handles them,
		// while missing assets continue to return a plain 404.
		if !strings.Contains(path.Base(r.URL.Path), ".") && !strings.HasPrefix(r.URL.Path, "/assets/") {
			serveIndex(w, r, http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *responseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(body)
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://image.tmdb.org https://coverartarchive.org https://archive.org; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		if r.TLS != nil || strings.HasPrefix(s.config.PublicURL, "https://") {
			w.Header().Set("Strict-Transport-Security", "max-age=86400")
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("request panic", "method", r.Method, "path", r.URL.Path)
				if wrapped.status == 0 {
					fail(wrapped, 500, "internal_error", "An unexpected error occurred.")
				}
			}
			status := wrapped.status
			if status == 0 {
				status = 200
			}
			s.logger.Info("http request", "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(wrapped, r)
	})
}
