import { useEffect, useState, type FormEvent } from "react";
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
  Rows3,
  Search,
  Table2,
} from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import {
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
  PageBody,
  PageToolbar,
  Table,
  ToolbarButton,
  ToolbarSection,
  ToolbarSeparator,
  type Kind,
} from "../ui";
import { useResource } from "../useResource";
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
  const [input, setInput] = useState(params.get("query") ?? "");
  useEffect(() => setInput(params.get("query") ?? ""), [params]);
  const showAll = params.get("page") === "all";
  const page = showAll ? 1 : Number(params.get("page") ?? 1);
  const apiParams = new URLSearchParams({
    kind,
    page: params.get("page") ?? "1",
  });
  for (const [key, value] of params)
    if (key !== "view" && key !== "page" && key !== "kind")
      apiParams.append(key, value);
  const resource = useResource<LibraryPage>(`/library?${apiParams.toString()}`);
  const selectionScope = apiParams.toString();
  const [selecting, setSelecting] = useState(false);
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
    if (!showAll && key !== "view" && key !== "page") next.delete("page");
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
  const activeFilters = ["status", "monitored", "profile"].filter(
    (k) => !!params.get(k) && params.get(k) !== "all",
  ).length;
  const filtered = activeFilters > 0 || !!params.get("query");
  const [filtersOpen, setFiltersOpen] = useState(activeFilters > 0);
  const canSelect = selecting && !showAll;
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
  const allSelected =
    items.length > 0 && items.every((item) => selected.includes(item.id));
  function applied(message: string) {
    setSelected([]);
    setMonitoringMessage(message);
    resource.reload();
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
          <ToolbarSeparator />
          <ToolbarButton
            icon={ListChecks}
            label="Select"
            active={selecting}
            disabled={showAll}
            title={
              showAll
                ? "Selection works one page at a time. Switch back to 48 per page."
                : undefined
            }
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

        <RequestState
          loading={resource.loading}
          error={resource.error}
          retry={resource.reload}
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
                  <tbody>
                    {items.map((item) => (
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
                </Table>
              </div>
            ) : view === "overview" ? (
              <ul className="lib-overview" aria-label={`${title} overview`}>
                {items.map((item) => (
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
              </ul>
            ) : (
              <ul className="lib-grid" aria-label={`${title} posters`}>
                {items.map((item) => (
                  <li
                    className={
                      canSelect && selected.includes(item.id)
                        ? "lib-card is-selected"
                        : "lib-card"
                    }
                    key={item.id}
                  >
                    {canSelect && (
                      <span className="lib-card-select">{selectBox(item)}</span>
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
                          <Link to={detailsPath(item)}>{item.title}</Link>
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
                      ? ` · ${(data.page - 1) * data.limit + 1}–${(data.page - 1) * data.limit + items.length} shown`
                      : ""
                  }`
                : ""}
            </p>
            {ready && !showAll && (
              <Pagination
                page={data.page}
                pages={Math.ceil(data.total / data.limit)}
                onPage={(p) => change("page", String(p))}
              />
            )}
            <label className="lib-filter lib-per-page">
              <span>Per page</span>
              <select
                aria-label="Library items per page"
                value={showAll ? "all" : "48"}
                onChange={(event) =>
                  change("page", event.target.value === "all" ? "all" : "")
                }
              >
                <option value="48">48</option>
                <option value="all">Show all</option>
              </select>
            </label>
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
                  aria-label="Select all titles on this page"
                  checked={allSelected}
                  ref={(element) => {
                    if (element)
                      element.indeterminate =
                        selected.length > 0 && !allSelected;
                  }}
                  onChange={(e) => {
                    setSelected(
                      e.target.checked ? items.map((item) => item.id) : [],
                    );
                    setMonitoringMessage("");
                  }}
                />
                <span aria-hidden="true">Select page</span>
              </label>
              <strong>{selected.length} selected on this page</strong>
              <span className="rs-muted rs-small">
                Clears when filters or pages change.
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
                    {data.profile_context && (
                      <BulkProfiles
                        kind={group.kind}
                        ids={group.ids}
                        context={data.profile_context}
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
      </PageBody>
    </>
  );
}
