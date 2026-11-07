import { PageBody } from "../ui";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { Badge, Button, Dialog, TextField } from "@radix-ui/themes";
import { Archive, Download, RefreshCw, Upload } from "lucide-react";
import { Link } from "react-router-dom";
import { api, messageFor, type AuthStatus } from "../api";
import { PageHeading, RequestState } from "../components";
import "./Admin.css";

type Snapshot = {
  id: string;
  kind: "manual" | "scheduled" | "upload" | "pre_restore";
  created_at: number;
  size_bytes: number;
  sha256: string;
  manifest: {
    created_at: string;
    application_version: string;
    schema_version: number;
  };
};
type Schedule = {
  enabled: boolean;
  interval_days: number;
  keep_count: number;
  keep_days: number;
  revision: number;
  next_run: number;
  last_attempt: number;
  last_success: number;
  last_error: string;
  running: boolean;
};
type State = {
  schedule: Schedule;
  archives: Snapshot[];
  directory: string;
  max_upload_bytes: number;
  recovery_pending: boolean;
  restore_message: string;
  restoring: boolean;
};
type Review = {
  archive: Snapshot;
  movies: number;
  series: number;
  registered_files: number;
  administrator: string;
};
const endpoint = "/system/backups";
const labels = {
  manual: "Manual",
  scheduled: "Scheduled",
  upload: "Uploaded",
  pre_restore: "Before restore",
};
function date(seconds: number) {
  return seconds ? new Date(seconds * 1000).toLocaleString() : "Never";
}
function size(bytes: number) {
  return `${(bytes / 1024 / 1024).toFixed(2)} MiB`;
}

export function Backups() {
  const [state, setState] = useState<State>();
  const [form, setForm] = useState<Schedule>();
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [review, setReview] = useState<Review>();
  const [confirmation, setConfirmation] = useState("");
  const [reviewed, setReviewed] = useState(false);
  const [deleting, setDeleting] = useState<Snapshot>();
  const [restarting, setRestarting] = useState(false);
  const upload = useRef<HTMLInputElement>(null);
  async function load(initial = false) {
    try {
      const value = await api<State>(endpoint);
      setState(value);
      setError("");
      if (initial) setForm(value.schedule);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load(true);
  }, []);
  useEffect(() => {
    if (!state?.schedule.running || restarting) return;
    const timer = window.setInterval(() => void load(), 2000);
    return () => window.clearInterval(timer);
  }, [state?.schedule.running, restarting]);
  useEffect(() => {
    if (!restarting) return;
    let active = true;
    const timer = window.setInterval(() => {
      void (async () => {
        try {
          const auth = await api<AuthStatus>("/auth/session");
          if (!active) return;
          if (!auth.authenticated) {
            window.location.assign("/system");
            return;
          }
          const value = await api<State>(endpoint);
          if (active && !value.restoring) {
            setState(value);
            setForm(value.schedule);
            setRestarting(false);
            setNotice(value.restore_message);
          }
        } catch {
          /* Restart temporarily closes the listener. Keep the recovery message visible. */
        }
      })();
    }, 3000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [restarting]);
  async function action(task: () => Promise<void>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await task();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (!form) return;
    await action(async () => {
      const { enabled, interval_days, keep_count, keep_days, revision } = form;
      const value = await api<State>(`${endpoint}/schedule`, {
        method: "PUT",
        body: JSON.stringify({
          enabled,
          interval_days,
          keep_count,
          keep_days,
          revision,
        }),
      });
      setState(value);
      setForm(value.schedule);
      setNotice("Backup schedule saved.");
    });
  }
  async function uploadFile(file?: File) {
    if (!file || !state) return;
    if (file.size > state.max_upload_bytes) {
      setError("Backup uploads are limited to 512 MiB.");
      if (upload.current) upload.current.value = "";
      return;
    }
    await action(async () => {
      await api(`${endpoint}/upload`, {
        method: "POST",
        body: file,
        headers: { "Content-Type": "application/zip" },
        signal: AbortSignal.timeout(600000),
      });
      await load();
      setNotice("Backup uploaded and verified. Review it before restoring.");
    });
    if (upload.current) upload.current.value = "";
  }
  const blocked = busy || !!state?.schedule.running || restarting;
  return (
    <PageBody>
      <PageHeading title="Backups">
        <Button
          disabled={blocked || !state}
          onClick={() =>
            void action(async () => {
              await api(endpoint, { method: "POST", body: "{}" });
              await load();
              setNotice("Backup started. This page updates when it finishes.");
            })
          }
        >
          <Archive size={17} />
          Back up now
        </Button>
      </PageHeading>
      <RequestState
        loading={loading}
        error={!state ? error : ""}
        retry={() => void load(true)}
      />
      {!!state && error && (
        <p role="alert" className="rs-alert is-danger">
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="rs-alert is-success">
          {notice}
        </p>
      )}
      {restarting && (
        <section role="status" className="rs-panel rs-stack">
          <h2>Restoring your backup</h2>
          <p>
            ReelScarey is closing active work, saving a safety backup and
            restarting. You will need to sign in again with the restored
            administrator account. Workers remain held until you review System
            checks.
          </p>
          <p>
            This page reconnects automatically. If the connection stays
            unavailable, reopen the app and check the container status.
          </p>
          <a href="/system">Open System checks</a>
        </section>
      )}
      {state && (
        <>
          {state.restore_message && !restarting && (
            <p className="rs-panel rs-stack">{state.restore_message}</p>
          )}
          {state.recovery_pending && (
            <p className="rs-alert is-danger">
              Recovery review is pending. Scheduled backups are held.{" "}
              <Link to="/system">Review System checks</Link>
            </p>
          )}
          <section className="rs-panel rs-stack backup-scope">
            <h2>What is included</h2>
            <p>
              Library records, profiles, settings, encrypted provider
              credentials and their matching encryption key. Video files,
              subtitles, download files, container settings and environment
              variables need a separate backup.
            </p>
            <p>
              Backups contain account and connection secrets. Download them to
              private storage. Copies in this container’s backup volume do not
              protect against loss of the host.
            </p>
            <p className="backup-path">
              Storage: <code>{state.directory}</code>
            </p>
          </section>
          {form && (
            <form
              onSubmit={(e) => void save(e)}
              className="rs-panel rs-stack"
              aria-label="Backup schedule"
            >
              <h2>Scheduled backups</h2>
              <fieldset disabled={blocked} className="backup-fields">
                <label>
                  <input
                    type="checkbox"
                    checked={form.enabled}
                    onChange={(e) =>
                      setForm({ ...form, enabled: e.target.checked })
                    }
                  />{" "}
                  Enable scheduled backups
                </label>
                <div className="backup-grid">
                  <label>
                    Every (days)
                    <input
                      type="number"
                      required
                      min={1}
                      max={30}
                      value={form.interval_days}
                      onChange={(e) =>
                        setForm({
                          ...form,
                          interval_days: Number(e.target.value),
                        })
                      }
                    />
                  </label>
                  <label>
                    Keep scheduled copies
                    <input
                      type="number"
                      required
                      min={1}
                      max={100}
                      value={form.keep_count}
                      onChange={(e) =>
                        setForm({ ...form, keep_count: Number(e.target.value) })
                      }
                    />
                  </label>
                  <label>
                    Keep for (days)
                    <input
                      type="number"
                      required
                      min={1}
                      max={365}
                      value={form.keep_days}
                      onChange={(e) =>
                        setForm({ ...form, keep_days: Number(e.target.value) })
                      }
                    />
                  </label>
                </div>
                <p>
                  After a successful scheduled backup, older scheduled copies
                  exceeding either limit are removed. The newest copy is always
                  kept. Manual, uploaded and pre-restore backups stay until you
                  delete them.
                </p>
                <Button type="submit">Save schedule</Button>
              </fieldset>
              <dl className="resource-grid">
                <div>
                  <dt>Next scheduled backup</dt>
                  <dd>
                    {state.schedule.enabled
                      ? date(state.schedule.next_run)
                      : "Paused"}
                  </dd>
                </div>
                <div>
                  <dt>Last successful backup</dt>
                  <dd>{date(state.schedule.last_success)}</dd>
                </div>
                <div>
                  <dt>Last attempt</dt>
                  <dd>{date(state.schedule.last_attempt)}</dd>
                </div>
              </dl>
              {state.schedule.running && (
                <p role="status">Creating and verifying a backup…</p>
              )}
              {state.schedule.last_error && (
                <p role="alert" className="rs-alert is-danger">
                  {state.schedule.last_error}
                </p>
              )}
            </form>
          )}
          <section className="rs-panel rs-stack" aria-label="Saved backups">
            <div className="backup-heading">
              <h2>Saved backups</h2>
              <div className="rs-form-actions">
                <Button
                  variant="soft"
                  disabled={blocked}
                  onClick={() => upload.current?.click()}
                >
                  <Upload size={16} />
                  Upload backup
                </Button>
                <Button
                  variant="ghost"
                  aria-label="Refresh backups"
                  disabled={blocked}
                  onClick={() => void load(true)}
                >
                  <RefreshCw size={16} />
                </Button>
              </div>
            </div>
            <input
              ref={upload}
              hidden
              type="file"
              accept=".zip,application/zip"
              aria-label="Upload backup ZIP"
              onChange={(e) => void uploadFile(e.target.files?.[0])}
            />
            <p>
              Upload a ReelScarey ZIP backup, up to 512 MiB. Checks run before
              the archive is saved. Uploading does not start a restore.
            </p>
            {!state.archives.length && (
              <p>
                No backups yet. Create a backup or upload one from another
                installation.
              </p>
            )}
            <div className="backup-list">
              {state.archives.map((a) => (
                <article key={a.id} className="backup-row">
                  <div>
                    <strong>
                      {new Date(a.manifest.created_at).toLocaleString()}
                    </strong>{" "}
                    <Badge color={a.kind === "pre_restore" ? "amber" : "gray"}>
                      {labels[a.kind]}
                    </Badge>
                    <p>
                      v{a.manifest.application_version} · Database{" "}
                      {a.manifest.schema_version} · {size(a.size_bytes)}
                    </p>
                  </div>
                  <div className="rs-form-actions">
                    <Button variant="soft" asChild>
                      <a href={`/api/v1${endpoint}/${a.id}/download`} download>
                        <Download size={15} />
                        Download
                      </a>
                    </Button>
                    <Button
                      variant="soft"
                      disabled={blocked}
                      onClick={() =>
                        void action(async () => {
                          const value = await api<Review>(
                            `${endpoint}/${a.id}/preview`,
                            {
                              method: "POST",
                              body: "{}",
                              signal: AbortSignal.timeout(600000),
                            },
                          );
                          setReview(value);
                          setConfirmation("");
                          setReviewed(false);
                        })
                      }
                    >
                      Restore
                    </Button>
                    <Button
                      variant="ghost"
                      color="red"
                      disabled={blocked}
                      onClick={() => setDeleting(a)}
                    >
                      Delete
                    </Button>
                  </div>
                </article>
              ))}
            </div>
          </section>
        </>
      )}
      <Dialog.Root
        open={!!review}
        onOpenChange={(open) => {
          if (!open && !busy) setReview(undefined);
        }}
      >
        <Dialog.Content maxWidth="600px">
          <Dialog.Title>Restore backup</Dialog.Title>
          <Dialog.Description>
            This replaces the current settings, accounts and library database.
            Media files stay in their current locations.
          </Dialog.Description>
          {review && (
            <>
              <dl className="resource-grid">
                <div>
                  <dt>Backup created</dt>
                  <dd>
                    {new Date(
                      review.archive.manifest.created_at,
                    ).toLocaleString()}
                  </dd>
                </div>
                <div>
                  <dt>Restored administrator</dt>
                  <dd>{review.administrator || "First-run setup required"}</dd>
                </div>
                <div>
                  <dt>Library</dt>
                  <dd>
                    {review.movies} movies, {review.series} shows,{" "}
                    {review.registered_files} registered files
                  </dd>
                </div>
              </dl>
              <p>
                A verified safety backup is saved after active work closes. The
                app restarts, signs everyone out and holds workers for review.
                Restoring cannot roll back changes to media files made since
                this backup. Use the restored administrator’s password to sign
                in.
              </p>
              <label className="recovery-confirm">
                <input
                  type="checkbox"
                  checked={reviewed}
                  onChange={(e) => setReviewed(e.target.checked)}
                  disabled={busy}
                />
                I understand that the current configuration and library database
                will be replaced.
              </label>
              <label className="backup-confirm">
                Type RESTORE to continue
                <TextField.Root
                  aria-label="Type RESTORE to continue"
                  value={confirmation}
                  onChange={(e) => setConfirmation(e.target.value)}
                  disabled={busy}
                  autoComplete="off"
                />
              </label>
              {error && (
                <p role="alert" className="rs-alert is-danger">
                  {error}
                </p>
              )}
              <div className="rs-form-actions">
                <Button
                  variant="soft"
                  disabled={busy}
                  onClick={() => setReview(undefined)}
                >
                  Cancel
                </Button>
                <Button
                  color="red"
                  disabled={busy || !reviewed || confirmation !== "RESTORE"}
                  onClick={() =>
                    void action(async () => {
                      await api(`${endpoint}/${review.archive.id}/restore`, {
                        method: "POST",
                        body: JSON.stringify({
                          sha256: review.archive.sha256,
                          confirmation,
                        }),
                        signal: AbortSignal.timeout(600000),
                      });
                      setReview(undefined);
                      setRestarting(true);
                    })
                  }
                >
                  {busy ? "Preparing…" : "Restore and restart"}
                </Button>
              </div>
            </>
          )}
        </Dialog.Content>
      </Dialog.Root>
      <Dialog.Root
        open={!!deleting}
        onOpenChange={(open) => {
          if (!open && !busy) setDeleting(undefined);
        }}
      >
        <Dialog.Content maxWidth="450px">
          <Dialog.Title>Delete backup?</Dialog.Title>
          <Dialog.Description>
            The saved ZIP will be permanently removed. Your current settings and
            library stay unchanged.
          </Dialog.Description>
          {error && (
            <p role="alert" className="rs-alert is-danger">
              {error}
            </p>
          )}
          <div className="rs-form-actions">
            <Button
              variant="soft"
              disabled={busy}
              onClick={() => setDeleting(undefined)}
            >
              Cancel
            </Button>
            <Button
              color="red"
              disabled={busy}
              onClick={() =>
                void action(async () => {
                  if (!deleting) return;
                  await api(`${endpoint}/${deleting.id}`, { method: "DELETE" });
                  setDeleting(undefined);
                  await load();
                })
              }
            >
              Delete backup
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Root>
    </PageBody>
  );
}
