import { useEffect, useState, type FormEvent } from "react";
import {
  AlertDialog,
  Badge,
  Button,
  Dialog,
  TextField,
} from "@radix-ui/themes";
import { FolderSearch, RefreshCw } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import {
  api,
  messageFor,
  type MediaItem,
  type SearchResults,
  type TVSeries,
  type TVSeasonDetails,
} from "../api";
import { PageHeading, RequestState } from "../components";
import { Pagination } from "../movieComponents";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";
import { bytes } from "./Releases";

export type LibraryScan = {
  id: string;
  kind: "movie" | "series";
  media_root: string;
  folder: string;
  state: string;
  message: string;
  created_at: number;
  updated_at: number;
  discovered: number;
  registered: number;
  queued: number;
  needs_match: number;
  needs_attention: number;
  examined: number;
  skipped: number;
};
export type DiscoveredFile = {
  id: string;
  scan_id: string;
  path: string;
  full_path: string;
  hint: string;
  size_bytes: number;
  mtime: number;
  state: string;
  media_id: number | null;
  media_file_id: number | null;
  message: string;
};
export type DiscoveredPage = {
  items: DiscoveredFile[];
  scan: LibraryScan;
  page: number;
  total: number;
  limit: number;
};
type ScanList = {
  items: LibraryScan[];
  media_root: string;
  movie_root: string;
  series_root: string;
  collections?: {
    key: string;
    name: string;
    content: "movie" | "series" | "mixed";
    movie_root: string;
    series_root: string;
  }[];
};
const stateLabels: Record<string, string> = {
  queued: "Queued",
  scanning: "Discovering files",
  completed: "Ready for review",
  cancelled: "Discovery stopped",
  failed: "Discovery needs attention",
  review: "Needs a match",
  verifying: "Verifying video",
  registered: "In library",
  skipped: "Skipped",
  blocked: "Needs attention",
};

export function ExistingLibrary() {
  const roots = useLiveResource<ScanList>("/library/imports", 5000);
  const [params, setParams] = useSearchParams();
  const scanID = params.get("scan") ?? roots.data?.items[0]?.id;
  const [kind, setKind] = useState<"movie" | "series">("movie");
  const [libraryKey, setLibraryKey] = useState("");
  const collection = roots.data?.collections?.find(
    (item) => item.key === libraryKey,
  );
  const movieAllowed = !collection || collection.content !== "series";
  const seriesAllowed = !collection || collection.content !== "movie";
  useEffect(() => {
    if (libraryKey && roots.data && !collection) setLibraryKey("");
    if (collection?.content === "movie") setKind("movie");
    else if (collection?.content === "series") setKind("series");
  }, [collection, libraryKey, roots.data]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function start() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const scan = await api<LibraryScan>("/library/imports", {
        method: "POST",
        body: JSON.stringify({
          kind,
          ...(collection ? { library_key: collection.key } : {}),
        }),
      });
      setParams({ scan: scan.id });
      roots.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeading
        title="Find unmatched files"
        description="Scan your configured media folders for videos that are not registered, then review and match them without moving the files."
      />
      <RequestState
        loading={roots.loading}
        error={roots.error}
        retry={roots.reload}
      />
      {roots.data && (
        <section className="panel system-panel">
          <h2>
            <FolderSearch size={20} /> Discover files
          </h2>
          <p>
            The scan reads filenames and sizes. It leaves files, folders and
            existing subtitles untouched. Nothing is matched or registered
            automatically.
          </p>
          {!!roots.data.collections?.length && (
            <label>
              Collection{" "}
              <select
                aria-label="Scan collection"
                value={libraryKey}
                onChange={(event) => setLibraryKey(event.target.value)}
                disabled={busy}
              >
                <option value="">Default folders</option>
                {roots.data.collections.map((item) => (
                  <option key={item.key} value={item.key}>
                    {item.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          <dl className="detail-paths">
            {movieAllowed && (
              <>
                <dt>Movies</dt>
                <dd>
                  <code>{collection?.movie_root ?? roots.data.movie_root}</code>
                </dd>
              </>
            )}
            {seriesAllowed && (
              <>
                <dt>TV shows</dt>
                <dd>
                  <code>
                    {collection?.series_root ?? roots.data.series_root}
                  </code>
                </dd>
              </>
            )}
          </dl>
          <div className="import-actions">
            <label>
              Look for{" "}
              <select
                aria-label="Scan media type"
                value={kind}
                onChange={(e) => setKind(e.target.value as "movie" | "series")}
                disabled={busy}
              >
                <option value="movie" disabled={!movieAllowed}>
                  Movies
                </option>
                <option value="series" disabled={!seriesAllowed}>
                  TV shows
                </option>
              </select>
            </label>
            <Button
              disabled={
                busy ||
                (kind === "movie" ? !movieAllowed : !seriesAllowed) ||
                roots.data.items.some((s) =>
                  ["queued", "scanning"].includes(s.state),
                )
              }
              onClick={() => void start()}
            >
              {busy ? "Starting…" : "Scan configured folder"}
            </Button>
          </div>
          <p className="field-help">
            These paths are on the ReelScarey server. Mount existing media under
            the media root and check your{" "}
            <Link to="/settings">folder preferences</Link>. Discovery and
            registration keep files in place. Review renaming or reorganization
            separately in <Link to="/organization">Organize library</Link>.
            Anime classification and episode numbering are explicit choices in
            the title's Library section settings.
          </p>
        </section>
      )}
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
      {roots.data?.items.length ? (
        <>
          <label className="import-history">
            Scan history{" "}
            <select
              aria-label="Scan history"
              value={scanID}
              onChange={(e) => setParams({ scan: e.target.value })}
            >
              {roots.data.items.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.kind === "movie" ? "Movies" : "TV"} ·{" "}
                  {new Date(s.created_at * 1000).toLocaleString()} ·{" "}
                  {stateLabels[s.state] ?? s.state} · {s.needs_match ?? 0} need
                  a match · {s.needs_attention ?? 0} need attention
                </option>
              ))}
            </select>
          </label>
          {scanID && (
            <ScanReview
              key={scanID}
              id={scanID}
              changed={roots.reload}
              forgotten={() => {
                setParams({});
                roots.reload();
              }}
            />
          )}
        </>
      ) : (
        !roots.loading && (
          <p>No discovery scans yet. Choose movies or TV shows to begin.</p>
        )
      )}
    </>
  );
}
function ScanReview({
  id,
  changed,
  forgotten,
}: {
  id: string;
  changed: () => void;
  forgotten: () => void;
}) {
  const [page, setPage] = useState(1);
  const [filter, setFilter] = useState("review");
  const [file, setFile] = useState<DiscoveredFile>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const result = useLiveResource<DiscoveredPage>(
    `/library/imports/${id}?page=${page}&state=${filter}`,
    3000,
  );
  async function action(op: "cancel" | "forget" | "skip", fileID?: string) {
    setBusy(true);
    setError("");
    try {
      await api(
        op === "skip"
          ? `/library/import-files/${fileID}/skip`
          : `/library/imports/${id}${op === "cancel" ? "/cancel" : ""}`,
        { method: op === "forget" ? "DELETE" : "POST", body: "{}" },
      );
      if (op === "forget") forgotten();
      else {
        result.reload();
        changed();
      }
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  const scan = result.data?.scan;
  const active = scan && ["queued", "scanning"].includes(scan.state);
  return (
    <>
      <RequestState
        loading={result.loading}
        error={result.error}
        retry={result.reload}
      />
      {scan && (
        <section className="panel system-panel">
          <div className="indexer-title">
            <h2>{scan.kind === "movie" ? "Movie" : "TV"} discovery</h2>
            <Badge color={scan.state === "failed" ? "amber" : "gray"}>
              {stateLabels[scan.state] ?? scan.state}
            </Badge>
          </div>
          <p>{scan.message || "Reading the selected folder…"}</p>
          <div className="item-storage-stats">
            <div>
              <span>Discovered videos</span>
              <strong>{scan.discovered}</strong>
            </div>
            <div>
              <span>Verified registrations</span>
              <strong>{scan.registered}</strong>
            </div>
            <div>
              <span>Waiting or verifying</span>
              <strong>{scan.queued}</strong>
            </div>
            <div>
              <span>Need a match</span>
              <strong>{scan.needs_match ?? 0}</strong>
            </div>
            <div>
              <span>Need attention</span>
              <strong>{scan.needs_attention ?? 0}</strong>
            </div>
          </div>
          <p className="field-help">
            {scan.examined} entries examined · {scan.skipped} skipped (including
            registered files, hidden entries, links and unsupported files).
            Counts update as folders finish.
          </p>
          <div className="import-actions">
            <Button variant="soft" onClick={result.reload}>
              <RefreshCw size={15} />
              Refresh status
            </Button>
            {active && (
              <Button
                variant="soft"
                disabled={busy}
                onClick={() => void action("cancel")}
              >
                Stop discovery
              </Button>
            )}
            {!active && (
              <AlertDialog.Root>
                <AlertDialog.Trigger>
                  <Button
                    variant="ghost"
                    color="gray"
                    disabled={busy || !!scan.queued}
                  >
                    Forget this scan
                  </Button>
                </AlertDialog.Trigger>
                <AlertDialog.Content>
                  <AlertDialog.Title>Forget this scan?</AlertDialog.Title>
                  <AlertDialog.Description>
                    This removes its discovery and review records. Registered
                    media files and their catalog entries remain in your
                    library.
                  </AlertDialog.Description>
                  <div className="import-actions">
                    <AlertDialog.Cancel>
                      <Button variant="soft">Keep scan</Button>
                    </AlertDialog.Cancel>
                    <AlertDialog.Action>
                      <Button onClick={() => void action("forget")}>
                        Forget review records
                      </Button>
                    </AlertDialog.Action>
                  </div>
                </AlertDialog.Content>
              </AlertDialog.Root>
            )}
          </div>
        </section>
      )}
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
      <label className="import-history">
        Show{" "}
        <select
          aria-label="Discovery status filter"
          value={filter}
          onChange={(e) => {
            setFilter(e.target.value);
            setPage(1);
          }}
        >
          <option value="">All files</option>
          <option value="review">Needs a match</option>
          <option value="blocked">Needs attention</option>
          <option value="registered">In library</option>
          <option value="skipped">Skipped</option>
        </select>
      </label>
      {result.data?.items.length ? (
        <div className="file-details-list panel system-panel">
          {result.data.items.map((item) => (
            <article key={item.id} className="discovery-file">
              <div className="indexer-title">
                <h3>{item.path.split("/").at(-1)}</h3>
                <Badge
                  color={
                    item.state === "registered"
                      ? "green"
                      : item.state === "blocked"
                        ? "amber"
                        : "gray"
                  }
                >
                  {stateLabels[item.state] ?? item.state}
                </Badge>
              </div>
              <p>
                <code>{item.full_path}</code>
              </p>
              <p>{bytes(item.size_bytes)}</p>
              {item.message && <p className="field-help">{item.message}</p>}
              <div className="import-actions">
                {["review", "blocked"].includes(item.state) && (
                  <>
                    <Button disabled={busy} onClick={() => setFile(item)}>
                      Review match
                    </Button>
                    <Button
                      variant="soft"
                      disabled={busy}
                      onClick={() => void action("skip", item.id)}
                    >
                      Skip file
                    </Button>
                  </>
                )}
                {item.state === "registered" && item.media_id && (
                  <Link
                    className="text-link"
                    to={`/${scan?.kind === "series" ? "series" : "movies"}/${item.media_id}`}
                  >
                    Open library details
                  </Link>
                )}
              </div>
            </article>
          ))}
        </div>
      ) : (
        !result.loading && (
          <p>
            No files on this page. Registered videos, samples, trailers, hidden
            entries and unsupported files are excluded from new discoveries.
          </p>
        )
      )}
      {result.data && (
        <Pagination
          page={page}
          pages={Math.ceil(result.data.total / 50)}
          onPage={setPage}
        />
      )}
      <Dialog.Root
        open={!!file}
        onOpenChange={(open) => {
          if (!open && !busy) setFile(undefined);
        }}
      >
        <Dialog.Content maxWidth="760px">
          <Dialog.Title>Review existing file</Dialog.Title>
          <Dialog.Description>
            Confirm the catalog identity before verifying and registering this
            file in its current location.
          </Dialog.Description>
          {file && scan && (
            <MatchReview
              key={file.id}
              file={file}
              kind={scan.kind}
              busy={busy}
              setBusy={setBusy}
              done={() => {
                setFile(undefined);
                result.reload();
                changed();
              }}
            />
          )}
        </Dialog.Content>
      </Dialog.Root>
    </>
  );
}
function MatchReview({
  file,
  kind,
  busy,
  setBusy,
  done,
}: {
  file: DiscoveredFile;
  kind: "movie" | "series";
  busy: boolean;
  setBusy: (busy: boolean) => void;
  done: () => void;
}) {
  const [query, setQuery] = useState(file.hint);
  const [savedQuery, setSavedQuery] = useState(file.hint);
  const catalog = useResource<{ items: MediaItem[] }>(
    `/library/imports/catalog?kind=${kind}&query=${encodeURIComponent(savedQuery)}`,
  );
  const provider = useResource<{ configured: boolean }>("/providers/tmdb");
  const [remote, setRemote] = useState<SearchResults>();
  const [chosen, setChosen] = useState<MediaItem>();
  const [episodes, setEpisodes] = useState<number[]>([]);
  const [reviewed, setReviewed] = useState(false);
  const [error, setError] = useState("");
  function choose(item: MediaItem) {
    setChosen(item);
    setEpisodes([]);
    setReviewed(false);
    setError("");
  }
  async function searchRemote() {
    setBusy(true);
    setError("");
    try {
      setRemote(
        await api<SearchResults>(
          `/metadata/${kind === "movie" ? "movies" : "series"}?query=${encodeURIComponent(query)}&page=1`,
        ),
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function add(tmdbID: number) {
    setBusy(true);
    setError("");
    try {
      const out = await api<{ item: MediaItem }>(
        `/library/${kind === "movie" ? "movies" : "series"}`,
        {
          method: "POST",
          body: JSON.stringify({ tmdb_id: tmdbID, monitored: false }),
        },
      );
      choose(out.item);
      catalog.reload();
      setRemote(undefined);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function register(e: FormEvent) {
    e.preventDefault();
    if (!chosen || !reviewed || busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`/library/import-files/${file.id}/register`, {
        method: "POST",
        body: JSON.stringify({
          media_id: chosen.id,
          episode_ids: episodes,
          reviewed: true,
        }),
      });
      done();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="import-match" onSubmit={register}>
      <div className="import-file-summary">
        <code>{file.full_path}</code>
        <p>{bytes(file.size_bytes)} · The file stays here.</p>
      </div>
      <label>
        Title to look for{" "}
        <TextField.Root
          aria-label="Match title search"
          value={query}
          maxLength={200}
          onChange={(e) => setQuery(e.target.value)}
          disabled={busy}
        />
      </label>
      <div className="import-actions">
        <Button
          type="button"
          variant="soft"
          disabled={busy}
          onClick={() => {
            setSavedQuery(query);
            setRemote(undefined);
          }}
        >
          Find in catalog
        </Button>
        <Button
          type="button"
          variant="soft"
          disabled={busy}
          onClick={() => {
            setQuery("");
            setSavedQuery("");
            setRemote(undefined);
          }}
        >
          Browse catalog
        </Button>
        <Button
          type="button"
          variant="soft"
          disabled={busy || !query.trim() || !provider.data?.configured}
          onClick={() => void searchRemote()}
        >
          Search TMDb
        </Button>
      </div>
      <RequestState
        loading={catalog.loading}
        error={catalog.error}
        retry={catalog.reload}
      />
      {!provider.data?.configured && (
        <p className="field-help">
          Saved catalog titles work offline. Connect TMDb in Settings to add new
          titles.
        </p>
      )}
      <div className="import-catalog-results">
        <h3>Already in your catalog</h3>
        {catalog.data?.items.length ? (
          catalog.data.items.map((item) => (
            <div className="import-match-row" key={item.id}>
              <div>
                <strong>{item.title}</strong>
                <span>
                  {item.year ?? "Year unknown"} ·{" "}
                  {item.status === "available"
                    ? "Has registered media"
                    : "Catalogued"}
                </span>
              </div>
              <Button
                type="button"
                variant={chosen?.id === item.id ? "solid" : "soft"}
                disabled={busy}
                onClick={() => choose(item)}
              >
                {chosen?.id === item.id ? "Selected" : "Choose title"}
              </Button>
            </div>
          ))
        ) : (
          <p>
            No catalog matches. Try a shorter title, browse the catalog or
            search TMDb.
          </p>
        )}
      </div>
      {remote && (
        <div className="import-catalog-results">
          <h3>TMDb matches</h3>
          <p className="field-help">
            Adding a new title starts with monitoring paused. Registration
            follows your existing subtitle preferences.
          </p>
          {remote.items.length ? (
            remote.items.map((item) => (
              <div className="import-match-row" key={item.tmdb_id}>
                <div>
                  <strong>{item.title}</strong>
                  <span>{item.year ?? "Year unknown"}</span>
                  <p>{item.overview}</p>
                </div>
                <Button
                  type="button"
                  disabled={busy}
                  onClick={() => void add(item.tmdb_id)}
                >
                  Add & choose
                </Button>
              </div>
            ))
          ) : (
            <p>No TMDb results. Try another title.</p>
          )}
        </div>
      )}
      {chosen && (
        <section className="import-confirmation">
          <h3>
            Selected: {chosen.title} ({chosen.year ?? "year unknown"})
          </h3>
          {kind === "series" && (
            <EpisodePicker
              key={chosen.id}
              id={chosen.id}
              selected={episodes}
              change={(ids) => {
                setEpisodes(ids);
                setReviewed(false);
              }}
              busy={busy}
            />
          )}
          <label className="check-label">
            <input
              type="checkbox"
              checked={reviewed}
              disabled={busy}
              onChange={(e) => setReviewed(e.target.checked)}
            />
            I checked that this file belongs to this{" "}
            {kind === "movie" ? "movie" : "show and selected episodes"}.
          </label>
          <p className="field-help">
            ReelScarey will read the video, verify its checksum and inspect it
            with FFprobe. Changed files, existing registrations and conflicting
            downloads are refused. Files are never moved, renamed or replaced.
          </p>
        </section>
      )}
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
      <div className="import-actions">
        <Button
          type="submit"
          disabled={
            busy ||
            !chosen ||
            !reviewed ||
            (kind === "series" && !episodes.length)
          }
        >
          {busy ? "Working…" : "Verify & add in place"}
        </Button>
        <Dialog.Close>
          <Button type="button" variant="soft" disabled={busy}>
            Cancel
          </Button>
        </Dialog.Close>
      </div>
    </form>
  );
}
function EpisodePicker({
  id,
  selected,
  change,
  busy,
}: {
  id: number;
  selected: number[];
  change: (ids: number[]) => void;
  busy: boolean;
}) {
  const resource = useResource<TVSeries>(`/library/series/${id}`);
  const [fresh, setFresh] = useState<TVSeries>();
  const show = fresh ?? resource.data;
  const [number, setNumber] = useState<number>();
  const season =
    number ??
    show?.seasons.find((s) => s.number === 1)?.number ??
    show?.seasons[0]?.number;
  const episodes = useResource<TVSeasonDetails>(
    season === undefined ? null : `/library/series/${id}/seasons/${season}`,
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  async function load() {
    if (!show || season === undefined) return;
    setLoading(true);
    setError("");
    try {
      setFresh(
        await api<TVSeries>(`/library/series/${id}/seasons/${season}/refresh`, {
          method: "POST",
          body: JSON.stringify({ revision: show.revision }),
        }),
      );
      change([]);
      episodes.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setLoading(false);
    }
  }
  return (
    <>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {show && (
        <>
          <label>
            Season{" "}
            <select
              aria-label="Import season"
              value={season ?? ""}
              disabled={busy || loading}
              onChange={(e) => {
                setNumber(Number(e.target.value));
                change([]);
              }}
            >
              {show.seasons.map((s) => (
                <option value={s.number} key={s.id}>
                  {s.title}
                </option>
              ))}
            </select>
          </label>
          <RequestState
            loading={episodes.loading}
            error={episodes.error}
            retry={episodes.reload}
          />
          {episodes.data && !episodes.data.season.loaded_at && (
            <Button
              type="button"
              variant="soft"
              disabled={busy || loading}
              onClick={() => void load()}
            >
              {loading ? "Loading episodes…" : "Load season episodes from TMDb"}
            </Button>
          )}
          <div className="import-episodes">
            {episodes.data?.episodes.map((ep) => (
              <label className="check-label" key={ep.id}>
                <input
                  type="checkbox"
                  disabled={
                    busy ||
                    loading ||
                    ep.available ||
                    (!selected.includes(ep.id) && selected.length >= 20)
                  }
                  checked={selected.includes(ep.id)}
                  onChange={(e) =>
                    change(
                      e.target.checked
                        ? [...selected, ep.id]
                        : selected.filter((id) => id !== ep.id),
                    )
                  }
                />
                E{String(ep.number).padStart(2, "0")} · {ep.title}
                {ep.available ? " · Already in library" : ""}
              </label>
            ))}
          </div>
          <p className="field-help">
            Select every episode in this file: up to 20 consecutive episodes
            from one season. Filename hints never select episodes for you.
          </p>
        </>
      )}
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
    </>
  );
}
