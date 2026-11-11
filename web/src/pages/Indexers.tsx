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
import { Plus, Radio, RefreshCw, ShieldCheck } from "lucide-react";
import {
  AddCard,
  Alert,
  EmptyState,
  PageBody,
  PageToolbar,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import "./Admin.css";

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
  has_definition: boolean;
  legacy: boolean;
  last_test_at: number;
  last_search_at: number;
  last_error: string;
  last_result_count: number;
};
type IndexerPreset = {
  id: string;
  label: string;
  name: string;
  kind: "torznab" | "newznab";
  url: string;
  prowlarr?: boolean;
};
// Presets only fill the existing add form. They are never sent to the API.
const presets: IndexerPreset[] = [
  {
    id: "nzbgeek",
    label: "NZBGeek (Usenet)",
    name: "NZBGeek",
    kind: "newznab",
    url: "https://api.nzbgeek.info/api",
  },
  {
    id: "prowlarr-torrent",
    label: "Prowlarr (Torrent)",
    name: "Prowlarr (Torrent)",
    kind: "torznab",
    url: "http://prowlarr:9696/{indexerId}/api",
    prowlarr: true,
  },
  {
    id: "prowlarr-usenet",
    label: "Prowlarr (Usenet)",
    name: "Prowlarr (Usenet)",
    kind: "newznab",
    url: "http://prowlarr:9696/{indexerId}/api",
    prowlarr: true,
  },
];
const kinds = [
  {
    id: "newznab",
    name: "Newznab (Usenet)",
    url: "",
    help: "Connect the complete Newznab API endpoint from your Usenet indexer. NZB releases download through SABnzbd.",
  },
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
  {
    id: "cardigann",
    name: "Cardigann definition",
    url: "",
    help: "Paste a Cardigann YAML definition and its homepage URL. ReelScarey implements a conservative subset of HTML search, field filters, cookie sessions and torrent/magnet downloads.",
  },
];

// Header values are commonly copied with their labels and line breaks. Remove
// those before sending; the value itself is kept exactly otherwise.
function cleanHeader(value: string) {
  return value
    .replace(/^(?:cookie|user-agent)\s*:\s*/i, "")
    .replace(/[\r\n]+/g, " ")
    .trim();
}

function IndexerHelp({ kind }: { kind: string }) {
  if (kind === "iptorrents" || kind === "xspeeds") {
    const home =
      kind === "iptorrents"
        ? "https://iptorrents.com"
        : "https://www.xspeeds.eu";
    return (
      <details className="indexer-help">
        <summary>How to get the session cookie and user agent</summary>
        <ol>
          <li>
            Sign in at the exact tracker address in the browser and on the
            network you will keep using. Tracker sessions can be bound to your
            public IP; the ReelScarey server must use the same one.
          </li>
          <li>
            Open DevTools (F12) → Network, reload the page, then click a torrent
            listing or search request to the tracker host (not a CDN host).
          </li>
          <li>
            Open <strong>Request Headers</strong>. Copy the <code>cookie</code>{" "}
            value only — not the <code>Cookie:</code> label, and not{" "}
            <code>document.cookie</code> (session cookies are HttpOnly). Keep it
            on one line.
          </li>
          <li>
            Copy the full <code>user-agent</code> value from the same request.
          </li>
          <li>
            Set the Website URL to the exact host from the address bar, with no
            path or trailing slash: <code>{home}</code>.
          </li>
          <li>
            Complete any browser challenge first. Redirects and challenges are
            not followed automatically. Re-enter the cookie if you change the
            URL, user agent or browser.
          </li>
        </ol>
      </details>
    );
  }
  if (kind === "nyaa") {
    return (
      <div className="indexer-help">
        <strong>No credentials are needed.</strong> Use{" "}
        <code>https://nyaa.si</code>. It is a public searchable RSS source.
      </div>
    );
  }
  if (kind === "rss") {
    return (
      <div className="indexer-help">
        <strong>Private feed:</strong> open the feed in the signed-in browser,
        copy the complete URL including its passkey, and keep the Website URL on
        the same origin. Add a cookie or user agent only if the feed requires
        them.
      </div>
    );
  }
  if (kind === "torznab" || kind === "newznab") {
    return (
      <div className="indexer-help">
        <strong>{kind === "newznab" ? "Newznab" : "Torznab"}:</strong> copy the
        full API endpoint from the provider (often ending in{" "}
        <code>/api?apikey=…</code>) and the API key. The host must be reachable
        from the ReelScarey server.
      </div>
    );
  }
  return null;
}
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
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={Plus}
            label="Add Indexer"
            onClick={() => setEditing(null)}
          />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={list.loading}
            onClick={list.reload}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title="Indexers" />
        <RequestState
          loading={list.loading}
          error={list.error}
          retry={list.reload}
        />
        {error && <Alert kind="danger">{error}</Alert>}
        {message && <Alert kind="success">{message}</Alert>}
        {list.data?.items.length === 0 && (
          <EmptyState
            icon={Radio}
            title="Add your first indexer"
            actions={
              <Button onClick={() => setEditing(null)}>
                Choose an indexer
              </Button>
            }
          >
            Torrent trackers, private RSS feeds, Torznab and Newznab sources.
          </EmptyState>
        )}
        <div className="indexer-grid">
          {list.data?.items.map((item) => (
            <section className="rs-panel rs-stack indexer-card" key={item.id}>
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
                <p className="rs-form-error">{item.last_error}</p>
              )}
              <p className="rs-form-help">
                {item.last_test_at
                  ? `Last verified ${new Date(item.last_test_at * 1000).toLocaleString()}`
                  : "Use Test connection to verify this source."}
                {item.last_search_at
                  ? ` · Last search: ${item.last_result_count} results`
                  : ""}
              </p>
              {item.freeleech_only && <Badge>Freeleech only</Badge>}
              <div className="rs-form-actions">
                {!item.legacy && (
                  <label className="rs-check">
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
                    <Link to="/settings/clients#torznab">
                      Edit existing connection
                    </Link>
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
                    <div className="rs-form-actions">
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
          {!!list.data?.items.length && (
            <AddCard label="Add indexer" onClick={() => setEditing(null)} />
          )}
        </div>
        <p className="search-disclaimer">
          Disabling an indexer stops new searches and release selections from
          it. Existing downloads continue and torrent sources remain available
          for seeding. Browser challenges must be completed on the tracker;
          ReelScarey does not solve them automatically.
        </p>
        <Dialog.Root
          open={editing !== undefined}
          onOpenChange={(open) => {
            if (!open) setEditing(undefined);
          }}
        >
          <Dialog.Content maxWidth="640px">
            <Dialog.Title>
              {editing ? `Edit ${editing.name}` : "Add an indexer"}
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
      </PageBody>
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
  const [presetID, setPresetID] = useState("");
  const preset = presets.find((value) => value.id === presetID);
  const [kind, setKind] = useState(initial?.kind ?? "iptorrents");
  const [name, setName] = useState(initial?.name ?? "IPTorrents");
  const [url, setURL] = useState(initial?.url ?? "https://iptorrents.com");
  const [agent, setAgent] = useState(initial?.user_agent ?? "");
  const [cookie, setCookie] = useState("");
  const [key, setKey] = useState("");
  const [feed, setFeed] = useState("");
  const [definition, setDefinition] = useState("");
  const [settings, setSettings] = useState("");
  const [free, setFree] = useState(initial?.freeleech_only ?? false);
  const [enabled, setEnabled] = useState(initial?.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [clearCookie, setClearCookie] = useState(false);
  const [clearKey, setClearKey] = useState(false);
  const privateSite = kind === "iptorrents" || kind === "xspeeds";
  const apiIndexer = kind === "torznab" || kind === "newznab";
  const cookieSite = privateSite || kind === "rss" || kind === "cardigann";
  function changeKind(value: string) {
    const selected = kinds.find((k) => k.id === value)!;
    setPresetID("");
    setKind(value);
    setName(selected.name);
    setURL(selected.url);
    setAgent("");
    setCookie("");
    setKey("");
    setFeed("");
    setDefinition("");
    setSettings("");
    setFree(false);
    setClearCookie(false);
    setClearKey(false);
    setError("");
  }
  function applyPreset(value: string) {
    setPresetID(value);
    const selected = presets.find((preset) => preset.id === value);
    if (!selected) return;
    setKind(selected.kind);
    setName(selected.name);
    setURL(selected.url);
    setError("");
  }
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    const cleanedCookie = cleanHeader(cookie);
    const cleanedAgent = cleanHeader(agent);
    let parsedSettings: Record<string, string> | undefined;
    if (kind === "cardigann" && settings.trim()) {
      try {
        const parsed: unknown = JSON.parse(settings);
        if (
          !parsed ||
          typeof parsed !== "object" ||
          Array.isArray(parsed) ||
          Object.values(parsed).some((value) => typeof value !== "string")
        ) {
          throw new Error("settings");
        }
        parsedSettings = parsed as Record<string, string>;
      } catch {
        setBusy(false);
        setError(
          'Cardigann settings must be a flat JSON object with string values, such as {"username":"owner"}.',
        );
        return;
      }
    }
    try {
      await api(initial ? `/indexers/${initial.id}` : "/indexers", {
        method: initial ? "PUT" : "POST",
        body: JSON.stringify({
          name: name.trim(),
          kind,
          url: url.trim(),
          enabled,
          revision: initial?.revision ?? 0,
          user_agent: cookieSite ? cleanedAgent : "",
          freeleech_only: privateSite && free,
          cookie: cookieSite
            ? clearCookie
              ? ""
              : cleanedCookie || undefined
            : undefined,
          api_key:
            apiIndexer || kind === "cardigann"
              ? clearKey
                ? ""
                : key.trim() || undefined
              : undefined,
          feed_url: kind === "rss" ? feed.trim() || undefined : undefined,
          definition: kind === "cardigann" ? definition.trim() : undefined,
          settings: kind === "cardigann" ? parsedSettings : undefined,
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
      {!initial && (
        <>
          <label className="indexer-preset">
            Preset
            <select
              aria-label="Indexer preset"
              value={presetID}
              disabled={busy}
              onChange={(e) => applyPreset(e.target.value)}
            >
              <option value="">Custom indexer</option>
              {presets.map((value) => (
                <option key={value.id} value={value.id}>
                  {value.label}
                </option>
              ))}
            </select>
          </label>
          {preset?.prowlarr && (
            <p className="rs-form-help indexer-preset-help">
              Replace <code>{"{indexerId}"}</code> with the numeric Prowlarr
              indexer ID shown in Prowlarr&apos;s indexer settings.
            </p>
          )}
        </>
      )}
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
      <p className="rs-form-help">{kinds.find((k) => k.id === kind)?.help}</p>
      <IndexerHelp kind={kind} />
      {kind === "cardigann" && (
        <label>
          Cardigann definition (YAML)
          <textarea
            aria-label="Cardigann YAML definition"
            className="rs-code-input"
            rows={12}
            value={definition}
            onChange={(e) => setDefinition(e.target.value)}
            disabled={busy}
            spellCheck={false}
            placeholder="id: example&#10;name: Example&#10;search:&#10;  path: /search&#10;  rows:&#10;    selector: tr.release&#10;  fields:&#10;    title: {selector: a.title}&#10;    download: {selector: a.download, attribute: href}"
          />
          <span className="rs-muted rs-small">
            Existing indexers cannot reveal a saved definition. Paste it again
            when editing. Supported: HTML search rows, common filters, cookie
            headers and torrent/magnet links.
          </span>
          <div>
            Cardigann settings (JSON, optional)
            <textarea
              aria-label="Cardigann settings JSON"
              className="rs-code-input"
              rows={4}
              value={settings}
              onChange={(e) => setSettings(e.target.value)}
              disabled={busy}
              spellCheck={false}
              placeholder='{"username":"owner"}'
            />
            <span className="rs-muted rs-small">
              Per-connection values for definition settings. They are encrypted
              with the indexer configuration and never returned by the API.
            </span>
          </div>
        </label>
      )}
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
        {apiIndexer
          ? `${kind === "newznab" ? "Newznab" : "Torznab"} API endpoint`
          : privateSite
            ? "Website URL (exact host, no path)"
            : "Website URL"}
        <TextField.Root
          aria-label="Indexer URL"
          value={url}
          onChange={(e) => setURL(e.target.value)}
          required
          disabled={busy}
        />
      </label>
      {apiIndexer && (
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
              required={!initial?.has_api_key && !!preset}
            />
          </label>
          <p className="rs-form-help">
            {initial?.has_api_key
              ? "A key is saved. Leave blank to keep it."
              : preset
                ? "This preset requires an API key."
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
          <p className="rs-form-help">
            {initial?.has_feed
              ? "A private URL is saved. Leave blank to keep it."
              : "Paste the complete feed URL, including its passkey. It must belong to the website above."}
          </p>
        </>
      )}
      {cookieSite && (
        <>
          <label>
            Session cookie (value only){!privateSite && " — optional"}
            <TextField.Root
              aria-label="Session cookie"
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={cookie}
              onChange={(e) => setCookie(e.target.value)}
              disabled={busy}
              maxLength={12000}
            />
          </label>
          <p className="rs-form-help">
            {initial?.has_cookie
              ? "A session is saved. Leave blank to keep it. Re-enter it after changing the URL or user agent. "
              : ""}
            Copy only the value after <code>cookie:</code> from a signed-in
            tracker request. Do not paste the <code>Cookie:</code> label or use{" "}
            <code>document.cookie</code>; session cookies are HttpOnly. Labels
            and line breaks are removed automatically. Keep it out of chat.
          </p>
          {(kind === "rss" || kind === "cardigann") && initial?.has_cookie && (
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
            Browser user agent (exact value)
            <TextField.Root
              aria-label="Browser user agent"
              value={agent}
              onChange={(e) => setAgent(e.target.value)}
              maxLength={512}
              required={privateSite}
              disabled={busy}
              autoComplete="off"
              spellCheck={false}
            />
          </label>
          <p className="rs-form-help">
            Copy the full <code>user-agent</code> value from the same request.
            The <code>User-Agent:</code> label and line breaks are removed
            automatically.
          </p>
        </>
      )}
      {privateSite && (
        <label className="rs-check">
          <Switch checked={free} onCheckedChange={setFree} />
          Freeleech releases only
        </label>
      )}
      {kind === "iptorrents" && (
        <p className="rs-form-help">
          For the tracker’s torrent table, choose Icons for categories and
          enable the files-count column in your IPTorrents settings.
        </p>
      )}
      <label className="rs-check">
        <Switch checked={enabled} onCheckedChange={setEnabled} />
        Use this indexer in searches
      </label>
      {error && (
        <p role="alert" className="rs-alert is-danger">
          {error}
        </p>
      )}
      <div className="rs-form-actions">
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
