export type Settings = {
  library_name: string;
  movie_folder: string;
  series_folder: string;
  episode_naming?: "standard" | "with-title";
  register_unverified_video: boolean;
  import_mode: "hardlink" | "copy";
  nfo_export: boolean;
  revision: number;
  updated_at: string;
};

export type SettingsUpdate = Omit<Settings, "updated_at">;
export type System = {
  name: string;
  version: string;
  milestone: string;
  database: string;
  schema_version: number;
  uptime_seconds: number;
  paths: { config: string; media: string; downloads: string };
  capabilities: Record<string, boolean>;
};
export type Dashboard = {
  library_name: string;
  counts: { movies: number; series: number; anime?: number };
  settings_revision: number;
  settings_updated_at: string;
};
export type ProfileSummary = { id: number; name: string; revision: number };
export type CatalogProfileContext = {
  default_profile: ProfileSummary;
  profiles: ProfileSummary[];
};
export type CatalogProfile = {
  profile_id: number | null;
  effective_profile: ProfileSummary;
};
export type MediaItem = {
  id: number;
  kind: "movie" | "series";
  tmdb_id: number;
  title: string;
  year: number | null;
  monitored: boolean;
  status: string;
  overview: string;
  poster_url: string;
  poster_path: string;
  release_date: string;
  runtime_minutes: number | null;
  quality_profile?: CatalogProfile;
  category?: "standard" | "anime";
  category_revision?: number;
};

/** One recent cast role returned for a TMDb person. */
export type PersonCredit = {
  kind: "movie" | "series";
  tmdb_id: number;
  title: string;
  year?: number;
  date?: string;
  character: string;
  poster_url: string;
  category: "standard" | "anime";
  in_library: boolean;
};

export type MusicArtist = {
  id?: number;
  musicbrainz_id: string;
  name: string;
  sort_name: string;
  disambiguation: string;
  country: string;
  type: string;
  overview: string;
  image_url: string;
  monitored: boolean;
  status: string;
  album_count: number;
  in_library?: boolean;
};

export type MusicAlbum = {
  id?: number;
  artist_id?: number;
  artist_name?: string;
  musicbrainz_id: string;
  title: string;
  primary_type: string;
  secondary_types: string[];
  year: number | null;
  release_date: string;
  cover_url: string;
  monitored: boolean;
  status: string;
  track_count: number;
};

export type MusicTrack = {
  id?: number;
  album_id?: number;
  musicbrainz_id: string;
  recording_id: string;
  disc: number;
  number: number;
  title: string;
  duration_ms: number;
  /** Preferred registered file for playback, when one exists. */
  file_id?: number | null;
};

export type MusicArtistPage = {
  items: MusicArtist[];
  page: number;
  total: number;
  limit: number;
};

export type MusicSearchResults = {
  items: MusicArtist[];
  page: number;
  total_pages: number;
  total_results: number;
};

export type MusicArtistDetails = {
  artist: MusicArtist;
  albums: MusicAlbum[];
};

export type MusicArtistTrack = MusicTrack & {
  artist_name?: string;
  album_title: string;
  album_year: number | null;
  cover_url: string;
};

export type MusicArtistTrackPage = {
  items: MusicArtistTrack[];
  page: number;
  total: number;
  limit: number;
};

export type MusicAlbumPage = {
  items: MusicAlbum[];
  page: number;
  total: number;
  limit: number;
};

export type MusicDiscographyAlbum = {
  provider_id: string;
  title: string;
  artist: string;
  year: number | null;
  cover_url: string;
  track_count: number;
  in_library: boolean;
  album_id: number | null;
  status: string;
};

export type MusicDiscographyPage = {
  source: string;
  items: MusicDiscographyAlbum[];
  message: string;
};

export type MusicTopTrack = {
  provider_id: string;
  title: string;
  artist: string;
  album: string;
  cover_url: string;
  duration_ms: number;
  in_library: boolean;
  file_id: number | null;
  album_id: number | null;
  track_id: number | null;
};

export type MusicTopTrackPage = {
  source: string;
  items: MusicTopTrack[];
  message: string;
};

export type MusicAcquisitionTrackInput = {
  title: string;
  artist: string;
  album: string;
  album_artist: string;
  year?: number | null;
  track_number: number;
  disc_number: number;
  duration_ms: number;
  cover_url: string;
  source_url: string;
  search_query: string;
};

export type MusicAcquisitionTrack = MusicAcquisitionTrackInput & {
  id: number;
  job_id: number;
  position: number;
  state: string;
  error: string;
  music_file_id: number | null;
  import_job_id: number | null;
};

export type MusicAcquisitionJob = {
  id: number;
  source_url: string;
  source_kind: string;
  title: string;
  root: string;
  format: string;
  state: string;
  track_count: number;
  completed_count: number;
  error: string;
  created_at: number;
  updated_at: number;
  tracks?: MusicAcquisitionTrack[];
};

export type MusicAcquisitionJobList = {
  items: MusicAcquisitionJob[];
  page: number;
  total: number;
  limit: number;
};

export type MusicLinkTrack = {
  title: string;
  artist: string;
  album: string;
  album_artist: string;
  year: number | null;
  track_number: number;
  disc_number: number;
  duration_ms: number;
  cover_url: string;
  source_url: string;
  search_query: string;
};

export type MusicLinkPreview = {
  source: string;
  kind: string;
  title: string;
  subtitle: string;
  cover_url: string;
  tracks: MusicLinkTrack[];
};

export type MusicMonitorSettings = {
  enabled: boolean;
  interval_hours: number;
  next_run: number;
  last_run: number;
  last_error: string;
  revision: number;
  updated_at: number;
};

export type MusicMonitorSource = {
  id: number;
  url: string;
  title: string;
  kind: string;
  provider: string;
  enabled: boolean;
  last_checked: number;
  last_error: string;
  created_at: number;
  updated_at: number;
};

export type MusicMonitorSourceList = {
  items: MusicMonitorSource[];
};

export type MusicServerStatus = {
  enabled: boolean;
  configured: boolean;
  token_hint: string;
  revision: number;
  updated_at: number;
};

export type MusicAlbumDetails = {
  album: MusicAlbum;
  tracks: MusicTrack[];
};

/** Workflow phase of a reviewed music scan. */
export type MusicScanPhase =
  | "queued"
  | "scanning"
  | "matching"
  | "fingerprinting"
  | "ready"
  | "organizing"
  | "completed"
  | "failed"
  | "cancelled";

export type MusicScan = {
  id: number;
  root: string;
  roots?: string[];
  state: "scanning" | "review" | "completed" | "failed" | "cancelled";
  /** Extended workflow fields; older servers omit them. */
  full_scan?: boolean;
  phase?: MusicScanPhase;
  progress_total?: number;
  progress_done?: number;
  current_path?: string;
  message?: string;
  skipped_count?: number;
  file_count: number;
  matched_count: number;
  unresolved_count: number;
  registered_count: number;
  request?: "match" | "finalize" | "";
  error: string;
  created_at: number;
  updated_at: number;
};

export type LibraryRoot = {
  path: string;
  available: boolean;
  default: boolean;
  message?: string;
};

export type LibraryRoots = {
  key: "movies" | "tv" | "anime" | "music" | string;
  name: string;
  kind: string;
  configured: boolean;
  roots: LibraryRoot[];
  message?: string;
};

export type LibraryRootsConfig = {
  revision: number;
  media_root: string;
  libraries: LibraryRoots[];
};

export type MusicScanFile = {
  id: number;
  scan_id: number;
  relative_path: string;
  size_bytes: number;
  modified_at: number;
  sha256: string;
  format: string;
  duration_ms: number;
  bitrate: number;
  sample_rate: number;
  channels: number;
  tag_artist: string;
  tag_album_artist: string;
  tag_album: string;
  tag_title: string;
  tag_track: number | null;
  tag_disc: number | null;
  tag_year: number | null;
  tag_musicbrainz_artist: string;
  tag_musicbrainz_album: string;
  tag_musicbrainz_release_group: string;
  tag_musicbrainz_track: string;
  tag_musicbrainz_recording: string;
  tag_json: string;
  artist_id: number | null;
  album_id: number | null;
  track_id: number | null;
  state: "matched" | "unresolved" | "registered" | "skipped";
  error: string;
};

export type MusicScanList = {
  items: MusicScan[];
  page: number;
  total: number;
  limit: number;
};

export type MusicScanPage = {
  scan: MusicScan;
  items: MusicScanFile[];
  page: number;
  total: number;
  limit: number;
};

export type MusicDiscoverResult = {
  selected: number;
  matched: number;
  artists_added: number;
  albums_added: number;
  unresolved: number;
  remaining: number;
  deferred: number;
  warnings: string[];
};

export type AcoustIDStatus = {
  configured: boolean;
  updated_at: string;
  fingerprint_available: boolean;
  message: string;
};

/** Optional MusicBrainz provider and local cache status. No secret is exposed. */
export type MusicBrainzStatus = {
  base_url: string;
  default_base_url: string;
  using_default: boolean;
  cooldown_until: number;
  message: string;
  cache_entries: number;
};

/** Fingerprint matching extends discovery with Chromaprint evidence counts. */
export type MusicFingerprintResult = MusicDiscoverResult & {
  fingerprinted: number;
  provider_calls: number;
};

export type MusicFile = {
  id: number;
  artist_id: number;
  album_id: number;
  track_id: number | null;
  artist_name: string;
  album_title: string;
  track_title: string;
  disc: number;
  number: number;
  relative_path: string;
  format: string;
  duration_ms: number;
  size_bytes: number;
};

export type MusicFilePage = {
  items: MusicFile[];
  page: number;
  total: number;
  limit: number;
};

export type MusicEditTags = {
  artist: string;
  album_artist: string;
  album: string;
  title: string;
  track: number;
  track_total: number;
  disc: number;
  disc_total: number;
  year: number;
  musicbrainz_artist_id: string;
  musicbrainz_album_id: string;
  musicbrainz_release_group_id: string;
  musicbrainz_track_id: string;
  musicbrainz_recording_id: string;
};

export type MusicEditFile = {
  music_file_id: number;
  source_path: string;
  destination_path: string;
  action: "move" | "retag" | "move_retag";
  tags: MusicEditTags;
};

export type MusicEditPlan = {
  files: MusicEditFile[];
  plan_hash: string;
};

export type MusicEditJobFile = {
  music_file_id: number;
  source_path: string;
  destination_path: string;
  action: string;
  state: string;
  error: string;
};

export type MusicEditJob = {
  id: number;
  state: "running" | "completed" | "failed" | "rolled_back";
  file_count: number;
  error: string;
  created_at: number;
  updated_at: number;
  files?: MusicEditJobFile[];
};

export type MusicEditJobList = {
  items: MusicEditJob[];
  page: number;
  total: number;
  limit: number;
};

export type MusicRelease = {
  id: string;
  protocol: string;
  indexer_id: string;
  indexer_name: string;
  freeleech: boolean;
  title: string;
  size: number;
  seeders: number | null;
  peers: number | null;
  published: string;
  info_url: string;
  parsed_artist: string;
  parsed_album: string;
  year: number | null;
  format: string;
  bitrate: number;
  lossless: boolean;
};

export type MusicReleaseSearch = {
  items: MusicRelease[];
  warnings?: string[];
};

export type MusicImportFile = {
  id: number;
  source_path: string;
  track_id: number | null;
  destination_path: string;
  state: string;
  error: string;
};

export type MusicImportJob = {
  id: number;
  album_id: number;
  root: string;
  source_path: string;
  state: "running" | "completed" | "failed";
  file_count: number;
  imported_count: number;
  error: string;
  created_at: number;
  updated_at: number;
  files?: MusicImportFile[];
};

export type MusicImportJobList = {
  items: MusicImportJob[];
  page: number;
  total: number;
  limit: number;
};

export type MusicPreferences = {
  allow_lossless: boolean;
  min_bitrate: number;
  preferred_formats: string[];
  revision: number;
};

export type MusicWantedAlbum = {
  id: number;
  artist_id: number;
  artist_name: string;
  title: string;
  year: number | null;
  status: string;
  track_count: number;
};

export type MusicWantedPage = {
  items: MusicWantedAlbum[];
  page: number;
  total: number;
  limit: number;
};

export type MusicSchedule = {
  enabled: boolean;
  interval_hours: number;
  mode: "search" | "download";
  next_run: number;
  last_run: number;
  last_error: string;
  revision: number;
};

export type MusicJob = {
  id: string;
  album_id: number;
  release_id: string;
  title: string;
  state: string;
  progress: number;
  total_bytes: number;
  downloaded_bytes: number;
  error: string;
  import_job_id?: number;
  created_at: number;
  updated_at: number;
};

export type MusicJobPage = {
  items: MusicJob[];
  page: number;
  total: number;
  limit: number;
};

export type MusicAlbumPreferences = {
  preferences: MusicPreferences;
  inherited: boolean;
  revision: number;
};

export type MusicUpgradeSuggestion = {
  album_id: number;
  artist_id: number;
  artist_name: string;
  title: string;
  year: number | null;
  formats: string[];
  bitrate: number;
  reason: string;
};

export type MusicUpgradeList = {
  items: MusicUpgradeSuggestion[];
};

export type MusicDownloadFile = {
  index: number;
  name: string;
  artist: string;
  album: string;
  title: string;
  track: number | null;
  disc: number | null;
};

export type MusicDownloadFileList = {
  items: MusicDownloadFile[];
};

export type AnimePreferences = {
  dub_rule: string;
  revision: number;
  default_rule: string;
};

export type AnimeLibrarySettings = {
  id: number;
  kind: "movie" | "series";
  category: "standard" | "anime";
  revision: number;
  numbering_revision: number;
  episodes: {
    episode_id: number;
    season: number;
    number: number;
    title: string;
    absolute_number: number | null;
  }[];
};

export type Movie = Omit<MediaItem, "id" | "kind" | "monitored" | "status">;
export type SearchResult = Movie & { in_library: boolean };
export type SearchResults = {
  items: SearchResult[];
  page: number;
  total_pages: number;
  total_results: number;
};
export type LibraryPage = {
  profile_context?: CatalogProfileContext;
  collection_total: number;
  items: MediaItem[];
  page: number;
  total: number;
  limit: number;
};
export type TVSeason = {
  available_episodes?: number;
  id: number;
  tmdb_id: number;
  number: number;
  title: string;
  overview: string;
  air_date: string;
  episode_count: number;
  monitored: boolean;
  loaded_at: string;
  known_episodes: number;
  selected_episodes: number;
};
export type TVEpisode = {
  available?: boolean;
  file_id?: number;
  file_path?: string;
  id: number;
  tmdb_id: number;
  number: number;
  title: string;
  overview: string;
  air_date: string;
  runtime_minutes: number | null;
  monitored: boolean;
  effective_monitored: boolean;
};
export type TVSeries = MediaItem & {
  series_status: string;
  revision: number;
  updated_at: string;
  seasons: TVSeason[];
};
export type TVSeasonDetails = {
  season: TVSeason;
  episodes: TVEpisode[];
  revision: number;
  series_monitored: boolean;
};
export type Session = {
  username: string;
  csrf_token: string;
  expires_at: number;
  role?: "admin" | "requester";
  account_id?: number;
};
export type AuthStatus = {
  setup_required: boolean;
  authenticated: boolean;
  session?: Session;
  /** True when the administrator must supply an authenticator code to sign in. */
  totp_enabled?: boolean;
};
export type ProviderStatus = {
  configured: boolean;
  updated_at: string;
  using_default?: boolean;
  default_available?: boolean;
};
export type IntegrationStatus = {
  configured: boolean;
  url: string;
  username?: string;
  download_path?: string;
};
export type Release = {
  protocol?: "torrent" | "usenet";
  indexer?: string;
  indexer_id?: string;
  freeleech?: boolean;
  pack?: boolean;
  /** Tracker page for this exact release; absent when the source has none. */
  info_url?: string | null;
  id: string;
  movie_id: number;
  title: string;
  size_bytes: number;
  seeders: number | null;
  peers: number | null;
  published_at?: string | null;
};
export type DownloadJob = {
  movie_version?: string;
  client_key?: string;
  client_kind?: "qbittorrent" | "sabnzbd" | "nzbget";
  client_label?: string;
  anime_search?: {
    mode: string;
    search_title: string;
    canonical_title: string;
    alternate_titles: string[];
    policy_revision: number;
    absolute_number?: number;
    queries: string[];
  };
  anime_order?: {
    media_id: number;
    revision: number;
    entries: {
      episode_id: number;
      provider_id: number;
      release_title: string;
      label: string;
    }[];
    identities: { id: number; title: string; url: string }[];
  };
  protocol?: "torrent" | "usenet";
  client_id?: string;
  kind?: "movie" | "series";
  targets?: {
    episode_id: number;
    tmdb_id: number;
    absolute_number?: number | null;
    season: number;
    number: number;
    title: string;
    skipped: boolean;
    imported: boolean;
  }[];
  id: string;
  movie_id: number;
  movie_title: string;
  title: string;
  info_hash: string;
  state: string;
  progress: number;
  total_bytes: number;
  downloaded_bytes: number;
  client_state: string;
  selected_file: number | null;
  choices: {
    index: number;
    name: string;
    size_bytes: number;
    suggestions?: number[];
  }[];
  attempts: number;
  error_code: string;
  error_message: string;
  created_at: number;
  updated_at: number;
};
export type DownloadPage = {
  items: DownloadJob[];
  page: number;
  total: number;
  limit: number;
};

/** One terminal release failure that automatic grabbing will skip. */
export type ReleaseBlacklistEntry = {
  id: number;
  protocol: "torrent" | "usenet";
  release_key: string;
  indexer: string;
  title: string;
  reason: string;
  /** Unix seconds. `expires_at` of 0 means the entry is permanent. */
  created_at: number;
  expires_at: number;
};

export type DuplicateFile = {
  id: number;
  relative_path: string;
  size_bytes: number;
  sha256: string;
  resolution: number;
};

/** A read-only duplicate review group. */
export type DuplicateGroup = {
  kind: "movie" | "episode";
  media_id: number;
  episode_id?: number;
  title: string;
  episode_title?: string;
  season?: number;
  episode?: number;
  file_count: number;
  status: "probable_duplicate" | "needs_attention";
  reason: string;
  files: DuplicateFile[];
};

export type DuplicateReport = {
  groups: DuplicateGroup[];
  total: number;
  limit: number;
  truncated: boolean;
};

export type TOTPStatus = {
  configured: boolean;
  enabled: boolean;
  recovery_codes_remaining: number;
};

/** Returned once by provisioning; never store the secret client-side. */
export type TOTPProvision = {
  secret: string;
  otpauth_uri: string;
};

let csrfToken = "";
export function setCSRFToken(value: string) {
  csrfToken = value;
}

export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
    public code = "",
  ) {
    super(message);
  }
}

export async function api<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    ...options,
    headers: {
      ...(options.body ? { "Content-Type": "application/json" } : {}),
      ...options.headers,
      ...(options.method && options.method !== "GET" && csrfToken
        ? { "X-CSRF-Token": csrfToken }
        : {}),
    },
    credentials: "same-origin",
    signal: options.signal ?? AbortSignal.timeout(30000),
  });
  if (response.status === 204) return undefined as T;
  let body: { error?: { message?: string; code?: string } } | null = null;
  try {
    body = await response.json();
  } catch {
    body = null;
  }
  if (!response.ok) {
    if (
      response.status === 401 &&
      path !== "/auth/login" &&
      path !== "/auth/setup"
    )
      window.dispatchEvent(new Event("reelscarey:unauthorized"));
    throw new APIError(
      body?.error?.message ??
        `The request could not be completed (HTTP ${response.status}).`,
      response.status,
      body?.error?.code ?? "",
    );
  }
  return body as T;
}

export function messageFor(error: unknown): string {
  if (error instanceof APIError) return error.message;
  return "ReelScarey could not be reached. Check that the server is running, then try again.";
}
