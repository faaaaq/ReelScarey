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
  type MusicEditJob,
  type MusicEditJobList,
  type MusicEditPlan,
  type MusicFilePage,
} from "../api";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
import "./Music.css";

function jobKind(state: string) {
  if (state === "completed") return "success" as const;
  if (state === "failed") return "danger" as const;
  if (state === "rolled_back") return "disabled" as const;
  return "info" as const;
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
  const pageCount = files.data
    ? Math.max(1, Math.ceil(files.data.total / files.data.limit))
    : 1;

  // Drop out-of-range pages when the catalog shrinks between reloads.
  useEffect(() => {
    if (files.data && page > pageCount) setPage(pageCount);
  }, [files.data, page, pageCount]);

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
      </PageBody>
    </>
  );
}
