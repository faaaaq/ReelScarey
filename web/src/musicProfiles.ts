import { useResource } from "./useResource";
import type { MusicStreamingQuality, QualityProfile } from "./api";

/** Profile keys used by the export menu on album and track file actions.
 * These must be builtin profile keys the backend can resolve
 * (music.ResolveProfileKey); Opus High is the builtin 128 kbps Opus target
 * and MP3 Compatible the builtin 192 kbps MP3 target. */
export const EXPORT_ORIGINAL_KEY = "original";
export const EXPORT_OPUS_128_KEY = "opus-high";
export const EXPORT_MP3_192_KEY = "mp3-compatible";

export const STREAMING_QUALITY_OPTIONS: {
  value: MusicStreamingQuality;
  label: string;
}[] = [
  { value: "auto", label: "Auto" },
  { value: "data-saver", label: "Data Saver" },
  { value: "balanced", label: "Balanced" },
  { value: "high", label: "High" },
  { value: "premium", label: "Premium" },
  { value: "original", label: "Original" },
];

export function streamingQualityLabel(value: string | undefined) {
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
