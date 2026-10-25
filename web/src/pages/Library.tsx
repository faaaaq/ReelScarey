import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Plus, LayoutGrid, List, Search } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import {
  type LibraryPage,
  type MediaItem,
  type CatalogProfileContext,
} from "../api";
import { PageHeading, RequestState } from "../components";
import { Poster, Pagination } from "../movieComponents";
import { useResource } from "../useResource";
import { BulkMonitoring } from "./BulkMonitoring";
import { BulkProfiles } from "./BulkProfiles";

function ProfileLabel({ item }: { item: MediaItem }) {
  const profile = item.quality_profile;
  return profile ? (
    <span className="catalog-profile-label">
      {profile.effective_profile.name}
      {profile.profile_id === null ? " · default" : " · assigned"}
    </span>
  ) : null;
}

function statusLabel(item: MediaItem) {
  if (item.status === "available")
    return item.kind === "series" ? "Episode files registered" : "In library";
  if (item.status === "downloading") return "Downloading";
  return item.kind === "series" ? "No episode files" : "Missing";
}
function ItemAction({ item }: { item: MediaItem }) {
  return (
    <Link
      className="text-link"
      to={
        item.kind === "series"
          ? `/series/${item.id}`
          : item.status === "downloading"
            ? "/activity"
            : item.status === "available"
              ? `/movies/${item.id}`
              : `/releases?movie=${item.id}`
      }
    >
      {item.kind === "series"
        ? "Episodes"
        : item.status === "downloading"
          ? "Activity"
          : item.status === "available"
            ? "Details"
            : "Find releases"}
    </Link>
  );
}
export function Library({ kind }: { kind: "movie" | "series" | "anime" }) {
  const [params, setParams] = useSearchParams();
  const [input, setInput] = useState(params.get("query") ?? "");
  useEffect(() => setInput(params.get("query") ?? ""), [params]);
  const page = Number(params.get("page") ?? 1);
  const apiParams = new URLSearchParams({
    kind,
    page: params.get("page") ?? "1",
  });
  for (const [key, value] of params)
    if (key !== "view" && key !== "page" && key !== "kind")
      apiParams.append(key, value);
  const resource = useResource<LibraryPage>(`/library?${apiParams.toString()}`);
  const selectionScope = apiParams.toString();
  const [selected, setSelected] = useState<number[]>([]);
  const [monitoringMessage, setMonitoringMessage] = useState("");
  useEffect(() => {
    setSelected([]);
    setMonitoringMessage("");
  }, [selectionScope]);
  function toggle(id: number, checked: boolean) {
    setSelected((current) =>
      checked
        ? [...new Set([...current, id])]
        : current.filter((value) => value !== id),
    );
    setMonitoringMessage("");
  }
  const movies = kind === "movie",
    anime = kind === "anime",
    noun = anime ? "titles" : movies ? "movies" : "shows",
    singular = anime ? "title" : movies ? "movie" : "show",
    searchPath = anime
      ? "/search?kind=anime"
      : movies
        ? "/search"
        : "/search?kind=series",
    table = params.get("view") === "table";
  function change(key: string, value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    if (key !== "view" && key !== "page") next.delete("page");
    setParams(next);
  }
  function submit(e: FormEvent) {
    e.preventDefault();
    change("query", input.trim());
  }
  const data = resource.data;
  const [savedProfileContext, setSavedProfileContext] =
    useState<CatalogProfileContext>();
  useEffect(() => {
    if (data?.profile_context) setSavedProfileContext(data.profile_context);
  }, [data]);
  const profileContext = data?.profile_context ?? savedProfileContext;
  const selectedGroups =
    kind === "anime"
      ? (["movie", "series"] as const)
          .map((groupKind) => ({
            kind: groupKind,
            ids: (data?.items ?? [])
              .filter(
                (item) => item.kind === groupKind && selected.includes(item.id),
              )
              .map((item) => item.id),
          }))
          .filter((group) => group.ids.length > 0)
      : selected.length
        ? [{ kind, ids: selected }]
        : [];
  const filtered = ["query", "status", "monitored", "profile"].some(
    (k) => !!params.get(k) && params.get(k) !== "all",
  );
  return (
    <>
      <PageHeading
        title={anime ? "Anime" : movies ? "Movies" : "TV shows"}
        description={
          data
            ? `${data.collection_total ?? data.total} ${(data.collection_total ?? data.total) === 1 ? singular : noun} in the catalog`
            : "Saved titles"
        }
      >
        <div className="heading-actions">
          <Button asChild variant="soft">
            <Link to="/import">Import existing</Link>
          </Button>
          <Button asChild>
            <Link to={searchPath}>
              <Plus size={16} />
              {anime ? "Add anime" : movies ? "Add movies" : "Add shows"}
            </Link>
          </Button>
        </div>
      </PageHeading>
      <section className="catalog-toolbar" aria-label="Library filters">
        <form onSubmit={submit} className="catalog-search">
          <TextField.Root
            aria-label="Search saved titles"
            placeholder="Filter by title…"
            maxLength={200}
            value={input}
            onChange={(e) => setInput(e.target.value)}
          >
            <TextField.Slot>
              <Search size={16} />
            </TextField.Slot>
          </TextField.Root>
          <Button type="submit" variant="soft">
            Filter
          </Button>
        </form>
        <div className="catalog-selects">
          <label>
            Status
            <select
              aria-label="Library status"
              value={params.get("status") ?? "all"}
              onChange={(e) => change("status", e.target.value)}
            >
              <option value="all">All statuses</option>
              <option value="missing">
                {anime
                  ? "Missing / no episode files"
                  : movies
                    ? "Missing"
                    : "No episode files"}
              </option>
              <option value="available">
                {anime
                  ? "Files registered"
                  : movies
                    ? "In library"
                    : "Episode files registered"}
              </option>
              <option value="downloading">Downloading</option>
            </select>
          </label>
          <label>
            Monitoring
            <select
              aria-label="Library monitoring"
              value={params.get("monitored") ?? "all"}
              onChange={(e) => change("monitored", e.target.value)}
            >
              <option value="all">All titles</option>
              <option value="monitored">Monitored</option>
              <option value="paused">Paused</option>
            </select>
          </label>
          <label>
            Sort
            <select
              aria-label="Sort library"
              value={params.get("sort") ?? "title"}
              onChange={(e) => change("sort", e.target.value)}
            >
              <option value="title">Title A–Z</option>
              <option value="title_desc">Title Z–A</option>
              <option value="year_desc">Year, newest first</option>
              <option value="year">Year, oldest first</option>
              <option value="added">Recently added</option>
            </select>
          </label>
          {profileContext && (
            <label>
              Quality profile
              <select
                aria-label="Library quality profile"
                value={params.get("profile") ?? "all"}
                onChange={(e) => change("profile", e.target.value)}
              >
                <option value="all">All profiles</option>
                <option value="default">Follows schedule default</option>
                {profileContext.profiles.map((profile) => (
                  <option key={profile.id} value={String(profile.id)}>
                    {profile.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          <Button variant="ghost" color="gray" onClick={() => setParams({})}>
            Reset filters
          </Button>
        </div>
      </section>
      <div className="catalog-result-bar">
        <p role="status">
          {data && !resource.loading && !resource.error
            ? `${data.total} matching ${data.total === 1 ? singular : noun}${data.items.length ? ` · ${(data.page - 1) * data.limit + 1}–${(data.page - 1) * data.limit + data.items.length} shown` : ""}`
            : ""}
        </p>
        <div className="view-switch" role="group" aria-label="Library view">
          <button
            aria-label="Poster view"
            aria-pressed={!table}
            onClick={() => change("view", "")}
          >
            <LayoutGrid size={16} />
            <span>Posters</span>
          </button>
          <button
            aria-label="Table view"
            aria-pressed={table}
            onClick={() => change("view", "table")}
          >
            <List size={16} />
            <span>Table</span>
          </button>
        </div>
      </div>
      {monitoringMessage && (
        <p role="status" className="save-success">
          {monitoringMessage}
        </p>
      )}
      {!resource.loading && !resource.error && data?.items.length ? (
        <div className="catalog-selection">
          <label>
            <input
              type="checkbox"
              aria-label="Select all titles on this page"
              checked={data.items.every((item) => selected.includes(item.id))}
              ref={(element) => {
                if (element)
                  element.indeterminate =
                    selected.length > 0 &&
                    !data.items.every((item) => selected.includes(item.id));
              }}
              onChange={(e) => {
                setSelected(
                  e.target.checked ? data.items.map((item) => item.id) : [],
                );
                setMonitoringMessage("");
              }}
            />
            Select this page
          </label>
          <span>Selection is cleared when filters or pages change.</span>
        </div>
      ) : null}
      {!resource.loading &&
        !resource.error &&
        selectedGroups.map((group) => (
          <section
            key={group.kind}
            aria-label={
              anime
                ? `Selected anime ${group.kind === "movie" ? "movies" : "shows"}`
                : undefined
            }
          >
            {anime && (
              <h2>
                {group.kind === "movie" ? "Selected movies" : "Selected shows"}
              </h2>
            )}
            <BulkMonitoring
              kind={group.kind}
              ids={group.ids}
              onClear={() => setSelected([])}
              onApplied={(message) => {
                setSelected([]);
                setMonitoringMessage(message);
                resource.reload();
              }}
            >
              {data?.profile_context && (
                <BulkProfiles
                  kind={group.kind}
                  ids={group.ids}
                  context={data.profile_context}
                  onApplied={(message) => {
                    setSelected([]);
                    setMonitoringMessage(message);
                    resource.reload();
                  }}
                />
              )}
            </BulkMonitoring>
          </section>
        ))}
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {!resource.loading && resource.error && (
        <Button variant="soft" onClick={() => setParams({})}>
          Clear library filters
        </Button>
      )}
      {!resource.loading && !resource.error && data && (
        <>
          {data.items.length ? (
            table ? (
              <div
                className="catalog-table-wrap"
                role="region"
                aria-label="Scrollable library table"
                tabIndex={0}
              >
                <table
                  className="catalog-table"
                  aria-label={
                    anime
                      ? "Anime library"
                      : movies
                        ? "Movie library"
                        : "TV library"
                  }
                >
                  <thead>
                    <tr>
                      <th scope="col">
                        <span className="sr-only">Select</span>
                      </th>
                      <th scope="col">Title</th>
                      <th scope="col">Year</th>
                      {anime && <th scope="col">Format</th>}
                      <th scope="col">Status</th>
                      <th scope="col">Monitoring</th>
                      <th scope="col">Quality profile</th>
                      <th scope="col">
                        <span className="sr-only">Actions</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.items.map((item) => (
                      <tr key={item.id}>
                        <td className="catalog-select-cell">
                          <input
                            type="checkbox"
                            aria-label={`Select ${item.title}`}
                            checked={selected.includes(item.id)}
                            onChange={(e) => toggle(item.id, e.target.checked)}
                          />
                        </td>
                        <th scope="row">
                          <Link
                            to={`/${item.kind === "movie" ? "movies" : "series"}/${item.id}`}
                          >
                            {item.title}
                          </Link>
                        </th>
                        <td>{item.year ?? "—"}</td>
                        {anime && (
                          <td>{item.kind === "movie" ? "Movie" : "Show"}</td>
                        )}
                        <td>
                          <span
                            className={`catalog-state state-${item.status}`}
                          >
                            {statusLabel(item)}
                          </span>
                        </td>
                        <td>{item.monitored ? "Monitored" : "Paused"}</td>
                        <td>
                          <ProfileLabel item={item} />
                        </td>
                        <td>
                          <ItemAction item={item} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="catalog-posters">
                {data.items.map((item) => (
                  <article className="catalog-card" key={item.id}>
                    <label className="catalog-card-select">
                      <input
                        type="checkbox"
                        aria-label={`Select ${item.title}`}
                        checked={selected.includes(item.id)}
                        onChange={(e) => toggle(item.id, e.target.checked)}
                      />
                      Select
                    </label>
                    <Link
                      className="catalog-art"
                      to={`/${item.kind === "movie" ? "movies" : "series"}/${item.id}`}
                      aria-label={`Open ${item.title} details`}
                    >
                      <Poster movie={item} />
                      <span className={`catalog-state state-${item.status}`}>
                        {statusLabel(item)}
                      </span>
                    </Link>
                    <h2>
                      <Link
                        to={`/${item.kind === "movie" ? "movies" : "series"}/${item.id}`}
                      >
                        {item.title}
                      </Link>
                    </h2>
                    <div className="catalog-card-meta">
                      <span>{item.year ?? "Year unknown"}</span>
                      {anime && (
                        <span>{item.kind === "movie" ? "Movie" : "Show"}</span>
                      )}
                      <span>{item.monitored ? "Monitored" : "Paused"}</span>
                    </div>
                    <ItemAction item={item} />
                    <ProfileLabel item={item} />
                  </article>
                ))}
              </div>
            )
          ) : (
            <section className="empty-state panel">
              <h2>
                {page > 1
                  ? `No ${noun} on this page`
                  : filtered
                    ? `No matching ${noun}`
                    : anime
                      ? "No anime in the library"
                      : movies
                        ? "No movies in the library"
                        : "No shows in the library"}
              </h2>
              <p>
                {page > 1
                  ? "Return to the first page with these filters."
                  : filtered
                    ? "Try a different title or clear the filters."
                    : `Add ${noun} from TMDb or register files from your existing library.`}
              </p>
              {page > 1 ? (
                <Button onClick={() => change("page", "1")}>
                  Go to first page
                </Button>
              ) : filtered ? (
                <Button onClick={() => setParams({})}>
                  Clear library filters
                </Button>
              ) : (
                <Button asChild>
                  <Link to={searchPath}>
                    {anime
                      ? "Find anime"
                      : movies
                        ? "Find a movie"
                        : "Find a show"}
                  </Link>
                </Button>
              )}
            </section>
          )}
          <Pagination
            page={data.page}
            pages={Math.ceil(data.total / data.limit)}
            onPage={(p) => change("page", String(p))}
          />
          {!movies && (
            <p className="catalog-note">
              Show status reflects saved file registrations and tracked
              downloads. Episode files registered does not mean every episode is
              available.
            </p>
          )}
        </>
      )}
    </>
  );
}
