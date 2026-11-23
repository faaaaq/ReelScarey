import {
  useEffect,
  useMemo,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  ArrowLeft,
  ChevronRight,
  Disc3,
  ExternalLink,
  FolderInput,
  LibraryBig,
  ListMusic,
  ListPlus,
  PanelRight,
  Play,
  Plus,
  Search,
  Shuffle,
  UserRound,
  Users,
} from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import {
  type MusicAlbum,
  type MusicAlbumDetails,
  type MusicAlbumPage,
  type MusicArtist,
  type MusicArtistDetails,
  type MusicArtistPage,
  type MusicArtistTrack,
  type MusicArtistTrackPage,
} from "../api";
import { MusicNowPlaying } from "../components/MusicNowPlaying";
import { PageHeading, RequestState } from "../components";
import { albumCoverURL, artistImageURL } from "../musicArtwork";
import {
  formatMusicFileType,
  formatTrackBitrate,
  MusicFileCells,
  MusicFileColumnHeadings,
  MusicFilenameToggle,
} from "../musicFile";
import { useMusicPlayer, type PlayerTrack } from "../musicPlayer";
import { Pagination } from "../movieComponents";
import {
  EmptyState,
  PageBody,
  PageToolbar,
  Table,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { formatTrackDuration } from "../ui/format";
import { useResource } from "../useResource";
import "./Library.css";
import "./Music.css";
import "./MusicPlayerWorkspace.css";

type View = "tracks" | "artists" | "albums";

type WorkspaceParams = {
  view?: View;
  query?: string;
  page?: number;
  artist?: number;
  album?: number;
};

function positiveID(value: string | null) {
  return value && /^[1-9][0-9]*$/.test(value) ? Number(value) : 0;
}

function pages(total: number, limit: number) {
  return Math.max(1, Math.ceil(total / Math.max(1, limit)));
}

function playerTrack(track: MusicArtistTrack): PlayerTrack | null {
  if (!track.file_id) return null;
  const format = formatMusicFileType(track.format, track.relative_path);
  const bitrate = formatTrackBitrate(track.bitrate);
  const formatLabel = [
    format === "—" ? "" : format,
    bitrate === "—" ? "" : bitrate,
  ]
    .filter(Boolean)
    .join(" · ");
  return {
    fileID: track.file_id,
    title: track.title,
    artist: track.artist_name || "Unknown artist",
    album: track.album_title,
    coverURL: albumCoverURL(track.album_id) || track.cover_url || undefined,
    durationMS: track.duration_ms,
    albumID: track.album_id,
    formatLabel: formatLabel || undefined,
  };
}

function playableTracks(tracks: MusicArtistTrack[]) {
  return tracks
    .map(playerTrack)
    .filter((track): track is PlayerTrack => track !== null);
}

function Artwork({
  artistID,
  albumID,
  className = "",
}: {
  artistID?: number;
  albumID?: number;
  className?: string;
}) {
  const source = albumCoverURL(albumID) || artistImageURL(artistID);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [source]);
  return (
    <span className={`mmp-artwork ${className}`} aria-hidden="true">
      {source && !failed ? (
        <img
          src={source}
          alt=""
          loading="lazy"
          referrerPolicy="no-referrer"
          onError={() => setFailed(true)}
        />
      ) : artistID ? (
        <UserRound size={28} strokeWidth={1.3} />
      ) : (
        <Disc3 size={30} strokeWidth={1.3} />
      )}
    </span>
  );
}

function PlaybackActions({
  tracks,
  scope,
  onQueueOpen,
}: {
  tracks: MusicArtistTrack[];
  scope: string;
  onQueueOpen: () => void;
}) {
  const player = useMusicPlayer();
  const items = useMemo(() => playableTracks(tracks), [tracks]);

  function play(shuffle = false) {
    if (!items.length) return;
    if (shuffle && !player.shuffle) player.toggleShuffle();
    player.play(items[0], items);
    onQueueOpen();
  }

  function enqueue(playNext: boolean) {
    const ordered = playNext ? [...items].reverse() : items;
    ordered.forEach((track) => player.enqueue(track, playNext));
    onQueueOpen();
  }

  return (
    <div className="mmp-playback-actions">
      <div className="mmp-playback-buttons">
        <Button size="1" disabled={!items.length} onClick={() => play()}>
          <Play size={14} aria-hidden="true" /> Play
        </Button>
        <Button
          size="1"
          variant="soft"
          disabled={!items.length}
          onClick={() => play(true)}
        >
          <Shuffle size={14} aria-hidden="true" /> Shuffle
        </Button>
        <Button
          size="1"
          variant="soft"
          disabled={!items.length}
          title={`Play ${scope} after the current track`}
          onClick={() => enqueue(true)}
        >
          <ListPlus size={14} aria-hidden="true" /> Play next
        </Button>
        <Button
          size="1"
          variant="soft"
          disabled={!items.length}
          title={`Add ${scope} to the playback queue`}
          onClick={() => enqueue(false)}
        >
          <Plus size={14} aria-hidden="true" /> Queue
        </Button>
      </div>
      <span className="mmp-playable-count">
        {items.length} of {tracks.length} loaded tracks can play
      </span>
    </div>
  );
}

function WorkspaceTrackTable({
  tracks,
  label,
  onOpenAlbum,
  onQueueOpen,
}: {
  tracks: MusicArtistTrack[];
  label: string;
  onOpenAlbum: (id: number) => void;
  onQueueOpen: () => void;
}) {
  const player = useMusicPlayer();
  const [showFilenames, setShowFilenames] = useState(false);
  const playable = useMemo(() => playableTracks(tracks), [tracks]);

  function play(item: PlayerTrack) {
    player.play(item, playable);
    onQueueOpen();
  }

  if (!tracks.length) {
    return (
      <EmptyState title="No playable tracks in this view">
        Choose another artist, album, or search. Tracks without a registered
        audio file cannot be played.
      </EmptyState>
    );
  }

  return (
    <div className="mmp-track-list">
      <MusicFilenameToggle
        checked={showFilenames}
        onChange={setShowFilenames}
      />
      <Table label={label}>
        <thead>
          <tr>
            <th scope="col" className="is-shrink">
              #
            </th>
            <th scope="col">Title</th>
            <th scope="col">Artist</th>
            <th scope="col">Album</th>
            <th scope="col">Year</th>
            <th scope="col">Duration</th>
            <MusicFileColumnHeadings showFilename={showFilenames} />
            <th scope="col" className="is-actions">
              <span className="sr-only">Playback actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {tracks.map((track) => {
            const item = playerTrack(track);
            const current = item && player.current?.fileID === item.fileID;
            return (
              <tr
                key={
                  track.id ?? `${track.album_id}-${track.disc}-${track.number}`
                }
                className={current ? "is-current" : undefined}
                data-playing={current || undefined}
                onDoubleClick={() => item && play(item)}
              >
                <td>
                  {track.disc > 1
                    ? `${track.disc}.${track.number}`
                    : track.number}
                </td>
                <td>
                  <span className="mmp-track-title">
                    {current && <Disc3 size={13} aria-label="Current track" />}
                    {track.title}
                  </span>
                </td>
                <td>{track.artist_name || "Unknown artist"}</td>
                <td>
                  {track.album_id ? (
                    <button
                      type="button"
                      className="mmp-text-button"
                      onClick={() => onOpenAlbum(track.album_id!)}
                    >
                      {track.album_title}
                    </button>
                  ) : (
                    track.album_title || "—"
                  )}
                </td>
                <td>{track.album_year || "—"}</td>
                <td>{formatTrackDuration(track.duration_ms)}</td>
                <MusicFileCells track={track} showFilename={showFilenames} />
                <td className="is-actions">
                  <div className="mmp-row-actions">
                    <Button
                      size="1"
                      variant="ghost"
                      color="gray"
                      disabled={!item}
                      aria-label={`Play ${track.title}`}
                      title={`Play ${track.title}`}
                      onClick={() => item && play(item)}
                    >
                      <Play size={14} aria-hidden="true" />
                    </Button>
                    <Button
                      size="1"
                      variant="ghost"
                      color="gray"
                      disabled={!item}
                      aria-label={`Play ${track.title} next`}
                      title={`Play ${track.title} next`}
                      onClick={() => {
                        if (item) player.enqueue(item, true);
                        onQueueOpen();
                      }}
                    >
                      <ListPlus size={14} aria-hidden="true" />
                    </Button>
                    <Button
                      size="1"
                      variant="ghost"
                      color="gray"
                      disabled={!item}
                      aria-label={`Add ${track.title} to queue`}
                      title={`Add ${track.title} to queue`}
                      onClick={() => {
                        if (item) player.enqueue(item);
                        onQueueOpen();
                      }}
                    >
                      <Plus size={14} aria-hidden="true" />
                    </Button>
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </Table>
    </div>
  );
}

function ArtistCards({
  artists,
  onOpen,
}: {
  artists: MusicArtist[];
  onOpen: (id: number) => void;
}) {
  if (!artists.length)
    return (
      <EmptyState title="No artists found">Try another search.</EmptyState>
    );
  return (
    <div className="mmp-card-grid">
      {artists.map((artist) => (
        <button
          type="button"
          className="mmp-catalog-card"
          key={artist.id ?? artist.musicbrainz_id}
          disabled={!artist.id}
          onClick={() => artist.id && onOpen(artist.id)}
        >
          <Artwork artistID={artist.id} />
          <span className="mmp-catalog-card-copy">
            <strong>{artist.name}</strong>
            <span>
              {artist.album_count}{" "}
              {artist.album_count === 1 ? "album" : "albums"}
            </span>
          </span>
          <ChevronRight size={16} aria-hidden="true" />
        </button>
      ))}
    </div>
  );
}

function AlbumCards({
  albums,
  onOpen,
}: {
  albums: MusicAlbum[];
  onOpen: (id: number) => void;
}) {
  if (!albums.length)
    return <EmptyState title="No albums found">Try another search.</EmptyState>;
  return (
    <div className="mmp-card-grid">
      {albums.map((album) => (
        <button
          type="button"
          className="mmp-catalog-card"
          key={album.id ?? album.musicbrainz_id}
          disabled={!album.id}
          onClick={() => album.id && onOpen(album.id)}
        >
          <Artwork albumID={album.id} />
          <span className="mmp-catalog-card-copy">
            <strong>{album.title}</strong>
            <span>{album.artist_name || "Unknown artist"}</span>
            <span>
              {album.year || "Year unknown"} · {album.track_count || 0} tracks
            </span>
          </span>
          <ChevronRight size={16} aria-hidden="true" />
        </button>
      ))}
    </div>
  );
}

function DetailHeading({
  artwork,
  title,
  subtitle,
  children,
}: {
  artwork: ReactNode;
  title: string;
  subtitle: string;
  children: ReactNode;
}) {
  return (
    <header className="mmp-detail-heading">
      {artwork}
      <div>
        <h2>{title}</h2>
        <p>{subtitle}</p>
        <div className="mmp-detail-links">{children}</div>
      </div>
    </header>
  );
}

export function MusicPlayerWorkspace() {
  const [params, setParams] = useSearchParams();
  const view: View =
    params.get("view") === "artists"
      ? "artists"
      : params.get("view") === "albums"
        ? "albums"
        : "tracks";
  const query = params.get("query") ?? "";
  const page = positiveID(params.get("page")) || 1;
  const artistID = view === "artists" ? positiveID(params.get("artist")) : 0;
  const albumID = view === "albums" ? positiveID(params.get("album")) : 0;
  const [input, setInput] = useState(query);
  const [queueOpen, setQueueOpen] = useState(true);

  useEffect(() => setInput(query), [query]);

  function update(next: WorkspaceParams) {
    const updated = new URLSearchParams();
    const nextView = next.view ?? view;
    if (nextView !== "tracks") updated.set("view", nextView);
    const nextQuery = next.query ?? query;
    if (nextQuery) updated.set("query", nextQuery);
    const nextPage = next.page ?? 1;
    if (nextPage > 1) updated.set("page", String(nextPage));
    if (next.artist) updated.set("artist", String(next.artist));
    if (next.album) updated.set("album", String(next.album));
    setParams(updated);
  }

  function changeView(nextView: View) {
    update({ view: nextView, query, page: 1 });
  }

  function openArtist(id: number) {
    update({ view: "artists", query, page: 1, artist: id });
  }

  function openAlbum(id: number) {
    update({ view: "albums", query, page: 1, album: id });
  }

  function submitSearch(event: FormEvent) {
    event.preventDefault();
    update({ view: "tracks", query: input.trim(), page: 1 });
  }

  const artistParams = new URLSearchParams({
    page: String(view === "artists" ? page : 1),
  });
  const albumParams = new URLSearchParams({
    page: String(view === "albums" ? page : 1),
    limit: "48",
  });
  const trackParams = new URLSearchParams({
    page: String(view === "tracks" ? page : 1),
    limit: "100",
  });
  if (query) {
    artistParams.set("query", query);
    albumParams.set("query", query);
    trackParams.set("query", query);
  }

  const artists = useResource<MusicArtistPage>(
    `/library/music/artists?${artistParams.toString()}`,
    { retain: true },
  );
  const albums = useResource<MusicAlbumPage>(
    `/library/music/albums?${albumParams.toString()}`,
    { retain: true },
  );
  const tracks = useResource<MusicArtistTrackPage>(
    `/library/music/tracks?${trackParams.toString()}`,
    { retain: true },
  );
  const artistDetails = useResource<MusicArtistDetails>(
    artistID ? `/library/music/artists/${artistID}` : null,
  );
  const artistTracks = useResource<MusicArtistTrackPage>(
    artistID
      ? `/library/music/artists/${artistID}/tracks?limit=100&page=1`
      : null,
  );
  const albumDetails = useResource<MusicAlbumDetails>(
    albumID ? `/library/music/albums/${albumID}` : null,
  );

  const albumTracks: MusicArtistTrack[] = albumDetails.data
    ? albumDetails.data.tracks.map((track) => ({
        ...track,
        album_id: albumDetails.data!.album.id,
        artist_name: albumDetails.data!.album.artist_name,
        album_title: albumDetails.data!.album.title,
        album_year: albumDetails.data!.album.year,
        cover_url: albumDetails.data!.album.cover_url,
      }))
    : [];

  const activeTracks = albumID
    ? albumTracks
    : artistID
      ? (artistTracks.data?.items ?? [])
      : view === "tracks"
        ? (tracks.data?.items ?? [])
        : [];
  const activeResource = albumID
    ? albumDetails
    : artistID
      ? artistTracks
      : view === "tracks"
        ? tracks
        : null;
  const leftAlbums = view === "albums";
  const currentPageData =
    view === "albums"
      ? albums.data
      : view === "artists"
        ? artists.data
        : tracks.data;
  const currentPages = currentPageData
    ? pages(currentPageData.total, currentPageData.limit)
    : 1;

  let heading = "All tracks";
  let headingLink: ReactNode = null;
  if (view === "artists" && !artistID) heading = "Artists";
  if (view === "albums" && !albumID) heading = "Albums";
  if (artistID)
    heading =
      artistDetails.data?.artist.name ??
      artists.data?.items.find((artist) => artist.id === artistID)?.name ??
      "Artist";
  if (albumID)
    heading =
      albumDetails.data?.album.title ??
      albums.data?.items.find((album) => album.id === albumID)?.title ??
      "Album";
  if (artistDetails.data) {
    heading = artistDetails.data.artist.name;
    headingLink = (
      <Link to={`/music/artists/${artistID}`}>
        Open full artist page <ExternalLink size={13} aria-hidden="true" />
      </Link>
    );
  }
  if (albumDetails.data) {
    heading = albumDetails.data.album.title;
    headingLink = (
      <Link to={`/music/albums/${albumID}`}>
        Open full album page <ExternalLink size={13} aria-hidden="true" />
      </Link>
    );
  }

  return (
    <>
      <PageToolbar label="Music player actions">
        <ToolbarSection>
          <ToolbarButton icon={ArrowLeft} label="Library" to="/music" />
          <ToolbarButton
            icon={FolderInput}
            label="Library Import"
            to="/music/import"
          />
        </ToolbarSection>
        <ToolbarSection align="right">
          <ToolbarButton
            icon={PanelRight}
            label="Queue"
            active={queueOpen}
            aria-expanded={queueOpen}
            aria-controls="music-workspace-queue"
            onClick={() => setQueueOpen((open) => !open)}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="mmp-body" flush>
        <div className="mmp-topbar">
          <PageHeading title="Music player" />
          <form className="mmp-search" role="search" onSubmit={submitSearch}>
            <TextField.Root
              aria-label="Search music"
              placeholder="Search title, artist, or album"
              value={input}
              maxLength={200}
              onChange={(event) => setInput(event.target.value)}
            >
              <TextField.Slot>
                <Search size={15} aria-hidden="true" />
              </TextField.Slot>
            </TextField.Root>
            <Button type="submit" variant="soft">
              Search
            </Button>
          </form>
        </div>

        <div className={`mmp-layout${queueOpen ? " has-queue" : ""}`}>
          <aside className="mmp-library" aria-label="Music library browser">
            <div className="mmp-pane-heading">
              <LibraryBig size={17} aria-hidden="true" />
              <h2>Library</h2>
            </div>
            <nav className="mmp-library-nav" aria-label="Music collections">
              <button
                type="button"
                className={view === "tracks" ? "is-active" : ""}
                aria-current={view === "tracks" ? "page" : undefined}
                onClick={() => changeView("tracks")}
              >
                <ListMusic size={16} aria-hidden="true" />
                <span>All tracks</span>
                <small>{tracks.data?.total ?? "…"}</small>
              </button>
              <button
                type="button"
                className={view === "artists" ? "is-active" : ""}
                aria-current={view === "artists" ? "page" : undefined}
                onClick={() => changeView("artists")}
              >
                <Users size={16} aria-hidden="true" />
                <span>Artists</span>
                <small>{artists.data?.total ?? "…"}</small>
              </button>
              <button
                type="button"
                className={view === "albums" ? "is-active" : ""}
                aria-current={view === "albums" ? "page" : undefined}
                onClick={() => changeView("albums")}
              >
                <Disc3 size={16} aria-hidden="true" />
                <span>Albums</span>
                <small>{albums.data?.total ?? "…"}</small>
              </button>
            </nav>

            <div className="mmp-library-index">
              <h3>{leftAlbums ? "Album index" : "Artist index"}</h3>
              {leftAlbums ? (
                <ul>
                  {(albums.data?.items ?? []).map((album) => (
                    <li key={album.id}>
                      <button
                        type="button"
                        className={albumID === album.id ? "is-active" : ""}
                        disabled={!album.id}
                        onClick={() => album.id && openAlbum(album.id)}
                      >
                        <Artwork albumID={album.id} className="is-small" />
                        <span>
                          <strong>{album.title}</strong>
                          <small>{album.artist_name || "Unknown artist"}</small>
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
              ) : (
                <ul>
                  {(artists.data?.items ?? []).map((artist) => (
                    <li key={artist.id}>
                      <button
                        type="button"
                        className={artistID === artist.id ? "is-active" : ""}
                        disabled={!artist.id}
                        onClick={() => artist.id && openArtist(artist.id)}
                      >
                        <Artwork artistID={artist.id} className="is-small" />
                        <span>
                          <strong>{artist.name}</strong>
                          <small>
                            {artist.album_count}{" "}
                            {artist.album_count === 1 ? "album" : "albums"}
                          </small>
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              {(leftAlbums ? albums.loading : artists.loading) && (
                <p className="mmp-index-status">Loading index…</p>
              )}
              {(leftAlbums ? albums.error : artists.error) && (
                <p className="mmp-index-status" role="alert">
                  {leftAlbums ? albums.error : artists.error}
                </p>
              )}
            </div>
          </aside>

          <section
            className="mmp-center"
            aria-labelledby="music-browser-heading"
          >
            <div className="mmp-center-heading">
              <div>
                {(artistID || albumID) && (
                  <button
                    type="button"
                    className="mmp-back-button"
                    onClick={() => update({ view, query, page: 1 })}
                  >
                    <ArrowLeft size={13} aria-hidden="true" />
                    {artistID ? "All artists" : "All albums"}
                  </button>
                )}
                <h1 id="music-browser-heading">{heading}</h1>
                {headingLink}
              </div>
              <span className="mmp-result-count">
                {activeTracks.length
                  ? `${activeTracks.length} loaded tracks`
                  : currentPageData
                    ? `${currentPageData.total} ${view === "tracks" ? "tracks" : view}`
                    : ""}
              </span>
            </div>

            {(artistID || albumID || view === "tracks") && (
              <PlaybackActions
                tracks={activeTracks}
                scope={heading}
                onQueueOpen={() => setQueueOpen(true)}
              />
            )}

            {activeResource?.loading && (
              <RequestState loading error="" retry={activeResource.reload} />
            )}
            {activeResource?.error && (
              <RequestState
                loading={false}
                error={activeResource.error}
                code={activeResource.code}
                retry={activeResource.reload}
              />
            )}

            {artistID && artistDetails.error && (
              <RequestState
                loading={false}
                error={artistDetails.error}
                code={artistDetails.code}
                retry={artistDetails.reload}
              />
            )}

            {!activeResource?.loading &&
              !activeResource?.error &&
              artistID &&
              artistDetails.data && (
                <>
                  <DetailHeading
                    artwork={
                      <Artwork artistID={artistID} className="is-large" />
                    }
                    title={artistDetails.data.artist.name}
                    subtitle={`${artistDetails.data.albums.length} albums · ${artistTracks.data?.total ?? 0} registered tracks`}
                  >
                    <Link to={`/music/artists/${artistID}`}>
                      Artist details{" "}
                      <ExternalLink size={13} aria-hidden="true" />
                    </Link>
                  </DetailHeading>
                  <section
                    className="mmp-artist-albums"
                    aria-label="Albums by this artist"
                  >
                    {artistDetails.data.albums.map((album) => (
                      <button
                        type="button"
                        key={album.id}
                        onClick={() => album.id && openAlbum(album.id)}
                      >
                        <Artwork albumID={album.id} className="is-small" />
                        <span>{album.title}</span>
                        <small>{album.year || "Year unknown"}</small>
                      </button>
                    ))}
                  </section>
                </>
              )}

            {!activeResource?.loading &&
              !activeResource?.error &&
              albumDetails.data && (
                <DetailHeading
                  artwork={<Artwork albumID={albumID} className="is-large" />}
                  title={albumDetails.data.album.title}
                  subtitle={`${albumDetails.data.album.artist_name || "Unknown artist"} · ${albumDetails.data.album.year || "Year unknown"} · ${albumTracks.length} tracks`}
                >
                  <Link to={`/music/albums/${albumID}`}>
                    Album details <ExternalLink size={13} aria-hidden="true" />
                  </Link>
                </DetailHeading>
              )}

            {!artistID && !albumID && view === "artists" && (
              <>
                <RequestState
                  loading={artists.loading}
                  error={artists.error}
                  code={artists.code}
                  retry={artists.reload}
                />
                {!artists.loading && !artists.error && (
                  <ArtistCards
                    artists={artists.data?.items ?? []}
                    onOpen={openArtist}
                  />
                )}
              </>
            )}
            {!artistID && !albumID && view === "albums" && (
              <>
                <RequestState
                  loading={albums.loading}
                  error={albums.error}
                  code={albums.code}
                  retry={albums.reload}
                />
                {!albums.loading && !albums.error && (
                  <AlbumCards
                    albums={albums.data?.items ?? []}
                    onOpen={openAlbum}
                  />
                )}
              </>
            )}
            {(artistID || albumID || view === "tracks") &&
              !activeResource?.loading &&
              !activeResource?.error && (
                <WorkspaceTrackTable
                  tracks={activeTracks}
                  label={
                    heading === "All tracks"
                      ? "Music tracks"
                      : `${heading} tracks`
                  }
                  onOpenAlbum={openAlbum}
                  onQueueOpen={() => setQueueOpen(true)}
                />
              )}

            {!artistID && !albumID && currentPageData && (
              <div className="mmp-pagination">
                <Pagination
                  page={currentPageData.page}
                  pages={currentPages}
                  onPage={(nextPage) => update({ view, query, page: nextPage })}
                />
              </div>
            )}
          </section>

          {queueOpen && (
            <div className="mmp-queue" id="music-workspace-queue">
              <MusicNowPlaying onClose={() => setQueueOpen(false)} />
            </div>
          )}
        </div>
      </PageBody>
    </>
  );
}
