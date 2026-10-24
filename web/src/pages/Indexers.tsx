import { useState, type FormEvent } from "react";
import {
  AlertDialog,
  Badge,
  Button,
  Dialog,
  Switch,
  TextField,
} from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { Plus, Radio, ShieldCheck } from "lucide-react";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";

export type Indexer = {
  id: string;
  name: string;
  kind: string;
  url: string;
  enabled: boolean;
  revision: number;
  user_agent: string;
  freeleech_only: boolean;
  has_cookie: boolean;
  has_feed: boolean;
  has_api_key: boolean;
  legacy: boolean;
  last_test_at: number;
  last_search_at: number;
  last_error: string;
  last_result_count: number;
};
const kinds = [
  {
    id: "iptorrents",
    name: "IPTorrents",
    url: "https://iptorrents.com",
    help: "Direct website search using your existing signed-in session. No Prowlarr or Jackett required.",
  },
  {
    id: "xspeeds",
    name: "XSpeeds",
    url: "https://www.xspeeds.eu",
    help: "Direct website search using your existing signed-in session. No intermediary required.",
  },
  {
    id: "nyaa",
    name: "NYAA",
    url: "https://nyaa.si",
    help: "Public searchable RSS. No account or API key required. Automatic anime episode matching still requires an unambiguous season/episode release name.",
  },
  {
    id: "torznab",
    name: "Torznab",
    url: "",
    help: "Connect a Torznab API directly, or use an optional Prowlarr/Jackett endpoint.",
  },
  {
    id: "rss",
    name: "Private torrent RSS",
    url: "",
    help: "Read and filter the entries currently in a torrent feed. This does not search the tracker's complete archive.",
  },
];
export function Indexers() {
  const list = useResource<{ items: Indexer[] }>("/indexers");
  const [editing, setEditing] = useState<Indexer | null | undefined>();
  const [pending, setPending] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  async function action(item: Indexer, op: "test" | "enabled" | "remove") {
    setPending(item.id);
    setError("");
    setMessage("");
    try {
      await api(`/indexers/${item.id}${op === "remove" ? "" : `/${op}`}`, {
        method: op === "test" ? "POST" : op === "enabled" ? "PUT" : "DELETE",
        body: JSON.stringify(
          op === "test"
            ? {}
            : op === "enabled"
              ? { revision: item.revision, enabled: !item.enabled }
              : { revision: item.revision },
        ),
      });
      setMessage(
        op === "test"
          ? `${item.name} connection verified.`
          : op === "remove"
            ? `${item.name} removed.`
            : `${item.name} ${item.enabled ? "disabled" : "enabled"}.`,
      );
      list.reload();
    } catch (e) {
      setError(messageFor(e));
      list.reload();
    } finally {
      setPending("");
    }
  }
  return (
    <>
      <PageHeading
        eyebrow="YOUR SOURCES, CONNECTED"
        title="Torrent indexers"
        description="Connect trackers directly or add Torznab and RSS sources. Search results keep their source, and downloads use the connection they came from."
      >
        <Button onClick={() => setEditing(null)}>
          <Plus size={17} />
          Add indexer
        </Button>
      </PageHeading>
      <RequestState
        loading={list.loading}
        error={list.error}
        retry={list.reload}
      />
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
      {message && (
        <p role="status" className="success-box">
          {message}
        </p>
      )}
      {list.data?.items.length === 0 && (
        <section className="panel system-panel">
          <Radio size={27} />
          <h2>Add your first indexer</h2>
          <p>
            IPTorrents and XSpeeds connect directly with a saved session. NYAA
            is public. Your library stays available while you set up sources.
          </p>
          <Button onClick={() => setEditing(null)}>Choose an indexer</Button>
        </section>
      )}
      <div className="indexer-grid">
        {list.data?.items.map((item) => (
          <section className="panel system-panel indexer-card" key={item.id}>
            <div className="indexer-title">
              <h2>{item.name}</h2>
              <Badge
                color={
                  !item.enabled ? "gray" : item.last_error ? "amber" : "green"
                }
              >
                {!item.enabled
                  ? "Disabled"
                  : item.last_error
                    ? "Needs attention"
                    : "Enabled"}
              </Badge>
            </div>
            <p>
              {kinds.find((k) => k.id === item.kind)?.name} · {item.url}
            </p>
            {item.last_error && (
              <p className="movie-error">{item.last_error}</p>
            )}
            <p className="field-help">
              {item.last_test_at
                ? `Last verified ${new Date(item.last_test_at * 1000).toLocaleString()}`
                : "Use Test connection to verify this source."}
              {item.last_search_at
                ? ` · Last search: ${item.last_result_count} results`
                : ""}
            </p>
            {item.freeleech_only && <Badge>Freeleech only</Badge>}
            <div className="indexer-actions">
              {!item.legacy && (
                <label className="monitor-choice">
                  <Switch
                    aria-label={`Enable ${item.name}`}
                    checked={item.enabled}
                    disabled={!!pending}
                    onCheckedChange={() => void action(item, "enabled")}
                  />
                  Use in searches
                </label>
              )}
              <Button
                variant="soft"
                disabled={!!pending}
                onClick={() => void action(item, "test")}
              >
                <ShieldCheck size={16} />
                {pending === item.id ? "Working…" : "Test connection"}
              </Button>
              {item.legacy ? (
                <Button asChild variant="soft">
                  <Link to="/settings#torznab">Edit existing connection</Link>
                </Button>
              ) : (
                <Button
                  variant="soft"
                  disabled={!!pending}
                  onClick={() => setEditing(item)}
                >
                  Edit
                </Button>
              )}
              <AlertDialog.Root>
                <AlertDialog.Trigger>
                  <Button variant="ghost" color="red" disabled={!!pending}>
                    Remove
                  </Button>
                </AlertDialog.Trigger>
                <AlertDialog.Content>
                  <AlertDialog.Title>Remove {item.name}?</AlertDialog.Title>
                  <AlertDialog.Description>
                    The saved connection and its credentials will be removed.
                    Existing downloads and library files remain.
                  </AlertDialog.Description>
                  <div className="indexer-actions">
                    <AlertDialog.Cancel>
                      <Button variant="soft">Keep indexer</Button>
                    </AlertDialog.Cancel>
                    <AlertDialog.Action>
                      <Button
                        color="red"
                        onClick={() => void action(item, "remove")}
                      >
                        Remove indexer
                      </Button>
                    </AlertDialog.Action>
                  </div>
                </AlertDialog.Content>
              </AlertDialog.Root>
            </div>
          </section>
        ))}
      </div>
      <p className="search-disclaimer">
        Disabling an indexer stops new searches and release selections from it.
        Existing downloads continue and torrent sources remain available for
        seeding. Browser challenges must be completed on the tracker; ReelScarey
        does not solve them automatically.
      </p>
      <Dialog.Root
        open={editing !== undefined}
        onOpenChange={(open) => {
          if (!open) setEditing(undefined);
        }}
      >
        <Dialog.Content maxWidth="640px">
          <Dialog.Title>
            {editing ? `Edit ${editing.name}` : "Add a torrent indexer"}
          </Dialog.Title>
          <Dialog.Description>
            Credentials are encrypted locally and never shown again after
            saving.
          </Dialog.Description>
          {editing !== undefined && (
            <IndexerForm
              key={editing?.id ?? "new"}
              initial={editing}
              done={() => {
                setEditing(undefined);
                setMessage("Indexer verified and saved.");
                list.reload();
              }}
            />
          )}
        </Dialog.Content>
      </Dialog.Root>
    </>
  );
}
function IndexerForm({
  initial,
  done,
}: {
  initial: Indexer | null;
  done: () => void;
}) {
  const [kind, setKind] = useState(initial?.kind ?? "iptorrents");
  const [name, setName] = useState(initial?.name ?? "IPTorrents");
  const [url, setURL] = useState(initial?.url ?? kinds[0].url);
  const [agent, setAgent] = useState(initial?.user_agent ?? "");
  const [cookie, setCookie] = useState("");
  const [key, setKey] = useState("");
  const [feed, setFeed] = useState("");
  const [free, setFree] = useState(initial?.freeleech_only ?? false);
  const [enabled, setEnabled] = useState(initial?.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [clearCookie, setClearCookie] = useState(false);
  const [clearKey, setClearKey] = useState(false);
  const privateSite = kind === "iptorrents" || kind === "xspeeds";
  function changeKind(value: string) {
    const selected = kinds.find((k) => k.id === value)!;
    setKind(value);
    setName(selected.name);
    setURL(selected.url);
    setAgent("");
    setCookie("");
    setKey("");
    setFeed("");
    setFree(false);
    setClearCookie(false);
    setClearKey(false);
    setError("");
  }
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api(initial ? `/indexers/${initial.id}` : "/indexers", {
        method: initial ? "PUT" : "POST",
        body: JSON.stringify({
          name,
          kind,
          url,
          enabled,
          revision: initial?.revision ?? 0,
          user_agent: privateSite || kind === "rss" ? agent : "",
          freeleech_only: privateSite && free,
          cookie:
            privateSite || kind === "rss"
              ? clearCookie
                ? ""
                : cookie || undefined
              : undefined,
          api_key:
            kind === "torznab" ? (clearKey ? "" : key || undefined) : undefined,
          feed_url: kind === "rss" ? feed || undefined : undefined,
        }),
      });
      setCookie("");
      setKey("");
      setFeed("");
      done();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="indexer-form" onSubmit={submit}>
      <label>
        Indexer type
        <select
          aria-label="Indexer type"
          value={kind}
          disabled={!!initial || busy}
          onChange={(e) => changeKind(e.target.value)}
        >
          {kinds.map((k) => (
            <option key={k.id} value={k.id}>
              {k.name}
            </option>
          ))}
        </select>
      </label>
      <p className="field-help">{kinds.find((k) => k.id === kind)?.help}</p>
      <label>
        Name
        <TextField.Root
          aria-label="Indexer name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={80}
          required
          disabled={busy}
        />
      </label>
      <label>
        {kind === "torznab" ? "Torznab API endpoint" : "Website URL"}
        <TextField.Root
          aria-label="Indexer URL"
          value={url}
          onChange={(e) => setURL(e.target.value)}
          required
          disabled={busy}
        />
      </label>
      {kind === "torznab" && (
        <>
          <label>
            API key
            <TextField.Root
              aria-label="Indexer API key"
              type="password"
              autoComplete="off"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              disabled={busy}
            />
          </label>
          <p className="field-help">
            {initial?.has_api_key
              ? "A key is saved. Leave blank to keep it."
              : "Leave blank only if this endpoint allows public access."}
          </p>
          {initial?.has_api_key && (
            <label>
              <input
                type="checkbox"
                checked={clearKey}
                onChange={(e) => setClearKey(e.target.checked)}
              />
              Clear saved API key
            </label>
          )}
        </>
      )}
      {kind === "rss" && (
        <>
          <label>
            Private RSS feed URL
            <TextField.Root
              aria-label="Private RSS feed URL"
              type="password"
              autoComplete="off"
              value={feed}
              onChange={(e) => setFeed(e.target.value)}
              disabled={busy}
            />
          </label>
          <p className="field-help">
            {initial?.has_feed
              ? "A private URL is saved. Leave blank to keep it."
              : "Paste the complete feed URL, including its passkey. It must belong to the website above."}
          </p>
        </>
      )}
      {(privateSite || kind === "rss") && (
        <>
          <label>
            Session cookie{!privateSite && " (optional)"}
            <TextField.Root
              aria-label="Session cookie"
              type="password"
              autoComplete="off"
              value={cookie}
              onChange={(e) => setCookie(e.target.value)}
              disabled={busy}
              maxLength={12000}
            />
          </label>
          <p className="field-help">
            {initial?.has_cookie
              ? "A session is saved. Leave blank to keep it. "
              : ""}
            Use the Cookie header value from your signed-in tracker browser
            session. Keep it out of chat.
          </p>
          {kind === "rss" && initial?.has_cookie && (
            <label>
              <input
                type="checkbox"
                checked={clearCookie}
                onChange={(e) => setClearCookie(e.target.checked)}
              />
              Clear saved cookie
            </label>
          )}
          <label>
            Browser user agent
            <TextField.Root
              aria-label="Browser user agent"
              value={agent}
              onChange={(e) => setAgent(e.target.value)}
              maxLength={512}
              required={privateSite}
              disabled={busy}
            />
          </label>
        </>
      )}
      {privateSite && (
        <label className="monitor-choice">
          <Switch checked={free} onCheckedChange={setFree} />
          Freeleech releases only
        </label>
      )}
      {kind === "iptorrents" && (
        <p className="field-help">
          For the tracker’s torrent table, choose Icons for categories and
          enable the files-count column in your IPTorrents settings.
        </p>
      )}
      <label className="monitor-choice">
        <Switch checked={enabled} onCheckedChange={setEnabled} />
        Use this indexer in searches
      </label>
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
      <div className="indexer-actions">
        <Button type="submit" disabled={busy}>
          {busy ? "Testing connection…" : "Save & test connection"}
        </Button>
        <Dialog.Close>
          <Button type="button" variant="soft" disabled={busy}>
            Cancel
          </Button>
        </Dialog.Close>
      </div>
    </form>
  );
}
