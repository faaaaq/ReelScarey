import { useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor } from "../../api";
import { RequestState } from "../../components";
import { ConfirmDialog, Label } from "../../ui";
import { useResource } from "../../useResource";
import { EpisodeTable } from "./EpisodeTable";
import { mappingLabel } from "./status";
import type {
  MatchPreview,
  SourceDraft,
  SeriesSource,
  SourcePreview,
} from "./types";
import "../Admin.css";

const empty: SourceDraft = {
  source_url: "",
  mapping: "episode",
  season_number: 0,
  start_episode: 1,
  quality: "best",
  auto_import: true,
  interval_hours: 6,
};

function ManualMatches({
  seriesID,
  source,
  reload,
}: {
  seriesID: number;
  source: SeriesSource;
  reload: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [preview, setPreview] = useState<MatchPreview>();
  const [selection, setSelection] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [queue, setQueue] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const assigned = source.matches?.length ?? 0;

  async function load() {
    setLoading(true);
    setError("");
    setNotice("");
    try {
      const data = await api<MatchPreview>(
        `/library/series/${seriesID}/youtube-sources/${source.id}/matches`,
        { signal: AbortSignal.timeout(60000) },
      );
      setPreview(data);
      const current: Record<string, number> = {};
      for (const video of data.videos) {
        current[video.video_id] = video.episode_id ?? 0;
      }
      setSelection(current);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setLoading(false);
    }
  }

  async function save() {
    if (!preview) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const matches = preview.videos
        .filter((video) => (selection[video.video_id] ?? 0) > 0)
        .map((video) => ({
          video_id: video.video_id,
          episode_id: selection[video.video_id],
          title: video.title,
          channel: video.channel,
        }));
      await api(
        `/library/series/${seriesID}/youtube-sources/${source.id}/matches`,
        {
          method: "PUT",
          body: JSON.stringify({
            revision: source.revision,
            queue,
            matches,
          }),
        },
      );
      const noun = matches.length === 1 ? "video" : "videos";
      setNotice(
        matches.length === 0
          ? "No videos are matched."
          : queue
            ? `${matches.length} ${noun} matched; the matching episodes will be queued.`
            : `${matches.length} ${noun} matched.`,
      );
      reload();
      await load();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div>
      <div className="rs-form-actions">
        <Button
          size="1"
          variant="soft"
          disabled={loading}
          onClick={() => {
            if (open) {
              setOpen(false);
            } else {
              setOpen(true);
              void load();
            }
          }}
        >
          {assigned ? `Match videos (${assigned})` : "Match videos"}
        </Button>
        {open && (
          <Button
            size="1"
            variant="ghost"
            disabled={loading || saving}
            onClick={() => void load()}
          >
            {loading ? "Loading…" : "Reload videos"}
          </Button>
        )}
      </div>
      {error && (
        <p role="alert" className="rs-alert is-danger">
          {error}
        </p>
      )}
      {notice && <p className="rs-alert is-success">{notice}</p>}
      {open && loading && !preview && (
        <p className="rs-form-help">Reading the playlist…</p>
      )}
      {open && preview && (
        <>
          <p className="rs-form-help">
            {preview.title} · {preview.videos.length} videos ·{" "}
            {preview.episodes.length} episodes. Choose the episode each video
            belongs to. Your picks override monitoring; an episode that already
            has a file is replaced after the reviewed swap. Unassigned videos
            are ignored, and an episode with an active download stays blocked.
          </p>
          <ul className="yt-source-entries">
            {preview.videos.map((video) => (
              <li key={video.video_id}>
                <div className="yt-source-head">
                  <div>
                    <strong title={video.title}>{video.title}</strong>
                    <span>{video.label || "Not matched"}</span>
                  </div>
                  <select
                    aria-label={`Episode for ${video.title}`}
                    value={String(selection[video.video_id] ?? 0)}
                    onChange={(e) =>
                      setSelection({
                        ...selection,
                        [video.video_id]: Number(e.target.value),
                      })
                    }
                  >
                    <option value="0">Not matched</option>
                    {preview.episodes.map((episode) => (
                      <option key={episode.id} value={String(episode.id)}>
                        {episode.label}
                        {episode.files > 0
                          ? " — replaces the existing file (review)"
                          : ""}
                        {episode.active > 0
                          ? " — blocked: a download is active"
                          : ""}
                      </option>
                    ))}
                  </select>
                </div>
                {video.note && <p className="rs-form-help">{video.note}</p>}
              </li>
            ))}
          </ul>
          <label className="rs-check">
            <input
              type="checkbox"
              checked={queue}
              onChange={(e) => setQueue(e.target.checked)}
            />{" "}
            Queue the matched, monitored, missing episodes now
          </label>
          <div className="rs-form-actions">
            <Button size="1" disabled={saving} onClick={() => void save()}>
              {saving ? "Saving…" : "Save matches"}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

/** Full source body for create/update; every mutation is explicit and reviewed. */
function sourceBody(source: SeriesSource) {
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

/**
 * Draft form for a new per-show source: preview the mapping, then save it
 * paused so the owner can review before anything queues.
 */
export function SourceDraftForm({
  seriesID,
  initialURL = "",
  onSaved,
  onCancel,
}: {
  seriesID: number;
  initialURL?: string;
  onSaved: () => void;
  onCancel?: () => void;
}) {
  const [draft, setDraft] = useState<SourceDraft>({
    ...empty,
    source_url: initialURL,
  });
  const [preview, setPreview] = useState<SourcePreview>();
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

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
        await api<SourcePreview>(
          `/library/series/${seriesID}/youtube-sources/preview`,
          {
            method: "POST",
            body: JSON.stringify(draft),
            signal: AbortSignal.timeout(60000),
          },
        ),
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }
  async function save() {
    if (draft.mapping !== "manual" && !preview) {
      setError("Preview the mapping first.");
      return;
    }
    setBusy("save");
    setError("");
    try {
      await api(`/library/series/${seriesID}/youtube-sources`, {
        method: "POST",
        body: JSON.stringify({
          ...draft,
          subtitles: {
            languages: [],
            automatic: false,
            quality: draft.quality,
          },
          enabled: false,
          revision: 0,
          reviewed: true,
        }),
      });
      // The parent owns the saved notice: it knows where the source landed.
      onSaved();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="yt-source-form">
      {error && (
        <p role="alert" className="rs-alert is-danger">
          {error}
        </p>
      )}
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
              mapping: e.target.value as SourceDraft["mapping"],
            });
            setPreview(undefined);
          }}
        >
          <option value="episode">Episode number in title (S01E02)</option>
          <option value="order">Playlist order</option>
          <option value="manual">Pick each video to an episode</option>
        </select>
      </label>
      {draft.mapping !== "manual" && (
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
      )}
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
              quality: e.target.value as SourceDraft["quality"],
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
        {draft.mapping === "manual" ? (
          <span className="rs-form-help">
            Save the source, then pick each video's episode on the source card.
          </span>
        ) : (
          <Button
            variant="soft"
            disabled={!!busy || !draft.source_url.trim()}
            onClick={previewDraft}
          >
            {busy === "preview" ? "Checking…" : "Preview matches"}
          </Button>
        )}
        <Button
          disabled={!!busy || (draft.mapping !== "manual" && !preview)}
          onClick={() => void save()}
        >
          {busy === "save" ? "Saving…" : "Save source"}
        </Button>
        {onCancel && (
          <Button
            variant="ghost"
            onClick={() => {
              onCancel();
            }}
          >
            Cancel
          </Button>
        )}
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
                <Label kind={entry.state === "ready" ? "success" : "default"}>
                  {entry.state === "ready" ? "Ready" : entry.note || "Skipped"}
                </Label>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

/** One saved per-show source with its actions and episode table. */
export function SourceCard({
  seriesID,
  source,
  onChanged,
}: {
  seriesID: number;
  source: SeriesSource;
  onChanged: () => void;
}) {
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
      onChanged();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }

  return (
    <li>
      <div className="yt-source-head">
        <div>
          <strong>{mappingLabel(source.mapping, source.season_number)}</strong>
          <span>{source.source_url}</span>
        </div>
        <Label kind={source.enabled ? "success" : "disabled"}>
          {source.enabled ? "Enabled" : "Paused"}
        </Label>
      </div>
      <p className="rs-form-help">
        {source.quality === "best" ? "Best" : `${source.quality}p`} · every{" "}
        {source.interval_hours}h ·{" "}
        {source.auto_import ? "auto-import exact matches" : "review imports"}
        {source.last_checked
          ? ` · checked ${new Date(source.last_checked * 1000).toLocaleString()}`
          : ""}
        {source.message ? ` · ${source.message}` : ""}
      </p>
      {error && (
        <p role="alert" className="rs-alert is-danger">
          {error}
        </p>
      )}
      {notice && <p className="rs-alert is-success">{notice}</p>}
      <div className="rs-form-actions yt-row-actions">
        <Button
          size="1"
          variant="soft"
          disabled={!!busy}
          onClick={() =>
            run(
              `enable-${source.id}`,
              () =>
                api(`/library/series/${seriesID}/youtube-sources`, {
                  method: "POST",
                  body: JSON.stringify({
                    ...sourceBody(source),
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
                  `/library/series/${seriesID}/youtube-sources/${source.id}`,
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
          <ConfirmDialog
            trigger={
              <Button size="1" variant="soft" disabled={!!busy}>
                Fill missing
              </Button>
            }
            title="Queue matching videos from the initial baseline?"
            description="Enable the source to keep checking automatically."
            onConfirm={() =>
              void run(
                `backfill-${source.id}`,
                () =>
                  api(
                    `/library/series/${seriesID}/youtube-sources/${source.id}`,
                    {
                      method: "POST",
                      body: JSON.stringify({
                        revision: source.revision,
                        action: "backfill",
                      }),
                    },
                  ),
                "Match request queued.",
              )
            }
          />
        )}
        <ConfirmDialog
          trigger={
            <Button
              size="1"
              variant="soft"
              className="rs-btn-danger"
              disabled={!!busy}
            >
              Remove
            </Button>
          }
          title="Remove this YouTube source?"
          onConfirm={() =>
            void run(
              `remove-${source.id}`,
              () =>
                api(
                  `/library/series/${seriesID}/youtube-sources/${source.id}?revision=${source.revision}`,
                  { method: "DELETE" },
                ),
              "Source removed.",
            )
          }
        />
      </div>
      {source.mapping === "manual" && (
        <ManualMatches seriesID={seriesID} source={source} reload={onChanged} />
      )}
      <EpisodeTable
        sourceID={source.id}
        revision={source.revision}
        onChanged={onChanged}
      />
    </li>
  );
}

/** All per-show YouTube sources for one show, with the add-source draft form. */
export function SeriesSources({ id }: { id: number }) {
  const sources = useResource<{ items: SeriesSource[] }>(
    `/library/series/${id}/youtube-sources`,
    // Retain the last list while refreshing: a queue action reloads this
    // resource, and clearing it would remount the episode table and erase its
    // action notice before the owner can read it.
    { retain: true },
  );
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState("");

  return (
    <section className="rs-panel rs-stack">
      <div className="rs-panel-header">
        <h2>YouTube source</h2>
        {!editing && (
          <Button
            variant="soft"
            disabled={(sources.data?.items.length ?? 0) >= 4}
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
        code={sources.code}
        retry={sources.reload}
      />
      {notice && <p className="rs-alert is-success">{notice}</p>}
      {editing && (
        <SourceDraftForm
          seriesID={id}
          onSaved={() => {
            sources.reload();
            setEditing(false);
            setNotice("Saved paused. Enable it after reviewing the source.");
          }}
          onCancel={() => setEditing(false)}
        />
      )}
      {(sources.data?.items.length ?? 0) === 0 && !editing && (
        <p className="rs-form-help">No YouTube source for this show.</p>
      )}
      <ul className="yt-source-list">
        {sources.data?.items.map((source) => (
          <SourceCard
            key={source.id}
            seriesID={id}
            source={source}
            onChanged={sources.reload}
          />
        ))}
      </ul>
    </section>
  );
}
