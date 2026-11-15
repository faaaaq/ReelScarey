import type { LibraryPage, TVSeries, TVSeasonDetails } from "../../api";

/** A video or playlist entry returned by /youtube/inspect. */
export type Entry = {
  kind: "video" | "playlist";
  id: string;
  title: string;
  channel: string;
  url: string;
  published: string;
  duration_seconds?: number;
  thumbnail?: string;
};
export type Inspection = {
  source: { kind: string; id: string; url: string };
  title: string;
  channel: string;
  entries: Entry[];
  truncated: boolean;
};
export type Collection = { id: number; title: string; revision: number };
export type Target = {
  kind: "custom" | "movie" | "episode";
  collection_id?: number;
  media_id?: number;
  episode_id?: number;
};
export type Review = {
  id: string;
  video: { id: string; title: string; channel: string };
  target_label: string;
  expires_at: number;
  subtitles: { languages: string[]; automatic: boolean; quality?: string };
};
export type Job = {
  id: string;
  video: { id: string; title: string; channel: string };
  target_label: string;
  state: string;
  message: string;
  quality: string;
  progress: number;
  downloaded_bytes: number;
  total_bytes: number;
  speed_bps: number;
  eta_seconds: number;
  created_at: number;
  /** Captions actually downloaded for this job. */
  subtitle_languages?: string[];
  /** False means the required English captions are missing; import is blocked. */
  english_subtitles: boolean;
  /** Serialized download target; groups series jobs under their show. */
  target?: {
    kind: string;
    collection_id?: number;
    media_id?: number;
    episode_id?: number;
  };
  /** Owning per-show source, when the job came from one. */
  series_source_id?: string;
};
export type JobCounts = {
  queued: number;
  downloading: number;
  awaiting_review: number;
  failed: number;
  completed: number;
};
export type JobsResponse = { items: Job[]; counts?: JobCounts };
export type CustomFile = {
  job_id: string;
  title: string;
  collection: string;
  relative_path: string;
  size_bytes: number;
};
export type ImportReview = {
  job_id: string;
  size_bytes: number;
  sha256: string;
  probe_json: string;
  destination: string;
  target_label: string;
  subtitles: {
    language: string;
    size_bytes: number;
    sha256: string;
    destination: string;
  }[];
};
export type Candidate = {
  id: number;
  title: string;
  year?: number;
  tmdb_id: number;
  score: number;
};
export type SeasonSummary = {
  number: number;
  episode_count: number;
  loaded: boolean;
};
export type BatchEntry = {
  video_id: string;
  title: string;
  channel: string;
  duration_seconds: number;
  thumbnail: string;
  season_number: number;
  episode_number: number;
  episode_id?: number;
  episode_title?: string;
  status: "missing" | "available" | "active" | "unmatched";
  note: string;
  selected_default: boolean;
  duplicate: boolean;
};
export type BatchPreview = {
  source_url: string;
  title: string;
  channel: string;
  truncated: boolean;
  needs_show: boolean;
  show?: Candidate;
  candidates: Candidate[];
  season_number: number;
  seasons: SeasonSummary[];
  entries: BatchEntry[];
};
export type BatchResult = {
  video_id: string;
  episode_id: number;
  job_id?: string;
  state: string;
  message: string;
};
export type Quality = "best" | "1080" | "720";
export type Resource<T> = {
  data?: T;
  error: string;
  loading: boolean;
  reload: () => void;
};

/* Per-show YouTube source (existing /library/series/{id}/youtube-sources APIs). */

export type SourceEntry = {
  video_id: string;
  title: string;
  channel: string;
  position: number;
  state: string;
  episode_id?: number;
  job_id?: string;
  note: string;
};
export type SourceSubtitles = {
  languages: string[];
  automatic: boolean;
  quality?: string;
};
export type SeriesSource = {
  id: string;
  media_id: number;
  source_url: string;
  title: string;
  mapping: "episode" | "order" | "manual";
  season_number: number;
  start_episode: number;
  quality: string;
  subtitles: SourceSubtitles;
  auto_import: boolean;
  enabled: boolean;
  interval_hours: number;
  initialized: boolean;
  revision: number;
  requested: boolean;
  next_run: number;
  last_checked: number;
  state: string;
  message: string;
  created_at: number;
  entries?: SourceEntry[];
  matches?: Match[];
};
export type SourcePreview = {
  title: string;
  channel: string;
  truncated: boolean;
  entries: {
    video: { id: string; title: string; channel: string };
    position: number;
    season_number: number;
    episode_number: number;
    label: string;
    state: string;
    note: string;
  }[];
};
export type Match = {
  video_id: string;
  title: string;
  channel: string;
  position: number;
  episode_id: number;
  season_number: number;
  episode_number: number;
  episode_title: string;
};
export type MatchVideo = {
  video_id: string;
  title: string;
  channel: string;
  position: number;
  episode_id?: number;
  label: string;
  state: string;
  note: string;
};
export type MatchEpisode = {
  id: number;
  season_number: number;
  episode_number: number;
  title: string;
  label: string;
  monitored: boolean;
  files: number;
  active: number;
};
export type MatchPreview = {
  title: string;
  channel: string;
  truncated: boolean;
  videos: MatchVideo[];
  episodes: MatchEpisode[];
};
export type SourceDraft = {
  source_url: string;
  mapping: "episode" | "order" | "manual";
  season_number: number;
  start_episode: number;
  quality: Quality;
  auto_import: boolean;
  interval_hours: number;
};

/* Cross-show sources overview: GET /youtube/sources. */
export type SourceCounts = {
  entries: number;
  queued: number;
  downloading: number;
  awaiting_review: number;
  completed: number;
  downloaded: number;
  missing: number;
};
export type SourcesOverviewItem = {
  id: string;
  media_id: number;
  show_title: string;
  source_url: string;
  mapping: string;
  season_number: number;
  quality: string;
  auto_import: boolean;
  enabled: boolean;
  initialized: boolean;
  revision: number;
  state: string;
  message: string;
  next_run: number;
  last_checked: number;
  counts: SourceCounts;
  show: { id: number; title: string };
  /** Serialized when present so Enable/Pause round-trips faithfully. */
  start_episode?: number;
  interval_hours?: number;
  subtitles?: SourceSubtitles;
};
export type SourcesOverviewResponse = { items: SourcesOverviewItem[] };

/* Per-source episode view: GET /youtube/sources/{id}/episodes. */
export type EpisodeState =
  | "missing"
  | "queued"
  | "downloading"
  | "awaiting_review"
  | "importing"
  | "downloaded"
  | "failed"
  | "skipped";
export type SourceEpisode = {
  episode_id: number;
  season_number: number;
  episode_number: number;
  title: string;
  monitored: boolean;
  file_count: number;
  state: EpisodeState;
  note: string;
  /** true/false from verified subtitle evidence; null when not checked yet. */
  english_subtitles: boolean | null;
  videos: {
    video_id: string;
    title: string;
    position: number;
    state: string;
    job_id?: string;
  }[];
};
export type SourceEpisodesResponse = { items: SourceEpisode[] };
export type EpisodeQueueResult = {
  items: {
    video_id: string;
    episode_id: number;
    job_id?: string;
    state: string;
    message: string;
  }[];
};

/** Kept for the extracted TargetPicker's prop types. */
export type TargetPickerResources = {
  library: LibraryPage;
  show: TVSeries;
  episodes: TVSeasonDetails;
};
