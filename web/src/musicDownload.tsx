import { useEffect, useState } from "react";
import { Button, DropdownMenu, TextField } from "@radix-ui/themes";
import { Download } from "lucide-react";
import { Link } from "react-router-dom";
import { Alert, Label, Modal, ProgressBar } from "./ui";
import {
  api,
  exportFileURL,
  messageFor,
  type MusicAcquisitionJob,
  type MusicDownloadSource,
  type MusicPreferences,
  type TidalStatus,
} from "./api";
import { useLiveResource } from "./useLiveResource";
import { useResource } from "./useResource";
import {
  EXPORT_MP3_192_KEY,
  EXPORT_OPUS_128_KEY,
  EXPORT_ORIGINAL_KEY,
  profileLabel,
  useMusicProfiles,
} from "./musicProfiles";

export type AlbumDownloadTarget = {
  albumID?: number | null;
  providerID?: string;
  title: string;
  artist?: string;
};

/**
 * Export submenu for a registered music file: the original bytes or a
 * converted copy served by the backend (session cookies carry the auth, so a
 * plain link works like every other same-origin download).
 */
export function MusicExportMenu({
  fileID,
  title,
}: {
  fileID: number;
  title: string;
}) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        <Button
          size="1"
          variant="ghost"
          color="gray"
          aria-label={`Export ${title}`}
        >
          <Download size={14} aria-hidden="true" />
        </Button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Content>
        <DropdownMenu.Item asChild>
          <a href={exportFileURL(fileID, EXPORT_ORIGINAL_KEY)} download>
            Original
          </a>
        </DropdownMenu.Item>
        <DropdownMenu.Item asChild>
          <a href={exportFileURL(fileID, EXPORT_OPUS_128_KEY)} download>
            Opus 128
          </a>
        </DropdownMenu.Item>
        <DropdownMenu.Item asChild>
          <a href={exportFileURL(fileID, EXPORT_MP3_192_KEY)} download>
            MP3 192
          </a>
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  );
}

function labelKind(state: string): "info" | "success" | "danger" {
  if (state === "completed") return "success";
  if (state === "failed") return "danger";
  return "info";
}

function barKind(state: string): "primary" | "success" | "danger" {
  if (state === "completed") return "success";
  if (state === "failed") return "danger";
  return "primary";
}

function trackVerb(state: string) {
  if (state === "importing") return "Importing";
  if (state === "downloading") return "Downloading";
  return "Waiting on";
}

/** One-line live status for a music acquisition job. */
export function acquisitionStatus(job: MusicAcquisitionJob): string {
  const total = job.track_count || 0;
  const done = job.completed_count || 0;
  if (job.state === "completed") {
    return `${done} of ${total} ${total === 1 ? "track" : "tracks"} completed.`;
  }
  if (job.state === "failed") {
    return job.error || "No tracks completed.";
  }
  const tracks = job.tracks ?? [];
  const current =
    tracks.find((track) => track.state === "downloading") ??
    tracks.find((track) => track.state === "importing") ??
    tracks.find((track) => track.state === "queued");
  if (!current) {
    return `${done} of ${total} ${total === 1 ? "track" : "tracks"} completed.`;
  }
  const index = tracks.indexOf(current) + 1;
  const name = current.title || `track ${index}`;
  return `${trackVerb(current.state)} ${index} of ${total} · ${name}`;
}

function audioCandidateHref(id: string): string {
  if (/^[A-Za-z0-9_-]{11}$/.test(id)) {
    return `https://www.youtube.com/watch?v=${id}`;
  }
  return id.startsWith("https://") ? id : "";
}

export async function retryMusicAcquisitionTrack(
  jobID: number,
  trackID: number,
  sourceURL: string,
): Promise<MusicAcquisitionJob> {
  return api<MusicAcquisitionJob>(
    `/library/music/acquisitions/${jobID}/tracks/${trackID}/retry`,
    {
      method: "POST",
      body: JSON.stringify({ source_url: sourceURL, reviewed: true }),
    },
  );
}

/**
 * Live progress for a queued music acquisition. Shared by the download
 * confirmation popup and the artist, album and library pages so a download can
 * be followed without leaving the page.
 */
export function AcquisitionProgress({
  job,
  label = "Music download progress",
  onRetry,
}: {
  job: MusicAcquisitionJob;
  label?: string;
  onRetry?: (trackID: number, sourceURL: string) => Promise<void>;
}) {
  const [replacement, setReplacement] = useState<Record<number, string>>({});
  const [retrying, setRetrying] = useState(0);
  const [retryError, setRetryError] = useState("");
  const total = job.track_count || 0;
  const done = job.completed_count || 0;
  const fraction = total > 0 ? Math.min(1, done / total) : 0;
  const tracks = job.tracks ?? [];
  const settled = job.state === "completed" || job.state === "failed";

  async function retry(trackID: number, sourceURL: string) {
    if (!onRetry || retrying > 0) return;
    setRetrying(trackID);
    setRetryError("");
    try {
      await onRetry(trackID, sourceURL);
    } catch (error) {
      setRetryError(messageFor(error));
    } finally {
      setRetrying(0);
    }
  }

  return (
    <div className="mus-progress">
      <div className="mus-progress-head">
        <Label kind={labelKind(job.state)}>{job.state}</Label>
        <span className="rs-muted rs-small">
          {done}/{total} {total === 1 ? "track" : "tracks"} completed
        </span>
      </div>
      <ProgressBar
        label={label}
        value={fraction}
        kind={barKind(job.state)}
        text={total > 0 ? `${Math.round(fraction * 100)}%` : undefined}
        indeterminate={total === 0 && !settled}
      />
      <p className="mus-progress-status" role="status" aria-live="polite">
        {acquisitionStatus(job)}
      </p>
      {job.error && job.state !== "failed" && (
        <p className="rs-muted rs-small">{job.error}</p>
      )}
      <p className="rs-muted rs-small">
        Requested audio source:{" "}
        {job.audio_source === "tidal"
          ? "TIDAL only"
          : job.audio_source === "youtube"
            ? "YouTube only"
            : "Automatic (TIDAL, then YouTube)"}
      </p>
      {job.source_url && (
        <p className="rs-muted rs-small">
          Catalog source:{" "}
          <a href={job.source_url} target="_blank" rel="noreferrer">
            {job.source_url}
          </a>
        </p>
      )}
      {tracks.length > 0 && (
        <ul className="mus-progress-failed">
          {tracks.map((track) => (
            <li key={track.id}>
              <span>{track.title || `Track ${track.position}`}</span>
              {track.error ? (
                <span className="rs-muted"> · {track.error}</span>
              ) : null}
              {track.selected_source_url && (
                <div className="rs-small">
                  Actual audio source:{" "}
                  <a
                    href={track.selected_source_url}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {track.selected_source_url}
                  </a>
                </div>
              )}
              {!!track.candidate_evidence?.length && (
                <details
                  open={
                    track.state === "failed" &&
                    track.candidate_evidence.some((item) => item.eligible)
                  }
                >
                  <summary>
                    Review candidates (
                    {
                      track.candidate_evidence.filter((item) => item.eligible)
                        .length
                    }{" "}
                    eligible of {track.candidate_evidence.length})
                  </summary>
                  {track.state === "failed" && onRetry && (
                    <p className="rs-muted rs-small">
                      Automatic matching could not choose safely. Review an
                      eligible candidate and select Download this candidate, or
                      provide a different source below.
                    </p>
                  )}
                  <ul>
                    {track.candidate_evidence.map((item) => (
                      <li key={item.candidate.id}>
                        <strong>{item.candidate.title}</strong> —{" "}
                        {item.candidate.channel || "channel unknown"} — score{" "}
                        {item.score}
                        {audioCandidateHref(item.candidate.id) && (
                          <>
                            {" "}
                            <a
                              href={audioCandidateHref(item.candidate.id)}
                              target="_blank"
                              rel="noreferrer"
                            >
                              Open candidate
                            </a>
                          </>
                        )}
                        <ul>
                          {item.reasons.map((reason) => (
                            <li key={reason}>{reason}</li>
                          ))}
                        </ul>
                        {track.state === "failed" &&
                          item.eligible &&
                          onRetry && (
                            <Button
                              size="1"
                              variant="soft"
                              className="mus-candidate-download"
                              disabled={retrying > 0}
                              aria-label={`Download candidate ${item.candidate.title} for ${track.title}`}
                              onClick={() =>
                                void retry(track.id, item.candidate.id)
                              }
                            >
                              {retrying === track.id
                                ? "Starting…"
                                : "Download this candidate"}
                            </Button>
                          )}
                      </li>
                    ))}
                  </ul>
                </details>
              )}
              {!!track.attempts?.length && (
                <details>
                  <summary>Source attempts ({track.attempts.length})</summary>
                  <ol>
                    {track.attempts.map((attempt) => (
                      <li key={attempt.id}>
                        {attempt.source_url || attempt.source_id}:{" "}
                        {attempt.outcome}
                      </li>
                    ))}
                  </ol>
                </details>
              )}
              {track.state === "failed" && onRetry && (
                <div className="rs-form-actions">
                  <TextField.Root
                    aria-label={`Replacement source for ${track.title}`}
                    placeholder="Optional replacement YouTube/source URL or ID"
                    value={replacement[track.id] ?? ""}
                    onChange={(event) =>
                      setReplacement((current) => ({
                        ...current,
                        [track.id]: event.target.value,
                      }))
                    }
                  />
                  <Button
                    size="1"
                    disabled={retrying > 0}
                    onClick={() =>
                      void retry(track.id, replacement[track.id] ?? "")
                    }
                  >
                    {retrying === track.id
                      ? "Retrying…"
                      : "Retry only this track"}
                  </Button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      {retryError && <Alert kind="danger">{retryError}</Alert>}
    </div>
  );
}

export function AlbumDownloadModal({
  target,
  onClose,
  onStarted,
  onJob,
}: {
  target: AlbumDownloadTarget | null;
  onClose: () => void;
  onStarted?: () => void;
  /** Receives the created job so the owning page can keep tracking it. */
  onJob?: (job: MusicAcquisitionJob) => void;
}) {
  const [root, setRoot] = useState("Music");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [jobID, setJobID] = useState<number | null>(null);
  const job = useLiveResource<MusicAcquisitionJob>(
    jobID ? `/library/music/acquisitions/${jobID}` : null,
    2000,
  );
  const profiles = useMusicProfiles();
  const prefs = useResource<MusicPreferences>("/library/music/preferences");
  const tidal = useResource<TidalStatus>(
    target ? "/library/music/tidal/status" : null,
    {
      retain: true,
      timeoutMs: 120000,
    },
  );
  /** Null inherits the saved default until this dialog explicitly overrides it. */
  const [sourceOverride, setSourceOverride] =
    useState<MusicDownloadSource | null>(null);
  const audioSource = sourceOverride ?? prefs.data?.download_source ?? "";
  /** Empty until the user overrides the global default for this download. */
  const [profileKey, setProfileKey] = useState("");
  const defaultProfile = profiles.profiles.find(
    (profile) => profile.key === (prefs.data?.download_profile ?? "original"),
  );

  useEffect(() => {
    if (!target) {
      setJobID(null);
      setError("");
      setPending(false);
      setProfileKey("");
      setSourceOverride(null);
    }
  }, [target]);

  const tidalVerified = Boolean(
    !tidal.error &&
    tidal.data?.available &&
    tidal.data.logged_in &&
    tidal.data.verification_state === "verified",
  );
  const tidalChoiceBlocked = audioSource === "tidal" && !tidalVerified;

  async function start() {
    if (!target || pending || tidalChoiceBlocked) return;
    setPending(true);
    setError("");
    try {
      const path = target.albumID
        ? `/library/music/albums/${target.albumID}/acquire`
        : `/library/music/provider-albums/${target.providerID}/acquire`;
      // No explicit choice means the backend resolves the global download
      // profile; the acquisition format field also accepts profile keys.
      const result = await api<MusicAcquisitionJob>(path, {
        method: "POST",
        body: JSON.stringify({
          root: root.trim() || "Music",
          format: profileKey,
          audio_source:
            sourceOverride === null ? audioSource : audioSource || "automatic",
          reviewed: true,
        }),
      });
      setJobID(result.id);
      onJob?.(result);
      onStarted?.();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function retryTrack(trackID: number, sourceURL: string) {
    if (!jobID)
      throw new Error(
        "The acquisition is no longer available. Reload and try again.",
      );
    const updated = await retryMusicAcquisitionTrack(jobID, trackID, sourceURL);
    onJob?.(updated);
    job.reload();
  }

  return (
    <Modal
      open={Boolean(target)}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title="Download album"
      description="Queue every missing track for automatic matching, audio download and import."
      footer={
        <>
          <Button variant="soft" color="gray" onClick={onClose}>
            Close
          </Button>
          <Button
            disabled={pending || Boolean(jobID) || tidalChoiceBlocked}
            onClick={() => void start()}
          >
            <Download size={15} aria-hidden="true" />
            {pending ? "Starting…" : "Download album"}
          </Button>
        </>
      }
    >
      {target && (
        <>
          <p>
            <strong>{target.title}</strong>
            {target.artist ? ` · ${target.artist}` : ""}
          </p>
          <div className="mus-download-options">
            <label>
              Audio source{" "}
              <select
                aria-label="Album audio source"
                value={audioSource}
                onChange={(event) =>
                  setSourceOverride(event.target.value as MusicDownloadSource)
                }
                disabled={Boolean(jobID)}
              >
                <option value="">Automatic (TIDAL, then YouTube)</option>
                <option
                  value="tidal"
                  disabled={tidal.data?.available === false}
                >
                  TIDAL only
                </option>
                <option value="youtube">YouTube only</option>
              </select>
            </label>
            <label>
              Profile{" "}
              <select
                aria-label="Album download profile"
                value={profileKey}
                onChange={(event) => setProfileKey(event.target.value)}
                disabled={Boolean(jobID)}
              >
                <option value="">
                  {defaultProfile
                    ? `${defaultProfile.name} (default)`
                    : "Default profile"}
                </option>
                <option value="source">Best available source</option>
                {profiles.profiles
                  .filter(
                    (profile) =>
                      profile.key !== "" && profile !== defaultProfile,
                  )
                  .map((profile) => (
                    <option key={profile.key} value={profile.key}>
                      {profileLabel(profile)}
                    </option>
                  ))}
              </select>
            </label>
            <label>
              Destination root{" "}
              <TextField.Root
                aria-label="Album destination root"
                value={root}
                onChange={(event) => setRoot(event.target.value)}
                disabled={Boolean(jobID)}
              />
            </label>
          </div>
          <Alert kind="info" title="Source and import behavior">
            {audioSource === "tidal"
              ? "Only TIDAL will be searched for each missing track. TIDAL failures are reported and never silently rerouted to YouTube. "
              : audioSource === "youtube"
                ? "Only YouTube will be searched for each missing track. TIDAL is not used, even when it is connected. "
                : "Automatic tries TIDAL first when installed and uses YouTube if TIDAL cannot supply a match. "}
            Matching requires a compatible title and close duration. Import uses
            the existing tagged no-overwrite pipeline; existing files are never
            replaced. If a track cannot be matched, the job reports the issue
            instead of guessing.
          </Alert>
          {tidalChoiceBlocked && (
            <Alert
              kind="info"
              title="TIDAL-only download requires verification"
            >
              {tidal.error
                ? `The current TIDAL check failed: ${tidal.error}`
                : tidal.data?.detail ||
                  "TIDAL has not reported a verified account session. Connect and verify TIDAL before starting a TIDAL-only download."}{" "}
              <Link to="/music/settings" onClick={onClose}>
                Open Music settings
              </Link>
            </Alert>
          )}
          {error && <Alert kind="danger">{error}</Alert>}
          {job.data && (
            <>
              <Alert kind="success" title="Matching queued">
                {job.data.track_count}{" "}
                {job.data.track_count === 1 ? "track" : "tracks"} queued for
                matching and import. Progress updates below and stays on the
                page after you close this dialog.
              </Alert>
              <AcquisitionProgress
                job={job.data}
                label="Album download progress"
                onRetry={retryTrack}
              />
            </>
          )}
        </>
      )}
    </Modal>
  );
}
