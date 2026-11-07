import { PageBody } from "../ui";
import { Link } from "react-router-dom";
import { useMemo, useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import {
  api,
  messageFor,
  type LibraryPage,
  type TVSeries,
  type TVSeasonDetails,
} from "../api";
import { PageHeading, RequestState } from "../components";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";
import { YouTubeBulk } from "./YouTubeBulk";
import "./Admin.css";

type Entry = {
  kind: "video" | "playlist";
  id: string;
  title: string;
  channel: string;
  url: string;
  published: string;
  duration_seconds?: number;
  thumbnail?: string;
};
type Inspection = {
  source: { kind: string; id: string; url: string };
  title: string;
  channel: string;
  entries: Entry[];
  truncated: boolean;
};
type Collection = { id: number; title: string; revision: number };
type Target = {
  kind: "custom" | "movie" | "episode";
  collection_id?: number;
  media_id?: number;
  episode_id?: number;
};
type Review = {
  id: string;
  video: { id: string; title: string; channel: string };
  target_label: string;
  expires_at: number;
  subtitles: { languages: string[]; automatic: boolean; quality?: string };
};
type Job = {
  id: string;
  video: { id: string; title: string; channel: string };
  target_label: string;
  state: string;
  message: string;
  quality: string;
  progress: number;
  downloaded_bytes: number;
  total_bytes: number;
  speed_bps: number;
  eta_seconds: number;
  created_at: number;
};
type CustomFile = {
  job_id: string;
  title: string;
  collection: string;
  relative_path: string;
  size_bytes: number;
};
type ImportReview = {
  job_id: string;
  size_bytes: number;
  sha256: string;
  probe_json: string;
  destination: string;
  target_label: string;
  subtitles: {
    language: string;
    size_bytes: number;
    sha256: string;
    destination: string;
  }[];
};
type Candidate = {
  id: number;
  title: string;
  year?: number;
  tmdb_id: number;
  score: number;
};
type SeasonSummary = { number: number; episode_count: number; loaded: boolean };
type BatchEntry = {
  video_id: string;
  title: string;
  channel: string;
  duration_seconds: number;
  thumbnail: string;
  season_number: number;
  episode_number: number;
  episode_id?: number;
  episode_title?: string;
  status: "missing" | "available" | "active" | "unmatched";
  note: string;
  selected_default: boolean;
  duplicate: boolean;
};
type BatchPreview = {
  source_url: string;
  title: string;
  channel: string;
  truncated: boolean;
  needs_show: boolean;
  show?: Candidate;
  candidates: Candidate[];
  season_number: number;
  seasons: SeasonSummary[];
  entries: BatchEntry[];
};
type BatchResult = {
  video_id: string;
  episode_id: number;
  job_id?: string;
  state: string;
  message: string;
};
type Quality = "best" | "1080" | "720";
type Resource<T> = {
  data?: T;
  error: string;
  loading: boolean;
  reload: () => void;
};

function duration(seconds?: number) {
  if (!seconds) return "";
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (h)
    return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  return `${m}:${String(s).padStart(2, "0")}`;
}
function bytes(value: number) {
  if (!value) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB"];
  let v = value;
  let unit = 0;
  while (v >= 1024 && unit < units.length - 1) {
    v /= 1024;
    unit++;
  }
  return `${v.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}
function speed(value: number) {
  return value > 0 ? `${bytes(value)}/s` : "";
}
function eta(seconds: number) {
  if (!seconds || seconds >= 8640000) return "";
  if (seconds >= 3600)
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
  if (seconds >= 60) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  return `${seconds}s`;
}
function qualityLabel(quality: string) {
  return quality === "1080" ? "1080p" : quality === "720" ? "720p" : "Best";
}
function stateLabel(state: string) {
  const labels: Record<string, string> = {
    queued: "Queued",
    downloading: "Downloading",
    awaiting_review: "Ready",
    importing: "Importing",
    completed: "Imported",
    failed: "Failed",
    cancelled: "Cancelled",
  };
  return labels[state] ?? state;
}
function stateColor(
  state: string,
): "gray" | "green" | "amber" | "red" | "blue" {
  if (state === "completed") return "green";
  if (state === "awaiting_review" || state === "importing") return "blue";
  if (state === "failed" || state === "cancelled") return "red";
  if (state === "downloading") return "amber";
  return "gray";
}
function thumb(videoID: string) {
  return `https://i.ytimg.com/vi/${videoID}/mqdefault.jpg`;
}
function episodeLabel(entry: BatchEntry) {
  if (!entry.episode_number) return "—";
  return `S${String(entry.season_number).padStart(2, "0")}E${String(
    entry.episode_number,
  ).padStart(2, "0")}${entry.episode_title ? ` — ${entry.episode_title}` : ""}`;
}

export function YouTube() {
  const [mode, setMode] = useState<"single" | "playlist">("single");

  // Single-video state.
  const [url, setURL] = useState("");
  const [inspection, setInspection] = useState<Inspection>();
  const [selected, setSelected] = useState<Entry>();
  const [kind, setKind] = useState<Target["kind"]>("custom");
  const [collection, setCollection] = useState("");
  const [collectionTitle, setCollectionTitle] = useState("");
  const [query, setQuery] = useState("");
  const [searched, setSearched] = useState("");
  const [page, setPage] = useState(1);
  const [media, setMedia] = useState("");
  const [season, setSeason] = useState("");
  const [episode, setEpisode] = useState("");
  const [review, setReview] = useState<Review>();
  const [quality, setQuality] = useState<Quality>("best");
  const [subtitleLanguages, setSubtitleLanguages] = useState("en");
  const [automaticSubtitles, setAutomaticSubtitles] = useState(false);
  const [importReview, setImportReview] = useState<ImportReview>();

  // Playlist-to-series state.
  const [batchURL, setBatchURL] = useState("");
  const [batch, setBatch] = useState<BatchPreview>();
  const [batchShow, setBatchShow] = useState(0);
  const [batchSeason, setBatchSeason] = useState(0);
  const [batchQuality, setBatchQuality] = useState<Quality>("best");
  const [selection, setSelection] = useState<Set<string>>(new Set());
  const [replaceExisting, setReplaceExisting] = useState(false);
  const [keepSource, setKeepSource] = useState(false);
  const [batchResults, setBatchResults] = useState<BatchResult[]>([]);
  const [showQuery, setShowQuery] = useState("");

  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const collections = useResource<{ items: Collection[] }>(
    "/youtube/collections",
  );
  const jobs = useLiveResource<{ items: Job[] }>("/youtube/jobs", 5000);
  const files = useResource<{ items: CustomFile[] }>("/youtube/files");
  const library = useResource<LibraryPage>(
    kind !== "custom" && searched
      ? `/library?kind=${kind === "movie" ? "movie" : "series"}&page=${page}&query=${encodeURIComponent(searched)}`
      : null,
  );
  const show = useResource<TVSeries>(
    kind === "episode" && media ? `/library/series/${media}` : null,
  );
  const episodes = useResource<TVSeasonDetails>(
    kind === "episode" && media && season !== ""
      ? `/library/series/${media}/seasons/${season}`
      : null,
  );
  const showSearch = useResource<LibraryPage>(
    showQuery.trim()
      ? `/library?kind=series&page=1&query=${encodeURIComponent(showQuery.trim())}`
      : null,
  );

  function clearReview() {
    setReview(undefined);
    setNotice("");
  }
  async function run<T>(
    operation: string,
    task: () => Promise<T>,
    complete: (result: T) => void,
  ) {
    if (busy) return;
    setBusy(operation);
    setError("");
    try {
      complete(await task());
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }
  function inspect(value: string) {
    clearReview();
    setSelected(undefined);
    void run(
      "inspect",
      () =>
        api<Inspection>("/youtube/inspect", {
          method: "POST",
          body: JSON.stringify({ url: value }),
          signal: AbortSignal.timeout(60000),
        }),
      (result) => {
        setURL(result.source.url);
        setInspection(result);
        if (result.source.kind === "video") setSelected(result.entries[0]);
      },
    );
  }
  function createCollection(e: FormEvent) {
    e.preventDefault();
    void run(
      "collection",
      () =>
        api<Collection>("/youtube/collections", {
          method: "POST",
          body: JSON.stringify({ title: collectionTitle.trim() }),
        }),
      (result) => {
        setCollection(String(result.id));
        setCollectionTitle("");
        clearReview();
        collections.reload();
      },
    );
  }
  const target: Target | undefined = useMemo(
    () =>
      kind === "custom" && collection
        ? { kind, collection_id: Number(collection) }
        : kind === "movie" && media
          ? { kind, media_id: Number(media) }
          : kind === "episode" && media && episode
            ? { kind, media_id: Number(media), episode_id: Number(episode) }
            : undefined,
    [kind, collection, media, episode],
  );
  function preview() {
    if (!selected || !target) return;
    void run(
      "review",
      () =>
        api<Review>("/youtube/reviews", {
          method: "POST",
          body: JSON.stringify({
            video_id: selected.id,
            target,
            subtitles: {
              languages: subtitleLanguages
                .split(",")
                .map((v) => v.trim())
                .filter(Boolean),
              automatic: automaticSubtitles && !!subtitleLanguages.trim(),
              quality,
            },
          }),
          signal: AbortSignal.timeout(60000),
        }),
      setReview,
    );
  }
  function queue() {
    if (!review) return;
    void run(
      "queue",
      () =>
        api<Job>(`/youtube/reviews/${encodeURIComponent(review.id)}/queue`, {
          method: "POST",
          body: "{}",
        }),
      () => {
        setReview(undefined);
        setNotice("Download queued.");
        jobs.reload();
      },
    );
  }
  function reviewImport(job: Job) {
    void run(
      job.id,
      () =>
        api<ImportReview>(
          `/youtube/jobs/${encodeURIComponent(job.id)}/import-review`,
        ),
      setImportReview,
    );
  }
  function cancelJob(job: Job) {
    void run(
      job.id,
      () =>
        api<{ status: string }>(
          `/youtube/jobs/${encodeURIComponent(job.id)}/cancel`,
          { method: "POST", body: "{}" },
        ),
      () => {
        setNotice("Cancelled. Isolated files, if any, were retained.");
        jobs.reload();
      },
    );
  }
  function importJob() {
    if (!importReview) return;
    void run(
      importReview.job_id,
      () =>
        api<{ status: string }>(
          `/youtube/jobs/${encodeURIComponent(importReview.job_id)}/import`,
          { method: "POST", body: JSON.stringify(importReview) },
        ),
      () => {
        setImportReview(undefined);
        setNotice("Import requested.");
        jobs.reload();
      },
    );
  }

  async function loadPlaylist(
    value = batchURL,
    showID = batchShow,
    seasonNumber = batchSeason,
  ) {
    if (!value.trim()) return;
    setBusy("batch-preview");
    setError("");
    setNotice("");
    setBatchResults([]);
    try {
      const result = await api<BatchPreview>("/youtube/playlist/preview", {
        method: "POST",
        body: JSON.stringify({
          source_url: value.trim(),
          show_id: showID,
          season_number: seasonNumber,
        }),
        signal: AbortSignal.timeout(75000),
      });
      setBatchURL(result.source_url);
      setBatch(result);
      setBatchShow(result.show?.id ?? 0);
      setBatchSeason(result.season_number);
      setSelection(
        new Set(
          result.entries
            .filter((entry) => entry.selected_default)
            .map((entry) => entry.video_id),
        ),
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }
  function chooseShow(id: number) {
    setBatchShow(id);
    setShowQuery("");
    void loadPlaylist(batchURL, id, 0);
  }
  function toggleEntry(id: string) {
    setSelection((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }
  const chosenEntries = (batch?.entries ?? []).filter((entry) =>
    selection.has(entry.video_id),
  );
  const availableChosen = chosenEntries.filter(
    (entry) => entry.status === "available",
  ).length;
  function queueBatch() {
    const showID = batch?.show?.id;
    if (!showID || !batchSeason || chosenEntries.length === 0) return;
    if (availableChosen > 0 && !replaceExisting) {
      setError(
        "Some selected episodes are already in the library. Enable replacement or deselect them.",
      );
      return;
    }
    void run(
      "batch-queue",
      () =>
        api<{ items: BatchResult[] }>("/youtube/playlist/queue", {
          method: "POST",
          body: JSON.stringify({
            source_url: batchURL,
            show_id: showID,
            season_number: batchSeason,
            quality: batchQuality,
            subtitles: {
              languages: [],
              automatic: false,
              quality: batchQuality,
            },
            entries: chosenEntries.map((entry) => entry.video_id),
            replace_existing: replaceExisting,
            keep_source: keepSource,
          }),
          signal: AbortSignal.timeout(150000),
        }),
      (result) => {
        setBatchResults(result.items);
        setNotice(
          `${result.items.filter((item) => item.state === "queued").length} of ${result.items.length} queued.`,
        );
        jobs.reload();
      },
    );
  }

  return (
    <PageBody>
      <PageHeading title="YouTube">
        <Button asChild variant="soft">
          <Link to="/youtube/subscriptions">Subscriptions</Link>
        </Button>
      </PageHeading>
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

      <div
        className="yt-mode-tabs"
        role="tablist"
        aria-label="YouTube workflow"
      >
        <button
          type="button"
          role="tab"
          aria-selected={mode === "single"}
          onClick={() => setMode("single")}
        >
          Single video
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={mode === "playlist"}
          onClick={() => setMode("playlist")}
        >
          Playlist to series
        </button>
      </div>

      {mode === "single" ? (
        <>
          <section
            className="rs-panel yt-downloader"
            aria-label="Add YouTube download"
          >
            <div className="yt-step-head">
              <span>1</span>
              <div>
                <h2>Load a video or playlist</h2>
                <p>Paste a public YouTube URL.</p>
              </div>
            </div>
            <form
              className="yt-url-row"
              onSubmit={(e) => {
                e.preventDefault();
                inspect(url);
              }}
            >
              <TextField.Root
                value={url}
                onChange={(e) => setURL(e.target.value)}
                placeholder="https://www.youtube.com/watch?v=…"
                aria-label="YouTube URL"
              />
              <Button type="submit" disabled={!!busy || !url.trim()}>
                {busy === "inspect" ? "Loading…" : "Load"}
              </Button>
            </form>

            {inspection && (
              <>
                <div className="yt-source-head">
                  <div>
                    <h2>{inspection.title || "YouTube source"}</h2>
                    <span>{inspection.channel}</span>
                  </div>
                  {inspection.truncated && (
                    <span className="rs-label">First 100</span>
                  )}
                </div>
                <ul className="yt-results" aria-label="Source results">
                  {inspection.entries.map((entry) => (
                    <li
                      key={`${entry.kind}-${entry.id}`}
                      className={selected?.id === entry.id ? "selected" : ""}
                    >
                      <button
                        type="button"
                        className="yt-result"
                        disabled={!!busy}
                        onClick={() => {
                          if (entry.kind === "playlist") {
                            inspect(entry.url);
                            return;
                          }
                          setSelected(entry);
                          clearReview();
                        }}
                      >
                        {entry.thumbnail ? (
                          <img src={entry.thumbnail} alt="" loading="lazy" />
                        ) : (
                          <span
                            className="yt-thumb-placeholder"
                            aria-hidden="true"
                          >
                            ▶
                          </span>
                        )}
                        <span className="yt-result-copy">
                          <strong>{entry.title || entry.id}</strong>
                          <small>
                            {entry.channel || "YouTube"}
                            {entry.duration_seconds
                              ? ` · ${duration(entry.duration_seconds)}`
                              : ""}
                          </small>
                        </span>
                        {entry.kind === "playlist" && (
                          <Badge color="gray">Playlist</Badge>
                        )}
                      </button>
                    </li>
                  ))}
                </ul>
                {inspection.entries.length === 0 && <p>No videos found.</p>}
              </>
            )}
          </section>

          {selected && (
            <section
              className="rs-panel yt-downloader"
              aria-label="Choose destination"
            >
              <div className="yt-step-head">
                <span>2</span>
                <div>
                  <h2>Choose where it belongs</h2>
                  <p>
                    {selected.title} · {selected.channel}
                  </p>
                </div>
              </div>
              <TargetPicker
                kind={kind}
                setKind={(value) => {
                  setKind(value);
                  setMedia("");
                  setSeason("");
                  setEpisode("");
                  setSearched("");
                  clearReview();
                }}
                busy={!!busy}
                collections={collections}
                collection={collection}
                setCollection={(value) => {
                  setCollection(value);
                  clearReview();
                }}
                createCollection={createCollection}
                collectionTitle={collectionTitle}
                setCollectionTitle={setCollectionTitle}
                query={query}
                setQuery={setQuery}
                searched={searched}
                setSearched={setSearched}
                page={page}
                setPage={setPage}
                library={library}
                media={media}
                setMedia={(value) => {
                  setMedia(value);
                  setSeason("");
                  setEpisode("");
                  clearReview();
                }}
                show={show}
                season={season}
                setSeason={(value) => {
                  setSeason(value);
                  setEpisode("");
                  clearReview();
                }}
                episodes={episodes}
                episode={episode}
                setEpisode={(value) => {
                  setEpisode(value);
                  clearReview();
                }}
              />
              <div className="yt-step-head">
                <span>3</span>
                <div>
                  <h2>Quality and subtitles</h2>
                  <p>Pick the downloaded version before reviewing.</p>
                </div>
              </div>
              <div className="yt-options">
                <label>
                  Quality
                  <select
                    value={quality}
                    disabled={!!busy}
                    onChange={(e) => {
                      setQuality(e.target.value as Quality);
                      clearReview();
                    }}
                  >
                    <option value="best">Best available</option>
                    <option value="1080">1080p</option>
                    <option value="720">720p</option>
                  </select>
                </label>
                <label>
                  Subtitle languages
                  <TextField.Root
                    value={subtitleLanguages}
                    disabled={!!busy}
                    placeholder="en, ja"
                    onChange={(e) => {
                      setSubtitleLanguages(e.target.value);
                      clearReview();
                    }}
                  />
                </label>
                <label className="rs-check">
                  <input
                    type="checkbox"
                    checked={automaticSubtitles}
                    disabled={!!busy || !subtitleLanguages.trim()}
                    onChange={(e) => {
                      setAutomaticSubtitles(e.target.checked);
                      clearReview();
                    }}
                  />{" "}
                  Use automatic captions
                </label>
              </div>
              <div className="yt-step-head">
                <span>4</span>
                <div>
                  <h2>Review and download</h2>
                  <p>The reviewed file is still verified before import.</p>
                </div>
              </div>
              <div className="rs-form-actions">
                <Button
                  variant="soft"
                  disabled={!!busy || !target}
                  onClick={preview}
                >
                  {busy === "review" ? "Checking…" : "Check destination"}
                </Button>
              </div>
              {review && (
                <div className="yt-review" aria-label="Review YouTube download">
                  <strong>{review.target_label}</strong>
                  <span>
                    {qualityLabel(quality)} ·{" "}
                    {review.subtitles?.languages.length
                      ? `${review.subtitles.languages.join(", ")} (${review.subtitles.automatic ? "auto" : "creator"})`
                      : "No subtitles"}
                  </span>
                  <Button disabled={!!busy} onClick={queue}>
                    Download
                  </Button>
                </div>
              )}
            </section>
          )}
        </>
      ) : (
        <>
          <section
            className="rs-panel yt-downloader"
            aria-label="Playlist to series"
          >
            <div className="yt-step-head">
              <span>1</span>
              <div>
                <h2>Load a playlist</h2>
                <p>Paste a playlist that belongs to a TV show.</p>
              </div>
            </div>
            <form
              className="yt-url-row"
              onSubmit={(e) => {
                e.preventDefault();
                void loadPlaylist(batchURL, 0, 0);
              }}
            >
              <TextField.Root
                value={batchURL}
                onChange={(e) => setBatchURL(e.target.value)}
                placeholder="https://www.youtube.com/playlist?list=…"
                aria-label="Playlist URL"
              />
              <Button type="submit" disabled={!!busy || !batchURL.trim()}>
                {busy === "batch-preview" ? "Loading…" : "Load playlist"}
              </Button>
            </form>

            {batch && (
              <>
                <div className="yt-step-head">
                  <span>2</span>
                  <div>
                    <h2>Confirm the show and season</h2>
                    <p>
                      {batch.title} · {batch.channel}
                    </p>
                  </div>
                </div>
                {batch.needs_show ? (
                  <div className="yt-show-match">
                    {batch.candidates.length > 0 ? (
                      <>
                        <p className="rs-form-help">
                          Choose the saved show this playlist belongs to.
                        </p>
                        <label>
                          Show
                          <select
                            value={batchShow || ""}
                            onChange={(e) => chooseShow(Number(e.target.value))}
                          >
                            <option value="">Choose a show</option>
                            {batch.candidates.map((candidate) => (
                              <option key={candidate.id} value={candidate.id}>
                                {candidate.title}
                                {candidate.year
                                  ? ` (${candidate.year})`
                                  : ""} · {Math.round(candidate.score * 100)}%
                                title match
                              </option>
                            ))}
                          </select>
                        </label>
                      </>
                    ) : (
                      <p className="rs-form-help">
                        No saved show matched this playlist. Search your TV
                        library.
                      </p>
                    )}
                    <form
                      className="yt-inline-form"
                      onSubmit={(e) => e.preventDefault()}
                    >
                      <TextField.Root
                        value={showQuery}
                        onChange={(e) => setShowQuery(e.target.value)}
                        placeholder="Search your TV shows"
                        aria-label="Search TV shows"
                      />
                    </form>
                    {showSearch.data?.items.length ? (
                      <label>
                        Saved show
                        <select
                          value=""
                          onChange={(e) => chooseShow(Number(e.target.value))}
                        >
                          <option value="">Choose a show</option>
                          {showSearch.data.items.map((item) => (
                            <option key={item.id} value={item.id}>
                              {item.title}
                              {item.year ? ` (${item.year})` : ""}
                            </option>
                          ))}
                        </select>
                      </label>
                    ) : null}
                  </div>
                ) : (
                  <div className="yt-show-match">
                    <p>
                      <strong>{batch.show?.title}</strong>
                      {batch.show?.year ? ` (${batch.show.year})` : ""} ·{" "}
                      {Math.round((batch.show?.score ?? 1) * 100)}% title match
                    </p>
                    <div className="yt-options">
                      <label>
                        Season
                        <select
                          value={batchSeason}
                          onChange={(e) => {
                            setBatchSeason(Number(e.target.value));
                            void loadPlaylist(
                              batchURL,
                              batchShow,
                              Number(e.target.value),
                            );
                          }}
                        >
                          <option value={0}>Choose a season</option>
                          {batch.seasons.map((seasonItem) => (
                            <option
                              key={seasonItem.number}
                              value={seasonItem.number}
                              disabled={!seasonItem.loaded}
                            >
                              Season {seasonItem.number}
                              {seasonItem.loaded
                                ? ` · ${seasonItem.episode_count} episodes`
                                : " · not loaded"}
                            </option>
                          ))}
                        </select>
                      </label>
                    </div>
                    {batch.seasons.length === 0 && (
                      <p className="rs-form-help">
                        Load the show's seasons on its TV page first.
                      </p>
                    )}
                  </div>
                )}

                {!batch.needs_show && batchSeason > 0 && (
                  <>
                    <div className="yt-step-head">
                      <span>3</span>
                      <div>
                        <h2>Select episodes</h2>
                        <p>
                          Longest video per episode is preselected. Unmatched
                          videos stay unselected.
                        </p>
                      </div>
                    </div>
                    <div className="rs-form-actions">
                      <Button
                        size="1"
                        variant="soft"
                        onClick={() =>
                          setSelection(
                            new Set(
                              batch.entries
                                .filter(
                                  (entry) =>
                                    !entry.duplicate &&
                                    (entry.status === "missing" ||
                                      (replaceExisting &&
                                        entry.status === "available")),
                                )
                                .map((entry) => entry.video_id),
                            ),
                          )
                        }
                      >
                        Select eligible
                      </Button>
                      <Button
                        size="1"
                        variant="ghost"
                        onClick={() => setSelection(new Set())}
                      >
                        Clear
                      </Button>
                      <span>{selection.size} selected</span>
                    </div>
                    {!replaceExisting &&
                      batch.entries.some(
                        (entry) => entry.status === "available",
                      ) && (
                        <p className="rs-form-help">
                          Matched episodes already in the library are disabled.
                          Enable replacement in step 4 to select new versions.
                        </p>
                      )}
                    <ul
                      className="yt-batch-list"
                      aria-label="Playlist episodes"
                    >
                      {batch.entries.map((entry) => {
                        const disabled =
                          entry.status === "active" ||
                          entry.status === "unmatched" ||
                          (entry.status === "available" && !replaceExisting);
                        return (
                          <li key={entry.video_id}>
                            <input
                              type="checkbox"
                              checked={selection.has(entry.video_id)}
                              disabled={disabled}
                              aria-label={`Select ${entry.title}`}
                              onChange={() => toggleEntry(entry.video_id)}
                            />
                            <img
                              src={entry.thumbnail || thumb(entry.video_id)}
                              alt=""
                              loading="lazy"
                            />
                            <div className="yt-batch-copy">
                              <strong>{entry.title}</strong>
                              <span>
                                {duration(entry.duration_seconds)} ·{" "}
                                {episodeLabel(entry)}
                              </span>
                              {entry.note && <small>{entry.note}</small>}
                            </div>
                            <Badge
                              color={
                                entry.status === "missing"
                                  ? "green"
                                  : entry.status === "available"
                                    ? "gray"
                                    : entry.status === "active"
                                      ? "amber"
                                      : "red"
                              }
                            >
                              {entry.status === "missing"
                                ? "Missing"
                                : entry.status === "available"
                                  ? "In library"
                                  : entry.status === "active"
                                    ? "Downloading"
                                    : "Unmatched"}
                            </Badge>
                          </li>
                        );
                      })}
                    </ul>
                    <div className="yt-step-head">
                      <span>4</span>
                      <div>
                        <h2>Download and import</h2>
                        <p>
                          Files use the show's organization and quality naming.
                        </p>
                      </div>
                    </div>
                    <div className="yt-options">
                      <label>
                        Quality
                        <select
                          value={batchQuality}
                          onChange={(e) =>
                            setBatchQuality(e.target.value as Quality)
                          }
                        >
                          <option value="best">Best available</option>
                          <option value="1080">1080p</option>
                          <option value="720">720p</option>
                        </select>
                      </label>
                      <label className="rs-check">
                        <input
                          type="checkbox"
                          checked={replaceExisting}
                          onChange={(e) => {
                            setReplaceExisting(e.target.checked);
                            if (!e.target.checked) {
                              setSelection((current) => {
                                const next = new Set(current);
                                for (const entry of batch.entries) {
                                  if (entry.status === "available")
                                    next.delete(entry.video_id);
                                }
                                return next;
                              });
                            }
                          }}
                        />{" "}
                        Replace episodes already in the library
                      </label>
                      <label className="rs-check">
                        <input
                          type="checkbox"
                          checked={keepSource}
                          onChange={(e) => setKeepSource(e.target.checked)}
                        />{" "}
                        Save a paused source for future episodes (enable it on
                        the show page)
                      </label>
                    </div>
                    {replaceExisting && availableChosen > 0 && (
                      <p className="rs-form-help">
                        Replacement downloads keep the old file and require a
                        separate swap review in Organize library.
                      </p>
                    )}
                    <div className="rs-form-actions">
                      <Button
                        disabled={!!busy || chosenEntries.length === 0}
                        onClick={queueBatch}
                      >
                        {busy === "batch-queue"
                          ? "Queueing…"
                          : `Download ${chosenEntries.length} selected`}
                      </Button>
                    </div>
                    {batchResults.length > 0 && (
                      <ul className="yt-batch-results">
                        {batchResults.map((result) => {
                          const entry = batch.entries.find(
                            (item) => item.video_id === result.video_id,
                          );
                          return (
                            <li key={result.video_id}>
                              <span>{entry?.title ?? result.video_id}</span>
                              <Badge
                                color={
                                  result.state === "queued" ? "green" : "gray"
                                }
                              >
                                {result.state === "queued"
                                  ? "Queued"
                                  : result.message}
                              </Badge>
                            </li>
                          );
                        })}
                      </ul>
                    )}
                  </>
                )}
              </>
            )}
          </section>
        </>
      )}

      {mode === "single" &&
        inspection &&
        kind === "custom" &&
        inspection.source.kind !== "video" && (
          <YouTubeBulk
            key={`${inspection.source.url}-${collection}-${subtitleLanguages}-${automaticSubtitles}`}
            url={inspection.source.url}
            entries={inspection.entries}
            collectionID={Number(collection)}
            subtitles={{
              languages: subtitleLanguages
                .split(",")
                .map((v) => v.trim())
                .filter(Boolean),
              automatic: automaticSubtitles,
            }}
            reload={jobs.reload}
          />
        )}

      <section className="rs-panel yt-queue" aria-label="YouTube downloads">
        <div className="rs-panel-header">
          <h2>Downloads</h2>
          <Button variant="ghost" onClick={jobs.reload}>
            Refresh
          </Button>
        </div>
        <RequestState
          loading={jobs.loading}
          error={jobs.error}
          retry={jobs.reload}
        />
        {jobs.data?.items.length === 0 && (
          <p className="yt-empty">No downloads yet.</p>
        )}
        <ul className="yt-jobs">
          {jobs.data?.items.map((job) => (
            <JobCard
              key={job.id}
              job={job}
              busy={!!busy}
              reviewImport={reviewImport}
              cancelJob={cancelJob}
            />
          ))}
        </ul>
        {importReview && (
          <div className="review-box" aria-label="Import review">
            <h3>{importReview.target_label}</h3>
            <p className="rs-form-help">{importReview.destination}</p>
            <p>
              {bytes(importReview.size_bytes)} · SHA-256{" "}
              {importReview.sha256.slice(0, 16)}…
            </p>
            {importReview.subtitles.map((sub) => (
              <p key={sub.language} className="rs-form-help">
                {sub.language}: {sub.destination} · SHA-256 {sub.sha256}
              </p>
            ))}
            <div className="rs-form-actions">
              <Button disabled={!!busy} onClick={importJob}>
                Import
              </Button>
              <Button variant="soft" onClick={() => setImportReview(undefined)}>
                Cancel
              </Button>
            </div>
          </div>
        )}
      </section>

      <details className="rs-panel yt-library">
        <summary>
          <h2>YouTube library</h2>
          <span>{files.data?.items.length ?? 0}</span>
        </summary>
        <RequestState
          loading={files.loading}
          error={files.error}
          retry={files.reload}
        />
        {files.data?.items.length === 0 && (
          <p className="yt-empty">No imported videos.</p>
        )}
        <ul>
          {files.data?.items.map((file) => (
            <li key={file.job_id}>
              <strong>{file.title}</strong>
              <span>
                {file.collection} · {file.relative_path} ·{" "}
                {bytes(file.size_bytes)}
              </span>
            </li>
          ))}
        </ul>
      </details>
    </PageBody>
  );
}

function TargetPicker(props: {
  kind: Target["kind"];
  setKind: (value: Target["kind"]) => void;
  busy: boolean;
  collections: Resource<{ items: Collection[] }>;
  collection: string;
  setCollection: (value: string) => void;
  createCollection: (e: FormEvent) => void;
  collectionTitle: string;
  setCollectionTitle: (value: string) => void;
  query: string;
  setQuery: (value: string) => void;
  searched: string;
  setSearched: (value: string) => void;
  page: number;
  setPage: (value: number) => void;
  library: Resource<LibraryPage>;
  media: string;
  setMedia: (value: string) => void;
  show: Resource<TVSeries>;
  season: string;
  setSeason: (value: string) => void;
  episodes: Resource<TVSeasonDetails>;
  episode: string;
  setEpisode: (value: string) => void;
}) {
  const {
    kind,
    setKind,
    busy,
    collections,
    collection,
    setCollection,
    createCollection,
    collectionTitle,
    setCollectionTitle,
    query,
    setQuery,
    searched,
    setSearched,
    page,
    setPage,
    library,
    media,
    setMedia,
    show,
    season,
    setSeason,
    episodes,
    episode,
    setEpisode,
  } = props;
  return (
    <div className="yt-target">
      <label>
        Destination
        <select
          value={kind}
          disabled={busy}
          onChange={(e) => setKind(e.target.value as Target["kind"])}
        >
          <option value="custom">Custom collection</option>
          <option value="movie">Movie</option>
          <option value="episode">TV episode</option>
        </select>
      </label>
      {kind === "custom" && (
        <>
          <RequestState
            loading={collections.loading}
            error={collections.error}
            retry={collections.reload}
          />
          <label>
            Collection
            <select
              value={collection}
              disabled={busy}
              onChange={(e) => setCollection(e.target.value)}
            >
              <option value="">Choose a collection</option>
              {collections.data?.items.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.title}
                </option>
              ))}
            </select>
          </label>
          <form className="yt-inline-form" onSubmit={createCollection}>
            <TextField.Root
              value={collectionTitle}
              onChange={(e) => setCollectionTitle(e.target.value)}
              placeholder="New collection"
              aria-label="New collection name"
              maxLength={200}
            />
            <Button
              type="submit"
              variant="soft"
              disabled={busy || !collectionTitle.trim()}
            >
              Create
            </Button>
          </form>
        </>
      )}
      {kind !== "custom" && (
        <>
          <form
            className="yt-inline-form"
            onSubmit={(e) => {
              e.preventDefault();
              setSearched(query.trim());
              setPage(1);
              setMedia("");
              setSeason("");
              setEpisode("");
            }}
          >
            <TextField.Root
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={`Search ${kind === "movie" ? "movies" : "TV shows"}`}
              aria-label="Search library"
            />
            <Button
              type="submit"
              variant="soft"
              disabled={busy || !query.trim()}
            >
              Search
            </Button>
          </form>
          {searched && (
            <>
              <RequestState
                loading={library.loading}
                error={library.error}
                retry={library.reload}
              />
              <label>
                {kind === "movie" ? "Movie" : "Show"}
                <select
                  value={media}
                  disabled={busy}
                  onChange={(e) => setMedia(e.target.value)}
                >
                  <option value="">Choose a title</option>
                  {library.data?.items.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.title}
                      {item.year ? ` (${item.year})` : ""}
                    </option>
                  ))}
                </select>
              </label>
              <div className="rs-form-actions">
                <Button
                  size="1"
                  variant="soft"
                  disabled={busy || page <= 1}
                  onClick={() => setPage(page - 1)}
                >
                  Previous
                </Button>
                <Button
                  size="1"
                  variant="soft"
                  disabled={
                    busy ||
                    !library.data ||
                    page * library.data.limit >= library.data.total
                  }
                  onClick={() => setPage(page + 1)}
                >
                  More
                </Button>
              </div>
            </>
          )}
          {kind === "episode" && media && (
            <>
              <RequestState
                loading={show.loading}
                error={show.error}
                retry={show.reload}
              />
              <label>
                Season
                <select
                  value={season}
                  disabled={busy}
                  onChange={(e) => setSeason(e.target.value)}
                >
                  <option value="">Choose a season</option>
                  {show.data?.seasons
                    .filter((item) => item.loaded_at)
                    .map((item) => (
                      <option key={item.id} value={item.number}>
                        Season {item.number}
                      </option>
                    ))}
                </select>
              </label>
            </>
          )}
          {kind === "episode" && season !== "" && (
            <>
              <RequestState
                loading={episodes.loading}
                error={episodes.error}
                retry={episodes.reload}
              />
              <label>
                Episode
                <select
                  value={episode}
                  disabled={busy}
                  onChange={(e) => setEpisode(e.target.value)}
                >
                  <option value="">Choose an episode</option>
                  {episodes.data?.episodes.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.number}. {item.title}
                    </option>
                  ))}
                </select>
              </label>
            </>
          )}
        </>
      )}
    </div>
  );
}

function JobCard({
  job,
  busy,
  reviewImport,
  cancelJob,
}: {
  job: Job;
  busy: boolean;
  reviewImport: (job: Job) => void;
  cancelJob: (job: Job) => void;
}) {
  const percent = Math.min(100, Math.max(0, Math.round(job.progress * 100)));
  const replacement =
    job.state === "awaiting_review" &&
    job.message.includes("Review replacement");
  return (
    <li className="yt-job">
      <img src={thumb(job.video.id)} alt="" loading="lazy" />
      <div className="yt-job-main">
        <div className="yt-job-title">
          <strong>{job.video.title}</strong>
          <Badge color={stateColor(job.state)}>{stateLabel(job.state)}</Badge>
        </div>
        <span className="yt-job-meta">
          {job.video.channel}
          {job.target_label ? ` · ${job.target_label}` : ""}
          {job.quality ? ` · ${qualityLabel(job.quality)}` : ""}
        </span>
        {job.state === "downloading" && (
          <div className="yt-progress">
            <progress
              value={job.total_bytes > 0 ? job.progress : undefined}
              max={1}
              aria-label={`Download progress for ${job.video.title}`}
            />
            <span>
              {job.total_bytes > 0
                ? `${percent}% of ${bytes(job.total_bytes)}`
                : job.downloaded_bytes > 0
                  ? bytes(job.downloaded_bytes)
                  : "Starting…"}
              {speed(job.speed_bps) ? ` · ${speed(job.speed_bps)}` : ""}
              {eta(job.eta_seconds) ? ` · ${eta(job.eta_seconds)}` : ""}
            </span>
          </div>
        )}
        {job.message && <span className="yt-job-message">{job.message}</span>}
        <div className="rs-form-actions">
          {replacement && (
            <Button asChild size="1" variant="soft">
              <Link
                to={`/organization?acquisition_kind=youtube&acquisition_job=${job.id}`}
              >
                Review replacement
              </Link>
            </Button>
          )}
          {job.state === "awaiting_review" && !replacement && (
            <Button size="1" disabled={busy} onClick={() => reviewImport(job)}>
              Review import
            </Button>
          )}
          {["queued", "downloading", "awaiting_review", "failed"].includes(
            job.state,
          ) && (
            <Button
              size="1"
              variant="soft"
              disabled={busy}
              onClick={() => cancelJob(job)}
            >
              Cancel
            </Button>
          )}
        </div>
      </div>
    </li>
  );
}
