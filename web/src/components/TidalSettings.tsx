import { useEffect, useState } from "react";
import { Button } from "@radix-ui/themes";
import { RefreshCw } from "lucide-react";
import { Alert, FieldSet } from "../ui";
import { RequestState } from "../components";
import {
  api,
  messageFor,
  type MusicDownloadSource,
  type TidalLoginState,
  type TidalQuality,
  type TidalStatus,
} from "../api";
import { useResource } from "../useResource";

const TIDAL_QUALITY_LABELS: Record<string, string> = {
  HI_RES_LOSSLESS: "TIDAL MAX (best available HiRes FLAC)",
  LOSSLESS: "Lossless (FLAC, 16-bit / 44.1 kHz)",
  HIGH: "High (AAC, 320 kbps)",
  LOW: "Low (AAC, 96 kbps)",
};

function verificationLabel(status: TidalStatus) {
  if (status.verification_state === "verified" && status.logged_in) {
    return "Verified with TIDAL";
  }
  if (status.verification_state === "failed") return "Verification failed";
  if (status.verification_state === "unavailable") return "Unavailable";
  return "Not checked — no verified session";
}

function DeviceInstructions({ state }: { state: TidalLoginState }) {
  const lines = state.lines ?? [];
  if (lines.length === 0) {
    return <p>Waiting for TIDAL to provide a device link…</p>;
  }
  return lines.map((line, index) => (
    <p key={index}>
      {line.split(/(https:\/\/[^\s]+)/).map((part, partIndex) =>
        part.startsWith("https://") ? (
          <a key={partIndex} href={part} target="_blank" rel="noreferrer">
            {part}
          </a>
        ) : (
          part
        ),
      )}
    </p>
  ));
}

export function TidalSection({
  downloadSource,
  quality,
  preferencesReady,
  saving,
  onDownloadSourceChange,
  onQualityChange,
  onSave,
}: {
  downloadSource: MusicDownloadSource;
  quality: TidalQuality;
  preferencesReady: boolean;
  saving: boolean;
  onDownloadSourceChange: (value: MusicDownloadSource) => void;
  onQualityChange: (value: TidalQuality) => void;
  onSave: () => Promise<void>;
}) {
  const status = useResource<TidalStatus>("/library/music/tidal/status", {
    retain: true,
    timeoutMs: 120000,
  });
  const [loginPath, setLoginPath] = useState<string | null>(null);
  const login = useResource<TidalLoginState>(loginPath, { retain: true });
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState("");

  const flow = login.data ?? status.data?.login;
  const phase = flow?.phase;
  const waiting = phase === "waiting";
  const finished = phase === "done";
  const succeeded = finished && flow?.result === "success";
  const verified = Boolean(
    !status.error &&
    status.data?.logged_in &&
    status.data.verification_state === "verified",
  );

  useEffect(() => {
    if (status.data?.login?.phase === "waiting") {
      setLoginPath("/library/music/tidal/login");
    }
  }, [status.data?.login?.phase]);

  useEffect(() => {
    if (!waiting || !loginPath) return;
    const id = window.setInterval(() => login.reload(), 2000);
    return () => window.clearInterval(id);
  }, [waiting, loginPath, login.reload]);

  // Device authorization completing is not itself the verification claim.
  // Reload the status so Connected requires the provider's real session check.
  useEffect(() => {
    if (succeeded) status.reload();
  }, [succeeded, status.reload]);

  async function connect() {
    if (starting || waiting) return;
    setStarting(true);
    setError("");
    try {
      await api<TidalLoginState>("/library/music/tidal/login", {
        method: "POST",
      });
      setLoginPath("/library/music/tidal/login");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setStarting(false);
    }
  }

  async function save() {
    await onSave();
    status.reload();
  }

  const actualQuality = TIDAL_QUALITY_LABELS[status.data?.quality ?? ""];
  const lastChecked = status.data?.verified_at
    ? new Date(status.data.verified_at * 1000).toLocaleString()
    : "Never";

  return (
    <FieldSet
      legend="TIDAL source"
      description="Automatic tries TIDAL first when installed and uses YouTube when TIDAL cannot supply a match. An explicit source stays on that provider and reports its errors instead of silently substituting another source."
    >
      <RequestState
        loading={status.loading && !status.data}
        error={status.data ? "" : status.error}
        code={status.code}
        retry={status.reload}
      />
      {status.data && !status.data.available && (
        <Alert kind="info" title="TIDAL provider not installed">
          {status.data.detail ||
            "Set REELSCAREY_TIDAL_PROVIDER and restart the server to enable TIDAL sourcing."}
        </Alert>
      )}
      {status.data?.available && (
        <>
          {status.error ? (
            <Alert
              kind="danger"
              title="Current TIDAL verification is unavailable"
            >
              {status.error} The details below are from the previous response,
              not a successful current check. Verify again before relying on
              them for a TIDAL-only download.
            </Alert>
          ) : verified ? (
            <Alert kind="success" title="Connected — session verified">
              TIDAL accepted the stored session. This status is based on a real
              session check, not just the presence of a credential file.
            </Alert>
          ) : status.data.token_present ? (
            <Alert kind="danger" title="TIDAL session is not verified">
              {status.data.detail ||
                "A credential file exists, but TIDAL has not accepted a verified session. Reconnect TIDAL or verify again."}
            </Alert>
          ) : (
            <Alert kind="info" title="TIDAL authorization required">
              No TIDAL credential file is stored. This server is not connected
              to a TIDAL account. Use Connect TIDAL to start device
              authorization.
            </Alert>
          )}
          {status.data.detail && verified && (
            <p className="rs-muted">{status.data.detail}</p>
          )}
          <dl
            className="mus-tidal-status"
            aria-label="TIDAL connection details"
          >
            <div>
              <dt>Provider</dt>
              <dd>Installed and executable</dd>
            </div>
            <div>
              <dt>Credential file</dt>
              <dd>
                {status.data.token_present
                  ? "Present — not proof of login"
                  : "Not present"}
              </dd>
            </div>
            <div>
              <dt>Session verification</dt>
              <dd>{verificationLabel(status.data)}</dd>
            </div>
            <div>
              <dt>Last session check</dt>
              <dd>{lastChecked}</dd>
            </div>
            <div>
              <dt>Device authorization</dt>
              <dd>
                {waiting
                  ? "Waiting for owner authorization"
                  : succeeded
                    ? "Completed; session verification is separate"
                    : finished && flow?.result === "failed"
                      ? "Failed"
                      : "Not in progress"}
              </dd>
            </div>
            <div>
              <dt>Effective requested quality</dt>
              <dd>{actualQuality ?? status.data.quality ?? "Unknown"}</dd>
            </div>
          </dl>
          {waiting && (
            <Alert kind="info" title="Finish authorization in your browser">
              <DeviceInstructions state={flow!} />
            </Alert>
          )}
          {succeeded && !verified && (
            <Alert kind="info" title="Device authorization completed">
              TIDAL login succeeded. The server is verifying the resulting
              session before displaying Connected.
            </Alert>
          )}
          {finished && !succeeded && (
            <Alert kind="danger" title="Device authorization failed">
              {flow?.error || "TIDAL did not accept the device authorization."}
            </Alert>
          )}
          {error && <Alert kind="danger">{error}</Alert>}
          <div className="rs-form-actions">
            <Button
              variant="soft"
              disabled={starting || waiting}
              onClick={() => void connect()}
            >
              {starting
                ? "Starting…"
                : waiting
                  ? "Waiting for login…"
                  : status.data.token_present
                    ? "Reconnect TIDAL"
                    : "Connect TIDAL"}
            </Button>
            <Button
              variant="soft"
              color="gray"
              disabled={status.loading}
              onClick={status.reload}
            >
              <RefreshCw size={14} aria-hidden="true" />
              {status.loading ? "Checking…" : "Verify again"}
            </Button>
          </div>
        </>
      )}
      <div className="mus-tidal-settings">
        <label>
          Default download source{" "}
          <select
            aria-label="Default download source"
            value={downloadSource}
            disabled={!preferencesReady}
            onChange={(event) =>
              onDownloadSourceChange(event.target.value as MusicDownloadSource)
            }
          >
            <option value="">Automatic (TIDAL, then YouTube)</option>
            <option value="tidal" disabled={status.data?.available === false}>
              TIDAL only (requires a verified session)
            </option>
            <option value="youtube">YouTube only</option>
          </select>
        </label>
        <label>
          TIDAL requested quality{" "}
          <select
            aria-label="TIDAL requested quality"
            value={quality}
            disabled={!preferencesReady || status.data?.available === false}
            onChange={(event) =>
              onQualityChange(event.target.value as TidalQuality)
            }
          >
            <option value="">
              Server default{actualQuality ? ` (${actualQuality})` : ""}
            </option>
            {Object.entries(TIDAL_QUALITY_LABELS).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <Button
          disabled={!preferencesReady || saving}
          onClick={() => void save()}
        >
          {saving ? "Saving…" : "Save TIDAL settings"}
        </Button>
      </div>
      <p className="rs-muted rs-small">
        Quality is a request, not a promise to upscale audio. The actual stream
        depends on the account and catalog. Original downloaded bytes are kept
        through import; any conversion uses the selected download profile.
      </p>
      {downloadSource === "tidal" && !verified && (
        <Alert kind="info" title="TIDAL-only downloads require verification">
          New downloads that explicitly use TIDAL will report an authorization
          error until the session is verified. They will not silently use
          YouTube.
        </Alert>
      )}
    </FieldSet>
  );
}
