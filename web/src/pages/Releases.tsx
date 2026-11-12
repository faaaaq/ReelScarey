import { useId, useMemo, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { ArrowLeft, Ban, ListFilter, Search, Upload } from "lucide-react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import {
  api,
  messageFor,
  type MediaItem,
  type IntegrationStatus,
  type Release,
} from "../api";
import { PageHeading, RequestState } from "../components";
import {
  eventStateKind,
  type Event as HistoryEvent,
} from "../components/EventRow";
import {
  Alert,
  EmptyState,
  Modal,
  ModalClose,
  PageBody,
  PageToolbar,
  ToolbarButton,
  ToolbarSection,
  ToolbarSeparator,
  episodeCode,
  type SortState,
} from "../ui";
import { useResource } from "../useResource";
import type { MovieVersionSettings } from "./MovieVersions";
import { ToolbarMenu } from "./activity/ToolbarMenu";
import "./Releases.css";
import {
  ReleaseResultsTable,
  normaliseTitle,
  type ReleaseSortKey,
} from "./details/ReleaseResults";
export { age, publishedTime, releaseInfoURL } from "./details/ReleaseResults";

export function Releases() {
  const [params] = useSearchParams();
  return <ReleaseBrowser key={params.toString()} />;
}

function ReleaseBrowser() {
  const [params] = useSearchParams();
  const tv = params.has("series");
  const movieID = Number(params.get(tv ? "series" : "movie"));
  const season = Number(params.get("season"));
  const episode = params.has("episode") ? Number(params.get("episode")) : 0;
  const valid =
    Number.isSafeInteger(movieID) &&
    movieID > 0 &&
    (!tv ||
      (params.has("season") &&
        Number.isInteger(season) &&
        season >= 0 &&
        season <= 10000 &&
        Number.isInteger(episode) &&
        episode >= 0 &&
        episode <= 100000));
  const movie = useResource<MediaItem>(
    valid ? `/library/${tv ? "series" : "movies"}/${movieID}` : null,
  );
  const indexer = useResource<IntegrationStatus>("/indexers/status");
  const client = useResource<IntegrationStatus>("/integrations/qbittorrent");
  const sab = useResource<IntegrationStatus>("/integrations/sabnzbd");
  // Best effort: recent failed download events warn before a repeat attempt.
  // A missing or slow history never blocks the search results.
  const history = useResource<{ items: HistoryEvent[] }>("/events");
  const failedTitles = useMemo(() => {
    const failed = new Set<string>();
    for (const event of history.data?.items ?? []) {
      if (event.kind !== "download") continue;
      if (eventStateKind(event.state) !== "danger") continue;
      failed.add(normaliseTitle(event.title));
    }
    return failed;
  }, [history.data]);
  // The included client makes torrent downloads ready without any setup.
  const clients = useResource<{
    items: { builtin?: boolean; enabled: boolean }[];
  }>("/download-clients");
  const builtinReady = (clients.data?.items ?? []).some(
    (item) => item.builtin && item.enabled,
  );
  const replacementFiles = useResource<{
    items: { id: number; path: string }[];
  }>(valid ? `/organization/files?media_id=${movieID}` : null);
  const versions = useResource<MovieVersionSettings>(
    valid && !tv ? `/library/movies/${movieID}/versions` : null,
  );
  const [purpose, setPurpose] = useState(() => {
    const fromURL = params.get("purpose") ?? "";
    if (tv) return /^[1-9][0-9]*$/.test(fromURL) ? fromURL : "";
    const version = params.get("movie_version") ?? "";
    if (["hd", "uhd", "additional"].includes(version)) return version;
    if (["hd", "uhd", "additional"].includes(fromURL)) return fromURL;
    return /^[1-9][0-9]*$/.test(fromURL) ? fromURL : "";
  });
  const replacementID = /^[1-9][0-9]*$/.test(purpose) ? purpose : "";
  // Only files of the season (and episode) being reviewed may be replaced from
  // here, so the purpose list can never point at another title's file.
  const purposeFiles = (replacementFiles.data?.items ?? []).filter((f) => {
    const path = f.path.replaceAll("\\", "/");
    if (!tv) return true;
    if (season === 0) return /\/Specials\//i.test(path);
    const marker = `Season ${String(season).padStart(2, "0")}`;
    if (!path.includes(marker) && !path.includes(`Season ${season}/`))
      return false;
    if (!episode) return true;
    const code = `S${String(season).padStart(2, "0")}E${String(episode).padStart(2, "0")}`;
    return path.includes(code);
  });
  const movieVersion =
    !tv && ["hd", "uhd", "additional"].includes(purpose) ? purpose : "";
  const [input, setInput] = useState("");
  const [items, setItems] = useState<Release[]>();
  const [searching, setSearching] = useState(false);
  const [pending, setPending] = useState("");
  const [error, setError] = useState("");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [attachOpen, setAttachOpen] = useState(false);
  const [protocol, setProtocol] = useState("all");
  const [sort, setSort] = useState<SortState<ReleaseSortKey>>({
    key: "rank",
    direction: "asc",
  });
  const purposeID = useId();
  const navigate = useNavigate();
  const target = tv
    ? episode
      ? episodeCode(season, episode)
      : `Season ${season}`
    : "";
  const suggested = movie.data
    ? tv
      ? `${movie.data.title} S${String(season).padStart(2, "0")}${episode ? `E${String(episode).padStart(2, "0")}` : ""}`
      : `${movie.data.title}${movie.data.year ? ` ${movie.data.year}` : ""}`
    : "";
  async function search(e: FormEvent) {
    e.preventDefault();
    if (searching || !valid) return;
    setSearching(true);
    setError("");
    setItems(undefined);
    try {
      const out = await api<{ items: Release[]; warnings?: string[] }>(
        tv
          ? `/library/series/${movieID}/seasons/${season}/releases?${replacementID ? `replacement_file_id=${replacementID}&` : ""}${episode ? `episode=${episode}&` : ""}query=${encodeURIComponent(input.trim() || suggested)}`
          : `/releases?movie_id=${movieID}&query=${encodeURIComponent(input.trim() || suggested)}`,
      );
      setItems(out.items);
      setWarnings(out.warnings ?? []);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setSearching(false);
    }
  }
  async function download(id: string) {
    if (pending) return;
    setPending(id);
    setError("");
    try {
      await api("/downloads", {
        method: "POST",
        body: JSON.stringify({
          release_id: id,
          ...(movieVersion ? { movie_version: movieVersion } : {}),
          ...(replacementID
            ? { replacement_file_id: Number(replacementID) }
            : {}),
        }),
      });
      navigate("/activity");
    } catch (e) {
      setError(
        `${messageFor(e)} Check Activity before retrying if the connection was interrupted.`,
      );
    } finally {
      setPending("");
    }
  }
  const configuredFor = (release: Release) =>
    release.protocol === "usenet"
      ? sab.data?.configured
      : client.data?.configured || builtinReady;
  const back = tv
    ? `/series/${movieID}`
    : valid
      ? `/movies/${movieID}`
      : "/movies";
  const showPurpose = !tv || !!replacementFiles.data?.items.length;
  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={ArrowLeft}
            label={tv ? "Back to show" : "Back to movie"}
            to={back}
          />
          <ToolbarSeparator />
          <ToolbarButton
            icon={Upload}
            label="Attach torrent"
            disabled={!valid || !!replacementID}
            title={
              replacementID
                ? "Replacements must come from a searched release."
                : undefined
            }
            onClick={() => setAttachOpen(true)}
          />
        </ToolbarSection>
        <ToolbarSection align="right">
          <ToolbarButton
            icon={Ban}
            label="Blacklist"
            to="/releases/blacklist"
          />
          <ToolbarMenu
            icon={ListFilter}
            label="Filter"
            menuLabel="Protocol"
            value={protocol}
            options={[
              { value: "all", label: "All protocols" },
              { value: "torrent", label: "Torrent" },
              { value: "usenet", label: "Usenet" },
            ]}
            onChange={setProtocol}
            disabled={!items?.length}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading
          title={
            movie.data
              ? `Releases for ${movie.data.title}`
              : tv
                ? "TV releases"
                : "Movie releases"
          }
          description={target || undefined}
        />
        {!valid && (
          <Alert kind="danger" role="alert">
            Choose a valid {tv ? "episode or season" : "movie"} from your
            library first.
          </Alert>
        )}
        <RequestState
          loading={
            movie.loading || indexer.loading || client.loading || sab.loading
          }
          error={movie.error || indexer.error || client.error || sab.error}
          retry={() => {
            movie.reload();
            indexer.reload();
            client.reload();
            sab.reload();
          }}
        />
        {indexer.data && !indexer.data.configured && (
          <EmptyState
            icon={Search}
            title="No indexer connected"
            actions={
              <Button asChild>
                <Link to="/indexers">Connect indexer</Link>
              </Button>
            }
          >
            <p>
              Add a tracker, RSS feed, Torznab or Newznab endpoint to search.
            </p>
          </EmptyState>
        )}
        {movie.data && indexer.data?.configured && (
          <form className="rel-search" onSubmit={search}>
            {showPurpose && replacementID ? (
              <div className="rel-purpose">
                <span className="rel-purpose-label">Replacing</span>
                <p className="rel-purpose-file">
                  {replacementFiles.data?.items.find(
                    (f) => String(f.id) === replacementID,
                  )?.path ?? `Registered file #${replacementID}`}
                </p>
              </div>
            ) : (
              showPurpose && (
                <div className="rel-purpose">
                  <label htmlFor={purposeID}>Download purpose</label>
                  <select
                    id={purposeID}
                    value={purpose}
                    disabled={searching || !!pending}
                    onChange={(e) => setPurpose(e.target.value)}
                  >
                    <option value="">
                      Normal import (existing files are protected)
                    </option>
                    {!tv && (
                      <option value="additional">
                        Keep existing files and add another version
                      </option>
                    )}
                    {!tv && versions.data?.enabled && (
                      <>
                        <option value="hd">HD version (up to 1080p)</option>
                        <option value="uhd">4K version (2160p)</option>
                      </>
                    )}
                    {purposeFiles.map((f) => (
                      <option key={f.id} value={f.id}>
                        Acquire replacement for {f.path}
                      </option>
                    ))}
                  </select>
                </div>
              )
            )}
            <div className="rel-query">
              <TextField.Root
                aria-label="Release search"
                value={input}
                onChange={(e) => setInput(e.target.value)}
                placeholder={suggested}
                maxLength={200}
                disabled={searching || !!pending}
              />
              <Button type="submit" disabled={searching || !!pending}>
                <Search size={16} aria-hidden="true" />
                {searching ? "Searching…" : "Search releases"}
              </Button>
            </div>
            {showPurpose && (
              <p className="rs-muted rs-small">
                {replacementID
                  ? "Only this file is replaced. The existing file is verified and kept until the new one passes review."
                  : movieVersion
                    ? "This version gets its own filename; existing copies are never overwritten."
                    : "Existing library files are never replaced."}
              </p>
            )}
          </form>
        )}
        {movie.data &&
          indexer.data?.configured &&
          client.data &&
          sab.data &&
          !client.data.configured &&
          !sab.data.configured &&
          !builtinReady && (
            <Alert kind="warning">
              <p>
                <Link to="/download-clients">Turn on the built-in client</Link>{" "}
                or connect qBittorrent or SABnzbd to download.
              </p>
            </Alert>
          )}
        {error && (
          <Alert kind="danger" role="alert">
            {error}
          </Alert>
        )}
        {warnings.length > 0 && (
          <Alert kind="warning" title="Some indexers need attention">
            <p>Results from the available sources are shown.</p>
            {warnings.map((warning) => (
              <p key={warning}>{warning}</p>
            ))}
            <p>
              <Link to="/indexers">Review indexer connections</Link>
            </p>
          </Alert>
        )}
        {movie.data && indexer.data?.configured && items && (
          <ReleaseResultsTable
            items={items}
            protocol={protocol}
            sort={sort}
            onSort={setSort}
            pending={pending}
            onDownload={(id) => void download(id)}
            readyFor={configuredFor}
            failedTitles={failedTitles}
            tv={tv}
          />
        )}
      </PageBody>
      {valid && !replacementID && (
        <AttachTorrent
          open={attachOpen}
          onOpenChange={setAttachOpen}
          tv={tv}
          movieID={movieID}
          season={season}
          movieVersion={movieVersion}
          onQueued={() => navigate("/activity")}
        />
      )}
    </>
  );
}

function AttachTorrent({
  open,
  onOpenChange,
  tv,
  movieID,
  season,
  movieVersion,
  onQueued,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tv: boolean;
  movieID: number;
  season: number;
  movieVersion: string;
  onQueued: () => void;
}) {
  const [torrentFile, setTorrentFile] = useState<File>();
  const [episodeInput, setEpisodeInput] = useState("");
  const [attaching, setAttaching] = useState(false);
  const [error, setError] = useState("");
  const fileID = useId();
  const episodesID = useId();
  async function attach() {
    if (!torrentFile || attaching) return;
    setAttaching(true);
    setError("");
    try {
      await api(
        tv
          ? `/library/series/${movieID}/manual-torrent?season=${season}${episodeInput.trim() ? `&episodes=${episodeInput.trim()}` : ""}`
          : `/library/movies/${movieID}/manual-torrent${movieVersion ? `?movie_version=${movieVersion}` : ""}`,
        {
          method: "POST",
          body: torrentFile,
          headers: { "Content-Type": "application/x-bittorrent" },
          signal: AbortSignal.timeout(60000),
        },
      );
      onQueued();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setAttaching(false);
    }
  }
  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title="Attach a .torrent file"
      description={`Queue an exact release you already have for this ${tv ? "series" : "movie"}. It goes through the same duplicate checks, client submission and verified import as a searched release.`}
      footer={
        <>
          <ModalClose>
            <Button variant="soft" color="gray">
              Cancel
            </Button>
          </ModalClose>
          <Button
            disabled={!torrentFile || attaching}
            onClick={() => void attach()}
          >
            {attaching ? "Attaching…" : "Attach and queue"}
          </Button>
        </>
      }
    >
      <div className="rel-attach-field">
        <label htmlFor={fileID}>Torrent file</label>
        <input
          id={fileID}
          type="file"
          accept=".torrent,application/x-bittorrent"
          onChange={(e) => setTorrentFile(e.target.files?.[0])}
          disabled={attaching}
        />
      </div>
      {tv && (
        <div className="rel-attach-field">
          <label htmlFor={episodesID}>
            Episode numbers (optional, e.g. 2 or 2,3)
          </label>
          <input
            id={episodesID}
            type="text"
            value={episodeInput}
            onChange={(e) => setEpisodeInput(e.target.value)}
            placeholder="Whole season when empty"
            disabled={attaching}
          />
          <p className="rs-form-help">
            Without episode numbers the torrent counts as a season pack for the
            missing episodes. Episode mapping is still reviewed in Activity
            before import.
          </p>
        </div>
      )}
      {error && (
        <Alert kind="danger" role="alert">
          {error}
        </Alert>
      )}
    </Modal>
  );
}
