import { Fragment, useEffect, useRef, useState, type FormEvent } from "react";
import { Button, DropdownMenu, TextField } from "@radix-ui/themes";
import {
  ArrowDownUp,
  Bookmark,
  BookmarkX,
  FolderInput,
  Filter,
  LayoutGrid,
  ListChecks,
  Plus,
  RefreshCw,
  Rows3,
  Search,
  Table2,
} from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import {
  api,
  messageFor,
  type AnimePreferences,
  type LibraryPage,
  type MediaItem,
  type CatalogProfileContext,
} from "../api";
import { RequestState } from "../components";
import { Poster, Pagination } from "../movieComponents";
import {
  Alert,
  EmptyState,
  Label,
  Modal,
  ModalClose,
  PageBody,
  PageToolbar,
  Table,
  ToolbarButton,
  ToolbarSection,
  ToolbarSeparator,
  pageParam,
  type Kind,
} from "../ui";
import { useResource } from "../useResource";
import { groupID, groupedTitles, sortLetters } from "../libraryGroups";
import { BulkMonitoring } from "./BulkMonitoring";
import { BulkProfiles } from "./BulkProfiles";
import "./Library.css";

const sorts = [
  { value: "title", label: "Title A–Z" },
  { value: "title_desc", label: "Title Z–A" },
  { value: "year_desc", label: "Year, newest first" },
  { value: "year", label: "Year, oldest first" },
  { value: "added", label: "Recently added" },
];

function ProfileLabel({ item }: { item: MediaItem }) {
  const profile = item.quality_profile;
  return profile ? (
    <span
      className="lib-profile"
      title={
        profile.profile_id === null
          ? "Follows the schedule default profile"
          : "Explicitly assigned profile"
      }
    >
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

/** Sonarr/Radarr colors: green available, blue downloading, red missing and
 * monitored, grey unmonitored. */
function statusKind(item: MediaItem): Kind {
  if (item.status === "available") return "success";
  if (item.status === "downloading") return "primary";
  return item.monitored ? "danger" : "disabled";
}

function detailsPath(item: MediaItem) {
  return `/${item.kind === "movie" ? "movies" : "series"}/${item.id}`;
}

function ItemAction({ item }: { item: MediaItem }) {
  return (
    <Link
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

function MonitorIcon({ monitored }: { monitored: boolean }) {
  const Icon = monitored ? Bookmark : BookmarkX;
  return (
    <span
      className={monitored ? "lib-monitor is-monitored" : "lib-monitor"}
      title={monitored ? "Monitored" : "Paused"}
    >
      <Icon size={15} aria-hidden="true" />
      <span className="sr-only lib-monitor-text">
        {monitored ? "Monitored" : "Paused"}
      </span>
    </span>
  );
}

export function Library({ kind }: { kind: "movie" | "series" | "anime" }) {
  const [params, setParams] = useSearchParams();
  const queryParam = params.get("query") ?? "";
  const [input, setInput] = useState(queryParam);
  useEffect(() => setInput(queryParam), [queryParam]);
  const requestedPage = pageParam(params.get("page"));
  // Ask for the whole collection. A server that ignores `page=all` returns a
  // normal page; the mismatch below falls back to the paginated view.
  const apiParams = new URLSearchParams({ kind, page: "all" });
  for (const [key, value] of params)
    if (key !== "view" && key !== "page" && key !== "kind")
      apiParams.append(key, value);
  const all = useResource<LibraryPage>(`/library?${apiParams.toString()}`);
  // The server may reject page=all past its cap, or return a normal page if
  // the option is unknown. Both cases fall back to the paginated view. Key on
  // the machine-readable error code, never on human-readable prose.
  const allLimitReached = all.code === "library_too_large";
  const capped =
    allLimitReached || (!!all.data && all.data.total > all.data.items.length);
  const pageParams = new URLSearchParams(apiParams);
  pageParams.set("page", String(requestedPage));
  const paged = useResource<LibraryPage>(
    capped ? `/library?${pageParams.toString()}` : null,
  );
  const resource = capped ? paged : all;
  const selectionScope = apiParams.toString();
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);
  const [monitoringMessage, setMonitoringMessage] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [refreshMessage, setRefreshMessage] = useState("");
  const [refreshError, setRefreshError] = useState("");
  const wasCapped = useRef(false);
  useEffect(() => {
    setSelected([]);
    setMonitoringMessage("");
    setRefreshMessage("");
    setRefreshError("");
  }, [selectionScope, requestedPage]);
  // Entering the paginated fallback cannot honour a library-wide selection, so
  // drop it without touching the last successful bulk-action message.
  useEffect(() => {
    if (capped && !wasCapped.current) setSelected([]);
    wasCapped.current = capped;
  }, [capped]);
  function reload() {
    all.reload();
    if (capped) paged.reload();
  }
  async function refreshLibraryMetadata() {
    if (refreshing) return;
    setRefreshing(true);
    setRefreshMessage("");
    setRefreshError("");
    try {
      const out = await api<{ queued: number }>("/library/refresh", {
        method: "POST",
        body: JSON.stringify({ kind }),
      });
      setRefreshMessage(
        out.queued === 0
          ? "This library has no titles to refresh."
          : out.queued === 1
            ? "Queued a metadata refresh for 1 title. Details and episode dates appear as the worker finishes."
            : `Queued a metadata refresh for ${out.queued} titles. Details and episode dates appear as the worker finishes.`,
      );
    } catch (e) {
      setRefreshError(messageFor(e));
    } finally {
      setRefreshing(false);
    }
  }
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
      ? "/search?kind=anime&format=series"
      : movies
        ? "/search"
        : "/search?kind=series",
    importPath = anime
      ? "/import?collection=anime"
      : movies
        ? "/import"
        : "/import?collection=series",
    view =
      params.get("view") === "table"
        ? "table"
        : params.get("view") === "overview"
          ? "overview"
          : "posters";
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
    const context = all.data?.profile_context ?? data?.profile_context;
    if (context) setSavedProfileContext(context);
  }, [all.data, data]);
  const profileContext = data?.profile_context ?? savedProfileContext;
  const activeFilters = ["status", "monitored", "profile"].filter(
    (k) => !!params.get(k) && params.get(k) !== "all",
  ).length;
  const filtered = activeFilters > 0 || !!params.get("query");
  const [filtersOpen, setFiltersOpen] = useState(activeFilters > 0);
  const canSelect = selecting;
  const selectedGroups = !canSelect
    ? []
    : kind === "anime"
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
  const title = anime ? "Anime" : movies ? "Movies" : "TV Shows";
  const sort = params.get("sort") ?? "title";
  const ready = !resource.loading && !resource.error && !!data;
  const items = data?.items ?? [];
  const page = capped ? (data?.page ?? requestedPage) : 1;
  const groups = groupedTitles(items);
  const letters = sortLetters(groups.map(([letter]) => letter));
  // The server accepts up to 5,000 explicit IDs in one reviewed write, so
  // "Select all shown" covers everything currently rendered. In the paginated
  // fallback that is exactly the current page; changing pages clears it.
  const selectable = items;
  const allSelected =
    selectable.length > 0 &&
    selectable.every((item) => selected.includes(item.id));
  function jump(letter: string) {
    document
      .getElementById(groupID(letter))
      ?.scrollIntoView({ block: "start" });
  }
  function applied(message: string) {
    setSelected([]);
    setMonitoringMessage(message);
    reload();
  }
  function selectBox(item: MediaItem) {
    return (
      <input
        type="checkbox"
        aria-label={`Select ${item.title}`}
        checked={selected.includes(item.id)}
        onChange={(e) => toggle(item.id, e.target.checked)}
      />
    );
  }

  return (
    <>
      <PageToolbar label={`${title} actions`}>
        <ToolbarSection>
          <ToolbarButton icon={Plus} label="Add New" to={searchPath} />
          <ToolbarButton
            icon={FolderInput}
            label="Library Import"
            to={importPath}
          />
          <ToolbarButton
            icon={RefreshCw}
            label={refreshing ? "Refreshing…" : "Refresh"}
            title="Queue a full metadata refresh for this library"
            spinning={refreshing}
            disabled={refreshing}
            onClick={() => void refreshLibraryMetadata()}
          />
          <ToolbarSeparator />
          <ToolbarButton
            icon={ListChecks}
            label="Select"
            active={selecting}
            onClick={() => {
              setSelecting((value) => !value);
              setSelected([]);
              setMonitoringMessage("");
            }}
          />
        </ToolbarSection>
        <ToolbarSection align="right">
          <ToolbarButton
            icon={LayoutGrid}
            label="Posters"
            active={view === "posters"}
            onClick={() => change("view", "")}
          />
          <ToolbarButton
            icon={Rows3}
            label="Overview"
            active={view === "overview"}
            onClick={() => change("view", "overview")}
          />
          <ToolbarButton
            icon={Table2}
            label="Table"
            active={view === "table"}
            onClick={() => change("view", "table")}
          />
          <ToolbarSeparator />
          <DropdownMenu.Root>
            <DropdownMenu.Trigger>
              <ToolbarButton
                icon={ArrowDownUp}
                label="Sort"
                aria-label="Sort library"
              />
            </DropdownMenu.Trigger>
            <DropdownMenu.Content align="end">
              <DropdownMenu.RadioGroup
                value={sort}
                onValueChange={(value) => change("sort", value)}
              >
                {sorts.map((option) => (
                  <DropdownMenu.RadioItem
                    key={option.value}
                    value={option.value}
                  >
                    {option.label}
                  </DropdownMenu.RadioItem>
                ))}
              </DropdownMenu.RadioGroup>
            </DropdownMenu.Content>
          </DropdownMenu.Root>
          <ToolbarButton
            icon={Filter}
            label={activeFilters ? `Filter (${activeFilters})` : "Filter"}
            aria-expanded={filtersOpen}
            active={filtersOpen || activeFilters > 0}
            onClick={() => setFiltersOpen((value) => !value)}
          />
        </ToolbarSection>
      </PageToolbar>

      <PageBody className="lib-body">
        <div className="lib-head">
          <h1 className="lib-title">{title}</h1>
          <form onSubmit={submit} className="lib-search" role="search">
            <TextField.Root
              aria-label="Search saved titles"
              placeholder="Filter by title"
              maxLength={200}
              value={input}
              onChange={(e) => setInput(e.target.value)}
            >
              <TextField.Slot>
                <Search size={15} aria-hidden="true" />
              </TextField.Slot>
            </TextField.Root>
            <Button
              type="submit"
              variant="soft"
              aria-label="Apply title filter"
            >
              Apply
            </Button>
          </form>
        </div>

        {ready && letters.length > 1 && (
          <nav className="lib-az" aria-label="Jump to letter">
            {letters.map((letter) => (
              <button
                key={letter}
                type="button"
                aria-label={`Jump to ${letter === "#" ? "other titles" : letter}`}
                onClick={() => jump(letter)}
              >
                {letter}
              </button>
            ))}
          </nav>
        )}

        {filtersOpen && (
          <section className="lib-filters" aria-label="Library filters">
            <label className="lib-filter">
              <span>Status</span>
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
            <label className="lib-filter">
              <span>Monitoring</span>
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
            {profileContext && (
              <label className="lib-filter">
                <span>Quality profile</span>
                <select
                  aria-label="Library quality profile"
                  value={params.get("profile") ?? "all"}
                  onChange={(e) => change("profile", e.target.value)}
                >
                  <option value="all">All profiles</option>
                  <option value="default">Follows schedule default</option>
                  {profileContext.profiles.map((profile) => (
                    <option key={profile.id} value={String(profile.id)}>
                      {profile.name} (assigned)
                    </option>
                  ))}
                </select>
              </label>
            )}
            <Button
              variant="ghost"
              color="gray"
              disabled={!filtered}
              onClick={() => {
                const next = new URLSearchParams();
                for (const key of ["sort", "view"])
                  if (params.get(key)) next.set(key, params.get(key)!);
                setParams(next);
              }}
            >
              Clear filters
            </Button>
          </section>
        )}

        {monitoringMessage && (
          <Alert kind="success" role="status">
            {monitoringMessage}
          </Alert>
        )}

        {refreshMessage && (
          <Alert kind="success" role="status">
            {refreshMessage}
          </Alert>
        )}
        {refreshError && (
          <Alert kind="danger" role="alert">
            {refreshError}
          </Alert>
        )}

        {capped && (
          <Alert kind="warning" title="Showing one page at a time">
            <p>
              {allLimitReached
                ? `This library view has more ${noun} than the server returns in one response. Narrow the filters, or use the page controls below.`
                : `This library has ${all.data?.total ?? 0} matching ${noun}, more than the server returns at once. Use the page controls below.`}{" "}
              The A–Z index jumps within the current page.
            </p>
          </Alert>
        )}

        <RequestState
          loading={resource.loading}
          error={resource.error}
          code={resource.code}
          retry={reload}
        />
        {!resource.loading && resource.error && (
          <div>
            <Button variant="soft" onClick={() => setParams({})}>
              Clear library filters
            </Button>
          </div>
        )}

        {ready &&
          (items.length ? (
            view === "table" ? (
              <div
                className="lib-table-region"
                role="region"
                aria-label="Scrollable library table"
                tabIndex={0}
              >
                <Table
                  label={
                    anime
                      ? "Anime library"
                      : movies
                        ? "Movie library"
                        : "TV library"
                  }
                >
                  <thead>
                    <tr>
                      {canSelect && (
                        <th scope="col" className="is-shrink">
                          <span className="sr-only">Select</span>
                        </th>
                      )}
                      <th scope="col" className="is-shrink">
                        <span className="sr-only">Monitoring</span>
                      </th>
                      <th scope="col">Title</th>
                      <th scope="col" className="is-shrink">
                        Year
                      </th>
                      {anime && <th scope="col">Format</th>}
                      <th scope="col">Quality profile</th>
                      <th scope="col">Status</th>
                      <th scope="col" className="is-actions">
                        <span className="sr-only">Actions</span>
                      </th>
                    </tr>
                  </thead>
                  {groups.map(([letter, rows]) => (
                    <tbody key={letter}>
                      <tr className="lib-group-row">
                        <th
                          scope="colgroup"
                          colSpan={(canSelect ? 8 : 7) + (anime ? 1 : 0)}
                        >
                          <h2
                            className="lib-group-heading"
                            id={groupID(letter)}
                          >
                            {letter}
                          </h2>
                        </th>
                      </tr>
                      {rows.map((item) => (
                        <tr
                          key={item.id}
                          className={
                            canSelect && selected.includes(item.id)
                              ? "is-selected"
                              : undefined
                          }
                        >
                          {canSelect && (
                            <td className="is-shrink">{selectBox(item)}</td>
                          )}
                          <td className="is-shrink">
                            <MonitorIcon monitored={item.monitored} />
                          </td>
                          <th scope="row">
                            <Link to={detailsPath(item)}>{item.title}</Link>
                          </th>
                          <td className="is-shrink">{item.year ?? "—"}</td>
                          {anime && (
                            <td>{item.kind === "movie" ? "Movie" : "Show"}</td>
                          )}
                          <td>
                            <ProfileLabel item={item} />
                          </td>
                          <td>
                            <Label kind={statusKind(item)}>
                              {statusLabel(item)}
                            </Label>
                          </td>
                          <td className="is-actions">
                            <ItemAction item={item} />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  ))}
                </Table>
              </div>
            ) : view === "overview" ? (
              <ul className="lib-overview" aria-label={`${title} overview`}>
                {groups.map(([letter, rows]) => (
                  <Fragment key={letter}>
                    <li className="lib-group-row">
                      <h2 className="lib-group-heading" id={groupID(letter)}>
                        {letter}
                      </h2>
                    </li>
                    {rows.map((item) => (
                      <li
                        key={item.id}
                        className={
                          canSelect && selected.includes(item.id)
                            ? "lib-overview-row is-selected"
                            : "lib-overview-row"
                        }
                      >
                        {canSelect && (
                          <span className="lib-overview-select">
                            {selectBox(item)}
                          </span>
                        )}
                        <Link
                          className="lib-overview-poster"
                          to={detailsPath(item)}
                          aria-label={`Open ${item.title} details`}
                        >
                          <Poster movie={item} />
                          <span
                            className={`lib-status-bar is-${statusKind(item)}`}
                            title={statusLabel(item)}
                          />
                        </Link>
                        <div className="lib-overview-body">
                          <div className="lib-overview-head">
                            <MonitorIcon monitored={item.monitored} />
                            <h2 className="lib-overview-title">
                              <Link to={detailsPath(item)}>{item.title}</Link>
                            </h2>
                            <span className="rs-muted">{item.year ?? ""}</span>
                          </div>
                          <div className="lib-overview-meta">
                            <Label kind={statusKind(item)}>
                              {statusLabel(item)}
                            </Label>
                            {anime && (
                              <Label outline>
                                {item.kind === "movie" ? "Movie" : "Show"}
                              </Label>
                            )}
                            <ProfileLabel item={item} />
                            <ItemAction item={item} />
                          </div>
                          {item.overview && (
                            <p className="lib-overview-text">{item.overview}</p>
                          )}
                        </div>
                      </li>
                    ))}
                  </Fragment>
                ))}
              </ul>
            ) : (
              <ul className="lib-grid" aria-label={`${title} posters`}>
                {groups.map(([letter, rows]) => (
                  <Fragment key={letter}>
                    <li className="lib-group-row">
                      <h2 className="lib-group-heading" id={groupID(letter)}>
                        {letter}
                      </h2>
                    </li>
                    {rows.map((item) => (
                      <li
                        className={
                          canSelect && selected.includes(item.id)
                            ? "lib-card is-selected"
                            : "lib-card"
                        }
                        key={item.id}
                      >
                        {canSelect && (
                          <span className="lib-card-select">
                            {selectBox(item)}
                          </span>
                        )}
                        <Link
                          className="lib-card-art"
                          to={detailsPath(item)}
                          aria-label={`Open ${item.title} details`}
                        >
                          <Poster movie={item} />
                        </Link>
                        <span
                          className={`lib-status-bar is-${statusKind(item)}`}
                          title={statusLabel(item)}
                        />
                        <div className="lib-card-info">
                          <div className="lib-card-line">
                            <h2 className="lib-card-title">
                              <Link to={detailsPath(item)} title={item.title}>
                                {item.title}
                              </Link>
                            </h2>
                            <MonitorIcon monitored={item.monitored} />
                          </div>
                          <div className="lib-card-meta">
                            <span>{item.year ?? "Year unknown"}</span>
                            {anime && (
                              <span>
                                {item.kind === "movie" ? "Movie" : "Show"}
                              </span>
                            )}
                            <span className="sr-only">{statusLabel(item)}</span>
                          </div>
                          <ProfileLabel item={item} />
                        </div>
                      </li>
                    ))}
                  </Fragment>
                ))}
              </ul>
            )
          ) : page > 1 ? (
            <EmptyState
              title={`No ${noun} on this page`}
              actions={
                <Button onClick={() => change("page", "1")}>
                  Go to first page
                </Button>
              }
            >
              Return to the first page with these filters.
            </EmptyState>
          ) : filtered ? (
            <EmptyState
              icon={Filter}
              title={`No matching ${noun}`}
              actions={
                <Button onClick={() => setParams({})}>
                  Clear library filters
                </Button>
              }
            >
              Try a different title or clear the filters.
            </EmptyState>
          ) : (
            <EmptyState
              icon={LayoutGrid}
              title={
                anime
                  ? "No anime in the library"
                  : movies
                    ? "No movies in the library"
                    : "No shows in the library"
              }
              actions={
                <>
                  <Button asChild>
                    <Link to={searchPath}>
                      {anime
                        ? "Add Anime"
                        : movies
                          ? "Add Movies"
                          : "Add Shows"}
                    </Link>
                  </Button>
                  <Button asChild variant="soft">
                    <Link to={importPath}>Import existing files</Link>
                  </Button>
                </>
              }
            >
              Add {noun} from TMDb or register files already in your library.
            </EmptyState>
          ))}

        {!resource.error && (
          <footer className="lib-footer">
            <p role="status" className="lib-totals">
              {ready
                ? `${data.total} matching ${data.total === 1 ? singular : noun}${
                    filtered && data.collection_total !== undefined
                      ? ` of ${data.collection_total}`
                      : ""
                  }${
                    items.length
                      ? capped
                        ? ` · showing ${(page - 1) * data.limit + 1}–${(page - 1) * data.limit + items.length}`
                        : " · all shown"
                      : ""
                  }`
                : ""}
            </p>
            {ready && capped && (
              <Pagination
                page={data.page}
                pages={Math.ceil(data.total / data.limit)}
                onPage={(p) => change("page", String(p))}
              />
            )}
          </footer>
        )}
        {ready && !movies && (
          <p className="rs-muted rs-small">
            Show status reflects registered files and tracked downloads, not
            full episode coverage.
          </p>
        )}

        {canSelect && ready && items.length > 0 && (
          <div className="lib-select-bar" aria-label="Selection">
            <div className="lib-select-summary">
              <label className="lib-select-page">
                <input
                  type="checkbox"
                  aria-label="Select all shown"
                  checked={allSelected}
                  ref={(element) => {
                    if (element)
                      element.indeterminate =
                        selected.length > 0 && !allSelected;
                  }}
                  onChange={(e) => {
                    setSelected(
                      e.target.checked ? selectable.map((item) => item.id) : [],
                    );
                    setMonitoringMessage("");
                  }}
                />
                <span aria-hidden="true">Select all shown</span>
              </label>
              <strong>{selected.length} selected</strong>
              <span className="rs-muted rs-small">
                {capped
                  ? `Select all shown covers the ${items.length} ${items.length === 1 ? singular : noun} on this page; changing pages or filters clears the selection.`
                  : `Select all shown covers all ${items.length} ${items.length === 1 ? singular : noun} currently shown; changing filters clears the selection.`}
              </span>
              {selected.length > 0 && (
                <Button
                  size="1"
                  variant="ghost"
                  color="gray"
                  onClick={() => setSelected([])}
                >
                  Clear selection
                </Button>
              )}
            </div>
            <div className="lib-select-editors">
              {selectedGroups.map((group) => (
                <section
                  key={group.kind}
                  className="lib-select-group"
                  aria-label={
                    anime
                      ? `Selected anime ${group.kind === "movie" ? "movies" : "shows"}`
                      : undefined
                  }
                >
                  <BulkMonitoring
                    kind={group.kind}
                    ids={group.ids}
                    label={
                      anime
                        ? `${group.ids.length} ${group.kind === "movie" ? (group.ids.length === 1 ? "movie" : "movies") : group.ids.length === 1 ? "show" : "shows"}`
                        : undefined
                    }
                    onApplied={applied}
                  >
                    {profileContext && (
                      <BulkProfiles
                        kind={group.kind}
                        ids={group.ids}
                        context={profileContext}
                        onApplied={applied}
                      />
                    )}
                  </BulkMonitoring>
                </section>
              ))}
              {selectedGroups.length === 0 && (
                <span className="rs-muted rs-small">
                  Select titles to edit monitoring or quality profiles.
                </span>
              )}
            </div>
          </div>
        )}
        {kind !== "movie" && (
          <MonitorAllFutureEpisodes
            category={kind === "anime" ? "anime" : "standard"}
            onDone={reload}
          />
        )}
        {kind === "anime" && <AnimeDubSettings />}
      </PageBody>
    </>
  );
}

/**
 * One-time bulk action: monitor the show, its regular seasons and episodes for
 * every show in this collection. Kept at the bottom of the page because it is
 * normally used once, and always behind an explicit confirmation.
 */
function MonitorAllFutureEpisodes({
  category,
  onDone,
}: {
  category: "standard" | "anime";
  onDone: () => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const label = category === "anime" ? "anime shows" : "TV shows";
  async function apply() {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const out = await api<{ shows: number }>(
        "/library/monitor-all-episodes",
        {
          method: "POST",
          body: JSON.stringify({ category }),
        },
      );
      setMessage(
        `Monitoring turned on for ${out.shows} ${label}. Only regular episodes releasing today or later are monitored; past and undated regular episodes are off. Specials keep their current choices.`,
      );
      onDone();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
      setConfirming(false);
    }
  }
  return (
    <section className="lib-maintenance" aria-label="Library maintenance">
      <div>
        <h2>Monitor all future episodes</h2>
        <p>
          Monitors only regular episodes releasing today or later. Past and
          undated regular episodes are unmonitored; specials keep their current
          choices. Use it once when setting up a library; individual choices can
          still be changed afterwards.
        </p>
        {message && <Alert kind="success">{message}</Alert>}
        {error && <Alert kind="danger">{error}</Alert>}
      </div>
      <div>
        <Button
          variant="soft"
          disabled={busy}
          onClick={() => setConfirming(true)}
        >
          Monitor all future episodes
        </Button>
      </div>
      {confirming && (
        <Modal
          open
          onOpenChange={(open) => !open && setConfirming(false)}
          title={`Monitor future episodes of your ${label}?`}
          footer={
            <>
              <ModalClose>
                <Button variant="soft" color="gray">
                  Cancel
                </Button>
              </ModalClose>
              <Button disabled={busy} onClick={() => void apply()}>
                {busy ? "Updating…" : "Monitor future episodes"}
              </Button>
            </>
          }
        >
          <p>
            Shows in this collection will be monitored, but only regular
            episodes releasing today or later are turned on. Past and undated
            regular episodes are turned off; specials are left exactly as they
            are. This does not download anything by itself; the automation
            schedule decides what to search.
          </p>
        </Modal>
      )}
    </section>
  );
}

export function AnimeDubSettings() {
  const prefs = useResource<AnimePreferences>("/library/anime/preferences");
  const [rule, setRule] = useState("");
  const [testTitle, setTestTitle] = useState("");
  const [testResult, setTestResult] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [confirming, setConfirming] = useState<"enable" | "disable" | null>(
    null,
  );

  useEffect(() => {
    if (prefs.data) setRule(prefs.data.dub_rule);
  }, [prefs.data]);

  async function saveRule() {
    if (!prefs.data || busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await api("/library/anime/preferences", {
        method: "PUT",
        body: JSON.stringify({ dub_rule: rule, revision: prefs.data.revision }),
      });
      prefs.reload();
      setMessage("English-dub rule saved.");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function test() {
    if (busy || !testTitle.trim()) return;
    setBusy(true);
    setError("");
    try {
      const result = await api<{ matches: boolean }>(
        "/library/anime/dub-test",
        {
          method: "POST",
          body: JSON.stringify({ rule, title: testTitle.trim() }),
        },
      );
      setTestResult(result.matches);
    } catch (e) {
      setError(messageFor(e));
      setTestResult(null);
    } finally {
      setBusy(false);
    }
  }

  async function applyAll(enabled: boolean) {
    if (busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const result = await api<{ shows: number }>(
        "/library/anime/english-dub",
        {
          method: "POST",
          body: JSON.stringify({ enabled, reviewed: true }),
        },
      );
      setMessage(
        `${result.shows} Anime shows updated. Existing downloads and files are unchanged.`,
      );
      setConfirming(null);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="lib-maintenance" aria-label="Anime English dub">
      <div>
        <h2>English dub only</h2>
        <p>
          Set the per-show English-dub filter for every Anime show, and edit the
          rule used to recognize dub releases. The rule accepts one term per
          line: <code>/regex/i</code> or a case-insensitive phrase. A release is
          a dub when any term matches.
        </p>
        {message && <Alert kind="success">{message}</Alert>}
        {error && <Alert kind="danger">{error}</Alert>}
        <div className="rs-row">
          <Button
            variant="soft"
            disabled={busy}
            onClick={() => setConfirming("enable")}
          >
            Enable for all anime
          </Button>
          <Button
            variant="soft"
            color="gray"
            disabled={busy}
            onClick={() => setConfirming("disable")}
          >
            Disable for all anime
          </Button>
        </div>
        <label htmlFor="anime-dub-rule">English-dub rule</label>
        <textarea
          id="anime-dub-rule"
          className="anime-dub-rule"
          rows={8}
          value={rule}
          onChange={(e) => setRule(e.target.value)}
        />
        <div className="rs-row">
          <Button
            disabled={busy || !prefs.data}
            onClick={() => void saveRule()}
          >
            Save rule
          </Button>
          <Button
            variant="soft"
            color="gray"
            disabled={busy || !prefs.data}
            onClick={() => setRule(prefs.data?.default_rule ?? "")}
          >
            Reset to default
          </Button>
        </div>
        <div className="rs-row">
          <TextField.Root
            aria-label="Test release title"
            placeholder="Test a release title"
            value={testTitle}
            onChange={(e) => setTestTitle(e.target.value)}
          />
          <Button
            variant="soft"
            disabled={busy || !testTitle.trim()}
            onClick={() => void test()}
          >
            Test
          </Button>
          {testResult !== null && (
            <Label kind={testResult ? "success" : "warning"}>
              {testResult ? "Matches" : "Does not match"}
            </Label>
          )}
        </div>
      </div>
      {confirming && (
        <Modal
          open
          onOpenChange={(open) => !open && setConfirming(null)}
          title={
            confirming === "enable"
              ? "Enable English dub only for all anime?"
              : "Disable English dub only for all anime?"
          }
          footer={
            <>
              <ModalClose>
                <Button variant="soft" color="gray">
                  Cancel
                </Button>
              </ModalClose>
              <Button
                disabled={busy}
                onClick={() => void applyAll(confirming === "enable")}
              >
                Apply
              </Button>
            </>
          }
        >
          <p>
            {confirming === "enable"
              ? "Every Anime show will reject releases that are not labeled as an English dub under the saved rule."
              : "Every Anime show will stop applying the English-dub filter."}{" "}
            Existing downloads and files are unchanged.
          </p>
        </Modal>
      )}
    </section>
  );
}
