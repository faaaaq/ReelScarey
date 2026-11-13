import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, SearchCheck } from "lucide-react";
import { Link } from "react-router-dom";
import { api, messageFor } from "../../api";
import { Alert, ToolbarButton, ToolbarSection } from "../../ui";
import { type RunSummary } from "./bulkSearch";

export type ItemSearchResult = {
  outcome: string;
  message?: string;
  download_id?: string;
  started_at?: number;
};

/** Summarise a finished scheduled run with the shared alert outcomes. */
function runToResult(run: RunSummary): ItemSearchResult {
  const message = (run.message ?? "").trim();
  if (run.queued > 0) {
    return {
      outcome: "queued",
      message:
        message ||
        `Queued ${run.queued} download${run.queued === 1 ? "" : "s"}.`,
    };
  }
  if ((run.matched ?? 0) > 0) {
    return {
      outcome: "match",
      message:
        message ||
        `Found ${run.matched} matching release${run.matched === 1 ? "" : "s"}. Open Automation to review them.`,
    };
  }
  if (run.state === "completed") {
    return {
      outcome: "no_match",
      message: message || "No matching release was found.",
    };
  }
  return {
    outcome: "needs_attention",
    message:
      message ||
      "The search run did not complete normally. Check Automation for details.",
  };
}

/**
 * Triggers the focused automatic search for one title (and optionally one
 * episode or one season) and follows its recorded outcome. The run itself is
 * the same scheduled pipeline: download mode queues the best matching
 * release, search-only mode records candidates for review in Automation.
 * Episode searches are followed through /automation/item-results; season
 * searches have no single-item endpoint, so they are followed through the
 * scheduled run list.
 */
export function useItemSearch() {
  const [pending, setPending] = useState<{
    mediaID: number;
    episodeID?: number;
    season?: number;
  } | null>(null);
  const [result, setResult] = useState<ItemSearchResult | null>(null);
  const startedAt = useRef(0);
  const attempt = useRef(0);
  const failures = useRef(0);
  const baselineRun = useRef<string | null>(null);

  /** Consecutive poll failures tolerated before giving up with an error. */
  const maxPollFailures = 3;

  function failPoll(message: string) {
    setResult({ outcome: "poll_failed", message });
    stop();
  }

  const stop = useCallback(() => {
    setPending(null);
    attempt.current = 0;
    failures.current = 0;
  }, []);

  const episodePoll = useCallback(
    (target: { mediaID: number; episodeID?: number }) => {
      const query = new URLSearchParams({
        media_id: String(target.mediaID),
      });
      if (target.episodeID) query.set("episode_id", String(target.episodeID));
      api<ItemSearchResult>(`/automation/item-results?${query.toString()}`)
        .then((value) => {
          failures.current = 0;
          if (attempt.current > 30) {
            setResult({
              outcome: "timeout",
              message:
                "The search is still running or has not recorded a result. Check Automation.",
            });
            stop();
            return;
          }
          if (
            value.outcome !== "none" &&
            (value.started_at ?? 0) + 2 >= startedAt.current
          ) {
            setResult(value);
            stop();
            return;
          }
          attempt.current += 1;
        })
        .catch((e) => {
          failures.current += 1;
          attempt.current += 1;
          if (failures.current >= maxPollFailures) {
            failPoll(
              `The search result could not be checked (${messageFor(e)}).`,
            );
          }
        });
    },
    [stop],
  );

  const runPoll = useCallback(
    (_target: { mediaID: number; season: number }) => {
      api<{ items: RunSummary[] }>("/automation/runs")
        .then((out) => {
          failures.current = 0;
          if (attempt.current > 40) {
            setResult({
              outcome: "timeout",
              message:
                "The search is still running or has not recorded a result. Check Automation.",
            });
            stop();
            return;
          }
          // Only the run created by this request is summarised; the baseline
          // run that existed before the trigger is ignored.
          const fresh = (out.items ?? []).filter(
            (run) =>
              run.id !== baselineRun.current &&
              run.started_at >= startedAt.current - 2,
          );
          const active = fresh.some(
            (run) => run.state === "running" || run.state === "scheduled",
          );
          if (!active && fresh.length > 0) {
            setResult(runToResult(fresh[0]));
            stop();
            return;
          }
          attempt.current += 1;
        })
        .catch((e) => {
          failures.current += 1;
          attempt.current += 1;
          if (failures.current >= maxPollFailures) {
            failPoll(
              `The search result could not be checked (${messageFor(e)}).`,
            );
          }
        });
    },
    [stop],
  );

  const poll = useCallback(
    (target: { mediaID: number; episodeID?: number; season?: number }) => {
      if (target.season !== undefined && target.episodeID === undefined) {
        runPoll({ mediaID: target.mediaID, season: target.season });
        return;
      }
      episodePoll({ mediaID: target.mediaID, episodeID: target.episodeID });
    },
    [episodePoll, runPoll],
  );

  useEffect(() => {
    if (!pending) return;
    const timer = window.setInterval(() => poll(pending), 3000);
    return () => window.clearInterval(timer);
  }, [pending, poll]);

  const trigger = useCallback(
    (mediaID: number, episodeID?: number, seasonNumber?: number) => {
      if (pending) return;
      const target = { mediaID, episodeID, season: seasonNumber };
      setPending(target);
      setResult(null);
      startedAt.current = Math.floor(Date.now() / 1000);
      attempt.current = 0;
      const request = api("/automation/search-item", {
        method: "POST",
        body: JSON.stringify({
          media_id: mediaID,
          ...(episodeID ? { episode_id: episodeID } : {}),
          ...(seasonNumber !== undefined ? { season: seasonNumber } : {}),
        }),
      });
      if (seasonNumber !== undefined && episodeID === undefined) {
        // Remember which run was newest so only the new one is summarised.
        api<{ items: RunSummary[] }>("/automation/runs")
          .catch(() => ({ items: [] as RunSummary[] }))
          .then((out) => {
            baselineRun.current = out.items?.[0]?.id ?? null;
            return request;
          })
          .then(() => runPoll({ mediaID, season: seasonNumber }))
          .catch((e) => {
            setResult({ outcome: "error", message: messageFor(e) });
            stop();
          });
        return;
      }
      request
        .then(() => episodePoll({ mediaID, episodeID }))
        .catch((e) => {
          setResult({ outcome: "error", message: messageFor(e) });
          stop();
        });
    },
    [pending, stop, episodePoll, runPoll],
  );

  return { trigger, pending, result, reset: () => setResult(null) };
}

export function ItemSearchResultAlert({
  result,
  onDismiss,
}: {
  result: ItemSearchResult | null;
  onDismiss: () => void;
}) {
  if (!result) return null;
  const kind =
    result.outcome === "queued" || result.outcome === "match"
      ? "success"
      : result.outcome === "error" ||
          result.outcome === "needs_attention" ||
          result.outcome === "poll_failed"
        ? "danger"
        : "info";
  const titles: Record<string, string> = {
    queued: "Download queued",
    match: "A matching release is available",
    no_match: "No matching release found",
    needs_attention: "Search needs attention",
    timeout: "Search still running",
    error: "Search could not start",
    poll_failed: "Search could not be checked",
  };
  return (
    <Alert
      kind={kind}
      title={titles[result.outcome] ?? "Search result"}
      role="status"
      dismissKey="item-search"
    >
      <p>{result.message}</p>
      <div className="rs-row">
        {result.download_id && (
          <Link
            className="rs-link-button rs-small"
            to={`/activity?job=${encodeURIComponent(result.download_id)}`}
          >
            View the download in Activity
          </Link>
        )}
        <button
          type="button"
          className="rs-link-button rs-small"
          onClick={onDismiss}
        >
          Dismiss
        </button>
      </div>
    </Alert>
  );
}

/** Toolbar button that starts the focused search for a title. */
export function TitleSearchButton({
  mediaID,
  pending,
  onTrigger,
}: {
  mediaID: number;
  pending: boolean;
  onTrigger: (mediaID: number) => void;
}) {
  return (
    <ToolbarSection>
      <ToolbarButton
        icon={pending ? Loader2 : SearchCheck}
        label="Search"
        spinning={pending}
        disabled={pending}
        title="Search for this title now: download mode queues the best match, search-only mode saves results for review"
        onClick={() => onTrigger(mediaID)}
      />
    </ToolbarSection>
  );
}
