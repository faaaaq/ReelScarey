import { useEffect, useState, type FormEvent } from "react";
import { Badge, Button } from "@radix-ui/themes";
import { Plus, RefreshCw } from "lucide-react";
import {
  AddCard,
  Card,
  CardGrid,
  FieldSet,
  Label,
  Modal,
  PageBody,
  PageToolbar,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import "./Admin.css";

type Client = {
  id: string;
  name: string;
  kind: "qbittorrent" | "transmission" | "sabnzbd" | "nzbget";
  enabled: boolean;
  builtin?: boolean;
  revision: number;
  url: string;
  username?: string;
  download_path: string;
  local_folder: string;
  active_jobs: number;
};
type Defaults = { torrent: string; usenet: string; revision: number };
type State = { items: Client[]; defaults: Defaults; downloads_root?: string };
type Builtin = {
  present: boolean;
  enabled: boolean;
  state: string;
  message?: string;
  revision: number;
  peer_port: number;
  active_jobs: number;
  name: string;
};
type VPN = {
  present: boolean;
  enabled: boolean;
  kill_switch: boolean;
  state: string;
  message?: string;
  interface?: string;
  address?: string;
  endpoint?: string;
  revision: number;
};
const names = {
  qbittorrent: "qBittorrent",
  transmission: "Transmission",
  sabnzbd: "SABnzbd",
  nzbget: "NZBGet",
};
export function DownloadClients() {
  const resource = useResource<State>("/download-clients");
  const [editing, setEditing] = useState<Client | "new" | null>(null);
  const root = resource.data?.downloads_root ?? "/downloads";
  const named = resource.data?.items.filter((c) => !c.builtin) ?? [];
  const defaults = resource.data?.defaults;
  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={Plus}
            label="Add Client"
            disabled={!resource.data}
            onClick={() => setEditing("new")}
          />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={resource.loading}
            onClick={resource.reload}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title="Download Clients" />
        <RequestState
          loading={resource.loading}
          error={resource.error}
          retry={resource.reload}
        />
        {resource.data && defaults && (
          <>
            <FieldSet legend="Clients">
              {named.length === 0 && (
                <p className="rs-muted">No named clients saved.</p>
              )}
              <CardGrid>
                {named.map((client) => {
                  const isDefault =
                    defaults.torrent === client.id ||
                    defaults.usenet === client.id;
                  return (
                    <Card
                      key={`${client.id}-${client.revision}`}
                      title={client.name}
                      ariaLabel={`Edit ${client.name}`}
                      onClick={() => setEditing(client)}
                      labels={
                        <>
                          <Label kind="info">{names[client.kind]}</Label>
                          <Label kind={client.enabled ? "success" : "disabled"}>
                            {client.enabled ? "Enabled" : "Paused"}
                          </Label>
                          {isDefault && <Label kind="primary">Default</Label>}
                          {client.active_jobs > 0 && (
                            <Label outline>
                              {client.active_jobs} active job
                              {client.active_jobs === 1 ? "" : "s"}
                            </Label>
                          )}
                        </>
                      }
                    >
                      {client.url}
                    </Card>
                  );
                })}
                <AddCard
                  label="Add download client"
                  onClick={() => setEditing("new")}
                />
              </CardGrid>
            </FieldSet>
            <FieldSet legend="Defaults for new jobs">
              <DefaultsForm
                key={defaults.revision}
                initial={defaults}
                clients={resource.data.items}
                reload={resource.reload}
              />
            </FieldSet>
          </>
        )}
        <BuiltinCard reload={resource.reload} />
        <VPNCard />
        <FieldSet legend="Client setup notes">
          <ul className="adm-notes">
            <li>
              <strong>Transmission 4:</strong> username and password, torrent
              scripts disabled, full RPC URL. Jobs use an ownership label and a
              dedicated folder.
            </li>
            <li>
              <strong>SABnzbd:</strong> a <code>reelscarey</code> category with
              folder <code>ReelScarey</code>, no sorting and no script.
            </li>
            <li>
              <strong>NZBGet:</strong> a <code>reelscarey</code> category
              pointing at the completed folder plus <code>/ReelScarey</code>.
            </li>
            <li>
              <strong>Built-in SABnzbd:</strong> configure its news server and
              turn it on from the Usenet page. It is never enabled automatically
              because it needs your NNTP account.
            </li>
            <li>
              Mount completed client folders under the app's download mount. TV
              files still require episode review.
            </li>
          </ul>
        </FieldSet>
        {editing && (
          <Modal
            open
            onOpenChange={(open) => !open && setEditing(null)}
            size="large"
            title={
              editing === "new" ? "Add download client" : `Edit ${editing.name}`
            }
          >
            <ClientForm
              client={editing === "new" ? undefined : editing}
              isDefault={
                editing !== "new" &&
                (defaults?.torrent === editing.id ||
                  defaults?.usenet === editing.id)
              }
              root={root}
              reload={resource.reload}
              onDone={() => setEditing(null)}
            />
          </Modal>
        )}
      </PageBody>
    </>
  );
}
const vpnStates: Record<string, string> = {
  up: "Tunnel up",
  starting: "Connecting…",
  stopped: "Stopped",
  failed: "Needs attention",
};
function parseWireGuard(text: string) {
  let section = "";
  const values: Record<string, string> = {};
  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#") || line.startsWith(";")) continue;
    if (line.startsWith("[")) {
      section = line.toLowerCase();
      continue;
    }
    const split = line.indexOf("=");
    if (split < 0) continue;
    values[`${section}.${line.slice(0, split).trim().toLowerCase()}`] = line
      .slice(split + 1)
      .trim();
  }
  const parsed = {
    address: values["[interface].address"] ?? "",
    private_key: values["[interface].privatekey"] ?? "",
    peer_key: values["[peer].publickey"] ?? "",
    endpoint: values["[peer].endpoint"] ?? "",
    allowed_ips: values["[peer].allowedips"] ?? "",
  };
  if (
    !parsed.address ||
    !parsed.private_key ||
    !parsed.peer_key ||
    !parsed.endpoint
  ) {
    throw new Error(
      "The file is missing the tunnel address, private key, server public key or endpoint.",
    );
  }
  return parsed;
}

function VPNCard() {
  const resource = useResource<VPN>("/vpn/mullvad");
  const [address, setAddress] = useState(""),
    [endpoint, setEndpoint] = useState(""),
    [peerKey, setPeerKey] = useState(""),
    [privateKey, setPrivateKey] = useState(""),
    [allowedIPs, setAllowedIPs] = useState(""),
    [killSwitch, setKillSwitch] = useState(true),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const data = resource.data;
  useEffect(() => {
    if (data) setKillSwitch(data.kill_switch);
  }, [data?.revision]);
  async function save(
    enabled: boolean,
    overrides?: ReturnType<typeof parseWireGuard>,
  ) {
    if (!data || busy) return;
    setBusy(true);
    setError("");
    setNotice(enabled ? "Connecting the tunnel…" : "");
    try {
      await api("/vpn/mullvad", {
        method: "PUT",
        signal: AbortSignal.timeout(100000),
        body: JSON.stringify({
          revision: data.revision,
          enabled,
          kill_switch: killSwitch,
          address: overrides?.address ?? (address || data.address || ""),
          endpoint: overrides?.endpoint ?? (endpoint || data.endpoint || ""),
          peer_key: overrides?.peer_key ?? peerKey,
          allowed_ips: overrides?.allowed_ips ?? allowedIPs,
          ...(overrides?.private_key || privateKey
            ? { private_key: overrides?.private_key ?? privateKey }
            : {}),
        }),
      });
      setPrivateKey("");
      setPeerKey("");
      setNotice("");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      // Reload in every case: a failed enable reverts the saved revision, so a
      // stale revision would otherwise cause a spurious conflict on retry.
      resource.reload();
      setBusy(false);
    }
  }
  async function uploadConfig(file?: File) {
    if (!file) return;
    setError("");
    try {
      const parsed = parseWireGuard(await file.text());
      setAddress(parsed.address);
      setEndpoint(parsed.endpoint);
      setPeerKey(parsed.peer_key);
      setPrivateKey(parsed.private_key);
      setAllowedIPs(parsed.allowed_ips);
      await save(true, parsed);
    } catch (e) {
      setError(e instanceof Error ? e.message : messageFor(e));
    }
  }
  async function toggleKillSwitch(next: boolean) {
    if (!data || busy) return;
    setBusy(true);
    setError("");
    try {
      await api("/vpn/mullvad", {
        method: "PUT",
        signal: AbortSignal.timeout(40000),
        body: JSON.stringify({ revision: data.revision, kill_switch: next }),
      });
      setKillSwitch(next);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      resource.reload();
      setBusy(false);
    }
  }
  return (
    <section className="rs-fieldset">
      <div className="rs-legend">
        <h2>Mullvad VPN (kill switch)</h2>
      </div>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {data && (
        <>
          <p>
            Optional WireGuard tunnel for the built-in torrent client. When the
            tunnel is down, that client stays stopped. Other app traffic uses
            the normal connection.
          </p>
          <p>
            Requires NET_ADMIN and <code>REELSCAREY_KEEP_NET_ADMIN=1</code>.
            Upload a Mullvad config to connect. The private key is stored
            encrypted.
          </p>
          <p className="rs-form-help">
            Upload a <code>.conf</code> file exported by Mullvad. The tunnel
            address may contain both IPv4 and IPv6 entries.
          </p>
          <label className="file-button">
            <input
              type="file"
              accept=".conf,text/plain"
              disabled={busy}
              onChange={(e) => void uploadConfig(e.target.files?.[0])}
            />
            Upload Mullvad config
          </label>
          <p>
            Status:{" "}
            <Badge
              color={
                data.state === "up"
                  ? "green"
                  : data.state === "failed"
                    ? "red"
                    : "gray"
              }
            >
              {vpnStates[data.state] ?? data.state}
            </Badge>
            {data.message ? ` · ${data.message}` : ""}
          </p>
          {notice && <p role="status">{notice}</p>}
          <label className="rs-check">
            <input
              type="checkbox"
              checked={killSwitch}
              disabled={busy}
              onChange={(e) => void toggleKillSwitch(e.target.checked)}
            />
            Kill switch: stop the built-in torrent client whenever the VPN
            tunnel is down
          </label>
          <p className="rs-form-help">
            Default on. When off, the built-in client may use the normal
            connection if the tunnel drops — only disable this if you accept
            that exposure.
          </p>
          <fieldset disabled={busy} className="backup-fields">
            <label>
              Tunnel address (IPv4 and optional IPv6, comma separated)
              <input
                value={address || data.address || ""}
                onChange={(e) => setAddress(e.target.value)}
                placeholder="10.66.123.45/32"
              />
            </label>
            <label>
              Server public key
              <input
                value={peerKey}
                onChange={(e) => setPeerKey(e.target.value)}
                placeholder={
                  data.endpoint ? "Saved — enter to change" : "From Mullvad"
                }
              />
            </label>
            <label>
              Peer endpoint (host:port)
              <input
                value={endpoint || data.endpoint || ""}
                onChange={(e) => setEndpoint(e.target.value)}
                placeholder="185.184.222.3:51820"
              />
            </label>
            <label>
              Allowed addresses{" "}
              <span className="rs-form-help">(optional, from the file)</span>
              <input
                value={allowedIPs}
                onChange={(e) => setAllowedIPs(e.target.value)}
                placeholder="0.0.0.0/0, ::/0"
              />
            </label>
            <label>
              Private key{" "}
              {data.present ? "(leave blank to keep the saved key)" : ""}
              <input
                type="password"
                value={privateKey}
                onChange={(e) => setPrivateKey(e.target.value)}
                autoComplete="off"
              />
            </label>
            {data.enabled ? (
              <Button
                variant="soft"
                disabled={busy}
                onClick={() => void save(false)}
              >
                Disable tunnel
              </Button>
            ) : (
              <Button disabled={busy} onClick={() => void save(true)}>
                Save and connect
              </Button>
            )}
          </fieldset>
          {error && (
            <p className="rs-alert is-danger" role="alert">
              {error}
            </p>
          )}
        </>
      )}
    </section>
  );
}
const builtinStates: Record<string, string> = {
  running: "Running",
  starting: "Starting…",
  stopped: "Stopped",
  failed: "Needs attention",
};
function BuiltinCard({ reload }: { reload: () => void }) {
  const resource = useResource<Builtin>("/download-clients/builtin");
  const [port, setPort] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function update(body: { enabled?: boolean; peer_port?: number }) {
    setBusy(true);
    setError("");
    try {
      await api("/download-clients/builtin", {
        method: "PUT",
        signal: AbortSignal.timeout(160000),
        body: JSON.stringify({
          revision: resource.data?.revision ?? 0,
          ...body,
        }),
      });
      reload();
      resource.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      // A failed enable can change the saved revision; refresh so a retry is
      // not rejected as a stale edit.
      resource.reload();
      setBusy(false);
    }
  }
  const data = resource.data;
  const enabled = data?.enabled ?? false;
  return (
    <section className="rs-fieldset">
      <div className="rs-legend">
        <h2>Built-in qBittorrent</h2>
      </div>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {data && (
        <>
          <p>
            The application supervises a private qBittorrent daemon inside its
            own container. Its API stays on the container's loopback interface,
            so nothing outside the app can reach it. Torrent data is written to
            the shared download folder and imported with the same verification
            as every other client. It is included and turned on by default, so a
            new installation can download without any setup.
          </p>
          <p>
            Status:{" "}
            <Badge
              color={
                data.state === "running"
                  ? "green"
                  : data.state === "failed"
                    ? "red"
                    : "gray"
              }
            >
              {builtinStates[data.state] ?? data.state}
            </Badge>
            {data.message ? ` · ${data.message}` : ""}
          </p>
          {enabled && data.active_jobs > 0 && (
            <p>
              {data.active_jobs} active{" "}
              {data.active_jobs === 1 ? "job" : "jobs"} use this client. It
              cannot be turned off until they finish.
            </p>
          )}
          <p className="rs-form-help">
            {enabled
              ? "Included and on by default: downloads use it unless another client is the torrent default. Turning it off clears that default, and ReelScarey then asks you to connect your own client."
              : "Turned off. ReelScarey asks you to connect your own download client until this is turned on again."}
          </p>
          <fieldset disabled={busy} className="backup-fields">
            {data.present ? (
              <>
                <label>
                  BitTorrent peer port
                  <input
                    type="number"
                    min={1024}
                    max={65535}
                    value={port}
                    placeholder={String(data.peer_port)}
                    onChange={(e) => setPort(e.target.value)}
                  />
                </label>
                <Button
                  variant="soft"
                  disabled={
                    busy || port === "" || Number(port) === data.peer_port
                  }
                  onClick={() => void update({ peer_port: Number(port) })}
                >
                  Change peer port
                </Button>
                {enabled ? (
                  <Button
                    variant="soft"
                    disabled={busy}
                    onClick={() => {
                      if (
                        window.confirm(
                          "Turn off the built-in qBittorrent client? Its torrent default is cleared and its downloads stay in the download folder for retry.",
                        )
                      )
                        void update({ enabled: false });
                    }}
                  >
                    Turn off
                  </Button>
                ) : (
                  <Button
                    disabled={busy}
                    onClick={() => {
                      if (
                        window.confirm(
                          "Turn on the built-in qBittorrent client? The app starts its daemon inside this container and downloads use it again.",
                        )
                      )
                        void update({ enabled: true });
                    }}
                  >
                    Turn on
                  </Button>
                )}
              </>
            ) : (
              <Button
                disabled={busy}
                onClick={() => {
                  if (
                    window.confirm(
                      "Set up the built-in qBittorrent client? The app creates its connection, starts the daemon inside this container and tests it.",
                    )
                  )
                    void update({ enabled: true });
                }}
              >
                Set up built-in client
              </Button>
            )}
          </fieldset>
          {error && (
            <p className="rs-alert is-danger" role="alert">
              {error}
            </p>
          )}
        </>
      )}
    </section>
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
    <form className="rs-stack" onSubmit={(e) => void save(e)}>
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
                      ? c.kind === "qbittorrent" || c.kind === "transmission"
                      : c.kind === "sabnzbd" || c.kind === "nzbget"),
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
        <p className="rs-alert is-danger" role="alert">
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
  onDone,
}: {
  client?: Client;
  isDefault?: boolean;
  root: string;
  reload: () => void;
  onDone?: () => void;
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
      onDone?.();
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
      if (remove) {
        reload();
        onDone?.();
      } else setNotice("Connection verified.");
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
        <p className="rs-form-help">
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
            <p className="rs-form-help">
              Existing jobs continue tracking their saved client while new
              downloads are paused. Address/folder changes are blocked while
              active jobs depend on them. Stopped jobs retain the client for
              retry.
            </p>
          </>
        )}
      </fieldset>
      {error && (
        <p className="rs-alert is-danger" role="alert">
          {error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
    </form>
  );
  return fields;
}
