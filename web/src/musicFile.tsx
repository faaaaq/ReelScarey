import type { MusicTrack } from "./api";
import { formatBytes } from "./ui/format";

export type MusicTrackFile = Pick<
  MusicTrack,
  "file_id" | "bitrate" | "size_bytes" | "format" | "relative_path"
>;

/** Stored probes use bps for larger values and kbps for legacy small values. */
export function formatTrackBitrate(bitrate: number | null | undefined) {
  if (!bitrate || bitrate < 0) return "—";
  const kbps = bitrate > 320 ? Math.round(bitrate / 1000) : Math.round(bitrate);
  return kbps > 0 ? `${kbps} kbps` : "—";
}

export function musicFilename(relativePath: string | null | undefined) {
  if (!relativePath) return "";
  return relativePath.split(/[\\/]/).filter(Boolean).pop() ?? "";
}

export function formatMusicFileType(
  format: string | null | undefined,
  relativePath?: string | null,
) {
  let type = format?.trim() ?? "";
  if (!type) {
    const filename = musicFilename(relativePath);
    const dot = filename.lastIndexOf(".");
    type = dot > 0 ? filename.slice(dot + 1) : "";
  }
  return type ? type.replace(/^\./, "").toUpperCase() : "—";
}

export function MusicFilenameToggle({
  checked,
  onChange,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <div className="mus-track-options">
      <label>
        <input
          type="checkbox"
          checked={checked}
          onChange={(event) => onChange(event.target.checked)}
        />
        Show filenames
      </label>
    </div>
  );
}

export function MusicFileColumnHeadings({
  showFilename,
}: {
  showFilename: boolean;
}) {
  return (
    <>
      <th scope="col">Bitrate</th>
      <th scope="col">File size</th>
      <th scope="col">File type</th>
      {showFilename && <th scope="col">Filename</th>}
    </>
  );
}

export function MusicFileCells({
  track,
  showFilename,
}: {
  track: MusicTrackFile;
  showFilename: boolean;
}) {
  const filename = musicFilename(track.relative_path);
  return (
    <>
      <td>{formatTrackBitrate(track.bitrate)}</td>
      <td>{track.size_bytes ? formatBytes(track.size_bytes) : "—"}</td>
      <td>{formatMusicFileType(track.format, track.relative_path)}</td>
      {showFilename && (
        <td className="mus-track-filename" title={filename || undefined}>
          {filename || "—"}
        </td>
      )}
    </>
  );
}
