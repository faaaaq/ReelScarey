import { useEffect, useState } from "react";
import { Button, DropdownMenu, TextField } from "@radix-ui/themes";
import { Download } from "lucide-react";
import { Alert, Label, Modal, ProgressBar } from "./ui";
import {
  api,
  exportFileURL,
  messageFor,
  type MusicAcquisitionJob,
  type MusicPreferences,
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
                <details>
                  <summary>
                    Candidate evidence ({track.candidate_evidence.length})
                  </summary>
                  <ul>
                    {track.candidate_evidence.map((item) => (
                      <li key={item.candidate.id}>
                        <strong>{item.candidate.title}</strong> —{" "}
                        {item.candidate.channel || "channel unknown"} — score{" "}
                        {item.score}
                        <ul>
                          {item.reasons.map((reason) => (
                            <li key={reason}>{reason}</li>
                          ))}
                        </ul>
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
                    onClick={() => {
                      setRetrying(track.id);
                      setRetryError("");
                      void onRetry(track.id, replacement[track.id] ?? "")
                        .catch((error) => setRetryError(messageFor(error)))
                        .finally(() => setRetrying(0));
                    }}
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
    }
  }, [target]);

  async function start() {
    if (!target || pending) return;
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
            disabled={pending || Boolean(jobID)}
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
          <div className="mus-search">
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
            <TextField.Root
              aria-label="Album destination root"
              value={root}
              onChange={(event) => setRoot(event.target.value)}
              disabled={Boolean(jobID)}
            />
          </div>
          <Alert kind="info" title="Automatic and reviewed">
            The worker searches YouTube for each missing track, requires a close
            duration match, and imports through the existing tagged no-overwrite
            pipeline. Existing files are never replaced. If a track cannot be
            matched, the job reports the issue instead of guessing.
          </Alert>
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
              />
            </>
          )}
        </>
      )}
    </Modal>
  );
}
