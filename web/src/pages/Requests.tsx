import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { useAuth } from "../auth";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";

type Request = {
  id: number;
  username: string;
  kind: "movie" | "series";
  tmdb_id: number;
  title: string;
  state: string;
  available: boolean;
  notification?: string;
};
type Account = { id: number; username: string; disabled: boolean };
type Result = {
  tmdb_id: number;
  title: string;
  year: number | null;
  in_library: boolean;
};

export function RequesterHome() {
  const { session, signOut } = useAuth();
  const requests = useLiveResource<{ items: Request[] }>(
    "/requester/requests",
    15000,
  );
  const [kind, setKind] = useState<"movie" | "series">("movie"),
    [query, setQuery] = useState(""),
    [results, setResults] = useState<Result[]>([]),
    [pending, setPending] = useState(false),
    [error, setError] = useState("");
  async function search(e: FormEvent) {
    e.preventDefault();
    if (pending || !query.trim()) return;
    setPending(true);
    setError("");
    try {
      const data = await api<{ items: Result[] }>(
        `/requester/search/${kind === "movie" ? "movies" : "series"}?query=${encodeURIComponent(query.trim())}&page=1`,
      );
      setResults(data.items);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  async function submit(id: number) {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api("/requester/requests", {
        method: "POST",
        body: JSON.stringify({ kind, tmdb_id: id }),
      });
      requests.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  async function retryNotification(id: number) {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/requester/requests/${id}/notification/retry`, {
        method: "POST",
        body: "{}",
      });
      requests.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <main className="page-content">
      <PageHeading
        title="My requests"
        description={`Signed in as ${session.username}`}
      />
      <Button
        variant="soft"
        onClick={() =>
          void signOut().catch(() => setError("Could not sign out. Try again."))
        }
      >
        Sign out
      </Button>
      <section className="panel settings-form">
        <h2>Find a title</h2>
        <form onSubmit={(e) => void search(e)} className="movie-search-form">
          <label>
            Type{" "}
            <select
              value={kind}
              disabled={pending}
              onChange={(e) => {
                setKind(e.target.value as "movie" | "series");
                setResults([]);
              }}
            >
              <option value="movie">Movie</option>
              <option value="series">TV show</option>
            </select>
          </label>
          <TextField.Root
            aria-label="Title"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            maxLength={200}
          />
          <Button type="submit" disabled={pending}>
            Search
          </Button>
        </form>
        {error && <p role="alert">{error}</p>}
        <ul>
          {results.map((item) => (
            <li key={item.tmdb_id}>
              {item.title}
              {item.year ? ` (${item.year})` : ""}
              {item.in_library ? " · In library" : ""}{" "}
              <Button
                size="1"
                disabled={pending}
                onClick={() => void submit(item.tmdb_id)}
              >
                Request
              </Button>
            </li>
          ))}
        </ul>
      </section>
      <section className="panel">
        <h2>My submitted requests</h2>
        <RequestState
          loading={requests.loading}
          error={requests.error}
          retry={requests.reload}
        />
        {requests.data &&
          (requests.data.items.length ? (
            <ul>
              {requests.data.items.map((item) => (
                <li key={item.id}>
                  {item.title} · {item.kind === "movie" ? "Movie" : "TV show"} ·{" "}
                  {item.state}
                  {item.available ? " · Media available" : ""}
                  {item.notification
                    ? ` · Notification ${item.notification}`
                    : ""}
                  {item.notification === "uncertain" && (
                    <button
                      type="button"
                      disabled={pending}
                      onClick={() => void retryNotification(item.id)}
                    >
                      Retry notification (may duplicate)
                    </button>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p>No requests yet.</p>
          ))}
      </section>
      <RequestLists onImported={requests.reload} />
      <DiscordSettings />
      <ChangePassword />
    </main>
  );
}

function ChangePassword() {
  const [current, setCurrent] = useState(""),
    [next, setNext] = useState(""),
    [pending, setPending] = useState(false),
    [error, setError] = useState("");
  async function save(e: FormEvent) {
    e.preventDefault();
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api("/requester/password", {
        method: "PUT",
        body: JSON.stringify({ current_password: current, new_password: next }),
      });
      setCurrent("");
      setNext("");
      window.dispatchEvent(new Event("reelscarey:unauthorized"));
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <section className="panel settings-form">
      <h2>Change password</h2>
      <p>Changing your password signs out all of your request sessions.</p>
      <form onSubmit={(e) => void save(e)}>
        <label>
          Current password{" "}
          <TextField.Root
            type="password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
        <label>
          New password{" "}
          <TextField.Root
            type="password"
            value={next}
            onChange={(e) => setNext(e.target.value)}
            autoComplete="new-password"
            minLength={12}
            required
          />
        </label>
        <Button disabled={pending} type="submit">
          Change password
        </Button>
      </form>
      {error && <p role="alert">{error}</p>}
    </section>
  );
}

type ImportList = {
  id: number;
  provider: string;
  owner: string;
  list_id: string;
  last_checked: number;
  last_error: string;
};
function RequestLists({ onImported }: { onImported: () => void }) {
  const lists = useLiveResource<{ items: ImportList[] }>(
    "/requester/lists",
    15000,
  );
  const [provider, setProvider] = useState<"tmdb" | "trakt">("tmdb"),
    [owner, setOwner] = useState(""),
    [listID, setListID] = useState(""),
    [clientID, setClientID] = useState(""),
    [pending, setPending] = useState(false),
    [error, setError] = useState(""),
    [message, setMessage] = useState("");
  async function add(e: FormEvent) {
    e.preventDefault();
    if (pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      await api("/requester/lists", {
        method: "POST",
        body: JSON.stringify({
          provider,
          owner: provider === "trakt" ? owner.trim() : "",
          list_id: listID.trim(),
          client_id: provider === "trakt" ? clientID.trim() : "",
        }),
      });
      setOwner("");
      setListID("");
      setClientID("");
      lists.reload();
      setMessage("List saved. New titles will be added as requests.");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  async function action(id: number, remove: boolean) {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/requester/lists/${id}${remove ? "" : "/refresh"}`, {
        method: remove ? "DELETE" : "POST",
        ...(remove ? {} : { body: "{}" }),
      });
      lists.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  async function importCSV(file: File | undefined) {
    if (!file || pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      if (file.size > 12000)
        throw new Error(
          "Use an IMDb CSV export smaller than 12 KB with at most 25 titles.",
        );
      const csv = await file.text();
      const result = await api<{ added: number; matched: number }>(
        "/requester/imdb-csv",
        { method: "POST", body: JSON.stringify({ csv }) },
      );
      onImported();
      setMessage(
        `Matched ${result.matched} titles; added ${result.added} new requests.`,
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <section className="panel settings-form">
      <h2>Import lists</h2>
      <p>
        Public TMDb and Trakt lists are checked automatically each day. Imported
        titles are submitted for administrator review. Private lists are not
        supported.
      </p>
      <form onSubmit={(e) => void add(e)}>
        <label>
          Provider{" "}
          <select
            value={provider}
            disabled={pending}
            onChange={(e) => setProvider(e.target.value as "tmdb" | "trakt")}
          >
            <option value="tmdb">TMDb</option>
            <option value="trakt">Trakt</option>
          </select>
        </label>
        {provider === "trakt" && (
          <label>
            Trakt list owner{" "}
            <TextField.Root
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
              required
            />
          </label>
        )}
        <label>
          {provider === "trakt"
            ? "Trakt list slug or ID"
            : "TMDb numeric list ID"}{" "}
          <TextField.Root
            value={listID}
            onChange={(e) => setListID(e.target.value)}
            required
          />
        </label>
        {provider === "trakt" && (
          <label>
            Trakt application client ID{" "}
            <TextField.Root
              type="password"
              value={clientID}
              onChange={(e) => setClientID(e.target.value)}
              autoComplete="off"
              required
            />
          </label>
        )}
        <Button type="submit" disabled={pending}>
          Add list
        </Button>
      </form>
      <RequestState
        loading={lists.loading}
        error={lists.error}
        retry={lists.reload}
      />
      {lists.data && (
        <ul>
          {lists.data.items.map((l) => (
            <li key={l.id}>
              {l.provider.toUpperCase()} · {l.owner ? `${l.owner}/` : ""}
              {l.list_id}
              {l.last_checked
                ? ` · Checked ${new Date(l.last_checked * 1000).toLocaleString()}`
                : " · Waiting to check"}
              {l.last_error && <span role="status"> · {l.last_error}</span>}{" "}
              <Button
                size="1"
                variant="soft"
                disabled={pending}
                onClick={() => void action(l.id, false)}
              >
                Check now
              </Button>{" "}
              <Button
                size="1"
                variant="soft"
                disabled={pending}
                onClick={() => void action(l.id, true)}
              >
                Remove
              </Button>
            </li>
          ))}
        </ul>
      )}
      <h3>IMDb CSV export</h3>
      <p>
        Upload an IMDb list export to submit up to 25 titles. This is a one-time
        import; it does not automatically sync.
      </p>
      <label>
        IMDb CSV file{" "}
        <input
          type="file"
          accept=".csv,text/csv"
          disabled={pending}
          onChange={(e) => {
            const file = e.target.files?.[0];
            e.target.value = "";
            void importCSV(file);
          }}
        />
      </label>
      {message && <p role="status">{message}</p>}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}

function DiscordSettings() {
  const status = useResource<{ configured: boolean }>("/requester/discord"),
    [webhook, setWebhook] = useState(""),
    [pending, setPending] = useState(false),
    [error, setError] = useState(""),
    [message, setMessage] = useState("");
  async function save(e: FormEvent) {
    e.preventDefault();
    if (pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      await api("/requester/discord", {
        method: "PUT",
        body: JSON.stringify({ webhook: webhook.trim() }),
      });
      setWebhook("");
      status.reload();
      setMessage("Webhook saved.");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  async function remove() {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api("/requester/discord", { method: "DELETE" });
      status.reload();
      setMessage("Webhook removed.");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <section className="panel settings-form">
      <h2>Discord notifications</h2>
      <p>
        Receive a webhook message when an approved request is available. For TV,
        all monitored regular seasons must be loaded and their monitored
        episodes must be available.
      </p>
      <RequestState
        loading={status.loading}
        error={status.error}
        retry={status.reload}
      />
      {status.data && (
        <p>
          {status.data.configured
            ? "Webhook configured"
            : "No webhook configured"}
        </p>
      )}
      <form onSubmit={(e) => void save(e)}>
        <label>
          Discord webhook URL{" "}
          <TextField.Root
            type="password"
            value={webhook}
            onChange={(e) => setWebhook(e.target.value)}
            autoComplete="off"
            required
          />
        </label>
        <Button type="submit" disabled={pending}>
          Save webhook
        </Button>
        {status.data?.configured && (
          <>
            {" "}
            <Button
              type="button"
              variant="soft"
              disabled={pending}
              onClick={() => void remove()}
            >
              Remove
            </Button>
          </>
        )}
      </form>
      {message && <p role="status">{message}</p>}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}

export function AdminRequests() {
  const accounts = useResource<{ items: Account[] }>("/request-accounts"),
    requests = useResource<{ items: Request[] }>("/requests");
  const [username, setUsername] = useState(""),
    [password, setPassword] = useState(""),
    [pending, setPending] = useState(false),
    [error, setError] = useState("");
  async function create(e: FormEvent) {
    e.preventDefault();
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api("/request-accounts", {
        method: "POST",
        body: JSON.stringify({ username, password }),
      });
      setUsername("");
      setPassword("");
      accounts.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  async function toggle(account: Account) {
    setPending(true);
    setError("");
    try {
      await api(`/request-accounts/${account.id}`, {
        method: "PUT",
        body: JSON.stringify({ disabled: !account.disabled }),
      });
      accounts.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  async function act(id: number, action: "approve" | "decline") {
    setPending(true);
    setError("");
    try {
      await api(`/requests/${id}/${action}`, { method: "POST", body: "{}" });
      requests.reload();
    } catch (e) {
      setError(messageFor(e));
      requests.reload();
    } finally {
      setPending(false);
    }
  }
  return (
    <>
      <PageHeading
        title="Requests"
        description="Manage request accounts and review requested titles."
      />
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      <section className="panel settings-form">
        <h2>Request accounts</h2>
        <form onSubmit={(e) => void create(e)}>
          <label>
            Username{" "}
            <TextField.Root
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="off"
              required
            />
          </label>
          <label>
            Initial password{" "}
            <TextField.Root
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              minLength={12}
              required
            />
          </label>
          <Button type="submit" disabled={pending}>
            Create account
          </Button>
        </form>
        <RequestState
          loading={accounts.loading}
          error={accounts.error}
          retry={accounts.reload}
        />
        {accounts.data && (
          <ul>
            {accounts.data.items.map((a) => (
              <li key={a.id}>
                {a.username} · {a.disabled ? "Disabled" : "Enabled"}{" "}
                <Button
                  size="1"
                  variant="soft"
                  disabled={pending}
                  onClick={() => void toggle(a)}
                >
                  {a.disabled ? "Enable" : "Disable"}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section className="panel">
        <h2>Requested titles</h2>
        <RequestState
          loading={requests.loading}
          error={requests.error}
          retry={requests.reload}
        />
        {requests.data &&
          (requests.data.items.length ? (
            <ul>
              {requests.data.items.map((item) => (
                <li key={item.id}>
                  {item.title} · {item.username} · {item.state}
                  {item.available ? " · Media available" : ""}
                  {(item.state === "pending" || item.state === "approving") && (
                    <>
                      {" "}
                      <Button
                        size="1"
                        disabled={pending}
                        onClick={() => void act(item.id, "approve")}
                      >
                        {item.state === "approving"
                          ? "Retry approval"
                          : "Approve"}
                      </Button>
                      {item.state === "pending" && (
                        <>
                          {" "}
                          <Button
                            size="1"
                            variant="soft"
                            disabled={pending}
                            onClick={() => void act(item.id, "decline")}
                          >
                            Decline
                          </Button>
                        </>
                      )}
                    </>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p>No requests yet.</p>
          ))}
      </section>
    </>
  );
}
