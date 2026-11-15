import { useResource } from "./useResource";
import type { MusicStreamingQuality, QualityProfile } from "./api";

/** Profile keys used by the export menu on album and track file actions.
 * These must be builtin profile keys the backend can resolve
 * (music.ResolveProfileKey); Opus High is the builtin 128 kbps Opus target
 * and MP3 Compatible the builtin 192 kbps MP3 target. */
export const EXPORT_ORIGINAL_KEY = "original";
export const EXPORT_OPUS_128_KEY = "opus-high";
export const EXPORT_MP3_192_KEY = "mp3-compatible";

/** Explicit per-quality descriptions of what the backend actually delivers.
 * These mirror music.StreamingQualityParams: "auto" and "balanced" both
 * convert to Opus 96 kbps, data-saver to Opus 64, high to Opus 128, premium
 * to Opus 160, and original serves the registered file byte-for-byte. No
 * bandwidth adaptation is performed server-side, so the labels name the
 * fixed codec and rate instead of implying adaptive behaviour. */
export const STREAMING_QUALITY_OPTIONS: {
  value: MusicStreamingQuality;
  label: string;
  detail: string;
}[] = [
  {
    value: "auto",
    label: "Auto (Opus 96 kbps)",
    detail:
      "Opus 96 kbps, same as Balanced; no connection adaptation is applied.",
  },
  {
    value: "data-saver",
    label: "Data Saver (Opus 64 kbps)",
    detail: "Always Opus 64 kbps for the smallest stream.",
  },
  {
    value: "balanced",
    label: "Balanced (Opus 96 kbps)",
    detail: "Always Opus 96 kbps.",
  },
  {
    value: "high",
    label: "High (Opus 128 kbps)",
    detail: "Always Opus 128 kbps.",
  },
  {
    value: "premium",
    label: "Premium (Opus 160 kbps)",
    detail: "Always Opus 160 kbps.",
  },
  {
    value: "original",
    label: "Original (unconverted)",
    detail:
      "Serves the registered file byte-for-byte when the client can decode it.",
  },
];

/** Human label for a saved quality value, e.g. "Balanced (Opus 96 kbps)". */
export function streamingQualityLabel(value: string | undefined) {
  if (value === undefined || value === "") return "Default";
  return (
    STREAMING_QUALITY_OPTIONS.find((option) => option.value === value)?.label ??
    value
  );
}

/** Codec and bitrate summary, e.g. "Opus 128 kbps" or "FLAC lossless". */
export function profileSummary(profile: QualityProfile) {
  const codec = profile.codec.charAt(0).toUpperCase() + profile.codec.slice(1);
  if (profile.codec === "original") return "Original";
  return profile.bitrate_kbps > 0
    ? `${codec} ${profile.bitrate_kbps} kbps`
    : `${codec} lossless`;
}

/**
 * Selectable identifier for a profile. Builtin profiles use their stable
 * key; the backend also assigns every saved custom profile a stable
 * `custom-<id>` key, so every listed profile is selectable.
 */
export function profileKeyOf(profile: QualityProfile) {
  return profile.key || "";
}

/** Human label with codec and rate, used in profile selectors. */
export function profileLabel(profile: QualityProfile) {
  return `${profile.name} (${profileSummary(profile)})`;
}

/** Loads the builtin plus custom quality profiles for selectors. */
export function useMusicProfiles() {
  const resource = useResource<QualityProfile[]>("/library/music/profiles");
  return {
    profiles: Array.isArray(resource.data) ? resource.data : [],
    ...resource,
  };
}
