import { useState } from "react";
import { Link } from "react-router-dom";
import { Button } from "@radix-ui/themes";
import { Loader2, Square } from "lucide-react";
import { api } from "../api";
import { useLiveResource } from "../useLiveResource";

export type ActivePlan = {
  id: string;
  state: string;
  kind: string;
  message?: string;
  processed: number;
  total: number;
  changed: number;
  skipped: number;
  dropped: number;
};

type ActiveResponse = { active: boolean; plan?: ActivePlan };

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
    return `${plan.processed} of ${plan.total} videos · ${plan.dropped} track${
      plan.dropped === 1 ? "" : "s"
    } removed${plan.skipped ? ` · ${plan.skipped} skipped` : ""}`;
  }
  return plan.state === "queued" ? "Waiting to start" : "In progress";
}

/** A queued plan can always return to review; a running track cleanup can be
 * stopped safely between videos. Other running jobs cannot be interrupted. */
export function activePlanStoppable(plan: ActivePlan) {
  return plan.state === "queued" || plan.kind === "track_cleanup";
}

/**
 * Persistent media-work indicator. The organization worker runs one reviewed
 * plan at a time and holds the media gate while it works, so every page shows
 * what is running and how far along it is. The state lives in the saved plan,
 * so navigation and full reloads never lose the progress. Transient polling
 * failures stay silent, exactly like the other cross-page badges.
 */
export function MediaWorkBanner() {
  const active = useLiveResource<ActiveResponse>("/organization/active", 2000);
  const [stopping, setStopping] = useState("");
  const [error, setError] = useState("");
  const plan = active.data?.plan;
  if (!active.data || !plan) return null;
  const label = activePlanLabel(plan.kind);
  const detail = activePlanDetail(plan);
  const stoppable = activePlanStoppable(plan);
  async function stop() {
    if (!plan) return;
    setStopping(plan.id);
    setError("");
    try {
      await api(`/organization/plans/${plan.id}/cancel`, {
        method: "POST",
        body: "{}",
      });
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
      aria-label={`${label}: ${detail}`}
    >
      <Loader2 size={14} className="rs-work-spinner" aria-hidden="true" />
      <span className="rs-work-label">
        {label} · {detail}
      </span>
      {stoppable && (
        <Button
          size="1"
          variant="soft"
          color="gray"
          disabled={!!stopping}
          aria-label={`Stop ${label}`}
          onClick={() => void stop()}
        >
          <Square size={11} aria-hidden="true" />
          {stopping ? "Stopping…" : "Stop"}
        </Button>
      )}
      {error && (
        <span className="rs-work-label" role="alert">
          {error}
        </span>
      )}
      <Link to="/organization" className="rs-work-link">
        Open organization
      </Link>
    </aside>
  );
}
