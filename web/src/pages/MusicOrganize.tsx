import { useEffect, useMemo, useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  ChevronLeft,
  ChevronRight,
  FolderCog,
  RefreshCw,
  Search,
  Undo2,
} from "lucide-react";
import {
  Alert,
  EmptyState,
  FieldSet,
  formatBytes,
  Label,
  PageBody,
  PageToolbar,
  ProgressBar,
  Table,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { PageHeading, RequestState } from "../components";
import {
  api,
  applyOptimization,
  cancelConversion,
  messageFor,
  previewOptimization,
  retryConversion,
  type ConversionJobPage,
  type MusicEditJob,
  type MusicEditJobList,
  type MusicEditPlan,
  type MusicFilePage,
  type OptimizationPreview,
} from "../api";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
import { MusicExportMenu } from "../musicDownload";
import { profileLabel, useMusicProfiles } from "../musicProfiles";
import "./Music.css";

function jobKind(state: string) {
  if (state === "completed") return "success" as const;
  if (state === "failed") return "danger" as const;
  if (state === "rolled_back") return "disabled" as const;
  return "info" as const;
}

function conversionKind(state: string) {
  if (state === "completed") return "success" as const;
  if (state === "failed") return "danger" as const;
  if (state === "cancelled") return "disabled" as const;
  return "info" as const;
}

/**
 * Splits file ids into the groups the optimize preview and apply endpoints
 * accept; the backend rejects any request with more than 500 file ids.
 */
export function chunkFileIds(ids: number[], size = 500): number[][] {
  const batches: number[][] = [];
  for (let index = 0; index < ids.length; index += size) {
    batches.push(ids.slice(index, index + size));
  }
  return batches;
}

/**
 * Combines the dry runs of a chunked preview into one preview so the review
 * table and totals cover the whole selection.
 */
export function mergePreviews(previews: OptimizationPreview[]) {
  const [first] = previews;
  return {
    profile: first.profile,
    items: previews.flatMap((preview) => preview.items),
    eligible_count: previews.reduce(
      (sum, preview) => sum + (preview.eligible_count ?? 0),
      0,
    ),
    current_bytes: previews.reduce(
      (sum, preview) => sum + (preview.current_bytes ?? 0),
      0,
    ),
    estimated_bytes: previews.reduce(
      (sum, preview) => sum + (preview.estimated_bytes ?? 0),
      0,
    ),
    saved_bytes: previews.reduce(
      (sum, preview) => sum + (preview.saved_bytes ?? 0),
      0,
    ),
  };
}

/** Optional catalog corrections sent with the preview and apply requests. */
type EditOverrideInput = {
  artist: string;
  album: string;
  albumID: string;
  year: string;
};

const emptyOverrides: EditOverrideInput = {
  artist: "",
  album: "",
  albumID: "",
  year: "",
};

/**
 * Turns the raw override inputs into the JSON fields the backend understands:
 * artist, album, album_id and year. Blank or invalid values are omitted so an
 * untouched field keeps the catalog value.
 */
function editOverrideFields(overrides: EditOverrideInput) {
  const fields: Record<string, string | number> = {};
  const artist = overrides.artist.trim();
  const album = overrides.album.trim();
  const year = Number.parseInt(overrides.year, 10);
  const albumID = Number.parseInt(overrides.albumID, 10);
  if (artist !== "") fields.artist = artist;
  if (album !== "") fields.album = album;
  if (Number.isInteger(year) && year > 0) fields.year = year;
  if (Number.isInteger(albumID) && albumID > 0) fields.album_id = albumID;
  return fields;
}

/** True when the overrides only correct values, without re-selecting files. */
function hasAlbumSelection(overrides: EditOverrideInput) {
  return "album_id" in editOverrideFields(overrides);
}

export function MusicOrganize() {
  const [page, setPage] = useState(1);
  const files = useResource<MusicFilePage>(`/library/music/files?page=${page}`);
  const jobs = useLiveResource<MusicEditJobList>("/library/music/edits", 5000);
  const [selected, setSelected] = useState<number[]>([]);
  const [filter, setFilter] = useState("");
  const [overrides, setOverrides] = useState<EditOverrideInput>(emptyOverrides);
  const [plan, setPlan] = useState<MusicEditPlan | null>(null);
  const [planBody, setPlanBody] = useState<Record<string, unknown> | null>(
    null,
  );
  const [jobID, setJobID] = useState<number | null>(null);
  const job = useLiveResource<MusicEditJob>(
    jobID ? `/library/music/edits/${jobID}` : null,
    2000,
  );
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [convPage, setConvPage] = useState(1);
  const [convState, setConvState] = useState("all");
  const conversions = useLiveResource<ConversionJobPage>(
    `/library/music/conversions?page=${convPage}`,
    5000,
  );
  const profiles = useMusicProfiles();
  const [optimizeProfile, setOptimizeProfile] = useState("");
  const [optimizePreview, setOptimizePreview] =
    useState<OptimizationPreview | null>(null);
  const [optimizeMessage, setOptimizeMessage] = useState("");
  const [optimizeProgress, setOptimizeProgress] = useState("");
  /** Eligible files already queued for the open preview, for batch resume. */
  const [optimizeQueued, setOptimizeQueued] = useState(0);

  const conversionItems = Array.isArray(conversions.data?.items)
    ? conversions.data.items
    : [];
  const failedConversions = conversionItems.filter(
    (job) => job.state === "failed",
  );
  // The backend exposes no state query param on the conversions list, so the
  // state filter narrows the jobs of the fetched page only and says so.
  const visibleConversions =
    convState === "all"
      ? conversionItems
      : conversionItems.filter((job) => job.state === convState);
  const conversionPageCount = conversions.data
    ? Math.max(
        1,
        Math.ceil(conversions.data.total / Math.max(1, conversions.data.limit)),
      )
    : 1;

  const items = files.data?.items ?? [];
  const visibleFiles = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (query === "") return items;
    return items.filter((file) =>
      `${file.artist_name} ${file.album_title} ${file.track_title} ${file.relative_path}`
        .toLowerCase()
        .includes(query),
    );
  }, [items, filter]);
  const fileByID = new Map(items.map((file) => [file.id, file]));
  const pageCount = files.data
    ? Math.max(1, Math.ceil(files.data.total / files.data.limit))
    : 1;

  // Drop out-of-range pages when the catalog shrinks between reloads.
  useEffect(() => {
    if (files.data && page > pageCount) setPage(pageCount);
  }, [files.data, page, pageCount]);

  // Same clamp for the conversion queue.
  useEffect(() => {
    if (conversions.data && convPage > conversionPageCount)
      setConvPage(conversionPageCount);
  }, [conversions.data, convPage, conversionPageCount]);

  function toggle(id: number, checked: boolean) {
    setSelected((current) =>
      checked ? [...current, id] : current.filter((value) => value !== id),
    );
  }

  function selectAllVisible() {
    setSelected((current) => {
      const next = new Set(current);
      for (const file of visibleFiles) next.add(file.id);
      return [...next];
    });
  }

  async function preview() {
    if (pending) return;
    const albumSelected = hasAlbumSelection(overrides);
    if (selected.length === 0 && !albumSelected) return;
    const body = { file_ids: selected, ...editOverrideFields(overrides) };
    setPending(true);
    setError("");
    try {
      const result = await api<MusicEditPlan>("/library/music/edits/preview", {
        method: "POST",
        body: JSON.stringify(body),
      });
      setPlan(result);
      setPlanBody(body);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function apply() {
    if (!plan || !planBody || pending) return;
    setPending(true);
    setError("");
    try {
      const result = await api<MusicEditJob>("/library/music/edits/apply", {
        method: "POST",
        body: JSON.stringify({
          ...planBody,
          plan_hash: plan.plan_hash,
          reviewed: true,
        }),
      });
      setJobID(result.id);
      setPlan(null);
      setPlanBody(null);
      setSelected([]);
      files.reload();
      jobs.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function rollback(id: number) {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/library/music/edits/${id}/rollback`, {
        method: "POST",
        body: JSON.stringify({ reviewed: true }),
      });
      files.reload();
      jobs.reload();
      if (jobID === id) job.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function conversionAction(id: number, action: "cancel" | "retry") {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      if (action === "cancel") await cancelConversion(id);
      else await retryConversion(id);
      conversions.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function runOptimizePreview() {
    if (!optimizeProfile || pending) return;
    setPending(true);
    setError("");
    setOptimizeMessage("");
    try {
      // With no page selection the backend previews every registered file;
      // a selection narrows the dry run. The preview endpoint also caps one
      // request at 500 file ids, so a larger selection is split and the dry
      // runs are merged into one review.
      const groups = selected.length > 0 ? chunkFileIds(selected) : [[]];
      const previews = await Promise.all(
        groups.map((ids) =>
          previewOptimization(
            optimizeProfile,
            ids.length > 0 ? ids : undefined,
          ),
        ),
      );
      setOptimizeQueued(0);
      setOptimizePreview(mergePreviews(previews));
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function applyOptimize() {
    if (!optimizePreview || pending) return;
    const eligibleIDs = optimizePreview.items
      .filter((item) => item.eligible)
      .map((item) => item.file_id);
    // After a failed batch the already queued files are skipped, so the next
    // click queues only the rest of the preview.
    const remaining = eligibleIDs.slice(optimizeQueued);
    if (remaining.length === 0) return;
    const batches = chunkFileIds(remaining);
    const totalBatches = chunkFileIds(eligibleIDs).length;
    setPending(true);
    setError("");
    setOptimizeMessage("");
    let queued = optimizeQueued;
    let failure = "";
    for (const [index, batch] of batches.entries()) {
      setOptimizeProgress(
        `Optimizing batch ${index + 1} of ${batches.length}…`,
      );
      try {
        await applyOptimization(optimizeProfile, batch);
        queued += batch.length;
        setOptimizeQueued(queued);
      } catch (e) {
        failure = messageFor(e);
        break;
      }
    }
    setPending(false);
    setOptimizeProgress("");
    conversions.reload();
    if (failure === "") {
      setOptimizePreview(null);
      setOptimizeQueued(0);
      setOptimizeMessage(
        `Queued ${queued} file${queued === 1 ? "" : "s"} for conversion${
          totalBatches > 1 ? ` across ${totalBatches} batches` : ""
        }. Originals are kept or deleted according to the original-file retention setting.`,
      );
    } else {
      setError(
        `Queued ${queued} of ${eligibleIDs.length} eligible files before a batch failed: ${failure}. Use Convert again to queue the remaining files.`,
      );
    }
  }

  return (
    <>
      <PageToolbar label="Music organization actions">
        <ToolbarSection>
          <ToolbarButton
            icon={FolderCog}
            label="Preview"
            disabled={
              pending ||
              (selected.length === 0 && !hasAlbumSelection(overrides))
            }
            onClick={() => void preview()}
          />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={files.loading}
            onClick={() => {
              files.reload();
              jobs.reload();
            }}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="mus-body">
        <PageHeading
          title="Organize music"
          description="Rename and move registered files into Artist/Album folders and write catalog tags. The original is retained before a tag rewrite."
        />
        <Alert kind="info" title="Review before applying">
          Every change is previewed first. Files are published without
          overwriting an existing destination. A completed job can be rolled
          back from this page.
        </Alert>
        <FieldSet
          legend="Correct metadata"
          description="Optional corrections applied to the selected files before the plan is built. Leave a field empty to keep the catalog value. An album ID previews every registered file of that album instead of the selection."
        >
          <div className="mus-overrides">
            <TextField.Root
              aria-label="Corrected artist"
              placeholder="Corrected artist"
              maxLength={200}
              value={overrides.artist}
              onChange={(e) =>
                setOverrides({ ...overrides, artist: e.target.value })
              }
            />
            <TextField.Root
              aria-label="Corrected album"
              placeholder="Corrected album"
              maxLength={200}
              value={overrides.album}
              onChange={(e) =>
                setOverrides({ ...overrides, album: e.target.value })
              }
            />
            <TextField.Root
              aria-label="Album ID"
              placeholder="Album ID"
              inputMode="numeric"
              value={overrides.albumID}
              onChange={(e) =>
                setOverrides({ ...overrides, albumID: e.target.value })
              }
            />
            <TextField.Root
              aria-label="Corrected year"
              placeholder="Year"
              inputMode="numeric"
              value={overrides.year}
              onChange={(e) =>
                setOverrides({ ...overrides, year: e.target.value })
              }
            />
          </div>
        </FieldSet>
        <RequestState
          loading={files.loading}
          error={files.error}
          code={files.code}
          retry={files.reload}
        />
        {files.data && items.length === 0 && (
          <EmptyState icon={FolderCog} title="No registered music files">
            Import and register files from the Music import page first.
          </EmptyState>
        )}
        {files.data && items.length > 0 && (
          <FieldSet
            legend="Registered files"
            actions={
              <>
                <Button
                  size="1"
                  variant="soft"
                  disabled={visibleFiles.length === 0}
                  onClick={selectAllVisible}
                >
                  Select all
                </Button>
                <Button
                  size="1"
                  variant="soft"
                  color="gray"
                  disabled={selected.length === 0}
                  onClick={() => setSelected([])}
                >
                  Clear
                </Button>
              </>
            }
          >
            <div className="mus-filter">
              <TextField.Root
                aria-label="Filter registered files"
                placeholder="Filter by artist, album, track or path"
                maxLength={200}
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              >
                <TextField.Slot>
                  <Search size={15} aria-hidden="true" />
                </TextField.Slot>
              </TextField.Root>
            </div>
            {visibleFiles.length === 0 ? (
              <Alert kind="info">No registered files match this filter.</Alert>
            ) : (
              <Table label="Registered music files">
                <thead>
                  <tr>
                    <th scope="col">
                      <span className="sr-only">Select</span>
                    </th>
                    <th scope="col">Artist</th>
                    <th scope="col">Album</th>
                    <th scope="col">Track</th>
                    <th scope="col">Path</th>
                    <th scope="col">Format</th>
                    <th scope="col" className="is-actions">
                      <span className="sr-only">Export</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {visibleFiles.map((file) => (
                    <tr key={file.id}>
                      <td>
                        <input
                          type="checkbox"
                          aria-label={`Select ${file.relative_path}`}
                          checked={selected.includes(file.id)}
                          onChange={(e) => toggle(file.id, e.target.checked)}
                        />
                      </td>
                      <td>{file.artist_name}</td>
                      <td>{file.album_title}</td>
                      <td>
                        {file.disc > 1
                          ? `${file.disc}.${file.number}`
                          : file.number}{" "}
                        {file.track_title}
                      </td>
                      <td>
                        <span className="mus-path">{file.relative_path}</span>
                      </td>
                      <td>{file.format || "—"}</td>
                      <td className="is-actions">
                        {file.id > 0 && (
                          <MusicExportMenu
                            fileID={file.id}
                            title={file.track_title || file.relative_path}
                          />
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            <div className="mus-paging">
              <span>{`Page ${page} of ${pageCount} · ${files.data.total} registered files · ${selected.length} selected`}</span>
              <Button
                size="1"
                variant="soft"
                aria-label="Previous page"
                disabled={pending || page <= 1}
                onClick={() => setPage((value) => Math.max(1, value - 1))}
              >
                <ChevronLeft size={14} aria-hidden="true" /> Previous
              </Button>
              <Button
                size="1"
                variant="soft"
                aria-label="Next page"
                disabled={pending || page >= pageCount}
                onClick={() =>
                  setPage((value) => Math.min(pageCount, value + 1))
                }
              >
                Next <ChevronRight size={14} aria-hidden="true" />
              </Button>
            </div>
          </FieldSet>
        )}
        <FieldSet
          legend="Optimize storage"
          description="Advanced: converts registered files to the selected profile to save disk space. The preview never writes files; applying queues conversion jobs and originals are kept or deleted according to the original-file retention setting in Music settings."
        >
          <div className="mus-search">
            <label>
              Target profile{" "}
              <select
                aria-label="Optimize target profile"
                value={optimizeProfile}
                onChange={(e) => {
                  setOptimizeProfile(e.target.value);
                  setOptimizePreview(null);
                  setOptimizeQueued(0);
                }}
              >
                <option value="">Choose a profile…</option>
                {profiles.profiles
                  .filter(
                    (profile) =>
                      profile.key !== "" && profile.codec !== "original",
                  )
                  .map((profile) => (
                    <option key={profile.key} value={profile.key}>
                      {profileLabel(profile)}
                    </option>
                  ))}
              </select>
            </label>
            <Button
              disabled={pending || optimizeProfile === ""}
              onClick={() => void runOptimizePreview()}
            >
              Preview savings
            </Button>
          </div>
          {selected.length > 0 ? (
            <p className="rs-muted rs-small">
              Previewing {selected.length} file
              {selected.length === 1 ? "" : "s"} chosen above. Clear the
              selection above to preview every registered file.
            </p>
          ) : (
            <p className="rs-muted rs-small">
              Previewing every registered file.
            </p>
          )}
          {optimizeMessage && <Alert kind="success">{optimizeMessage}</Alert>}
          {optimizeProgress && (
            <p className="rs-muted rs-small" role="status" aria-live="polite">
              {optimizeProgress}
            </p>
          )}
          {optimizePreview && (
            <>
              {optimizePreview.items.length === 0 ? (
                <Alert kind="info">
                  No registered files were returned for this profile.
                </Alert>
              ) : (
                <>
                  <Table label="Optimization preview">
                    <thead>
                      <tr>
                        <th scope="col">Track</th>
                        <th scope="col">Eligible</th>
                        <th scope="col">Current</th>
                        <th scope="col">Estimated</th>
                        <th scope="col">Savings</th>
                        <th scope="col">Reason</th>
                      </tr>
                    </thead>
                    <tbody>
                      {optimizePreview.items.map((item) => {
                        const file = fileByID.get(item.file_id);
                        return (
                          <tr key={item.file_id}>
                            <td>
                              {file
                                ? `${file.artist_name} — ${file.track_title}`
                                : `File ${item.file_id}`}
                            </td>
                            <td>
                              <Label
                                kind={item.eligible ? "success" : "disabled"}
                              >
                                {item.eligible ? "Eligible" : "Skipped"}
                              </Label>
                            </td>
                            <td>{formatBytes(item.current_bytes)}</td>
                            <td>{formatBytes(item.estimated_bytes)}</td>
                            <td>
                              {item.saved_bytes > 0
                                ? formatBytes(item.saved_bytes)
                                : "—"}
                            </td>
                            <td>{item.reason || "—"}</td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </Table>
                  <div className="mus-scan-actions">
                    <Button
                      size="1"
                      variant="soft"
                      color="gray"
                      disabled={pending}
                      onClick={() => {
                        setOptimizePreview(null);
                        setOptimizeQueued(0);
                      }}
                    >
                      Cancel
                    </Button>
                    <Button
                      size="1"
                      disabled={
                        pending ||
                        optimizePreview.items.every((item) => !item.eligible)
                      }
                      onClick={() => void applyOptimize()}
                    >
                      Convert{" "}
                      {
                        optimizePreview.items.filter((item) => item.eligible)
                          .length
                      }{" "}
                      file
                      {optimizePreview.items.filter((item) => item.eligible)
                        .length === 1
                        ? ""
                        : "s"}
                    </Button>
                  </div>
                </>
              )}
            </>
          )}
        </FieldSet>
        {error && <Alert kind="danger">{error}</Alert>}
        {plan && (
          <FieldSet
            legend="Review plan"
            actions={
              <>
                <Button
                  size="1"
                  variant="soft"
                  color="gray"
                  onClick={() => {
                    setPlan(null);
                    setPlanBody(null);
                  }}
                >
                  Cancel
                </Button>
                <Button
                  size="1"
                  disabled={pending || plan.files.length === 0}
                  onClick={() => void apply()}
                >
                  Apply reviewed plan
                </Button>
              </>
            }
          >
            {plan.files.length === 0 ? (
              <Alert kind="info">
                These files are already organized and tagged.
              </Alert>
            ) : (
              <Table label="Music organization plan">
                <thead>
                  <tr>
                    <th scope="col">Source</th>
                    <th scope="col">Title</th>
                    <th scope="col">Artist</th>
                    <th scope="col">Album</th>
                    <th scope="col">Year</th>
                    <th scope="col">Track</th>
                    <th scope="col">Destination</th>
                    <th scope="col">Action</th>
                  </tr>
                </thead>
                <tbody>
                  {plan.files.map((file) => (
                    <tr key={file.music_file_id}>
                      <td>
                        <span className="mus-path">{file.source_path}</span>
                      </td>
                      <td>{file.tags.title}</td>
                      <td>{file.tags.artist}</td>
                      <td>{file.tags.album}</td>
                      <td>{file.tags.year > 0 ? file.tags.year : "—"}</td>
                      <td>
                        {file.tags.disc > 1
                          ? `${file.tags.disc}.${file.tags.track}`
                          : file.tags.track}
                      </td>
                      <td>
                        <span className="mus-path">
                          {file.destination_path}
                        </span>
                      </td>
                      <td>
                        <Label
                          kind={
                            file.action === "move_retag"
                              ? "warning"
                              : file.action === "retag"
                                ? "info"
                                : "success"
                          }
                        >
                          {file.action}
                        </Label>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </FieldSet>
        )}
        {job.data && (
          <FieldSet
            legend={`Edit job #${job.data.id}`}
            actions={
              (job.data.state === "completed" ||
                job.data.state === "failed") && (
                <Button
                  size="1"
                  variant="soft"
                  color="gray"
                  disabled={pending}
                  onClick={() => void rollback(job.data!.id)}
                >
                  <Undo2 size={14} aria-hidden="true" /> Roll back
                </Button>
              )
            }
          >
            <div className="mus-scan-summary">
              <Label kind={jobKind(job.data.state)}>{job.data.state}</Label>
              <span>{job.data.file_count} files</span>
            </div>
            {job.data.error && <Alert kind="danger">{job.data.error}</Alert>}
            {job.data.files && job.data.files.length > 0 && (
              <Table label="Edit job files">
                <thead>
                  <tr>
                    <th scope="col">Source</th>
                    <th scope="col">Destination</th>
                    <th scope="col">State</th>
                    <th scope="col">Error</th>
                  </tr>
                </thead>
                <tbody>
                  {job.data.files.map((file) => (
                    <tr key={`${file.music_file_id}-${file.source_path}`}>
                      <td>
                        <span className="mus-path">{file.source_path}</span>
                      </td>
                      <td>
                        <span className="mus-path">
                          {file.destination_path}
                        </span>
                      </td>
                      <td>
                        <Label kind={jobKind(file.state)}>{file.state}</Label>
                      </td>
                      <td>{file.error || "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </FieldSet>
        )}
        {jobs.data && jobs.data.items.length > 0 && (
          <FieldSet legend="Recent edits">
            <Table label="Recent music edits">
              <thead>
                <tr>
                  <th scope="col">Job</th>
                  <th scope="col">State</th>
                  <th scope="col">Files</th>
                  <th scope="col">Created</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {jobs.data.items.map((item) => (
                  <tr key={item.id}>
                    <td>#{item.id}</td>
                    <td>
                      <Label kind={jobKind(item.state)}>{item.state}</Label>
                    </td>
                    <td>{item.file_count}</td>
                    <td>{new Date(item.created_at * 1000).toLocaleString()}</td>
                    <td className="is-actions">
                      <Button
                        size="1"
                        variant="soft"
                        onClick={() => setJobID(item.id)}
                      >
                        View
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </FieldSet>
        )}
        {failedConversions.length > 0 && (
          <Alert kind="warning" title="Conversions need attention">
            <p>
              {failedConversions.length}{" "}
              {failedConversions.length === 1
                ? "conversion has"
                : "conversions have"}{" "}
              failed. Use Retry in the Conversions panel below.
            </p>
          </Alert>
        )}
        <FieldSet
          legend="Conversions"
          description="The durable conversion queue for downloads, exports and library optimization. Failed or cancelled jobs can be retried; queued or running jobs can be cancelled."
        >
          <RequestState
            loading={conversions.loading}
            error={conversions.error}
            code={conversions.code}
            retry={conversions.reload}
          />
          {conversions.data && conversionItems.length > 0 && (
            <div className="mus-filter">
              <label>
                State{" "}
                <select
                  aria-label="Filter conversions by state"
                  value={convState}
                  onChange={(e) => setConvState(e.target.value)}
                >
                  <option value="all">All states</option>
                  <option value="queued">Queued</option>
                  <option value="processing">Processing</option>
                  <option value="failed">Failed</option>
                  <option value="completed">Completed</option>
                </select>
              </label>
            </div>
          )}
          {conversions.data && conversionItems.length === 0 && (
            <Alert kind="info">
              No conversion jobs. Jobs appear here when a download, export or
              optimization needs a converted copy.
            </Alert>
          )}
          {conversions.data &&
            conversionItems.length > 0 &&
            visibleConversions.length === 0 && (
              <Alert kind="info">
                No {convState} jobs on this page. Choose another state or use
                the page controls to check other pages of the queue.
              </Alert>
            )}
          {conversions.data && visibleConversions.length > 0 && (
            <Table label="Music conversion queue">
              <thead>
                <tr>
                  <th scope="col">Kind</th>
                  <th scope="col">Profile</th>
                  <th scope="col">State</th>
                  <th scope="col">Progress</th>
                  <th scope="col">Error</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {visibleConversions.map((item) => (
                  <tr key={item.id}>
                    <td>{item.kind}</td>
                    <td>{item.profile}</td>
                    <td>
                      <Label kind={conversionKind(item.state)}>
                        {item.state}
                      </Label>
                    </td>
                    <td>
                      {item.state === "processing" ? (
                        <ProgressBar
                          label={`Conversion ${item.id} progress`}
                          value={(item.progress || 0) / 100}
                          size="small"
                          text={`${item.progress || 0}%`}
                        />
                      ) : item.state === "completed" ? (
                        "100%"
                      ) : (
                        "—"
                      )}
                    </td>
                    <td>{item.error || "—"}</td>
                    <td className="is-actions">
                      {["failed", "cancelled"].includes(item.state) && (
                        <Button
                          size="1"
                          variant="soft"
                          disabled={pending}
                          onClick={() =>
                            void conversionAction(item.id, "retry")
                          }
                        >
                          Retry
                        </Button>
                      )}
                      {["queued", "processing"].includes(item.state) && (
                        <Button
                          size="1"
                          variant="soft"
                          color="gray"
                          disabled={pending}
                          onClick={() =>
                            void conversionAction(item.id, "cancel")
                          }
                        >
                          Cancel
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
          {convState !== "all" &&
            conversions.data &&
            conversionItems.length > 0 &&
            visibleConversions.length > 0 && (
              <p className="rs-muted rs-small">
                The state filter applies to the {conversionItems.length} job
                {conversionItems.length === 1 ? "" : "s"} on this page only; the
                backend has no state filter, so use the page controls to check
                older pages of the queue.
              </p>
            )}
          {conversions.data && conversionPageCount > 1 && (
            <div className="mus-paging">
              <span>{`Page ${convPage} of ${conversionPageCount} · ${conversions.data.total} queued jobs`}</span>
              <Button
                size="1"
                variant="soft"
                aria-label="Previous conversions page"
                disabled={pending || convPage <= 1}
                onClick={() => setConvPage((value) => Math.max(1, value - 1))}
              >
                <ChevronLeft size={14} aria-hidden="true" /> Previous
              </Button>
              <Button
                size="1"
                variant="soft"
                aria-label="Next conversions page"
                disabled={pending || convPage >= conversionPageCount}
                onClick={() =>
                  setConvPage((value) =>
                    Math.min(conversionPageCount, value + 1),
                  )
                }
              >
                Next <ChevronRight size={14} aria-hidden="true" />
              </Button>
            </div>
          )}
        </FieldSet>
      </PageBody>
    </>
  );
}
