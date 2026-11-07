import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import "./Admin.css";
export type BulkEntry = { id: string; title: string; kind?: string };
type Review = {
  id: string;
  video: { id: string; title: string };
  target_label: string;
  expires_at: number;
};
export function YouTubeBulk({
  url,
  entries,
  collectionID,
  subtitles,
  reload,
}: {
  url: string;
  entries: BulkEntry[];
  collectionID: number;
  subtitles: { languages: string[]; automatic: boolean };
  reload: () => void;
}) {
  const [selected, setSelected] = useState<string[]>([]),
    [reviews, setReviews] = useState<Review[]>([]),
    [checked, setChecked] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  async function action(queue = false) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      if (queue) {
        await api("/youtube/bulk/queue", {
          method: "POST",
          body: JSON.stringify({
            review_ids: reviews.map((v) => v.id),
            reviewed: true,
          }),
        });
        setReviews([]);
        setSelected([]);
        setNotice(
          "The reviewed videos were queued. Review verified imports in YouTube activity before publishing.",
        );
        reload();
      } else {
        const result = await api<{ items: Review[] }>("/youtube/bulk/review", {
          method: "POST",
          body: JSON.stringify({
            source_url: url,
            video_ids: selected,
            target: { kind: "custom", collection_id: collectionID },
            subtitles,
          }),
          signal: AbortSignal.timeout(40000),
        });
        setReviews(result.items);
        setChecked(false);
      }
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      className="rs-panel rs-stack"
      aria-label="Bulk YouTube acquisition"
    >
      <h3>Acquire selected videos</h3>
      <p>
        Select up to 25 videos for the chosen custom collection. The complete
        reviewed selection queues together; destination conflicts refuse the
        whole batch. Verified files still require import review.
      </p>
      <fieldset className="backup-fields" disabled={busy || !!reviews.length}>
        <div className="bulk-video-list">
          {entries
            .filter((v) => v.kind !== "playlist")
            .map((v) => (
              <label key={v.id}>
                <input
                  type="checkbox"
                  checked={selected.includes(v.id)}
                  disabled={selected.length >= 25 && !selected.includes(v.id)}
                  onChange={(e) =>
                    setSelected(
                      e.target.checked
                        ? [...selected, v.id]
                        : selected.filter((id) => id !== v.id),
                    )
                  }
                />
                {v.title}
              </label>
            ))}
        </div>
        <Button
          disabled={!selected.length || !collectionID}
          onClick={() => void action()}
        >
          Review selected videos ({selected.length})
        </Button>
      </fieldset>
      {reviews.length > 0 && (
        <section>
          <h3>Review acquisition batch</h3>
          <ul>
            {reviews.map((v) => (
              <li key={v.id}>
                {v.video.title} → {v.target_label}
              </li>
            ))}
          </ul>
          <label>
            <input
              type="checkbox"
              disabled={busy}
              checked={checked}
              onChange={(e) => setChecked(e.target.checked)}
            />{" "}
            I reviewed every selected video and destination.
          </label>
          <div className="rs-form-actions">
            <Button
              variant="soft"
              disabled={busy}
              onClick={() => setReviews([])}
            >
              Cancel batch review
            </Button>
            <Button
              disabled={busy || !checked}
              onClick={() => void action(true)}
            >
              Queue reviewed videos
            </Button>
          </div>
        </section>
      )}
      {error && (
        <p role="alert" className="rs-alert is-danger">
          {error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
    </section>
  );
}
