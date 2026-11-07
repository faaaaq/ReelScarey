import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, SearchCheck } from "lucide-react";
import { Link } from "react-router-dom";
import { api, messageFor } from "../../api";
import { Alert, ToolbarButton, ToolbarSection } from "../../ui";

export type ItemSearchResult = {
  outcome: string;
  message?: string;
  download_id?: string;
  started_at?: number;
};

/**
 * Triggers the focused automatic search for one title (and optionally one
 * episode) and follows its recorded outcome. The run itself is the same
 * scheduled pipeline: download mode queues the best matching release,
 * search-only mode records candidates for review in Automation.
 */
export function useItemSearch() {
  const [pending, setPending] = useState<{
    mediaID: number;
    episodeID?: number;
  } | null>(null);
  const [result, setResult] = useState<ItemSearchResult | null>(null);
  const startedAt = useRef(0);
  const attempt = useRef(0);

  const stop = useCallback(() => {
    setPending(null);
    attempt.current = 0;
  }, []);

  const poll = useCallback(
    (target: { mediaID: number; episodeID?: number }) => {
      const query = new URLSearchParams({
        media_id: String(target.mediaID),
      });
      if (target.episodeID) query.set("episode_id", String(target.episodeID));
      api<ItemSearchResult>(`/automation/item-results?${query.toString()}`)
        .then((value) => {
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
        .catch(() => {
          attempt.current += 1;
        });
    },
    [stop],
  );

  useEffect(() => {
    if (!pending) return;
    const timer = window.setInterval(() => poll(pending), 3000);
    return () => window.clearInterval(timer);
  }, [pending, poll]);

  const trigger = useCallback(
    (mediaID: number, episodeID?: number) => {
      if (pending) return;
      setPending({ mediaID, episodeID });
      setResult(null);
      startedAt.current = Math.floor(Date.now() / 1000);
      attempt.current = 0;
      api("/automation/search-item", {
        method: "POST",
        body: JSON.stringify({
          media_id: mediaID,
          ...(episodeID ? { episode_id: episodeID } : {}),
        }),
      })
        .then(() => poll({ mediaID, episodeID }))
        .catch((e) => {
          setResult({ outcome: "error", message: messageFor(e) });
          stop();
        });
    },
    [pending, stop],
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
      : result.outcome === "error" || result.outcome === "needs_attention"
        ? "danger"
        : "info";
  const titles: Record<string, string> = {
    queued: "Download queued",
    match: "A matching release is available",
    no_match: "No matching release found",
    needs_attention: "Search needs attention",
    timeout: "Search still running",
    error: "Search could not start",
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
