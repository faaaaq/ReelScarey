import { useEffect, useId, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { ArrowLeft, FolderSearch, RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import {
  Alert,
  EmptyState,
  FieldSet,
  FormActions,
  FormGroup,
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
  APIError,
  api,
  messageFor,
  type AcoustIDStatus,
  type LibraryRootsConfig,
  type MusicDiscoverResult,
  type MusicFingerprintResult,
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

function fingerprintSummary(result: MusicFingerprintResult) {
  const parts = [
    `Fingerprinted ${result.fingerprinted} of ${result.selected} selected files.`,
    result.matched > 0
      ? `Matched ${result.matched} of ${result.selected} selected files.`
      : "No selected files matched an AcoustID recording.",
    result.artists_added || result.albums_added
      ? `Added ${countLabel(result.artists_added, "artist")} and ${countLabel(result.albums_added, "album")} to the catalog.`
      : "No catalog entries were added.",
  ];
  if (result.provider_calls > 0) {
    parts.push(`Made ${countLabel(result.provider_calls, "AcoustID lookup")}.`);
  }
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
      } deferred; run fingerprint matching again to continue.`,
    );
  }
  parts.push(
    result.remaining > 0
      ? `${countLabel(result.remaining, "unresolved file")} remain in this scan.`
      : "No unresolved files remain in this scan.",
  );
  return parts.join(" ");
}

/** Adds recovery guidance to an AcoustID fingerprint error or warning. */
function fingerprintHelp(text: string, status = 0) {
  const message = text.toLowerCase();
  if (
    message.includes("not configured") ||
    message.includes("missing key") ||
    message.includes("no acoustid key")
  ) {
    return "Save an AcoustID application API key under Configure AcoustID before fingerprinting.";
  }
  if (message.includes("fpcalc") || message.includes("chromaprint")) {
    return "fpcalc (Chromaprint) is not available to the server. Install it in the ReelScarey container and restart, then retry.";
  }
  if (
    status === 429 ||
    message.includes("rate limit") ||
    message.includes("too many")
  ) {
    return "AcoustID is rate limiting lookups. Wait for the retry window, then fingerprint fewer files.";
  }
  if (
    status === 403 ||
    message.includes("rejected the") ||
    message.includes("invalid application") ||
    message.includes("invalid api key") ||
    message.includes("invalid key")
  ) {
    return "Create or copy a valid application key at acoustid.org/new-application, then save it under Configure AcoustID.";
  }
  if (message.includes("ambiguous")) {
    return "AcoustID returned more than one plausible recording, so the file stays unresolved. Review its tags, then try Discover missing catalog.";
  }
  if (
    message.includes("no match") ||
    message.includes("not found") ||
    message.includes("unrecognized")
  ) {
    return "No AcoustID match was found for that audio. Check that the file is a complete track, or continue with tag-based discovery.";
  }
  return "";
}

function fingerprintErrorHelp(error: unknown) {
  return fingerprintHelp(
    messageFor(error),
    error instanceof APIError ? error.status : 0,
  );
}

export function MusicImport() {
  const scans = useLiveResource<MusicScanList>("/library/music/scans", 5000);
  const rootsResource = useResource<LibraryRootsConfig>("/library/roots");
  const acoustid = useResource<AcoustIDStatus>("/library/music/acoustid");
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
  const [acoustidOpen, setAcoustidOpen] = useState(false);
  const [acoustidKey, setAcoustidKey] = useState("");
  const [acoustidPending, setAcoustidPending] = useState(false);
  const [acoustidError, setAcoustidError] = useState("");
  const [acoustidNotice, setAcoustidNotice] = useState("");
  const [fingerprintOpen, setFingerprintOpen] = useState(false);
  const [fingerprintResult, setFingerprintResult] =
    useState<MusicFingerprintResult | null>(null);
  const [fingerprintError, setFingerprintError] = useState("");
  const [error, setError] = useState("");
  const acoustidKeyID = useId();
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
  const acoustidReady =
    Boolean(acoustid.data?.configured) &&
    Boolean(acoustid.data?.fingerprint_available);
  const fingerprintBlockedReason = acoustid.error
    ? "AcoustID status is unavailable. Retry the status check before fingerprinting."
    : !acoustid.data
      ? "Checking the saved AcoustID key and server fingerprint tool…"
      : !acoustid.data.configured
        ? "AcoustID has no application key. Open Configure AcoustID to save one."
        : !acoustid.data.fingerprint_available
          ? "fpcalc (Chromaprint) is not available on the server, so audio cannot be fingerprinted."
          : "";

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
    setFingerprintOpen(false);
    setFingerprintResult(null);
    setFingerprintError("");
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

  function openAcoustID() {
    setAcoustidError("");
    setAcoustidNotice("");
    setAcoustidOpen(true);
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

  async function saveAcoustID(e: FormEvent) {
    e.preventDefault();
    const key = acoustidKey.trim();
    if (acoustidPending || !key) return;
    setAcoustidPending(true);
    setAcoustidError("");
    setAcoustidNotice("");
    try {
      await api<AcoustIDStatus>("/library/music/acoustid", {
        method: "PUT",
        body: JSON.stringify({ api_key: key }),
      });
      setAcoustidKey("");
      setAcoustidNotice("AcoustID application key saved.");
      acoustid.reload();
    } catch (e) {
      setAcoustidError(messageFor(e));
    } finally {
      setAcoustidPending(false);
    }
  }

  async function removeAcoustID() {
    if (acoustidPending) return;
    setAcoustidPending(true);
    setAcoustidError("");
    setAcoustidNotice("");
    try {
      await api<AcoustIDStatus>("/library/music/acoustid", {
        method: "DELETE",
      });
      setAcoustidKey("");
      setAcoustidNotice(
        "AcoustID application key removed. Fingerprint matching is disabled until a key is saved.",
      );
      acoustid.reload();
    } catch (e) {
      setAcoustidError(messageFor(e));
    } finally {
      setAcoustidPending(false);
    }
  }

  async function fingerprint() {
    if (!active.data || selectedUnresolved.length === 0 || pending) return;
    if (!acoustidReady) {
      setFingerprintError(fingerprintBlockedReason);
      return;
    }
    setPending(true);
    setFingerprintError("");
    try {
      const result = await api<MusicFingerprintResult>(
        `/library/music/scans/${active.data.scan.id}/fingerprint`,
        {
          method: "POST",
          body: JSON.stringify({
            file_ids: selectedUnresolved.map((file) => file.id),
            reviewed: true,
          }),
          signal: AbortSignal.timeout(180000),
        },
      );
      setFingerprintOpen(false);
      setFingerprintResult(result);
      setSelected([]);
      active.reload();
      scans.reload();
    } catch (e) {
      const help = fingerprintErrorHelp(e);
      setFingerprintError(help ? `${messageFor(e)} ${help}` : messageFor(e));
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
        {acoustid.loading && !acoustid.data && (
          <Alert kind="info" title="AcoustID fingerprint matching">
            Checking the saved AcoustID key and server fingerprint tool…
          </Alert>
        )}
        {acoustid.error && (
          <Alert kind="danger" title="AcoustID status unavailable">
            {acoustid.error} Fingerprint matching stays disabled until the
            status can be read.{" "}
            <Button size="1" variant="soft" onClick={acoustid.reload}>
              Retry status
            </Button>{" "}
            <Button size="1" variant="soft" onClick={openAcoustID}>
              Configure AcoustID
            </Button>
          </Alert>
        )}
        {acoustid.data && !acoustid.error && (
          <Alert
            kind={
              acoustid.data.configured && acoustid.data.fingerprint_available
                ? "info"
                : "warning"
            }
            title="AcoustID fingerprint matching"
          >
            <div className="mus-acoustid">
              <Label kind={acoustid.data.configured ? "success" : "disabled"}>
                API key{" "}
                {acoustid.data.configured ? "configured" : "not configured"}
              </Label>
              <Label
                kind={
                  acoustid.data.fingerprint_available ? "success" : "disabled"
                }
              >
                fpcalc{" "}
                {acoustid.data.fingerprint_available
                  ? "available"
                  : "unavailable"}
              </Label>
              {acoustid.data.message && (
                <span className="rs-muted">{acoustid.data.message}</span>
              )}
              <Button size="1" variant="soft" onClick={openAcoustID}>
                Configure AcoustID
              </Button>
            </div>
          </Alert>
        )}
        <Alert kind="info" title="Scan, then fill catalog gaps">
          Scan the configured roots first. Select unresolved files and use{" "}
          <strong>Discover missing catalog</strong> to search MusicBrainz using
          tagged MusicBrainz IDs first. Without those IDs it requires one exact
          normalized artist name and album title. Files with ambiguous or
          missing evidence stay unresolved and are never guessed. When tags are
          unusable, select unresolved files and use{" "}
          <strong>Match by audio fingerprint</strong> if an AcoustID key and
          fpcalc are available. Roots are relative to the media root and names
          are case-sensitive.
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
            {fingerprintResult && (
              <Alert
                kind={
                  fingerprintResult.warnings.length ||
                  fingerprintResult.matched === 0
                    ? "warning"
                    : "success"
                }
                title="Audio fingerprint matching finished"
              >
                {fingerprintSummary(fingerprintResult)}
                {fingerprintResult.warnings.map((warning) => {
                  const help = fingerprintHelp(warning);
                  return (
                    <span key={warning} className="rs-muted">
                      {" "}
                      {warning}
                      {help ? ` ${help}` : ""}
                    </span>
                  );
                })}
              </Alert>
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
              <Button
                variant="soft"
                disabled={
                  pending || selectedUnresolved.length === 0 || !acoustidReady
                }
                title={
                  selectedUnresolved.length === 0
                    ? "Select unresolved files first."
                    : fingerprintBlockedReason || undefined
                }
                onClick={() => {
                  setFingerprintError("");
                  setFingerprintOpen(true);
                }}
              >
                Match by audio fingerprint ({selectedUnresolved.length})
              </Button>
              {selectedUnresolved.length > 0 && !acoustidReady && (
                <span className="rs-muted" role="status">
                  {fingerprintBlockedReason}
                </span>
              )}
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
      <Modal
        open={acoustidOpen}
        onOpenChange={setAcoustidOpen}
        title="Configure AcoustID"
        description="Audio fingerprint matching uses a Chromaprint fingerprint and an AcoustID application API key."
      >
        <form onSubmit={(e) => void saveAcoustID(e)} className="rs-stack">
          <FormGroup
            label="AcoustID application API key"
            htmlFor={acoustidKeyID}
            help={
              <>
                Create an application at{" "}
                <a
                  href="https://acoustid.org/new-application"
                  target="_blank"
                  rel="noreferrer"
                >
                  acoustid.org/new-application
                </a>
                . The key is stored encrypted and never returned to this
                browser.{" "}
                {acoustid.data?.configured
                  ? "A saved key is retained; enter a new key only to replace it."
                  : "Save a key before fingerprint matching can run."}
              </>
            }
          >
            <TextField.Root
              id={acoustidKeyID}
              name="acoustid-api-key"
              type="password"
              autoComplete="off"
              value={acoustidKey}
              onChange={(event) => setAcoustidKey(event.target.value)}
              placeholder={
                acoustid.data?.configured
                  ? "Saved key retained; enter a replacement"
                  : "Application API key"
              }
              maxLength={2048}
              disabled={acoustidPending}
            />
          </FormGroup>
          <FormActions>
            <Button
              type="submit"
              disabled={acoustidPending || !acoustidKey.trim()}
            >
              Save key
            </Button>
            {acoustid.data?.configured && (
              <Button
                type="button"
                variant="soft"
                color="red"
                disabled={acoustidPending}
                onClick={() => void removeAcoustID()}
              >
                Remove key
              </Button>
            )}
          </FormActions>
        </form>
        <p className="rs-form-help">
          If fpcalc (Chromaprint) is missing, install it in the ReelScarey
          container and restart; the status panel reports whether it is
          available.
        </p>
        {acoustidError && <Alert kind="danger">{acoustidError}</Alert>}
        {acoustidNotice && <Alert kind="success">{acoustidNotice}</Alert>}
      </Modal>
      <Modal
        open={fingerprintOpen}
        onOpenChange={setFingerprintOpen}
        title="Match unresolved files by audio fingerprint"
        description="Review this AcoustID lookup before ReelScarey computes fingerprints and adds catalog entries."
        footer={
          <>
            <Button
              variant="soft"
              color="gray"
              disabled={pending}
              onClick={() => setFingerprintOpen(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={
                pending || !acoustidReady || selectedUnresolved.length === 0
              }
              onClick={() => void fingerprint()}
            >
              Fingerprint selected ({selectedUnresolved.length})
            </Button>
          </>
        }
      >
        <p>
          ReelScarey will compute an audio fingerprint for{" "}
          {countLabel(selectedUnresolved.length, "selected unresolved file")}{" "}
          with fpcalc and look it up through AcoustID. Fingerprinting identifies
          audio that has no usable tags and does not replace tag-based catalog
          discovery.
        </p>
        <Alert kind="warning">
          This action does not move, tag or register files. It changes only the
          catalog and the scan's match state. Audio with no confident AcoustID
          match stays unresolved.
        </Alert>
        {fingerprintBlockedReason && (
          <Alert kind="warning">{fingerprintBlockedReason}</Alert>
        )}
        <p className="rs-form-help">
          A request is bounded. If files are deferred, select the remaining
          unresolved files and run fingerprint matching again.
        </p>
        {fingerprintError && <Alert kind="danger">{fingerprintError}</Alert>}
      </Modal>
    </>
  );
}
