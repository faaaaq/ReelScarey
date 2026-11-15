import type { BatchEntry, EpisodeState } from "./types";

/** Single source of truth for YouTube status labels and colors. */
export function duration(seconds?: number) {
  if (!seconds) return "";
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (h)
    return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  return `${m}:${String(s).padStart(2, "0")}`;
}
export function bytes(value: number) {
  if (!value) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB"];
  let v = value;
  let unit = 0;
  while (v >= 1024 && unit < units.length - 1) {
    v /= 1024;
    unit++;
  }
  return `${v.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}
export function speed(value: number) {
  return value > 0 ? `${bytes(value)}/s` : "";
}
export function eta(seconds: number) {
  if (!seconds || seconds >= 8640000) return "";
  if (seconds >= 3600)
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
  if (seconds >= 60) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  return `${seconds}s`;
}
export function qualityLabel(quality: string) {
  return quality === "1080" ? "1080p" : quality === "720" ? "720p" : "Best";
}
export function stateLabel(state: string) {
  const labels: Record<string, string> = {
    queued: "Queued",
    downloading: "Downloading",
    awaiting_review: "Review needed",
    importing: "Importing",
    completed: "Imported",
    failed: "Failed",
    cancelled: "Cancelled",
  };
  return labels[state] ?? state;
}
export function stateColor(
  state: string,
): "gray" | "green" | "amber" | "red" | "blue" {
  if (state === "completed") return "green";
  if (state === "awaiting_review" || state === "importing") return "blue";
  if (state === "failed" || state === "cancelled") return "red";
  if (state === "downloading") return "amber";
  return "gray";
}
/** Episode state labels for the per-source episode table. */
export function episodeStateLabel(state: EpisodeState | string) {
  const labels: Record<string, string> = {
    missing: "Missing",
    queued: "Queued",
    downloading: "Downloading",
    awaiting_review: "Review needed",
    importing: "Importing",
    downloaded: "Downloaded",
    failed: "Failed",
    skipped: "Skipped",
  };
  return labels[state] ?? state;
}
export function episodeStateColor(
  state: EpisodeState | string,
): "gray" | "green" | "amber" | "red" | "blue" {
  if (state === "downloaded") return "green";
  if (state === "awaiting_review" || state === "importing") return "blue";
  if (state === "failed") return "red";
  if (state === "downloading" || state === "queued") return "amber";
  return "gray";
}
/** English subtitle evidence: null means the episode was not checked yet. */
export function englishSubsBadge(value: boolean | null | undefined): {
  label: string;
  color: "gray" | "green" | "red";
} {
  if (value === true) return { label: "English subs", color: "green" };
  if (value === false) return { label: "English subs missing", color: "red" };
  return { label: "English subs not checked", color: "gray" };
}
export function thumb(videoID: string) {
  return `https://i.ytimg.com/vi/${videoID}/mqdefault.jpg`;
}
export function episodeLabel(entry: BatchEntry) {
  if (!entry.episode_number) return "—";
  return `S${String(entry.season_number).padStart(2, "0")}E${String(
    entry.episode_number,
  ).padStart(2, "0")}${entry.episode_title ? ` — ${entry.episode_title}` : ""}`;
}
/** Human label for a per-show source mapping, shared by cards and overview. */
export function mappingLabel(mapping: string, seasonNumber: number): string {
  if (mapping === "order") return `Playlist order · Season ${seasonNumber}`;
  if (mapping === "manual") return "Manual video matching";
  return `Episode number${seasonNumber ? ` · Season ${seasonNumber}` : ""}`;
}
/** Source state badge: paused vs. active vs. needing attention. */
export function sourceState(source: {
  enabled: boolean;
  state: string;
  message?: string;
}): { label: string; color: "gray" | "green" | "amber" } {
  if (!source.enabled) return { label: "Paused", color: "gray" };
  if (
    source.state === "attention" ||
    source.state === "failed" ||
    source.state === "error"
  )
    return { label: "Attention", color: "amber" };
  return { label: "Active", color: "green" };
}
