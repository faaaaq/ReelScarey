import { LibraryDeletion, selectionClick } from "../components/LibraryDeletion";
import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  ArrowLeft,
  Download,
  ExternalLink,
  FolderInput,
  ListMusic,
  Pencil,
  Play,
  RefreshCw,
  Search,
} from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Alert,
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
import {
  APIError,
  api,
  messageFor,
  refreshMusicAlbum,
  refreshMusicArtist,
  restoreMusicAlbumField,
  restoreMusicArtistField,
  type MusicAlbum,
  type MusicAlbumDetails,
  type MusicAlbumPreferences,
  type MusicAcquisitionJob,
  type MusicArtist,
  type MusicArtistDetails,
  type MusicArtistTrack,
  type MusicArtistTrackPage,
  type MusicDiscographyAlbum,
  type MusicDiscographyPage,
  type MusicEditJob,
  type MusicEditPlan,
  type MusicImportJob,
  type MusicJob,
  type MusicRelease,
  type MusicReleaseSearch,
  type MusicTopTrack,
  type MusicTopTrackPage,
} from "../api";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
import { useMusicPlayer, type PlayerTrack } from "../musicPlayer";
import { albumCoverURL, artistImageURL } from "../musicArtwork";
import {
  AcquisitionProgress,
  AlbumDownloadModal,
  MusicExportMenu,
  retryMusicAcquisitionTrack,
  type AlbumDownloadTarget,
} from "../musicDownload";
import { useMusicProfiles } from "../musicProfiles";
import "./Library.css";
import "./Music.css";
import { formatBytes, formatTrackDuration } from "../ui/format";

function statusKind(status: string, monitored: boolean) {
  if (status === "available") return "success";
  return monitored ? "danger" : "disabled";
}

type TileSelection = { checked: boolean; toggle: () => void };

function SelectionBox({
  selection,
  name,
}: {
  selection: TileSelection;
  name: string;
}) {
  return (
    <input
      type="checkbox"
      aria-label={`Select ${name}`}
      checked={selection.checked}
      onChange={selection.toggle}
    />
  );
}

function RecordSleeve({
  initial,
  anchor,
}: {
  initial: string;
  anchor?: string;
}) {
  return (
    <span className="mus-tile-fallback mus-record-sleeve" aria-hidden="true">
      {anchor && <span className="mus-tile-anchor">{anchor}</span>}
      <span className="mus-record">
        <span className="mus-record-label">{initial}</span>
      </span>
    </span>
  );
}

export function ArtistTile({
  artist,
  selection,
  anchor,
}: {
  artist: MusicArtist;
  selection?: TileSelection;
  anchor?: string;
}) {
  const [failed, setFailed] = useState(false);
  const initial = artist.name.trim().charAt(0).toUpperCase() || "?";
  const label =
    artist.status === "available" ? "In library" : "No registered files";
  const imageURL = artistImageURL(artist.id) || artist.image_url;
  return (
    <li
      className={selection?.checked ? "lib-card is-selected" : "lib-card"}
      data-selecting={!!selection || undefined}
      onClickCapture={selection && selectionClick(selection.toggle)}
    >
      {selection && (
        <span className="lib-card-select">
          <SelectionBox selection={selection} name={artist.name} />
        </span>
      )}
      <Link
        className="lib-card-art mus-card-art"
        to={`/music/artists/${artist.id}`}
        tabIndex={selection ? -1 : undefined}
        aria-label={`Open ${artist.name}`}
      >
        <RecordSleeve initial={initial} anchor={anchor} />
        {imageURL && !failed && (
          <img
            className="mus-tile-img"
            src={imageURL}
            alt=""
            loading="lazy"
            referrerPolicy="no-referrer"
            onError={() => setFailed(true)}
          />
        )}
      </Link>
      <span
        className={`lib-status-bar is-${statusKind(artist.status, artist.monitored)}`}
        title={label}
      />
      <div className="lib-card-info">
        <div className="lib-card-line">
          <h2 className="lib-card-title">
            <Link
              to={`/music/artists/${artist.id}`}
              tabIndex={selection ? -1 : undefined}
            >
              {artist.name}
            </Link>
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

export function AlbumTile({
  album,
  onDownload,
  showArtist = false,
  selection,
}: {
  album: MusicAlbum;
  onDownload?: (album: MusicAlbum) => void;
  showArtist?: boolean;
  selection?: TileSelection;
}) {
  const [failed, setFailed] = useState(false);
  const label =
    album.status === "available" ? "In library" : "No registered files";
  const type =
    [album.primary_type, ...(album.secondary_types ?? [])]
      .filter(Boolean)
      .join(" · ") || "Release";
  const coverURL = albumCoverURL(album.id) || album.cover_url;
  return (
    <li
      className={selection?.checked ? "lib-card is-selected" : "lib-card"}
      data-selecting={!!selection || undefined}
      onClickCapture={selection && selectionClick(selection.toggle)}
    >
      {selection && (
        <span className="lib-card-select">
          <SelectionBox selection={selection} name={album.title} />
        </span>
      )}
      <Link
        className="lib-card-art mus-card-art"
        to={`/music/albums/${album.id}`}
        tabIndex={selection ? -1 : undefined}
        aria-label={`Open ${album.title}`}
      >
        <RecordSleeve
          initial={album.title.trim().charAt(0).toUpperCase() || "?"}
        />
        {coverURL && !failed && (
          <img
            className="mus-tile-img"
            src={coverURL}
            alt=""
            loading="lazy"
            referrerPolicy="no-referrer"
            onError={() => setFailed(true)}
          />
        )}
      </Link>
      <span
        className={`lib-status-bar is-${statusKind(album.status, album.monitored)}`}
        title={label}
      />
      <div className="lib-card-info">
        <div className="lib-card-line">
          <h2 className="lib-card-title">
            <Link
              to={`/music/albums/${album.id}`}
              tabIndex={selection ? -1 : undefined}
            >
              {album.title}
            </Link>
          </h2>
        </div>
        <div className="lib-card-meta">
          {showArtist && album.artist_name && <span>{album.artist_name}</span>}
          <span>{album.year ?? "Year unknown"}</span>
          <span>
            {album.track_count
              ? `${album.track_count} ${album.track_count === 1 ? "track" : "tracks"}`
              : "Tracks not loaded"}
          </span>
        </div>
        <span className="lib-profile">{type}</span>
        {!selection && onDownload && album.status !== "available" && (
          <Button
            size="1"
            variant="soft"
            className="mus-card-download"
            onClick={() => onDownload(album)}
          >
            <Download size={14} aria-hidden="true" />
            Download
          </Button>
        )}
        <span className="sr-only">{label}</span>
      </div>
    </li>
  );
}

export function LibraryTrackTable({
  tracks,
  select,
  onDeleted,
}: {
  tracks: MusicArtistTrack[];
  select?: (id: number) => TileSelection;
  onDeleted?: () => void;
}) {
  const player = useMusicPlayer();
  const playable = tracks
    .map((track) => artistPlayerTrack(track, track.artist_name ?? ""))
    .filter((track): track is PlayerTrack => track !== null);
  return (
    <Table label="Music tracks">
      <thead>
        <tr>
          {select && (
            <th scope="col">
              <span className="sr-only">Select</span>
            </th>
          )}
          <th scope="col" className="is-shrink">
            #
          </th>
          <th scope="col">Title</th>
          <th scope="col">Artist</th>
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
          const selection = select && track.id ? select(track.id) : undefined;
          return (
            <tr
              data-selecting={!!selection || undefined}
              className={selection?.checked ? "is-selected" : undefined}
              onClickCapture={selection && selectionClick(selection.toggle)}
              key={
                track.id ??
                `${track.album_id}-${track.disc}-${track.number}-${track.title}`
              }
            >
              {select && (
                <td>
                  {selection && (
                    <SelectionBox selection={selection} name={track.title} />
                  )}
                </td>
              )}
              <td>
                {track.disc > 1
                  ? `${track.disc}.${track.number}`
                  : track.number}
              </td>
              <td>{track.title}</td>
              <td>{track.artist_name || ""}</td>
              <td>
                <Link
                  to={`/music/albums/${track.album_id}`}
                  tabIndex={select ? -1 : undefined}
                >
                  {track.album_title}
                </Link>
              </td>
              <td>{formatTrackDuration(track.duration_ms)}</td>
              <td className="is-actions">
                {fileID && !select ? (
                  <Button
                    size="1"
                    variant="ghost"
                    color="gray"
                    aria-label={`Play ${track.title}`}
                    onClick={() => {
                      const item = artistPlayerTrack(
                        track,
                        track.artist_name ?? "",
                      );
                      if (item) player.play(item, playable);
                    }}
                  >
                    <Play size={14} aria-hidden="true" />
                  </Button>
                ) : null}
                {!select && onDeleted && track.id && (
                  <LibraryDeletion
                    selection="tracks"
                    ids={[track.id]}
                    label="Delete track"
                    name={track.title}
                    onDone={onDeleted}
                  />
                )}
              </td>
            </tr>
          );
        })}
      </tbody>
    </Table>
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
    coverURL: albumCoverURL(track.album_id) || track.cover_url || undefined,
    durationMS: track.duration_ms,
    albumID: track.album_id,
  };
}

function ArtistTrackTable({
  tracks,
  label,
  artistName,
  onDeleted,
}: {
  tracks: MusicArtistTrack[];
  label: string;
  artistName: string;
  onDeleted: () => void;
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
              <td>{formatTrackDuration(track.duration_ms)}</td>
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
                {track.id && (
                  <LibraryDeletion
                    selection="tracks"
                    ids={[track.id]}
                    label="Delete track"
                    name={track.title}
                    onDone={onDeleted}
                  />
                )}
              </td>
            </tr>
          );
        })}
      </tbody>
    </Table>
  );
}

function topTrackPlayer(track: MusicTopTrack): PlayerTrack | null {
  if (!track.file_id) return null;
  return {
    fileID: track.file_id,
    title: track.title,
    artist: track.artist,
    album: track.album,
    coverURL: track.cover_url || undefined,
    durationMS: track.duration_ms,
    albumID: track.album_id ?? undefined,
  };
}

function TopTrackTable({
  tracks,
  onDownload,
  selected,
  onToggle,
}: {
  tracks: MusicTopTrack[];
  onDownload: (track: MusicTopTrack) => void;
  selected: string[];
  onToggle: (providerID: string, checked: boolean) => void;
}) {
  const player = useMusicPlayer();
  const playable = tracks
    .map(topTrackPlayer)
    .filter((track): track is PlayerTrack => track !== null);
  function play(track: MusicTopTrack) {
    const item = topTrackPlayer(track);
    if (item) player.play(item, playable);
  }
  return (
    <Table label="Top tracks">
      <thead>
        <tr>
          <th scope="col" className="is-shrink">
            <span className="sr-only">Select</span>
          </th>
          <th scope="col" className="is-shrink">
            #
          </th>
          <th scope="col">Title</th>
          <th scope="col">Album</th>
          <th scope="col">Duration</th>
          <th scope="col" className="is-actions">
            <span className="sr-only">Actions</span>
          </th>
        </tr>
      </thead>
      <tbody>
        {tracks.map((track, index) => (
          <tr key={`${track.provider_id}-${index}`}>
            <td>
              {!track.in_library && (
                <input
                  type="checkbox"
                  aria-label={`Select ${track.title}`}
                  checked={selected.includes(track.provider_id)}
                  onChange={(event) =>
                    onToggle(track.provider_id, event.target.checked)
                  }
                />
              )}
            </td>
            <td>{index + 1}</td>
            <td>
              {track.title}{" "}
              {track.in_library && <Label kind="success">In library</Label>}
            </td>
            <td>{track.album || ""}</td>
            <td>{formatTrackDuration(track.duration_ms)}</td>
            <td className="is-actions">
              {track.file_id ? (
                <Button
                  size="1"
                  variant="ghost"
                  color="gray"
                  aria-label={`Play ${track.title}`}
                  onClick={() => play(track)}
                >
                  <Play size={14} aria-hidden="true" />
                </Button>
              ) : (
                <Button
                  size="1"
                  variant="soft"
                  aria-label={`Search and download ${track.title}`}
                  onClick={() => onDownload(track)}
                >
                  <Download size={14} aria-hidden="true" />
                  Download
                </Button>
              )}
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

function DiscographyTile({
  album,
  onDownload,
}: {
  album: MusicDiscographyAlbum;
  onDownload: (album: MusicDiscographyAlbum) => void;
}) {
  const [failed, setFailed] = useState(false);
  const coverURL = albumCoverURL(album.album_id) || album.cover_url;
  const title = album.album_id ? (
    <Link to={`/music/albums/${album.album_id}`}>{album.title}</Link>
  ) : (
    album.title
  );
  return (
    <li className="lib-card">
      <div className="lib-card-art mus-card-art">
        <RecordSleeve
          initial={album.title.trim().charAt(0).toUpperCase() || "?"}
        />
        {coverURL && !failed && (
          <img
            className="mus-tile-img"
            src={coverURL}
            alt=""
            loading="lazy"
            referrerPolicy="no-referrer"
            onError={() => setFailed(true)}
          />
        )}
      </div>
      <span
        className={`lib-status-bar is-${album.in_library ? "success" : "danger"}`}
        title={album.in_library ? "In library" : "Missing"}
      />
      <div className="lib-card-info">
        <div className="lib-card-line">
          <h2 className="lib-card-title">{title}</h2>
        </div>
        <div className="lib-card-meta">
          <span>{album.year ?? "Year unknown"}</span>
          <span>
            {album.track_count
              ? `${album.track_count} ${album.track_count === 1 ? "track" : "tracks"}`
              : album.status || "Album"}
          </span>
        </div>
        <span className="lib-profile">
          {album.in_library ? "In library" : "Not in library"}
        </span>
        {!album.in_library && (
          <Button
            size="1"
            variant="soft"
            className="mus-card-download"
            onClick={() => onDownload(album)}
          >
            <Download size={14} aria-hidden="true" />
            Download album
          </Button>
        )}
      </div>
    </li>
  );
}

export function MusicArtistPage() {
  const { id } = useParams();
  return <MusicArtistDetail key={id} id={id} />;
}

function ArtistAlbums({
  albums,
  remote,
  showAll,
  loading,
  error,
  toggle,
  onDownload,
}: {
  albums: MusicAlbum[];
  remote: MusicDiscographyAlbum[];
  showAll: boolean;
  loading: boolean;
  error: string;
  toggle: () => void;
  onDownload: (target: AlbumDownloadTarget) => void;
}) {
  const saved = albums.filter(
    (album) => showAll || album.status === "available",
  );
  const savedIDs = new Set(saved.map((album) => album.id));
  const provider = remote.filter(
    (album) => album.album_id == null || !savedIDs.has(album.album_id),
  );
  const localSingles = saved.filter(
    (album) => album.primary_type.toLowerCase() === "single",
  );
  const providerSingles = provider.filter(
    (album) => album.status.toLowerCase() === "single",
  );
  function localDownload(album: MusicAlbum) {
    onDownload({
      albumID: album.id,
      title: album.title,
      artist: album.artist_name,
    });
  }
  function remoteDownload(album: MusicDiscographyAlbum) {
    onDownload({
      albumID: album.album_id,
      providerID: album.provider_id,
      title: album.title,
      artist: album.artist,
    });
  }
  return (
    <section className="mus-section" aria-labelledby="artist-albums">
      <div className="mus-section-head">
        <h2 id="artist-albums">Albums</h2>
        <Button variant="soft" onClick={toggle}>
          {showAll ? "Show library albums" : "Show all albums"}
        </Button>
      </div>
      {loading && <p role="status">Loading discography…</p>}
      {error && <Alert kind="warning">{error} Showing saved albums.</Alert>}
      {!saved.length && !provider.length && !loading && (
        <p className="rs-muted">No albums to show.</p>
      )}
      <ul className="lib-grid" aria-label="Music albums">
        {saved
          .filter((album) => !localSingles.includes(album))
          .map((album) => (
            <AlbumTile
              key={album.id}
              album={album}
              onDownload={localDownload}
            />
          ))}
        {provider
          .filter((album) => !providerSingles.includes(album))
          .map((album) => (
            <DiscographyTile
              key={album.provider_id}
              album={album}
              onDownload={remoteDownload}
            />
          ))}
        {(localSingles.length > 0 || providerSingles.length > 0) && (
          <li className="lib-card">
            <details className="lib-card-info">
              <summary>
                Singles ({localSingles.length + providerSingles.length})
              </summary>
              <ul>
                {localSingles.map((single) => (
                  <li key={single.id}>
                    <Link to={`/music/albums/${single.id}`}>
                      {single.title}
                    </Link>
                  </li>
                ))}
                {providerSingles.map((single) => (
                  <li key={single.provider_id}>
                    <Button
                      size="1"
                      variant="ghost"
                      onClick={() => remoteDownload(single)}
                    >
                      {single.title}
                    </Button>
                  </li>
                ))}
              </ul>
            </details>
          </li>
        )}
      </ul>
    </section>
  );
}

function MusicArtistDetail({ id }: { id?: string }) {
  const navigate = useNavigate();
  const [showAllAlbums, setShowAllAlbums] = useState(false);
  const details = useResource<MusicArtistDetails>(
    id ? `/library/music/artists/${id}` : null,
  );
  const topTracks = useResource<MusicTopTrackPage>(
    id ? `/library/music/artists/${id}/top-tracks?limit=20` : null,
  );
  const discography = useResource<MusicDiscographyPage>(
    id && showAllAlbums
      ? `/library/music/artists/${id}/discography?limit=100`
      : null,
  );
  const [selectedTopTracks, setSelectedTopTracks] = useState<string[]>([]);
  const [albumDownload, setAlbumDownload] =
    useState<AlbumDownloadTarget | null>(null);
  const [allOpen, setAllOpen] = useState(false);
  const [allPage, setAllPage] = useState(1);
  const allTracks = useResource<MusicArtistTrackPage>(
    allOpen && id
      ? `/library/music/artists/${id}/tracks?page=${allPage}&limit=50`
      : null,
  );
  const libraryTracks = useResource<MusicArtistTrackPage>(
    id ? `/library/music/artists/${id}/tracks?limit=1` : null,
  );
  const [downloadTrack, setDownloadTrack] = useState<MusicTopTrack | null>(
    null,
  );
  const [downloadFormat, setDownloadFormat] = useState("source");
  const [downloadRoot, setDownloadRoot] = useState("Music");
  const [downloadPending, setDownloadPending] = useState(false);
  const [downloadError, setDownloadError] = useState("");
  const [downloadJobID, setDownloadJobID] = useState<number | null>(null);
  const [batchJobID, setBatchJobID] = useState<number | null>(null);
  const [imageVersion, setImageVersion] = useState(0);
  const [refreshPending, setRefreshPending] = useState(false);
  const [refreshNotice, setRefreshNotice] = useState("");
  const [refreshError, setRefreshError] = useState("");
  const downloadJob = useLiveResource<MusicAcquisitionJob>(
    downloadJobID ? `/library/music/acquisitions/${downloadJobID}` : null,
    2000,
  );
  const batchJob = useLiveResource<MusicAcquisitionJob>(
    batchJobID ? `/library/music/acquisitions/${batchJobID}` : null,
    2000,
  );
  const [albumJobID, setAlbumJobID] = useState<number | null>(null);
  const albumJob = useLiveResource<MusicAcquisitionJob>(
    albumJobID ? `/library/music/acquisitions/${albumJobID}` : null,
    2000,
  );
  const artist = details.data?.artist;
  const player = useMusicPlayer();
  const playableTop = (topTracks.data?.items ?? [])
    .map(topTrackPlayer)
    .filter((track): track is PlayerTrack => track !== null);
  const missingTop = (topTracks.data?.items ?? []).filter(
    (track) => !track.in_library,
  );
  const selectedTop = missingTop.filter((track) =>
    selectedTopTracks.includes(track.provider_id),
  );
  const allPages = allTracks.data
    ? Math.ceil(allTracks.data.total / allTracks.data.limit)
    : 1;

  function openDownload(track: MusicTopTrack) {
    setDownloadTrack(track);
    setDownloadJobID(null);
    setDownloadError("");
    setDownloadFormat("source");
    setDownloadRoot("Music");
  }

  async function refreshArtist() {
    if (!id || refreshPending) return;
    setRefreshPending(true);
    setRefreshError("");
    setRefreshNotice("");
    try {
      const result = await refreshMusicArtist(Number(id));
      const parts: string[] = [];
      if (result.image_updated) parts.push("a new artist image");
      if (result.image_cleared)
        parts.push("the unverified artist image removed");
      if (result.artist_updated) parts.push("the artist details");
      if (result.albums_updated > 0)
        parts.push(
          `${result.albums_updated} album cover${result.albums_updated === 1 ? "" : "s"}`,
        );
      setRefreshNotice(
        parts.length > 0
          ? `Refreshed ${parts.join(", ")}.`
          : result.message || "The stored metadata was already current.",
      );
      setImageVersion((version) => version + 1);
      await Promise.all([
        details.reload(),
        discography.reload(),
        topTracks.reload(),
        libraryTracks.reload(),
      ]);
    } catch (e) {
      setRefreshError(messageFor(e));
    } finally {
      setRefreshPending(false);
    }
  }

  async function restoreArtistName() {
    if (!id || refreshPending) return;
    setRefreshPending(true);
    setRefreshError("");
    setRefreshNotice("");
    try {
      const result = await restoreMusicArtistField(Number(id), "name");
      setRefreshNotice(result.message);
      await Promise.all([
        details.reload(),
        discography.reload(),
        topTracks.reload(),
        libraryTracks.reload(),
      ]);
    } catch (e) {
      setRefreshError(messageFor(e));
    } finally {
      setRefreshPending(false);
    }
  }

  async function startTopDownload() {
    if (!downloadTrack || downloadPending) return;
    setDownloadPending(true);
    setDownloadError("");
    try {
      const job = await api<MusicAcquisitionJob>(
        "/library/music/acquisitions",
        {
          method: "POST",
          body: JSON.stringify({
            source_kind: "top_track",
            title: downloadTrack.title,
            root: downloadRoot.trim() || "Music",
            format: downloadFormat,
            reviewed: true,
            tracks: [
              {
                title: downloadTrack.title,
                artist: downloadTrack.artist || artist?.name || "",
                album: downloadTrack.album || downloadTrack.title,
                album_artist: artist?.name || downloadTrack.artist || "",
                year: null,
                track_number: 1,
                disc_number: 1,
                duration_ms: downloadTrack.duration_ms,
                cover_url: downloadTrack.cover_url,
                source_url: "",
                search_query: [downloadTrack.artist, downloadTrack.title]
                  .filter(Boolean)
                  .join(" "),
              },
            ],
          }),
        },
      );
      setDownloadJobID(job.id);
      topTracks.reload();
    } catch (e) {
      setDownloadError(messageFor(e));
    } finally {
      setDownloadPending(false);
    }
  }

  async function startTopBatch() {
    if (selectedTop.length === 0 || downloadPending) return;
    setDownloadPending(true);
    setDownloadError("");
    try {
      const job = await api<MusicAcquisitionJob>(
        "/library/music/acquisitions",
        {
          method: "POST",
          body: JSON.stringify({
            source_kind: "top_tracks",
            title: `${artist?.name ?? "Artist"} top tracks`,
            root: "Music",
            format: "source",
            reviewed: true,
            tracks: selectedTop.map((track) => ({
              title: track.title,
              artist: track.artist || artist?.name || "",
              album: track.album || track.title,
              album_artist: artist?.name || track.artist || "",
              year: null,
              track_number: 1,
              disc_number: 1,
              duration_ms: track.duration_ms,
              cover_url: track.cover_url,
              source_url: "",
              search_query: [track.artist, track.title]
                .filter(Boolean)
                .join(" "),
            })),
          }),
        },
      );
      setBatchJobID(job.id);
      setSelectedTopTracks([]);
      topTracks.reload();
    } catch (e) {
      setDownloadError(messageFor(e));
    } finally {
      setDownloadPending(false);
    }
  }

  function toggleTopTrack(providerID: string, checked: boolean) {
    setSelectedTopTracks((current) =>
      checked
        ? [...new Set([...current, providerID])]
        : current.filter((value) => value !== providerID),
    );
  }
  const topTrackState = downloadJob.data?.tracks?.[0]?.state || "";
  const topTrackError = downloadJob.data?.tracks?.[0]?.error || "";

  useEffect(() => {
    if (
      downloadJob.data?.state === "completed" ||
      batchJob.data?.state === "completed"
    ) {
      topTracks.reload();
      libraryTracks.reload();
    }
  }, [downloadJob.data?.state, batchJob.data?.state]);
  useEffect(() => {
    if (albumJob.data?.state === "completed") {
      discography.reload();
      libraryTracks.reload();
    }
  }, [albumJob.data?.state]);
  return (
    <>
      <PageToolbar label="Music artist actions">
        <ToolbarSection>
          <ToolbarButton icon={ArrowLeft} label="Artists" to="/music" />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh metadata"
            title="Re-resolve the artist's details, image and album covers from the providers"
            spinning={refreshPending}
            onClick={refreshArtist}
          />
          {artist?.name_overridden && (
            <ToolbarButton
              icon={RefreshCw}
              label="Use MusicBrainz name"
              title="Restore the provider artist name in the catalog only; audio files are not retagged"
              spinning={refreshPending}
              onClick={restoreArtistName}
            />
          )}
          {artist?.id && (
            <LibraryDeletion
              selection="artists"
              ids={[artist.id]}
              label="Delete artist"
              name={artist.name}
              onDone={() => navigate("/music")}
            />
          )}
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
        >
          {artist?.name_overridden && (
            <Label kind="info" outline>
              Artist name: owner override
            </Label>
          )}
          {artist?.id ? (
            <img
              className="mus-heading-cover"
              src={`${artistImageURL(artist.id)}${imageVersion ? `?v=${imageVersion}` : ""}`}
              alt=""
              loading="lazy"
              referrerPolicy="no-referrer"
              onError={(event) => {
                event.currentTarget.style.display = "none";
              }}
            />
          ) : null}
        </PageHeading>
        <RequestState
          loading={details.loading}
          error={details.error}
          code={details.code}
          retry={details.reload}
        />
        {refreshNotice && !refreshError && (
          <Alert kind="success" title="Metadata refreshed">
            {refreshNotice}
          </Alert>
        )}
        {refreshError && (
          <Alert kind="danger" title="Metadata refresh failed">
            {refreshError}
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
              onRetry={async (trackID, sourceURL) => {
                if (!albumJobID) return;
                await retryMusicAcquisitionTrack(
                  albumJobID,
                  trackID,
                  sourceURL,
                );
                albumJob.reload();
              }}
            />
          </FieldSet>
        )}
        {details.data && (
          <ArtistAlbums
            albums={details.data.albums}
            remote={showAllAlbums ? (discography.data?.items ?? []) : []}
            showAll={showAllAlbums}
            loading={showAllAlbums && discography.loading}
            error={showAllAlbums ? discography.error : ""}
            toggle={() => setShowAllAlbums((value) => !value)}
            onDownload={setAlbumDownload}
          />
        )}

        {details.data && (
          <section className="mus-section" aria-labelledby="artist-top-tracks">
            <div className="mus-section-head">
              <div>
                <h2 id="artist-top-tracks">Top tracks</h2>
                <p className="rs-muted rs-small">
                  Overall {topTracks.data?.source || "provider"} top tracks.
                  Play ones already in your library, or download the rest.
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
                {missingTop.length > 0 && (
                  <Button
                    variant="soft"
                    onClick={() =>
                      setSelectedTopTracks(
                        selectedTopTracks.length === missingTop.length
                          ? []
                          : missingTop.map((track) => track.provider_id),
                      )
                    }
                  >
                    {selectedTopTracks.length === missingTop.length
                      ? "Clear selection"
                      : `Select missing (${missingTop.length})`}
                  </Button>
                )}
                {selectedTop.length > 0 && (
                  <Button
                    disabled={downloadPending}
                    onClick={() => void startTopBatch()}
                  >
                    <Download size={15} aria-hidden="true" />
                    Download selected ({selectedTop.length})
                  </Button>
                )}
                {libraryTracks.data && libraryTracks.data.total > 0 && (
                  <Button
                    variant="soft"
                    onClick={() => {
                      setAllPage(1);
                      setAllOpen(true);
                    }}
                  >
                    <ListMusic size={15} aria-hidden="true" />
                    All library tracks ({libraryTracks.data.total})
                  </Button>
                )}
              </div>
            </div>
            {topTracks.loading && (
              <p className="rs-muted">Loading top tracks</p>
            )}
            {topTracks.error && <Alert kind="danger">{topTracks.error}</Alert>}
            {topTracks.data?.message && (
              <p className="rs-muted">{topTracks.data.message}</p>
            )}
            {topTracks.data &&
              (topTracks.data.items.length ? (
                <TopTrackTable
                  tracks={topTracks.data.items}
                  onDownload={openDownload}
                  selected={selectedTopTracks}
                  onToggle={toggleTopTrack}
                />
              ) : (
                !topTracks.data.message && (
                  <p className="rs-muted">
                    No top tracks are available for this artist right now.
                  </p>
                )
              ))}
            {downloadError && !downloadTrack && (
              <Alert kind="danger">{downloadError}</Alert>
            )}
            {batchJob.data && (
              <Alert
                kind={
                  batchJob.data.state === "completed"
                    ? "success"
                    : batchJob.data.state === "failed"
                      ? "danger"
                      : "info"
                }
                title={`Batch acquisition #${batchJob.data.id}`}
              >
                {batchJob.data.state} · {batchJob.data.completed_count}/
                {batchJob.data.track_count} completed
                {batchJob.data.error ? ` · ${batchJob.data.error}` : ""}
              </Alert>
            )}
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
        {allTracks.loading && <p className="rs-muted">Loading tracks</p>}
        {allTracks.error && <Alert kind="danger">{allTracks.error}</Alert>}
        {allTracks.data &&
          (allTracks.data.items.length ? (
            <ArtistTrackTable
              tracks={allTracks.data.items}
              label="All tracks"
              onDeleted={() => {
                allTracks.reload();
                libraryTracks.reload();
                details.reload();
                topTracks.reload();
              }}
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
      <Modal
        open={Boolean(downloadTrack)}
        onOpenChange={(open) => {
          if (!open) {
            setDownloadTrack(null);
            setDownloadJobID(null);
            setDownloadError("");
          }
        }}
        title="Download top track"
        description="Import the matched audio through the reviewed music import pipeline."
        footer={
          <>
            <Button
              variant="soft"
              color="gray"
              onClick={() => {
                setDownloadTrack(null);
                setDownloadJobID(null);
                setDownloadError("");
              }}
            >
              Close
            </Button>
            <Button
              disabled={downloadPending || Boolean(downloadJobID)}
              onClick={() => void startTopDownload()}
            >
              <Download size={15} aria-hidden="true" />
              {downloadPending ? "Starting" : "Download"}
            </Button>
          </>
        }
      >
        {downloadTrack && (
          <>
            <p>
              <strong>{downloadTrack.title}</strong>
              {downloadTrack.artist ? ` · ${downloadTrack.artist}` : ""}
              {downloadTrack.album ? ` · ${downloadTrack.album}` : ""}
            </p>
            <div className="mus-search">
              <label>
                Format{" "}
                <select
                  aria-label="Download format"
                  value={downloadFormat}
                  onChange={(event) => setDownloadFormat(event.target.value)}
                  disabled={Boolean(downloadJobID)}
                >
                  <option value="source">Best available source</option>
                  <option value="mp3">MP3</option>
                  <option value="flac">FLAC</option>
                  <option value="m4a">M4A</option>
                  <option value="opus">Opus</option>
                </select>
              </label>
              <TextField.Root
                aria-label="Destination root"
                value={downloadRoot}
                onChange={(event) => setDownloadRoot(event.target.value)}
                disabled={Boolean(downloadJobID)}
              />
            </div>
            <Alert kind="info" title="Review before downloading">
              TIDAL is the default download source whenever a TIDAL account is
              connected in Music settings; otherwise the worker searches
              YouTube. Only a close duration match is imported, and an existing
              library file is never replaced. The source preference in Music
              settings can also restrict downloads to one provider.
            </Alert>
            {downloadError && <Alert kind="danger">{downloadError}</Alert>}
            {downloadJob.data && (
              <Alert
                kind={
                  downloadJob.data.state === "completed"
                    ? "success"
                    : downloadJob.data.state === "failed"
                      ? "danger"
                      : "info"
                }
                title={`Acquisition #${downloadJob.data.id}`}
              >
                <p>
                  {downloadJob.data.state}
                  {topTrackState && topTrackState !== downloadJob.data.state
                    ? ` · ${topTrackState}`
                    : ""}
                </p>
                {downloadJob.data.error && <p>{downloadJob.data.error}</p>}
                {topTrackError && <p>{topTrackError}</p>}
              </Alert>
            )}
          </>
        )}
      </Modal>
      <AlbumDownloadModal
        target={albumDownload}
        onClose={() => setAlbumDownload(null)}
        onJob={(job) => setAlbumJobID(job.id)}
        onStarted={() => {
          discography.reload();
          libraryTracks.reload();
        }}
      />
    </>
  );
}

export function MusicAlbumPage() {
  const navigate = useNavigate();
  const { id } = useParams();
  const details = useResource<MusicAlbumDetails>(
    id ? `/library/music/albums/${id}` : null,
  );
  const album = details.data?.album;
  const tracks = details.data?.tracks ?? [];
  const multiDisc = new Set(tracks.map((track) => track.disc)).size > 1;
  const [coverVersion, setCoverVersion] = useState(0);
  const albumCover = album
    ? `${albumCoverURL(album.id) || album.cover_url}${coverVersion ? `?v=${coverVersion}` : ""}`
    : "";
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
                  coverURL:
                    albumCoverURL(album.id) || album.cover_url || undefined,
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
  const [albumJobID, setAlbumJobID] = useState<number | null>(null);
  const albumJob = useLiveResource<MusicAcquisitionJob>(
    albumJobID ? `/library/music/acquisitions/${albumJobID}` : null,
    2000,
  );
  const albumPrefs = useResource<MusicAlbumPreferences>(
    id ? `/library/music/albums/${id}/preferences` : null,
  );
  const [allowLossless, setAllowLossless] = useState(true);
  const [minBitrate, setMinBitrate] = useState("0");
  const [formats, setFormats] = useState("");
  const [albumProfile, setAlbumProfile] = useState("");
  const { profiles: albumProfileChoices } = useMusicProfiles();
  const [prefsPending, setPrefsPending] = useState(false);
  const [albumDownload, setAlbumDownload] =
    useState<AlbumDownloadTarget | null>(null);
  const [fixOpen, setFixOpen] = useState(false);
  const [fixYear, setFixYear] = useState("");
  const [fixAlbum, setFixAlbum] = useState("");
  const [fixArtist, setFixArtist] = useState("");
  const [fixPlan, setFixPlan] = useState<MusicEditPlan | null>(null);
  const [fixPending, setFixPending] = useState(false);
  const [fixMessage, setFixMessage] = useState("");
  const [refreshPending, setRefreshPending] = useState(false);
  const [refreshNotice, setRefreshNotice] = useState("");
  const [refreshError, setRefreshError] = useState("");
  const hasMissingTracks = tracks.some((track) => !track.file_id);
  const fixOverrides = () => ({
    album_id: album?.id ?? 0,
    year: Number(fixYear) || 0,
    album: fixAlbum.trim(),
    artist: fixArtist.trim(),
  });

  useEffect(() => {
    if (!albumPrefs.data?.preferences) return;
    setAllowLossless(albumPrefs.data.preferences.allow_lossless);
    setMinBitrate(String(albumPrefs.data.preferences.min_bitrate));
    setFormats(albumPrefs.data.preferences.preferred_formats.join(","));
    setAlbumProfile(albumPrefs.data.preferences.download_profile || "");
  }, [albumPrefs.data]);

  useEffect(() => {
    if (albumJob.data?.state === "completed") details.reload();
  }, [albumJob.data?.state]);

  async function refreshAlbum() {
    if (!id || refreshPending) return;
    setRefreshPending(true);
    setRefreshError("");
    setRefreshNotice("");
    try {
      const result = await refreshMusicAlbum(Number(id));
      const parts: string[] = [];
      if (result.cover_updated) parts.push("a new cover");
      if (result.cover_cleared) parts.push("the unverifiable cover removed");
      if (result.album_updated) parts.push("the title and year");
      setRefreshNotice(
        parts.length > 0
          ? `Refreshed ${parts.join(", ")}.`
          : result.message || "The stored metadata was already current.",
      );
      setCoverVersion((version) => version + 1);
      await details.reload();
    } catch (e) {
      setRefreshError(messageFor(e));
    } finally {
      setRefreshPending(false);
    }
  }

  async function restoreAlbumField(field: "title" | "year") {
    if (!id || refreshPending) return;
    setRefreshPending(true);
    setRefreshError("");
    setRefreshNotice("");
    try {
      const result = await restoreMusicAlbumField(Number(id), field);
      setRefreshNotice(result.message);
      await details.reload();
    } catch (e) {
      setRefreshError(messageFor(e));
    } finally {
      setRefreshPending(false);
    }
  }

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
          // Forward the loaded preferences (including the transcoding fields
          // managed on the Music settings page) so this save cannot reset
          // them; the revision fence rejects a stale save regardless.
          ...albumPrefs.data.preferences,
          allow_lossless: allowLossless,
          min_bitrate: Number(minBitrate) || 0,
          download_profile: albumProfile,
          preferred_formats: formats
            .split(",")
            .map((value) => value.trim())
            .filter(Boolean),
          revision: albumPrefs.data.revision,
        }),
      });
      albumPrefs.reload();
      setError("");
    } catch (e) {
      if (
        e instanceof APIError &&
        (e.status === 409 || e.code === "music_revision_conflict")
      ) {
        // Another save won the revision fence. Rebase the form from the
        // server so the conflict loop ends; the current edit is discarded.
        albumPrefs.reload();
        setError(
          "The album preferences changed while you were working. The saved preferences have been reloaded  review them and apply your changes again.",
        );
      } else {
        setError(messageFor(e));
      }
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

  async function openFixMetadata() {
    setFixYear(album?.year ? String(album.year) : "");
    setFixAlbum(album?.title ?? "");
    setFixArtist(album?.artist_name ?? "");
    setFixPlan(null);
    setFixMessage("");
    setError("");
    setFixOpen(true);
  }

  async function previewFixMetadata(e: FormEvent) {
    e.preventDefault();
    if (!album?.id || fixPending) return;
    const overrides = fixOverrides();
    if (!overrides.year && !overrides.album && !overrides.artist) {
      setFixMessage("Enter a year, album title or artist to change.");
      return;
    }
    setFixPending(true);
    setError("");
    setFixMessage("");
    try {
      const plan = await api<MusicEditPlan>("/library/music/edits/preview", {
        method: "POST",
        body: JSON.stringify(overrides),
      });
      setFixPlan(plan);
      setFixMessage(
        `${plan.files.length} file${plan.files.length === 1 ? "" : "s"} will be rewritten. Review the plan, then apply.`,
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setFixPending(false);
    }
  }

  async function applyFixMetadata() {
    if (!album?.id || !fixPlan || fixPending) return;
    setFixPending(true);
    setError("");
    try {
      const job = await api<MusicEditJob>("/library/music/edits/apply", {
        method: "POST",
        body: JSON.stringify({
          ...fixOverrides(),
          plan_hash: fixPlan.plan_hash,
          reviewed: true,
        }),
      });
      setFixPlan(null);
      setFixMessage(
        job.state === "completed"
          ? `Updated ${job.file_count} file${job.file_count === 1 ? "" : "s"}.`
          : job.error
            ? `Correction ${job.state}: ${job.error}`
            : `Correction ${job.state}.`,
      );
      details.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setFixPending(false);
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
            icon={Download}
            label="Download album"
            disabled={!album?.id || !hasMissingTracks}
            onClick={() =>
              album?.id &&
              setAlbumDownload({
                albumID: album.id,
                title: album.title,
                artist: album.artist_name,
              })
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
          <ToolbarButton
            icon={Pencil}
            label="Fix metadata"
            disabled={!album?.id || tracks.length === 0}
            onClick={openFixMetadata}
          />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh metadata"
            title="Re-resolve the album's cover art and canonical details from the providers"
            spinning={refreshPending}
            onClick={refreshAlbum}
          />
          {album?.title_overridden && (
            <ToolbarButton
              icon={RefreshCw}
              label="Use MusicBrainz title"
              title="Restore the provider album title in the catalog only; audio files are not retagged"
              spinning={refreshPending}
              onClick={() => void restoreAlbumField("title")}
            />
          )}
          {album?.year_overridden && (
            <ToolbarButton
              icon={RefreshCw}
              label="Use MusicBrainz year"
              title="Restore the provider release year in the catalog only; audio files are not retagged"
              spinning={refreshPending}
              onClick={() => void restoreAlbumField("year")}
            />
          )}
          {album?.id && (
            <LibraryDeletion
              selection="albums"
              ids={[album.id]}
              label="Delete album"
              name={album.title}
              onDone={() =>
                navigate(
                  album.artist_id
                    ? `/music/artists/${album.artist_id}`
                    : "/music",
                )
              }
            />
          )}
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
              {album.title_overridden && (
                <Label kind="info" outline>
                  Album title: owner override
                </Label>
              )}
              {album.year_overridden && (
                <Label kind="info" outline>
                  Release year: owner override
                </Label>
              )}
              {albumCover && (
                <img
                  className="mus-heading-cover"
                  src={albumCover}
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
          code={details.code}
          retry={details.reload}
        />
        {refreshNotice && !refreshError && (
          <Alert kind="success" title="Metadata refreshed">
            {refreshNotice}
          </Alert>
        )}
        {refreshError && (
          <Alert kind="danger" title="Metadata refresh failed">
            {refreshError}
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
              onRetry={async (trackID, sourceURL) => {
                if (!albumJobID) return;
                await retryMusicAcquisitionTrack(
                  albumJobID,
                  trackID,
                  sourceURL,
                );
                albumJob.reload();
              }}
            />
          </FieldSet>
        )}
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
                    <td>{formatTrackDuration(track.duration_ms)}</td>
                    <td className="is-actions">
                      {fileID ? (
                        <>
                          <Button
                            size="1"
                            variant="ghost"
                            color="gray"
                            aria-label={`Play ${track.title}`}
                            onClick={() => playTrack(fileID)}
                          >
                            <Play size={14} aria-hidden="true" />
                          </Button>{" "}
                          <MusicExportMenu
                            fileID={fileID}
                            title={track.title}
                          />
                        </>
                      ) : null}
                      {track.id && (
                        <LibraryDeletion
                          selection="tracks"
                          ids={[track.id]}
                          label="Delete track"
                          name={track.title}
                          onDone={details.reload}
                        />
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        )}
        {!!details.data?.files?.length && (
          <FieldSet legend="Registered files">
            <Table label="Registered music files">
              <thead>
                <tr>
                  <th scope="col">File</th>
                  <th scope="col">Track</th>
                  <th scope="col">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {details.data.files.map((file) => (
                  <tr key={file.id}>
                    <td>
                      <code>{file.relative_path}</code>
                    </td>
                    <td>{file.track_title || "—"}</td>
                    <td>
                      <LibraryDeletion
                        selection="music_files"
                        ids={[file.id]}
                        label="Delete file"
                        name={file.relative_path}
                        onDone={details.reload}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </FieldSet>
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
              <label>
                Download profile{" "}
                <select
                  aria-label="Album download profile"
                  value={albumProfile}
                  onChange={(e) => setAlbumProfile(e.target.value)}
                >
                  <option value="">Use global default</option>
                  {albumProfileChoices.map((profile) => (
                    <option
                      key={profile.key || profile.name}
                      value={profile.key || profile.name}
                    >
                      {profile.name}
                    </option>
                  ))}
                </select>
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
                          {file.destination_path || ""}
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
                      <td>{file.error || ""}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </FieldSet>
        )}
      </PageBody>
      <Modal
        open={fixOpen}
        onOpenChange={setFixOpen}
        title="Correct album metadata"
        description="Rewrites the year, album title and artist tags of every file on this album, with a reviewed plan and rollback."
        footer={
          <Button variant="soft" color="gray" onClick={() => setFixOpen(false)}>
            Close
          </Button>
        }
      >
        <form
          className="mus-search"
          onSubmit={(e) => void previewFixMetadata(e)}
        >
          <label>
            Year{" "}
            <TextField.Root
              aria-label="Album year"
              placeholder="e.g. 1998"
              value={fixYear}
              onChange={(e) => {
                setFixYear(e.target.value);
                setFixPlan(null);
              }}
              style={{ width: 100 }}
            />
          </label>
          <label>
            Album{" "}
            <TextField.Root
              aria-label="Album title"
              value={fixAlbum}
              onChange={(e) => {
                setFixAlbum(e.target.value);
                setFixPlan(null);
              }}
            />
          </label>
          <label>
            Artist{" "}
            <TextField.Root
              aria-label="Album artist"
              value={fixArtist}
              onChange={(e) => {
                setFixArtist(e.target.value);
                setFixPlan(null);
              }}
            />
          </label>
          <Button type="submit" disabled={fixPending || !album?.id}>
            Preview changes
          </Button>
        </form>
        <p className="rs-muted">
          Leave a field as it is to keep it. Clearing the year to 0 leaves the
          year untouched, so enter the correct year explicitly.
        </p>
        {fixMessage && <Alert kind="info">{fixMessage}</Alert>}
        {fixPlan && fixPlan.files.length > 0 && (
          <>
            <Table label="Planned metadata corrections">
              <thead>
                <tr>
                  <th scope="col">File</th>
                  <th scope="col">New destination</th>
                </tr>
              </thead>
              <tbody>
                {fixPlan.files.map((file) => (
                  <tr key={file.music_file_id}>
                    <td>
                      <span className="mus-path">{file.source_path}</span>
                    </td>
                    <td>
                      <span className="mus-path">{file.destination_path}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
            <Button
              className="rs-btn-danger"
              disabled={fixPending}
              onClick={() => void applyFixMetadata()}
            >
              <Pencil size={15} aria-hidden="true" /> Apply to{" "}
              {fixPlan.files.length} file
              {fixPlan.files.length === 1 ? "" : "s"}
            </Button>
          </>
        )}
      </Modal>
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
                      .join(" ") || ""}
                  </td>
                  <td>{formatBytes(release.size)}</td>
                  <td>{release.seeders ?? ""}</td>
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
                      ""
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
      <AlbumDownloadModal
        target={albumDownload}
        onClose={() => setAlbumDownload(null)}
        onJob={(job) => setAlbumJobID(job.id)}
        onStarted={() => details.reload()}
      />
    </>
  );
}
