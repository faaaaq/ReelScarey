import { Fragment, useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  ArrowLeft,
  Disc3,
  ExternalLink,
  FolderInput,
  ListMusic,
  Play,
  Plus,
  RefreshCw,
  Search,
} from "lucide-react";
import { Link, useParams, useSearchParams } from "react-router-dom";
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
import { PageHeading, RequestState } from "../components";
import { Pagination } from "../movieComponents";
import { groupID, groupedByLetter, sortLetters } from "../libraryGroups";
import {
  api,
  messageFor,
  type MusicAlbum,
  type MusicAlbumDetails,
  type MusicAlbumPreferences,
  type MusicArtist,
  type MusicArtistDetails,
  type MusicArtistPage,
  type MusicArtistTrack,
  type MusicArtistTrackPage,
  type MusicImportJob,
  type MusicJob,
  type MusicRelease,
  type MusicReleaseSearch,
  type MusicSearchResults,
} from "../api";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
import { useMusicPlayer, type PlayerTrack } from "../musicPlayer";
import "./Library.css";
import "./Music.css";

function formatDuration(ms: number) {
  if (ms <= 0) return "—";
  const total = Math.round(ms / 1000);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

function ArtistType({ artist }: { artist: MusicArtist }) {
  return (
    <>{[artist.type, artist.country].filter(Boolean).join(" · ") || "—"}</>
  );
}

function statusKind(status: string, monitored: boolean) {
  if (status === "available") return "success";
  return monitored ? "danger" : "disabled";
}

function ArtistTile({ artist }: { artist: MusicArtist }) {
  const [failed, setFailed] = useState(false);
  const initial = artist.name.trim().charAt(0).toUpperCase() || "?";
  const label =
    artist.status === "available" ? "In library" : "No registered files";
  return (
    <li className="lib-card">
      <Link
        className="lib-card-art mus-card-art"
        to={`/music/artists/${artist.id}`}
        aria-label={`Open ${artist.name}`}
      >
        {artist.image_url && !failed ? (
          <img
            className="mus-tile-img"
            src={artist.image_url}
            alt=""
            loading="lazy"
            referrerPolicy="no-referrer"
            onError={() => setFailed(true)}
          />
        ) : (
          <span
            className="mus-tile-fallback mus-tile-initial"
            aria-hidden="true"
          >
            {initial}
          </span>
        )}
      </Link>
      <span
        className={`lib-status-bar is-${statusKind(artist.status, artist.monitored)}`}
        title={label}
      />
      <div className="lib-card-info">
        <div className="lib-card-line">
          <h2 className="lib-card-title">
            <Link to={`/music/artists/${artist.id}`}>{artist.name}</Link>
          </h2>
        </div>
        <div className="lib-card-meta">
          <span>
            {artist.album_count} {artist.album_count === 1 ? "album" : "albums"}
          </span>
          <span>
            {[artist.type, artist.country].filter(Boolean).join(" · ") ||
              "Artist"}
          </span>
        </div>
        {artist.disambiguation && (
          <span className="lib-profile">{artist.disambiguation}</span>
        )}
        <span className="sr-only">{label}</span>
      </div>
    </li>
  );
}

export function Music() {
  const [params, setParams] = useSearchParams();
  const pageValue = params.get("page") ?? "";
  const page = /^[1-9][0-9]*$/.test(pageValue) ? Number(pageValue) : 1;
  const query = params.get("query") ?? "";
  const [input, setInput] = useState(query);
  const artistsParams = new URLSearchParams({ page: String(page) });
  if (query) artistsParams.set("query", query);
  const artists = useResource<MusicArtistPage>(
    `/library/music/artists?${artistsParams.toString()}`,
  );
  const [open, setOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [results, setResults] = useState<MusicArtist[]>([]);
  const [searching, setSearching] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

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
      artists.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  const data = artists.data;
  const items = data?.items ?? [];
  const groups = groupedByLetter(items, (artist) => artist.name);
  const letters = sortLetters(groups.map(([letter]) => letter));
  const ready = !artists.loading && !artists.error && !!data;
  const pages = data ? Math.ceil(data.total / data.limit) : 1;

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
            icon={RefreshCw}
            label="Refresh"
            spinning={artists.loading}
            onClick={artists.reload}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="lib-body">
        <div className="lib-head">
          <h1 className="lib-title">Music</h1>
          <form onSubmit={submit} className="lib-search" role="search">
            <TextField.Root
              aria-label="Filter saved artists"
              placeholder="Filter by artist"
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
              aria-label="Apply artist filter"
            >
              Apply
            </Button>
          </form>
        </div>

        {ready && letters.length > 1 && (
          <nav className="lib-az" aria-label="Jump to artist letter">
            {letters.map((letter) => (
              <button
                key={letter}
                type="button"
                aria-label={`Jump to ${letter === "#" ? "other artists" : letter}`}
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
          loading={artists.loading}
          error={artists.error}
          retry={artists.reload}
        />
        {ready &&
          (items.length ? (
            <ul className="lib-grid" aria-label="Music artists">
              {groups.map(([letter, rows]) => (
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
                  Clear artist filter
                </Button>
              }
            >
              Try a different artist name or clear the filter.
            </EmptyState>
          ) : (
            <EmptyState
              icon={Disc3}
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
          ))}

        {!artists.error && (
          <footer className="lib-footer">
            <p role="status" className="lib-totals">
              {ready && data
                ? `${data.total} matching ${
                    data.total === 1 ? "artist" : "artists"
                  }${
                    items.length
                      ? ` · showing ${(page - 1) * data.limit + 1}–${
                          (page - 1) * data.limit + items.length
                        }`
                      : ""
                  }`
                : ""}
            </p>
            {ready && data && (
              <Pagination
                page={data.page}
                pages={pages}
                onPage={(p) => change("page", String(p))}
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
    </>
  );
}

function AlbumTile({ album }: { album: MusicAlbum }) {
  const [failed, setFailed] = useState(false);
  const label =
    album.status === "available" ? "In library" : "No registered files";
  const type =
    [album.primary_type, ...(album.secondary_types ?? [])]
      .filter(Boolean)
      .join(" · ") || "Release";
  return (
    <li className="lib-card">
      <Link
        className="lib-card-art mus-card-art"
        to={`/music/albums/${album.id}`}
        aria-label={`Open ${album.title}`}
      >
        {album.cover_url && !failed ? (
          <img
            className="mus-tile-img"
            src={album.cover_url}
            alt=""
            loading="lazy"
            referrerPolicy="no-referrer"
            onError={() => setFailed(true)}
          />
        ) : (
          <span className="mus-tile-fallback" aria-hidden="true">
            <Disc3 size={34} strokeWidth={1} />
          </span>
        )}
      </Link>
      <span
        className={`lib-status-bar is-${statusKind(album.status, album.monitored)}`}
        title={label}
      />
      <div className="lib-card-info">
        <div className="lib-card-line">
          <h2 className="lib-card-title">
            <Link to={`/music/albums/${album.id}`}>{album.title}</Link>
          </h2>
        </div>
        <div className="lib-card-meta">
          <span>{album.year ?? "Year unknown"}</span>
          <span>
            {album.track_count
              ? `${album.track_count} ${album.track_count === 1 ? "track" : "tracks"}`
              : "Tracks not loaded"}
          </span>
        </div>
        <span className="lib-profile">{type}</span>
        <span className="sr-only">{label}</span>
      </div>
    </li>
  );
}

function artistPlayerTrack(
  track: MusicArtistTrack,
  artistName: string,
): PlayerTrack | null {
  if (!track.file_id) return null;
  return {
    fileID: track.file_id,
    title: track.title,
    artist: artistName,
    album: track.album_title,
    coverURL: track.cover_url || undefined,
    durationMS: track.duration_ms,
    albumID: track.album_id,
  };
}

function ArtistTrackTable({
  tracks,
  label,
  artistName,
}: {
  tracks: MusicArtistTrack[];
  label: string;
  artistName: string;
}) {
  const player = useMusicPlayer();
  const playable = tracks.flatMap((track) => {
    const item = artistPlayerTrack(track, artistName);
    return item ? [item] : [];
  });
  function play(fileID: number) {
    const item = playable.find((candidate) => candidate.fileID === fileID);
    if (item) player.play(item, playable);
  }
  return (
    <Table label={label}>
      <thead>
        <tr>
          <th scope="col" className="is-shrink">
            #
          </th>
          <th scope="col">Title</th>
          <th scope="col">Album</th>
          <th scope="col">Duration</th>
          <th scope="col" className="is-actions">
            <span className="sr-only">Play</span>
          </th>
        </tr>
      </thead>
      <tbody>
        {tracks.map((track) => {
          const fileID = track.file_id;
          return (
            <tr
              key={
                track.id ??
                `${track.album_id}-${track.disc}-${track.number}-${track.title}`
              }
            >
              <td>
                {track.disc > 1
                  ? `${track.disc}.${track.number}`
                  : track.number}
              </td>
              <td>{track.title}</td>
              <td>
                <Link to={`/music/albums/${track.album_id}`}>
                  {track.album_title}
                </Link>
              </td>
              <td>{formatDuration(track.duration_ms)}</td>
              <td className="is-actions">
                {fileID ? (
                  <Button
                    size="1"
                    variant="ghost"
                    color="gray"
                    aria-label={`Play ${track.title}`}
                    onClick={() => play(fileID)}
                  >
                    <Play size={14} aria-hidden="true" />
                  </Button>
                ) : null}
              </td>
            </tr>
          );
        })}
      </tbody>
    </Table>
  );
}

export function MusicArtistPage() {
  const { id } = useParams();
  const details = useResource<MusicArtistDetails>(
    id ? `/library/music/artists/${id}` : null,
  );
  const topTracks = useResource<MusicArtistTrackPage>(
    id ? `/library/music/artists/${id}/tracks?limit=15` : null,
  );
  const [allOpen, setAllOpen] = useState(false);
  const [allPage, setAllPage] = useState(1);
  const allTracks = useResource<MusicArtistTrackPage>(
    allOpen && id
      ? `/library/music/artists/${id}/tracks?page=${allPage}&limit=50`
      : null,
  );
  const artist = details.data?.artist;
  const player = useMusicPlayer();
  const playableTop = (topTracks.data?.items ?? []).flatMap((track) => {
    const item = artistPlayerTrack(track, artist?.name ?? "");
    return item ? [item] : [];
  });
  const allPages = allTracks.data
    ? Math.ceil(allTracks.data.total / allTracks.data.limit)
    : 1;
  return (
    <>
      <PageToolbar label="Music artist actions">
        <ToolbarSection>
          <ToolbarButton icon={ArrowLeft} label="Artists" to="/music" />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="lib-body">
        <PageHeading
          title={artist?.name ?? "Music artist"}
          description={
            artist
              ? artist.overview ||
                [artist.type, artist.country].filter(Boolean).join(" · ") ||
                undefined
              : undefined
          }
        />
        <RequestState
          loading={details.loading}
          error={details.error}
          retry={details.reload}
        />
        {details.data &&
          (details.data.albums.length === 0 ? (
            <EmptyState icon={Disc3} title="No albums">
              MusicBrainz has no release groups for this artist.
            </EmptyState>
          ) : (
            <ul className="lib-grid" aria-label="Music albums">
              {details.data.albums.map((album) => (
                <AlbumTile key={album.id} album={album} />
              ))}
            </ul>
          ))}

        {details.data && (
          <section className="mus-section" aria-labelledby="artist-top-tracks">
            <div className="mus-section-head">
              <div>
                <h2 id="artist-top-tracks">Top tracks</h2>
                <p className="rs-muted rs-small">
                  Tracks with registered files, newest albums first.
                </p>
              </div>
              <div className="mus-section-actions">
                {playableTop.length > 0 && (
                  <Button
                    variant="soft"
                    onClick={() => player.play(playableTop[0], playableTop)}
                  >
                    <Play size={15} aria-hidden="true" />
                    Play top tracks
                  </Button>
                )}
                {topTracks.data &&
                  topTracks.data.total > topTracks.data.items.length && (
                    <Button
                      variant="soft"
                      onClick={() => {
                        setAllPage(1);
                        setAllOpen(true);
                      }}
                    >
                      <ListMusic size={15} aria-hidden="true" />
                      See every track ({topTracks.data.total})
                    </Button>
                  )}
              </div>
            </div>
            {topTracks.loading && <p className="rs-muted">Loading tracks…</p>}
            {topTracks.error && <Alert kind="danger">{topTracks.error}</Alert>}
            {topTracks.data &&
              (topTracks.data.items.length ? (
                <ArtistTrackTable
                  tracks={topTracks.data.items}
                  label="Top tracks"
                  artistName={artist?.name ?? ""}
                />
              ) : (
                <p className="rs-muted">
                  No tracks with registered files yet. Import or register this
                  artist's files to see them here.
                </p>
              ))}
          </section>
        )}
      </PageBody>
      <Modal
        open={allOpen}
        onOpenChange={(open) => {
          setAllOpen(open);
          if (!open) setAllPage(1);
        }}
        title="All tracks"
        description={
          artist
            ? `${artist.name} · tracks with registered files`
            : "Tracks with registered files"
        }
        size="xlarge"
        footer={
          <Button variant="soft" color="gray" onClick={() => setAllOpen(false)}>
            Close
          </Button>
        }
      >
        {allTracks.loading && <p className="rs-muted">Loading tracks…</p>}
        {allTracks.error && <Alert kind="danger">{allTracks.error}</Alert>}
        {allTracks.data &&
          (allTracks.data.items.length ? (
            <ArtistTrackTable
              tracks={allTracks.data.items}
              label="All tracks"
              artistName={artist?.name ?? ""}
            />
          ) : (
            <p className="rs-muted">No tracks with registered files.</p>
          ))}
        {allTracks.data && allTracks.data.total > 0 && (
          <div className="mus-modal-footer">
            <span className="rs-muted">
              {allTracks.data.total}{" "}
              {allTracks.data.total === 1 ? "track" : "tracks"} with registered
              files.
            </span>
            <Pagination
              page={allTracks.data.page}
              pages={allPages}
              onPage={setAllPage}
            />
          </div>
        )}
      </Modal>
    </>
  );
}

function formatBytes(bytes: number) {
  if (!bytes) return "—";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(value >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function MusicAlbumPage() {
  const { id } = useParams();
  const details = useResource<MusicAlbumDetails>(
    id ? `/library/music/albums/${id}` : null,
  );
  const album = details.data?.album;
  const tracks = details.data?.tracks ?? [];
  const multiDisc = new Set(tracks.map((track) => track.disc)).size > 1;
  const player = useMusicPlayer();
  const playableTracks: PlayerTrack[] =
    album?.id === undefined
      ? []
      : tracks.flatMap((track) =>
          track.file_id
            ? [
                {
                  fileID: track.file_id,
                  title: track.title,
                  artist: album.artist_name ?? "",
                  album: album.title,
                  coverURL: album.cover_url || undefined,
                  durationMS: track.duration_ms,
                  albumID: album.id,
                },
              ]
            : [],
        );
  function playTrack(fileID: number) {
    const item = playableTracks.find(
      (candidate) => candidate.fileID === fileID,
    );
    if (item) player.play(item, playableTracks);
  }
  const [releases, setReleases] = useState<MusicRelease[]>([]);
  const [releaseOpen, setReleaseOpen] = useState(false);
  const [releaseQuery, setReleaseQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [importPath, setImportPath] = useState("");
  const [importRoot, setImportRoot] = useState("Music");
  const [importID, setImportID] = useState<number | null>(null);
  const [downloadJobID, setDownloadJobID] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const importJob = useLiveResource<MusicImportJob>(
    importID ? `/library/music/imports/${importID}` : null,
    2000,
  );
  const downloadJob = useLiveResource<MusicJob>(
    downloadJobID ? `/library/music/downloads/${downloadJobID}` : null,
    3000,
  );
  const albumPrefs = useResource<MusicAlbumPreferences>(
    id ? `/library/music/albums/${id}/preferences` : null,
  );
  const [allowLossless, setAllowLossless] = useState(true);
  const [minBitrate, setMinBitrate] = useState("0");
  const [formats, setFormats] = useState("");
  const [prefsPending, setPrefsPending] = useState(false);

  useEffect(() => {
    if (!albumPrefs.data?.preferences) return;
    setAllowLossless(albumPrefs.data.preferences.allow_lossless);
    setMinBitrate(String(albumPrefs.data.preferences.min_bitrate));
    setFormats(albumPrefs.data.preferences.preferred_formats.join(","));
  }, [albumPrefs.data]);

  async function searchReleases(e: FormEvent) {
    e.preventDefault();
    if (searching || !id) return;
    setSearching(true);
    setError("");
    try {
      const data = await api<MusicReleaseSearch>(
        `/library/music/albums/${id}/releases`,
        {
          method: "POST",
          body: JSON.stringify({ query: releaseQuery.trim() }),
        },
      );
      setReleases(data.items);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setSearching(false);
    }
  }

  async function startImport(e: FormEvent) {
    e.preventDefault();
    if (pending || !id || !importPath.trim()) return;
    setPending(true);
    setError("");
    try {
      const job = await api<MusicImportJob>(
        `/library/music/albums/${id}/import`,
        {
          method: "POST",
          body: JSON.stringify({
            source_path: importPath.trim(),
            root: importRoot.trim() || "Music",
          }),
        },
      );
      setImportID(job.id);
      setImportOpen(false);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function download(releaseID: string) {
    if (!id || pending) return;
    setPending(true);
    setError("");
    try {
      const job = await api<MusicJob>(`/library/music/albums/${id}/download`, {
        method: "POST",
        body: JSON.stringify({ release_id: releaseID, root: "Music" }),
      });
      setDownloadJobID(job.id);
      setReleaseOpen(false);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function saveAlbumPrefs(e: FormEvent) {
    e.preventDefault();
    if (!id || !albumPrefs.data || prefsPending) return;
    setPrefsPending(true);
    setError("");
    try {
      await api(`/library/music/albums/${id}/preferences`, {
        method: "PUT",
        body: JSON.stringify({
          allow_lossless: allowLossless,
          min_bitrate: Number(minBitrate) || 0,
          preferred_formats: formats
            .split(",")
            .map((value) => value.trim())
            .filter(Boolean),
          revision: albumPrefs.data.revision,
        }),
      });
      albumPrefs.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPrefsPending(false);
    }
  }

  async function resetAlbumPrefs() {
    if (!id || prefsPending) return;
    setPrefsPending(true);
    setError("");
    try {
      await api(`/library/music/albums/${id}/preferences`, {
        method: "DELETE",
      });
      albumPrefs.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPrefsPending(false);
    }
  }

  const importKind =
    importJob.data?.state === "completed"
      ? "success"
      : importJob.data?.state === "failed"
        ? "danger"
        : "info";

  return (
    <>
      <PageToolbar label="Music album actions">
        <ToolbarSection>
          <ToolbarButton
            icon={ArrowLeft}
            label="Artist"
            to={
              album?.artist_id ? `/music/artists/${album.artist_id}` : "/music"
            }
          />
          <ToolbarButton
            icon={Play}
            label="Play album"
            disabled={playableTracks.length === 0}
            onClick={() =>
              playableTracks.length > 0 &&
              player.play(playableTracks[0], playableTracks)
            }
          />
          <ToolbarButton
            icon={Search}
            label="Find releases"
            onClick={() => setReleaseOpen(true)}
          />
          <ToolbarButton
            icon={FolderInput}
            label="Import folder"
            onClick={() => setImportOpen(true)}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="mus-body">
        <PageHeading
          title={album?.title ?? "Music album"}
          description={
            album
              ? [album.artist_name, album.year, album.primary_type]
                  .filter(Boolean)
                  .join(" · ")
              : undefined
          }
        >
          {album && (
            <>
              <Label
                kind={
                  album.status === "available"
                    ? "success"
                    : album.monitored
                      ? "danger"
                      : "disabled"
                }
              >
                {album.status === "available"
                  ? "In library"
                  : "No registered files"}
              </Label>
              {album.cover_url && (
                <img
                  className="mus-heading-cover"
                  src={album.cover_url}
                  alt=""
                  loading="lazy"
                  referrerPolicy="no-referrer"
                  onError={(e) => {
                    e.currentTarget.style.display = "none";
                  }}
                />
              )}
            </>
          )}
        </PageHeading>
        <RequestState
          loading={details.loading}
          error={details.error}
          retry={details.reload}
        />
        {details.data && (
          <Table label="Music tracks">
            <thead>
              <tr>
                <th scope="col">{multiDisc ? "Disc" : "#"}</th>
                <th scope="col">Title</th>
                <th scope="col">Duration</th>
                <th scope="col" className="is-actions">
                  <span className="sr-only">Play</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {tracks.map((track) => {
                const fileID = track.file_id;
                return (
                  <tr key={`${track.disc}-${track.number}`}>
                    <td>
                      {multiDisc
                        ? `${track.disc}.${track.number}`
                        : track.number}
                    </td>
                    <td>{track.title}</td>
                    <td>{formatDuration(track.duration_ms)}</td>
                    <td className="is-actions">
                      {fileID ? (
                        <Button
                          size="1"
                          variant="ghost"
                          color="gray"
                          aria-label={`Play ${track.title}`}
                          onClick={() => playTrack(fileID)}
                        >
                          <Play size={14} aria-hidden="true" />
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        )}
        {albumPrefs.data?.preferences && (
          <FieldSet legend="Album preferences">
            <form
              className="mus-search"
              onSubmit={(e) => void saveAlbumPrefs(e)}
            >
              <label>
                <input
                  type="checkbox"
                  checked={allowLossless}
                  onChange={(e) => setAllowLossless(e.target.checked)}
                />{" "}
                Allow lossless
              </label>
              <label>
                Minimum bitrate{" "}
                <TextField.Root
                  aria-label="Album minimum bitrate"
                  value={minBitrate}
                  onChange={(e) => setMinBitrate(e.target.value)}
                  style={{ width: 90 }}
                />
              </label>
              <label>
                Formats{" "}
                <TextField.Root
                  aria-label="Album preferred formats"
                  value={formats}
                  onChange={(e) => setFormats(e.target.value)}
                />
              </label>
              <Button type="submit" disabled={prefsPending}>
                Save
              </Button>
              {!albumPrefs.data.inherited && (
                <Button
                  type="button"
                  variant="soft"
                  color="gray"
                  disabled={prefsPending}
                  onClick={() => void resetAlbumPrefs()}
                >
                  Use global
                </Button>
              )}
            </form>
            <p className="rs-muted">
              {albumPrefs.data.inherited
                ? "Inheriting the global music preferences."
                : "This album has its own preferences."}
            </p>
          </FieldSet>
        )}
        {error && <Alert kind="danger">{error}</Alert>}
        {downloadJob.data && (
          <FieldSet legend={`Download · ${downloadJob.data.title}`}>
            <div className="mus-scan-summary">
              <Label
                kind={
                  downloadJob.data.state === "completed"
                    ? "success"
                    : downloadJob.data.state === "blocked" ||
                        downloadJob.data.state === "failed"
                      ? "danger"
                      : "info"
                }
              >
                {downloadJob.data.state}
              </Label>
              <span>{Math.round(downloadJob.data.progress * 100)}%</span>
              {downloadJob.data.error && (
                <span className="rs-muted">{downloadJob.data.error}</span>
              )}
            </div>
          </FieldSet>
        )}
        {importJob.data && (
          <FieldSet legend={`Import #${importJob.data.id}`}>
            <div className="mus-scan-summary">
              <Label kind={importKind}>{importJob.data.state}</Label>
              <span>
                {importJob.data.imported_count} / {importJob.data.file_count}{" "}
                imported
              </span>
            </div>
            {importJob.data.error && (
              <Alert kind="danger">{importJob.data.error}</Alert>
            )}
            {importJob.data.files && importJob.data.files.length > 0 && (
              <Table label="Music import files">
                <thead>
                  <tr>
                    <th scope="col">Source</th>
                    <th scope="col">Destination</th>
                    <th scope="col">State</th>
                    <th scope="col">Error</th>
                  </tr>
                </thead>
                <tbody>
                  {importJob.data.files.map((file) => (
                    <tr key={file.id}>
                      <td>
                        <span className="mus-path">{file.source_path}</span>
                      </td>
                      <td>
                        <span className="mus-path">
                          {file.destination_path || "—"}
                        </span>
                      </td>
                      <td>
                        <Label
                          kind={
                            file.state === "imported"
                              ? "success"
                              : file.state === "failed"
                                ? "danger"
                                : "info"
                          }
                        >
                          {file.state}
                        </Label>
                      </td>
                      <td>{file.error || "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </FieldSet>
        )}
      </PageBody>
      <Modal
        open={releaseOpen}
        onOpenChange={setReleaseOpen}
        title="Find releases"
        description="Search the configured indexers with the audio category."
        size="xlarge"
        footer={
          <Button
            variant="soft"
            color="gray"
            onClick={() => setReleaseOpen(false)}
          >
            Close
          </Button>
        }
      >
        <form onSubmit={(e) => void searchReleases(e)} className="mus-search">
          <TextField.Root
            aria-label="Release search"
            placeholder={`${album?.artist_name ?? ""} ${album?.title ?? ""}`.trim()}
            value={releaseQuery}
            onChange={(e) => setReleaseQuery(e.target.value)}
          />
          <Button type="submit" disabled={searching}>
            <Search size={15} aria-hidden="true" /> Search
          </Button>
        </form>
        <Alert kind="info" title="Download then import">
          Automatic queueing is not implemented yet. Open a release in your
          tracker or add it to your download client, then use Import folder with
          the completed folder.
        </Alert>
        {releases.length > 0 && (
          <Table label="Music release results">
            <thead>
              <tr>
                <th scope="col">Release</th>
                <th scope="col">Format</th>
                <th scope="col">Size</th>
                <th scope="col">Seeders</th>
                <th scope="col" className="is-actions">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {releases.map((release) => (
                <tr key={`${release.indexer_id}-${release.id}`}>
                  <td>
                    {release.title}
                    {release.lossless && (
                      <>
                        {" "}
                        <Label kind="success">Lossless</Label>
                      </>
                    )}
                  </td>
                  <td>
                    {[
                      release.format,
                      release.bitrate ? `${release.bitrate} kbps` : "",
                    ]
                      .filter(Boolean)
                      .join(" ") || "—"}
                  </td>
                  <td>{formatBytes(release.size)}</td>
                  <td>{release.seeders ?? "—"}</td>
                  <td className="is-actions">
                    <Button
                      size="1"
                      disabled={pending}
                      onClick={() => void download(release.id)}
                    >
                      Download
                    </Button>{" "}
                    {release.info_url ? (
                      <a
                        href={release.info_url}
                        target="_blank"
                        rel="noreferrer"
                      >
                        Open <ExternalLink size={12} aria-hidden="true" />
                      </a>
                    ) : (
                      "—"
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Modal>
      <Modal
        open={importOpen}
        onOpenChange={setImportOpen}
        title="Import completed folder"
        description="Choose a completed album folder below the downloads root. Files are copied, tagged and published without overwriting."
        footer={
          <Button
            variant="soft"
            color="gray"
            onClick={() => setImportOpen(false)}
          >
            Close
          </Button>
        }
      >
        <form onSubmit={(e) => void startImport(e)} className="mus-search">
          <TextField.Root
            aria-label="Completed folder"
            placeholder="Album"
            value={importPath}
            onChange={(e) => setImportPath(e.target.value)}
          />
          <TextField.Root
            aria-label="Destination root"
            placeholder="Music"
            value={importRoot}
            onChange={(e) => setImportRoot(e.target.value)}
          />
          <Button type="submit" disabled={pending || !importPath.trim()}>
            Import
          </Button>
        </form>
        {error && <Alert kind="danger">{error}</Alert>}
      </Modal>
    </>
  );
}
