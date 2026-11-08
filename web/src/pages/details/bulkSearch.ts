import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api";

export type RunSummary = {
  id: string;
  started_at: number;
  finished_at: number;
  state: string;
  message: string;
  mode: string;
  checked: number;
  queued: number;
};

export type BulkSearchState =
  | { status: "idle" }
  | {
      status: "running";
      started: number;
      /** A scheduler run has been observed for this request. */
      active?: boolean;
      /** An earlier run still holds the scheduler; this request is queued. */
      waiting?: boolean;
      checked?: number;
      queued?: number;
      message?: string;
    }
  | {
      status: "done";
      mode: string;
      checked: number;
      queued: number;
      interrupted: boolean;
      message: string;
    }
  | { status: "error"; message: string };

/**
 * Requests a scoped search (one title or a whole media kind) and follows the
 * scheduled runs it produces, so the page can say plainly what happened.
 * Download mode counts queued downloads; search-only mode counts reviewed
 * candidates.
 */
export function useBulkSearch() {
  const [state, setState] = useState<BulkSearchState>({ status: "idle" });
  const started = useRef(0);
  const baseline = useRef<string | null>(null);
  const attempts = useRef(0);

  const run = useCallback(
    async (request: () => Promise<unknown>) => {
      if (state.status === "running") return;
      setState({ status: "running", started: Date.now() });
      started.current = Math.floor(Date.now() / 1000);
      attempts.current = 0;
      try {
        // Remember which run was newest so only the new one is summarised.
        const runs = await api<{ items: RunSummary[] }>(
          "/automation/runs",
        ).catch(() => ({ items: [] as RunSummary[] }));
        baseline.current = runs.items[0]?.id ?? null;
        await request();
      } catch (error) {
        setState({
          status: "error",
          message:
            error instanceof Error
              ? error.message
              : "The search could not be started.",
        });
      }
    },
    [state.status],
  );

  useEffect(() => {
    if (state.status !== "running") return;
    let cancelled = false;
    const poll = async () => {
      try {
        const response = await api<{ items: RunSummary[] }>("/automation/runs");
        if (cancelled) return;
        const runs = response.items ?? [];
        const fresh = runs.filter(
          (run) =>
            run.id !== baseline.current &&
            run.started_at >= started.current - 2,
        );
        const activeRuns = fresh.filter(
          (run) => run.state === "running" || run.state === "scheduled",
        );
        const active = activeRuns.length > 0;
        const busy = runs.find(
          (run) =>
            run.id === baseline.current &&
            (run.state === "running" || run.state === "scheduled"),
        );
        if (active) {
          // The scheduler does not expose a total; report its live counters
          // instead of inventing a percentage.
          setState({
            status: "running",
            started: started.current,
            active: true,
            checked: fresh.reduce((total, run) => total + run.checked, 0),
            queued: fresh.reduce((total, run) => total + run.queued, 0),
            message: activeRuns[0]?.message ?? "",
          });
        }
        if (active && attempts.current < 40) {
          attempts.current += 1;
          return;
        }
        if (fresh.length) {
          const newest = fresh[0];
          setState({
            status: "done",
            mode: newest.mode,
            checked: fresh.reduce((total, run) => total + run.checked, 0),
            queued: fresh.reduce((total, run) => total + run.queued, 0),
            interrupted: fresh.some((run) => run.state !== "completed"),
            message: newest.message,
          });
        } else if (busy) {
          // A search already owns the scheduler. Say so instead of looking
          // stuck until the worker can pick up this request.
          setState({
            status: "running",
            started: started.current,
            waiting: true,
          });
        } else if (attempts.current >= 40) {
          setState({
            status: "error",
            message:
              "The search has not started yet. It runs when the scheduler is free; check Automation for progress.",
          });
        }
        attempts.current += 1;
      } catch (error) {
        attempts.current += 1;
        if (attempts.current >= 40) {
          setState({
            status: "error",
            message:
              error instanceof Error
                ? error.message
                : "The search status could not be read. Check Automation for progress.",
          });
        }
      }
    };
    const timer = window.setInterval(() => void poll(), 2500);
    void poll();
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [state.status]);

  return { state, run, reset: () => setState({ status: "idle" }) };
}

export function bulkSearchMessage(state: BulkSearchState): string {
  if (state.status === "running")
    return state.waiting
      ? "Waiting for the current search to finish…"
      : state.active
        ? `Searching… ${state.checked ?? 0} checked, ${state.queued ?? 0} queued.`
        : "Starting search…";
  if (state.status === "error") return state.message;
  if (state.status !== "done") return "";
  if (state.checked === 0) {
    const message = state.message.trim();
    return `No items were checked. ${message ? `${message} ` : ""}This usually means the run is waiting for an indexer or download client; open Automation for details.`;
  }
  if (state.mode === "download")
    return `Searched ${state.checked} item${state.checked === 1 ? "" : "s"} and queued ${state.queued} download${state.queued === 1 ? "" : "s"}.`;
  const results = `${state.queued} result${state.queued === 1 ? "" : "s"}`;
  return state.queued === 0
    ? `Searched ${state.checked} item${state.checked === 1 ? "" : "s"}; no releases matched, so nothing was saved for review in Automation.`
    : `Searched ${state.checked} item${state.checked === 1 ? "" : "s"}; ${results} are saved for review in Automation.`;
}
