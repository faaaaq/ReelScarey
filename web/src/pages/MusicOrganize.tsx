import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { FolderCog, RefreshCw, Undo2 } from "lucide-react";
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

export function MusicOrganize() {
  const files = useResource<MusicFilePage>("/library/music/files");
  const jobs = useLiveResource<MusicEditJobList>("/library/music/edits", 5000);
  const [selected, setSelected] = useState<number[]>([]);
  const [plan, setPlan] = useState<MusicEditPlan | null>(null);
  const [jobID, setJobID] = useState<number | null>(null);
  const job = useLiveResource<MusicEditJob>(
    jobID ? `/library/music/edits/${jobID}` : null,
    2000,
  );
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  async function preview() {
    if (pending || selected.length === 0) return;
    setPending(true);
    setError("");
    try {
      const result = await api<MusicEditPlan>("/library/music/edits/preview", {
        method: "POST",
        body: JSON.stringify({ file_ids: selected }),
      });
      setPlan(result);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function apply() {
    if (!plan || pending) return;
    setPending(true);
    setError("");
    try {
      const result = await api<MusicEditJob>("/library/music/edits/apply", {
        method: "POST",
        body: JSON.stringify({
          file_ids: plan.files.map((file) => file.music_file_id),
          plan_hash: plan.plan_hash,
          reviewed: true,
        }),
      });
      setJobID(result.id);
      setPlan(null);
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
            disabled={pending || selected.length === 0}
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
        <RequestState
          loading={files.loading}
          error={files.error}
          retry={files.reload}
        />
        {files.data && files.data.items.length === 0 && (
          <EmptyState icon={FolderCog} title="No registered music files">
            Import and register files from the Music import page first.
          </EmptyState>
        )}
        {files.data && files.data.items.length > 0 && (
          <FieldSet legend="Registered files">
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
                {files.data.items.map((file) => (
                  <tr key={file.id}>
                    <td>
                      <input
                        type="checkbox"
                        aria-label={`Select ${file.relative_path}`}
                        checked={selected.includes(file.id)}
                        onChange={(e) =>
                          setSelected(
                            e.target.checked
                              ? [...selected, file.id]
                              : selected.filter((id) => id !== file.id),
                          )
                        }
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
                  onClick={() => setPlan(null)}
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
