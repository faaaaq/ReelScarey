import { useState } from "react";
import { Badge, Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { CheckCircle2, ShieldCheck } from "lucide-react";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";

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
    <>
      <PageHeading
        title="Set up your library"
        description="Start with your library and metadata. Connect the optional services whenever you are ready."
      />
      <RequestState
        loading={guide.loading}
        error={guide.error}
        retry={guide.reload}
      />
      {guide.data && (
        <>
          {guide.data.recovery_pending && (
            <div className="error-box">
              <p>
                Restored jobs are paused for review.{" "}
                <Link to="/system">Review recovery</Link>
              </p>
            </div>
          )}
          <div className="setup-steps">
            {guide.data.steps.map((step, index) => (
              <section className="panel setup-step" key={step.id}>
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
          <section className="panel system-panel">
            <h2>Check your installation</h2>
            <p>
              Verify folder access, database health and safe file publication
              before your first import.
            </p>
            <Button asChild>
              <Link to="/system">Open system checks</Link>
            </Button>
            <Button asChild variant="soft">
              <Link to="/search">Find a movie or TV show</Link>
            </Button>
          </section>
        </>
      )}
    </>
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
      <PageHeading
        title="System checks"
        description="Check this installation before importing media or resuming a restored configuration."
      >
        <Button
          disabled={busy || resource.loading}
          onClick={() => void action()}
        >
          <ShieldCheck size={17} />
          {busy ? "Checking…" : "Run checks"}
        </Button>
      </PageHeading>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
      {resumed && (
        <p role="status" className="success-box">
          Recovery hold released. Search and subtitle schedules remain paused;
          enable them when ready.
        </p>
      )}
      {state?.recovery_pending && (
        <section
          className="panel system-panel recovery-panel"
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
            <Link to="/settings">provider connections and storage paths</Link>,{" "}
            <Link to="/activity">download activity</Link>,{" "}
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
        <section className="panel system-panel">
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
          <p className="muted">
            Last checked{" "}
            {new Date(state.report.checked_at * 1000).toLocaleString()}. Results
            describe this moment; run again after changing storage.
          </p>
          <div className="setup-steps">
            {state.report.checks.map((check) => (
              <section className="panel setup-step" key={check.id}>
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
          <section className="panel system-panel">
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
      <section className="panel system-panel">
        <h2>Back up and recover</h2>
        <p>
          Stop ReelScarey before a backup. The local backup tools verify the
          database and its matching encryption key. Backups contain credentials
          and account data; store them privately. Media and torrent files need a
          separate backup.
        </p>
        <p>
          Use the backup and recovery guide in <code>docs/OPERATIONS.md</code>{" "}
          from the project or installation package. Restore always creates a new
          folder, revokes sessions and pauses jobs for review.
        </p>
        <Button asChild variant="soft">
          <Link to="/setup">View setup checklist</Link>
        </Button>
      </section>
    </>
  );
}
