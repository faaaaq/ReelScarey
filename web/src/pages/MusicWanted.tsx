import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { ListMusic, Play, RefreshCw, SlidersHorizontal } from "lucide-react";
import { Link } from "react-router-dom";
import {
  Alert,
  Check,
  EmptyState,
  FieldSet,
  FormActions,
  Label,
  Modal,
  PageBody,
  PageToolbar,
  ProgressBar,
  Table,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { PageHeading, RequestState } from "../components";
import {
  APIError,
  api,
  messageFor,
  type ConversionJobPage,
  type MusicAlbumDetails,
  type MusicAlbumPage,
  type MusicArtistPage,
  type MusicArtistTrackPage,
  type MusicDownloadFileList,
  type MusicJob,
  type MusicJobPage,
  type MusicPreferences,
  type MusicSchedule,
  type MusicSourceUpgradeJobPage,
  type MusicSourceUpgradeScope,
  type MusicSourceUpgradeTarget,
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

const sourceUpgradeTargetLabels: Record<MusicSourceUpgradeTarget, string> = {
  high: "High quality",
  lossless: "Lossless",
  hires: "Hi-Res (lossless, above 48 kHz)",
};

const sourceUpgradeScopeLabels: Record<MusicSourceUpgradeScope, string> = {
  library: "Entire library",
  artist: "Artist",
  album: "Album",
  track: "Track",
};

type SourceUpgradeSelection = { id: number; label: string };
type SourceUpgradePickerItem = SourceUpgradeSelection & { detail: string };

function SourceUpgradePager({
  label,
  page,
  pages,
  onPage,
}: {
  label: string;
  page: number;
  pages: number;
  onPage: (page: number) => void;
}) {
  if (pages <= 1) return null;
  return (
    <nav className="rs-pagination" aria-label={`${label} pages`}>
      <Button
        size="1"
        variant="soft"
        color="gray"
        aria-label={`Previous ${label} page`}
        disabled={page <= 1}
        onClick={() => onPage(Math.max(1, page - 1))}
      >
        Previous
      </Button>
      <span aria-live="polite">
        Page {page} of {pages}
      </span>
      <Button
        size="1"
        variant="soft"
        color="gray"
        aria-label={`Next ${label} page`}
        disabled={page >= pages}
        onClick={() => onPage(Math.min(pages, page + 1))}
      >
        Next
      </Button>
    </nav>
  );
}

function sourceUpgradeTime(value: number) {
  if (!value) return "-";
  const date = new Date(value * 1000);
  return Number.isNaN(date.getTime()) ? "-" : date.toLocaleString();
}

export function MusicWanted() {
  const [upgradeScope, setUpgradeScope] =
    useState<MusicSourceUpgradeScope>("library");
  const [upgradeTarget, setUpgradeTarget] =
    useState<MusicSourceUpgradeTarget>("lossless");
  const [upgradeMaxBitrate, setUpgradeMaxBitrate] = useState("0");
  const [upgradeSelection, setUpgradeSelection] =
    useState<SourceUpgradeSelection | null>(null);
  const [libraryUpgradeConfirmed, setLibraryUpgradeConfirmed] = useState(false);
  const [catalogInput, setCatalogInput] = useState("");
  const [catalogQuery, setCatalogQuery] = useState("");
  const [catalogPage, setCatalogPage] = useState(1);
  const [upgradePending, setUpgradePending] = useState(false);
  const [upgradeError, setUpgradeError] = useState("");
  const [upgradeNotice, setUpgradeNotice] = useState("");
  const [upgradeActionPending, setUpgradeActionPending] = useState<
    number | null
  >(null);
  const [upgradeActionError, setUpgradeActionError] = useState("");
  const [upgradeQueuePage, setUpgradeQueuePage] = useState(1);
  const wanted = useLiveResource<MusicWantedPage>(
    "/library/music/wanted",
    15000,
  );
  const prefs = useResource<MusicPreferences>("/library/music/preferences");
  const schedule = useResource<MusicSchedule>("/library/music/schedule");
  const jobs = useLiveResource<MusicJobPage>("/library/music/downloads", 5000);
  const upgrades = useResource<MusicUpgradeList>("/library/music/upgrades");
  const conversions = useLiveResource<ConversionJobPage>(
    "/library/music/conversions?page=1",
    10000,
  );
  const upgradeJobs = useLiveResource<MusicSourceUpgradeJobPage>(
    `/library/music/upgrade-jobs?page=${upgradeQueuePage}`,
    5000,
  );
  const upgradeCatalogParams = new URLSearchParams({
    page: String(catalogPage),
    limit: "12",
  });
  if (catalogQuery) upgradeCatalogParams.set("query", catalogQuery);
  const artistUpgradeCatalog = useResource<MusicArtistPage>(
    upgradeScope === "artist"
      ? `/library/music/artists?${upgradeCatalogParams.toString()}`
      : null,
  );
  const albumUpgradeCatalog = useResource<MusicAlbumPage>(
    upgradeScope === "album"
      ? `/library/music/albums?${upgradeCatalogParams.toString()}`
      : null,
  );
  const trackUpgradeCatalog = useResource<MusicArtistTrackPage>(
    upgradeScope === "track"
      ? `/library/music/tracks?${upgradeCatalogParams.toString()}`
      : null,
  );
  // Same attention-banner pattern as the download queue: a count from the
  // backend drives the message, never a fabricated state.
  const failedConversions = (
    Array.isArray(conversions.data?.items) ? conversions.data.items : []
  ).filter((job) => job.state === "failed").length;
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
  const upgradeCatalog =
    upgradeScope === "artist"
      ? artistUpgradeCatalog
      : upgradeScope === "album"
        ? albumUpgradeCatalog
        : trackUpgradeCatalog;
  const upgradePickerItems: SourceUpgradePickerItem[] =
    upgradeScope === "artist"
      ? (artistUpgradeCatalog.data?.items ?? []).flatMap((artist) =>
          artist.id
            ? [
                {
                  id: artist.id,
                  label: artist.name,
                  detail:
                    [artist.type, artist.country].filter(Boolean).join(" · ") ||
                    "Saved artist",
                },
              ]
            : [],
        )
      : upgradeScope === "album"
        ? (albumUpgradeCatalog.data?.items ?? []).flatMap((album) =>
            album.id
              ? [
                  {
                    id: album.id,
                    label: album.artist_name
                      ? `${album.artist_name} - ${album.title}`
                      : album.title,
                    detail: album.year ? String(album.year) : "Saved album",
                  },
                ]
              : [],
          )
        : upgradeScope === "track"
          ? (trackUpgradeCatalog.data?.items ?? []).flatMap((track) =>
              track.id
                ? [
                    {
                      id: track.id,
                      label: track.artist_name
                        ? `${track.artist_name} - ${track.title}`
                        : track.title,
                      detail: `${track.album_title || "Saved album"} · disc ${track.disc}, track ${track.number}`,
                    },
                  ]
                : [],
            )
          : [];
  const upgradeCatalogTotal = upgradeCatalog.data?.total ?? 0;
  const upgradeCatalogLimit = upgradeCatalog.data?.limit || 12;
  const upgradeCatalogPages = Math.max(
    1,
    Math.ceil(upgradeCatalogTotal / upgradeCatalogLimit),
  );
  const upgradeCatalogCurrentPage = Math.min(
    upgradeCatalog.data?.page || catalogPage,
    upgradeCatalogPages,
  );
  const upgradeJobItems = Array.isArray(upgradeJobs.data?.items)
    ? upgradeJobs.data.items
    : [];
  const upgradeQueueTotal = upgradeJobs.data?.total ?? 0;
  const upgradeQueueLimit = upgradeJobs.data?.limit || 24;
  const upgradeQueuePages = Math.max(
    1,
    Math.ceil(upgradeQueueTotal / upgradeQueueLimit),
  );
  const upgradeQueueCurrentPage = Math.min(
    upgradeJobs.data?.page || upgradeQueuePage,
    upgradeQueuePages,
  );

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

  useEffect(() => {
    if (upgradeJobs.data && upgradeQueuePage > upgradeQueuePages) {
      setUpgradeQueuePage(upgradeQueuePages);
    }
  }, [upgradeJobs.data, upgradeQueuePage, upgradeQueuePages]);

  function changeUpgradeScope(scope: MusicSourceUpgradeScope) {
    setUpgradeScope(scope);
    setUpgradeSelection(null);
    setLibraryUpgradeConfirmed(false);
    setCatalogInput("");
    setCatalogQuery("");
    setCatalogPage(1);
    setUpgradeError("");
    setUpgradeNotice("");
  }

  function searchUpgradeCatalog() {
    setCatalogQuery(catalogInput.trim());
    setCatalogPage(1);
  }

  async function queueSourceUpgrade(e: FormEvent) {
    e.preventDefault();
    if (upgradePending) return;
    setUpgradeError("");
    setUpgradeNotice("");
    if (upgradeScope !== "library" && !upgradeSelection) {
      setUpgradeError(
        `Select a saved ${upgradeScope} before queueing the source upgrade.`,
      );
      return;
    }
    if (upgradeScope === "library" && !libraryUpgradeConfirmed) {
      setUpgradeError(
        "Confirm that you want to queue a source upgrade for the entire library.",
      );
      return;
    }
    const maxBitrate =
      upgradeMaxBitrate.trim() === "" ? 0 : Number(upgradeMaxBitrate);
    if (!Number.isFinite(maxBitrate) || maxBitrate < 0) {
      setUpgradeError(
        "Maximum bitrate must be 0 or a positive number of kbps.",
      );
      return;
    }
    const selection = upgradeSelection;
    const scope = upgradeScope;
    setUpgradePending(true);
    try {
      await api("/library/music/upgrade-jobs", {
        method: "POST",
        body: JSON.stringify({
          scope,
          scope_id: scope === "library" ? 0 : (selection?.id ?? 0),
          target_quality: upgradeTarget,
          max_bitrate_kbps: maxBitrate,
        }),
      });
      setUpgradeNotice(
        `Source upgrade queued for ${scope === "library" ? "the entire library" : (selection?.label ?? `saved ${scope}`)}.`,
      );
      setLibraryUpgradeConfirmed(false);
      if (upgradeQueuePage === 1) upgradeJobs.reload();
      else setUpgradeQueuePage(1);
    } catch (e) {
      setUpgradeError(messageFor(e));
    } finally {
      setUpgradePending(false);
    }
  }

  async function sourceUpgradeJobAction(
    id: number,
    action: "cancel" | "retry",
  ) {
    if (upgradeActionPending !== null) return;
    setUpgradeActionPending(id);
    setUpgradeActionError("");
    try {
      await api(`/library/music/upgrade-jobs/${id}/${action}`, {
        method: "POST",
        body: "{}",
      });
      upgradeJobs.reload();
    } catch (e) {
      setUpgradeActionError(messageFor(e));
    } finally {
      setUpgradeActionPending(null);
    }
  }

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
          // Transcoding fields (profiles, streaming quality, cache) are
          // managed on the Music settings page; forward the loaded values so
          // saving here cannot reset them, and the revision fence rejects a
          // stale save if they changed in between.
          ...prefs.data,
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
      if (
        e instanceof APIError &&
        (e.status === 409 || e.code === "music_revision_conflict")
      ) {
        // Someone else saved music settings first. Rebase the form from the
        // server so the conflict loop ends; the current edit is discarded.
        prefs.reload();
        setError(
          "The music settings changed while you were working. The saved settings have been reloaded — review them and apply your changes again.",
        );
      } else {
        setError(messageFor(e));
      }
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
      if (
        e instanceof APIError &&
        (e.status === 409 || e.code === "music_revision_conflict")
      ) {
        schedule.reload();
        setError(
          "The music schedule changed while you were working. The saved schedule has been reloaded — review it and apply your changes again.",
        );
      } else {
        setError(messageFor(e));
      }
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
            spinning={wanted.loading || upgradeJobs.loading}
            onClick={() => {
              wanted.reload();
              jobs.reload();
              upgradeJobs.reload();
              if (upgradeScope !== "library") upgradeCatalog.reload();
            }}
          />
          <ToolbarButton
            icon={SlidersHorizontal}
            label="Transcoding settings"
            title="Profiles, streaming quality, retention and stream cache"
            to="/music/settings"
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
        {failedConversions > 0 && (
          <Alert kind="warning" title="Conversions need attention">
            <p>
              {failedConversions}{" "}
              {failedConversions === 1 ? "conversion has" : "conversions have"}{" "}
              failed.{" "}
              <Link to="/music/organize">Review the conversion queue</Link> to
              retry or cancel them.
            </p>
          </Alert>
        )}
        <FieldSet
          legend="Queue source upgrade"
          description="Search for better source copies of music already saved in the catalog."
        >
          <Alert kind="info" title="How source upgrades work">
            Source copies are downloaded and their actual bytes are probed
            before import. Originals are retained as archive variants. A fixed
            maximum of 200 attempts per hour applies.
          </Alert>
          {upgradeError && <Alert kind="danger">{upgradeError}</Alert>}
          {upgradeNotice && <Alert kind="success">{upgradeNotice}</Alert>}
          <form onSubmit={(e) => void queueSourceUpgrade(e)}>
            <div className="mus-download-options">
              <label>
                Upgrade scope{" "}
                <select
                  aria-label="Upgrade scope"
                  value={upgradeScope}
                  disabled={upgradePending}
                  onChange={(e) =>
                    changeUpgradeScope(
                      e.target.value as MusicSourceUpgradeScope,
                    )
                  }
                >
                  <option value="library">Entire library</option>
                  <option value="artist">Artist</option>
                  <option value="album">Album</option>
                  <option value="track">Track</option>
                </select>
              </label>
              <label>
                Target quality{" "}
                <select
                  aria-label="Target quality"
                  value={upgradeTarget}
                  disabled={upgradePending}
                  onChange={(e) =>
                    setUpgradeTarget(e.target.value as MusicSourceUpgradeTarget)
                  }
                >
                  <option value="high">High quality</option>
                  <option value="lossless">Lossless</option>
                  <option value="hires">Hi-Res (lossless, above 48 kHz)</option>
                </select>
              </label>
              <label>
                Maximum bitrate (kbps){" "}
                <TextField.Root
                  aria-label="Maximum bitrate (kbps)"
                  type="number"
                  min="0"
                  step="1"
                  inputMode="numeric"
                  value={upgradeMaxBitrate}
                  disabled={upgradePending}
                  onChange={(e) => setUpgradeMaxBitrate(e.target.value)}
                />
                <span className="rs-form-help">
                  Optional downloaded-candidate cap in kbps. Use 0 for no cap.
                </span>
              </label>
            </div>
            {upgradeScope === "library" ? (
              <Check
                label="I confirm this source upgrade for the entire saved music library."
                description="This can queue work across every saved artist, album and track."
                checked={libraryUpgradeConfirmed}
                disabled={upgradePending}
                onChange={setLibraryUpgradeConfirmed}
              />
            ) : (
              <div aria-label={`Saved ${upgradeScope} picker`}>
                <div
                  className="mus-scan-summary"
                  role="status"
                  aria-live="polite"
                >
                  <span>
                    <strong>Selected {upgradeScope}: </strong>
                    {upgradeSelection?.label ?? "None"}
                  </span>
                  {upgradeSelection && (
                    <Button
                      type="button"
                      size="1"
                      variant="soft"
                      color="gray"
                      disabled={upgradePending}
                      onClick={() => setUpgradeSelection(null)}
                    >
                      Clear selection
                    </Button>
                  )}
                </div>
                <div
                  className="mus-search"
                  role="search"
                  aria-label={`Search saved ${upgradeScope}s`}
                >
                  <TextField.Root
                    aria-label={`Search saved ${upgradeScope}s`}
                    placeholder={`Search saved ${upgradeScope}s by name`}
                    value={catalogInput}
                    maxLength={200}
                    disabled={upgradePending}
                    onChange={(e) => setCatalogInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.preventDefault();
                        searchUpgradeCatalog();
                      }
                    }}
                  />
                  <Button
                    type="button"
                    variant="soft"
                    disabled={upgradePending}
                    onClick={searchUpgradeCatalog}
                  >
                    Search catalog
                  </Button>
                </div>
                <RequestState
                  loading={upgradeCatalog.loading}
                  error={upgradeCatalog.error}
                  code={upgradeCatalog.code}
                  retry={upgradeCatalog.reload}
                />
                {upgradeCatalog.data && upgradePickerItems.length === 0 && (
                  <p className="rs-muted" role="status">
                    No saved {upgradeScope}s match this search. Your current
                    selection is unchanged.
                  </p>
                )}
                {upgradePickerItems.length > 0 && (
                  <Table label={`Saved ${upgradeScope}s for source upgrade`}>
                    <thead>
                      <tr>
                        <th scope="col">Name</th>
                        <th scope="col">Details</th>
                        <th scope="col" className="is-actions">
                          <span className="sr-only">Actions</span>
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {upgradePickerItems.map((item) => (
                        <tr key={item.id}>
                          <td>{item.label}</td>
                          <td>{item.detail}</td>
                          <td className="is-actions">
                            <Button
                              type="button"
                              size="1"
                              variant={
                                upgradeSelection?.id === item.id
                                  ? "solid"
                                  : "soft"
                              }
                              aria-label={`Select ${upgradeScope} ${item.label}`}
                              disabled={upgradePending}
                              onClick={() =>
                                setUpgradeSelection({
                                  id: item.id,
                                  label: item.label,
                                })
                              }
                            >
                              {upgradeSelection?.id === item.id
                                ? "Selected"
                                : "Select"}
                            </Button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </Table>
                )}
                <SourceUpgradePager
                  label={`saved ${upgradeScope} catalog`}
                  page={upgradeCatalogCurrentPage}
                  pages={upgradeCatalogPages}
                  onPage={setCatalogPage}
                />
              </div>
            )}
            <FormActions>
              <Button type="submit" disabled={upgradePending}>
                {upgradePending
                  ? "Queueing source upgrade"
                  : "Queue source upgrade"}
              </Button>
            </FormActions>
          </form>
        </FieldSet>
        <FieldSet
          legend="Source upgrade queue"
          description="Durable source-upgrade jobs, refreshed every 5 seconds."
        >
          <RequestState
            loading={upgradeJobs.loading}
            error={upgradeJobs.error}
            code={upgradeJobs.code}
            retry={upgradeJobs.reload}
          />
          {upgradeActionError && (
            <Alert kind="danger">{upgradeActionError}</Alert>
          )}
          {upgradeJobs.data && upgradeJobItems.length === 0 && (
            <EmptyState icon={ListMusic} title="No source upgrades queued">
              Queue a source upgrade above to see its progress here.
            </EmptyState>
          )}
          {upgradeJobItems.length > 0 && (
            <Table label="Source upgrade jobs">
              <thead>
                <tr>
                  <th scope="col">Scope</th>
                  <th scope="col">Target</th>
                  <th scope="col">State</th>
                  <th scope="col">Progress</th>
                  <th scope="col">Results</th>
                  <th scope="col">Current track</th>
                  <th scope="col">Error</th>
                  <th scope="col">Updated</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {upgradeJobItems.map((job) => (
                  <tr key={job.id}>
                    <td>
                      {job.scope_name || sourceUpgradeScopeLabels[job.scope]}
                      <span className="rs-muted" style={{ display: "block" }}>
                        {sourceUpgradeScopeLabels[job.scope]}
                      </span>
                    </td>
                    <td>
                      {sourceUpgradeTargetLabels[job.target_quality]}
                      <span className="rs-muted" style={{ display: "block" }}>
                        {job.max_bitrate_kbps > 0
                          ? `Candidate cap: ${job.max_bitrate_kbps} kbps`
                          : "No downloaded-candidate bitrate cap"}
                      </span>
                      <span className="rs-muted" style={{ display: "block" }}>
                        {job.rate_per_hour} attempts/hour
                      </span>
                    </td>
                    <td>
                      <Label kind={jobKind(job.state)}>{job.state}</Label>
                    </td>
                    <td>
                      <ProgressBar
                        value={
                          job.total_count > 0
                            ? job.processed_count / job.total_count
                            : 0
                        }
                        kind={
                          job.state === "completed"
                            ? "success"
                            : job.state === "failed"
                              ? "danger"
                              : job.state === "cancelled"
                                ? "disabled"
                                : "primary"
                        }
                        size="small"
                        label={`Source upgrade job ${job.id} progress`}
                      />
                      <span>
                        {job.processed_count} of {job.total_count} processed
                      </span>
                    </td>
                    <td>
                      <span style={{ display: "block" }}>
                        Upgraded: {job.upgraded_count}
                      </span>
                      <span style={{ display: "block" }}>
                        Skipped: {job.skipped_count}
                      </span>
                      <span style={{ display: "block" }}>
                        Failed: {job.failed_count}
                      </span>
                    </td>
                    <td>{job.current_track || "-"}</td>
                    <td>{job.error || "-"}</td>
                    <td>{sourceUpgradeTime(job.updated_at)}</td>
                    <td className="is-actions">
                      {["queued", "running"].includes(job.state) && (
                        <Button
                          size="1"
                          variant="soft"
                          aria-label={`Cancel source upgrade job ${job.id}`}
                          disabled={upgradeActionPending !== null}
                          onClick={() =>
                            void sourceUpgradeJobAction(job.id, "cancel")
                          }
                        >
                          Cancel
                        </Button>
                      )}
                      {["failed", "cancelled"].includes(job.state) && (
                        <Button
                          size="1"
                          variant="soft"
                          aria-label={`Retry source upgrade job ${job.id}`}
                          disabled={upgradeActionPending !== null}
                          onClick={() =>
                            void sourceUpgradeJobAction(job.id, "retry")
                          }
                        >
                          Retry
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
          {upgradeJobs.data && (
            <div className="mus-scan-summary">
              <span>
                {upgradeQueueTotal} source upgrade{" "}
                {upgradeQueueTotal === 1 ? "job" : "jobs"}
              </span>
            </div>
          )}
          <SourceUpgradePager
            label="source upgrade queue"
            page={upgradeQueueCurrentPage}
            pages={upgradeQueuePages}
            onPage={setUpgradeQueuePage}
          />
        </FieldSet>
        <FieldSet legend="Download preferences">
          <RequestState
            loading={prefs.loading}
            error={prefs.error}
            code={prefs.code}
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
            code={schedule.code}
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
          code={wanted.code}
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
