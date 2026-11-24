import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  ChevronRight,
  Disc3,
  ExternalLink,
  FolderInput,
  LibraryBig,
  ListMusic,
  ListPlus,
  PanelRight,
  Pencil,
  Play,
  Plus,
  Search,
  Shuffle,
  Trash2,
  UserRound,
  Users,
  X,
} from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import {
  api,
  messageFor,
  type MusicAlbum,
  type MusicAlbumDetails,
  type MusicAlbumPage,
  type MusicArtist,
  type MusicArtistDetails,
  type MusicArtistPage,
  type MusicArtistTrack,
  type MusicArtistTrackPage,
  type MusicPlaylistDetails,
  type MusicPlaylistList,
  type MusicPlaylistSummary,
} from "../api";
import {
  PlaylistCreateDialog,
  PlaylistRenameDialog,
  PlaylistTrackPickerDialog,
} from "../components/MusicPlaylistDialogs";
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
import { useMusicQueue, type PlayerTrack } from "../musicPlayer";
import { Pagination } from "../movieComponents";
import {
  Alert,
  ConfirmDialog,
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

type View = "tracks" | "artists" | "albums" | "playlists";

type WorkspaceParams = {
  view?: View;
  query?: string;
  page?: number;
  artist?: number;
  album?: number;
  playlist?: number;
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

function albumPlayerTracks(details: MusicAlbumDetails): MusicArtistTrack[] {
  return details.tracks.map((track) => ({
    ...track,
    album_id: details.album.id,
    artist_name: details.album.artist_name,
    album_title: details.album.title,
    album_year: details.album.year,
    cover_url: details.album.cover_url,
  }));
}

/** Load every result only for an explicit full-scope Shuffle action. */
async function fetchAllTrackPages(base: string, query = "") {
  const result: MusicArtistTrack[] = [];
  let page = 1;
  for (;;) {
    const params = new URLSearchParams({
      page: String(page),
      limit: "100",
    });
    if (query) params.set("query", query);
    const separator = base.includes("?") ? "&" : "?";
    const response = await api<MusicArtistTrackPage>(
      `${base}${separator}${params.toString()}`,
    );
    if (response.page !== page)
      throw new Error(
        "The track list changed pages unexpectedly. Try Shuffle again.",
      );
    result.push(...response.items);
    const lastPage = pages(response.total, response.limit);
    if (page >= lastPage || response.items.length === 0) return result;
    page += 1;
    if (page > 100000)
      throw new Error("This track list is too large to shuffle in one action.");
  }
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
  onShuffle,
  shufflePending,
  onInvalidate,
}: {
  tracks: MusicArtistTrack[];
  scope: string;
  onQueueOpen: () => void;
  onShuffle: () => void;
  shufflePending: boolean;
  onInvalidate: () => void;
}) {
  const player = useMusicQueue();
  const items = useMemo(() => playableTracks(tracks), [tracks]);

  function play() {
    onInvalidate();
    if (!items.length) return;
    player.play(items[0], items);
    onQueueOpen();
  }

  function enqueue(playNext: boolean) {
    onInvalidate();
    const ordered = playNext ? [...items].reverse() : items;
    ordered.forEach((track) => player.enqueue(track, playNext));
    onQueueOpen();
  }

  return (
    <div className="mmp-playback-actions">
      <div className="mmp-playback-buttons">
        <Button
          size="1"
          disabled={shufflePending || !items.length}
          onClick={play}
        >
          <Play size={14} aria-hidden="true" /> Play
        </Button>
        <Button
          size="1"
          variant="soft"
          disabled={shufflePending}
          aria-busy={shufflePending}
          title={`Shuffle every playable track in ${scope}`}
          onClick={onShuffle}
        >
          <Shuffle size={14} aria-hidden="true" />
          {shufflePending ? "Loading all…" : "Shuffle"}
        </Button>
        <Button
          size="1"
          variant="soft"
          disabled={shufflePending || !items.length}
          title={`Play ${scope} after the current track`}
          onClick={() => enqueue(true)}
        >
          <ListPlus size={14} aria-hidden="true" /> Play next
        </Button>
        <Button
          size="1"
          variant="soft"
          disabled={shufflePending || !items.length}
          title={`Add ${scope} to the playback queue`}
          onClick={() => enqueue(false)}
        >
          <Plus size={14} aria-hidden="true" /> Queue
        </Button>
      </div>
      <span className="mmp-playable-count">
        {shufflePending
          ? "Loading the complete track scope…"
          : `${items.length} of ${tracks.length} loaded tracks can play`}
      </span>
    </div>
  );
}

type PlaylistTrackActions = {
  busy: boolean;
  onRemove: (track: MusicArtistTrack) => void;
  onMove: (track: MusicArtistTrack, direction: -1 | 1) => void;
};

function WorkspaceTrackTable({
  tracks,
  label,
  onOpenAlbum,
  onQueueOpen,
  onAddToPlaylist,
  onInvalidate,
  playlist,
  emptyMessage,
}: {
  tracks: MusicArtistTrack[];
  label: string;
  onOpenAlbum: (id: number) => void;
  onQueueOpen: () => void;
  onAddToPlaylist: (track: MusicArtistTrack) => void;
  onInvalidate: () => void;
  playlist?: PlaylistTrackActions;
  emptyMessage?: string;
}) {
  const player = useMusicQueue();
  const [showFilenames, setShowFilenames] = useState(false);
  const playable = useMemo(() => playableTracks(tracks), [tracks]);

  function play(item: PlayerTrack) {
    onInvalidate();
    player.play(item, playable);
    onQueueOpen();
  }

  if (!tracks.length) {
    return (
      <EmptyState title="No tracks in this view">
        {emptyMessage ??
          "Choose another artist, album, or search. Tracks without a registered audio file cannot be played."}
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
          {tracks.map((track, trackIndex) => {
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
                        onInvalidate();
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
                        onInvalidate();
                        if (item) player.enqueue(item);
                        onQueueOpen();
                      }}
                    >
                      <Plus size={14} aria-hidden="true" />
                    </Button>
                    <Button
                      size="1"
                      variant="ghost"
                      color="gray"
                      disabled={!track.id}
                      aria-label={`Add ${track.title} to playlist`}
                      title={`Add ${track.title} to playlist`}
                      onClick={() => onAddToPlaylist(track)}
                    >
                      <ListMusic size={14} aria-hidden="true" />
                    </Button>
                    {playlist && (
                      <>
                        <Button
                          size="1"
                          variant="ghost"
                          color="gray"
                          disabled={playlist.busy || trackIndex === 0}
                          aria-label={`Move ${track.title} up in playlist`}
                          title={`Move ${track.title} up in playlist`}
                          onClick={() => playlist.onMove(track, -1)}
                        >
                          <ArrowUp size={14} aria-hidden="true" />
                        </Button>
                        <Button
                          size="1"
                          variant="ghost"
                          color="gray"
                          disabled={
                            playlist.busy || trackIndex === tracks.length - 1
                          }
                          aria-label={`Move ${track.title} down in playlist`}
                          title={`Move ${track.title} down in playlist`}
                          onClick={() => playlist.onMove(track, 1)}
                        >
                          <ArrowDown size={14} aria-hidden="true" />
                        </Button>
                        <Button
                          size="1"
                          variant="ghost"
                          color="gray"
                          disabled={playlist.busy || !track.id}
                          aria-label={`Remove ${track.title} from playlist`}
                          title={`Remove ${track.title} from playlist`}
                          onClick={() => playlist.onRemove(track)}
                        >
                          <X size={14} aria-hidden="true" />
                        </Button>
                      </>
                    )}
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

function PlaylistCards({
  items,
  onOpen,
  onCreate,
}: {
  items: MusicPlaylistSummary[];
  onOpen: (id: number) => void;
  onCreate: () => void;
}) {
  if (!items.length) {
    return (
      <EmptyState
        title="No playlists yet"
        actions={
          <Button size="1" onClick={onCreate}>
            <Plus size={14} aria-hidden="true" /> New playlist
          </Button>
        }
      >
        Create a playlist, then add tracks from any track table.
      </EmptyState>
    );
  }
  return (
    <div className="mmp-card-grid">
      {items.map((playlist) => (
        <button
          type="button"
          className="mmp-catalog-card"
          key={playlist.id}
          onClick={() => onOpen(playlist.id)}
        >
          <span className="mmp-artwork" aria-hidden="true">
            <ListMusic size={28} />
          </span>
          <span className="mmp-catalog-card-copy">
            <strong>{playlist.name}</strong>
            <span>
              {playlist.track_count}{" "}
              {playlist.track_count === 1 ? "track" : "tracks"}
            </span>
            <span>{formatTrackDuration(playlist.duration_ms)}</span>
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
        : params.get("view") === "playlists"
          ? "playlists"
          : "tracks";
  const query = params.get("query") ?? "";
  const page = positiveID(params.get("page")) || 1;
  const artistID = view === "artists" ? positiveID(params.get("artist")) : 0;
  const albumID = view === "albums" ? positiveID(params.get("album")) : 0;
  const playlistID =
    view === "playlists" ? positiveID(params.get("playlist")) : 0;
  const [input, setInput] = useState(query);
  const [queueOpen, setQueueOpen] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [pickerTrack, setPickerTrack] = useState<MusicArtistTrack | null>(null);
  const [renamePlaylist, setRenamePlaylist] =
    useState<MusicPlaylistSummary | null>(null);
  const [playlistBusy, setPlaylistBusy] = useState(false);
  const [playlistError, setPlaylistError] = useState("");
  const shuffleGeneration = useRef(0);
  const [shufflePending, setShufflePending] = useState(false);
  const [shuffleError, setShuffleError] = useState("");
  const scopeKey = `${view}|${query}|${artistID}|${albumID}|${playlistID}`;

  useEffect(() => setInput(query), [query]);
  useEffect(() => {
    shuffleGeneration.current += 1;
    setShufflePending(false);
    setShuffleError("");
  }, [scopeKey]);

  function invalidateShuffle() {
    shuffleGeneration.current += 1;
    setShufflePending(false);
    setShuffleError("");
  }

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
    if (next.playlist) updated.set("playlist", String(next.playlist));
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

  function openPlaylist(id: number) {
    update({ view: "playlists", query, page: 1, playlist: id });
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
  const playlists = useResource<MusicPlaylistList>("/library/music/playlists", {
    retain: true,
  });
  const playlistDetails = useResource<MusicPlaylistDetails>(
    playlistID ? `/library/music/playlists/${playlistID}` : null,
  );
  const selectedPlaylistDetails =
    playlistID && playlistDetails.data?.playlist.id === playlistID
      ? playlistDetails.data
      : undefined;
  const playlistIDRef = useRef(playlistID);
  playlistIDRef.current = playlistID;
  const player = useMusicQueue();
  const playerRef = useRef(player);
  playerRef.current = player;
  const playerActionSnapshot = useRef({
    current: player.current,
    queue: player.queue,
    index: player.index,
    playing: player.playing,
    shuffle: player.shuffle,
  });
  useEffect(() => {
    const previous = playerActionSnapshot.current;
    const changed =
      previous.current !== player.current ||
      previous.queue !== player.queue ||
      previous.index !== player.index ||
      previous.playing !== player.playing ||
      previous.shuffle !== player.shuffle;
    playerActionSnapshot.current = {
      current: player.current,
      queue: player.queue,
      index: player.index,
      playing: player.playing,
      shuffle: player.shuffle,
    };
    if (shufflePending && changed) invalidateShuffle();
  }, [
    player.current,
    player.queue,
    player.index,
    player.playing,
    player.shuffle,
    shufflePending,
  ]);

  const albumTracks: MusicArtistTrack[] = albumDetails.data
    ? albumPlayerTracks(albumDetails.data)
    : [];
  const activeTracks = playlistID
    ? (selectedPlaylistDetails?.tracks ?? [])
    : albumID
      ? albumTracks
      : artistID
        ? (artistTracks.data?.items ?? [])
        : view === "tracks"
          ? (tracks.data?.items ?? [])
          : [];
  const activeResource = playlistID
    ? playlistDetails
    : albumID
      ? albumDetails
      : artistID
        ? artistTracks
        : view === "tracks"
          ? tracks
          : null;
  const currentPageData =
    view === "albums"
      ? albums.data
      : view === "artists"
        ? artists.data
        : view === "tracks"
          ? tracks.data
          : null;
  const currentPages = currentPageData
    ? pages(currentPageData.total, currentPageData.limit)
    : 1;

  let heading = "All tracks";
  let headingLink: ReactNode = null;
  if (view === "artists" && !artistID) heading = "Artists";
  if (view === "albums" && !albumID) heading = "Albums";
  if (view === "playlists" && !playlistID) heading = "Playlists";
  if (playlistID)
    heading =
      selectedPlaylistDetails?.playlist.name ??
      playlists.data?.items.find((playlist) => playlist.id === playlistID)
        ?.name ??
      "Playlist";
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
  if (selectedPlaylistDetails) {
    const summary = selectedPlaylistDetails.playlist;
    heading = summary.name;
    headingLink = (
      <div className="mmp-playlist-heading-actions">
        <Button
          size="1"
          variant="soft"
          onClick={() => setRenamePlaylist(summary)}
        >
          <Pencil size={13} aria-hidden="true" /> Rename
        </Button>
        <ConfirmDialog
          trigger={
            <Button size="1" variant="soft" color="red">
              <Trash2 size={13} aria-hidden="true" /> Delete playlist
            </Button>
          }
          title={`Delete ${summary.name}?`}
          description="This removes only the playlist. Tracks and audio files stay in the library."
          confirmLabel="Delete playlist"
          onConfirm={async () => {
            await api(`/library/music/playlists/${summary.id}`, {
              method: "DELETE",
              body: JSON.stringify({ revision: summary.revision }),
            });
            update({ view: "playlists", query, page: 1 });
            await playlists.reload();
          }}
        />
      </div>
    );
  }
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

  async function shuffleAll() {
    const request = ++shuffleGeneration.current;
    setShufflePending(true);
    setShuffleError("");
    try {
      let complete: MusicArtistTrack[];
      if (playlistID) {
        const details = await api<MusicPlaylistDetails>(
          `/library/music/playlists/${playlistID}`,
        );
        complete = details.tracks;
      } else if (albumID) {
        const details = await api<MusicAlbumDetails>(
          `/library/music/albums/${albumID}`,
        );
        complete = albumPlayerTracks(details);
      } else if (artistID) {
        complete = await fetchAllTrackPages(
          `/library/music/artists/${artistID}/tracks`,
        );
      } else {
        complete = await fetchAllTrackPages("/library/music/tracks", query);
      }
      if (request !== shuffleGeneration.current) return;
      const items = playableTracks(complete);
      if (!items.length) {
        setShuffleError(
          `No registered audio files are available in ${heading}. Nothing was shuffled.`,
        );
        return;
      }
      const currentPlayer = playerRef.current;
      setShufflePending(false);
      // Install one concrete shuffled order. Transport and the bounded Now
      // Playing preview both follow this same queue instead of choosing
      // unrelated random indexes as playback advances.
      currentPlayer.playShuffled(items);
      setQueueOpen(true);
    } catch (error) {
      if (request === shuffleGeneration.current)
        setShuffleError(messageFor(error));
    } finally {
      if (request === shuffleGeneration.current) setShufflePending(false);
    }
  }

  function applyPlaylistDetails(details: MusicPlaylistDetails) {
    // Creation and track-picker mutations also happen outside the playlist
    // detail view. Update their summaries without letting a hidden detail
    // resource relabel the currently visible artist, album, or track table.
    if (playlistIDRef.current === details.playlist.id) {
      playlistDetails.setData(details);
    }
    playlists.setData((current) => {
      const items = [
        ...(current?.items ?? []).filter(
          (playlist) => playlist.id !== details.playlist.id,
        ),
        details.playlist,
      ];
      items.sort(
        (left, right) =>
          left.name.localeCompare(right.name, undefined, {
            sensitivity: "base",
          }) || left.id - right.id,
      );
      return { items };
    });
  }

  async function refreshPlaylists(details?: MusicPlaylistDetails) {
    if (details) {
      applyPlaylistDetails(details);
      return;
    }
    await Promise.all([playlists.reload(), playlistDetails.reload()]);
  }

  async function removePlaylistTrack(track: MusicArtistTrack) {
    const summary = selectedPlaylistDetails?.playlist;
    if (!summary || !track.id || playlistBusy) return;
    setPlaylistBusy(true);
    setPlaylistError("");
    try {
      const details = await api<MusicPlaylistDetails>(
        `/library/music/playlists/${summary.id}/tracks`,
        {
          method: "DELETE",
          body: JSON.stringify({
            track_id: track.id,
            revision: summary.revision,
          }),
        },
      );
      applyPlaylistDetails(details);
    } catch (error) {
      setPlaylistError(messageFor(error));
      await refreshPlaylists();
    } finally {
      setPlaylistBusy(false);
    }
  }

  async function movePlaylistTrack(track: MusicArtistTrack, direction: -1 | 1) {
    const details = selectedPlaylistDetails;
    if (!details || !track.id || playlistBusy) return;
    const ids = details.tracks.flatMap((item) => (item.id ? [item.id] : []));
    if (ids.length !== details.tracks.length) return;
    const index = details.tracks.findIndex((item) => item.id === track.id);
    const target = index + direction;
    if (index < 0 || target < 0 || target >= ids.length) return;
    [ids[index], ids[target]] = [ids[target], ids[index]];
    setPlaylistBusy(true);
    setPlaylistError("");
    try {
      const updated = await api<MusicPlaylistDetails>(
        `/library/music/playlists/${details.playlist.id}/tracks`,
        {
          method: "PUT",
          body: JSON.stringify({
            track_ids: ids,
            revision: details.playlist.revision,
          }),
        },
      );
      applyPlaylistDetails(updated);
    } catch (error) {
      setPlaylistError(messageFor(error));
      await refreshPlaylists();
    } finally {
      setPlaylistBusy(false);
    }
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

            <div className="mmp-library-index mmp-playlists-pane">
              <div className="mmp-playlists-title">
                <button
                  type="button"
                  className={view === "playlists" ? "is-active" : ""}
                  onClick={() => changeView("playlists")}
                >
                  <h3>Playlists</h3>
                  <small>{playlists.data?.items.length ?? 0}</small>
                </button>
                <Button
                  size="1"
                  variant="ghost"
                  color="gray"
                  aria-label="New playlist"
                  title="New playlist"
                  onClick={() => setCreateOpen(true)}
                >
                  <Plus size={14} aria-hidden="true" />
                </Button>
              </div>
              <ul>
                {(playlists.data?.items ?? []).map((playlist) => (
                  <li key={playlist.id}>
                    <button
                      type="button"
                      className={playlistID === playlist.id ? "is-active" : ""}
                      onClick={() => openPlaylist(playlist.id)}
                    >
                      <ListMusic size={17} aria-hidden="true" />
                      <span>
                        <strong>{playlist.name}</strong>
                        <small>
                          {playlist.track_count}{" "}
                          {playlist.track_count === 1 ? "track" : "tracks"} ·{" "}
                          {formatTrackDuration(playlist.duration_ms)}
                        </small>
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
              {playlists.loading && (
                <p className="mmp-index-status">Loading playlists…</p>
              )}
              {playlists.error && (
                <p className="mmp-index-status" role="alert">
                  {playlists.error}
                </p>
              )}
              {!playlists.loading && !playlists.data?.items.length && (
                <p className="mmp-index-status">
                  No playlists yet. Use + to create one.
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
                {(artistID || albumID || playlistID) && (
                  <button
                    type="button"
                    className="mmp-back-button"
                    onClick={() => update({ view, query, page: 1 })}
                  >
                    <ArrowLeft size={13} aria-hidden="true" />
                    {artistID
                      ? "All artists"
                      : albumID
                        ? "All albums"
                        : "All playlists"}
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

            {(artistID || albumID || playlistID || view === "tracks") && (
              <PlaybackActions
                tracks={activeTracks}
                scope={heading}
                onQueueOpen={() => setQueueOpen(true)}
                onShuffle={() => void shuffleAll()}
                shufflePending={shufflePending}
                onInvalidate={invalidateShuffle}
              />
            )}
            {shuffleError && (
              <Alert
                kind="danger"
                title="Could not shuffle the complete selection"
              >
                <p>{shuffleError}</p>
                <Button
                  size="1"
                  variant="soft"
                  onClick={() => void shuffleAll()}
                >
                  Retry
                </Button>
              </Alert>
            )}
            {playlistError && (
              <Alert kind="danger" title="Playlist update failed">
                {playlistError}
              </Alert>
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
              selectedPlaylistDetails && (
                <DetailHeading
                  artwork={
                    <span
                      className="mmp-artwork is-large mmp-playlist-artwork"
                      aria-hidden="true"
                    >
                      <ListMusic size={42} />
                    </span>
                  }
                  title={selectedPlaylistDetails.playlist.name}
                  subtitle={`${selectedPlaylistDetails.playlist.track_count} ${selectedPlaylistDetails.playlist.track_count === 1 ? "track" : "tracks"} · ${formatTrackDuration(selectedPlaylistDetails.playlist.duration_ms)}`}
                >
                  <span>Persistent library playlist</span>
                </DetailHeading>
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
            {!artistID && !albumID && !playlistID && view === "playlists" && (
              <>
                <RequestState
                  loading={playlists.loading}
                  error={playlists.error}
                  code={playlists.code}
                  retry={playlists.reload}
                />
                {!playlists.loading && !playlists.error && (
                  <PlaylistCards
                    items={playlists.data?.items ?? []}
                    onOpen={openPlaylist}
                    onCreate={() => setCreateOpen(true)}
                  />
                )}
              </>
            )}
            {(artistID || albumID || playlistID || view === "tracks") &&
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
                  onAddToPlaylist={setPickerTrack}
                  onInvalidate={invalidateShuffle}
                  playlist={
                    playlistID
                      ? {
                          busy: playlistBusy,
                          onRemove: (track) => void removePlaylistTrack(track),
                          onMove: (track, direction) =>
                            void movePlaylistTrack(track, direction),
                        }
                      : undefined
                  }
                  emptyMessage={
                    playlistID
                      ? "This playlist is empty. Add tracks from another library view."
                      : undefined
                  }
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
      <PlaylistCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={refreshPlaylists}
      />
      <PlaylistTrackPickerDialog
        track={pickerTrack}
        playlists={playlists.data?.items ?? []}
        onOpenChange={(open) => {
          if (!open) setPickerTrack(null);
        }}
        onChanged={refreshPlaylists}
      />
      <PlaylistRenameDialog
        playlist={renamePlaylist}
        onOpenChange={(open) => {
          if (!open) setRenamePlaylist(null);
        }}
        onChanged={refreshPlaylists}
      />
    </>
  );
}
