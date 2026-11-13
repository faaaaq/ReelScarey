import { Fragment, useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  Disc3,
  FolderInput,
  LayoutGrid,
  ListMusic,
  Plus,
  RefreshCw,
  Search,
  Server,
  Users,
} from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import {
  Alert,
  EmptyState,
  FieldSet,
  Label,
  Modal,
  PageBody,
  PageToolbar,
  Table,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { Pagination } from "../movieComponents";
import { RequestState } from "../components";
import { groupID, groupedByLetter, sortLetters } from "../libraryGroups";
import {
  api,
  messageFor,
  type MusicAcquisitionJob,
  type MusicAcquisitionJobList,
  type MusicAlbumPage,
  type MusicArtist,
  type MusicArtistPage,
  type MusicArtistTrackPage,
  type MusicSearchResults,
} from "../api";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
import {
  AcquisitionProgress,
  AlbumDownloadModal,
  type AlbumDownloadTarget,
} from "../musicDownload";
import { AlbumTile, ArtistTile, LibraryTrackTable } from "./Music";
import "./Library.css";
import "./Music.css";

type View = "albums" | "artists" | "tracks";

function ArtistType({ artist }: { artist: MusicArtist }) {
  return (
    <>{[artist.type, artist.country].filter(Boolean).join(" · ") || "—"}</>
  );
}

export function MusicLibrary() {
  const [params, setParams] = useSearchParams();
  const pageValue = params.get("page") ?? "";
  const page = /^[1-9][0-9]*$/.test(pageValue) ? Number(pageValue) : 1;
  const query = params.get("query") ?? "";
  const view: View =
    params.get("view") === "albums"
      ? "albums"
      : params.get("view") === "tracks"
        ? "tracks"
        : "artists";
  const [input, setInput] = useState(query);
  const [open, setOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [results, setResults] = useState<MusicArtist[]>([]);
  const [searching, setSearching] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [download, setDownload] = useState<AlbumDownloadTarget | null>(null);

  const apiParams = new URLSearchParams({ page: String(page) });
  if (query) apiParams.set("query", query);
  const albums = useResource<MusicAlbumPage>(
    view === "albums" ? `/library/music/albums?${apiParams.toString()}` : null,
  );
  // The artist list renders on a single page with an A–Z index. Ask for the
  // whole catalog; a server that rejects `page=all` past its cap (or ignores
  // it) falls back to the paginated view, keyed on the machine-readable code.
  const artistsParams = new URLSearchParams();
  if (query) artistsParams.set("query", query);
  artistsParams.set("page", "all");
  const artistsAll = useResource<MusicArtistPage>(
    view === "artists"
      ? `/library/music/artists?${artistsParams.toString()}`
      : null,
  );
  const artistsCapped =
    artistsAll.code === "library_too_large" ||
    (!!artistsAll.data && artistsAll.data.total > artistsAll.data.items.length);
  const artistsPagedParams = new URLSearchParams(artistsParams);
  artistsPagedParams.set("page", String(page));
  const artistsPaged = useResource<MusicArtistPage>(
    view === "artists" && artistsCapped
      ? `/library/music/artists?${artistsPagedParams.toString()}`
      : null,
  );
  const artists = artistsCapped ? artistsPaged : artistsAll;
  const tracksParams = new URLSearchParams(apiParams);
  tracksParams.set("limit", "100");
  const tracks = useResource<MusicArtistTrackPage>(
    view === "tracks"
      ? `/library/music/tracks?${tracksParams.toString()}`
      : null,
  );
  const downloads = useLiveResource<MusicAcquisitionJobList>(
    "/library/music/acquisitions?page=1",
    5000,
  );
  const [albumJobID, setAlbumJobID] = useState<number | null>(null);
  const albumJob = useLiveResource<MusicAcquisitionJob>(
    albumJobID ? `/library/music/acquisitions/${albumJobID}` : null,
    2000,
  );

  useEffect(() => setInput(query), [query]);

  function change(key: string, value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    if (key !== "page") next.delete("page");
    setParams(next);
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    change("query", input.trim());
  }

  function reload() {
    albums.reload();
    artists.reload();
    tracks.reload();
    downloads.reload();
  }

  async function search(e: FormEvent) {
    e.preventDefault();
    if (searching || !searchQuery.trim()) return;
    setSearching(true);
    setError("");
    try {
      const data = await api<MusicSearchResults>(
        `/metadata/music/artists?query=${encodeURIComponent(searchQuery.trim())}&page=1`,
      );
      setResults(data.items);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setSearching(false);
    }
  }

  async function add(musicbrainzID: string) {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api("/library/music/artists", {
        method: "POST",
        body: JSON.stringify({ musicbrainz_id: musicbrainzID }),
      });
      setOpen(false);
      setSearchQuery("");
      setResults([]);
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  const albumItems = albums.data?.items ?? [];
  const artistItems = artists.data?.items ?? [];
  const trackItems = tracks.data?.items ?? [];
  const activeResource =
    view === "albums" ? albums : view === "artists" ? artists : tracks;
  const ready =
    !activeResource.loading && !activeResource.error && !!activeResource.data;
  const fallbackTotal =
    view === "albums"
      ? (albums.data?.total ?? 0)
      : view === "artists"
        ? (artists.data?.total ?? 0)
        : (tracks.data?.total ?? 0);
  const limit =
    view === "albums"
      ? (albums.data?.limit ?? 48)
      : view === "artists"
        ? (artists.data?.limit ?? 48)
        : (tracks.data?.limit ?? 100);
  const pages = limit > 0 ? Math.ceil(fallbackTotal / limit) : 1;
  const currentPage =
    view === "albums"
      ? (albums.data?.page ?? page)
      : view === "artists"
        ? (artists.data?.page ?? page)
        : (tracks.data?.page ?? page);
  const albumGroups = groupedByLetter(albumItems, (album) => album.title);
  const artistGroups = groupedByLetter(artistItems, (artist) => artist.name);
  const letters = sortLetters(
    (view === "albums" ? albumGroups : artistGroups).map(([letter]) => letter),
  );
  const noun =
    view === "albums" ? "albums" : view === "artists" ? "artists" : "tracks";
  const activeJobs =
    downloads.data?.items.filter(
      (job) => job.state === "queued" || job.state === "running",
    ) ?? [];
  const attentionJobs =
    downloads.data?.items.filter(
      (job) => job.state === "failed" || !!job.error,
    ) ?? [];

  return (
    <>
      <PageToolbar label="Music actions">
        <ToolbarSection>
          <ToolbarButton
            icon={Plus}
            label="Add artist"
            onClick={() => setOpen(true)}
          />
          <ToolbarButton
            icon={FolderInput}
            label="Library Import"
            title="Scan an existing music folder and register files"
            to="/music/import"
          />
          <ToolbarButton
            icon={Search}
            label="Discover"
            title="Download from links, browse charts and monitor playlists"
            to="/music/discover"
          />
          <ToolbarButton
            icon={Server}
            label="Server"
            title="Optional Subsonic-compatible music access"
            to="/music/server"
          />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={activeResource.loading}
            onClick={reload}
          />
        </ToolbarSection>
        <ToolbarSection align="right">
          <ToolbarButton
            icon={LayoutGrid}
            label="Albums"
            active={view === "albums"}
            onClick={() => change("view", "albums")}
          />
          <ToolbarButton
            icon={Users}
            label="Artists"
            active={view === "artists"}
            onClick={() => change("view", "artists")}
          />
          <ToolbarButton
            icon={ListMusic}
            label="Tracks"
            active={view === "tracks"}
            onClick={() => change("view", "tracks")}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="lib-body">
        <div className="lib-head">
          <h1 className="lib-title">Music</h1>
          <form onSubmit={submit} className="lib-search" role="search">
            <TextField.Root
              aria-label="Filter saved music"
              placeholder="Filter by title or artist"
              maxLength={200}
              value={input}
              onChange={(e) => setInput(e.target.value)}
            >
              <TextField.Slot>
                <Search size={15} aria-hidden="true" />
              </TextField.Slot>
            </TextField.Root>
            <Button
              type="submit"
              variant="soft"
              aria-label="Apply music filter"
            >
              Apply
            </Button>
          </form>
        </div>

        {activeJobs.length > 0 && (
          <Alert kind="info" title="Music downloads running">
            {activeJobs.length}{" "}
            {activeJobs.length === 1 ? "job is" : "jobs are"} being processed
            automatically.{" "}
            <Link to="/music/discover">View the download queue</Link>.
          </Alert>
        )}
        {attentionJobs.length > 0 && (
          <Alert kind="warning" title="Music downloads need attention">
            {attentionJobs.length}{" "}
            {attentionJobs.length === 1 ? "job has" : "jobs have"} an issue.{" "}
            <Link to="/music/discover">Review the queue</Link>.
          </Alert>
        )}

        {albumJob.data && (
          <FieldSet
            legend={`Album download · ${albumJob.data.title}`}
            actions={
              <Button
                variant="soft"
                color="gray"
                size="1"
                onClick={() => setAlbumJobID(null)}
              >
                Hide
              </Button>
            }
          >
            <AcquisitionProgress
              job={albumJob.data}
              label="Album download progress"
            />
          </FieldSet>
        )}

        {view !== "tracks" && ready && letters.length > 1 && (
          <nav
            className="lib-az"
            aria-label={`Jump to ${view === "albums" ? "album" : "artist"} letter`}
          >
            {letters.map((letter) => (
              <button
                key={letter}
                type="button"
                aria-label={`Jump to ${letter === "#" ? "other" : letter}`}
                onClick={() =>
                  document
                    .getElementById(groupID(letter))
                    ?.scrollIntoView({ block: "start" })
                }
              >
                {letter}
              </button>
            ))}
          </nav>
        )}

        <RequestState
          loading={activeResource.loading}
          error={activeResource.error}
        code={activeResource.code}
          retry={reload}
        />
        {ready &&
          (view === "albums" ? (
            albumItems.length ? (
              <ul className="lib-grid" aria-label="Music albums">
                {albumGroups.map(([letter, rows]) => (
                  <Fragment key={letter}>
                    <li className="lib-group-row">
                      <h2 className="lib-group-heading" id={groupID(letter)}>
                        {letter}
                      </h2>
                    </li>
                    {rows.map((album) => (
                      <AlbumTile
                        key={album.id}
                        album={album}
                        showArtist
                        onDownload={(item) =>
                          setDownload({
                            albumID: item.id,
                            title: item.title,
                            artist: item.artist_name,
                          })
                        }
                      />
                    ))}
                  </Fragment>
                ))}
              </ul>
            ) : query ? (
              <EmptyState
                icon={Disc3}
                title="No matching albums"
                actions={
                  <Button onClick={() => setParams({})}>
                    Clear music filter
                  </Button>
                }
              >
                Try a different title or artist.
              </EmptyState>
            ) : (
              <EmptyState
                icon={Disc3}
                title="No albums yet"
                actions={
                  <>
                    <Button asChild>
                      <Link to="/music/import">Import existing music</Link>
                    </Button>
                    <Button variant="soft" onClick={() => setOpen(true)}>
                      Add artist
                    </Button>
                  </>
                }
              >
                Scan an existing music folder, or search MusicBrainz and add an
                artist to the catalog.
              </EmptyState>
            )
          ) : view === "artists" ? (
            artistItems.length ? (
              <ul className="lib-grid" aria-label="Music artists">
                {artistGroups.map(([letter, rows]) => (
                  <Fragment key={letter}>
                    <li className="lib-group-row">
                      <h2 className="lib-group-heading" id={groupID(letter)}>
                        {letter}
                      </h2>
                    </li>
                    {rows.map((artist) => (
                      <ArtistTile key={artist.id} artist={artist} />
                    ))}
                  </Fragment>
                ))}
              </ul>
            ) : query ? (
              <EmptyState
                icon={Search}
                title="No matching artists"
                actions={
                  <Button onClick={() => setParams({})}>
                    Clear music filter
                  </Button>
                }
              >
                Try a different artist name.
              </EmptyState>
            ) : (
              <EmptyState
                icon={Users}
                title="No artists yet"
                actions={
                  <>
                    <Button asChild>
                      <Link to="/music/import">Import existing music</Link>
                    </Button>
                    <Button variant="soft" onClick={() => setOpen(true)}>
                      Add artist
                    </Button>
                  </>
                }
              >
                Scan an existing music folder, or search MusicBrainz and add an
                artist to the catalog.
              </EmptyState>
            )
          ) : trackItems.length ? (
            <LibraryTrackTable tracks={trackItems} />
          ) : query ? (
            <EmptyState
              icon={Search}
              title="No matching tracks"
              actions={
                <Button onClick={() => setParams({})}>
                  Clear music filter
                </Button>
              }
            >
              Try a different title, album or artist.
            </EmptyState>
          ) : (
            <EmptyState icon={ListMusic} title="No registered tracks">
              Import or register music files to see tracks here.
            </EmptyState>
          ))}

        {!activeResource.error && (
          <footer className="lib-footer">
            <p role="status" className="lib-totals">
              {ready
                ? `${fallbackTotal} matching ${
                    fallbackTotal === 1 ? noun.slice(0, -1) : noun
                  }${
                    view === "tracks"
                      ? " · registered tracks"
                      : fallbackTotal > limit
                        ? ` · showing ${(currentPage - 1) * limit + 1}–${(currentPage - 1) * limit + (view === "albums" ? albumItems.length : artistItems.length)}`
                        : " · all shown"
                  }`
                : ""}
            </p>
            {ready && (
              <Pagination
                page={currentPage}
                pages={pages}
                onPage={(value) => change("page", String(value))}
              />
            )}
          </footer>
        )}
      </PageBody>
      <Modal
        open={open}
        onOpenChange={setOpen}
        title="Add artist"
        description="Search MusicBrainz for an artist."
        size="large"
        footer={
          <Button variant="soft" color="gray" onClick={() => setOpen(false)}>
            Close
          </Button>
        }
      >
        <form onSubmit={(e) => void search(e)} className="mus-search">
          <TextField.Root
            aria-label="Artist name"
            placeholder="Search MusicBrainz"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            maxLength={200}
          />
          <Button type="submit" disabled={searching || !searchQuery.trim()}>
            <Search size={15} aria-hidden="true" /> Search
          </Button>
        </form>
        {error && <Alert kind="danger">{error}</Alert>}
        {results.length > 0 && (
          <Table label="MusicBrainz search results">
            <thead>
              <tr>
                <th scope="col">Artist</th>
                <th scope="col">Type</th>
                <th scope="col" className="is-actions">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {results.map((artist) => (
                <tr key={artist.musicbrainz_id}>
                  <td>
                    {artist.name}
                    {artist.disambiguation && (
                      <span className="rs-muted">
                        {" "}
                        · {artist.disambiguation}
                      </span>
                    )}
                    {artist.in_library && (
                      <>
                        {" "}
                        <Label kind="success">In catalog</Label>
                      </>
                    )}
                  </td>
                  <td>
                    <ArtistType artist={artist} />
                  </td>
                  <td className="is-actions">
                    <Button
                      size="1"
                      disabled={pending || artist.in_library}
                      onClick={() => void add(artist.musicbrainz_id)}
                    >
                      {artist.in_library ? "Added" : "Add"}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Modal>
      <AlbumDownloadModal
        target={download}
        onClose={() => setDownload(null)}
        onJob={(job) => setAlbumJobID(job.id)}
        onStarted={() => downloads.reload()}
      />
    </>
  );
}
