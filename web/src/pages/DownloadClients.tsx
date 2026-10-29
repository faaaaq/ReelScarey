import { useState, type FormEvent } from "react";
import { Badge, Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";

type Client = {
  id: string;
  name: string;
  kind: "qbittorrent" | "sabnzbd" | "nzbget";
  enabled: boolean;
  revision: number;
  url: string;
  username?: string;
  download_path: string;
  local_folder: string;
  active_jobs: number;
};
type Defaults = { torrent: string; usenet: string; revision: number };
type State = { items: Client[]; defaults: Defaults; downloads_root?: string };
const names = {
  qbittorrent: "qBittorrent",
  sabnzbd: "SABnzbd",
  nzbget: "NZBGet",
};
export function DownloadClients() {
  const resource = useResource<State>("/download-clients");
  return (
    <>
      <PageHeading
        title="Download clients"
        description="Save named connections and choose defaults for future torrent and Usenet jobs."
      />
      <p>
        Each job keeps its original client and folder mapping. Changing a
        default affects future jobs. Existing connections in{" "}
        <Link to="/settings#qbittorrent">Settings</Link> remain available as the
        fallback choices.
      </p>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {resource.data && (
        <>
          <DefaultsForm
            key={resource.data.defaults.revision}
            initial={resource.data.defaults}
            clients={resource.data.items}
            reload={resource.reload}
          />
          <section className="panel system-panel">
            <h2>Named connections</h2>
            {!resource.data.items.length && (
              <p>
                No named clients saved. Existing Settings connections continue
                to work.
              </p>
            )}
            {resource.data.items.map((client) => (
              <ClientForm
                key={`${client.id}-${client.revision}`}
                client={client}
                isDefault={
                  resource.data!.defaults.torrent === client.id ||
                  resource.data!.defaults.usenet === client.id
                }
                root={resource.data!.downloads_root ?? "/downloads"}
                reload={resource.reload}
              />
            ))}
          </section>
          <section className="panel system-panel">
            <h2>Add a download client</h2>
            <ClientForm
              root={resource.data.downloads_root ?? "/downloads"}
              reload={resource.reload}
            />
          </section>
        </>
      )}
      <section className="panel system-panel">
        <h2>Usenet folder setup</h2>
        <p>
          SABnzbd needs a <code>reelscarey</code> category with folder{" "}
          <code>ReelScarey</code>, no sorting and no script. NZBGet needs a{" "}
          <code>reelscarey</code> category whose destination is the completed
          folder plus <code>/ReelScarey</code>, using a profile without
          post-processing extensions. Only fully completed jobs in their own
          folders can import.
        </p>
        <p>
          Client services stay separate from the ReelScarey container. Mount
          their completed folders under the app's download mount. TV files still
          require the existing episode-mapping review. Uncertain Usenet
          submissions are reconciled by their unique job identity and are never
          blindly resubmitted.
        </p>
      </section>
    </>
  );
}
function DefaultsForm({
  initial,
  clients,
  reload,
}: {
  initial: Defaults;
  clients: Client[];
  reload: () => void;
}) {
  const [form, setForm] = useState(initial),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/download-clients/defaults", {
        method: "PUT",
        body: JSON.stringify(form),
      });
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="panel system-panel" onSubmit={(e) => void save(e)}>
      <h2>Defaults for new jobs</h2>
      <fieldset disabled={busy} className="backup-fields">
        {(["torrent", "usenet"] as const).map((protocol) => (
          <label key={protocol}>
            {protocol === "torrent" ? "Torrent default" : "Usenet default"}
            <select
              value={form[protocol]}
              onChange={(e) => setForm({ ...form, [protocol]: e.target.value })}
            >
              <option value="">Settings connection</option>
              {clients
                .filter(
                  (c) =>
                    c.enabled &&
                    (protocol === "torrent"
                      ? c.kind === "qbittorrent"
                      : c.kind !== "qbittorrent"),
                )
                .map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name} · {names[c.kind]}
                  </option>
                ))}
            </select>
          </label>
        ))}
        <Button type="submit">Save defaults</Button>
      </fieldset>
      {error && (
        <p className="error-box" role="alert">
          {error}
        </p>
      )}
    </form>
  );
}
function ClientForm({
  client,
  isDefault = false,
  root,
  reload,
}: {
  client?: Client;
  isDefault?: boolean;
  root: string;
  reload: () => void;
}) {
  const [name, setName] = useState(client?.name ?? ""),
    [kind, setKind] = useState<Client["kind"]>(client?.kind ?? "qbittorrent"),
    [url, setURL] = useState(client?.url ?? ""),
    [username, setUsername] = useState(client?.username ?? ""),
    [password, setPassword] = useState(""),
    [key, setKey] = useState(""),
    [remote, setRemote] = useState(client?.download_path ?? ""),
    [local, setLocal] = useState(client?.local_folder ?? ""),
    [enabled, setEnabled] = useState(client?.enabled ?? false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api(`/download-clients${client ? `/${client.id}` : ""}`, {
        method: client ? "PUT" : "POST",
        signal: AbortSignal.timeout(40000),
        body: JSON.stringify({
          name,
          kind,
          url,
          username,
          enabled: client ? enabled : false,
          download_path: remote,
          local_folder: local,
          revision: client?.revision ?? 0,
          ...(kind === "sabnzbd"
            ? key
              ? { api_key: key }
              : {}
            : password
              ? { password }
              : {}),
        }),
      });
      setPassword("");
      setKey("");
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function action(remove = false) {
    if (!client) return;
    if (
      remove &&
      !window.confirm(
        `Remove ${client.name} and its saved credentials? Its completed job history stays in Activity.`,
      )
    )
      return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api(`/download-clients/${client.id}${remove ? "" : "/test"}`, {
        method: remove ? "DELETE" : "POST",
        signal: AbortSignal.timeout(40000),
        body: JSON.stringify(
          remove ? { revision: client.revision, reviewed: true } : {},
        ),
      });
      if (remove) reload();
      else setNotice("Connection verified.");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  const fields = (
    <form onSubmit={(e) => void save(e)}>
      <fieldset disabled={busy} className="backup-fields">
        <label>
          Client name
          <input
            value={name}
            maxLength={60}
            required
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label>
          Client type
          <select
            value={kind}
            disabled={!!client}
            onChange={(e) => setKind(e.target.value as Client["kind"])}
          >
            {Object.entries(names).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Server address
          <input
            value={url}
            type="url"
            required
            placeholder={
              kind === "nzbget"
                ? "http://nzbget:6789"
                : "http://download-server:8080"
            }
            onChange={(e) => setURL(e.target.value)}
          />
        </label>
        {kind === "sabnzbd" ? (
          <label>
            API key
            <input
              type="password"
              value={key}
              autoComplete="new-password"
              required={!client}
              placeholder={
                client ? "Saved; leave blank to keep" : "Full API key"
              }
              onChange={(e) => setKey(e.target.value)}
            />
          </label>
        ) : (
          <>
            <label>
              Username
              <input
                value={username}
                required
                autoComplete="off"
                onChange={(e) => setUsername(e.target.value)}
              />
            </label>
            <label>
              Password
              <input
                type="password"
                value={password}
                autoComplete="new-password"
                required={!client}
                placeholder={client ? "Saved; leave blank to keep" : ""}
                onChange={(e) => setPassword(e.target.value)}
              />
            </label>
          </>
        )}
        <label>
          Client's completed download folder
          <input
            value={remote}
            required
            placeholder="/completed"
            onChange={(e) => setRemote(e.target.value)}
          />
        </label>
        <label>
          Local subfolder inside {root}
          <input
            value={local}
            placeholder="Optional, for example server-a"
            onChange={(e) => setLocal(e.target.value)}
          />
        </label>
        <p className="field-help">
          Leave the local subfolder blank when both services share the download
          root. Otherwise this maps the client's completed folder to {root}/
          {local || "server-a"}. Separate clients may use different subfolders.
        </p>
        {client ? (
          <label>
            <input
              type="checkbox"
              checked={enabled}
              disabled={isDefault}
              onChange={(e) => setEnabled(e.target.checked)}
            />{" "}
            Enable for new downloads
            {isDefault && " · choose another default before pausing"}
          </label>
        ) : (
          <p>
            New connections start paused. Enable and select a default after
            reviewing the saved mapping.
          </p>
        )}
        <Button type="submit">
          {busy ? "Working…" : "Test and save client"}
        </Button>
        {client && (
          <>
            <Button type="button" variant="soft" onClick={() => void action()}>
              Test saved connection
            </Button>
            <Button
              type="button"
              variant="soft"
              color="red"
              disabled={isDefault || client.active_jobs > 0}
              onClick={() => void action(true)}
            >
              Remove client
            </Button>
            <p className="field-help">
              Existing jobs continue tracking their saved client while new
              downloads are paused. Address/folder changes are blocked while
              active jobs depend on them. Stopped jobs retain the client for
              retry.
            </p>
          </>
        )}
      </fieldset>
      {error && (
        <p className="error-box" role="alert">
          {error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
    </form>
  );
  return client ? (
    <details className="client-card">
      <summary>
        <strong>{client.name}</strong> · {names[client.kind]}{" "}
        <Badge color={client.enabled ? "green" : "gray"}>
          {client.enabled ? "Enabled" : "Paused"}
        </Badge>
        {isDefault && <Badge>Default</Badge>} · {client.active_jobs} active jobs
      </summary>
      {fields}
    </details>
  ) : (
    fields
  );
}
