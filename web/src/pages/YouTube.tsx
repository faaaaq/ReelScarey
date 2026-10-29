import { Link } from "react-router-dom";
import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  api,
  messageFor,
  type LibraryPage,
  type TVSeries,
  type TVSeasonDetails,
} from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import { YouTubeBulk } from "./YouTubeBulk";

type Entry = {
  kind: "video" | "playlist";
  id: string;
  title: string;
  channel: string;
  url: string;
  published: string;
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
  subtitles: { languages: string[]; automatic: boolean };
};
type Job = {
  id: string;
  video: { id: string; title: string; channel: string };
  state: string;
  message: string;
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

export function YouTube() {
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
  const [subtitleLanguages, setSubtitleLanguages] = useState("en");
  const [automaticSubtitles, setAutomaticSubtitles] = useState(false);
  const [importReview, setImportReview] = useState<ImportReview>();
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const collections = useResource<{ items: Collection[] }>(
    "/youtube/collections",
  );
  const jobs = useResource<{ items: Job[] }>("/youtube/jobs");
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
  const target: Target | undefined =
    kind === "custom" && collection
      ? { kind, collection_id: Number(collection) }
      : kind === "movie" && media
        ? { kind, media_id: Number(media) }
        : kind === "episode" && media && episode
          ? { kind, media_id: Number(media), episode_id: Number(episode) }
          : undefined;
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
        setNotice(
          "Download queued. Check its status below before reviewing the import.",
        );
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
        setNotice("Download cancelled. Isolated files, if any, were retained.");
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
          {
            method: "POST",
            body: JSON.stringify(importReview),
          },
        ),
      () => {
        setImportReview(undefined);
        setNotice("Import requested.");
        jobs.reload();
      },
    );
  }
  return (
    <>
      <PageHeading
        title="YouTube"
        description="Inspect a video, playlist or channel and choose where a video belongs."
      />
      <p>
        <a href="/youtube/subscriptions">
          Manage channel and playlist subscriptions
        </a>
      </p>
      {error && (
        <p className="error-box" role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className="save-success" role="status">
          {notice}
        </p>
      )}
      <section
        className="panel settings-form"
        aria-label="Inspect YouTube source"
      >
        <h2>Find a source</h2>
        <p className="field-help">
          Use a public YouTube URL. Download only content you are permitted to
          use. Videos are reviewed again before import.
        </p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            inspect(url);
          }}
        >
          <label htmlFor="youtube-url">Video, playlist or channel URL</label>
          <TextField.Root
            id="youtube-url"
            value={url}
            onChange={(e) => setURL(e.target.value)}
            placeholder="https://www.youtube.com/…"
          />
          <Button type="submit" disabled={!!busy || !url.trim()}>
            {busy === "inspect" ? "Inspecting…" : "Inspect source"}
          </Button>
        </form>
        {inspection && (
          <div>
            <h3>{inspection.title || "YouTube source"}</h3>
            <p>{inspection.channel}</p>
            {inspection.truncated && (
              <p className="field-help">Showing the first 100 results.</p>
            )}
            {inspection.entries.length === 0 && (
              <p>No available videos were found.</p>
            )}
            <ul className="space-y-2" aria-label="Source results">
              {inspection.entries.map((entry, index) => (
                <li key={`${entry.kind}-${entry.id}-${index}`}>
                  <span>
                    {entry.title || entry.id}
                    {entry.channel ? ` · ${entry.channel}` : ""}
                  </span>{" "}
                  {entry.kind === "playlist" ? (
                    <Button
                      size="1"
                      variant="soft"
                      disabled={!!busy}
                      onClick={() => inspect(entry.url)}
                    >
                      Open playlist
                    </Button>
                  ) : (
                    <Button
                      size="1"
                      variant="soft"
                      disabled={!!busy}
                      onClick={() => {
                        setSelected(entry);
                        clearReview();
                      }}
                    >
                      {selected?.id === entry.id ? "Selected" : "Select video"}
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>
      {selected && (
        <section
          className="panel settings-form"
          aria-label="Choose YouTube target"
        >
          <h2>Choose where this video belongs</h2>
          <p>
            <strong>{selected.title || selected.id}</strong>
            {selected.channel ? ` · ${selected.channel}` : ""}
          </p>
          <label htmlFor="youtube-kind">Destination type</label>
          <select
            id="youtube-kind"
            value={kind}
            disabled={!!busy}
            onChange={(e) => {
              setKind(e.target.value as Target["kind"]);
              setMedia("");
              setSeason("");
              setEpisode("");
              setSearched("");
              clearReview();
            }}
          >
            <option value="custom">Custom collection</option>
            <option value="movie">Existing movie</option>
            <option value="episode">Existing TV episode</option>
          </select>
          {kind === "custom" ? (
            <>
              <RequestState
                loading={collections.loading}
                error={collections.error}
                retry={collections.reload}
              />
              <label htmlFor="youtube-collection">Custom collection</label>
              <select
                id="youtube-collection"
                value={collection}
                disabled={!!busy}
                onChange={(e) => {
                  setCollection(e.target.value);
                  clearReview();
                }}
              >
                <option value="">Choose a collection</option>
                {collections.data?.items.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.title}
                  </option>
                ))}
              </select>
              <form onSubmit={createCollection}>
                <label htmlFor="youtube-new-collection">
                  New collection name
                </label>
                <TextField.Root
                  id="youtube-new-collection"
                  value={collectionTitle}
                  onChange={(e) => setCollectionTitle(e.target.value)}
                  maxLength={200}
                />
                <Button
                  type="submit"
                  variant="soft"
                  disabled={!!busy || !collectionTitle.trim()}
                >
                  Create collection
                </Button>
              </form>
            </>
          ) : (
            <>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  setSearched(query.trim());
                  setPage(1);
                  setMedia("");
                  setSeason("");
                  setEpisode("");
                  clearReview();
                }}
              >
                <label htmlFor="youtube-catalog-query">
                  Search your {kind === "movie" ? "movies" : "TV shows"}
                </label>
                <TextField.Root
                  id="youtube-catalog-query"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
                <Button
                  type="submit"
                  variant="soft"
                  disabled={!!busy || !query.trim()}
                >
                  Search library
                </Button>
              </form>
              {searched && (
                <>
                  <RequestState
                    loading={library.loading}
                    error={library.error}
                    retry={library.reload}
                  />
                  <label htmlFor="youtube-media">
                    Choose {kind === "movie" ? "movie" : "show"}
                  </label>
                  <select
                    id="youtube-media"
                    value={media}
                    disabled={!!busy}
                    onChange={(e) => {
                      setMedia(e.target.value);
                      setSeason("");
                      setEpisode("");
                      clearReview();
                    }}
                  >
                    <option value="">Choose a title</option>
                    {library.data?.items.map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.title}
                        {item.year ? ` (${item.year})` : ""}
                      </option>
                    ))}
                  </select>
                  <div>
                    <Button
                      size="1"
                      variant="soft"
                      disabled={!!busy || page <= 1}
                      onClick={() => setPage(page - 1)}
                    >
                      Previous results
                    </Button>{" "}
                    <Button
                      size="1"
                      variant="soft"
                      disabled={
                        !!busy ||
                        !library.data ||
                        page * library.data.limit >= library.data.total
                      }
                      onClick={() => setPage(page + 1)}
                    >
                      More results
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
                  <label htmlFor="youtube-season">Season</label>
                  <select
                    id="youtube-season"
                    value={season}
                    disabled={!!busy}
                    onChange={(e) => {
                      setSeason(e.target.value);
                      setEpisode("");
                      clearReview();
                    }}
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
                  <p className="field-help">
                    Only seasons already loaded in your library are shown.
                  </p>
                </>
              )}
              {kind === "episode" && season !== "" && (
                <>
                  <RequestState
                    loading={episodes.loading}
                    error={episodes.error}
                    retry={episodes.reload}
                  />
                  <label htmlFor="youtube-episode">Episode</label>
                  <select
                    id="youtube-episode"
                    value={episode}
                    disabled={!!busy}
                    onChange={(e) => {
                      setEpisode(e.target.value);
                      clearReview();
                    }}
                  >
                    <option value="">Choose an episode</option>
                    {episodes.data?.episodes.map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.number}. {item.title}
                      </option>
                    ))}
                  </select>
                </>
              )}
            </>
          )}
          <label htmlFor="youtube-subtitles">Subtitle languages</label>
          <TextField.Root
            id="youtube-subtitles"
            value={subtitleLanguages}
            disabled={!!busy}
            onChange={(event) => {
              setSubtitleLanguages(event.target.value);
              clearReview();
            }}
          />
          <p className="field-help">
            Enter language codes separated by commas, such as en, ja. Leave
            blank for no subtitles. Unavailable languages are reported after
            download.
          </p>
          <label>
            <input
              type="checkbox"
              checked={automaticSubtitles}
              disabled={!!busy || !subtitleLanguages.trim()}
              onChange={(event) => {
                setAutomaticSubtitles(event.target.checked);
                clearReview();
              }}
            />{" "}
            Request automatic captions instead of creator-provided subtitles
          </label>
          <Button variant="soft" disabled={!!busy || !target} onClick={preview}>
            Review selection
          </Button>
          {review && (
            <div aria-label="Review YouTube download">
              <h3>Review download</h3>
              <p>
                {review.video.title} · {review.video.channel}
              </p>
              <p>Destination: {review.target_label}</p>
              <p>
                Subtitles:{" "}
                {review.subtitles?.languages.length
                  ? `${review.subtitles.languages.join(", ")} (${review.subtitles.automatic ? "automatic" : "creator-provided"})`
                  : "none"}
              </p>
              <p className="field-help">
                The completed file will still require an import review.
              </p>
              <Button disabled={!!busy} onClick={queue}>
                Queue download
              </Button>
            </div>
          )}
        </section>
      )}
      {inspection &&
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
      <section className="panel settings-form" aria-label="YouTube downloads">
        <h2>YouTube downloads</h2>
        <Button variant="soft" disabled={!!busy} onClick={jobs.reload}>
          Refresh status
        </Button>
        <RequestState
          loading={jobs.loading}
          error={jobs.error}
          retry={jobs.reload}
        />
        {jobs.data?.items.length === 0 && <p>No YouTube downloads yet.</p>}
        {jobs.data?.items.map((job) => (
          <article key={job.id}>
            <h3>{job.video.title}</h3>
            <p>
              {job.state.replaceAll("_", " ")}
              {job.message ? ` · ${job.message}` : ""}
            </p>
            {job.state === "awaiting_review" &&
              job.message.includes("Review replacement") && (
                <Link
                  className="text-link"
                  to={`/organization?acquisition_kind=youtube&acquisition_job=${job.id}`}
                >
                  Review acquired replacement
                </Link>
              )}
            {job.state === "awaiting_review" &&
              !job.message.includes("Review replacement") && (
                <Button
                  variant="soft"
                  disabled={!!busy}
                  onClick={() => reviewImport(job)}
                >
                  Review and import
                </Button>
              )}
            {["queued", "awaiting_review", "failed"].includes(job.state) && (
              <Button
                variant="soft"
                disabled={!!busy}
                onClick={() => cancelJob(job)}
              >
                Cancel download
              </Button>
            )}
          </article>
        ))}
        {importReview && (
          <div className="review-box" aria-label="Import review">
            <h3>Review import</h3>
            <p>Target: {importReview.target_label}</p>
            <p>Destination: {importReview.destination}</p>
            <p>Size: {importReview.size_bytes.toLocaleString()} bytes</p>
            <p>SHA-256: {importReview.sha256}</p>
            <p>Media inspection: {importReview.probe_json}</p>
            {importReview.subtitles.map((sub) => (
              <p key={sub.language}>
                Subtitle {sub.language}: {sub.destination} ·{" "}
                {sub.size_bytes.toLocaleString()} bytes · SHA-256 {sub.sha256}
              </p>
            ))}
            <Button disabled={!!busy} onClick={importJob}>
              Approve import
            </Button>{" "}
            <Button
              variant="soft"
              disabled={!!busy}
              onClick={() => setImportReview(undefined)}
            >
              Cancel
            </Button>
          </div>
        )}
      </section>
      <section
        className="panel settings-form"
        aria-label="Custom YouTube library"
      >
        <h2>Custom YouTube library</h2>
        <Button variant="soft" disabled={!!busy} onClick={files.reload}>
          Refresh library
        </Button>
        <RequestState
          loading={files.loading}
          error={files.error}
          retry={files.reload}
        />
        {files.data?.items.length === 0 && (
          <p>No custom videos imported yet.</p>
        )}
        {files.data?.items.map((file) => (
          <article key={file.job_id}>
            <h3>{file.title}</h3>
            <p>
              {file.collection} · {file.relative_path}
            </p>
            <p>{file.size_bytes.toLocaleString()} bytes</p>
          </article>
        ))}
      </section>
    </>
  );
}
