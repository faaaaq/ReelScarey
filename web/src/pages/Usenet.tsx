import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type FormEvent,
} from "react";
import { Button } from "@radix-ui/themes";
import {
  CloudDownload,
  FilePlus,
  Gauge,
  Pause,
  Play,
  RefreshCw,
  Settings2,
  Trash,
} from "lucide-react";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import {
  Alert,
  Check,
  ConfirmDialog,
  EmptyState,
  FieldSet,
  FormActions,
  FormGroup,
  Label,
  PageBody,
  PageToolbar,
  ToolbarButton,
  ToolbarSection,
  ToolbarSeparator,
  formatBytes,
  formatSpeed,
  readFile,
} from "../ui";
import { useLiveResource } from "../useLiveResource";
import {
  AddNZBDialog,
  DeleteQueueDialog,
  UsenetSpeedLimitDialog,
  type NZBFailure,
} from "./usenet/dialogs";
import {
  lifecycleKind,
  lifecycleLabels,
  maxQueueSelection,
  queueBlocker,
  type UsenetOverview,
  type UsenetStatus,
} from "./usenet/model";
import { UsenetQueueTable } from "./usenet/QueueTable";
import "./Usenet.css";

type Message = { kind: "success" | "danger" | "warning"; text: string };

function scrollToSettings() {
  document
    .getElementById("usenet-settings")
    ?.scrollIntoView({ block: "start" });
}

function NewsServerForm({
  status,
  onSaved,
  busy,
  setBusy,
  setMessage,
}: {
  status: UsenetStatus;
  onSaved: () => void;
  busy: boolean;
  setBusy: (value: boolean) => void;
  setMessage: (value: Message | null) => void;
}) {
  const [host, setHost] = useState(status.host ?? "");
  const [port, setPort] = useState(String(status.port || 563));
  const [tls, setTLS] = useState(status.tls);
  const [username, setUsername] = useState(status.username ?? "");
  const [password, setPassword] = useState("");
  const [connections, setConnections] = useState(
    String(status.connections || 8),
  );
  const [problem, setProblem] = useState("");
  const [notice, setNotice] = useState("");

  async function save(event: FormEvent) {
    event.preventDefault();
    setProblem("");
    setNotice("");
    setBusy(true);
    try {
      await api<UsenetStatus>("/usenet/news", {
        method: "PUT",
        signal: AbortSignal.timeout(40000),
        body: JSON.stringify({
          host,
          port: Number(port),
          tls,
          username,
          connections: Number(connections),
          ...(password ? { password } : {}),
        }),
      });
      setPassword("");
      setNotice("News server saved.");
      setMessage({ kind: "success", text: "News server saved." });
      onSaved();
    } catch (e) {
      setProblem(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function test() {
    setProblem("");
    setNotice("");
    setBusy(true);
    try {
      await api("/usenet/news/test", {
        method: "POST",
        signal: AbortSignal.timeout(15000),
        body: JSON.stringify({
          ...(host ? { host } : {}),
          port: Number(port),
          tls,
          username,
          connections: Number(connections),
        }),
      });
      setNotice("The news server accepted a TCP connection.");
      setMessage({
        kind: "success",
        text: "The news server accepted a TCP connection. This does not verify the account or article access.",
      });
    } catch (e) {
      setProblem(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="use-stack" onSubmit={save}>
      <fieldset disabled={busy} className="use-grid">
        <FormGroup label="Host" htmlFor="use-news-host">
          <input
            id="use-news-host"
            value={host}
            required
            placeholder="news.example.com"
            autoComplete="off"
            onChange={(e) => setHost(e.target.value)}
          />
        </FormGroup>
        <FormGroup label="Port" htmlFor="use-news-port">
          <input
            id="use-news-port"
            type="number"
            min={1}
            max={65535}
            value={port}
            required
            onChange={(e) => setPort(e.target.value)}
          />
        </FormGroup>
        <FormGroup
          label="Connections"
          htmlFor="use-news-connections"
          help="1–64 parallel NNTP connections. Use what your provider allows."
        >
          <input
            id="use-news-connections"
            type="number"
            min={1}
            max={64}
            value={connections}
            required
            onChange={(e) => setConnections(e.target.value)}
          />
        </FormGroup>
        <FormGroup label="Security" htmlFor="use-news-tls">
          <Check
            id="use-news-tls"
            label="Use TLS"
            description="Recommended for port 563. The connection test checks reachability, not the certificate or account."
            checked={tls}
            onChange={setTLS}
            disabled={busy}
          />
        </FormGroup>
        <FormGroup label="Username" htmlFor="use-news-username">
          <input
            id="use-news-username"
            value={username}
            autoComplete="off"
            placeholder="Leave blank for IP-based access"
            onChange={(e) => setUsername(e.target.value)}
          />
        </FormGroup>
        <FormGroup
          label="Password"
          htmlFor="use-news-password"
          help={
            status.configured
              ? "Leave blank to keep the saved password. Changing the host or username requires entering it again."
              : "Stored encrypted. Re-enter it whenever you change the host or username."
          }
        >
          <input
            id="use-news-password"
            type="password"
            value={password}
            autoComplete="new-password"
            placeholder={status.configured ? "Saved; leave blank to keep" : ""}
            onChange={(e) => setPassword(e.target.value)}
          />
        </FormGroup>
      </fieldset>
      <FormActions>
        <Button type="submit" disabled={busy}>
          {busy ? "Working…" : "Save news server"}
        </Button>
        <Button
          type="button"
          variant="soft"
          disabled={busy || !host}
          onClick={() => void test()}
        >
          Test connection
        </Button>
      </FormActions>
      {problem && <Alert kind="danger">{problem}</Alert>}
      {notice && <Alert kind="success">{notice}</Alert>}
    </form>
  );
}

export function Usenet() {
  const overview = useLiveResource<UsenetOverview>("/usenet", 2000);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [message, setMessage] = useState<Message | null>(null);
  const [busy, setBusy] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [speedOpen, setSpeedOpen] = useState(false);
  const data = overview.data;
  const status = data?.status;
  const queue = data?.queue;
  const items = useMemo(() => queue?.items ?? [], [queue]);
  const selectedItems = useMemo(
    () => items.filter((i) => selected.has(i.id)),
    [items, selected],
  );
  const ids = useMemo(() => selectedItems.map((i) => i.id), [selectedItems]);

  // Selections survive polling only while the same jobs remain in the queue.
  useEffect(() => {
    setSelected((current) => {
      const next = new Set(
        [...current].filter((id) => items.some((item) => item.id === id)),
      );
      return next.size === current.size ? current : next;
    });
  }, [items]);

  const reload = overview.reload;
  const blocker = status && queue ? queueBlocker(status, queue) : null;
  const canQueue = !!queue?.available && !blocker;

  const toggle = useCallback((id: string) => {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else if (next.size < maxQueueSelection) next.add(id);
      return next;
    });
  }, []);
  const toggleAll = useCallback(
    (checked: boolean) =>
      setSelected(
        checked
          ? new Set(items.slice(0, maxQueueSelection).map((i) => i.id))
          : new Set(),
      ),
    [items],
  );

  async function act(
    action: string,
    extra: Record<string, unknown> = {},
    done?: string,
  ) {
    if (!ids.length) return false;
    if (ids.length > maxQueueSelection) {
      setMessage({
        kind: "danger",
        text: `Select at most ${maxQueueSelection} jobs for one action.`,
      });
      return false;
    }
    setBusy(true);
    setMessage(null);
    try {
      await api("/usenet/queue/actions", {
        method: "POST",
        signal: AbortSignal.timeout(50000),
        body: JSON.stringify({ ids, action, ...extra }),
      });
      if (done) setMessage({ kind: "success", text: done });
      return true;
    } catch (e) {
      setMessage({ kind: "danger", text: messageFor(e) });
      return false;
    } finally {
      setBusy(false);
      reload();
    }
  }

  async function addFiles(files: File[]): Promise<NZBFailure[]> {
    const failures: NZBFailure[] = [];
    setMessage(null);
    for (const file of files) {
      try {
        const body = await readFile(file);
        await api("/usenet/queue", {
          method: "POST",
          headers: { "Content-Type": "application/x-nzb" },
          body,
          signal: AbortSignal.timeout(60000),
        });
      } catch (e) {
        failures.push({ name: file.name, message: messageFor(e) });
      }
    }
    reload();
    const added = files.length - failures.length;
    if (added) {
      setMessage({
        kind: "success",
        text: added === 1 ? "Added 1 NZB." : `Added ${added} NZBs.`,
      });
    }
    return failures;
  }

  async function saveSpeed(bytes: number) {
    setBusy(true);
    setMessage(null);
    try {
      await api("/usenet/speed-limits", {
        method: "POST",
        signal: AbortSignal.timeout(40000),
        body: JSON.stringify({ download_limit: bytes }),
      });
      setMessage({
        kind: "success",
        text: bytes
          ? "Speed limit saved for the running client."
          : "Speed limit removed for the running client.",
      });
      return true;
    } catch (e) {
      setMessage({ kind: "danger", text: messageFor(e) });
      return false;
    } finally {
      setBusy(false);
      reload();
    }
  }

  async function setEnabled(next: boolean) {
    if (!status) return;
    setBusy(true);
    setMessage(null);
    try {
      await api("/usenet", {
        method: "PUT",
        signal: AbortSignal.timeout(130000),
        body: JSON.stringify({ revision: status.revision, enabled: next }),
      });
      setMessage({
        kind: "success",
        text: next
          ? "The bundled Usenet client is on."
          : "The bundled Usenet client is off.",
      });
    } catch (e) {
      setMessage({ kind: "danger", text: messageFor(e) });
    } finally {
      setBusy(false);
      reload();
    }
  }

  if (!data || !status || !queue) {
    return (
      <PageBody>
        <h1 className="sr-only">Usenet</h1>
        <RequestState
          loading={overview.loading}
          error={overview.error}
        code={overview.code}
          retry={overview.reload}
        />
      </PageBody>
    );
  }

  const limit = queue?.limit_bytes ?? 0;

  return (
    <div className="use-app">
      <h1 className="sr-only">Usenet</h1>
      <PageToolbar label="Usenet actions">
        <ToolbarSection>
          <ToolbarButton
            icon={FilePlus}
            label="Add NZB"
            title="Upload NZB files to the bundled client"
            disabled={!canQueue || busy}
            onClick={() => setAddOpen(true)}
          />
          <ToolbarSeparator />
          <ToolbarButton
            icon={Play}
            label="Resume"
            disabled={!ids.length || busy}
            onClick={() => void act("resume", {}, "Queue resumed.")}
          />
          <ToolbarButton
            icon={Pause}
            label="Pause"
            disabled={!ids.length || busy}
            onClick={() => void act("pause", {}, "Queue paused.")}
          />
          <ToolbarButton
            icon={Trash}
            label="Delete"
            danger
            disabled={!ids.length || busy}
            onClick={() => setDeleteOpen(true)}
          />
          <ToolbarSeparator />
          <ToolbarButton
            icon={Gauge}
            label="Speed limit"
            title="Set the bundled SABnzbd download limit"
            disabled={!canQueue || busy}
            onClick={() => setSpeedOpen(true)}
          />
          <ToolbarButton
            icon={Settings2}
            label="Settings"
            onClick={scrollToSettings}
          />
        </ToolbarSection>
        <ToolbarSection align="right">
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={overview.loading}
            onClick={reload}
          />
        </ToolbarSection>
      </PageToolbar>

      <PageBody>
        <div className="use-statusbar" aria-label="Usenet status">
          <span className="use-status-item">
            <Label kind={lifecycleKind(status.state)}>
              {lifecycleLabels[status.state] ?? status.state}
            </Label>
            <span className="use-status-name">{status.name}</span>
          </span>
          <span className="use-status-item">
            {status.installed ? "SABnzbd installed" : "SABnzbd not installed"}
          </span>
          <span className="use-status-item">
            Queue:{" "}
            <strong>{queue.paused ? "Paused" : (queue.state ?? "—")}</strong>
          </span>
          <span className="use-status-item">
            Speed: <strong>{formatSpeed(queue?.speed, "0 B/s")}</strong>
          </span>
          <span className="use-status-item">
            Remaining: <strong>{formatBytes(queue?.remaining ?? 0)}</strong>
          </span>
          {queue?.eta && (
            <span className="use-status-item">
              ETA: <strong>{queue.eta}</strong>
            </span>
          )}
          <span className="use-status-item">
            Limit: <strong>{limit ? formatSpeed(limit) : "No limit"}</strong>
          </span>
          <span className="use-status-spacer" />
          <span className="use-status-item">
            {status.active_jobs} active ReelScarey job
            {status.active_jobs === 1 ? "" : "s"}
          </span>
        </div>

        {message && (
          <Alert
            kind={message.kind}
            role={message.kind === "danger" ? "alert" : "status"}
          >
            {message.text}
          </Alert>
        )}
        {overview.error && (
          <Alert kind="warning" title="Status refresh failed">
            {overview.error}
          </Alert>
        )}

        {blocker ? (
          <EmptyState
            icon={CloudDownload}
            title={blocker.title}
            actions={
              blocker.action === "settings" ? (
                <Button onClick={scrollToSettings}>
                  Open news server settings
                </Button>
              ) : blocker.action === "clients" ? (
                <Button asChild>
                  <Link to="/download-clients">Open download clients</Link>
                </Button>
              ) : blocker.action === "refresh" ? (
                <Button variant="soft" onClick={reload}>
                  Refresh status
                </Button>
              ) : undefined
            }
          >
            <p>{blocker.message}</p>
          </EmptyState>
        ) : (
          <UsenetQueueTable
            items={items}
            selected={selected}
            onToggle={toggle}
            onToggleAll={toggleAll}
            emptyText="No jobs in the bundled SABnzbd queue."
          />
        )}

        <div id="usenet-settings" className="use-settings">
          <FieldSet
            legend="News server"
            description="The bundled SABnzbd daemon uses this NNTP provider. It is stored encrypted with the existing configuration key; the password is never returned."
          >
            <NewsServerForm
              key={status.revision}
              status={status}
              onSaved={reload}
              busy={busy}
              setBusy={setBusy}
              setMessage={setMessage}
            />
          </FieldSet>
          <FieldSet
            legend="Bundled SABnzbd client"
            description="A private SABnzbd daemon supervised inside this container. Its web API listens only on loopback, and its download folder is dedicated to ReelScarey."
          >
            <div className="use-stack">
              <p>
                Status:{" "}
                <Label kind={lifecycleKind(status.state)}>
                  {lifecycleLabels[status.state] ?? status.state}
                </Label>
                {status.message ? ` · ${status.message}` : ""}
              </p>
              {!status.installed && (
                <Alert kind="danger" title="SABnzbd is not installed">
                  This deployment does not include <code>sabnzbdplus</code>. The
                  Docker image ships it; native installs must provide it or set{" "}
                  <code>REELSCAREY_BUNDLED_SAB</code>.
                </Alert>
              )}
              {!status.configured && (
                <p className="rs-form-help">
                  Save a news server first. The client never starts without one.
                </p>
              )}
              {status.enabled && status.active_jobs > 0 && (
                <p>
                  {status.active_jobs} active{" "}
                  {status.active_jobs === 1 ? "job uses" : "jobs use"} this
                  client. It cannot be turned off until they finish or are
                  stopped.
                </p>
              )}
              <p className="rs-form-help">
                {status.enabled
                  ? "Existing jobs keep the client they were reserved with. To make new Usenet downloads use it, choose it as the Usenet default in Download clients."
                  : "New Usenet downloads use the configured Usenet default. Turning this client on does not change that default."}
              </p>
              <FormActions>
                {status.enabled ? (
                  <ConfirmDialog
                    trigger={
                      <Button
                        variant="soft"
                        color="red"
                        disabled={busy || status.active_jobs > 0}
                      >
                        Turn off client
                      </Button>
                    }
                    title="Turn off the bundled SABnzbd client?"
                    description="Its Usenet default is cleared and its process stops. Existing jobs keep their binding."
                    onConfirm={() => void setEnabled(false)}
                  />
                ) : (
                  <ConfirmDialog
                    trigger={
                      <Button
                        disabled={
                          busy || !status.configured || !status.installed
                        }
                      >
                        Turn on client
                      </Button>
                    }
                    title="Turn on the bundled SABnzbd client?"
                    description="The app starts a private daemon and waits for its API. It does not make itself the Usenet default."
                    onConfirm={() => void setEnabled(true)}
                  />
                )}
                <Button variant="soft" asChild>
                  <Link to="/download-clients">Choose Usenet default</Link>
                </Button>
              </FormActions>
            </div>
          </FieldSet>
        </div>
      </PageBody>

      <AddNZBDialog
        open={addOpen}
        onOpenChange={setAddOpen}
        onSubmit={addFiles}
      />
      <DeleteQueueDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        items={selectedItems}
        onConfirm={(deleteFiles) =>
          act(
            "delete",
            { delete_files: deleteFiles, reviewed: true },
            deleteFiles
              ? "Queued jobs and their SABnzbd files deleted."
              : "Queued jobs deleted.",
          )
        }
      />
      <UsenetSpeedLimitDialog
        open={speedOpen}
        onOpenChange={setSpeedOpen}
        current={limit}
        onSubmit={saveSpeed}
      />
    </div>
  );
}
