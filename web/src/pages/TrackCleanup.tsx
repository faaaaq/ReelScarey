import { useEffect, useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { Alert, FieldSet, Modal } from "../ui";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";

const actions = [
  ["all_subtitles", "All subtitles"],
  ["other_subtitles", "Subtitles outside configured languages"],
  ["other_non_forced", "Non-forced subtitles outside configured languages"],
  ["other_audio", "Audio outside the original and configured languages"],
] as const;

type CleanupPlan = {
  id: string;
  state: string;
  message: string;
  track_cleanup: {
    targets: { id: number; path: string }[];
    processed: number;
    changed: number;
    skipped: number;
    dropped: number;
    issues?: string[];
  };
};

export function TrackCleanup({
  mediaID,
  onDone,
}: {
  mediaID?: number;
  onDone?: () => void;
}) {
  const libraries = useResource<{ libraries: { key: string; name: string }[] }>(
    mediaID ? null : "/organization",
  );
  const [mode, setMode] = useState<string>(actions[0][0]);
  const [library, setLibrary] = useState("");
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [review, setReview] = useState<CleanupPlan>();
  const [id, setID] = useState("");
  const status = useLiveResource<CleanupPlan>(
    id ? `/organization/plans/${id}` : null,
    2000,
  );
  const progress = status.data?.track_cleanup;
  const ended =
    status.data?.state === "completed" ||
    status.data?.state === "failed" ||
    status.data?.state === "cancelled";
  const running = !!id && !ended;
  const [reported, setReported] = useState("");
  useEffect(() => {
    if (id && status.data?.state === "completed" && reported !== id) {
      setReported(id);
      onDone?.();
    }
  }, [id, status.data?.state, reported, onDone]);

  async function submit() {
    setBusy(true);
    setError("");
    try {
      if (review) {
        await api(`/organization/plans/${review.id}/apply`, {
          method: "POST",
          body: "{}",
        });
        setID(review.id);
        status.reload();
      } else {
        setReview(
          await api<CleanupPlan>("/subtitles/track-cleanup", {
            method: "POST",
            body: JSON.stringify({
              mode,
              library,
              ...(mediaID ? { media_id: mediaID } : {}),
            }),
          }),
        );
      }
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  function changeOpen(value: boolean) {
    if (busy) return;
    setOpen(value);
    if (!value && ended) {
      setID("");
      setReview(undefined);
      setError("");
    }
  }

  return (
    <FieldSet
      legend="Remove media tracks"
      description="Embedded tracks are removed by rewriting the video container. External subtitles move to the media trash."
    >
      <Modal
        open={open}
        onOpenChange={changeOpen}
        title="Remove media tracks"
        trigger={
          <Button variant="soft">
            {running ? "View cleanup progress" : "Open track cleanup"}
          </Button>
        }
        footer={
          <>
            <Button
              variant="soft"
              color="gray"
              onClick={() => changeOpen(false)}
            >
              {id ? "Close" : "Cancel"}
            </Button>
            {!running && status.data?.state !== "completed" && (
              <Button color="red" disabled={busy} onClick={() => void submit()}>
                {busy
                  ? "Working…"
                  : id
                    ? "Resume cleanup"
                    : review
                      ? "Remove tracks"
                      : "Review cleanup"}
              </Button>
            )}
          </>
        }
      >
        {review ? (
          <>
            <p>{actions.find(([value]) => value === mode)?.[1]}</p>
            <p>
              {review.track_cleanup.targets.length} registered videos in scope.
            </p>
            {!id && (
              <Button
                size="1"
                variant="soft"
                onClick={() => setReview(undefined)}
              >
                Change selection
              </Button>
            )}
          </>
        ) : (
          <div className="rs-stack">
            {!mediaID && (
              <label>
                Library{" "}
                <select
                  value={library}
                  onChange={(event) => setLibrary(event.target.value)}
                >
                  <option value="">All video libraries</option>
                  {libraries.data?.libraries.map((item) => (
                    <option value={item.key} key={item.key}>
                      {item.name}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <label>
              Remove{" "}
              <select
                value={mode}
                onChange={(event) => setMode(event.target.value)}
              >
                {actions.map(([value, label]) => (
                  <option value={value} key={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
          </div>
        )}
        {mode === "other_audio" && (
          <p>
            Videos with no saved original language or no audio track that can be
            safely kept are skipped.
          </p>
        )}
        {id && (
          <p role="status">
            {progress?.processed ?? 0} of{" "}
            {review?.track_cleanup.targets.length ?? 0} videos reviewed.{" "}
            {progress?.dropped ?? 0} tracks removed across{" "}
            {progress?.changed ?? 0} videos.{" "}
            {progress?.skipped ? `${progress.skipped} skipped.` : ""}
          </p>
        )}
        {!!progress?.issues?.length && (
          <details>
            <summary>Skipped videos</summary>
            <ul>
              {progress.issues.map((issue) => (
                <li key={issue}>{issue}</li>
              ))}
            </ul>
          </details>
        )}
        {(error ||
          status.error ||
          (libraries.error && !mediaID) ||
          status.data?.state === "failed") && (
          <Alert kind="danger" role="alert">
            {error || status.error || status.data?.message || libraries.error}
          </Alert>
        )}
      </Modal>
    </FieldSet>
  );
}
