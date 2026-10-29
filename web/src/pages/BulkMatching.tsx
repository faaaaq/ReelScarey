import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor, type MediaItem, type TVSeries } from "../api";
import type { DiscoveredFile } from "./ExistingLibrary";
type Candidate = {
  media_id: number;
  tmdb_id: number;
  title: string;
  year: number | null;
  source: string;
  score: number;
  reasons: string[];
  episode_ids: number[] | null;
};
type Suggestion = {
  file: DiscoveredFile;
  candidates: Candidate[];
  message: string;
};
type Review = {
  review_id: string;
  expires_at: number;
  items: {
    choice: { file_id: string; media_id: number; episode_ids: number[] };
    file: DiscoveredFile;
    title: string;
    year: number;
    episodes: {
      episode_id: number;
      season: number;
      number: number;
      title: string;
    }[];
  }[];
};
export function BulkMatching({
  scanID,
  kind,
  files,
  reload,
  match,
}: {
  scanID: string;
  kind: "movie" | "series";
  files: DiscoveredFile[];
  reload: () => void;
  match: (file: DiscoveredFile) => void;
}) {
  const [open, setOpen] = useState(false),
    [selected, setSelected] = useState<string[]>([]),
    [remote, setRemote] = useState(false),
    [suggestions, setSuggestions] = useState<Suggestion[]>([]),
    [choices, setChoices] = useState<Record<string, number>>({}),
    [review, setReview] = useState<Review>(),
    [checked, setChecked] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const eligible = files.filter((v) => ["review", "blocked"].includes(v.state)),
    base = `/library/imports/${scanID}/matches`;
  async function perform(task: () => Promise<void>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await task();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function suggest() {
    const result = await api<{ items: Suggestion[] }>(base + "/suggest", {
      method: "POST",
      body: JSON.stringify({ file_ids: selected, include_provider: remote }),
      signal: AbortSignal.timeout(100000),
    });
    setSuggestions(result.items);
    setChoices({});
    setReview(undefined);
  }
  async function add(file: DiscoveredFile, c: Candidate) {
    await perform(async () => {
      const result = await api<{ item: MediaItem }>(
        kind === "movie" ? "/library/movies" : "/library/series",
        {
          method: "POST",
          body: JSON.stringify({ tmdb_id: c.tmdb_id, monitored: false }),
        },
      );
      if (kind === "series") {
        const tokens = [
          ...file.path.matchAll(
            /S([0-9]{1,4})E[0-9]{1,5}|([0-9]{1,4})x[0-9]{1,5}/gi,
          ),
        ];
        if (tokens.length === 1) {
          const n = Number(tokens[0][1] ?? tokens[0][2]);
          const show = await api<TVSeries>(`/library/series/${result.item.id}`);
          if (show.seasons.some((s) => s.number === n)) {
            await api(`/library/series/${show.id}/seasons/${n}/refresh`, {
              method: "POST",
              body: JSON.stringify({ revision: show.revision }),
            });
          }
        }
      }
      await suggest();
      setNotice(
        "Title added with monitoring paused. Review the refreshed matches before registration.",
      );
    });
  }
  return (
    <section className="panel system-panel" aria-label="Bulk metadata matching">
      <Button variant="soft" onClick={() => setOpen(!open)}>
        {open ? "Hide bulk matching" : "Match multiple files"}
      </Button>
      {open && (
        <>
          <h3>Batch matching suggestions</h3>
          <p>
            Select up to 25 files on this page. Suggestions never register files
            automatically. Choose each title and its canonical episodes, then
            review the complete batch. Existing files stay in place and pass
            verification before registration.
          </p>
          <fieldset className="backup-fields" disabled={busy || !!review}>
            <div className="bulk-video-list">
              {eligible.map((file) => (
                <label key={file.id}>
                  <input
                    type="checkbox"
                    checked={selected.includes(file.id)}
                    disabled={
                      selected.length >= 25 && !selected.includes(file.id)
                    }
                    onChange={(e) => {
                      setSelected(
                        e.target.checked
                          ? [...selected, file.id]
                          : selected.filter((v) => v !== file.id),
                      );
                      setSuggestions([]);
                    }}
                  />
                  {file.path}
                </label>
              ))}
            </div>
            <label>
              <input
                type="checkbox"
                checked={remote}
                onChange={(e) => setRemote(e.target.checked)}
              />{" "}
              Include TMDb suggestions (at most five distinct provider searches
              per batch)
            </label>
            <Button
              disabled={!selected.length}
              onClick={() => void perform(suggest)}
            >
              Find batch suggestions
            </Button>
            {suggestions.map((s) => (
              <article className="system-panel" key={s.file.id}>
                <h4>{s.file.path}</h4>
                <p>{s.message}</p>
                <label>
                  Canonical title
                  <select
                    value={choices[s.file.id] ?? ""}
                    onChange={(e) => {
                      setChoices({
                        ...choices,
                        [s.file.id]: Number(e.target.value),
                      });
                      setReview(undefined);
                    }}
                  >
                    <option value="">Choose a match explicitly</option>
                    {s.candidates.map((c, index) => (
                      <option
                        key={`${c.source}-${c.tmdb_id}`}
                        value={index}
                        disabled={
                          !c.media_id ||
                          (kind === "series" && !c.episode_ids?.length)
                        }
                      >
                        {c.title} {c.year ? `(${c.year})` : ""} · {c.source}{" "}
                        {kind === "series" && c.episode_ids?.length
                          ? ` · ${c.episode_ids.length} episode(s)`
                          : ""}
                        {!c.media_id ? " · add paused title first" : ""}
                      </option>
                    ))}
                  </select>
                </label>
                {s.candidates
                  .filter((c) => !c.media_id)
                  .map((c) => (
                    <div className="backup-row" key={c.tmdb_id}>
                      <span>
                        {c.title} ({c.year ?? "Year unknown"}) · TMDb{" "}
                        {c.tmdb_id}
                      </span>
                      <Button
                        variant="soft"
                        onClick={() => void add(s.file, c)}
                      >
                        Add paused title
                      </Button>
                    </div>
                  ))}
                <Button variant="ghost" onClick={() => match(s.file)}>
                  Open individual match and episode review
                </Button>
              </article>
            ))}
            <Button
              disabled={
                !suggestions.some((s) => choices[s.file.id] !== undefined)
              }
              onClick={() =>
                void perform(async () => {
                  const items = suggestions
                    .filter((s) => choices[s.file.id] !== undefined)
                    .map((s) => {
                      const c = s.candidates[choices[s.file.id]];
                      return {
                        file_id: s.file.id,
                        media_id: c.media_id,
                        episode_ids: c.episode_ids ?? [],
                      };
                    });
                  setReview(
                    await api<Review>(base + "/review", {
                      method: "POST",
                      body: JSON.stringify({ items }),
                    }),
                  );
                  setChecked(false);
                })
              }
            >
              Review chosen file matches
            </Button>
          </fieldset>
          {review && (
            <section>
              <h3>Review registration batch</h3>
              <ul>
                {review.items.map((v) => (
                  <li key={v.choice.file_id}>
                    {v.file.path} → {v.title} ({v.year || "Year unknown"}){" "}
                    {v.episodes
                      .map((e) => `S${e.season}E${e.number} ${e.title}`)
                      .join(", ")}
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
                I reviewed every file, canonical title and episode in this
                batch.
              </label>
              <div className="button-row">
                <Button
                  variant="soft"
                  disabled={busy}
                  onClick={() => setReview(undefined)}
                >
                  Cancel registration review
                </Button>
                <Button
                  disabled={busy || !checked}
                  onClick={() =>
                    void perform(async () => {
                      await api(base + "/apply", {
                        method: "POST",
                        body: JSON.stringify({
                          review_id: review.review_id,
                          reviewed: true,
                        }),
                      });
                      setReview(undefined);
                      setSuggestions([]);
                      setSelected([]);
                      setNotice(
                        "The reviewed batch is queued for file verification. Existing paths and sources are preserved.",
                      );
                      reload();
                    })
                  }
                >
                  Register reviewed batch
                </Button>
              </div>
            </section>
          )}
          {error && (
            <p role="alert" className="error-box">
              {error}
            </p>
          )}
          {notice && <p role="status">{notice}</p>}
        </>
      )}
    </section>
  );
}
