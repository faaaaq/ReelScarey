import { useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@radix-ui/themes";
import { ArrowDown, ArrowUp, Disc3, Play, Trash2, X } from "lucide-react";
import { Link } from "react-router-dom";
import { albumCoverURL } from "../musicArtwork";
import { useMusicQueue, type PlayerTrack } from "../musicPlayer";
import { formatTrackDuration } from "../ui/format";
import "./MusicNowPlaying.css";

function TrackCover({
  track,
  size,
}: {
  track: PlayerTrack;
  size: "current" | "queue";
}) {
  const source = albumCoverURL(track.albumID) || track.coverURL || "";
  const [failed, setFailed] = useState(false);

  useEffect(() => setFailed(false), [source]);

  const className = `music-now-playing-cover music-now-playing-cover-${size}`;
  if (!source || failed) {
    return (
      <span className={`${className} is-fallback`} aria-hidden="true">
        <Disc3 size={size === "current" ? 42 : 19} strokeWidth={1.4} />
      </span>
    );
  }

  return (
    <img
      className={className}
      src={source}
      alt=""
      loading={size === "queue" ? "lazy" : "eager"}
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
    />
  );
}

const MAX_VISIBLE_QUEUE_ROWS = 100;
const MAX_VISIBLE_HISTORY_ROWS = 25;

function formatUpNextDuration(count: number, known: number, totalMS: number) {
  if (count <= 0) return "";
  if (!known) return "duration unknown";
  const duration = formatTrackDuration(totalMS);
  return known === count ? duration : `at least ${duration}`;
}

/** A compact, browser-local playback queue for the music workspace's right pane. */
export function MusicNowPlaying({ onClose }: { onClose?: () => void }) {
  const player = useMusicQueue();
  const current = player.current;
  const [announcement, setAnnouncement] = useState("");
  const lastAnnounced = useRef<number | null>(null);

  useEffect(() => {
    if (!current) {
      lastAnnounced.current = null;
      setAnnouncement("");
      return;
    }
    if (lastAnnounced.current === current.fileID) return;
    lastAnnounced.current = current.fileID;
    setAnnouncement(
      `Current track: ${current.title}${
        current.artist ? ` by ${current.artist}` : ""
      }`,
    );
  }, [current]);

  // Suffix totals are rebuilt only for structural queue edits. Advancing the
  // current index is then O(1), even for a very large live queue.
  const durationTotals = useMemo(() => {
    const milliseconds = new Array<number>(player.queue.length + 1).fill(0);
    const known = new Array<number>(player.queue.length + 1).fill(0);
    for (let itemIndex = player.queue.length - 1; itemIndex >= 0; itemIndex--) {
      const durationMS = player.queue[itemIndex].durationMS;
      const valid =
        typeof durationMS === "number" &&
        Number.isFinite(durationMS) &&
        durationMS > 0;
      milliseconds[itemIndex] =
        milliseconds[itemIndex + 1] + (valid ? durationMS : 0);
      known[itemIndex] = known[itemIndex + 1] + (valid ? 1 : 0);
    }
    return { milliseconds, known };
  }, [player.queue]);
  const upNextStart =
    player.index >= 0 ? Math.min(player.index + 1, player.queue.length) : 0;
  const upNextCount = player.queue.length - upNextStart;
  const upNextTime = formatUpNextDuration(
    upNextCount,
    durationTotals.known[upNextStart],
    durationTotals.milliseconds[upNextStart],
  );
  const queueWindow = useMemo(() => {
    const start =
      player.queue.length > MAX_VISIBLE_QUEUE_ROWS && player.index >= 0
        ? Math.min(
            Math.max(0, player.index - MAX_VISIBLE_HISTORY_ROWS),
            player.queue.length - 1,
          )
        : 0;
    const end = Math.min(player.queue.length, start + MAX_VISIBLE_QUEUE_ROWS);
    return {
      start,
      end,
      tracks:
        start === 0 && end === player.queue.length
          ? player.queue
          : player.queue.slice(start, end),
    };
  }, [player.queue, player.index]);
  const queueTruncated =
    queueWindow.start > 0 || queueWindow.end < player.queue.length;

  return (
    <aside className="music-now-playing" aria-label="Now playing and queue">
      <div
        className="music-now-playing-sr-only"
        role="status"
        aria-live="polite"
        aria-atomic="true"
      >
        {announcement}
      </div>

      <header className="music-now-playing-header">
        <div className="music-now-playing-heading">
          <h2>Now Playing</h2>
          <span className="music-now-playing-count">
            {player.queue.length}{" "}
            {player.queue.length === 1 ? "track" : "tracks"}
          </span>
        </div>
        {onClose && (
          <Button
            type="button"
            size="1"
            variant="ghost"
            color="gray"
            aria-label="Close queue"
            title="Close queue"
            onClick={onClose}
          >
            <X size={15} aria-hidden="true" />
            <span>Close</span>
          </Button>
        )}
      </header>

      {current ? (
        <section
          className="music-now-playing-current"
          aria-label="Current track"
        >
          <TrackCover track={current} size="current" />
          <div className="music-now-playing-current-copy">
            <div className="music-now-playing-status-line">
              <span
                className="music-now-playing-status"
                data-playing={player.playing}
              >
                {player.playing ? "Playing" : "Paused"}
              </span>
              {current.formatLabel && (
                <span className="music-now-playing-format">
                  {current.formatLabel}
                </span>
              )}
            </div>
            <h3 title={current.title}>{current.title}</h3>
            <p className="music-now-playing-current-meta">
              <span>{current.artist || "Unknown artist"}</span>
              {current.album && (
                <>
                  <span aria-hidden="true"> · </span>
                  {current.albumID && player.canManageSettings !== false ? (
                    <Link to={`/music/albums/${current.albumID}`}>
                      {current.album}
                    </Link>
                  ) : (
                    <span>{current.album}</span>
                  )}
                </>
              )}
            </p>
          </div>
        </section>
      ) : player.queue.length ? (
        <div className="music-now-playing-no-selection">
          <Disc3 size={28} aria-hidden="true" />
          <div>
            <strong>No track selected</strong>
            <p>Choose Play on a queued track to start listening.</p>
          </div>
        </div>
      ) : (
        <div className="music-now-playing-empty">
          <Disc3 size={38} strokeWidth={1.25} aria-hidden="true" />
          <h3>Your queue is empty</h3>
          <p>
            Add music to start listening. The browser queue is saved on this
            device.
          </p>
        </div>
      )}

      {queueTruncated && (
        <p className="music-now-playing-window-status" role="status">
          Showing positions {queueWindow.start + 1}–{queueWindow.end} of{" "}
          {player.queue.length}
        </p>
      )}

      {player.queue.length > 0 && (
        <ol className="music-now-playing-queue" aria-label="Playback queue">
          {queueWindow.tracks.map((track, visibleIndex) => {
            const itemIndex = queueWindow.start + visibleIndex;
            const isCurrent = itemIndex === player.index;
            const duration = formatTrackDuration(track.durationMS);
            return (
              <li
                className={`music-now-playing-row${
                  isCurrent ? " is-current" : ""
                }`}
                key={`${track.fileID}-${itemIndex}`}
                aria-current={isCurrent ? "true" : undefined}
              >
                <span
                  className="music-now-playing-index"
                  aria-label={`Queue position ${itemIndex + 1}`}
                >
                  {itemIndex + 1}
                </span>
                <TrackCover track={track} size="queue" />
                <div className="music-now-playing-row-copy">
                  <span
                    className="music-now-playing-row-title"
                    title={track.title}
                  >
                    {isCurrent && (
                      <span className="music-now-playing-sr-only">
                        Current track:{" "}
                      </span>
                    )}
                    {track.title}
                  </span>
                  <span
                    className="music-now-playing-row-artist"
                    title={track.artist}
                  >
                    {track.artist || "Unknown artist"}
                  </span>
                </div>
                <span className="music-now-playing-duration">{duration}</span>
                <div className="music-now-playing-row-actions">
                  <Button
                    type="button"
                    size="1"
                    variant="ghost"
                    color="gray"
                    className="music-now-playing-icon-button"
                    aria-label={`Play ${track.title}`}
                    title={`Play ${track.title}`}
                    onClick={() => player.playAt(itemIndex)}
                  >
                    <Play size={14} aria-hidden="true" />
                  </Button>
                  <Button
                    type="button"
                    size="1"
                    variant="ghost"
                    color="gray"
                    className="music-now-playing-icon-button"
                    aria-label={`Move ${track.title} up`}
                    title={`Move ${track.title} up`}
                    disabled={itemIndex === 0}
                    onClick={() =>
                      player.moveQueueItem(itemIndex, itemIndex - 1)
                    }
                  >
                    <ArrowUp size={14} aria-hidden="true" />
                  </Button>
                  <Button
                    type="button"
                    size="1"
                    variant="ghost"
                    color="gray"
                    className="music-now-playing-icon-button"
                    aria-label={`Move ${track.title} down`}
                    title={`Move ${track.title} down`}
                    disabled={itemIndex === player.queue.length - 1}
                    onClick={() =>
                      player.moveQueueItem(itemIndex, itemIndex + 1)
                    }
                  >
                    <ArrowDown size={14} aria-hidden="true" />
                  </Button>
                  <Button
                    type="button"
                    size="1"
                    variant="ghost"
                    color="gray"
                    className="music-now-playing-icon-button"
                    aria-label={`Remove ${track.title}`}
                    title={`Remove ${track.title}`}
                    onClick={() => player.removeAt(itemIndex)}
                  >
                    <X size={14} aria-hidden="true" />
                  </Button>
                </div>
              </li>
            );
          })}
        </ol>
      )}

      <footer className="music-now-playing-footer">
        <p className="music-now-playing-summary">
          {upNextCount} {upNextCount === 1 ? "track" : "tracks"} up next
          {upNextTime ? ` · ${upNextTime}` : ""}
        </p>
        <Button
          type="button"
          size="1"
          variant="soft"
          color="gray"
          disabled={!player.queue.length}
          onClick={player.clearQueue}
        >
          <Trash2 size={13} aria-hidden="true" />
          Clear queue
        </Button>
      </footer>
    </aside>
  );
}
