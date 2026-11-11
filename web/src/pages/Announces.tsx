import { useState, type FormEvent } from "react";
import { Button } from "@radix-ui/themes";
import { Plus, Radio, RefreshCw } from "lucide-react";
import {
  Alert,
  ConfirmDialog,
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
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";
import "./Admin.css";

type Definition = {
  id: string;
  name: string;
  server: string;
  port: number;
  tls: boolean;
  channel: string;
  key_label: string;
  require_account: boolean;
};
type Source = {
  id: string;
  tracker: string;
  name: string;
  enabled: boolean;
  revision: number;
  last_error?: string;
  last_event_at: number;
  server: string;
  port: number;
  tls: boolean;
  channel: string;
  nick: string;
  account: string;
  has_password: boolean;
  has_key: boolean;
  key_label: string;
  require_account: boolean;
};
type Event = {
  id: number;
  source_id: string;
  tracker: string;
  release_name: string;
  category: string;
  size_bytes: number;
  freeleech?: string;
  torrent_id?: string;
  info_url?: string;
  received_at: number;
};
type Sources = { items: Source[]; definitions: Definition[] };
type Events = { items: Event[]; page: number; total: number; limit: number };
type Form = {
  tracker: string;
  name: string;
  nick: string;
  account: string;
  password: string;
  invite: string;
  key: string;
  enabled: boolean;
};

const emptyForm: Form = {
  tracker: "iptorrents",
  name: "IPTorrents",
  nick: "",
  account: "",
  password: "",
  invite: "",
  key: "",
  enabled: false,
};

function bytes(value: number) {
  if (!value) return "Size unknown";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit++;
  }
  return `${size >= 10 || unit === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[unit]}`;
}

export function Announces() {
  const sources = useResource<Sources>("/announce-sources");
  const events = useLiveResource<Events>("/announces?page=1", 10000);
  const [form, setForm] = useState<Form>(emptyForm);
  const [editing, setEditing] = useState<Source>();
  const [formOpen, setFormOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const def = sources.data?.definitions.find((d) => d.id === form.tracker);

  function chooseTracker(id: string) {
    const next = sources.data?.definitions.find((d) => d.id === id);
    setForm((current) => ({
      ...current,
      tracker: id,
      name: next?.name ?? current.name,
      key: "",
    }));
  }
  function add() {
    setEditing(undefined);
    setForm(emptyForm);
    setError("");
    setNotice("");
    setFormOpen(true);
  }
  function edit(source: Source) {
    setEditing(source);
    setFormOpen(true);
    setForm({
      tracker: source.tracker,
      name: source.name,
      nick: source.nick,
      account: source.account,
      password: "",
      invite: "",
      key: "",
      enabled: source.enabled,
    });
    setNotice("");
    setError("");
  }
  function reset() {
    setFormOpen(false);
    setEditing(undefined);
    setForm(emptyForm);
    setError("");
    setNotice("");
  }
  async function save(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const body = {
        tracker: form.tracker,
        name: form.name,
        nick: form.nick,
        account: form.account,
        password: form.password,
        invite: form.invite,
        ...(form.key ? { key: form.key } : {}),
        enabled: form.enabled,
        revision: editing?.revision ?? 0,
      };
      if (editing) {
        await api(`/announce-sources/${editing.id}`, {
          method: "PUT",
          body: JSON.stringify(body),
        });
      } else {
        await api("/announce-sources", {
          method: "POST",
          body: JSON.stringify(body),
        });
      }
      const wasEditing = !!editing;
      const wasEnabled = form.enabled;
      reset();
      setNotice(
        wasEditing
          ? "Source saved."
          : wasEnabled
            ? "Source added and enabled."
            : "Source added paused.",
      );
      sources.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function toggle(source: Source) {
    setBusy(true);
    setError("");
    try {
      await api(`/announce-sources/${source.id}`, {
        method: "PUT",
        body: JSON.stringify({
          tracker: source.tracker,
          name: source.name,
          nick: source.nick,
          account: source.account,
          enabled: !source.enabled,
          revision: source.revision,
        }),
      });
      sources.reload();
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
            icon={Plus}
            label="Add Source"
            disabled={!sources.data}
            onClick={add}
          />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={sources.loading || events.loading}
            onClick={() => {
              sources.reload();
              events.reload();
            }}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading
          title="Announce feeds"
          description="Watches tracker IRC announce channels and records new releases. Capture only: nothing is downloaded from here."
        />
        <RequestState
          loading={sources.loading || events.loading}
          error={sources.error || events.error}
          retry={() => {
            sources.reload();
            events.reload();
          }}
        />
        {error && !formOpen && <Alert kind="danger">{error}</Alert>}
        {notice && <Alert kind="success">{notice}</Alert>}
        <FieldSet legend="Sources">
          {sources.data && !sources.data.items.length ? (
            <EmptyState
              icon={Radio}
              title="No announce sources configured"
              actions={<Button onClick={add}>Add source</Button>}
            >
              Sources are saved paused and connect only when you enable them.
            </EmptyState>
          ) : (
            <Table label="Announce sources">
              <thead>
                <tr>
                  <th scope="col">Name</th>
                  <th scope="col">Channel</th>
                  <th scope="col">Nick</th>
                  <th scope="col">Last announce</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {sources.data?.items.map((source) => (
                  <tr key={source.id}>
                    <td>
                      <span className="rs-row">
                        <strong>{source.name}</strong>
                        <Label kind={source.enabled ? "success" : "disabled"}>
                          {source.enabled ? "Enabled" : "Paused"}
                        </Label>
                        {source.has_key && <Label outline>Key saved</Label>}
                      </span>
                      {source.last_error && (
                        <p className="rs-danger-text rs-small" role="alert">
                          {source.last_error}
                        </p>
                      )}
                    </td>
                    <td className="rs-small">
                      {source.channel}
                      <span className="rs-muted">
                        {" "}
                        · {source.server}:{source.port}
                      </span>
                    </td>
                    <td>{source.nick}</td>
                    <td className="rs-small">
                      {source.last_event_at
                        ? new Date(source.last_event_at * 1000).toLocaleString()
                        : "None yet"}
                    </td>
                    <td className="is-actions">
                      <span
                        className="rs-row"
                        style={{ justifyContent: "flex-end" }}
                      >
                        <Button
                          size="1"
                          variant="soft"
                          disabled={busy}
                          onClick={() => void toggle(source)}
                        >
                          {source.enabled ? "Pause" : "Connect"}
                        </Button>
                        <Button
                          size="1"
                          variant="soft"
                          disabled={busy}
                          onClick={() => edit(source)}
                        >
                          Edit
                        </Button>
                        <ConfirmDialog
                          trigger={
                            <Button
                              size="1"
                              variant="soft"
                              color="red"
                              disabled={busy}
                            >
                              Remove
                            </Button>
                          }
                          title={`Remove ${source.name}?`}
                          description="Remove the source and its captured announce history? The IRC password is deleted."
                          onConfirm={async () => {
                            setBusy(true);
                            setError("");
                            try {
                              await api(`/announce-sources/${source.id}`, {
                                method: "DELETE",
                                body: JSON.stringify({
                                  revision: source.revision,
                                }),
                              });
                              if (editing?.id === source.id) reset();
                              sources.reload();
                              events.reload();
                            } catch (e) {
                              setError(messageFor(e));
                            } finally {
                              setBusy(false);
                            }
                          }}
                        />
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </FieldSet>
        <FieldSet
          legend="Recent announces"
          description={`${events.data?.total ?? 0} stored announce${events.data?.total === 1 ? "" : "s"}; the newest 50 are shown.`}
        >
          {!events.data?.items.length ? (
            <p className="rs-muted">No announces captured.</p>
          ) : (
            <Table label="Recent announces">
              <thead>
                <tr>
                  <th scope="col">Release</th>
                  <th scope="col">Tracker</th>
                  <th scope="col">Category</th>
                  <th scope="col" className="is-numeric">
                    Size
                  </th>
                  <th scope="col">Received</th>
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Link</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {events.data.items.map((event) => (
                  <tr key={event.id}>
                    <td>
                      <span className="rs-row">
                        {event.release_name}
                        {event.freeleech && (
                          <Label kind="success">{event.freeleech}</Label>
                        )}
                      </span>
                    </td>
                    <td>{event.tracker}</td>
                    <td className="rs-small">{event.category || "—"}</td>
                    <td className="is-numeric">{bytes(event.size_bytes)}</td>
                    <td className="rs-small">
                      {new Date(event.received_at * 1000).toLocaleString()}
                    </td>
                    <td className="is-actions">
                      {event.info_url && (
                        <a
                          href={event.info_url}
                          target="_blank"
                          rel="noreferrer"
                        >
                          Open on tracker
                        </a>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </FieldSet>
        {formOpen && (
          <Modal
            open
            size="large"
            onOpenChange={(open) => !open && !busy && reset()}
            title={editing ? `Edit ${editing.name}` : "Add an announce source"}
          >
            <details className="indexer-help">
              <summary>How to get the IRC values</summary>
              <ol>
                <li>
                  Register a bot nick on the tracker’s IRC network and group it
                  with your account when the tracker requires that. Do not use
                  your main nick, and use a nick different from any other IRC
                  client or announce tool you run — two clients with the same
                  nick will disconnect each other.
                </li>
                <li>
                  <strong>NickServ password:</strong> your tracker account
                  password. It is sent only to NickServ over TLS.
                </li>
                <li>
                  <strong>NickServ account:</strong> leave blank when the
                  network accepts <code>IDENTIFY password</code>; fill it when
                  it requires <code>IDENTIFY account password</code>.
                </li>
                <li>
                  <strong>Invite command:</strong> open the tracker’s IRC page
                  and paste the full command (for example{" "}
                  <code>Millie announce YOURKEY</code>). BeyondHD and AnimeBytes
                  usually require this; the others normally do not.
                </li>
                <li>
                  The server, port, TLS and channel come from the tracker
                  definition and are fixed. Enable the source; connection errors
                  appear under it.
                </li>
              </ol>
            </details>

            {error && <Alert kind="danger">{error}</Alert>}
            <form className="rs-stack" onSubmit={(e) => void save(e)}>
              <fieldset disabled={busy} className="backup-fields">
                <label>
                  Tracker
                  <select
                    value={form.tracker}
                    disabled={!!editing}
                    onChange={(e) => chooseTracker(e.target.value)}
                  >
                    {(sources.data?.definitions ?? []).map((d) => (
                      <option key={d.id} value={d.id}>
                        {d.name}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  Source name
                  <input
                    value={form.name}
                    onChange={(e) => setForm({ ...form, name: e.target.value })}
                    maxLength={80}
                    required
                  />
                </label>
                <label>
                  IRC nick
                  <input
                    value={form.nick}
                    onChange={(e) => setForm({ ...form, nick: e.target.value })}
                    placeholder="e.g. reelscarey_bot"
                    maxLength={32}
                    required
                  />
                </label>
                <label>
                  NickServ account{" "}
                  <span className="rs-form-help">(when required)</span>
                  <input
                    value={form.account}
                    onChange={(e) =>
                      setForm({ ...form, account: e.target.value })
                    }
                    maxLength={64}
                  />
                </label>
                <label>
                  NickServ password{" "}
                  {editing?.has_password ? "(leave blank to keep)" : ""}
                  <input
                    type="password"
                    value={form.password}
                    onChange={(e) =>
                      setForm({ ...form, password: e.target.value })
                    }
                    autoComplete="off"
                    maxLength={256}
                  />
                </label>
                <label>
                  {def?.key_label || "Tracker key"}{" "}
                  {editing?.has_key ? "(saved; leave blank to keep)" : ""}
                  <input
                    type="password"
                    value={form.key}
                    onChange={(e) => setForm({ ...form, key: e.target.value })}
                    autoComplete="off"
                    maxLength={512}
                    placeholder={
                      form.tracker === "iptorrents"
                        ? "From your IPTorrents details page"
                        : "From your tracker account"
                    }
                  />
                </label>
                <p className="rs-form-help">
                  Encrypted. Used for automatic downloads when that step is
                  enabled. For IPTorrents, copy the passkey from your details
                  page.
                </p>
                <label>
                  Invite command{" "}
                  <span className="rs-form-help">
                    (BeyondHD/AnimeBytes; optional)
                  </span>
                  <input
                    value={form.invite}
                    onChange={(e) =>
                      setForm({ ...form, invite: e.target.value })
                    }
                    placeholder="e.g. Millie announce YOURKEY"
                    maxLength={512}
                  />
                </label>
                <label className="rs-check">
                  <input
                    type="checkbox"
                    checked={form.enabled}
                    onChange={(e) =>
                      setForm({ ...form, enabled: e.target.checked })
                    }
                  />
                  Connect this source now
                </label>
                <p className="rs-form-help">
                  New sources start paused. A paused source keeps its
                  credentials without opening an IRC connection.
                </p>
                {def && (
                  <p className="rs-form-help">
                    {def.tls ? "TLS" : "Plain"} · {def.server}:{def.port} ·{" "}
                    {def.channel}
                    {def.require_account ? " · NickServ account required" : ""}
                  </p>
                )}
                <div className="rs-form-actions">
                  <Button type="submit" disabled={busy}>
                    {busy ? "Saving…" : editing ? "Save source" : "Add source"}
                  </Button>
                  {editing && (
                    <Button
                      type="button"
                      variant="soft"
                      disabled={busy}
                      onClick={reset}
                    >
                      Cancel
                    </Button>
                  )}
                </div>
              </fieldset>
            </form>
          </Modal>
        )}
      </PageBody>
    </>
  );
}
