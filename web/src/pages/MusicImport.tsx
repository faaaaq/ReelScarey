import { useEffect, useState, type FormEvent } from "react";
import { Button } from "@radix-ui/themes";
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
  type LibraryRootsConfig,
  type MusicDiscoverResult,
  type MusicScan,
  type MusicScanList,
  type MusicScanPage,
} from "../api";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";
import "./Music.css";

function countLabel(count: number, singular: string) {
  return `${count} ${singular}${count === 1 ? "" : "s"}`;
}

function discoverySummary(result: MusicDiscoverResult) {
  const parts = [
    result.matched > 0
      ? `Matched ${result.matched} of ${result.selected} selected files.`
      : `No selected files matched (${result.selected} checked).`,
    result.artists_added || result.albums_added
      ? `Added ${countLabel(result.artists_added, "artist")} and ${countLabel(result.albums_added, "album")} to the catalog.`
      : "No catalog entries were added.",
  ];
  if (result.unresolved > 0) {
    parts.push(
      `${countLabel(result.unresolved, "selected file")} ${
        result.unresolved === 1 ? "is" : "are"
      } still unresolved.`,
    );
  }
  if (result.deferred > 0) {
    parts.push(
      `${countLabel(result.deferred, "selected file")} ${
        result.deferred === 1 ? "was" : "were"
      } deferred; run discovery again to continue.`,
    );
  }
  parts.push(
    result.remaining > 0
      ? `${countLabel(result.remaining, "unresolved file")} remain in this scan.`
      : "No unresolved files remain in this scan.",
  );
  return parts.join(" ");
}

export function MusicImport() {
  const scans = useLiveResource<MusicScanList>("/library/music/scans", 5000);
  const rootsResource = useResource<LibraryRootsConfig>("/library/roots");
  const [scanID, setScanID] = useState<number | null>(null);
  const [page, setPage] = useState(1);
  const active = useLiveResource<MusicScanPage>(
    scanID ? `/library/music/scans/${scanID}?page=${page}` : null,
    3000,
  );
  const [open, setOpen] = useState(false);
  const [roots, setRoots] = useState<string[]>([]);
  const [selected, setSelected] = useState<number[]>([]);
  const [pending, setPending] = useState(false);
  const [discoverOpen, setDiscoverOpen] = useState(false);
  const [discoverResult, setDiscoverResult] =
    useState<MusicDiscoverResult | null>(null);
  const [error, setError] = useState("");
  const musicRoots =
    rootsResource.data?.libraries?.find((library) => library.key === "music")
      ?.roots ?? [];

  const items = active.data?.items ?? [];
  const selectedFiles = items.filter((file) => selected.includes(file.id));
  const selectedMatched = selectedFiles.filter(
    (file) => file.state === "matched",
  );
  const selectedUnresolved = selectedFiles.filter(
    (file) => file.state === "unresolved",
  );

  useEffect(() => {
    if (scanID === null && scans.data?.items.length) {
      setScanID(scans.data.items[0].id);
    }
  }, [scanID, scans.data]);

  useEffect(() => {
    setRoots(
      musicRoots.filter((root) => root.available).map((root) => root.path),
    );
  }, [rootsResource.data]);

  useEffect(() => {
    setSelected([]);
    setDiscoverOpen(false);
    setDiscoverResult(null);
  }, [scanID, page]);

  async function startScan(e: FormEvent) {
    e.preventDefault();
    if (pending || roots.length === 0) return;
    setPending(true);
    setError("");
    try {
      const scan = await api<MusicScan>("/library/music/scans", {
        method: "POST",
        body: JSON.stringify({ roots }),
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
    if (!active.data || selectedMatched.length === 0 || pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/library/music/scans/${active.data.scan.id}/register`, {
        method: "POST",
        body: JSON.stringify({
          file_ids: selectedMatched.map((file) => file.id),
          reviewed: true,
        }),
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

  async function discover() {
    if (!active.data || selectedUnresolved.length === 0 || pending) return;
    setPending(true);
    setError("");
    try {
      const result = await api<MusicDiscoverResult>(
        `/library/music/scans/${active.data.scan.id}/discover`,
        {
          method: "POST",
          body: JSON.stringify({
            file_ids: selectedUnresolved.map((file) => file.id),
            reviewed: true,
          }),
          signal: AbortSignal.timeout(60000),
        },
      );
      setDiscoverOpen(false);
      setDiscoverResult(result);
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
        <Alert kind="info" title="Scan, then fill catalog gaps">
          Scan the folder first. Select unresolved files and use{" "}
          <strong>Discover missing catalog</strong> to search MusicBrainz for
          exact artist and album matches. Strong evidence only: a release-group
          ID, or one exact normalized artist name and album title. Files with
          ambiguous or missing evidence stay unresolved and are never guessed.
          Roots are relative to the media root and names are case-sensitive.
        </Alert>
        {musicRoots.length > 0 && (
          <Alert kind="info" title="Configured music roots">
            {musicRoots
              .map(
                (root) =>
                  `${root.path}${root.available ? "" : " (unavailable)"}`,
              )
              .join(" · ")}{" "}
            <Link to="/settings">Manage library roots</Link>
          </Alert>
        )}
        {rootsResource.data && musicRoots.length === 0 && (
          <Alert kind="warning" title="No music root is available">
            Add an existing music folder below the media root in{" "}
            <Link to="/settings">Library roots</Link> before scanning.
          </Alert>
        )}
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
            {discoverResult && (
              <Alert
                kind={
                  discoverResult.warnings.length
                    ? "warning"
                    : discoverResult.matched
                      ? "success"
                      : "info"
                }
                title="Catalog discovery finished"
              >
                {discoverySummary(discoverResult)}
                {discoverResult.warnings.map((warning) => (
                  <span key={warning} className="rs-muted">
                    {" "}
                    {warning}
                  </span>
                ))}
              </Alert>
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
                        {(file.state === "matched" ||
                          file.state === "unresolved") && (
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
                disabled={pending || selectedMatched.length === 0}
                onClick={() => void register()}
              >
                Register selected ({selectedMatched.length})
              </Button>
              <Button
                variant="soft"
                disabled={pending || selectedUnresolved.length === 0}
                onClick={() => setDiscoverOpen(true)}
              >
                Discover missing catalog ({selectedUnresolved.length})
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
        title="Scan music folders"
        description="Choose configured folders below the media root. Files are read in place and never moved."
        footer={
          <Button variant="soft" color="gray" onClick={() => setOpen(false)}>
            Close
          </Button>
        }
      >
        <form onSubmit={(e) => void startScan(e)} className="mus-search">
          <fieldset className="mus-scan-roots">
            {musicRoots.map((root) => (
              <label className="rs-check" key={root.path}>
                <input
                  type="checkbox"
                  aria-label={`Music root ${root.path}`}
                  checked={roots.includes(root.path)}
                  disabled={pending || !root.available}
                  onChange={(event) =>
                    setRoots(
                      event.target.checked
                        ? [...roots, root.path]
                        : roots.filter((path) => path !== root.path),
                    )
                  }
                />
                <span>
                  <strong>{root.path}</strong>
                  {!root.available && (
                    <small>{root.message || "Folder unavailable."}</small>
                  )}
                </span>
              </label>
            ))}
          </fieldset>
          <Button type="submit" disabled={pending || roots.length === 0}>
            Start scan
          </Button>
        </form>
        <p className="rs-form-help">
          Relative to the media root. Folder names are case-sensitive. Add or
          remove roots in <Link to="/settings">Library roots</Link>.
        </p>
        {error && <Alert kind="danger">{error}</Alert>}
      </Modal>
      <Modal
        open={discoverOpen}
        onOpenChange={setDiscoverOpen}
        title="Discover missing artists and albums"
        description="Review the exact-evidence MusicBrainz search before it adds catalog entries."
        footer={
          <>
            <Button
              variant="soft"
              color="gray"
              disabled={pending}
              onClick={() => setDiscoverOpen(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={pending || selectedUnresolved.length === 0}
              onClick={() => void discover()}
            >
              Discover catalog ({selectedUnresolved.length})
            </Button>
          </>
        }
      >
        <p>
          ReelScarey will search MusicBrainz for{" "}
          {countLabel(selectedUnresolved.length, "selected unresolved file")}.
          It only adds an artist or album when the file carries a release-group
          ID, or when there is one exact normalized artist name and album title.
          Ambiguous or missing evidence stays unresolved.
        </p>
        <Alert kind="warning">
          This action does not move, tag or register files. It changes only the
          catalog and the scan's match state.
        </Alert>
        <p className="rs-form-help">
          A request is bounded to a small number of albums. If files are
          deferred, select the remaining unresolved files and run discovery
          again.
        </p>
        {error && <Alert kind="danger">{error}</Alert>}
      </Modal>
    </>
  );
}
