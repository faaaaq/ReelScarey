import {
  CleanupSkipDetails,
  type CleanupSkipReport,
} from "../components/CleanupSkipDetails";
import { BatchProgress, type WorkProgress } from "../components/BatchProgress";
import { useEffect, useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor, APIError } from "../api";
import { Alert, FieldSet, Modal } from "../ui";
import {
  activePlanDetail,
  activePlanLabel,
  activePlanStoppable,
  type ActivePlan,
} from "../components/MediaWorkBanner";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";

const modes = [
  ["all_subtitles", "All subtitles"],
  ["other_subtitles", "Subtitles outside configured languages"],
  ["other_non_forced", "Non-forced subtitles outside configured languages"],
  ["other_audio", "Audio outside the original and configured languages"],
] as const;

type CleanupPlan = {
  progress?: WorkProgress;
  id: string;
  state: string;
  message: string;
  track_cleanup: CleanupSkipReport & {
    request?: { action: string };
    targets: { id: number; path: string }[];
    processed: number;
    changed: number;
    skipped: number;
    dropped: number;
    disabled: number;
    issues?: string[];
  };
};

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
  const available = modes.filter(
    ([value]) =>
      kind === "all" || (kind === "audio") === (value === "other_audio"),
  );
  const title =
    kind === "subtitles"
      ? "Subtitle tracks"
      : kind === "audio"
        ? "Audio tracks"
        : "Media tracks";
  const libraries = useResource<{ libraries: { key: string; name: string }[] }>(
    mediaID ? null : "/organization",
  );
  const [mode, setMode] = useState<string>(available[0][0]);
  // Hiding is the default: it only exposes the languages the viewer needs
  // without deleting anything. Removing rewrites the container (and deletes
  // matching external subtitles) and remains an explicit choice.
  const [action, setAction] = useState("disable");
  const [verification, setVerification] = useState("bounded");
  const [library, setLibrary] = useState("");
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [errorCode, setErrorCode] = useState("");
  const [review, setReview] = useState<CleanupPlan>();
  const [id, setID] = useState("");
  const [stopping, setStopping] = useState(false);
  // The worker runs every plan through one shared queue. Watching the active
  // plan keeps this control honest about work started elsewhere - including a
  // cleanup started on another page or before a reload.
  const active = useLiveResource<ActiveResponse>("/organization/active", 3000);
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
  const running =
    !!id &&
    (!status.data ||
      status.data.state === "queued" ||
      status.data.state === "running");
  const plan = active.data?.plan;
  const adopted =
    !id &&
    !review &&
    plan?.kind === "track_cleanup" &&
    (!plan.mode ||
      kind === "all" ||
      (kind === "audio") === (plan.mode === "other_audio")) &&
    (plan.state === "queued" || plan.state === "running");
  // A refused request names the operation that is using the library; the same
  // active-plan poll shows its live progress and offers the stop control.
  const blocking =
    errorCode === "organization_busy" ||
    (!id && plan && plan.kind !== "track_cleanup")
      ? plan
      : undefined;
  const disable = action === "disable";

  useEffect(() => {
    if (adopted && plan) setID(plan.id);
  }, [adopted, plan]);

  useEffect(() => {
    if (id && status.data?.state === "completed" && reported !== id) {
      setReported(id);
      onDone?.();
    }
  }, [id, status.data?.state, reported, onDone]);

  async function submit() {
    setBusy(true);
    setError("");
    setErrorCode("");
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
              verification,
              mode,
              action,
              library,
              ...(mediaID ? { media_id: mediaID } : {}),
            }),
            signal: AbortSignal.timeout(120000),
          }),
        );
      }
    } catch (e) {
      setError(messageFor(e));
      setErrorCode(e instanceof APIError ? e.code : "");
    } finally {
      setBusy(false);
    }
  }

  async function stopBlocking() {
    if (!blocking) return;
    setStopping(true);
    try {
      await api(`/organization/plans/${blocking.id}/cancel`, {
        method: "POST",
        body: "{}",
      });
      setError("");
      setErrorCode("");
      active.reload();
    } catch (e) {
      setError(messageFor(e));
      setErrorCode("");
    } finally {
      setStopping(false);
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
      setErrorCode("");
    }
  }

  return (
    <FieldSet
      legend={title}
      description={
        disable
          ? "Unwanted track languages are hidden in place on Matroska (.mkv/.webm) files: the container header is edited without rewriting the video, and no files are deleted. Other containers are skipped and can be removed instead."
          : "Embedded tracks are removed by rewriting the video container. Selected external subtitles are permanently deleted."
      }
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
              !blocking &&
              status.data?.state !== "completed" && (
                <Button
                  color={disable ? undefined : "red"}
                  disabled={busy}
                  onClick={() => void submit()}
                >
                  {busy
                    ? "Working…"
                    : id
                      ? "Resume cleanup"
                      : review
                        ? disable
                          ? "Hide tracks"
                          : "Remove tracks"
                        : "Review cleanup"}
                </Button>
              )}
          </>
        }
      >
        {review ? (
          <>
            <p>{modes.find(([value]) => value === mode)?.[1]}</p>
            <p>
              {review.track_cleanup.targets.length} registered videos in scope.{" "}
              {disable ? "Hide in place" : "Remove"}.
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
              Action{" "}
              <select
                value={action}
                onChange={(event) => setAction(event.target.value)}
              >
                <option value="disable">Hide in place (recommended)</option>
                <option value="remove">Remove (rewrite the video)</option>
              </select>
            </label>
            {disable && (
              <label>
                Verification{" "}
                <select
                  value={verification}
                  onChange={(e) => setVerification(e.target.value)}
                >
                  <option value="bounded">
                    Header and track checks (fast)
                  </option>
                  <option value="full">Full file checksums (slow)</option>
                </select>
                <span className="rs-form-help">
                  Fast checks use file identity, sampled fingerprints and live
                  track comparisons. They do not prove every media byte is
                  unchanged.
                </span>
              </label>
            )}
            <label>
              Tracks{" "}
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
        {disable && !review && (
          <p className="rs-muted rs-small">
            Hide keeps every track: it clears the Matroska “enabled” flag so
            players stop offering the language. Only Matroska (.mkv/.webm) files
            are changed; others are reported as skipped.
          </p>
        )}
        {mode === "other_audio" && (
          <p>
            Videos with no saved original language or no audio track that can be
            safely kept are skipped.
          </p>
        )}
        {blocking && (
          <div role="status" className="rs-stack">
            <p>
              <strong>{activePlanLabel(blocking.kind)}</strong> is using the
              library. One media operation runs at a time, so this review waits
              for it.
            </p>
            <p>
              {activePlanDetail(blocking)} ·{" "}
              {blocking.state === "queued" ? "queued" : "running"}
            </p>
            {activePlanStoppable(blocking) && (
              <Button
                size="1"
                variant="soft"
                color="gray"
                disabled={stopping}
                onClick={() => void stopBlocking()}
              >
                {stopping
                  ? "Stopping…"
                  : blocking.state === "queued"
                    ? "Cancel the queued job"
                    : "Stop after the video in progress"}
              </Button>
            )}
            {!activePlanStoppable(blocking) && (
              <p className="rs-muted rs-small">
                This job cannot be interrupted safely; it finishes the file it
                is working on, and this review can start afterwards.
              </p>
            )}
          </div>
        )}
        {id && (
          <BatchProgress
            progress={status.data?.progress}
            processed={progress?.processed}
            total={total}
            state={status.data?.state ?? "queued"}
          />
        )}
        {id && (
          <p role="status">
            {progress?.processed ?? 0} of {total} videos reviewed.{" "}
            {(status.data?.track_cleanup.request?.action ?? action) ===
            "disable"
              ? `${progress?.disabled ?? 0} tracks hidden`
              : `${progress?.dropped ?? 0} tracks removed`}{" "}
            across {progress?.changed ?? 0} videos.{" "}
            {progress?.skipped
              ? `${progress.skipped} unresolved skip${progress.skipped === 1 ? "" : "s"}.`
              : ""}
          </p>
        )}
        {progress && <CleanupSkipDetails cleanup={progress} />}
        {!blocking &&
          (error ||
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
