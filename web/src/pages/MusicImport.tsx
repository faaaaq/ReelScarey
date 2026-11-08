import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { ArrowLeft, FolderSearch, RefreshCw } from "lucide-react";
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
  type MusicScan,
  type MusicScanList,
  type MusicScanPage,
} from "../api";
import { useLiveResource } from "../useLiveResource";
import "./Music.css";

export function MusicImport() {
  const scans = useLiveResource<MusicScanList>("/library/music/scans", 5000);
  const [scanID, setScanID] = useState<number | null>(null);
  const [page, setPage] = useState(1);
  const active = useLiveResource<MusicScanPage>(
    scanID ? `/library/music/scans/${scanID}?page=${page}` : null,
    3000,
  );
  const [open, setOpen] = useState(false);
  const [root, setRoot] = useState("Music");
  const [selected, setSelected] = useState<number[]>([]);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (scanID === null && scans.data?.items.length) {
      setScanID(scans.data.items[0].id);
    }
  }, [scanID, scans.data]);

  useEffect(() => {
    setSelected([]);
  }, [scanID, page]);

  async function startScan(e: FormEvent) {
    e.preventDefault();
    if (pending || !root.trim()) return;
    setPending(true);
    setError("");
    try {
      const scan = await api<MusicScan>("/library/music/scans", {
        method: "POST",
        body: JSON.stringify({ root: root.trim() }),
      });
      setOpen(false);
      setScanID(scan.id);
      setPage(1);
      scans.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function register() {
    if (!active.data || selected.length === 0 || pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/library/music/scans/${active.data.scan.id}/register`, {
        method: "POST",
        body: JSON.stringify({ file_ids: selected, reviewed: true }),
      });
      setSelected([]);
      active.reload();
      scans.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function skip(fileID: number) {
    if (!active.data || pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/library/music/scans/${active.data.scan.id}/skip`, {
        method: "POST",
        body: JSON.stringify({ file_id: fileID }),
      });
      active.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function cancel() {
    if (!active.data || pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/library/music/scans/${active.data.scan.id}/cancel`, {
        method: "POST",
        body: "{}",
      });
      active.reload();
      scans.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  return (
    <>
      <PageToolbar label="Music import actions">
        <ToolbarSection>
          <ToolbarButton icon={ArrowLeft} label="Music" to="/music" />
          <ToolbarButton
            icon={FolderSearch}
            label="Scan folder"
            onClick={() => setOpen(true)}
          />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={scans.loading}
            onClick={() => {
              scans.reload();
              active.reload();
            }}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="mus-body">
        <PageHeading
          title="Music import"
          description="Read tags from a music folder, match them to catalog albums and register files in place. Files are never moved."
        />
        <Alert kind="info" title="Prepare the catalog first">
          <Link to="/music">Add the artist</Link> and open each album once so
          its tracks are loaded before scanning. Files with no matching catalog
          album stay unresolved; tag them with MusicBrainz IDs or add the artist
          first. The folder is relative to the media root and names are
          case-sensitive.
        </Alert>
        <RequestState
          loading={scans.loading}
          error={scans.error}
          retry={scans.reload}
        />
        {scans.data && scans.data.items.length === 0 && (
          <EmptyState
            icon={FolderSearch}
            title="No scans yet"
            actions={<Button onClick={() => setOpen(true)}>Scan folder</Button>}
          >
            Choose a music folder below the media root.
          </EmptyState>
        )}
        {scans.data && scans.data.items.length > 0 && (
          <FieldSet legend="Scans">
            <Table label="Music scans">
              <thead>
                <tr>
                  <th scope="col">Folder</th>
                  <th scope="col">State</th>
                  <th scope="col">Files</th>
                  <th scope="col">Matched</th>
                  <th scope="col">Registered</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {scans.data.items.map((scan) => (
                  <tr key={scan.id}>
                    <td>{scan.root}</td>
                    <td>
                      <Label
                        kind={
                          scan.state === "review" || scan.state === "completed"
                            ? "success"
                            : scan.state === "failed"
                              ? "danger"
                              : "info"
                        }
                      >
                        {scan.state}
                      </Label>
                    </td>
                    <td>{scan.file_count}</td>
                    <td>{scan.matched_count}</td>
                    <td>{scan.registered_count}</td>
                    <td className="is-actions">
                      <Button
                        size="1"
                        variant="soft"
                        onClick={() => {
                          setScanID(scan.id);
                          setPage(1);
                        }}
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
        {error && <Alert kind="danger">{error}</Alert>}
        {active.data?.scan && (
          <FieldSet
            legend={`Scan #${active.data.scan.id} · ${active.data.scan.root}`}
            actions={
              active.data.scan.state === "scanning" ? (
                <Button
                  size="1"
                  variant="soft"
                  color="gray"
                  disabled={pending}
                  onClick={() => void cancel()}
                >
                  Cancel scan
                </Button>
              ) : undefined
            }
          >
            <div className="mus-scan-summary">
              <Label
                kind={active.data.scan.state === "failed" ? "danger" : "info"}
              >
                {active.data.scan.state}
              </Label>
              <span>{active.data.scan.file_count} files</span>
              <span>{active.data.scan.matched_count} matched</span>
              <span>{active.data.scan.unresolved_count} unresolved</span>
              <span>{active.data.scan.registered_count} registered</span>
            </div>
            {active.data.scan.error && (
              <Alert kind="danger">{active.data.scan.error}</Alert>
            )}
            {active.data.scan.state === "scanning" && (
              <Alert kind="info">Scanning the folder…</Alert>
            )}
            {(active.data.items ?? []).length > 0 && (
              <Table label="Scanned music files">
                <thead>
                  <tr>
                    <th scope="col">
                      <span className="sr-only">Select</span>
                    </th>
                    <th scope="col">File</th>
                    <th scope="col">Tags</th>
                    <th scope="col">Track</th>
                    <th scope="col">State</th>
                    <th scope="col" className="is-actions">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {(active.data.items ?? []).map((file) => (
                    <tr key={file.id}>
                      <td>
                        {file.state === "matched" && (
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
                        )}
                      </td>
                      <td>
                        <span className="mus-path">{file.relative_path}</span>
                        {file.error && (
                          <span className="rs-muted"> · {file.error}</span>
                        )}
                      </td>
                      <td>
                        {[
                          file.tag_album_artist || file.tag_artist,
                          file.tag_album,
                          file.tag_title,
                        ]
                          .filter(Boolean)
                          .join(" · ") || "—"}
                      </td>
                      <td>
                        {file.tag_disc && file.tag_track
                          ? `${file.tag_disc}.${file.tag_track}`
                          : (file.tag_track ?? "—")}
                      </td>
                      <td>
                        <Label
                          kind={
                            file.state === "matched"
                              ? "success"
                              : file.state === "unresolved"
                                ? "warning"
                                : file.state === "skipped"
                                  ? "disabled"
                                  : "info"
                          }
                        >
                          {file.state}
                        </Label>
                      </td>
                      <td className="is-actions">
                        {(file.state === "matched" ||
                          file.state === "unresolved") && (
                          <Button
                            size="1"
                            variant="soft"
                            disabled={pending}
                            onClick={() => void skip(file.id)}
                          >
                            Skip
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            <div className="mus-scan-actions">
              <Button
                disabled={pending || selected.length === 0}
                onClick={() => void register()}
              >
                Register selected ({selected.length})
              </Button>
              {(active.data.total ?? 0) > (active.data.limit || 100) && (
                <>
                  <Button
                    variant="soft"
                    color="gray"
                    disabled={page <= 1}
                    onClick={() => setPage(page - 1)}
                  >
                    Previous
                  </Button>
                  <span className="rs-muted">
                    Page {active.data.page ?? page}
                  </span>
                  <Button
                    variant="soft"
                    color="gray"
                    disabled={
                      page * (active.data.limit || 100) >=
                      (active.data.total ?? 0)
                    }
                    onClick={() => setPage(page + 1)}
                  >
                    Next
                  </Button>
                </>
              )}
            </div>
          </FieldSet>
        )}
      </PageBody>
      <Modal
        open={open}
        onOpenChange={setOpen}
        title="Scan a music folder"
        description="Choose a folder below the media root. Files are read in place and never moved."
        footer={
          <Button variant="soft" color="gray" onClick={() => setOpen(false)}>
            Close
          </Button>
        }
      >
        <form onSubmit={(e) => void startScan(e)} className="mus-search">
          <TextField.Root
            aria-label="Music folder"
            value={root}
            onChange={(e) => setRoot(e.target.value)}
            placeholder="Music"
          />
          <Button type="submit" disabled={pending || !root.trim()}>
            Start scan
          </Button>
        </form>
        <p className="rs-form-help">
          Relative to the media root, for example <code>Music</code> or{" "}
          <code>music</code>. Folder names are case-sensitive.
        </p>
        {error && <Alert kind="danger">{error}</Alert>}
      </Modal>
    </>
  );
}
