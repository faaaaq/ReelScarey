import { useState } from "react";
import { Badge, Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import {
  Archive,
  CheckCircle2,
  Copy,
  Download,
  Gauge,
  HardDrive,
  ShieldCheck,
} from "lucide-react";
import {
  DescriptionItem,
  DescriptionList,
  FieldSet,
  PageBody,
  PageToolbar,
  ToolbarButton,
  ToolbarSection,
  formatDuration,
} from "../ui";
import { api, messageFor, type System } from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import "./Admin.css";

type Guide = {
  recovery_pending: boolean;
  steps: {
    id: string;
    title: string;
    message: string;
    state: string;
    href: string;
    optional: boolean;
  }[];
};
export type CheckReport = {
  checked_at: number;
  checks: {
    id: string;
    title: string;
    message: string;
    state: string;
    free_bytes?: number;
  }[];
  resources: {
    heap_bytes: number;
    go_memory_bytes: number;
    goroutines: number;
    logical_cpus: number;
  };
};
type CheckState = { report: CheckReport | null; recovery_pending: boolean };

export function Setup() {
  const guide = useResource<Guide>("/setup");
  return (
    <PageBody>
      <PageHeading title="Set up your library" />
      <RequestState
        loading={guide.loading}
        error={guide.error}
        retry={guide.reload}
      />
      {guide.data && (
        <>
          {guide.data.recovery_pending && (
            <div className="rs-alert is-danger">
              <p>
                Restored jobs are paused for review.{" "}
                <Link to="/system">Review recovery</Link>
              </p>
            </div>
          )}
          <div className="setup-steps">
            {guide.data.steps.map((step, index) => (
              <section className="rs-panel setup-step" key={step.id}>
                <span className="setup-number">
                  {step.state === "ready" ? (
                    <CheckCircle2 size={23} />
                  ) : (
                    index + 1
                  )}
                </span>
                <div>
                  <h2>{step.title}</h2>
                  <p>{step.message}</p>
                </div>
                <Badge color={step.state === "ready" ? "green" : "gray"}>
                  {step.state === "ready"
                    ? "Saved"
                    : step.optional
                      ? "Optional"
                      : "Next step"}
                </Badge>
                {step.id !== "account" && (
                  <Button asChild variant="soft">
                    <Link to={step.href}>Open</Link>
                  </Button>
                )}
              </section>
            ))}
          </div>
          <section className="rs-panel rs-stack">
            <h2>Check your installation</h2>
            <Button asChild>
              <Link to="/system">Open system checks</Link>
            </Button>
            <Button asChild variant="soft">
              <Link to="/search">Find a movie or TV show</Link>
            </Button>
          </section>
        </>
      )}
    </PageBody>
  );
}

function size(bytes: number) {
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
}
export function SystemChecks() {
  const resource = useResource<CheckState>("/system/checks");
  const [result, setResult] = useState<CheckState>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [reviewed, setReviewed] = useState(false);
  const [resumed, setResumed] = useState(false);
  const state = result ?? resource.data;
  async function action(resume = false) {
    setBusy(true);
    setError("");
    try {
      if (resume) {
        await api("/system/recovery/resume", {
          method: "POST",
          body: JSON.stringify({ reviewed }),
        });
        setResumed(true);
        setReviewed(false);
      }
      setResult(
        await api<CheckState>("/system/checks", { method: "POST", body: "{}" }),
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={ShieldCheck}
            label="Run checks"
            spinning={busy}
            disabled={busy || resource.loading}
            onClick={() => void action()}
          />
          <ToolbarButton icon={Archive} label="Backups" to="/backups" />
          <ToolbarButton icon={HardDrive} label="Storage" to="/storage" />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title="Status" />
        <SystemAbout />
        <h2 className="adm-section-title">Health</h2>
        <RequestState
          loading={resource.loading}
          error={resource.error}
          retry={resource.reload}
        />
        {error && (
          <p role="alert" className="rs-alert is-danger">
            {error}
          </p>
        )}
        {resumed && (
          <p role="status" className="rs-alert is-success">
            Recovery hold released. Search and subtitle schedules remain paused;
            enable them when ready.
          </p>
        )}
        {state?.recovery_pending && (
          <section
            className="rs-panel rs-stack recovery-panel"
            aria-label="Recovery review"
          >
            <h2>Review your restored installation</h2>
            <p>
              Download, import and subtitle workers are held. Existing torrent
              jobs can resume after this review; search and subtitle schedules
              remain paused.
            </p>
            <p>
              Check{" "}
              <Link to="/settings">provider connections and storage paths</Link>
              , <Link to="/activity">download activity</Link>,{" "}
              <Link to="/automation">automation</Link> and{" "}
              <Link to="/subtitles">subtitle preferences</Link>. Confirm the
              download client and folders belong to this installation before
              resuming.
            </p>
            <label className="recovery-confirm">
              <input
                type="checkbox"
                checked={reviewed}
                onChange={(e) => setReviewed(e.target.checked)}
              />
              I reviewed the restored settings, storage paths and download
              activity.
            </label>
            <Button
              disabled={!reviewed || busy}
              onClick={() => void action(true)}
            >
              Resume workers
            </Button>
          </section>
        )}
        {state && !state.report && (
          <section className="rs-panel rs-stack">
            <h2>No checks run this session</h2>
            <p>
              Run checks to see current results. Checks create and remove tiny
              private test files in the config and media folders. The download
              folder is checked for read access.
            </p>
          </section>
        )}
        {state?.report && (
          <>
            <p className="rs-muted">
              Last checked{" "}
              {new Date(state.report.checked_at * 1000).toLocaleString()}. Run
              again after changing storage.
            </p>
            <div className="setup-steps">
              {state.report.checks.map((check) => (
                <section className="rs-panel setup-step" key={check.id}>
                  <div>
                    <h2>{check.title}</h2>
                    <p>{check.message}</p>
                    {check.free_bytes !== undefined && (
                      <small>
                        {(check.free_bytes / 1024 ** 3).toFixed(1)} GiB free
                      </small>
                    )}
                  </div>
                  <Badge color={check.state === "ready" ? "green" : "amber"}>
                    {check.state === "ready" ? "Passed" : "Needs attention"}
                  </Badge>
                </section>
              ))}
            </div>
            <section className="rs-panel rs-stack">
              <h2>Application resources</h2>
              <dl className="resource-grid">
                <div>
                  <dt>Go heap</dt>
                  <dd>{size(state.report.resources.heap_bytes)}</dd>
                </div>
                <div>
                  <dt>Go runtime memory</dt>
                  <dd>{size(state.report.resources.go_memory_bytes)}</dd>
                </div>
                <div>
                  <dt>Goroutines</dt>
                  <dd>{state.report.resources.goroutines}</dd>
                </div>
                <div>
                  <dt>Logical CPUs</dt>
                  <dd>{state.report.resources.logical_cpus}</dd>
                </div>
              </dl>
              <p>
                Memory figures describe Go allocations, not total process or
                container memory. NAS performance still needs validation on your
                hardware.
              </p>
            </section>
          </>
        )}
        <MetricsPanel />
        <section className="rs-panel rs-stack">
          <h2>Back up and recover</h2>
          <p>
            Create, schedule, download and restore verified configuration and
            library database backups. Backups contain credentials and account
            data; store them privately. Media and download files need a separate
            backup.
          </p>
          <p>
            Restore saves a safety backup, restarts the app, revokes sessions
            and holds background work for review. Offline backup tools are also
            available in the operations guide.
          </p>
          <Button asChild variant="soft">
            <Link to="/backups">Manage backups</Link>
          </Button>
        </section>
      </PageBody>
    </>
  );
}

function MetricsPanel() {
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function load() {
    setLoading(true);
    setError("");
    setNotice("");
    try {
      const response = await fetch("/api/v1/metrics", {
        credentials: "same-origin",
        headers: { Accept: "text/plain" },
        signal: AbortSignal.timeout(15000),
      });
      if (!response.ok)
        throw new Error(
          `The server answered metrics with HTTP ${response.status}.`,
        );
      setContent(await response.text());
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : "Could not load metrics from the server.",
      );
    } finally {
      setLoading(false);
    }
  }

  async function copy() {
    setError("");
    setNotice("");
    if (!navigator.clipboard?.writeText) {
      setError(
        "This browser cannot copy automatically. Select the text and copy it manually.",
      );
      return;
    }
    try {
      await navigator.clipboard.writeText(content);
      setNotice("Metrics copied.");
    } catch {
      setError(
        "This browser cannot copy automatically. Select the text and copy it manually.",
      );
    }
  }

  function download() {
    setError("");
    setNotice("");
    if (!content) return;
    if (typeof URL.createObjectURL !== "function") {
      setError(
        "This browser cannot create a file download. Select the text and save it manually.",
      );
      return;
    }
    const url = URL.createObjectURL(
      new Blob([content], { type: "text/plain;charset=utf-8" }),
    );
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "reelscarey-metrics.txt";
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
    setNotice("Metrics downloaded.");
  }

  return (
    <FieldSet
      legend="Metrics"
      description="Read-only Prometheus text with durable queue counts. No titles, paths or secrets are included. ReelScarey does not draw a graph from this data."
      actions={
        content ? (
          <>
            <Button
              size="1"
              variant="soft"
              disabled={loading}
              onClick={() => void load()}
            >
              <Gauge size={14} aria-hidden="true" />
              {loading ? "Loading…" : "Refresh"}
            </Button>
            <Button size="1" variant="soft" onClick={() => void copy()}>
              <Copy size={14} aria-hidden="true" />
              Copy
            </Button>
            <Button size="1" variant="soft" onClick={download}>
              <Download size={14} aria-hidden="true" />
              Download
            </Button>
          </>
        ) : (
          <Button size="1" disabled={loading} onClick={() => void load()}>
            <Gauge size={14} aria-hidden="true" />
            {loading ? "Loading…" : "Load metrics"}
          </Button>
        )
      }
    >
      {notice && (
        <p role="status" className="rs-alert is-success">
          {notice}
        </p>
      )}
      {error && (
        <p role="alert" className="rs-alert is-danger">
          {error}
        </p>
      )}
      {content && (
        <pre className="rs-metrics" aria-label="Prometheus metrics">
          {content}
        </pre>
      )}
      {!content && !error && (
        <p className="rs-muted">
          Load the text on demand. Access requires the same administrator
          session as the rest of this page.
        </p>
      )}
    </FieldSet>
  );
}

function SystemAbout() {
  const system = useResource<System>("/system");
  return (
    <FieldSet legend="About">
      <RequestState
        loading={system.loading}
        error={system.error}
        retry={system.reload}
      />
      {system.data && (
        <DescriptionList>
          <DescriptionItem title="Version">
            {system.data.version}
          </DescriptionItem>
          <DescriptionItem title="Database">
            {system.data.database} · schema {system.data.schema_version}
          </DescriptionItem>
          {system.data.uptime_seconds !== undefined && (
            <DescriptionItem title="Uptime">
              {formatDuration(system.data.uptime_seconds)}
            </DescriptionItem>
          )}
          {Object.entries(system.data.paths ?? {}).map(([key, value]) => (
            <DescriptionItem
              key={key}
              title={`${key[0].toUpperCase()}${key.slice(1)} folder`}
            >
              <code>{value}</code>
            </DescriptionItem>
          ))}
        </DescriptionList>
      )}
    </FieldSet>
  );
}
