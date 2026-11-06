import { useState, type FormEvent } from "react";
import { Badge, Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";

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
  function edit(source: Source) {
    setEditing(source);
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
  async function remove(source: Source) {
    if (
      !window.confirm(
        `Remove ${source.name} and its captured announce history? The IRC password is deleted.`,
      )
    )
      return;
    setBusy(true);
    setError("");
    try {
      await api(`/announce-sources/${source.id}`, {
        method: "DELETE",
        body: JSON.stringify({ revision: source.revision }),
      });
      if (editing?.id === source.id) reset();
      sources.reload();
      events.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeading title="Announce feeds">
        <Badge color="gray">Capture only</Badge>
      </PageHeading>
      <details className="indexer-help">
        <summary>How to get the IRC values</summary>
        <ol>
          <li>
            Register a bot nick on the tracker’s IRC network and group it with
            your account when the tracker requires that. Do not use your main
            nick, and use a nick different from any other IRC client or announce
            tool you run — two clients with the same nick will disconnect each
            other.
          </li>
          <li>
            <strong>NickServ password:</strong> your tracker account password.
            It is sent only to NickServ over TLS.
          </li>
          <li>
            <strong>NickServ account:</strong> leave blank when the network
            accepts <code>IDENTIFY password</code>; fill it when it requires{" "}
            <code>IDENTIFY account password</code>.
          </li>
          <li>
            <strong>Invite command:</strong> open the tracker’s IRC page and
            paste the full command (for example{" "}
            <code>Millie announce YOURKEY</code>). BeyondHD and AnimeBytes
            usually require this; the others normally do not.
          </li>
          <li>
            The server, port, TLS and channel come from the tracker definition
            and are fixed. Enable the source; connection errors appear under it.
          </li>
        </ol>
      </details>
      <RequestState
        loading={sources.loading || events.loading}
        error={sources.error || events.error}
        retry={() => {
          sources.reload();
          events.reload();
        }}
      />
      {error && (
        <p className="error-box" role="alert">
          {error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
      <section className="panel system-panel">
        <h2>{editing ? `Edit ${editing.name}` : "Add an announce source"}</h2>
        <form className="settings-form" onSubmit={(e) => void save(e)}>
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
              <span className="field-help">(when required)</span>
              <input
                value={form.account}
                onChange={(e) => setForm({ ...form, account: e.target.value })}
                maxLength={64}
              />
            </label>
            <label>
              NickServ password{" "}
              {editing?.has_password ? "(leave blank to keep)" : ""}
              <input
                type="password"
                value={form.password}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
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
            <p className="field-help">
              Encrypted. Used for automatic downloads when that step is enabled.
              For IPTorrents, copy the passkey from your details page.
            </p>
            <label>
              Invite command{" "}
              <span className="field-help">
                (BeyondHD/AnimeBytes; optional)
              </span>
              <input
                value={form.invite}
                onChange={(e) => setForm({ ...form, invite: e.target.value })}
                placeholder="e.g. Millie announce YOURKEY"
                maxLength={512}
              />
            </label>
            <label className="checkbox-row">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(e) =>
                  setForm({ ...form, enabled: e.target.checked })
                }
              />
              Connect this source now
            </label>
            <p className="field-help">
              New sources start paused. A paused source keeps its credentials
              without opening an IRC connection.
            </p>
            {def && (
              <p className="field-help">
                {def.tls ? "TLS" : "Plain"} · {def.server}:{def.port} ·{" "}
                {def.channel}
                {def.require_account ? " · NickServ account required" : ""}
              </p>
            )}
            <div className="form-footer">
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
      </section>
      <section className="panel system-panel">
        <h2>Sources</h2>
        {!sources.data?.items.length && <p>No announce sources configured.</p>}
        {sources.data?.items.map((source) => (
          <div key={source.id} className="announce-source">
            <div>
              <strong>{source.name}</strong>{" "}
              <Badge color={source.enabled ? "green" : "gray"}>
                {source.enabled ? "Enabled" : "Paused"}
              </Badge>
              <p className="field-help">
                {source.tracker} · {source.server}:{source.port} ·{" "}
                {source.channel} · nick {source.nick}
                {source.has_key ? " · key saved" : ""}
              </p>
              {source.last_error && (
                <p className="error-box" role="alert">
                  {source.last_error}
                </p>
              )}
              <p className="field-help">
                Last announce:{" "}
                {source.last_event_at
                  ? new Date(source.last_event_at * 1000).toLocaleString()
                  : "none yet"}
              </p>
            </div>
            <div className="form-footer">
              <Button
                variant="soft"
                disabled={busy}
                onClick={() => edit(source)}
              >
                Edit
              </Button>
              <Button
                variant="soft"
                disabled={busy}
                onClick={() => void toggle(source)}
              >
                {source.enabled ? "Pause" : "Connect"}
              </Button>
              <Button
                variant="soft"
                color="red"
                disabled={busy}
                onClick={() => void remove(source)}
              >
                Remove
              </Button>
            </div>
          </div>
        ))}
      </section>
      <section className="panel system-panel">
        <div className="form-footer">
          <h2>Recent announces</h2>
          <Button variant="soft" onClick={events.reload}>
            Refresh
          </Button>
        </div>
        <p className="field-help">
          {events.data?.total ?? 0} stored announce
          {events.data?.total === 1 ? "" : "s"}; the newest 50 are shown.
        </p>
        {!events.data?.items.length && <p>No announces captured.</p>}
        {events.data?.items.map((event) => (
          <div key={event.id} className="announce-event">
            <div>
              <strong>{event.release_name}</strong>
              {event.freeleech && (
                <>
                  {" "}
                  <Badge color="green">{event.freeleech}</Badge>
                </>
              )}
              <p className="field-help">
                {event.tracker}
                {event.category ? ` · ${event.category}` : ""} ·{" "}
                {bytes(event.size_bytes)} ·{" "}
                {new Date(event.received_at * 1000).toLocaleString()}
              </p>
            </div>
            {event.info_url && (
              <a
                className="text-link"
                href={event.info_url}
                target="_blank"
                rel="noreferrer"
              >
                Open on tracker
              </a>
            )}
          </div>
        ))}
      </section>
    </>
  );
}
