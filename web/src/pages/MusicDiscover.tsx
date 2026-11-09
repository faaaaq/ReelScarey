import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  ArrowLeft,
  Download,
  Music2,
  Play,
  RefreshCw,
  Search,
  Trash2,
} from "lucide-react";
import {
  Alert,
  EmptyState,
  FieldSet,
  Label,
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
  type MusicAcquisitionJob,
  type MusicAcquisitionJobList,
  type MusicLinkPreview,
  type MusicMonitorSettings,
  type MusicMonitorSource,
  type MusicMonitorSourceList,
  type MusicTopTrack,
  type MusicTopTrackPage,
} from "../api";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
import { useMusicPlayer, type PlayerTrack } from "../musicPlayer";
import { albumCoverURL } from "../musicArtwork";
import "./Music.css";

function formatDuration(ms: number) {
  if (ms <= 0) return "—";
  const total = Math.round(ms / 1000);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

function trackPlayer(track: MusicTopTrack): PlayerTrack | null {
  if (!track.file_id) return null;
  return {
    fileID: track.file_id,
    title: track.title,
    artist: track.artist,
    album: track.album,
    coverURL: albumCoverURL(track.album_id) || track.cover_url || undefined,
    durationMS: track.duration_ms,
    albumID: track.album_id ?? undefined,
  };
}

function ChartTable({
  tracks,
  onDownload,
}: {
  tracks: MusicTopTrack[];
  onDownload: (track: MusicTopTrack) => void;
}) {
  const player = useMusicPlayer();
  const playable = tracks
    .map(trackPlayer)
    .filter((track): track is PlayerTrack => track !== null);
  return (
    <Table label="Music charts">
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
            <span className="sr-only">Actions</span>
          </th>
        </tr>
      </thead>
      <tbody>
        {tracks.map((track, index) => (
          <tr key={`${track.provider_id}-${index}`}>
            <td>{index + 1}</td>
            <td>
              {track.title}{" "}
              {track.in_library && <Label kind="success">In library</Label>}
            </td>
            <td>{track.artist || "—"}</td>
            <td>{track.album || "—"}</td>
            <td>{formatDuration(track.duration_ms)}</td>
            <td className="is-actions">
              {track.file_id ? (
                <Button
                  size="1"
                  variant="ghost"
                  color="gray"
                  aria-label={`Play ${track.title}`}
                  onClick={() => {
                    const item = trackPlayer(track);
                    if (item) player.play(item, playable);
                  }}
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

export function MusicDiscover() {
  const charts = useResource<MusicTopTrackPage>(
    "/library/music/charts?limit=20",
  );
  const monitor = useResource<MusicMonitorSettings>("/library/music/monitor");
  const sources = useResource<MusicMonitorSourceList>(
    "/library/music/monitor/sources",
  );
  const jobs = useLiveResource<MusicAcquisitionJobList>(
    "/library/music/acquisitions?page=1",
    5000,
  );
  const [link, setLink] = useState("");
  const [preview, setPreview] = useState<MusicLinkPreview | null>(null);
  const [selected, setSelected] = useState<number[]>([]);
  const [linkRoot, setLinkRoot] = useState("Music");
  const [linkFormat, setLinkFormat] = useState("source");
  const [previewing, setPreviewing] = useState(false);
  const [linkPending, setLinkPending] = useState(false);
  const [linkError, setLinkError] = useState("");
  const [jobID, setJobID] = useState<number | null>(null);
  const [monitorPending, setMonitorPending] = useState(false);
  const [monitorError, setMonitorError] = useState("");
  const [monitorMessage, setMonitorMessage] = useState("");
  const [intervalHours, setIntervalHours] = useState("24");
  const [sourceURL, setSourceURL] = useState("");
  const [sourcePending, setSourcePending] = useState(false);
  const [sourceError, setSourceError] = useState("");
  const [chartDownloadID, setChartDownloadID] = useState<number | null>(null);
  const [chartError, setChartError] = useState("");
  const job = useLiveResource<MusicAcquisitionJob>(
    jobID ? `/library/music/acquisitions/${jobID}` : null,
    2000,
  );
  const chartJob = useLiveResource<MusicAcquisitionJob>(
    chartDownloadID ? `/library/music/acquisitions/${chartDownloadID}` : null,
    2000,
  );

  useEffect(() => {
    if (monitor.data) setIntervalHours(String(monitor.data.interval_hours));
  }, [monitor.data]);

  async function previewLink(e: FormEvent) {
    e.preventDefault();
    if (previewing || !link.trim()) return;
    setPreviewing(true);
    setLinkError("");
    setPreview(null);
    try {
      const result = await api<MusicLinkPreview>(
        "/library/music/links/preview",
        {
          method: "POST",
          body: JSON.stringify({ url: link.trim() }),
        },
      );
      setPreview(result);
      setSelected(result.tracks.map((_, index) => index));
    } catch (e) {
      setLinkError(messageFor(e));
    } finally {
      setPreviewing(false);
    }
  }

  async function startLinkDownload() {
    if (!preview || selected.length === 0 || linkPending) return;
    setLinkPending(true);
    setLinkError("");
    try {
      const result = await api<MusicAcquisitionJob>("/library/music/links", {
        method: "POST",
        body: JSON.stringify({
          url: link.trim(),
          track_indices: selected,
          root: linkRoot.trim() || "Music",
          format: linkFormat,
          reviewed: true,
        }),
      });
      setJobID(result.id);
      jobs.reload();
    } catch (e) {
      setLinkError(messageFor(e));
    } finally {
      setLinkPending(false);
    }
  }

  async function downloadChartTrack(track: MusicTopTrack) {
    setChartError("");
    try {
      const result = await api<MusicAcquisitionJob>(
        "/library/music/acquisitions",
        {
          method: "POST",
          body: JSON.stringify({
            source_kind: "chart",
            title: track.title,
            root: "Music",
            format: "source",
            reviewed: true,
            tracks: [
              {
                title: track.title,
                artist: track.artist,
                album: track.album || track.title,
                album_artist: track.artist,
                year: null,
                track_number: 1,
                disc_number: 1,
                duration_ms: track.duration_ms,
                cover_url: track.cover_url,
                source_url: "",
                search_query: `${track.artist} ${track.title}`.trim(),
              },
            ],
          }),
        },
      );
      setChartDownloadID(result.id);
      jobs.reload();
    } catch (e) {
      setChartError(messageFor(e));
    }
  }

  async function saveMonitor(enabled: boolean) {
    if (!monitor.data || monitorPending) return;
    setMonitorPending(true);
    setMonitorError("");
    setMonitorMessage("");
    try {
      await api("/library/music/monitor", {
        method: "PUT",
        body: JSON.stringify({
          enabled,
          interval_hours: Number(intervalHours) || 24,
          revision: monitor.data.revision,
        }),
      });
      setMonitorMessage(
        enabled
          ? "Playlist monitoring is enabled. It uses the existing server; no extra port is opened."
          : "Playlist monitoring is paused.",
      );
      monitor.reload();
    } catch (e) {
      setMonitorError(messageFor(e));
    } finally {
      setMonitorPending(false);
    }
  }

  async function runMonitor() {
    if (monitorPending) return;
    setMonitorPending(true);
    setMonitorError("");
    setMonitorMessage("");
    try {
      await api("/library/music/monitor/run", { method: "POST" });
      setMonitorMessage(
        "Monitor run requested. New tracks queue through the acquisition worker.",
      );
      monitor.reload();
    } catch (e) {
      setMonitorError(messageFor(e));
    } finally {
      setMonitorPending(false);
    }
  }

  async function addSource(e: FormEvent) {
    e.preventDefault();
    if (sourcePending || !sourceURL.trim()) return;
    setSourcePending(true);
    setSourceError("");
    try {
      await api<MusicMonitorSource>("/library/music/monitor/sources", {
        method: "POST",
        body: JSON.stringify({ url: sourceURL.trim() }),
      });
      setSourceURL("");
      sources.reload();
    } catch (e) {
      setSourceError(messageFor(e));
    } finally {
      setSourcePending(false);
    }
  }

  async function toggleSource(source: MusicMonitorSource) {
    try {
      await api(`/library/music/monitor/sources/${source.id}`, {
        method: "PUT",
        body: JSON.stringify({ enabled: !source.enabled }),
      });
      sources.reload();
    } catch (e) {
      setMonitorError(messageFor(e));
    }
  }

  async function removeSource(source: MusicMonitorSource) {
    try {
      await api(`/library/music/monitor/sources/${source.id}`, {
        method: "DELETE",
      });
      sources.reload();
    } catch (e) {
      setMonitorError(messageFor(e));
    }
  }

  return (
    <>
      <PageToolbar label="Music discover actions">
        <ToolbarSection>
          <ToolbarButton icon={ArrowLeft} label="Music" to="/music" />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={charts.loading || monitor.loading}
            onClick={() => {
              charts.reload();
              monitor.reload();
              sources.reload();
              jobs.reload();
            }}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="mus-body">
        <PageHeading
          title="Music discover"
          description="Download from links, browse Deezer charts, and monitor playlists. All downloads use reviewed matching and the existing music import pipeline."
        />
        <FieldSet legend="Add from link">
          <form onSubmit={(e) => void previewLink(e)} className="mus-search">
            <TextField.Root
              aria-label="Music link"
              placeholder="Spotify, Deezer, YouTube or YouTube Music link, or search text"
              value={link}
              onChange={(event) => setLink(event.target.value)}
              maxLength={2048}
            />
            <Button type="submit" disabled={previewing || !link.trim()}>
              <Search size={15} aria-hidden="true" />
              {previewing ? "Reading…" : "Preview"}
            </Button>
          </form>
          <Alert kind="info" title="Public links and search">
            Spotify links are read from their public embed page. Deezer uses its
            public API. YouTube and YouTube Music use yt-dlp. Free text uses a
            YouTube search preview. No Spotify account or API key is required.
            Nothing is downloaded until you review the track list and confirm.
          </Alert>
          {linkError && <Alert kind="danger">{linkError}</Alert>}
          {preview && (
            <>
              <div className="mus-scan-summary">
                <Label kind="info">{preview.source}</Label>
                <strong>{preview.title}</strong>
                {preview.subtitle && (
                  <span className="rs-muted">{preview.subtitle}</span>
                )}
                <Button
                  size="1"
                  variant="soft"
                  onClick={() =>
                    setSelected(
                      selected.length === preview.tracks.length
                        ? []
                        : preview.tracks.map((_, index) => index),
                    )
                  }
                >
                  {selected.length === preview.tracks.length
                    ? "Clear selection"
                    : "Select all"}
                </Button>
              </div>
              <Table label="Link tracks">
                <thead>
                  <tr>
                    <th scope="col">
                      <span className="sr-only">Select</span>
                    </th>
                    <th scope="col">Title</th>
                    <th scope="col">Artist</th>
                    <th scope="col">Album</th>
                    <th scope="col">Duration</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.tracks.map((track, index) => (
                    <tr key={`${index}-${track.title}`}>
                      <td>
                        <input
                          type="checkbox"
                          aria-label={`Select ${track.title}`}
                          checked={selected.includes(index)}
                          onChange={(event) =>
                            setSelected(
                              event.target.checked
                                ? [...selected, index]
                                : selected.filter((value) => value !== index),
                            )
                          }
                        />
                      </td>
                      <td>{track.title}</td>
                      <td>{track.artist || "—"}</td>
                      <td>{track.album || "—"}</td>
                      <td>{formatDuration(track.duration_ms)}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
              <div className="mus-search">
                <label>
                  Format{" "}
                  <select
                    aria-label="Link download format"
                    value={linkFormat}
                    onChange={(event) => setLinkFormat(event.target.value)}
                  >
                    <option value="source">Best available source</option>
                    <option value="mp3">MP3</option>
                    <option value="flac">FLAC</option>
                    <option value="m4a">M4A</option>
                    <option value="opus">Opus</option>
                  </select>
                </label>
                <TextField.Root
                  aria-label="Link destination root"
                  value={linkRoot}
                  onChange={(event) => setLinkRoot(event.target.value)}
                />
                <Button
                  disabled={selected.length === 0 || linkPending}
                  onClick={() => void startLinkDownload()}
                >
                  <Download size={15} aria-hidden="true" />
                  Download {selected.length} selected
                </Button>
              </div>
            </>
          )}
          {job.data && (
            <Alert
              kind={
                job.data.state === "completed"
                  ? "success"
                  : job.data.state === "failed"
                    ? "danger"
                    : "info"
              }
              title={`Acquisition #${job.data.id}`}
            >
              {job.data.state} · {job.data.completed_count}/
              {job.data.track_count} completed
              {job.data.error ? ` · ${job.data.error}` : ""}
            </Alert>
          )}
        </FieldSet>

        <FieldSet legend="Deezer charts">
          <RequestState
            loading={charts.loading}
            error={charts.error}
            retry={charts.reload}
          />
          {charts.data?.message && (
            <p className="rs-muted">{charts.data.message}</p>
          )}
          {charts.data && charts.data.items.length > 0 && (
            <ChartTable
              tracks={charts.data.items}
              onDownload={(track) => void downloadChartTrack(track)}
            />
          )}
          {chartError && <Alert kind="danger">{chartError}</Alert>}
          {chartJob.data && (
            <Alert
              kind={
                chartJob.data.state === "completed"
                  ? "success"
                  : chartJob.data.state === "failed"
                    ? "danger"
                    : "info"
              }
              title={`Chart download #${chartJob.data.id}`}
            >
              {chartJob.data.state} · {chartJob.data.completed_count}/
              {chartJob.data.track_count} completed
            </Alert>
          )}
        </FieldSet>

        <FieldSet legend="Playlist monitor">
          <RequestState
            loading={monitor.loading}
            error={monitor.error}
            retry={monitor.reload}
          />
          {monitor.data && (
            <>
              <Alert kind="info" title="Opt-in schedule">
                Monitoring starts paused. Enabling it uses the existing
                ReelScarey server and port; no additional port is opened. New
                tracks are queued through the same reviewed acquisition and
                import worker.
              </Alert>
              <div className="mus-scan-summary">
                <label className="rs-check">
                  <input
                    type="checkbox"
                    checked={monitor.data.enabled}
                    disabled={monitorPending}
                    onChange={(event) => void saveMonitor(event.target.checked)}
                  />
                  <span>
                    <strong>Enable playlist monitor</strong>
                  </span>
                </label>
                <label>
                  Interval hours{" "}
                  <TextField.Root
                    aria-label="Monitor interval hours"
                    value={intervalHours}
                    onChange={(event) => setIntervalHours(event.target.value)}
                    style={{ width: 80 }}
                  />
                </label>
                <Button
                  variant="soft"
                  disabled={monitorPending}
                  onClick={() => void saveMonitor(monitor.data!.enabled)}
                >
                  Save interval
                </Button>
                <Button
                  variant="soft"
                  disabled={monitorPending || !monitor.data.enabled}
                  title={
                    monitor.data.enabled
                      ? undefined
                      : "Enable the monitor before running it."
                  }
                  onClick={() => void runMonitor()}
                >
                  Run now
                </Button>
              </div>
              {monitor.data.last_error && (
                <Alert kind="warning">{monitor.data.last_error}</Alert>
              )}
              {monitorMessage && <Alert kind="success">{monitorMessage}</Alert>}
              {monitorError && <Alert kind="danger">{monitorError}</Alert>}
              <form onSubmit={(e) => void addSource(e)} className="mus-search">
                <TextField.Root
                  aria-label="Playlist or artist link"
                  placeholder="Spotify, Deezer or YouTube Music playlist/artist link"
                  value={sourceURL}
                  onChange={(event) => setSourceURL(event.target.value)}
                  maxLength={2048}
                />
                <Button
                  type="submit"
                  disabled={sourcePending || !sourceURL.trim()}
                >
                  Add monitor source
                </Button>
              </form>
              {sourceError && <Alert kind="danger">{sourceError}</Alert>}
              <RequestState
                loading={sources.loading}
                error={sources.error}
                retry={sources.reload}
              />
              {sources.data && sources.data.items.length === 0 && (
                <EmptyState icon={Music2} title="No monitor sources">
                  Add a public playlist or artist link above. It remains paused
                  until you enable the monitor.
                </EmptyState>
              )}
              {sources.data && sources.data.items.length > 0 && (
                <Table label="Monitor sources">
                  <thead>
                    <tr>
                      <th scope="col">Source</th>
                      <th scope="col">Provider</th>
                      <th scope="col">State</th>
                      <th scope="col">Last checked</th>
                      <th scope="col" className="is-actions">
                        <span className="sr-only">Actions</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {sources.data.items.map((source) => (
                      <tr key={source.id}>
                        <td>
                          <a href={source.url} target="_blank" rel="noreferrer">
                            {source.title || source.url}
                          </a>
                          {source.last_error && (
                            <span className="rs-muted">
                              {" "}
                              · {source.last_error}
                            </span>
                          )}
                        </td>
                        <td>{source.provider || "—"}</td>
                        <td>
                          <Label kind={source.enabled ? "success" : "disabled"}>
                            {source.enabled ? "Watching" : "Paused"}
                          </Label>
                        </td>
                        <td>
                          {source.last_checked
                            ? new Date(
                                source.last_checked * 1000,
                              ).toLocaleString()
                            : "Never"}
                        </td>
                        <td className="is-actions">
                          <Button
                            size="1"
                            variant="soft"
                            onClick={() => void toggleSource(source)}
                          >
                            {source.enabled ? "Pause" : "Watch"}
                          </Button>{" "}
                          <Button
                            size="1"
                            variant="ghost"
                            color="gray"
                            aria-label={`Remove ${source.title || source.url}`}
                            onClick={() => void removeSource(source)}
                          >
                            <Trash2 size={14} aria-hidden="true" />
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </>
          )}
        </FieldSet>

        <FieldSet legend="Recent downloads">
          <RequestState
            loading={jobs.loading}
            error={jobs.error}
            retry={jobs.reload}
          />
          {jobs.data && jobs.data.items.length === 0 && (
            <p className="rs-muted">No link or chart downloads yet.</p>
          )}
          {jobs.data && jobs.data.items.length > 0 && (
            <Table label="Recent music downloads">
              <thead>
                <tr>
                  <th scope="col">ID</th>
                  <th scope="col">Title</th>
                  <th scope="col">State</th>
                  <th scope="col">Progress</th>
                  <th scope="col">Error</th>
                </tr>
              </thead>
              <tbody>
                {jobs.data.items.map((item) => (
                  <tr key={item.id}>
                    <td>{item.id}</td>
                    <td>{item.title || item.source_url || "—"}</td>
                    <td>
                      <Label
                        kind={
                          item.state === "completed"
                            ? "success"
                            : item.state === "failed"
                              ? "danger"
                              : "info"
                        }
                      >
                        {item.state}
                      </Label>
                    </td>
                    <td>
                      {item.completed_count}/{item.track_count}
                    </td>
                    <td>{item.error || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </FieldSet>
      </PageBody>
    </>
  );
}
