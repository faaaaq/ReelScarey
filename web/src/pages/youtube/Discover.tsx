import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { Rss } from "lucide-react";
import { Button, TextField } from "@radix-ui/themes";
import {
  api,
  messageFor,
  type LibraryPage,
  type TVSeries,
  type TVSeasonDetails,
} from "../../api";
import { PageHeading, RequestState } from "../../components";
import { Label } from "../../ui";
import {
  PageBody,
  PageToolbar,
  ToolbarButton,
  ToolbarSection,
  WorkInProgress,
} from "../../ui";
import { useResource } from "../../useResource";
import { YouTubeBulk } from "../YouTubeBulk";
import { SourceDraftForm } from "./SourceCard";
import { TargetPicker } from "./TargetPicker";
import { duration, episodeLabel, qualityLabel, thumb } from "./status";
import type {
  BatchPreview,
  BatchResult,
  Collection,
  Entry,
  Inspection,
  Quality,
  Review,
  Target,
} from "./types";
import "../Admin.css";

/**
 * Discover: one inspect step with two explicit outcomes. Nothing is queued
 * until a reviewed target or episode selection is confirmed.
 */
export function Discover() {
  const [url, setURL] = useState("");
  const [inspection, setInspection] = useState<Inspection>();
  const [outcome, setOutcome] = useState<"none" | "download" | "show">("none");
  const [selected, setSelected] = useState<Entry>();
  const [inspecting, setInspecting] = useState(false);
  const [inspectError, setInspectError] = useState("");

  async function inspect(value: string) {
    if (!value.trim() || inspecting) return;
    setInspecting(true);
    setInspectError("");
    try {
      const result = await api<Inspection>("/youtube/inspect", {
        method: "POST",
        body: JSON.stringify({ url: value }),
        signal: AbortSignal.timeout(60000),
      });
      setURL(result.source.url);
      setInspection(result);
      setOutcome("none");
      setSelected(
        result.source.kind === "video" ? result.entries[0] : undefined,
      );
    } catch (e) {
      setInspectError(messageFor(e));
    } finally {
      setInspecting(false);
    }
  }

  const isPlaylist = inspection && inspection.source.kind !== "video";

  return (
    <>
      <WorkInProgress noticeKey="work-in-progress:youtube">
        <p>
          Nothing is queued until you review a source and confirm the target.
        </p>
      </WorkInProgress>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={Rss}
            label="Subscriptions"
            to="/youtube/subscriptions"
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading
          title="YouTube"
          description="Paste a public video or playlist, then download it or add it to a show."
        />
        <section
          className="rs-panel yt-downloader"
          aria-label="Load a YouTube source"
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
              void inspect(url);
            }}
          >
            <TextField.Root
              value={url}
              onChange={(e) => setURL(e.target.value)}
              placeholder="https://www.youtube.com/watch?v=…"
              aria-label="YouTube URL"
            />
            <Button type="submit" disabled={inspecting || !url.trim()}>
              {inspecting ? "Loading…" : "Load"}
            </Button>
          </form>
          {inspectError && (
            <p className="rs-alert is-danger" role="alert">
              {inspectError}
            </p>
          )}
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
              {inspection.truncated && (
                <p className="rs-form-help" role="note">
                  Discovery lists the newest 100 videos of a channel. To reach
                  older episodes, paste the show's playlist URL here.
                </p>
              )}
              <ul className="yt-results" aria-label="Source results">
                {inspection.entries.map((entry) => (
                  <li
                    key={`${entry.kind}-${entry.id}`}
                    className={selected?.id === entry.id ? "selected" : ""}
                  >
                    <button
                      type="button"
                      className="yt-result"
                      disabled={inspecting}
                      onClick={() => {
                        if (entry.kind === "playlist") {
                          void inspect(entry.url);
                          return;
                        }
                        setSelected(entry);
                        setOutcome("none");
                      }}
                    >
                      {entry.thumbnail ? (
                        <img
                          className="yt-thumb"
                          src={entry.thumbnail}
                          alt=""
                          loading="lazy"
                          onError={(e) => {
                            e.currentTarget.style.visibility = "hidden";
                          }}
                        />
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
                        <Label kind="default">Playlist</Label>
                      )}
                    </button>
                  </li>
                ))}
              </ul>
              {inspection.entries.length === 0 && <p>No videos found.</p>}
            </>
          )}
          {inspection && !selected && (
            <div
              className="yt-outcomes"
              role="group"
              aria-label="Choose what to do with this source"
            >
              {isPlaylist && (
                <button
                  type="button"
                  className="yt-outcome"
                  onClick={() => {
                    setSelected(undefined);
                    setOutcome("download");
                  }}
                >
                  <strong>Download now</strong>
                  <span>
                    Match the playlist to a show and queue the episodes you
                    select.
                  </span>
                </button>
              )}
              <button
                type="button"
                className="yt-outcome"
                onClick={() => {
                  setSelected(undefined);
                  setOutcome("show");
                }}
              >
                <strong>Add to a show</strong>
                <span>
                  Create a paused source that watches this playlist and maps new
                  videos to episodes.
                </span>
              </button>
            </div>
          )}
        </section>

        {selected && inspection && (
          <SingleDownload inspection={inspection} selected={selected} />
        )}
        {outcome === "download" && inspection && isPlaylist && !selected && (
          <PlaylistDownload initialURL={inspection.source.url} />
        )}
        {outcome === "show" && inspection && (
          <ShowSource initialURL={isPlaylist ? inspection.source.url : ""} />
        )}
      </PageBody>
    </>
  );
}

/** Existing single-video flow: reviewed target, quality, review, queue. */
function SingleDownload({
  inspection,
  selected,
}: {
  inspection: Inspection;
  selected: Entry;
}) {
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
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const collections = useResource<{ items: Collection[] }>(
    "/youtube/collections",
  );
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
        api<unknown>(
          `/youtube/reviews/${encodeURIComponent(review.id)}/queue`,
          {
            method: "POST",
            body: "{}",
          },
        ),
      () => {
        setReview(undefined);
        setNotice("Download queued.");
      },
    );
  }

  return (
    <>
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
        {error && (
          <p className="rs-alert is-danger" role="alert">
            {error}
          </p>
        )}
        {notice && (
          <p className="rs-alert is-success" role="status">
            {notice} <Link to="/youtube/downloads">Open downloads</Link>
          </p>
        )}
        <div className="rs-form-actions">
          <Button variant="soft" disabled={!!busy || !target} onClick={preview}>
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
      {inspection.source.kind !== "video" && kind === "custom" && (
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
          reload={() => {
            // Downloads moved to their own page; nothing to refresh here.
          }}
        />
      )}
    </>
  );
}

/** Existing playlist-to-series flow, opened from the Download now outcome. */
function PlaylistDownload({ initialURL }: { initialURL: string }) {
  const [batchURL, setBatchURL] = useState(initialURL);
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
  const showSearch = useResource<LibraryPage>(
    showQuery.trim()
      ? `/library?kind=series&page=1&query=${encodeURIComponent(showQuery.trim())}`
      : null,
  );
  const loaded = useRef(false);

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
  // The playlist was already inspected above; preview it immediately.
  useEffect(() => {
    if (loaded.current) return;
    loaded.current = true;
    void loadPlaylist(initialURL, 0, 0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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
  async function queueBatch() {
    const showID = batch?.show?.id;
    if (!showID || !batchSeason || chosenEntries.length === 0) return;
    if (availableChosen > 0 && !replaceExisting) {
      setError(
        "Some selected episodes are already in the library. Enable replacement or deselect them.",
      );
      return;
    }
    setBusy("batch-queue");
    setError("");
    try {
      const result = await api<{ items: BatchResult[] }>(
        "/youtube/playlist/queue",
        {
          method: "POST",
          body: JSON.stringify({
            source_url: batchURL,
            show_id: showID,
            season_number: batchSeason,
            quality: batchQuality,
            subtitles: {
              // English subtitles are mandatory for every YouTube download;
              // the playlist-to-series path requests them explicitly.
              languages: ["en"],
              automatic: true,
              quality: batchQuality,
            },
            entries: chosenEntries.map((entry) => entry.video_id),
            replace_existing: replaceExisting,
            keep_source: keepSource,
          }),
          signal: AbortSignal.timeout(150000),
        },
      );
      setBatchResults(result.items);
      setNotice(
        `${result.items.filter((item) => item.state === "queued").length} of ${result.items.length} queued.`,
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }

  return (
    <section className="rs-panel yt-downloader" aria-label="Playlist to series">
      {error && (
        <p className="rs-alert is-danger" role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className="rs-alert is-success" role="status">
          {notice} <Link to="/youtube/downloads">Open downloads</Link>
        </p>
      )}
      {busy === "batch-preview" && !batch && (
        <p className="rs-form-help" role="status">
          Loading…
        </p>
      )}
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
                          {candidate.year ? ` (${candidate.year})` : ""} ·{" "}
                          {Math.round(candidate.score * 100)}% title match
                        </option>
                      ))}
                    </select>
                  </label>
                </>
              ) : (
                <p className="rs-form-help">
                  No saved show matched this playlist. Search your TV library.
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
                    Longest video per episode is preselected. Unmatched videos
                    stay unselected.
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
                batch.entries.some((entry) => entry.status === "available") && (
                  <p className="rs-form-help">
                    Matched episodes already in the library are disabled. Enable
                    replacement in step 4 to select new versions.
                  </p>
                )}
              <ul className="yt-batch-list" aria-label="Playlist episodes">
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
                      <Label
                        kind={
                          entry.status === "missing"
                            ? "success"
                            : entry.status === "available"
                              ? "default"
                              : entry.status === "active"
                                ? "warning"
                                : "danger"
                        }
                      >
                        {entry.status === "missing"
                          ? "Missing"
                          : entry.status === "available"
                            ? "In library"
                            : entry.status === "active"
                              ? "Downloading"
                              : "Unmatched"}
                      </Label>
                    </li>
                  );
                })}
              </ul>
              <div className="yt-step-head">
                <span>4</span>
                <div>
                  <h2>Download and import</h2>
                  <p>Files use the show's organization and quality naming.</p>
                </div>
              </div>
              <div className="yt-options">
                <label>
                  Quality
                  <select
                    value={batchQuality}
                    onChange={(e) => setBatchQuality(e.target.value as Quality)}
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
                  Save a paused source for future episodes (enable it on the
                  show page)
                </label>
              </div>
              {replaceExisting && availableChosen > 0 && (
                <p className="rs-form-help">
                  Replacement downloads keep the old file and require a separate
                  swap review in Organize library.
                </p>
              )}
              <div className="rs-form-actions">
                <Button
                  disabled={!!busy || chosenEntries.length === 0}
                  onClick={() => void queueBatch()}
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
                        <Label
                          kind={
                            result.state === "queued" ? "success" : "default"
                          }
                        >
                          {result.state === "queued"
                            ? "Queued"
                            : result.message}
                        </Label>
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
  );
}

/** "Add to a show" outcome: pick the show, then build a paused source draft. */
function ShowSource({ initialURL }: { initialURL: string }) {
  const [showQuery, setShowQuery] = useState("");
  const [searched, setSearched] = useState("");
  const [chosenShow, setChosenShow] = useState<{ id: number; title: string }>();
  const [saved, setSaved] = useState(false);
  const showSearch = useResource<LibraryPage>(
    searched
      ? `/library?kind=series&page=1&query=${encodeURIComponent(searched)}`
      : null,
  );

  return (
    <section className="rs-panel yt-downloader" aria-label="Add to a show">
      <div className="yt-step-head">
        <span>2</span>
        <div>
          <h2>Add to a show</h2>
          <p>
            Choose the saved show this playlist belongs to, preview how the
            videos map to episodes, and save the source paused. Nothing queues
            until you enable it on the show page.
          </p>
        </div>
      </div>
      <form
        className="yt-inline-form"
        onSubmit={(e) => {
          e.preventDefault();
          setSearched(showQuery.trim());
          setChosenShow(undefined);
          setSaved(false);
        }}
      >
        <TextField.Root
          value={showQuery}
          onChange={(e) => setShowQuery(e.target.value)}
          placeholder="Search your TV shows"
          aria-label="Search TV shows"
        />
        <Button type="submit" variant="soft" disabled={!showQuery.trim()}>
          Search
        </Button>
      </form>
      {searched && (
        <RequestState
          loading={showSearch.loading}
          error={showSearch.error}
          retry={showSearch.reload}
        />
      )}
      {searched && showSearch.data?.items.length ? (
        <label>
          Saved show
          <select
            value={chosenShow?.id ?? ""}
            onChange={(e) => {
              const id = Number(e.target.value);
              const found = showSearch.data?.items.find(
                (item) => item.id === id,
              );
              setChosenShow(found ? { id, title: found.title } : undefined);
              setSaved(false);
            }}
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
      {chosenShow && (
        <>
          {saved && (
            <p className="rs-alert is-success" role="status">
              Source saved paused for {chosenShow.title}.{" "}
              <Link to={`/series/${chosenShow.id}`}>
                Review it on the show page
              </Link>
            </p>
          )}
          <SourceDraftForm
            key={`${chosenShow.id}-${saved}`}
            seriesID={chosenShow.id}
            initialURL={initialURL}
            onSaved={() => setSaved(true)}
          />
        </>
      )}
    </section>
  );
}
