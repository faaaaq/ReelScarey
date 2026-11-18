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

type ActivePlan = {
  id: string;
  state: string;
  kind: string;
  message?: string;
};

/** One media operation runs at a time; this is the one the worker is on. */
type ActiveResponse = { active: boolean; plan?: ActivePlan };

export function TrackCleanup({
  mediaID,
  onDone,
  kind = "all",
}: {
  mediaID?: number;
  onDone?: () => void;
  kind?: "all" | "subtitles" | "audio";
}) {
  const available = actions.filter(
    ([value]) =>
      kind === "all" || (kind === "audio") === (value === "other_audio"),
  );
  const title =
    kind === "subtitles"
      ? "Delete subtitles"
      : kind === "audio"
        ? "Delete audio tracks"
        : "Remove media tracks";
  const libraries = useResource<{ libraries: { key: string; name: string }[] }>(
    mediaID ? null : "/organization",
  );
  const [mode, setMode] = useState<string>(available[0][0]);
  const [library, setLibrary] = useState("");
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [review, setReview] = useState<CleanupPlan>();
  const [id, setID] = useState("");
  // The worker runs every plan through one shared queue. Watching the active
  // plan keeps this control honest about work started elsewhere — including a
  // cleanup started on another page or before a reload.
  const active = useLiveResource<ActiveResponse>("/organization/active", 3000);
  const adopted =
    !id &&
    !review &&
    active.data?.active &&
    active.data.plan?.kind === "track_cleanup" &&
    (active.data.plan.state === "queued" ||
      active.data.plan.state === "running");
  const [reported, setReported] = useState("");
  const status = useLiveResource<CleanupPlan>(
    id ? `/organization/plans/${id}` : null,
    2000,
  );
  const progress = status.data?.track_cleanup;
  const total =
    status.data?.track_cleanup.targets.length ??
    review?.track_cleanup.targets.length ??
    0;
  const ended =
    status.data?.state === "completed" ||
    status.data?.state === "failed" ||
    status.data?.state === "cancelled";
  const running = !!id && !ended;

  useEffect(() => {
    if (adopted && active.data?.plan) setID(active.data.plan.id);
  }, [adopted, active.data?.plan]);

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
        // Applying never runs the cleanup inside the request; it validates and
        // queues, then the worker reports progress through the plan poll.
        await api(`/organization/plans/${review.id}/apply`, {
          method: "POST",
          body: "{}",
          signal: AbortSignal.timeout(300000),
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
            signal: AbortSignal.timeout(120000),
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
    if (!value && (ended || !id)) {
      if (!running) {
        setID("");
        setReview(undefined);
      }
      setError("");
    }
  }

  const busyElsewhere =
    active.data?.active &&
    active.data.plan &&
    active.data.plan.kind !== "track_cleanup" &&
    !id;
  return (
    <FieldSet
      legend={title}
      description="Embedded tracks are removed by rewriting the video container. Selected external subtitles are permanently deleted."
    >
      <Modal
        open={open}
        onOpenChange={changeOpen}
        title={title}
        trigger={
          <Button variant="soft">
            {running || adopted
              ? "View cleanup progress"
              : kind === "all"
                ? "Open track cleanup"
                : title}
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
            {!running &&
              !adopted &&
              status.data?.state !== "completed" &&
              !busyElsewhere && (
                <Button
                  color="red"
                  disabled={busy}
                  onClick={() => void submit()}
                >
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
                {available.map(([value, label]) => (
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
        {busyElsewhere && (
          <p role="status">
            Another media operation is running. This review can start once it
            finishes; its progress is shown on any page.
          </p>
        )}
        {id && (
          <p role="status">
            {progress?.processed ?? 0} of {total} videos reviewed.{" "}
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
