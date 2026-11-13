import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Inbox, LogOut, RefreshCw, UserPlus, Clock } from "lucide-react";
import {
  Alert,
  EmptyState,
  FieldSet,
  Label,
  PageBody,
  PageToolbar,
  Table,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { Pagination } from "../movieComponents";
import "./Admin.css";
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
  bot_notice?: string;
};
type Account = {
  id: number;
  username: string;
  disabled: boolean;
  discord_user_id?: string;
};
type Result = {
  tmdb_id: number;
  title: string;
  year: number | null;
  in_library: boolean;
};

/** Friendly labels for internal request states, shown to requesters. */
const requestStateLabels: Record<string, string> = {
  pending: "Waiting for review",
  approving: "Being added",
  uncertain: "Delivery result unknown",
  declined: "Declined",
};

function friendlyRequestState(state: string) {
  return requestStateLabels[state] ?? state;
}

export function RequesterHome() {
  const { session, signOut } = useAuth();
  useEffect(() => {
    document.title = "My requests · ReelScarey";
  }, []);
  const requests = useLiveResource<{ items: Request[] }>(
    "/requester/requests",
    15000,
  );
  const [kind, setKind] = useState<"movie" | "series">("movie"),
    [query, setQuery] = useState(""),
    [page, setPage] = useState(1),
    [totalPages, setTotalPages] = useState(1),
    [results, setResults] = useState<Result[]>([]),
    [pending, setPending] = useState(false),
    [error, setError] = useState("");
  async function fetchResults(nextPage: number) {
    setPending(true);
    setError("");
    try {
      const data = await api<{
        items: Result[];
        page: number;
        total_pages: number;
      }>(
        `/requester/search/${kind === "movie" ? "movies" : "series"}?query=${encodeURIComponent(query.trim())}&page=${nextPage}`,
      );
      setResults(data.items);
      setPage(nextPage);
      setTotalPages(data.total_pages || 1);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  async function search(e: FormEvent) {
    e.preventDefault();
    if (pending || !query.trim()) return;
    await fetchResults(1);
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
    <div className="rs-app">
      <header className="rs-header">
        <div className="rs-header-logo">
          <span className="rs-logo">
            <span className="rs-logo-mark">
              <Inbox size={18} aria-hidden="true" />
            </span>
            <span className="rs-logo-text">ReelScarey</span>
          </span>
        </div>
        <div className="rs-header-actions">
          <span className="adm-requester-user">{session.username}</span>
          <Button
            variant="soft"
            color="gray"
            onClick={() =>
              void signOut().catch(() =>
                setError("Could not sign out. Try again."),
              )
            }
          >
            <LogOut size={15} aria-hidden="true" /> Sign out
          </Button>
        </div>
      </header>
      <main id="main-content" className="adm-requester-main" tabIndex={-1}>
        <PageBody>
          <PageHeading
            title="My requests"
            description={`Signed in as ${session.username}`}
          />
          <FieldSet legend="Find a title">
            <form
              onSubmit={(e) => void search(e)}
              className="adm-request-search"
            >
              <select
                aria-label="Type"
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
              <TextField.Root
                aria-label="Title"
                placeholder="Search for a movie or show"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                maxLength={200}
              />
              <Button type="submit" disabled={pending}>
                Search
              </Button>
            </form>
            {error && <Alert kind="danger">{error}</Alert>}
            {results.length > 0 && (
              <Table label="Search results">
                <tbody>
                  {results.map((item) => (
                    <tr key={item.tmdb_id}>
                      <td>
                        {item.title}
                        {item.year ? ` (${item.year})` : ""}{" "}
                        {item.in_library && (
                          <Label kind="success">In library</Label>
                        )}
                      </td>
                      <td className="is-actions">
                        <Button
                          size="1"
                          disabled={pending || item.in_library}
                          onClick={() => void submit(item.tmdb_id)}
                        >
                          {item.in_library ? "Already in library" : "Request"}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            <Pagination
              page={page}
              pages={totalPages}
              onPage={(p) => void fetchResults(p)}
            />
          </FieldSet>
          <FieldSet legend="My submitted requests">
            <RequestState
              loading={requests.loading}
              error={requests.error}
        code={requests.code}
              retry={requests.reload}
            />
            {requests.data &&
              (requests.data.items.length ? (
                <Table label="My submitted requests">
                  <thead>
                    <tr>
                      <th scope="col">Title</th>
                      <th scope="col">Type</th>
                      <th scope="col">State</th>
                      <th scope="col">Notification</th>
                    </tr>
                  </thead>
                  <tbody>
                    {requests.data.items.map((item) => (
                      <tr key={item.id}>
                        <td>
                          #{item.id} · {item.title}
                        </td>
                        <td>{item.kind === "movie" ? "Movie" : "TV show"}</td>
                        <td>
                          <Label kind={item.available ? "success" : "info"}>
                            {friendlyRequestState(item.state)}
                          </Label>
                          {item.available ? " · Media available" : ""}
                        </td>
                        <td>
                          {item.notification
                            ? `Notification ${item.notification}`
                            : "—"}
                          {item.notification === "uncertain" && (
                            <>
                              {" "}
                              <Button
                                size="1"
                                variant="soft"
                                disabled={pending}
                                onClick={() => void retryNotification(item.id)}
                              >
                                Retry notification (may duplicate)
                              </Button>
                            </>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              ) : (
                <EmptyState icon={Clock} title="No requests yet">
                  <p>
                    Titles you request are reviewed by an administrator before
                    they are added. Use the search above to find a movie or show
                    and request it.
                  </p>
                </EmptyState>
              ))}
          </FieldSet>
          <RequestLists onImported={requests.reload} />
          <DiscordSettings />
          <ChangePassword />
        </PageBody>
      </main>
    </div>
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
    <section className="rs-fieldset">
      <div className="rs-legend">
        <h2>Change password</h2>
      </div>
      <p>Changing your password signs out all of your request sessions.</p>
      <form className="backup-fields" onSubmit={(e) => void save(e)}>
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
  const [accessToken, setAccessToken] = useState("");
  const [provider, setProvider] = useState<
      | "tmdb"
      | "trakt"
      | "imdb_mdblist"
      | "tmdb_private"
      | "trakt_private"
      | "trakt_watchlist"
      | "trakt_watchlist_private"
    >("tmdb"),
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
          access_token: provider.endsWith("_private") ? accessToken.trim() : "",
          owner: provider.startsWith("trakt") ? owner.trim() : "",
          list_id: provider.startsWith("trakt_watchlist")
            ? "watchlist"
            : listID.trim(),
          client_id: !provider.startsWith("tmdb") ? clientID.trim() : "",
        }),
      });
      setOwner("");
      setListID("");
      setClientID("");
      setAccessToken("");
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
    <section className="rs-fieldset">
      <div className="rs-legend">
        <h2>Import lists</h2>
      </div>
      <p>
        Public or token-authorized private TMDb/Trakt lists and IMDb lists
        imported into MDBList are checked automatically each day. Imported
        titles need administrator review.
      </p>
      <form className="backup-fields" onSubmit={(e) => void add(e)}>
        <label>
          Provider{" "}
          <select
            value={provider}
            disabled={pending}
            onChange={(e) => {
              setProvider(
                e.target.value as
                  | "tmdb"
                  | "trakt"
                  | "imdb_mdblist"
                  | "tmdb_private"
                  | "trakt_private"
                  | "trakt_watchlist"
                  | "trakt_watchlist_private",
              );
              setClientID("");
              setAccessToken("");
            }}
          >
            <option value="tmdb_private">TMDb private list</option>
            <option value="trakt_private">Trakt private list</option>
            <option value="trakt_watchlist_private">
              Trakt private watchlist
            </option>
            <option value="tmdb">TMDb</option>
            <option value="trakt">Trakt</option>
            <option value="trakt_watchlist">Trakt watchlist</option>
            <option value="imdb_mdblist">IMDb via MDBList</option>
          </select>
        </label>
        {provider.endsWith("_private") && (
          <label>
            User access token{" "}
            <TextField.Root
              type="password"
              value={accessToken}
              onChange={(e) => setAccessToken(e.target.value)}
              autoComplete="off"
            />
            <span>
              Use your provider-issued user token. Expired tokens require adding
              the list again. Tokens are encrypted and never displayed.
            </span>
          </label>
        )}
        {provider.startsWith("trakt") && (
          <label>
            {provider.startsWith("trakt_watchlist")
              ? "Trakt username"
              : "Trakt list owner"}{" "}
            <TextField.Root
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
              required
            />
          </label>
        )}
        {!provider.startsWith("trakt_watchlist") && (
          <label>
            {provider.startsWith("trakt")
              ? "Trakt list slug or ID"
              : provider === "imdb_mdblist"
                ? "MDBList external list ID"
                : "TMDb numeric list ID"}{" "}
            <TextField.Root
              value={listID}
              onChange={(e) => setListID(e.target.value)}
              required
            />
          </label>
        )}
        {!provider.startsWith("tmdb") && (
          <label>
            {provider.startsWith("trakt")
              ? "Trakt application client ID"
              : "MDBList API key"}{" "}
            <TextField.Root
              type="password"
              value={clientID}
              onChange={(e) => setClientID(e.target.value)}
              autoComplete="off"
              required
            />
          </label>
        )}
        {provider.startsWith("trakt_watchlist") && (
          <p>
            {provider.endsWith("_private")
              ? "Use a Trakt user access token for a private profile. ReelScarey imports the newest 200 watchlist titles daily."
              : "The Trakt profile must be public. ReelScarey imports the newest 200 watchlist titles daily."}
          </p>
        )}
        {provider === "imdb_mdblist" && (
          <p>
            First import your IMDb list into your MDBList account. MDBList
            refreshes it on its account schedule; ReelScarey checks the MDBList
            copy daily.
          </p>
        )}
        <Button type="submit" disabled={pending}>
          Add list
        </Button>
      </form>
      <RequestState
        loading={lists.loading}
        error={lists.error}
        code={lists.code}
        retry={lists.reload}
      />
      {lists.data && (
        <ul>
          {lists.data.items.map((l) => (
            <li key={l.id}>
              {l.provider === "imdb_mdblist"
                ? "IMDb via MDBList"
                : l.provider === "trakt_watchlist"
                  ? "Trakt watchlist"
                  : l.provider === "trakt_watchlist_private"
                    ? "Trakt private watchlist"
                    : l.provider.toUpperCase()}{" "}
              · {l.owner ? `${l.owner}/` : ""}
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
    <section className="rs-fieldset">
      <div className="rs-legend">
        <h2>Discord notifications</h2>
      </div>
      <p>
        Receive a webhook message when an approved request is available. For TV,
        all monitored regular seasons must be loaded and their monitored
        episodes must be available.
      </p>
      <RequestState
        loading={status.loading}
        error={status.error}
        code={status.code}
        retry={status.reload}
      />
      {status.data && (
        <p>
          {status.data.configured
            ? "Webhook configured"
            : "No webhook configured"}
        </p>
      )}
      <form className="backup-fields" onSubmit={(e) => void save(e)}>
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
  async function retryNotice(id: number) {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/discord-bot/notices/${id}/retry`, {
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
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={requests.loading || accounts.loading}
            onClick={() => {
              requests.reload();
              accounts.reload();
            }}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title="Requests" />
        {error && <Alert kind="danger">{error}</Alert>}
        <FieldSet legend="Requested titles">
          <RequestState
            loading={requests.loading}
            error={requests.error}
        code={requests.code}
            retry={requests.reload}
          />
          {requests.data &&
            (requests.data.items.length ? (
              <Table label="Requested titles">
                <thead>
                  <tr>
                    <th scope="col">Title</th>
                    <th scope="col">Requested by</th>
                    <th scope="col">Status</th>
                    <th scope="col" className="is-actions">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {requests.data.items.map((item) => (
                    <tr key={item.id}>
                      <td>
                        {item.title}{" "}
                        <span className="rs-muted rs-small">
                          {item.kind === "movie" ? "Movie" : "TV show"}
                        </span>
                      </td>
                      <td>{item.username}</td>
                      <td>
                        <span className="rs-row">
                          <Label
                            kind={
                              item.available
                                ? "success"
                                : item.state === "pending"
                                  ? "warning"
                                  : item.state === "declined"
                                    ? "disabled"
                                    : "info"
                            }
                          >
                            {item.state}
                          </Label>
                          {item.available && (
                            <Label kind="success" outline>
                              Media available
                            </Label>
                          )}
                          {item.bot_notice === "uncertain" && (
                            <Label kind="warning" outline>
                              Discord notice uncertain
                            </Label>
                          )}
                        </span>
                      </td>
                      <td className="is-actions">
                        <span
                          className="rs-row"
                          style={{ justifyContent: "flex-end" }}
                        >
                          {item.bot_notice === "uncertain" && (
                            <Button
                              size="1"
                              variant="soft"
                              disabled={pending}
                              onClick={() => void retryNotice(item.id)}
                            >
                              Retry notice (may duplicate)
                            </Button>
                          )}
                          {(item.state === "pending" ||
                            item.state === "approving") && (
                            <Button
                              size="1"
                              disabled={pending}
                              onClick={() => void act(item.id, "approve")}
                            >
                              {item.state === "approving"
                                ? "Retry approval"
                                : "Approve"}
                            </Button>
                          )}
                          {item.state === "pending" && (
                            <Button
                              size="1"
                              variant="soft"
                              color="red"
                              disabled={pending}
                              onClick={() => void act(item.id, "decline")}
                            >
                              Decline
                            </Button>
                          )}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            ) : (
              <EmptyState icon={Clock} title="No requests yet">
                <p>
                  No titles have been submitted for review yet. Requests from
                  request accounts appear here for approval or decline.
                </p>
              </EmptyState>
            ))}
        </FieldSet>
        <FieldSet legend="Request accounts">
          <form className="adm-request-search" onSubmit={(e) => void create(e)}>
            <TextField.Root
              aria-label="Username"
              placeholder="Username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="off"
              required
            />
            <TextField.Root
              aria-label="Initial password"
              placeholder="Initial password (12+ characters)"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              minLength={12}
              required
            />
            <Button type="submit" disabled={pending}>
              Create account
            </Button>
          </form>
          <RequestState
            loading={accounts.loading}
            error={accounts.error}
        code={accounts.code}
            retry={accounts.reload}
          />
          {accounts.data &&
            (accounts.data.items.length ? (
              <Table label="Request accounts">
                <thead>
                  <tr>
                    <th scope="col">Username</th>
                    <th scope="col">Status</th>
                    <th scope="col">Discord</th>
                    <th scope="col" className="is-actions">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {accounts.data.items.map((a) => (
                    <tr key={a.id}>
                      <td>{a.username}</td>
                      <td>
                        <Label kind={a.disabled ? "disabled" : "success"}>
                          {a.disabled ? "Disabled" : "Enabled"}
                        </Label>
                      </td>
                      <td>
                        <DiscordLink account={a} saved={accounts.reload} />
                      </td>
                      <td className="is-actions">
                        <Button
                          size="1"
                          variant="soft"
                          disabled={pending}
                          onClick={() => void toggle(a)}
                        >
                          {a.disabled ? "Enable" : "Disable"}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            ) : (
              <EmptyState icon={UserPlus} title="No request accounts yet">
                <p>
                  Create a request account so someone can sign in, browse the
                  catalog, and submit titles. Link a Discord user ID to enable
                  notifications.
                </p>
              </EmptyState>
            ))}
        </FieldSet>
        <BotSettings />
      </PageBody>
    </>
  );
}

function DiscordLink({
  account,
  saved,
}: {
  account: Account;
  saved: () => void;
}) {
  const [value, setValue] = useState(account.discord_user_id ?? ""),
    [pending, setPending] = useState(false),
    [error, setError] = useState("");
  async function save(e: FormEvent) {
    e.preventDefault();
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/request-accounts/${account.id}/discord`, {
        method: "PUT",
        body: JSON.stringify({ discord_user_id: value.trim() }),
      });
      saved();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <form className="backup-fields" onSubmit={(e) => void save(e)}>
      <label>
        Discord user ID for {account.username}{" "}
        <TextField.Root
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="Optional"
        />
      </label>
      <Button size="1" variant="soft" disabled={pending} type="submit">
        Save Discord link
      </Button>
      {error && <span role="alert">{error}</span>}
    </form>
  );
}

type BotConfig = {
  configured: boolean;
  application_id: string;
  public_key: string;
  guild_id: string;
  admin_user_id: string;
  admin_channel_id: string;
};
function BotSettings() {
  const config = useResource<BotConfig>("/discord-bot"),
    [applicationID, setApplicationID] = useState(""),
    [publicKey, setPublicKey] = useState(""),
    [guildID, setGuildID] = useState(""),
    [adminID, setAdminID] = useState(""),
    [channelID, setChannelID] = useState(""),
    [token, setToken] = useState(""),
    [pending, setPending] = useState(false),
    [error, setError] = useState(""),
    [message, setMessage] = useState("");
  useEffect(() => {
    if (!config.data?.configured) return;
    setApplicationID(config.data.application_id);
    setPublicKey(config.data.public_key);
    setGuildID(config.data.guild_id);
    setAdminID(config.data.admin_user_id);
    setChannelID(config.data.admin_channel_id);
  }, [config.data]);
  async function save(e: FormEvent) {
    e.preventDefault();
    if (pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      await api("/discord-bot", {
        method: "PUT",
        body: JSON.stringify({
          application_id: applicationID.trim(),
          public_key: publicKey.trim(),
          guild_id: guildID.trim(),
          admin_user_id: adminID.trim(),
          admin_channel_id: channelID.trim(),
          token: token.trim(),
        }),
      });
      setToken("");
      config.reload();
      setMessage(
        "Bot settings saved. Register the commands and configure Discord’s interaction URL.",
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  async function action(remove: boolean) {
    if (pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      await api(remove ? "/discord-bot" : "/discord-bot/register", {
        method: remove ? "DELETE" : "POST",
        ...(remove ? {} : { body: "{}" }),
      });
      config.reload();
      setMessage(
        remove ? "Bot disabled." : "Commands registered with Discord.",
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <section className="rs-fieldset">
      <div className="rs-legend">
        <h2>Discord companion bot</h2>
      </div>
      <p>
        Requires a Discord application, bot token, and a public HTTPS address
        for ReelScarey. Link each request account to its Discord user ID above.
      </p>
      <p>
        Set the Discord interaction endpoint to{" "}
        <code>{window.location.origin}/api/v1/discord/interactions</code> using
        your public HTTPS address.
      </p>
      <RequestState
        loading={config.loading}
        error={config.error}
        code={config.code}
        retry={config.reload}
      />
      {config.data?.configured && (
        <p>
          Bot configured for server {config.data.guild_id}. Saving new settings
          requires the token again.
        </p>
      )}
      <form className="backup-fields" onSubmit={(e) => void save(e)}>
        <label>
          Application ID{" "}
          <TextField.Root
            value={applicationID}
            onChange={(e) => setApplicationID(e.target.value)}
            required
          />
        </label>
        <label>
          Application public key{" "}
          <TextField.Root
            value={publicKey}
            onChange={(e) => setPublicKey(e.target.value)}
            required
          />
        </label>
        <label>
          Server ID{" "}
          <TextField.Root
            value={guildID}
            onChange={(e) => setGuildID(e.target.value)}
            required
          />
        </label>
        <label>
          Administrator Discord user ID{" "}
          <TextField.Root
            value={adminID}
            onChange={(e) => setAdminID(e.target.value)}
            required
          />
        </label>
        <label>
          Administrator channel ID{" "}
          <TextField.Root
            value={channelID}
            onChange={(e) => setChannelID(e.target.value)}
            required
          />
        </label>
        <label>
          Bot token{" "}
          <TextField.Root
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            autoComplete="off"
            required
          />
        </label>
        <Button type="submit" disabled={pending}>
          Save bot settings
        </Button>
      </form>
      {config.data?.configured && (
        <>
          <Button
            variant="soft"
            disabled={pending}
            onClick={() => void action(false)}
          >
            Register commands
          </Button>{" "}
          <Button
            variant="soft"
            disabled={pending}
            onClick={() => void action(true)}
          >
            Disable bot
          </Button>
        </>
      )}
      {message && <p role="status">{message}</p>}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
