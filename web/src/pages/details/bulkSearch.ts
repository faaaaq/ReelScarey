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
  | { status: "running"; started: number }
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
        const runs = await api<RunSummary[]>("/automation/runs").catch(
          () => [] as RunSummary[],
        );
        baseline.current = runs[0]?.id ?? null;
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
        const runs = await api<RunSummary[]>("/automation/runs");
        if (cancelled) return;
        const fresh = runs.filter(
          (run) =>
            run.id !== baseline.current &&
            run.started_at >= started.current - 2,
        );
        const active = fresh.some(
          (run) => run.state === "running" || run.state === "scheduled",
        );
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
        } else if (attempts.current >= 40) {
          setState({
            status: "error",
            message:
              "The search has not started yet. It runs when the scheduler is free; check Automation for progress.",
          });
        }
        attempts.current += 1;
      } catch {
        attempts.current += 1;
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
    return "Searching saved releases in the background…";
  if (state.status === "error") return state.message;
  if (state.status !== "done") return "";
  if (state.checked === 0)
    return state.message || "Nothing matched the search filters.";
  if (state.mode === "download")
    return `Searched ${state.checked} item${state.checked === 1 ? "" : "s"} and queued ${state.queued} download${state.queued === 1 ? "" : "s"}.`;
  return `Searched ${state.checked} item${state.checked === 1 ? "" : "s"}; ${state.queued === 0 ? "no releases matched" : "results are saved for review in Automation"}.`;
}
