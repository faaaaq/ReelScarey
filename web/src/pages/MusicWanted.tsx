import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { ListMusic, Play, RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
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
  type MusicDownloadFileList,
  type MusicJob,
  type MusicJobPage,
  type MusicPreferences,
  type MusicSchedule,
  type MusicUpgradeList,
  type MusicWantedPage,
} from "../api";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
import "./Music.css";

function jobKind(state: string) {
  if (state === "completed") return "success" as const;
  if (state === "failed" || state === "blocked") return "danger" as const;
  if (state === "cancelled") return "disabled" as const;
  return "info" as const;
}

export function MusicWanted() {
  const wanted = useLiveResource<MusicWantedPage>(
    "/library/music/wanted",
    15000,
  );
  const prefs = useResource<MusicPreferences>("/library/music/preferences");
  const schedule = useResource<MusicSchedule>("/library/music/schedule");
  const jobs = useLiveResource<MusicJobPage>("/library/music/downloads", 5000);
  const upgrades = useResource<MusicUpgradeList>("/library/music/upgrades");
  const [allowLossless, setAllowLossless] = useState(true);
  const [minBitrate, setMinBitrate] = useState("0");
  const [formats, setFormats] = useState("FLAC,ALAC,MP3");
  const [enabled, setEnabled] = useState(false);
  const [intervalHours, setIntervalHours] = useState("24");
  const [mode, setMode] = useState<"search" | "download">("search");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [mappingJob, setMappingJob] = useState<MusicJob | null>(null);
  const [mappingFiles, setMappingFiles] =
    useState<MusicDownloadFileList | null>(null);
  const [mappingAlbum, setMappingAlbum] = useState<MusicAlbumDetails | null>(
    null,
  );
  const [mapping, setMapping] = useState<Record<number, number>>({});
  const [mappingPending, setMappingPending] = useState(false);
  const [mappingError, setMappingError] = useState("");

  useEffect(() => {
    if (!prefs.data) return;
    setAllowLossless(prefs.data.allow_lossless);
    setMinBitrate(String(prefs.data.min_bitrate));
    setFormats(prefs.data.preferred_formats.join(","));
  }, [prefs.data]);

  useEffect(() => {
    if (!schedule.data) return;
    setEnabled(schedule.data.enabled);
    setIntervalHours(String(schedule.data.interval_hours));
    setMode(schedule.data.mode);
  }, [schedule.data]);

  async function savePreferences(e: FormEvent) {
    e.preventDefault();
    if (!prefs.data || pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      await api("/library/music/preferences", {
        method: "PUT",
        body: JSON.stringify({
          allow_lossless: allowLossless,
          min_bitrate: Number(minBitrate) || 0,
          preferred_formats: formats
            .split(",")
            .map((value) => value.trim())
            .filter(Boolean),
          revision: prefs.data.revision,
        }),
      });
      prefs.reload();
      setMessage("Download preferences saved.");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function saveSchedule(e: FormEvent) {
    e.preventDefault();
    if (!schedule.data || pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      await api("/library/music/schedule", {
        method: "PUT",
        body: JSON.stringify({
          enabled,
          interval_hours: Number(intervalHours) || 24,
          mode,
          revision: schedule.data.revision,
        }),
      });
      schedule.reload();
      setMessage("Music schedule saved.");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function runNow() {
    if (pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      await api("/library/music/schedule/run", {
        method: "POST",
        body: "{}",
      });
      schedule.reload();
      jobs.reload();
      setMessage("Music search run started.");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function jobAction(id: string, action: "cancel" | "retry") {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/library/music/downloads/${id}/${action}`, {
        method: "POST",
        body: "{}",
      });
      jobs.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function openMapping(job: MusicJob) {
    setMappingJob(job);
    setMappingFiles(null);
    setMappingAlbum(null);
    setMapping({});
    setMappingError("");
    try {
      const [files, album] = await Promise.all([
        api<MusicDownloadFileList>(`/library/music/downloads/${job.id}/files`),
        api<MusicAlbumDetails>(`/library/music/albums/${job.album_id}`),
      ]);
      setMappingFiles(files);
      setMappingAlbum(album);
      const defaults: Record<number, number> = {};
      for (const file of files.items) {
        const match = album.tracks.find(
          (track) =>
            (file.disc !== null &&
              file.track !== null &&
              track.disc === file.disc &&
              track.number === file.track) ||
            (file.title !== "" &&
              track.title.toLowerCase() === file.title.toLowerCase()),
        );
        if (match?.id) defaults[file.index] = match.id;
      }
      setMapping(defaults);
    } catch (e) {
      setMappingError(messageFor(e));
    }
  }

  async function saveMapping() {
    if (!mappingJob || !mappingFiles || mappingPending) return;
    if (mappingFiles.items.some((file) => !mapping[file.index])) {
      setMappingError("Map every downloaded file to a catalog track.");
      return;
    }
    setMappingPending(true);
    setMappingError("");
    try {
      await api(`/library/music/downloads/${mappingJob.id}/mapping`, {
        method: "POST",
        body: JSON.stringify({
          mappings: mappingFiles.items.map((file) => ({
            file_index: file.index,
            track_id: mapping[file.index],
          })),
        }),
      });
      setMappingJob(null);
      jobs.reload();
    } catch (e) {
      setMappingError(messageFor(e));
    } finally {
      setMappingPending(false);
    }
  }

  return (
    <>
      <PageToolbar label="Music wanted actions">
        <ToolbarSection>
          <ToolbarButton
            icon={Play}
            label="Run now"
            disabled={pending}
            onClick={() => void runNow()}
          />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={wanted.loading}
            onClick={() => {
              wanted.reload();
              jobs.reload();
            }}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="mus-body">
        <PageHeading
          title="Music wanted"
          description="Monitored albums without registered files, download preferences and the wanted-album search schedule."
        />
        <Alert kind="info" title="How the run works">
          A run searches the configured indexers for each due album and applies
          the download preference. In download mode it queues the best matching
          release through the selected client; search mode only stores the
          results for review.
        </Alert>
        {error && <Alert kind="danger">{error}</Alert>}
        {message && <Alert kind="success">{message}</Alert>}
        <FieldSet legend="Download preferences">
          <RequestState
            loading={prefs.loading}
            error={prefs.error}
            retry={prefs.reload}
          />
          {prefs.data && (
            <form
              className="mus-search"
              onSubmit={(e) => void savePreferences(e)}
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
                  aria-label="Minimum bitrate"
                  value={minBitrate}
                  onChange={(e) => setMinBitrate(e.target.value)}
                  style={{ width: 90 }}
                />
              </label>
              <label>
                Formats{" "}
                <TextField.Root
                  aria-label="Preferred formats"
                  value={formats}
                  onChange={(e) => setFormats(e.target.value)}
                />
              </label>
              <Button type="submit" disabled={pending}>
                Save
              </Button>
            </form>
          )}
        </FieldSet>
        <FieldSet legend="Search schedule">
          <RequestState
            loading={schedule.loading}
            error={schedule.error}
            retry={schedule.reload}
          />
          {schedule.data && (
            <form className="mus-search" onSubmit={(e) => void saveSchedule(e)}>
              <label>
                <input
                  type="checkbox"
                  checked={enabled}
                  onChange={(e) => setEnabled(e.target.checked)}
                />{" "}
                Enabled
              </label>
              <label>
                Every{" "}
                <TextField.Root
                  aria-label="Interval hours"
                  value={intervalHours}
                  onChange={(e) => setIntervalHours(e.target.value)}
                  style={{ width: 80 }}
                />
                hours
              </label>
              <label>
                Mode{" "}
                <select
                  aria-label="Schedule mode"
                  value={mode}
                  onChange={(e) =>
                    setMode(e.target.value as "search" | "download")
                  }
                >
                  <option value="search">Search only</option>
                  <option value="download">Download best match</option>
                </select>
              </label>
              <Button type="submit" disabled={pending}>
                Save
              </Button>
            </form>
          )}
          {schedule.data && (
            <div className="mus-scan-summary">
              <span>
                Last run:{" "}
                {schedule.data.last_run
                  ? new Date(schedule.data.last_run * 1000).toLocaleString()
                  : "never"}
              </span>
              <span>
                Next run:{" "}
                {schedule.data.next_run
                  ? new Date(schedule.data.next_run * 1000).toLocaleString()
                  : "paused"}
              </span>
              {schedule.data.last_error && (
                <span className="rs-muted">{schedule.data.last_error}</span>
              )}
            </div>
          )}
        </FieldSet>
        <RequestState
          loading={wanted.loading}
          error={wanted.error}
          retry={wanted.reload}
        />
        {wanted.data && wanted.data.items.length === 0 && (
          <EmptyState icon={ListMusic} title="Nothing wanted">
            Every monitored album has at least one registered file.
          </EmptyState>
        )}
        {wanted.data && wanted.data.items.length > 0 && (
          <FieldSet legend="Wanted albums">
            <Table label="Wanted music albums">
              <thead>
                <tr>
                  <th scope="col">Artist</th>
                  <th scope="col">Album</th>
                  <th scope="col">Year</th>
                  <th scope="col">Tracks</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {wanted.data.items.map((album) => (
                  <tr key={album.id}>
                    <td>{album.artist_name}</td>
                    <td>{album.title}</td>
                    <td>{album.year ?? "—"}</td>
                    <td>{album.track_count || "—"}</td>
                    <td className="is-actions">
                      <Link to={`/music/albums/${album.id}`}>Find release</Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </FieldSet>
        )}
        {upgrades.data?.items && upgrades.data.items.length > 0 && (
          <FieldSet legend="Upgrade suggestions">
            <Table label="Music upgrade suggestions">
              <thead>
                <tr>
                  <th scope="col">Artist</th>
                  <th scope="col">Album</th>
                  <th scope="col">Formats</th>
                  <th scope="col">Bitrate</th>
                  <th scope="col">Reason</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {upgrades.data.items.map((item) => (
                  <tr key={item.album_id}>
                    <td>{item.artist_name}</td>
                    <td>{item.title}</td>
                    <td>{item.formats.join(", ") || "—"}</td>
                    <td>{item.bitrate || "—"}</td>
                    <td>{item.reason}</td>
                    <td className="is-actions">
                      <Link to={`/music/albums/${item.album_id}`}>
                        Find release
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </FieldSet>
        )}
        {jobs.data && jobs.data.items.length > 0 && (
          <FieldSet legend="Recent downloads">
            <Table label="Recent music downloads">
              <thead>
                <tr>
                  <th scope="col">Release</th>
                  <th scope="col">State</th>
                  <th scope="col">Progress</th>
                  <th scope="col">Error</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {jobs.data.items.map((job) => (
                  <tr key={job.id}>
                    <td>{job.title}</td>
                    <td>
                      <Label kind={jobKind(job.state)}>{job.state}</Label>
                    </td>
                    <td>{Math.round(job.progress * 100)}%</td>
                    <td>{job.error || "—"}</td>
                    <td className="is-actions">
                      {[
                        "queued",
                        "submitting",
                        "downloading",
                        "importing",
                        "blocked",
                        "failed",
                      ].includes(job.state) && (
                        <Button
                          size="1"
                          variant="soft"
                          disabled={pending}
                          onClick={() => void jobAction(job.id, "cancel")}
                        >
                          Stop tracking
                        </Button>
                      )}
                      {job.state === "blocked" && (
                        <Button
                          size="1"
                          variant="soft"
                          disabled={pending}
                          onClick={() => void openMapping(job)}
                        >
                          Map files
                        </Button>
                      )}
                      {["blocked", "failed"].includes(job.state) && (
                        <Button
                          size="1"
                          variant="soft"
                          disabled={pending}
                          onClick={() => void jobAction(job.id, "retry")}
                        >
                          Retry
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </FieldSet>
        )}
      </PageBody>
      <Modal
        open={mappingJob !== null}
        onOpenChange={(open) => {
          if (!open) setMappingJob(null);
        }}
        title="Map downloaded files"
        description="Assign each completed file to one catalog track."
        size="large"
        footer={
          <Button
            variant="soft"
            color="gray"
            onClick={() => setMappingJob(null)}
          >
            Close
          </Button>
        }
      >
        {mappingError && <Alert kind="danger">{mappingError}</Alert>}
        {mappingFiles && mappingAlbum ? (
          <Table label="Downloaded files">
            <thead>
              <tr>
                <th scope="col">File</th>
                <th scope="col">Tags</th>
                <th scope="col">Track</th>
              </tr>
            </thead>
            <tbody>
              {mappingFiles.items.map((file) => (
                <tr key={file.index}>
                  <td>
                    <span className="mus-path">{file.name}</span>
                  </td>
                  <td>
                    {[file.artist, file.album, file.title]
                      .filter(Boolean)
                      .join(" · ") || "—"}
                  </td>
                  <td>
                    <select
                      aria-label={`Track for ${file.name}`}
                      value={mapping[file.index] ?? ""}
                      onChange={(e) =>
                        setMapping({
                          ...mapping,
                          [file.index]: Number(e.target.value),
                        })
                      }
                    >
                      <option value="">Choose…</option>
                      {mappingAlbum.tracks.map((track) => (
                        <option key={track.id} value={track.id}>
                          {track.disc > 1 ? `${track.disc}.` : ""}
                          {track.number} {track.title}
                        </option>
                      ))}
                    </select>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : (
          !mappingError && <p className="rs-muted">Loading completed files…</p>
        )}
        <div className="mus-scan-actions">
          <Button
            disabled={mappingPending || !mappingFiles}
            onClick={() => void saveMapping()}
          >
            Import mapped files
          </Button>
        </div>
      </Modal>
    </>
  );
}
