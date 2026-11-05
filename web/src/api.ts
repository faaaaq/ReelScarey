export type Settings = {
  library_name: string;
  movie_folder: string;
  series_folder: string;
  episode_naming?: "standard" | "with-title";
  register_unverified_video: boolean;
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
};
export type ProviderStatus = { configured: boolean; updated_at: string };
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
  id: string;
  movie_id: number;
  title: string;
  size_bytes: number;
  seeders: number | null;
  peers: number | null;
  published_at: string;
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
let csrfToken = "";
export function setCSRFToken(value: string) {
  csrfToken = value;
}

export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
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
  const body = await response.json();
  if (!response.ok) {
    if (
      response.status === 401 &&
      path !== "/auth/login" &&
      path !== "/auth/setup"
    )
      window.dispatchEvent(new Event("reelscarey:unauthorized"));
    throw new APIError(
      body.error?.message ?? "The request could not be completed.",
      response.status,
    );
  }
  return body as T;
}

export function messageFor(error: unknown): string {
  if (error instanceof APIError) return error.message;
  return "ReelScarey could not be reached. Check that the server is running, then try again.";
}
