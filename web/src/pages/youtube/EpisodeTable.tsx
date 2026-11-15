import { useState } from "react";
import { Badge, Button } from "@radix-ui/themes";
import { APIError, api, messageFor } from "../../api";
import { RequestState } from "../../components";
import { Table } from "../../ui";
import { useResource } from "../../useResource";
import {
  englishSubsBadge,
  episodeStateColor,
  episodeStateLabel,
} from "./status";
import type {
  EpisodeQueueResult,
  SourceEpisode,
  SourceEpisodesResponse,
} from "./types";

type EpisodeFilter =
  | "all"
  | "missing"
  | "queued"
  | "downloading"
  | "review"
  | "completed"
  | "failed"
  | "subs";

const FILTERS: { key: EpisodeFilter; label: string }[] = [
  { key: "all", label: "All" },
  { key: "missing", label: "Missing" },
  { key: "queued", label: "Queued" },
  { key: "downloading", label: "Downloading" },
  { key: "review", label: "Review needed" },
  { key: "completed", label: "Completed" },
  { key: "failed", label: "Failed" },
  { key: "subs", label: "Missing English subtitles" },
];

function matches(episode: SourceEpisode, filter: EpisodeFilter) {
  switch (filter) {
    case "missing":
      return episode.state === "missing";
    case "queued":
      return episode.state === "queued";
    case "downloading":
      return episode.state === "downloading" || episode.state === "importing";
    case "review":
      return episode.state === "awaiting_review";
    case "completed":
      return episode.state === "downloaded";
    case "failed":
      return episode.state === "failed";
    case "subs":
      return episode.english_subtitles === false;
    default:
      return true;
  }
}

function episodeCode(episode: SourceEpisode) {
  if (!episode.episode_number) return "unnumbered";
  return `S${String(episode.season_number).padStart(2, "0")}E${String(
    episode.episode_number,
  ).padStart(2, "0")}`;
}

/** A row can only be queued when no download is active and no file exists;
 * an episode awaiting import review already has an active job and is handled
 * from the Downloads page instead. */
function queueable(episode: SourceEpisode) {
  return (
    episode.videos.length > 0 &&
    episode.file_count === 0 &&
    !["queued", "downloading", "importing", "awaiting_review"].includes(
      episode.state,
    )
  );
}

/** Per-episode view for one YouTube source (read-only feed + reviewed queue). */
export function EpisodeTable({
  sourceID,
  revision,
  onChanged,
}: {
  sourceID: string;
  revision: number;
  /** Called after queue attempts so the parent can refresh the source revision. */
  onChanged?: () => void;
}) {
  const episodes = useResource<SourceEpisodesResponse>(
    sourceID ? `/youtube/sources/${sourceID}/episodes` : null,
  );
  const [filter, setFilter] = useState<EpisodeFilter>("all");
  const [selection, setSelection] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [results, setResults] = useState<EpisodeQueueResult["items"]>([]);

  const items = episodes.data?.items ?? [];
  const visible = items.filter((episode) => matches(episode, filter));
  const selectedEpisodes = items.filter((episode) =>
    selection.has(String(episode.episode_id)),
  );
  const queueableSelected = selectedEpisodes.filter(queueable);

  function toggle(episodeID: number) {
    setSelection((current) => {
      const next = new Set(current);
      const key = String(episodeID);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  async function queue(selections: { video_id: string; episode_id: number }[]) {
    if (busy || selections.length === 0 || selections.length > 25) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const outcome = await api<EpisodeQueueResult>(
        `/youtube/sources/${sourceID}/episodes/queue`,
        {
          method: "POST",
          body: JSON.stringify({
            revision,
            selections,
            reviewed: true,
          }),
        },
      );
      const queued = outcome.items.filter((item) => item.state === "queued");
      setResults(outcome.items);
      setNotice(
        queued.length === outcome.items.length
          ? `Queued ${queued.length} download${queued.length === 1 ? "" : "s"}.`
          : `${queued.length} of ${outcome.items.length} queued. See messages below.`,
      );
      setSelection(new Set());
      episodes.reload();
      onChanged?.();
    } catch (e) {
      // A revision conflict means the show changed under us; the source row
      // must be re-read before any further queue attempt.
      if (e instanceof APIError && e.status === 409) {
        setError("This show changed; review the YouTube source.");
      } else {
        setError(messageFor(e));
      }
      onChanged?.();
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="yt-episodes">
      <div
        className="yt-chips"
        role="group"
        aria-label="Filter episodes by state"
      >
        {FILTERS.map((item) => (
          <button
            key={item.key}
            type="button"
            className="yt-chip"
            aria-pressed={filter === item.key}
            onClick={() => setFilter(item.key)}
          >
            {item.label}
          </button>
        ))}
      </div>
      {error && (
        <p className="rs-alert is-danger" role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className="rs-alert is-success" role="status">
          {notice}
        </p>
      )}
      <RequestState
        loading={episodes.loading}
        error={episodes.error}
        retry={episodes.reload}
      />
      {items.length === 0 && !episodes.loading && !episodes.error && (
        <p className="yt-empty">
          No episodes are loaded for this show's seasons yet.
        </p>
      )}
      {items.length > 0 && (
        <>
          <div className="rs-form-actions yt-row-actions">
            <Button
              size="1"
              disabled={busy || queueableSelected.length === 0}
              onClick={() =>
                void queue(
                  queueableSelected.slice(0, 25).map((episode) => ({
                    video_id: episode.videos[0].video_id,
                    episode_id: episode.episode_id,
                  })),
                )
              }
            >
              {queueableSelected.length > 25
                ? `Queue selected (first 25 of ${queueableSelected.length})`
                : `Queue selected (${queueableSelected.length})`}
            </Button>
            {queueableSelected.length > 25 && (
              <span className="rs-form-help">
                At most 25 episodes can be queued at once; deselect the rest.
              </span>
            )}
          </div>
          {results.length > 0 && (
            <ul className="yt-batch-results" aria-label="Queue results">
              {results.map((result) => (
                <li key={`${result.video_id}-${result.episode_id}`}>
                  <span>{result.video_id}</span>
                  <Badge color={result.state === "queued" ? "green" : "gray"}>
                    {result.state === "queued" ? "Queued" : result.message}
                  </Badge>
                </li>
              ))}
            </ul>
          )}
          <Table label="YouTube episodes" className="yt-episode-table">
            <thead>
              <tr>
                <th scope="col">Select</th>
                <th scope="col">Episode</th>
                <th scope="col">Video</th>
                <th scope="col">State</th>
                <th scope="col">Files</th>
                <th scope="col">Subtitles</th>
                <th scope="col">Actions</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((episode) => {
                const subs = englishSubsBadge(episode.english_subtitles);
                const eligible = queueable(episode);
                return (
                  <tr key={episode.episode_id}>
                    <td>
                      <input
                        type="checkbox"
                        disabled={!eligible}
                        checked={selection.has(String(episode.episode_id))}
                        aria-label={`Select ${episodeCode(episode)} ${episode.title}`}
                        onChange={() => toggle(episode.episode_id)}
                      />
                    </td>
                    <td>
                      {episodeCode(episode)} · {episode.title}
                      {!episode.monitored && (
                        <>
                          {" "}
                          <Badge color="gray">Unmonitored</Badge>
                        </>
                      )}
                    </td>
                    <td>
                      {episode.videos.length === 0
                        ? "No matching video"
                        : episode.videos.map((video) => (
                            <span
                              key={video.video_id}
                              className="yt-episode-video"
                            >
                              {video.title}
                            </span>
                          ))}
                      {episode.note && (
                        <span className="rs-form-help">{episode.note}</span>
                      )}
                    </td>
                    <td>
                      <Badge color={episodeStateColor(episode.state)}>
                        {episodeStateLabel(episode.state)}
                      </Badge>
                    </td>
                    <td>{episode.file_count}</td>
                    <td>
                      <Badge color={subs.color}>{subs.label}</Badge>
                    </td>
                    <td>
                      <Button
                        size="1"
                        variant="soft"
                        disabled={busy || !eligible}
                        aria-label={`Download ${episodeCode(episode)}`}
                        onClick={() =>
                          void queue([
                            {
                              video_id: episode.videos[0].video_id,
                              episode_id: episode.episode_id,
                            },
                          ])
                        }
                      >
                        Download
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        </>
      )}
    </div>
  );
}
