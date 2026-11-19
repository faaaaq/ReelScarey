import { BatchProgress, type WorkProgress } from "./BatchProgress";
import { useState } from "react";
import { Link } from "react-router-dom";
import { Button } from "@radix-ui/themes";
import { Loader2, Pause, Play, Square } from "lucide-react";
import { api } from "../api";
import { ConfirmDialog } from "../ui";
import { useLiveResource } from "../useLiveResource";

export type ActivePlan = {
  progress?: WorkProgress;
  disabled?: number;
  action?: string;
  mode?: string;
  id: string;
  state: string;
  kind: string;
  message?: string;
  processed: number;
  total: number;
  changed: number;
  skipped: number;
  dropped: number;
  // The server decides whether the owner can stop this plan while it runs.
  stoppable?: boolean;
};

export type TitleQueue = {
  paused: boolean;
  pending: number;
  active_plan_id?: string;
  message?: string;
};

type ActiveResponse = {
  active: boolean;
  plan?: ActivePlan;
  title_queue?: TitleQueue;
};

const labels: Record<string, string> = {
  track_cleanup: "Removing media tracks",
  deletion: "Deleting library files",
  replacement: "Replacing a library file",
  folder_move: "Moving a library folder",
  organization: "A library organization job",
};

export function activePlanLabel(kind: string) {
  return labels[kind] ?? "Working on the library";
}

export function activePlanDetail(plan: ActivePlan) {
  if (plan.kind === "track_cleanup") {
    const count =
      plan.action === "disable" ? (plan.disabled ?? 0) : plan.dropped;
    return `${plan.processed} of ${plan.total} videos · ${count} track${
      count === 1 ? "" : "s"
    } ${plan.action === "disable" ? "hidden" : "removed"}${plan.skipped ? ` · ${plan.skipped} skipped` : ""}`;
  }
  return plan.state === "queued" ? "Waiting to start" : "In progress";
}

/** A queued plan can always return to review; a running job the server marks
 * stoppable (track cleanup, permanent deletion) stops safely between files. */
export function activePlanStoppable(plan: ActivePlan) {
  return (
    plan.stoppable ?? (plan.state === "queued" || plan.kind === "track_cleanup")
  );
}

function titleQueueSummary(queue: TitleQueue) {
  const items = `${queue.pending} title update${queue.pending === 1 ? "" : "s"}`;
  return queue.paused ? `${items} waiting · queue paused` : `${items} waiting`;
}

/**
 * Persistent media-work indicator. The organization worker runs one reviewed
 * plan at a time and holds the media gate while it works, so every page shows
 * what is running and how far along it is. The automatic title queue is shown
 * here too: it can be paused (stops claiming new files) or stopped outright
 * (drops the pending work without changing any media). The state lives in the
 * saved plan and the durable queue, so navigation and reloads never lose it.
 * Transient polling failures stay silent, like the other cross-page badges.
 */
export function MediaWorkBanner() {
  const active = useLiveResource<ActiveResponse>("/organization/active", 2000);
  const [stopping, setStopping] = useState("");
  const [queueBusy, setQueueBusy] = useState(false);
  const [error, setError] = useState("");
  const data = active.data;
  const plan = data?.plan;
  const queue = data?.title_queue;
  if (!data || (!plan && !queue?.pending && !queue?.paused)) return null;

  // A queue with work is only worth a control when it is not just the file the
  // worker is already rewriting right now.
  const showQueue = !!queue && (queue.pending > 0 || queue.paused);
  const label = plan
    ? plan.kind === "track_cleanup" && plan.action === "disable"
      ? "Hiding media tracks"
      : activePlanLabel(plan.kind)
    : "";
  const detail = plan ? activePlanDetail(plan) : "";
  const stoppable = !!plan && activePlanStoppable(plan);

  async function run(action: () => Promise<unknown>) {
    setQueueBusy(true);
    setError("");
    try {
      await action();
      active.reload();
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "The action could not be completed.",
      );
    } finally {
      setQueueBusy(false);
    }
  }

  async function stop(plan: ActivePlan) {
    setStopping(plan.id);
    setError("");
    try {
      await api(`/organization/plans/${plan.id}/cancel`, {
        method: "POST",
        body: "{}",
      });
      active.reload();
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "The job could not be stopped.",
      );
    } finally {
      setStopping("");
    }
  }

  return (
    <aside
      className="rs-work-banner"
      role="status"
      aria-label={plan ? `${label}: ${detail}` : titleQueueSummary(queue!)}
    >
      <Loader2 size={14} className="rs-work-spinner" aria-hidden="true" />
      {plan && (
        <span className="rs-work-label">
          {label} · {detail}
        </span>
      )}
      {plan && (
        <details className="rs-work-details">
          <summary>{plan.progress?.stage ?? "Progress details"}</summary>
          <BatchProgress
            progress={plan.progress}
            processed={plan.processed}
            total={plan.total}
            state={plan.state}
          />
        </details>
      )}
      {stoppable && plan && (
        <Button
          size="1"
          variant="soft"
          color="gray"
          disabled={!!stopping}
          aria-label={`Stop ${label}`}
          onClick={() => void stop(plan)}
        >
          <Square size={11} aria-hidden="true" />
          {stopping ? "Stopping…" : "Stop"}
        </Button>
      )}
      {showQueue && queue && (
        <>
          <span className="rs-work-label">{titleQueueSummary(queue)}</span>
          <Button
            size="1"
            variant="soft"
            color="gray"
            disabled={queueBusy}
            aria-label={
              queue.paused ? "Resume title updates" : "Pause title updates"
            }
            onClick={() =>
              void run(() =>
                api("/organization/title-queue", {
                  method: "PUT",
                  body: JSON.stringify({ paused: !queue.paused }),
                }),
              )
            }
          >
            {queue.paused ? (
              <Play size={11} aria-hidden="true" />
            ) : (
              <Pause size={11} aria-hidden="true" />
            )}
            {queue.paused ? "Resume" : "Pause"}
          </Button>
          {queue.pending > 0 && (
            <ConfirmDialog
              title="Stop the title-update batch?"
              description={`${titleQueueSummary(queue)} will be dropped from the queue. No media files are changed; a title update already running finishes safely.`}
              confirmLabel="Stop batch"
              cancelLabel="Keep waiting"
              color="orange"
              onConfirm={() =>
                run(() =>
                  api("/organization/title-queue/discard", {
                    method: "POST",
                    body: "{}",
                  }),
                )
              }
              trigger={
                <Button
                  size="1"
                  variant="soft"
                  color="gray"
                  disabled={queueBusy}
                >
                  Stop batch
                </Button>
              }
            />
          )}
        </>
      )}
      {error && (
        <span className="rs-work-label" role="alert">
          {error}
        </span>
      )}
      <Link to="/organization?view=jobs" className="rs-work-link">
        Open organization
      </Link>
    </aside>
  );
}
