import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  ArrowLeft,
  Disc3,
  ExternalLink,
  FolderInput,
  Plus,
  RefreshCw,
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
import {
  api,
  messageFor,
  type MusicAlbumDetails,
  type MusicAlbumPreferences,
  type MusicArtist,
  type MusicArtistDetails,
  type MusicArtistPage,
  type MusicImportJob,
  type MusicJob,
  type MusicRelease,
  type MusicReleaseSearch,
  type MusicSearchResults,
} from "../api";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
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

export function Music() {
  const artists = useResource<MusicArtistPage>("/library/music/artists");
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<MusicArtist[]>([]);
  const [searching, setSearching] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  async function search(e: FormEvent) {
    e.preventDefault();
    if (searching || !query.trim()) return;
    setSearching(true);
    setError("");
    try {
      const data = await api<MusicSearchResults>(
        `/metadata/music/artists?query=${encodeURIComponent(query.trim())}&page=1`,
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
      setQuery("");
      setResults([]);
      artists.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  return (
    <>
      <PageToolbar label="Music actions">
        <ToolbarSection>
          <ToolbarButton
            icon={FolderInput}
            label="Library Import"
            title="Scan an existing music folder and register files"
            to="/music/import"
          />
          <ToolbarButton
            icon={Plus}
            label="Add artist"
            onClick={() => setOpen(true)}
          />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={artists.loading}
            onClick={artists.reload}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="mus-body">
        <PageHeading
          title="Music"
          description="MusicBrainz artist and album catalog."
        />
        <Alert kind="info" title="Add existing music">
          Use <Link to="/music/import">Music Library Import</Link> to scan a
          folder below the media root. Add the artist and open each album once
          so its tracks are loaded before registering files. Then use{" "}
          <Link to="/music/organize">Organize</Link> to rename and tag
          registered files.
        </Alert>
        <RequestState
          loading={artists.loading}
          error={artists.error}
          retry={artists.reload}
        />
        {artists.data &&
          (artists.data.items.length === 0 ? (
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
          ) : (
            <Table label="Music artists">
              <thead>
                <tr>
                  <th scope="col">Artist</th>
                  <th scope="col">Type</th>
                  <th scope="col">Albums</th>
                  <th scope="col">Status</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {artists.data.items.map((artist) => (
                  <tr key={artist.id}>
                    <td>
                      <Link to={`/music/artists/${artist.id}`}>
                        {artist.name}
                      </Link>
                      {artist.disambiguation && (
                        <span className="rs-muted">
                          {" "}
                          · {artist.disambiguation}
                        </span>
                      )}
                    </td>
                    <td>
                      <ArtistType artist={artist} />
                    </td>
                    <td>{artist.album_count}</td>
                    <td>
                      <Label
                        kind={
                          artist.status === "available" ? "success" : "info"
                        }
                      >
                        {artist.status}
                      </Label>
                    </td>
                    <td className="is-actions">
                      <Link to={`/music/artists/${artist.id}`}>Details</Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          ))}
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
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            maxLength={200}
          />
          <Button type="submit" disabled={searching || !query.trim()}>
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

export function MusicArtistPage() {
  const { id } = useParams();
  const details = useResource<MusicArtistDetails>(
    id ? `/library/music/artists/${id}` : null,
  );
  const artist = details.data?.artist;
  return (
    <>
      <PageToolbar label="Music artist actions">
        <ToolbarSection>
          <ToolbarButton icon={ArrowLeft} label="Artists" to="/music" />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="mus-body">
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
            <Table label="Music albums">
              <thead>
                <tr>
                  <th scope="col">Album</th>
                  <th scope="col">Year</th>
                  <th scope="col">Type</th>
                  <th scope="col">Tracks</th>
                  <th scope="col">Status</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {details.data.albums.map((album) => (
                  <tr key={album.id}>
                    <td>
                      <div className="mus-album-cell">
                        {album.cover_url ? (
                          <img
                            className="mus-cover"
                            src={album.cover_url}
                            alt=""
                            loading="lazy"
                            onError={(e) => {
                              e.currentTarget.style.display = "none";
                            }}
                          />
                        ) : (
                          <span className="mus-cover mus-cover-empty">
                            <Disc3 size={18} aria-hidden="true" />
                          </span>
                        )}
                        <Link to={`/music/albums/${album.id}`}>
                          {album.title}
                        </Link>
                      </div>
                    </td>
                    <td>{album.year ?? "—"}</td>
                    <td>
                      {[album.primary_type, ...album.secondary_types]
                        .filter(Boolean)
                        .join(" · ") || "—"}
                    </td>
                    <td>{album.track_count || "—"}</td>
                    <td>
                      <Label
                        kind={album.status === "available" ? "success" : "info"}
                      >
                        {album.status}
                      </Label>
                    </td>
                    <td className="is-actions">
                      <Link to={`/music/albums/${album.id}`}>Tracks</Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          ))}
      </PageBody>
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
        />
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
              </tr>
            </thead>
            <tbody>
              {tracks.map((track) => (
                <tr key={`${track.disc}-${track.number}`}>
                  <td>
                    {multiDisc ? `${track.disc}.${track.number}` : track.number}
                  </td>
                  <td>{track.title}</td>
                  <td>{formatDuration(track.duration_ms)}</td>
                </tr>
              ))}
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
