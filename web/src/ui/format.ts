/** Validated positive integer page from a query parameter; defaults to 1. */
export function pageParam(value: string | null | undefined) {
  return value && /^[1-9][0-9]*$/.test(value) ? Number(value) : 1;
}

/** Binary byte size, e.g. "1.4 GiB". */
export function formatBytes(value: number | null | undefined, digits = 1) {
  if (!value || value < 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit++;
  }
  return `${size.toFixed(unit === 0 ? 0 : digits)} ${units[unit]}`;
}

/** Transfer rate; zero renders as an em dash unless `zero` is given. */
export function formatSpeed(value: number | null | undefined, zero = "—") {
  return value && value > 0 ? `${formatBytes(value)}/s` : zero;
}

/** Compact duration such as "2d 4h", "3h 12m", "45s". */
export function formatDuration(seconds: number | null | undefined) {
  if (seconds === null || seconds === undefined || seconds < 0) return "—";
  if (seconds >= 8640000) return "∞";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m ${s}s`;
  return `${s}s`;
}

/** Local date and time from a Unix timestamp in seconds. */
export function formatTimestamp(seconds: number | null | undefined) {
  if (!seconds || seconds <= 0) return "—";
  return new Date(seconds * 1000).toLocaleString();
}

/** "5 minutes ago" style relative time from a Unix timestamp in seconds. */
export function formatRelative(seconds: number | null | undefined) {
  if (!seconds || seconds <= 0) return "—";
  const delta = Math.round(seconds - Date.now() / 1000);
  const abs = Math.abs(delta);
  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  if (abs < 60) return rtf.format(delta, "second");
  if (abs < 3600) return rtf.format(Math.round(delta / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(delta / 3600), "hour");
  if (abs < 86400 * 30) return rtf.format(Math.round(delta / 86400), "day");
  return new Date(seconds * 1000).toLocaleDateString();
}

/** S01E02 style episode code. */
export function episodeCode(season: number, episode: number) {
  return `S${String(season).padStart(2, "0")}E${String(episode).padStart(2, "0")}`;
}

/** Release size; an unknown or non-positive size is labeled honestly. */
export function releaseBytes(size: number) {
  if (size <= 0) return "Size unknown";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const n = Math.min(Math.floor(Math.log(size) / Math.log(1024)), 4);
  return `${(size / 1024 ** n).toLocaleString(undefined, {
    maximumFractionDigits: n ? 1 : 0,
  })} ${units[n]}`;
}

/** Reads an uploaded file across browsers that lack File.arrayBuffer. */
export function readFile(file: File): Promise<ArrayBuffer> {
  if (typeof file.arrayBuffer === "function") return file.arrayBuffer();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as ArrayBuffer);
    reader.onerror = () => reject(reader.error);
    reader.readAsArrayBuffer(file);
  });
}
