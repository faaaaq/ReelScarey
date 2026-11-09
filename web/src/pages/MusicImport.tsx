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
  type AcoustIDStatus,
  type LibraryRootsConfig,
  type MusicDiscoverResult,
  type MusicFingerprintResult,
  type MusicBrainzStatus,
  type MusicScan,
  type MusicScanList,
  type MusicScanPage,
  type MusicScanPhase,
} from "../api";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";
import "./Music.css";

function countLabel(count: number, singular: string) {
  return `${count} ${singular}${count === 1 ? "" : "s"}`;
}

const activePhases: ReadonlySet<MusicScanPhase> = new Set([
  "queued",
  "scanning",
  "matching",
  "fingerprinting",
  "organizing",
]);

const phaseLabels: Record<MusicScanPhase, string> = {
  queued: "Queued",
  scanning: "Scanning files",
  matching: "Matching MusicBrainz",
  fingerprinting: "Fingerprinting with AcoustID",
  ready: "Ready",
  organizing: "Organizing files",
  completed: "Completed",
  failed: "Needs attention",
  cancelled: "Cancelled",
};

/** Older servers only report `state`; map it onto the workflow phases. */
function scanPhase(scan: MusicScan): MusicScanPhase {
  if (scan.phase) return scan.phase;
  switch (scan.state) {
    case "scanning":
      return "scanning";
    case "completed":
      return "completed";
    case "failed":
      return "failed";
    case "cancelled":
      return "cancelled";
    default:
      return "ready";
  }
}

function isActivePhase(phase: MusicScanPhase) {
  return activePhases.has(phase);
}

function phaseDetail(phase: MusicScanPhase) {
  switch (phase) {
    case "queued":
      return "Waiting for the scan worker to start.";
    case "scanning":
      return "Reading tags, durations and hashes from the selected folders.";
    case "matching":
      return "Matching tagged files to MusicBrainz releases already in the catalog.";
    case "fingerprinting":
      return "Identifying files whose tags were not enough with Chromaprint and AcoustID.";
    case "ready":
      return "Review the files below, or run the automatic actions.";
    case "organizing":
      return "Registering matched files in place, then renaming, tagging and organizing them.";
    case "completed":
      return "The workflow finished. Registered files stay in place and are available in the library.";
    case "failed":
      return "The workflow stopped with an error. Review the message and retry the step.";
    case "cancelled":
      return "The workflow was cancelled. Files already registered are unchanged.";
  }
}

/** Detects a scan message that says AcoustID matching was skipped. */
function mentionsAcoustidSkip(message: string) {
  const text = message.toLowerCase();
  const skipped =
    text.includes("skip") ||
    text.includes("unavailable") ||
    text.includes("not configured") ||
    text.includes("no application key") ||
    text.includes("disabled");
  return (text.includes("acoustid") || text.includes("fingerprint")) && skipped;
}

/** Detects a scan message that says MusicBrainz paused matching. */
function mentionsMusicBrainzCooldown(message: string) {
  const text = message.toLowerCase();
  return (
    text.includes("musicbrainz") &&
    (text.includes("limiting requests") ||
      text.includes("paused until") ||
      text.includes("paused matching") ||
      text.includes("rate limit") ||
      text.includes("cooling down"))
  );
}

function cooldownLabel(until: number) {
  if (!until) return "";
  const date = new Date(until * 1000);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString();
}

function phaseKind(phase: MusicScanPhase) {
  if (phase === "failed") return "danger" as const;
  if (phase === "completed" || phase === "ready") return "success" as const;
  if (phase === "fingerprinting") return "purple" as const;
  return "primary" as const;
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
  const musicbrainz = useResource<MusicBrainzStatus>(
    "/library/music/musicbrainz",
  );
  const [scanID, setScanID] = useState<number | null>(null);
  const [page, setPage] = useState(1);
  const active = useLiveResource<MusicScanPage>(
    scanID ? `/library/music/scans/${scanID}?page=${page}` : null,
    2500,
  );
  const [open, setOpen] = useState(false);
  const [roots, setRoots] = useState<string[]>([]);
  const [fullScan, setFullScan] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);
  const [pending, setPending] = useState(false);
  const [workflowBusy, setWorkflowBusy] = useState(false);
  const [discoverOpen, setDiscoverOpen] = useState(false);
  const [discoverResult, setDiscoverResult] =
    useState<MusicDiscoverResult | null>(null);
  const [matchAllOpen, setMatchAllOpen] = useState(false);
  const [finalizeOpen, setFinalizeOpen] = useState(false);
  const [workflowNotice, setWorkflowNotice] = useState("");
  const [acoustidOpen, setAcoustidOpen] = useState(false);
  const [acoustidKey, setAcoustidKey] = useState("");
  const [acoustidPending, setAcoustidPending] = useState(false);
  const [acoustidError, setAcoustidError] = useState("");
  const [acoustidNotice, setAcoustidNotice] = useState("");
  const [musicbrainzOpen, setMusicbrainzOpen] = useState(false);
  const [musicbrainzBaseURL, setMusicbrainzBaseURL] = useState("");
  const [musicbrainzPending, setMusicbrainzPending] = useState(false);
  const [musicbrainzError, setMusicbrainzError] = useState("");
  const [musicbrainzNotice, setMusicbrainzNotice] = useState("");
  const [fingerprintOpen, setFingerprintOpen] = useState(false);
  const [fingerprintResult, setFingerprintResult] =
    useState<MusicFingerprintResult | null>(null);
  const [fingerprintError, setFingerprintError] = useState("");
  const [error, setError] = useState("");
  const acoustidKeyID = useId();
  const musicbrainzURLID = useId();
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
  const musicbrainzCooldown =
    (musicbrainz.data?.cooldown_until ?? 0) > Date.now() / 1000;
  const musicbrainzCooldownText = musicbrainzCooldown
    ? cooldownLabel(musicbrainz.data?.cooldown_until ?? 0)
    : "";
  const fingerprintBlockedReason = acoustid.error
    ? "AcoustID status is unavailable. Retry the status check before fingerprinting."
    : !acoustid.data
      ? "Checking the saved AcoustID key and server fingerprint tool…"
      : !acoustid.data.configured
        ? "AcoustID has no application key. Open Configure AcoustID to save one."
        : !acoustid.data.fingerprint_available
          ? "fpcalc (Chromaprint) is not available on the server, so audio cannot be fingerprinted."
          : "";
  const scan = active.data?.scan;
  const phase = scan ? scanPhase(scan) : "ready";
  const phaseActive = isActivePhase(phase);
  const skippedCount = Math.max(0, scan?.skipped_count ?? 0);
  const unresolvedCount = Math.max(0, scan?.unresolved_count ?? 0);
  const matchedCount = Math.max(0, scan?.matched_count ?? 0);
  const registeredCount = Math.max(0, scan?.registered_count ?? 0);
  const fileCount = Math.max(0, scan?.file_count ?? 0);
  const progressTotal = Math.max(
    Math.max(0, scan?.progress_total ?? 0),
    phase === "organizing" ? matchedCount + registeredCount : 0,
    phase === "matching" || phase === "fingerprinting" ? unresolvedCount : 0,
    phase === "queued"
      ? Math.max(matchedCount, registeredCount, unresolvedCount)
      : 0,
  );
  const progressDone = Math.min(
    Math.max(0, scan?.progress_done ?? 0),
    progressTotal,
  );
  const remainingCount = Math.max(
    0,
    fileCount - registeredCount - skippedCount,
  );
  const scanWarningMessage =
    scan && scan.message && mentionsAcoustidSkip(scan.message)
      ? scan.message
      : "";
  const scanCooldownMessage =
    scan && scan.message && mentionsMusicBrainzCooldown(scan.message)
      ? scan.message
      : "";
  const scanWarning = Boolean(
    scan?.error ||
    (scanWarningMessage && mentionsAcoustidSkip(scanWarningMessage)) ||
    scanCooldownMessage,
  );
  const canMatchAll = phase === "ready" && unresolvedCount > 0;
  const canFinalize = phase === "ready" && matchedCount > 0;
  const hasAvailableRoots = musicRoots.some((root) => root.available);

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
    setMatchAllOpen(false);
    setFinalizeOpen(false);
    setWorkflowNotice("");
    setWorkflowBusy(false);
    setFingerprintOpen(false);
    setFingerprintResult(null);
    setFingerprintError("");
  }, [scanID, page]);

  // Keep one-click actions disabled until the accepted workflow leaves `ready`
  // (or a short grace period passes if the polled scan never shows the phase).
  useEffect(() => {
    if (!workflowBusy) return;
    if (phase !== "ready") {
      setWorkflowBusy(false);
      return;
    }
    const timer = window.setTimeout(() => setWorkflowBusy(false), 5000);
    return () => window.clearTimeout(timer);
  }, [workflowBusy, phase, active.updatedAt]);

  async function startScan(e: FormEvent) {
    e.preventDefault();
    if (pending || roots.length === 0) return;
    setPending(true);
    setError("");
    try {
      const scan = await api<MusicScan>("/library/music/scans", {
        method: "POST",
        body: JSON.stringify({ roots, full: fullScan }),
      });
      setOpen(false);
      setFullScan(false);
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

  function openMusicBrainz() {
    setMusicbrainzBaseURL(
      musicbrainz.data?.using_default ? "" : (musicbrainz.data?.base_url ?? ""),
    );
    setMusicbrainzError("");
    setMusicbrainzNotice("");
    setMusicbrainzOpen(true);
  }

  async function saveMusicBrainz(e: FormEvent) {
    e.preventDefault();
    const baseURL = musicbrainzBaseURL.trim();
    if (musicbrainzPending || !baseURL) return;
    setMusicbrainzPending(true);
    setMusicbrainzError("");
    setMusicbrainzNotice("");
    try {
      await api<MusicBrainzStatus>("/library/music/musicbrainz", {
        method: "PUT",
        body: JSON.stringify({ base_url: baseURL }),
      });
      setMusicbrainzNotice("MusicBrainz base URL saved.");
      musicbrainz.reload();
    } catch (e) {
      setMusicbrainzError(messageFor(e));
    } finally {
      setMusicbrainzPending(false);
    }
  }

  async function resetMusicBrainz() {
    if (musicbrainzPending) return;
    setMusicbrainzPending(true);
    setMusicbrainzError("");
    setMusicbrainzNotice("");
    try {
      await api<MusicBrainzStatus>("/library/music/musicbrainz", {
        method: "DELETE",
      });
      setMusicbrainzBaseURL("");
      setMusicbrainzNotice("MusicBrainz base URL reset to the public server.");
      musicbrainz.reload();
    } catch (e) {
      setMusicbrainzError(messageFor(e));
    } finally {
      setMusicbrainzPending(false);
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

  async function matchAll() {
    if (!active.data || pending || workflowBusy || !canMatchAll) return;
    setPending(true);
    setWorkflowBusy(true);
    setError("");
    try {
      await api<MusicScan>(
        `/library/music/scans/${active.data.scan.id}/match-all`,
        {
          method: "POST",
          body: JSON.stringify({ reviewed: true }),
          signal: AbortSignal.timeout(30000),
        },
      );
      setMatchAllOpen(false);
      setWorkflowNotice(
        "Matching all unmatched files started. MusicBrainz matching, AcoustID fingerprinting and current work are shown below while the scan runs.",
      );
      active.reload();
      scans.reload();
    } catch (e) {
      setWorkflowBusy(false);
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function finalize() {
    if (!active.data || pending || workflowBusy || !canFinalize) return;
    setPending(true);
    setWorkflowBusy(true);
    setError("");
    try {
      await api<MusicScan>(
        `/library/music/scans/${active.data.scan.id}/finalize`,
        {
          method: "POST",
          body: JSON.stringify({ reviewed: true }),
          signal: AbortSignal.timeout(30000),
        },
      );
      setFinalizeOpen(false);
      setWorkflowNotice(
        "Finalizing matched files started. Registration, tagging and organization progress are shown below while the scan runs.",
      );
      active.reload();
      scans.reload();
    } catch (e) {
      setWorkflowBusy(false);
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
          description="Scan music folders, match files to the catalog with MusicBrainz tags and AcoustID fingerprints, then register and organize matched files with explicit review. Scan reads files in place."
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
            dismissKey="music-import-acoustid"
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
        {musicbrainz.error && (
          <Alert kind="danger" title="MusicBrainz status unavailable">
            {musicbrainz.error} Matching still uses the saved provider, but the
            cache and cooldown state cannot be read.{" "}
            <Button size="1" variant="soft" onClick={musicbrainz.reload}>
              Retry status
            </Button>{" "}
            <Button size="1" variant="soft" onClick={openMusicBrainz}>
              Configure MusicBrainz
            </Button>
          </Alert>
        )}
        {musicbrainz.data && !musicbrainz.error && (
          <Alert
            kind={musicbrainzCooldown ? "warning" : "info"}
            title="MusicBrainz provider and cache"
            dismissKey="music-import-musicbrainz"
          >
            <div className="mus-acoustid">
              <Label kind={musicbrainz.data.using_default ? "info" : "success"}>
                {musicbrainz.data.using_default
                  ? "Public server"
                  : "Custom base URL"}
              </Label>
              {musicbrainz.data.using_default ? (
                <span className="rs-muted">
                  {musicbrainz.data.base_url ||
                    musicbrainz.data.default_base_url ||
                    "https://musicbrainz.org/ws/2"}
                </span>
              ) : (
                <span className="rs-muted">
                  {musicbrainz.data.base_url || "Custom MusicBrainz base URL"}
                </span>
              )}
              <Label kind="disabled">
                {musicbrainz.data.cache_entries ?? 0} cached responses
              </Label>
              {musicbrainzCooldown && (
                <Label kind="warning">
                  Paused until {musicbrainzCooldownText}
                </Label>
              )}
              {musicbrainz.data.message && (
                <span className="rs-muted">{musicbrainz.data.message}</span>
              )}
              <Button size="1" variant="soft" onClick={openMusicBrainz}>
                Configure MusicBrainz
              </Button>
            </div>
          </Alert>
        )}
        <Alert
          kind="info"
          title="Scan, match, then finalize"
          dismissKey="music-import-guide"
        >
          Start a scan of the configured roots. When it is ready, use{" "}
          <strong>Match all unmatched</strong> to match every remaining file
          with MusicBrainz tags and, when needed, AcoustID fingerprinting. Then
          use <strong>Finalize matched files</strong> to register them in place
          and rename, tag and organize them with the existing no-overwrite
          safety. Use the manual actions for individual files when automatic
          matching is not appropriate. Files that cannot be proven stay
          unresolved; nothing is guessed. Roots are relative to the media root
          and names are case-sensitive.
        </Alert>
        {musicRoots.length > 0 && (
          <Alert
            kind="info"
            title="Configured music roots"
            dismissKey="music-import-roots"
          >
            {musicRoots
              .map(
                (root) =>
                  `${root.path}${root.available ? "" : " (unavailable)"}`,
              )
              .join(" · ")}{" "}
            <Link to="/settings/media">Manage library roots</Link>
          </Alert>
        )}
        {rootsResource.data && musicRoots.length === 0 && (
          <Alert kind="warning" title="No music root is available">
            Add an existing music folder below the media root in{" "}
            <Link to="/settings/media">
              Settings → Media Management → Library roots
            </Link>{" "}
            before scanning.
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
            actions={
              hasAvailableRoots ? (
                <Button onClick={() => setOpen(true)}>Scan folder</Button>
              ) : (
                <Button asChild>
                  <Link to="/settings/media">Add media folder</Link>
                </Button>
              )
            }
          >
            {hasAvailableRoots
              ? "Choose a music folder below the media root."
              : "Add a music folder in Settings → Media Management → Library roots first."}
          </EmptyState>
        )}
        {error && <Alert kind="danger">{error}</Alert>}
        {active.data?.scan && scan && (
          <FieldSet
            legend={`Scan #${scan.id} · ${scan.root}${scan.full_scan ? " · full rescan" : ""}${scan.request === "finalize" ? " · finalize" : scan.request === "match" ? " · matching" : ""}`}
            actions={
              phaseActive ? (
                <Button
                  size="1"
                  variant="soft"
                  color="gray"
                  disabled={pending}
                  onClick={() => void cancel()}
                >
                  {phase === "scanning" ? "Cancel scan" : "Cancel workflow"}
                </Button>
              ) : undefined
            }
          >
            <div className="mus-scan-summary">
              <Label
                kind={
                  scan.error
                    ? "danger"
                    : scanWarning
                      ? "warning"
                      : phaseKind(phase)
                }
              >
                {phaseLabels[phase]}
              </Label>
              <span>{countLabel(fileCount, "file")}</span>
              <span>{countLabel(matchedCount, "matched file")}</span>
              <span>{countLabel(unresolvedCount, "unresolved file")}</span>
              <span>{countLabel(registeredCount, "registered file")}</span>
              <span>{countLabel(skippedCount, "skipped file")}</span>
              <span>{countLabel(remainingCount, "remaining file")}</span>
              {scanWarning && (
                <Label kind={scan.error ? "danger" : "warning"}>
                  {scan.error ? "1 error" : "1 warning"}
                </Label>
              )}
            </div>
            <div className="mus-scan-status">
              <p className="mus-scan-phase">
                <strong>{phaseLabels[phase]}</strong>{" "}
                <span className="rs-muted">{phaseDetail(phase)}</span>
              </p>
              {(phaseActive || progressTotal > 0) && (
                <ProgressBar
                  label={`${phaseLabels[phase]} progress`}
                  value={progressTotal > 0 ? progressDone / progressTotal : 0}
                  indeterminate={phaseActive && progressTotal === 0}
                  kind={phaseKind(phase)}
                  text={
                    progressTotal > 0
                      ? `${progressDone} of ${progressTotal} files`
                      : undefined
                  }
                />
              )}
              {phaseActive && (
                <p className="rs-muted" role="status" aria-live="polite">
                  {progressTotal > 0
                    ? `${progressDone} of ${progressTotal} files processed${
                        skippedCount > 0
                          ? ` · ${countLabel(skippedCount, "file")} skipped`
                          : ""
                      }`
                    : phase === "queued"
                      ? "Waiting for the scan worker to start."
                      : `Discovering files… ${countLabel(fileCount, "file")} found so far.`}
                </p>
              )}
              {scan.current_path && (
                <p className="mus-path rs-muted">
                  Current: {scan.current_path}
                </p>
              )}
              {scan.message && !scanWarningMessage && !scanCooldownMessage && (
                <p className="rs-muted">{scan.message}</p>
              )}
            </div>
            {scan.error && (
              <Alert kind="danger" title="Scan error">
                {scan.error}
              </Alert>
            )}
            {scanWarningMessage && mentionsAcoustidSkip(scanWarningMessage) && (
              <Alert kind="warning" title="AcoustID fingerprinting was skipped">
                {scanWarningMessage}
              </Alert>
            )}
            {scanCooldownMessage && (
              <Alert kind="warning" title="MusicBrainz matching is paused">
                {scanCooldownMessage} This is a provider cooldown, not a failed
                scan. Matched files are unchanged; wait for the retry window,
                then run Match all unmatched again.
              </Alert>
            )}
            {!phaseActive && skippedCount > 0 && (
              <Alert
                kind="info"
                title={`${countLabel(skippedCount, "file")} skipped`}
              >
                Files already matched or registered by earlier scans are
                skipped. Run a full rescan to re-examine
                matched-but-unregistered files; registered files are never
                duplicated.
              </Alert>
            )}
            {phase === "ready" && unresolvedCount > 0 && (
              <Alert
                kind="warning"
                title={`${countLabel(unresolvedCount, "file")} still unresolved`}
              >
                {matchedCount > 0
                  ? "Finalize matched files now, then retry automatic or manual matching for the rest."
                  : "Run Match all unmatched, or select files for Discover missing catalog or Match by audio fingerprint."}
              </Alert>
            )}
            {workflowNotice && (
              <Alert kind="info" title="Workflow started">
                {workflowNotice}
              </Alert>
            )}
            {(canMatchAll || canFinalize) && (
              <div className="mus-scan-actions mus-scan-primary">
                {canMatchAll && (
                  <Button
                    disabled={pending || workflowBusy}
                    onClick={() => setMatchAllOpen(true)}
                  >
                    Match all unmatched ({unresolvedCount})
                  </Button>
                )}
                {canFinalize && (
                  <Button
                    disabled={pending || workflowBusy}
                    onClick={() => setFinalizeOpen(true)}
                  >
                    Finalize matched ({matchedCount})
                  </Button>
                )}
              </div>
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
            <p className="rs-form-help mus-manual-help">
              Manual actions apply only to the files you select. Leave the page
              or run an automatic action to work on the whole scan.
            </p>
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
        description="Choose configured folders below the media root. Scanning reads files in place; finalizing a ready scan later registers and organizes matched files."
        footer={
          <Button variant="soft" color="gray" onClick={() => setOpen(false)}>
            Close
          </Button>
        }
      >
        {musicRoots.length === 0 && (
          <Alert kind="warning" title="No music root is configured">
            Add a music folder in Settings → Media Management → Library roots
            before starting a scan.
          </Alert>
        )}
        <form onSubmit={(e) => void startScan(e)} className="mus-scan-form">
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
          <label className="rs-check mus-scan-full">
            <input
              type="checkbox"
              aria-label="Full rescan"
              checked={fullScan}
              disabled={pending}
              onChange={(event) => setFullScan(event.target.checked)}
            />
            <span>
              <strong>Full rescan</strong>
              <small>
                Normal scans skip files already matched or registered in
                previous scans. A full rescan re-examines
                matched-but-unregistered files; registered files are never
                scanned again or duplicated.
              </small>
            </span>
          </label>
          <FormActions>
            <Button type="submit" disabled={pending || roots.length === 0}>
              Start scan
            </Button>
            <Button
              asChild
              variant={musicRoots.length === 0 ? "solid" : "soft"}
            >
              <Link to="/settings/media">Add media folder</Link>
            </Button>
          </FormActions>
        </form>
        <p className="rs-form-help">
          Relative to the media root. Folder names are case-sensitive. Add or
          remove roots in{" "}
          <Link to="/settings/media">
            Settings → Media Management → Library roots
          </Link>
          .
        </p>
        {error && <Alert kind="danger">{error}</Alert>}
      </Modal>
      <Modal
        open={matchAllOpen}
        onOpenChange={setMatchAllOpen}
        title="Match all unmatched files"
        description="Review this automatic MusicBrainz and AcoustID step before it runs."
        footer={
          <>
            <Button
              variant="soft"
              color="gray"
              disabled={pending}
              onClick={() => setMatchAllOpen(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={pending || workflowBusy || !canMatchAll}
              onClick={() => void matchAll()}
            >
              Match all ({unresolvedCount})
            </Button>
          </>
        }
      >
        <p>
          ReelScarey will match the{" "}
          {countLabel(unresolvedCount, "unresolved file")} in this scan. It uses
          the file tags and MusicBrainz first and, when tags are not enough, an
          AcoustID Chromaprint fingerprint.
        </p>
        <Alert kind="warning">
          This may add artists and albums to the catalog. It does not move, tag
          or register files, and audio is never uploaded. Files it cannot prove
          stay unresolved.
        </Alert>
        {!acoustidReady && (
          <Alert kind="warning" title="AcoustID fingerprinting may be skipped">
            {fingerprintBlockedReason} Tag-based MusicBrainz matching still
            runs; any skipped fingerprinting is reported on the scan.
          </Alert>
        )}
        <p className="rs-form-help">
          The scan continues in the background and reports MusicBrainz matching,
          AcoustID fingerprinting, current work and skipped files. Matching
          never changes a file.
        </p>
        {error && <Alert kind="danger">{error}</Alert>}
      </Modal>
      <Modal
        open={finalizeOpen}
        onOpenChange={setFinalizeOpen}
        title="Finalize matched files"
        description="Review registration and organization before ReelScarey changes files on disk."
        footer={
          <>
            <Button
              variant="soft"
              color="gray"
              disabled={pending}
              onClick={() => setFinalizeOpen(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={pending || workflowBusy || !canFinalize}
              onClick={() => void finalize()}
            >
              Finalize matched files ({matchedCount})
            </Button>
          </>
        }
      >
        <p>
          ReelScarey will register the{" "}
          {countLabel(matchedCount, "matched file")} in this scan at their
          current paths, then rename, tag and organize them with the existing
          collection and naming rules.
        </p>
        <Alert kind="warning">
          This changes the catalog and files on disk. Existing destination files
          are never overwritten, and registered files are not duplicated.
          Anything that cannot be published safely stays for review.
        </Alert>
        <p className="rs-form-help">
          The scan continues in the background and shows organizing progress,
          current work and skipped files. Use Organize to review completed edits
          or roll them back.
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
        open={musicbrainzOpen}
        onOpenChange={setMusicbrainzOpen}
        title="Configure MusicBrainz"
        description="Choose the public MusicBrainz service or an optional local mirror. No MusicBrainz mirror is bundled with ReelScarey."
      >
        <form onSubmit={(e) => void saveMusicBrainz(e)} className="rs-stack">
          <FormGroup
            label="MusicBrainz base URL"
            htmlFor={musicbrainzURLID}
            help={
              <>
                Default:{" "}
                {musicbrainz.data?.default_base_url ??
                  "https://musicbrainz.org/ws/2"}
                . The server root is enough; ReelScarey appends /ws/2 when it is
                missing. HTTPS is allowed anywhere. Plain HTTP is accepted only
                for localhost, loopback or a private LAN address.
              </>
            }
          >
            <TextField.Root
              id={musicbrainzURLID}
              name="musicbrainz-base-url"
              type="url"
              autoComplete="off"
              value={musicbrainzBaseURL}
              onChange={(event) => setMusicbrainzBaseURL(event.target.value)}
              placeholder={
                musicbrainz.data?.using_default
                  ? "https://musicbrainz.org (default)"
                  : "https://mirror.example"
              }
              maxLength={2048}
              disabled={musicbrainzPending}
            />
          </FormGroup>
          <FormActions>
            <Button
              type="submit"
              disabled={musicbrainzPending || !musicbrainzBaseURL.trim()}
            >
              Save base URL
            </Button>
            {musicbrainz.data && !musicbrainz.data.using_default && (
              <Button
                type="button"
                variant="soft"
                color="red"
                disabled={musicbrainzPending}
                onClick={() => void resetMusicBrainz()}
              >
                Reset to public server
              </Button>
            )}
          </FormActions>
        </form>
        <p className="rs-form-help">
          Public MusicBrainz is paced at one request per second. A local mirror
          is paced at 50 ms but still honors its own 429/503 Retry-After
          cooldown. Responses are cached locally for 30 days (searches for 7
          days, not-found evidence for 1 day). The cache is evidence, not
          permanent truth. A mirror is optional and is not bundled; see the{" "}
          <a
            href="https://github.com/faaaaq/ReelScarey/blob/main/docs/MUSIC.md"
            target="_blank"
            rel="noreferrer"
          >
            MusicBrainz cache and custom mirror documentation
          </a>
          .
        </p>
        {musicbrainzError && <Alert kind="danger">{musicbrainzError}</Alert>}
        {musicbrainzNotice && <Alert kind="success">{musicbrainzNotice}</Alert>}
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
