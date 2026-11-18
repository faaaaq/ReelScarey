import { Link } from "react-router-dom";
import { Loader2 } from "lucide-react";
import { useLiveResource } from "../useLiveResource";

type ActivePlan = {
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
  organization: "Organizing the library",
};

/**
 * Persistent media-work indicator. The organization worker runs one reviewed
 * plan at a time and holds the media gate while it works, so every page shows
 * what is running and how far along it is. The state lives in the saved plan,
 * so navigation and full reloads never lose the progress. Transient polling
 * failures stay silent, exactly like the other cross-page badges.
 */
export function MediaWorkBanner() {
  const active = useLiveResource<ActiveResponse>("/organization/active", 2000);
  const plan = active.data?.plan;
  if (!active.data || !plan) return null;
  const label = labels[plan.kind] ?? "Working on the library";
  const detail =
    plan.kind === "track_cleanup"
      ? `${plan.processed} of ${plan.total} videos · ${plan.dropped} track${
          plan.dropped === 1 ? "" : "s"
        } removed${plan.skipped ? ` · ${plan.skipped} skipped` : ""}`
      : plan.state === "queued"
        ? "Waiting to start"
        : "In progress";
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
      <Link to="/organization" className="rs-work-link">
        Open organization
      </Link>
    </aside>
  );
}
