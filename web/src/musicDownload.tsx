import { useEffect, useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Download } from "lucide-react";
import { Alert, Modal } from "./ui";
import { api, messageFor, type MusicAcquisitionJob } from "./api";
import { useLiveResource } from "./useLiveResource";

export type AlbumDownloadTarget = {
  albumID?: number | null;
  providerID?: string;
  title: string;
  artist?: string;
};

export function AlbumDownloadModal({
  target,
  onClose,
  onStarted,
}: {
  target: AlbumDownloadTarget | null;
  onClose: () => void;
  onStarted?: () => void;
}) {
  const [format, setFormat] = useState("source");
  const [root, setRoot] = useState("Music");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [jobID, setJobID] = useState<number | null>(null);
  const job = useLiveResource<MusicAcquisitionJob>(
    jobID ? `/library/music/acquisitions/${jobID}` : null,
    2000,
  );

  useEffect(() => {
    if (!target) {
      setJobID(null);
      setError("");
      setPending(false);
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
      const result = await api<MusicAcquisitionJob>(path, {
        method: "POST",
        body: JSON.stringify({
          root: root.trim() || "Music",
          format,
          reviewed: true,
        }),
      });
      setJobID(result.id);
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
              Format{" "}
              <select
                aria-label="Album download format"
                value={format}
                onChange={(event) => setFormat(event.target.value)}
                disabled={Boolean(jobID)}
              >
                <option value="source">Best available source</option>
                <option value="mp3">MP3</option>
                <option value="flac">FLAC</option>
                <option value="m4a">M4A</option>
                <option value="opus">Opus</option>
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
            <Alert
              kind={
                job.data.state === "completed"
                  ? "success"
                  : job.data.state === "failed"
                    ? "danger"
                    : "info"
              }
              title={`Album acquisition #${job.data.id}`}
            >
              {job.data.state} · {job.data.completed_count}/
              {job.data.track_count} completed
              {job.data.error ? ` · ${job.data.error}` : ""}
            </Alert>
          )}
        </>
      )}
    </Modal>
  );
}
