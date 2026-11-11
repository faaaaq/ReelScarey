import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  ArrowLeft,
  Disc3,
  Download,
  ExternalLink,
  FolderInput,
  ListMusic,
  Play,
  Search,
} from "lucide-react";
import { Link, useParams } from "react-router-dom";
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
import {
  api,
  messageFor,
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
  type AlbumDownloadTarget,
} from "../musicDownload";
import "./Library.css";
import "./Music.css";

function formatDuration(ms: number) {
  if (ms <= 0) return "—";
  const total = Math.round(ms / 1000);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

function statusKind(status: string, monitored: boolean) {
  if (status === "available") return "success";
  return monitored ? "danger" : "disabled";
}

export function ArtistTile({ artist }: { artist: MusicArtist }) {
  const [failed, setFailed] = useState(false);
  const initial = artist.name.trim().charAt(0).toUpperCase() || "?";
  const label =
    artist.status === "available" ? "In library" : "No registered files";
  const imageURL = artistImageURL(artist.id) || artist.image_url;
  return (
    <li className="lib-card">
      <Link
        className="lib-card-art mus-card-art"
        to={`/music/artists/${artist.id}`}
        aria-label={`Open ${artist.name}`}
      >
        {imageURL && !failed ? (
          <img
            className="mus-tile-img"
            src={imageURL}
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

export function AlbumTile({
  album,
  onDownload,
  showArtist = false,
}: {
  album: MusicAlbum;
  onDownload?: (album: MusicAlbum) => void;
  showArtist?: boolean;
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
    <li className="lib-card">
      <Link
        className="lib-card-art mus-card-art"
        to={`/music/albums/${album.id}`}
        aria-label={`Open ${album.title}`}
      >
        {coverURL && !failed ? (
          <img
            className="mus-tile-img"
            src={coverURL}
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
          {showArtist && album.artist_name && <span>{album.artist_name}</span>}
          <span>{album.year ?? "Year unknown"}</span>
          <span>
            {album.track_count
              ? `${album.track_count} ${album.track_count === 1 ? "track" : "tracks"}`
              : "Tracks not loaded"}
          </span>
        </div>
        <span className="lib-profile">{type}</span>
        {onDownload && album.status !== "available" && (
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

export function LibraryTrackTable({ tracks }: { tracks: MusicArtistTrack[] }) {
  const player = useMusicPlayer();
  const playable = tracks
    .map((track) => artistPlayerTrack(track, track.artist_name ?? ""))
    .filter((track): track is PlayerTrack => track !== null);
  return (
    <Table label="Music tracks">
      <thead>
        <tr>
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
              <td>{track.artist_name || "—"}</td>
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
            <td>{track.album || "—"}</td>
            <td>{formatDuration(track.duration_ms)}</td>
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
        {coverURL && !failed ? (
          <img
            className="mus-tile-img"
            src={coverURL}
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
  const details = useResource<MusicArtistDetails>(
    id ? `/library/music/artists/${id}` : null,
  );
  const topTracks = useResource<MusicTopTrackPage>(
    id ? `/library/music/artists/${id}/top-tracks?limit=20` : null,
  );
  const discography = useResource<MusicDiscographyPage>(
    id ? `/library/music/artists/${id}/discography?limit=100` : null,
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
          {artist?.id ? (
            <img
              className="mus-heading-cover"
              src={artistImageURL(artist.id)}
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
          retry={details.reload}
        />
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
        {discography.error && (
          <Alert kind="warning" title="Full discography unavailable">
            {discography.error} Showing saved catalog albums instead.
          </Alert>
        )}
        {discography.data?.message && (
          <p className="rs-muted">{discography.data.message}</p>
        )}
        {discography.data ? (
          discography.data.items.length ? (
            <>
              <div className="mus-section-head">
                <div>
                  <h2>Albums</h2>
                  <p className="rs-muted rs-small">
                    Full {discography.data.source} discography. Albums already
                    in the library open normally; missing albums can be
                    downloaded.
                  </p>
                </div>
              </div>
              <ul className="lib-grid" aria-label="Music albums">
                {discography.data.items.map((album) => (
                  <DiscographyTile
                    key={album.provider_id}
                    album={album}
                    onDownload={(item) =>
                      setAlbumDownload({
                        albumID: item.album_id,
                        providerID: item.provider_id,
                        title: item.title,
                        artist: item.artist || artist?.name,
                      })
                    }
                  />
                ))}
              </ul>
            </>
          ) : (
            <EmptyState icon={Disc3} title="No albums">
              Deezer has no albums for this artist.
            </EmptyState>
          )
        ) : details.data ? (
          details.data.albums.length === 0 ? (
            <EmptyState icon={Disc3} title="No albums">
              MusicBrainz has no release groups for this artist.
            </EmptyState>
          ) : (
            <ul className="lib-grid" aria-label="Music albums">
              {details.data.albums.map((album) => (
                <AlbumTile
                  key={album.id}
                  album={album}
                  onDownload={(item) =>
                    setAlbumDownload({
                      albumID: item.id,
                      title: item.title,
                      artist: item.artist_name,
                    })
                  }
                />
              ))}
            </ul>
          )
        ) : null}

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
              <p className="rs-muted">Loading top tracks…</p>
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
        description="Search YouTube and import the matched audio through the reviewed music import pipeline."
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
              {downloadPending ? "Starting…" : "Download"}
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
              The worker searches YouTube for this track and imports only a
              close duration match. Nothing replaces an existing library file.
              This uses the same yt-dlp and FFmpeg tools as the rest of
              ReelScarey, not a Spotify account.
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
                  {topTrackState ? ` · ${topTrackState}` : ""}
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
  const albumCover = album ? albumCoverURL(album.id) || album.cover_url : "";
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
  const [prefsPending, setPrefsPending] = useState(false);
  const [albumDownload, setAlbumDownload] =
    useState<AlbumDownloadTarget | null>(null);
  const hasMissingTracks = tracks.some((track) => !track.file_id);

  useEffect(() => {
    if (!albumPrefs.data?.preferences) return;
    setAllowLossless(albumPrefs.data.preferences.allow_lossless);
    setMinBitrate(String(albumPrefs.data.preferences.min_bitrate));
    setFormats(albumPrefs.data.preferences.preferred_formats.join(","));
  }, [albumPrefs.data]);

  useEffect(() => {
    if (albumJob.data?.state === "completed") details.reload();
  }, [albumJob.data?.state]);

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
          retry={details.reload}
        />
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
      <AlbumDownloadModal
        target={albumDownload}
        onClose={() => setAlbumDownload(null)}
        onJob={(job) => setAlbumJobID(job.id)}
        onStarted={() => details.reload()}
      />
    </>
  );
}
