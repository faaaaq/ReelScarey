import { useState } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";
import "./Admin.css";

type Entry = {
  video_id: string;
  title: string;
  channel: string;
  position: number;
  state: string;
  episode_id?: number;
  job_id?: string;
  note: string;
};
type Source = {
  id: string;
  media_id: number;
  source_url: string;
  title: string;
  mapping: "episode" | "order";
  season_number: number;
  start_episode: number;
  quality: string;
  subtitles: { languages: string[]; automatic: boolean; quality?: string };
  auto_import: boolean;
  enabled: boolean;
  interval_hours: number;
  initialized: boolean;
  revision: number;
  requested: boolean;
  next_run: number;
  last_checked: number;
  state: string;
  message: string;
  created_at: number;
  entries?: Entry[];
};
type Preview = {
  title: string;
  channel: string;
  truncated: boolean;
  entries: {
    video: { id: string; title: string; channel: string };
    position: number;
    season_number: number;
    episode_number: number;
    label: string;
    state: string;
    note: string;
  }[];
};
type Draft = {
  source_url: string;
  mapping: "episode" | "order";
  season_number: number;
  start_episode: number;
  quality: "best" | "1080" | "720";
  auto_import: boolean;
  interval_hours: number;
};
const empty: Draft = {
  source_url: "",
  mapping: "episode",
  season_number: 0,
  start_episode: 1,
  quality: "best",
  auto_import: true,
  interval_hours: 6,
};

export function SeriesYouTubeSource({ id }: { id: number }) {
  const sources = useResource<{ items: Source[] }>(
    `/library/series/${id}/youtube-sources`,
  );
  const [draft, setDraft] = useState<Draft>(empty);
  const [preview, setPreview] = useState<Preview>();
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function run(
    operation: string,
    task: () => Promise<unknown>,
    done?: string,
  ) {
    if (busy) return;
    setBusy(operation);
    setError("");
    setNotice("");
    try {
      await task();
      if (done) setNotice(done);
      sources.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }
  function body(source?: Source) {
    if (source) {
      return {
        source_url: source.source_url,
        mapping: source.mapping,
        season_number: source.season_number,
        start_episode: source.start_episode,
        quality: source.quality,
        subtitles: { ...source.subtitles, quality: source.quality },
        auto_import: source.auto_import,
        interval_hours: source.interval_hours,
        enabled: source.enabled,
        revision: source.revision,
        reviewed: true,
      };
    }
    return {
      ...draft,
      subtitles: { languages: [], automatic: false, quality: draft.quality },
      enabled: false,
      revision: 0,
      reviewed: true,
    };
  }
  async function previewDraft() {
    if (draft.mapping === "order" && draft.season_number < 1) {
      setError("Playlist order needs a season number.");
      return;
    }
    setBusy("preview");
    setError("");
    setPreview(undefined);
    try {
      setPreview(
        await api<Preview>(`/library/series/${id}/youtube-sources/preview`, {
          method: "POST",
          body: JSON.stringify(draft),
          signal: AbortSignal.timeout(60000),
        }),
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }
  function save() {
    if (!preview) {
      setError("Preview the mapping first.");
      return;
    }
    void run(
      "save",
      () =>
        api(`/library/series/${id}/youtube-sources`, {
          method: "POST",
          body: JSON.stringify(body()),
        }),
      "Saved paused. Enable it after reviewing the source.",
    );
    setEditing(false);
    setDraft(empty);
    setPreview(undefined);
  }

  return (
    <section className="rs-panel rs-stack">
      <div className="rs-panel-header">
        <h2>YouTube source</h2>
        {!editing && (
          <Button
            variant="soft"
            disabled={!!busy || (sources.data?.items.length ?? 0) >= 4}
            onClick={() => {
              setEditing(true);
              setNotice("");
            }}
          >
            Add source
          </Button>
        )}
      </div>
      <RequestState
        loading={sources.loading}
        error={sources.error}
        retry={sources.reload}
      />
      {error && (
        <p role="alert" className="rs-alert is-danger">
          {error}
        </p>
      )}
      {notice && <p className="rs-alert is-success">{notice}</p>}

      {editing && (
        <div className="yt-source-form">
          <label>
            Channel or playlist URL
            <TextField.Root
              value={draft.source_url}
              onChange={(e) => {
                setDraft({ ...draft, source_url: e.target.value });
                setPreview(undefined);
              }}
              placeholder="https://www.youtube.com/playlist?list=…"
            />
          </label>
          <label>
            Mapping
            <select
              value={draft.mapping}
              onChange={(e) => {
                setDraft({
                  ...draft,
                  mapping: e.target.value as Draft["mapping"],
                });
                setPreview(undefined);
              }}
            >
              <option value="episode">Episode number in title (S01E02)</option>
              <option value="order">Playlist order</option>
            </select>
          </label>
          <label>
            Season
            <TextField.Root
              type="number"
              min={draft.mapping === "order" ? 1 : 0}
              value={draft.season_number}
              onChange={(e) => {
                setDraft({ ...draft, season_number: Number(e.target.value) });
                setPreview(undefined);
              }}
            />
          </label>
          {draft.mapping === "order" && (
            <label>
              First episode
              <TextField.Root
                type="number"
                min={1}
                value={draft.start_episode}
                onChange={(e) => {
                  setDraft({ ...draft, start_episode: Number(e.target.value) });
                  setPreview(undefined);
                }}
              />
            </label>
          )}
          <label>
            Quality
            <select
              value={draft.quality}
              onChange={(e) => {
                setDraft({
                  ...draft,
                  quality: e.target.value as Draft["quality"],
                });
                setPreview(undefined);
              }}
            >
              <option value="best">Best</option>
              <option value="1080">1080p</option>
              <option value="720">720p</option>
            </select>
          </label>
          <label>
            Check every (hours)
            <TextField.Root
              type="number"
              min={1}
              max={168}
              value={draft.interval_hours}
              onChange={(e) =>
                setDraft({ ...draft, interval_hours: Number(e.target.value) })
              }
            />
          </label>
          <label className="rs-check">
            <input
              type="checkbox"
              checked={draft.auto_import}
              onChange={(e) =>
                setDraft({ ...draft, auto_import: e.target.checked })
              }
            />{" "}
            Import verified exact matches without review
          </label>
          <div className="rs-form-actions">
            <Button
              variant="soft"
              disabled={!!busy || !draft.source_url.trim()}
              onClick={previewDraft}
            >
              {busy === "preview" ? "Checking…" : "Preview matches"}
            </Button>
            <Button disabled={!!busy || !preview} onClick={save}>
              Save source
            </Button>
            <Button
              variant="ghost"
              onClick={() => {
                setEditing(false);
                setPreview(undefined);
                setDraft(empty);
              }}
            >
              Cancel
            </Button>
          </div>
          {preview && (
            <div className="yt-source-preview">
              <strong>
                {preview.title} · {preview.entries.length} videos
              </strong>
              <ul>
                {preview.entries.map((entry) => (
                  <li key={entry.video.id}>
                    <span title={entry.video.title}>{entry.video.title}</span>
                    <span>
                      {entry.label ||
                        (entry.episode_number
                          ? `S${String(entry.season_number).padStart(2, "0")}E${String(entry.episode_number).padStart(2, "0")}`
                          : "No episode")}
                    </span>
                    <Badge color={entry.state === "ready" ? "green" : "gray"}>
                      {entry.state === "ready"
                        ? "Ready"
                        : entry.note || "Skipped"}
                    </Badge>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      {(sources.data?.items.length ?? 0) === 0 && !editing && (
        <p className="rs-form-help">No YouTube source for this show.</p>
      )}
      <ul className="yt-source-list">
        {sources.data?.items.map((source) => (
          <li key={source.id}>
            <div className="yt-source-head">
              <div>
                <strong>
                  {source.mapping === "order"
                    ? `Playlist order · Season ${source.season_number}`
                    : `Episode number${source.season_number ? ` · Season ${source.season_number}` : ""}`}
                </strong>
                <span>{source.source_url}</span>
              </div>
              <Badge color={source.enabled ? "green" : "gray"}>
                {source.enabled ? "Enabled" : "Paused"}
              </Badge>
            </div>
            <p className="rs-form-help">
              {source.quality === "best" ? "Best" : `${source.quality}p`} ·
              every {source.interval_hours}h ·{" "}
              {source.auto_import
                ? "auto-import exact matches"
                : "review imports"}
              {source.last_checked
                ? ` · checked ${new Date(source.last_checked * 1000).toLocaleString()}`
                : ""}
              {source.message ? ` · ${source.message}` : ""}
            </p>
            <div className="rs-form-actions">
              <Button
                size="1"
                variant="soft"
                disabled={!!busy}
                onClick={() =>
                  run(
                    `enable-${source.id}`,
                    () =>
                      api(`/library/series/${id}/youtube-sources`, {
                        method: "POST",
                        body: JSON.stringify({
                          ...body(source),
                          enabled: !source.enabled,
                        }),
                      }),
                    source.enabled ? "Source paused." : "Source enabled.",
                  )
                }
              >
                {source.enabled ? "Pause" : "Enable"}
              </Button>
              <Button
                size="1"
                variant="soft"
                disabled={!!busy || source.requested}
                onClick={() =>
                  run(
                    `check-${source.id}`,
                    () =>
                      api(
                        `/library/series/${id}/youtube-sources/${source.id}`,
                        {
                          method: "POST",
                          body: JSON.stringify({ revision: source.revision }),
                        },
                      ),
                    "Check requested.",
                  )
                }
              >
                Check now
              </Button>
              {!source.enabled && (
                <Button
                  size="1"
                  variant="soft"
                  disabled={!!busy}
                  onClick={() => {
                    if (
                      !window.confirm(
                        "Queue matching videos from the initial baseline? Enable the source to keep checking automatically.",
                      )
                    )
                      return;
                    void run(
                      `backfill-${source.id}`,
                      () =>
                        api(
                          `/library/series/${id}/youtube-sources/${source.id}`,
                          {
                            method: "POST",
                            body: JSON.stringify({
                              revision: source.revision,
                              action: "backfill",
                            }),
                          },
                        ),
                      "Match request queued.",
                    );
                  }}
                >
                  Fill missing
                </Button>
              )}
              <Button
                size="1"
                variant="soft"
                color="red"
                disabled={!!busy}
                onClick={() => {
                  if (!window.confirm("Remove this YouTube source?")) return;
                  void run(
                    `remove-${source.id}`,
                    () =>
                      api(
                        `/library/series/${id}/youtube-sources/${source.id}?revision=${source.revision}`,
                        { method: "DELETE" },
                      ),
                    "Source removed.",
                  );
                }}
              >
                Remove
              </Button>
            </div>
            {(source.entries?.length ?? 0) > 0 && (
              <details>
                <summary>{source.entries?.length} discovered videos</summary>
                <ul className="yt-source-entries">
                  {source.entries?.map((entry) => (
                    <li key={entry.video_id}>
                      <span title={entry.title}>{entry.title}</span>
                      <span>{entry.note || entry.state}</span>
                    </li>
                  ))}
                </ul>
              </details>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
