export type Origin = "job" | "manual" | "external";

export type Torrent = {
  hash: string;
  name: string;
  state: string;
  save_path: string;
  progress: number;
  amount_left: number;
  total_size: number;
  completed: number;
  tags: string;
  category: string;
  downloaded: number;
  uploaded: number;
  dlspeed: number;
  upspeed: number;
  eta: number;
  ratio: number;
  num_seeds: number;
  num_leechs: number;
  added_on: number;
  completion_on: number;
  content_path: string;
  priority: number;
  num_complete: number;
  num_incomplete: number;
  dl_limit: number;
  up_limit: number;
  seq_dl: boolean;
  f_l_piece_prio: boolean;
  force_start: boolean;
  super_seeding: boolean;
  auto_tmm: boolean;
  time_active: number;
  seeding_time: number;
  last_activity: number;
  availability: number;
  ratio_limit: number;
  seeding_time_limit: number;
  downloaded_session: number;
  uploaded_session: number;
  tracker: string;
  origin?: Origin;
  job_id?: string;
};

export type Transfer = {
  dl_info_speed: number;
  up_info_speed: number;
  dl_info_data: number;
  up_info_data: number;
  connection_status: string;
  dht_nodes: number;
  dl_rate_limit: number;
  up_rate_limit: number;
};

export type Server = {
  alt_speed_enabled: boolean;
  queueing_enabled: boolean;
  alt_dl_limit: number;
  alt_up_limit: number;
  max_active_downloads: number;
  max_active_uploads: number;
  max_active_torrents: number;
};

export type QueueLimits = {
  queueing_enabled: boolean;
  max_active_downloads: number;
  max_active_uploads: number;
  max_active_torrents: number;
  revision: number;
};

export type Overview = {
  available: boolean;
  state: string;
  message: string;
  transfer: Partial<Transfer>;
  server?: Server | null;
  queue?: QueueLimits;
  items: Torrent[];
};

export type ShareAction = "pause" | "remove" | "remove_and_delete";

/** Application-enforced seeding limit for the built-in client. */
export type SharePolicy = {
  enabled: boolean;
  action: ShareAction;
  max_ratio: number;
  max_seeding_minutes: number;
  include_reelscarey: boolean;
  revision: number;
  updated_at: number;
};

export type SharePolicyRun = { checked: number; acted: number };

/**
 * Auto-pause option for finished torrents from public sources in the built-in
 * client. Public sources are the public Nyaa indexer and manually uploaded
 * torrents; private-tracker downloads keep seeding.
 */
export type PublicPause = {
  enabled: boolean;
  revision: number;
  updated_at: number;
};

export const shareActionLabels: Record<ShareAction, string> = {
  pause: "Pause",
  remove: "Remove torrent",
  remove_and_delete: "Remove torrent and delete files",
};

/** Short status-bar summary such as "ratio 2.0 · 3 h · pause". */
export function sharePolicySummary(policy: SharePolicy) {
  if (!policy.enabled) return "off";
  const parts: string[] = [];
  if (policy.max_ratio > 0) parts.push(`ratio ${policy.max_ratio.toFixed(1)}`);
  if (policy.max_seeding_minutes > 0)
    parts.push(formatSeedingMinutes(policy.max_seeding_minutes));
  parts.push(shareActionLabels[policy.action].toLowerCase());
  return parts.join(" · ");
}

export function formatSeedingMinutes(minutes: number) {
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  if (hours > 0 && rest > 0) return `${hours} h ${rest} min`;
  if (hours > 0) return `${hours} h`;
  return `${rest} min`;
}

export type TorrentFile = {
  index: number;
  name: string;
  size: number;
  progress: number;
  priority: number;
  availability: number;
  is_seed?: boolean;
};

export type Properties = {
  save_path: string;
  creation_date: number;
  piece_size: number;
  comment: string;
  total_wasted: number;
  total_uploaded: number;
  total_downloaded: number;
  up_limit: number;
  dl_limit: number;
  time_elapsed: number;
  seeding_time: number;
  nb_connections: number;
  nb_connections_limit: number;
  share_ratio: number;
  addition_date: number;
  completion_date: number;
  created_by: string;
  dl_speed_avg: number;
  dl_speed: number;
  up_speed_avg: number;
  up_speed: number;
  eta: number;
  last_seen: number;
  peers: number;
  peers_total: number;
  seeds: number;
  seeds_total: number;
  pieces_have: number;
  pieces_num: number;
  reannounce: number;
  total_size: number;
};

export type Tracker = {
  url: string;
  status: number;
  tier: number;
  num_peers: number;
  num_seeds: number;
  num_leeches: number;
  num_downloaded: number;
  msg: string;
};

export type Peer = {
  ip: string;
  port: number;
  client: string;
  country_code: string;
  connection: string;
  flags: string;
  progress: number;
  dl_speed: number;
  up_speed: number;
  downloaded: number;
  uploaded: number;
  relevance: number;
};

export type Detail = {
  torrent: Torrent;
  files: TorrentFile[];
  properties: Properties;
  trackers: Tracker[];
  peers: Peer[];
  webseeds: { url: string }[];
  pieces: number[];
};

export type StatusFilter =
  | "all"
  | "downloading"
  | "seeding"
  | "completed"
  | "resumed"
  | "paused"
  | "active"
  | "inactive"
  | "stalled"
  | "stalled_uploading"
  | "stalled_downloading"
  | "checking"
  | "errored";

export const statusFilters: { key: StatusFilter; label: string }[] = [
  { key: "all", label: "All" },
  { key: "downloading", label: "Downloading" },
  { key: "seeding", label: "Seeding" },
  { key: "completed", label: "Completed" },
  { key: "resumed", label: "Resumed" },
  { key: "paused", label: "Paused" },
  { key: "active", label: "Active" },
  { key: "inactive", label: "Inactive" },
  { key: "stalled", label: "Stalled" },
  { key: "stalled_uploading", label: "Stalled Uploading" },
  { key: "stalled_downloading", label: "Stalled Downloading" },
  { key: "checking", label: "Checking" },
  { key: "errored", label: "Errored" },
];

export type SourceFilter = "all" | Origin;

export const sourceFilters: { key: SourceFilter; label: string }[] = [
  { key: "all", label: "All" },
  { key: "job", label: "ReelScarey downloads" },
  { key: "manual", label: "Manual" },
  { key: "external", label: "Other" },
];

const downloadingStates = new Set([
  "downloading",
  "metaDL",
  "forcedMetaDL",
  "forcedDL",
  "stalledDL",
  "queuedDL",
  "checkingDL",
  "pausedDL",
  "allocating",
]);
const seedingStates = new Set([
  "uploading",
  "forcedUP",
  "stalledUP",
  "queuedUP",
  "checkingUP",
]);
const completedStates = new Set([...seedingStates, "pausedUP"]);
const checkingStates = new Set([
  "checkingDL",
  "checkingUP",
  "checkingResumeData",
]);
const erroredStates = new Set(["error", "missingFiles", "unknown"]);

export function originOf(t: Torrent): Origin {
  return t.origin ?? "external";
}

export function isPaused(t: Torrent) {
  return t.state.startsWith("paused");
}

export function isActive(t: Torrent) {
  return t.dlspeed > 0 || t.upspeed > 0;
}

export function matchesStatus(t: Torrent, filter: StatusFilter) {
  switch (filter) {
    case "all":
      return true;
    case "downloading":
      return downloadingStates.has(t.state);
    case "seeding":
      return seedingStates.has(t.state);
    case "completed":
      return completedStates.has(t.state);
    case "resumed":
      return !isPaused(t);
    case "paused":
      return isPaused(t);
    case "active":
      return isActive(t);
    case "inactive":
      return !isActive(t);
    case "stalled":
      return t.state === "stalledUP" || t.state === "stalledDL";
    case "stalled_uploading":
      return t.state === "stalledUP";
    case "stalled_downloading":
      return t.state === "stalledDL";
    case "checking":
      return checkingStates.has(t.state);
    case "errored":
      return erroredStates.has(t.state);
  }
}

const stateLabels: Record<string, string> = {
  downloading: "Downloading",
  metaDL: "Downloading metadata",
  forcedMetaDL: "[F] Downloading metadata",
  forcedDL: "[F] Downloading",
  stalledDL: "Stalled",
  queuedDL: "Queued",
  checkingDL: "Checking",
  allocating: "Allocating",
  uploading: "Seeding",
  forcedUP: "[F] Seeding",
  stalledUP: "Seeding",
  queuedUP: "Queued",
  checkingUP: "Checking",
  pausedDL: "Paused",
  pausedUP: "Completed",
  checkingResumeData: "Checking resume data",
  moving: "Moving",
  missingFiles: "Missing Files",
  error: "Errored",
  unknown: "Unknown",
};

export function stateLabel(state: string) {
  return stateLabels[state] ?? state;
}

export type StateTone =
  | "downloading"
  | "seeding"
  | "completed"
  | "paused"
  | "stalled"
  | "queued"
  | "checking"
  | "error";

export function stateTone(state: string): StateTone {
  if (erroredStates.has(state)) return "error";
  if (state === "pausedUP") return "completed";
  if (state === "pausedDL") return "paused";
  if (state.startsWith("queued")) return "queued";
  if (checkingStates.has(state) || state === "moving") return "checking";
  if (state === "stalledDL") return "stalled";
  if (seedingStates.has(state)) return "seeding";
  return "downloading";
}

/** Tags ReelScarey assigns itself; they are shown under Source instead. */
export function isInternalTag(tag: string) {
  return tag === "reelscarey" || tag.startsWith("reelscarey-");
}

export function tagsOf(t: Torrent) {
  return t.tags
    .split(",")
    .map((tag) => tag.trim())
    .filter((tag) => tag && !isInternalTag(tag));
}

export function trackerHost(t: Torrent) {
  if (!t.tracker) return "";
  try {
    return new URL(t.tracker).host || t.tracker;
  } catch {
    return t.tracker;
  }
}

export type Filters = {
  status: StatusFilter;
  source: SourceFilter;
  /** null = all, "" = uncategorized */
  category: string | null;
  /** null = all, "" = untagged */
  tag: string | null;
  /** null = all, "" = trackerless */
  tracker: string | null;
};

export const defaultFilters: Filters = {
  status: "all",
  source: "all",
  category: null,
  tag: null,
  tracker: null,
};

export function matchesFilters(t: Torrent, f: Filters, terms: string[]) {
  if (!matchesStatus(t, f.status)) return false;
  if (f.source !== "all" && originOf(t) !== f.source) return false;
  if (f.category !== null && t.category !== f.category) return false;
  if (f.tag !== null) {
    const tags = tagsOf(t);
    if (f.tag === "" ? tags.length > 0 : !tags.includes(f.tag)) return false;
  }
  if (f.tracker !== null && trackerHost(t) !== f.tracker) return false;
  if (terms.length) {
    const name = t.name.toLowerCase();
    if (!terms.every((term) => name.includes(term))) return false;
  }
  return true;
}

export function searchTerms(query: string) {
  return query.trim().toLowerCase().split(/\s+/).filter(Boolean);
}

export function magnetFor(t: Pick<Torrent, "hash" | "name">) {
  return `magnet:?xt=urn:btih:${t.hash}&dn=${encodeURIComponent(t.name)}`;
}

export const infinityEta = 8640000;

export function formatRatio(value: number) {
  if (value < 0 || value >= 9999) return "∞";
  return value.toFixed(2);
}

export function formatPercent(value: number) {
  const percent = Math.max(0, Math.min(100, value * 100));
  if (percent >= 100) return "100%";
  return `${percent.toFixed(1)}%`;
}

export const KiB = 1024;

export function readStored<T>(key: string, fallback: T): T {
  try {
    const raw = window.localStorage.getItem(key);
    return raw === null ? fallback : (JSON.parse(raw) as T);
  } catch {
    return fallback;
  }
}

export function writeStored(key: string, value: unknown) {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Storage may be unavailable (private mode); preferences stay in memory.
  }
}
