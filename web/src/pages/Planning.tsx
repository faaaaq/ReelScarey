import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api } from "../api";
import {
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Clapperboard,
  Eye,
  Film,
  LayoutGrid,
  List,
  RefreshCw,
  RotateCcw,
  Search,
  Ticket,
  Tv,
} from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  BatchProgress,
  DescriptionItem,
  DescriptionList,
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
  episodeCode,
  formatRelative,
  restoreNotices,
  useHiddenNotices,
  type Kind,
} from "../ui";
import { ItemSearchResultAlert, useItemSearch } from "./details/ItemSearch";
import { bulkSearchMessage, useBulkSearch } from "./details/bulkSearch";
import { useLiveResource } from "../useLiveResource";
import { Pager } from "./activity/Pager";
import { ToolbarMenu } from "./activity/ToolbarMenu";
import "./Planning.css";

export type PlanningEntry = {
  runtime_minutes?: number | null;
  series_status?: string;
  metadata_updated_at?: number;
  kind: "movie" | "series";
  movie_version?: string;
  media_id: number;
  episode_id: number | null;
  title: string;
  year: number | null;
  episode_title: string;
  season: number | null;
  episode: number | null;
  date: string;
  monitored: boolean;
  monitoring_reason: string;
  available: boolean;
  status: string;
  download_id: string;
  download_state: string;
  protocol: string;
  poster_url: string;
  overview?: string;
  /** Movie release edition; optional until the metadata fields are saved. */
  release_kind?: "theatrical" | "digital" | "physical";
  theatrical_date?: string;
  digital_date?: string;
  physical_date?: string;
};
export type PlanningData = {
  items: PlanningEntry[];
  counts: Record<string, number>;
  coverage: { unloaded_seasons: number; undated: number };
  total: number;
  page: number;
  limit: number;
  as_of_date: string;
  month: string;
  kind: string;
  status: string;
  query: string;
  monitored_only: boolean;
};
const labels: Record<string, string> = {
  missing: "Missing",
  upcoming: "Upcoming",
  undated: "No date",
  downloading: "In progress",
  attention: "Needs attention",
  available: "In library",
};
const kinds: Record<string, Kind> = {
  missing: "danger",
  upcoming: "info",
  undated: "default",
  downloading: "purple",
  attention: "warning",
  available: "success",
};
const wantedStatuses = [
  "missing",
  "downloading",
  "attention",
  "upcoming",
  "undated",
];
const releaseLabels: Record<string, string> = {
  theatrical: "Theatrical",
  digital: "Digital",
  physical: "Physical",
};
function releaseLabel(entry: PlanningEntry) {
  return entry.release_kind ? releaseLabels[entry.release_kind] : "";
}
function knownReleaseDates(entry: PlanningEntry) {
  return [
    { kind: "Theatrical", date: entry.theatrical_date },
    { kind: "Digital", date: entry.digital_date },
    { kind: "Physical", date: entry.physical_date },
  ].filter((value) => !!value.date);
}
const viewKey = "reelscarey.calendar.view";
const calendarFiltersKey = "reelscarey.calendar.filters";

/** "Updated just now" until the next background refresh replaces it. */
function updatedLabel(timestamp: number | undefined) {
  if (!timestamp) return "";
  if (Date.now() - timestamp < 60000) return "Updated just now";
  return `Updated ${formatRelative(Math.floor(timestamp / 1000))}`;
}

function dateLabel(value: string, month = false) {
  const date = new Date(`${value}${month ? "-01" : ""}T12:00:00Z`);
  if (!Number.isFinite(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    timeZone: "UTC",
    ...(month
      ? { month: "long", year: "numeric" }
      : { month: "short", day: "numeric", year: "numeric" }),
  }).format(date);
}
function shiftedMonth(month: string, step: number) {
  if (!/^\d{4}-\d{2}$/.test(month)) return "";
  const value = new Date(`${month}-01T12:00:00Z`);
  value.setUTCMonth(value.getUTCMonth() + step);
  if (
    !Number.isFinite(value.getTime()) ||
    value.getUTCFullYear() < 1 ||
    value.getUTCFullYear() > 9999
  )
    return "";
  return value.toISOString().slice(0, 7);
}
/** Day keys (YYYY-MM-DD) for complete Sunday-first weeks covering a month. */
function monthDays(month: string) {
  if (!/^\d{4}-\d{2}$/.test(month)) return [];
  const first = new Date(`${month}-01T12:00:00Z`);
  if (!Number.isFinite(first.getTime())) return [];
  const cursor = new Date(first.getTime());
  cursor.setUTCDate(1 - first.getUTCDay());
  const days: { key: string; day: number; inMonth: boolean }[] = [];
  do {
    for (let i = 0; i < 7; i++) {
      days.push({
        key: isoDay(cursor),
        day: cursor.getUTCDate(),
        inMonth: cursor.getUTCMonth() === first.getUTCMonth(),
      });
      cursor.setUTCDate(cursor.getUTCDate() + 1);
    }
  } while (cursor.getUTCMonth() === first.getUTCMonth());
  return days;
}
function isoDay(date: Date) {
  const y = String(date.getUTCFullYear()).padStart(4, "0");
  const m = String(date.getUTCMonth() + 1).padStart(2, "0");
  const d = String(date.getUTCDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}
const weekdays = Array.from({ length: 7 }, (_, i) =>
  new Intl.DateTimeFormat(undefined, {
    weekday: "short",
    timeZone: "UTC",
  }).format(new Date(Date.UTC(2023, 0, 1 + i, 12))),
);
function entryKey(entry: PlanningEntry) {
  return `${entry.kind}-${entry.media_id}-${entry.episode_id ?? "movie"}-${entry.movie_version ?? ""}`;
}
function entryCode(entry: PlanningEntry) {
  return entry.kind === "series" &&
    entry.season !== null &&
    entry.episode !== null
    ? episodeCode(entry.season, entry.episode)
    : "";
}
function entryPath(entry: PlanningEntry) {
  return entry.kind === "series"
    ? `/series/${entry.media_id}`
    : `/movies/${entry.media_id}`;
}
function releasePath(entry: PlanningEntry) {
  return entry.kind === "series"
    ? `/releases?series=${entry.media_id}&season=${entry.season}&episode=${entry.episode}`
    : `/releases?movie=${entry.media_id}${entry.movie_version ? `&movie_version=${entry.movie_version}` : ""}`;
}
function metadataNote(entry: PlanningEntry) {
  return entry.metadata_updated_at
    ? `Metadata refreshed ${new Date(entry.metadata_updated_at * 1000).toLocaleString()}`
    : "Saved catalog metadata";
}

export function Planning({ calendar = false }: { calendar?: boolean }) {
  const [params, setParams] = useSearchParams();
  const queryParam = params.get("query") ?? "";
  const [input, setInput] = useState(queryParam);
  useEffect(() => setInput(queryParam), [queryParam]);
  const [view, setViewState] = useState<"month" | "agenda">(() =>
    typeof localStorage !== "undefined" &&
    localStorage.getItem(viewKey) === "agenda"
      ? "agenda"
      : "month",
  );
  const [opened, setOpened] = useState<PlanningEntry>();
  const bulk = useBulkSearch();
  const schedule = useLiveResource<{
    mode?: "search" | "download";
    state?: string;
    message?: string;
  }>(calendar ? "/automation" : null, calendar ? 30000 : 0);
  const automationMode =
    schedule.data?.mode === "download"
      ? "download"
      : schedule.data?.mode === "search"
        ? "search"
        : "";
  const [scope, setScope] = useState<"movie" | "series" | null>(null);
  // The Calendar remembers the last filters and month; Reset clears them.
  const [ready, setReady] = useState(!calendar);
  useEffect(() => {
    if (!calendar) return;
    if (params.toString()) {
      setReady(true);
      return;
    }
    try {
      const saved = localStorage.getItem(calendarFiltersKey);
      if (saved) setParams(new URLSearchParams(saved), { replace: true });
    } catch {
      /* Storage can be unavailable; defaults stay. */
    }
    setReady(true);
    // Restore once per visit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => {
    if (!calendar || !ready) return;
    try {
      localStorage.setItem(calendarFiltersKey, params.toString());
    } catch {
      /* Storage can be unavailable. */
    }
  }, [calendar, ready, params]);
  function resetAll() {
    try {
      localStorage.removeItem(calendarFiltersKey);
    } catch {
      /* Storage can be unavailable. */
    }
    setInput("");
    setView("month");
    setParams({}, { replace: true });
  }
  function setView(next: "month" | "agenda") {
    setViewState(next);
    try {
      localStorage.setItem(viewKey, next);
    } catch {
      /* Storage can be unavailable; the view still changes for this visit. */
    }
  }
  const base = calendar ? "/calendar" : "/wanted";
  // Calendar and Wanted refresh while they are open. This keeps queue states
  // and newly finished downloads current without losing scroll or dialogs.
  const resource = useLiveResource<PlanningData>(
    `${base}?${params.toString()}`,
    calendar ? 25000 : 30000,
  );
  const data = resource.data;
  const status = params.get("status") || (calendar ? "all" : "missing");
  const statuses = calendar
    ? [...wantedStatuses.filter((v) => v !== "undated"), "available"]
    : wantedStatuses;
  function change(key: string, value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    next.delete("page");
    setParams(next);
  }
  function submit(e: FormEvent) {
    e.preventDefault();
    change("query", input.trim());
  }
  const previous = shiftedMonth(data?.month ?? "", -1),
    next = shiftedMonth(data?.month ?? "", 1);
  const undated = new URLSearchParams(params);
  undated.delete("month");
  undated.delete("page");
  undated.set("status", "undated");
  const today = data?.as_of_date?.slice(0, 7) ?? "";
  const monthView = calendar && view === "month";
  const noticeKeys = [
    ...(data && data.coverage.unloaded_seasons > 0
      ? ["coverage-unloaded-seasons"]
      : []),
    ...(calendar && data && data.coverage.undated > 0
      ? ["calendar-undated"]
      : []),
    ...(monthView && data && data.total > data.items.length
      ? ["calendar-page-limit"]
      : []),
  ];
  const hiddenNotes = useHiddenNotices(noticeKeys);
  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={resource.loading}
            disabled={resource.loading}
            onClick={resource.reload}
          />
          <ToolbarSeparator />
          {calendar ? (
            <>
              <ToolbarButton icon={List} label="Wanted" to="/wanted" />
              <ToolbarButton
                icon={Film}
                label="Search movies"
                title="Search every missing monitored movie now"
                disabled={bulk.state.status === "running"}
                onClick={() => setScope("movie")}
              />
              <ToolbarButton
                icon={Tv}
                label="Search episodes"
                title="Search every missing monitored TV episode now"
                disabled={bulk.state.status === "running"}
                onClick={() => setScope("series")}
              />
            </>
          ) : (
            <ToolbarButton
              icon={CalendarDays}
              label="Calendar"
              to="/calendar"
            />
          )}
        </ToolbarSection>
        <ToolbarSection align="right">
          {calendar && (
            <>
              <ToolbarButton
                icon={LayoutGrid}
                label="Month"
                active={view === "month"}
                onClick={() => setView("month")}
              />
              <ToolbarButton
                icon={List}
                label="Agenda"
                active={view === "agenda"}
                onClick={() => setView("agenda")}
              />
              <ToolbarSeparator />
            </>
          )}
          {calendar && (
            <ToolbarMenu
              icon={Ticket}
              label="Releases"
              menuLabel="Releases"
              value={params.get("release") ?? "all"}
              options={[
                { value: "all", label: "All releases" },
                { value: "theatrical", label: "Theatrical" },
                { value: "digital", label: "Digital" },
                { value: "physical", label: "Physical" },
              ]}
              onChange={(value) =>
                change("release", value === "all" ? "" : value)
              }
            />
          )}
          <ToolbarMenu
            icon={Clapperboard}
            label="Type"
            menuLabel="Media type"
            value={params.get("kind") ?? "all"}
            options={[
              { value: "all", label: "Movies & TV" },
              { value: "movie", label: "Movies" },
              { value: "series", label: "TV episodes" },
            ]}
            onChange={(value) => change("kind", value)}
          />
          <ToolbarMenu
            icon={Eye}
            label="Monitored"
            menuLabel="Monitoring filter"
            value={params.get("monitored") ?? "monitored"}
            options={[
              { value: "monitored", label: "Monitored only" },
              { value: "all", label: "Include paused" },
            ]}
            onChange={(value) => change("monitored", value)}
          />
          <ToolbarButton icon={RotateCcw} label="Reset" onClick={resetAll} />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title={calendar ? "Calendar" : "Wanted"} />
        {calendar && bulk.state.status === "running" && (
          <div className="plan-bulk-progress">
            <BatchProgress
              label="Calendar bulk search"
              message={bulkSearchMessage(bulk.state)}
              detail={
                bulk.state.active && bulk.state.message
                  ? bulk.state.message
                  : undefined
              }
            />
          </div>
        )}
        {calendar &&
          (bulk.state.status === "done" || bulk.state.status === "error") && (
            <Alert
              kind={bulk.state.status === "error" ? "danger" : "success"}
              role="status"
            >
              <p>{bulkSearchMessage(bulk.state)}</p>
              <div className="rs-row">
                <Link className="rs-link-button rs-small" to="/automation">
                  Open Automation for details
                </Link>
                <button
                  type="button"
                  className="rs-link-button rs-small"
                  onClick={bulk.reset}
                >
                  Dismiss
                </button>
              </div>
            </Alert>
          )}
        <div className="plan-filterbar">
          <div className="plan-status" role="group" aria-label="Status">
            <button
              type="button"
              className="plan-status-button"
              aria-pressed={status === "all"}
              onClick={() => change("status", "all")}
            >
              {calendar ? "All statuses" : "All not in library"}
            </button>
            {statuses.map((key) => (
              <button
                type="button"
                key={key}
                className="plan-status-button"
                aria-pressed={status === key}
                aria-label={`${labels[key]}: ${data?.counts[key] ?? 0}`}
                disabled={!data}
                onClick={() => change("status", key)}
              >
                <span
                  className={`plan-dot is-${kinds[key]}`}
                  aria-hidden="true"
                />
                {labels[key]}
                {data && (
                  <span className="plan-count">{data.counts[key] ?? 0}</span>
                )}
              </button>
            ))}
          </div>
          <form className="plan-search" onSubmit={submit}>
            <TextField.Root
              aria-label="Find in saved catalog"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              maxLength={200}
              placeholder="Find a title or episode…"
            >
              <TextField.Slot>
                <Search size={14} aria-hidden="true" />
              </TextField.Slot>
            </TextField.Root>
            <Button type="submit" variant="soft">
              Filter titles
            </Button>
          </form>
        </div>
        {calendar && (
          <div
            className="plan-month-nav"
            aria-label="Month navigation"
            role="group"
          >
            <div className="plan-month-buttons">
              <Button
                variant="soft"
                color="gray"
                aria-label="Previous month"
                disabled={!previous || resource.loading}
                onClick={() => change("month", previous)}
              >
                <ChevronLeft size={16} aria-hidden="true" />
              </Button>
              <Button
                variant="soft"
                color="gray"
                disabled={!data || data.month === today}
                onClick={() => change("month", today)}
              >
                Today
              </Button>
              <Button
                variant="soft"
                color="gray"
                aria-label="Next month"
                disabled={!next || resource.loading}
                onClick={() => change("month", next)}
              >
                <ChevronRight size={16} aria-hidden="true" />
              </Button>
            </div>
            <h2 className="plan-month-title">
              {data ? dateLabel(data.month, true) : "Saved release dates"}
            </h2>
            <input
              aria-label="Calendar month"
              type="month"
              className="plan-month-input"
              min="0001-01"
              max="9999-12"
              value={params.get("month") ?? data?.month ?? ""}
              onChange={(e) => change("month", e.target.value)}
            />
          </div>
        )}
        <RequestState
          loading={resource.loading && !data}
          error={data ? "" : resource.error}
          retry={resource.reload}
        />
        {data && resource.error && (
          <Alert kind="warning" role="status" title="Could not refresh">
            {resource.error} Showing the last data received.{" "}
            <button
              type="button"
              className="rs-link-button rs-small"
              onClick={resource.reload}
            >
              Retry now
            </button>
          </Alert>
        )}
        {data && (
          <>
            {data.coverage.unloaded_seasons > 0 && (
              <Alert
                kind="info"
                dismissKey="coverage-unloaded-seasons"
                title={`${data.coverage.unloaded_seasons} ${
                  data.coverage.unloaded_seasons === 1
                    ? "season's episodes are"
                    : "seasons' episodes are"
                } not loaded yet`}
              >
                <p>
                  Episode lists are fetched from TMDb per season, so episodes
                  from these seasons are not shown here. Turn on the{" "}
                  <Link to="/metadata-refresh">Metadata refresh schedule</Link>{" "}
                  to load them gradually, or open a show in{" "}
                  <Link to="/series">TV Shows</Link> and expand a season to load
                  it now.
                </p>
              </Alert>
            )}
            {calendar && data.coverage.undated > 0 && (
              <Alert kind="info" dismissKey="calendar-undated">
                <p>
                  {data.coverage.undated} missing{" "}
                  {data.coverage.undated === 1 ? "item has" : "items have"} no
                  valid date and {data.coverage.undated === 1 ? "is" : "are"}{" "}
                  not on the calendar.{" "}
                  <Link to={`/wanted?${undated.toString()}`}>
                    Review undated items
                  </Link>
                </p>
              </Alert>
            )}
            {hiddenNotes.length > 0 && (
              <p className="rs-muted rs-small">
                {hiddenNotes.length} coverage{" "}
                {hiddenNotes.length === 1 ? "note is" : "notes are"} hidden.{" "}
                <button
                  type="button"
                  className="rs-link-button"
                  onClick={() => restoreNotices(hiddenNotes)}
                >
                  Show again
                </button>
              </p>
            )}
            {data.items.length === 0 && data.total > 0 ? (
              <EmptyState
                title="This page has no items."
                actions={
                  <Button
                    onClick={() => {
                      const p = new URLSearchParams(params);
                      p.delete("page");
                      setParams(p);
                    }}
                  >
                    First page
                  </Button>
                }
              >
                <p>Return to the first page to see the current results.</p>
              </EmptyState>
            ) : monthView ? (
              <>
                {data.total > data.items.length && (
                  <Alert kind="info" dismissKey="calendar-page-limit">
                    This month has {data.total} items; the grid shows{" "}
                    {(data.page - 1) * data.limit + 1}–
                    {Math.min(data.page * data.limit, data.total)}. Use the
                    pages below for the rest.
                  </Alert>
                )}
                <MonthGrid
                  month={data.month}
                  today={data.as_of_date}
                  items={data.items}
                  onOpen={setOpened}
                />
                {data.total === 0 && (
                  <p className="rs-muted">
                    No saved releases match this month and these filters.
                  </p>
                )}
              </>
            ) : data.items.length === 0 ? (
              <EmptyState
                icon={calendar ? CalendarDays : Search}
                title={
                  calendar
                    ? "No saved releases match this month."
                    : "No items match these filters."
                }
                actions={
                  <Button asChild variant="soft">
                    <Link to="/search">Add movies or shows</Link>
                  </Button>
                }
              >
                <p>
                  Try another status, include paused titles, or add titles and
                  load TV episodes.
                </p>
              </EmptyState>
            ) : (
              <PlanningTable items={data.items} calendar={calendar} />
            )}
            <Pager
              page={data.page}
              pages={Math.ceil(data.total / data.limit)}
              onPage={(page) => {
                const p = new URLSearchParams(params);
                p.set("page", String(page));
                setParams(p);
              }}
            />
            <div className="plan-notes rs-muted rs-small">
              <p>
                {data.total} matching {data.total === 1 ? "item" : "items"}.
                Based on your saved catalog as of {dateLabel(data.as_of_date)}{" "}
                (server timezone); availability reflects registered files and
                tracked jobs.
              </p>
              {calendar && resource.updatedAt !== undefined && (
                <p className="plan-updated" role="status">
                  {updatedLabel(resource.updatedAt)}
                </p>
              )}
              {calendar && (
                <p>
                  Movie dates may be theatrical releases. Theatrical, digital
                  and physical dates are shown separately when saved. They do
                  not establish download availability or broadcast times.
                </p>
              )}
            </div>
          </>
        )}
      </PageBody>
      {opened && (
        <EntryModal entry={opened} onClose={() => setOpened(undefined)} />
      )}
      {scope && (
        <Modal
          open
          onOpenChange={(open) => !open && setScope(null)}
          title={
            scope === "movie"
              ? "Search every missing monitored movie?"
              : "Search every missing monitored episode?"
          }
          footer={
            <>
              <ModalClose>
                <Button variant="soft" color="gray">
                  Cancel
                </Button>
              </ModalClose>
              <Button
                onClick={() => {
                  const target = scope;
                  setScope(null);
                  void bulk.run(() =>
                    api("/automation/search-bulk", {
                      method: "POST",
                      body: JSON.stringify({ scope: target }),
                    }),
                  );
                }}
              >
                Start search
              </Button>
            </>
          }
        >
          <p>
            ReelScarey searches{" "}
            {scope === "movie"
              ? "every missing monitored movie"
              : "every missing monitored TV episode"}{" "}
            with your saved quality profiles.{" "}
            {automationMode === "download"
              ? "Your saved automation mode is Download: the best matching release of each is queued."
              : automationMode === "search"
                ? "Your saved automation mode is Search-only: matching results are saved for review in Automation and nothing is downloaded."
                : "The saved automation mode could not be loaded; Automation shows whether new runs download or only save results for review."}
          </p>
          <p className="rs-muted">
            Large libraries can take several runs. Nothing is overwritten and
            existing files stay as they are.
          </p>
        </Modal>
      )}
    </>
  );
}

function StatusLabels({ entry }: { entry: PlanningEntry }) {
  return (
    <span className="plan-labels">
      <Label kind={kinds[entry.status] ?? "default"}>
        {labels[entry.status] ?? entry.status}
      </Label>
      {!entry.monitored && <Label kind="disabled">Not monitored</Label>}
    </span>
  );
}
function downloadText(entry: PlanningEntry) {
  return `${entry.protocol === "usenet" ? "SABnzbd" : "qBittorrent"} · ${entry.download_state.replaceAll("_", " ")}`;
}
function EntryAction({ entry }: { entry: PlanningEntry }) {
  if (entry.download_id)
    return (
      <Button asChild size="1" variant="soft">
        <Link to={`/activity?job=${encodeURIComponent(entry.download_id)}`}>
          View activity
        </Link>
      </Button>
    );
  if (!entry.available)
    return (
      <Button asChild size="1" variant="soft">
        <Link to={releasePath(entry)}>Find releases</Link>
      </Button>
    );
  return null;
}

function PlanningTable({
  items,
  calendar,
}: {
  items: PlanningEntry[];
  calendar: boolean;
}) {
  return (
    <Table
      label={calendar ? "Calendar agenda" : "Wanted"}
      className="plan-table"
    >
      <thead>
        <tr>
          <th scope="col" className="is-shrink">
            <span className="sr-only">Type</span>
          </th>
          <th scope="col">Title</th>
          <th scope="col" className="is-shrink">
            Episode
          </th>
          <th scope="col" className="plan-col-episode-title">
            Episode title
          </th>
          <th scope="col" className="is-shrink">
            {calendar ? "Date" : "Air date"}
          </th>
          <th scope="col">Status</th>
          <th scope="col" className="is-actions">
            <span className="sr-only">Actions</span>
          </th>
        </tr>
      </thead>
      <tbody>
        {items.map((entry) => {
          const tv = entry.kind === "series";
          const details = [
            entry.runtime_minutes ? `${entry.runtime_minutes} min` : "",
            entry.series_status ?? "",
          ].filter(Boolean);
          return (
            <tr key={entryKey(entry)}>
              <td className="plan-kind" title={tv ? "TV episode" : "Movie"}>
                {tv ? (
                  <Tv size={15} aria-label="TV episode" />
                ) : (
                  <Film size={15} aria-label="Movie" />
                )}
              </td>
              <td className="plan-col-title" title={metadataNote(entry)}>
                <Link to={entryPath(entry)} className="plan-title-link">
                  {entry.title}
                </Link>
                {entry.year ? (
                  <span className="rs-muted"> ({entry.year})</span>
                ) : null}
                {entry.movie_version && (
                  <>
                    {" "}
                    <Label outline>
                      {entry.movie_version === "uhd" ? "4K" : "HD"}
                    </Label>
                  </>
                )}
                {!tv && entry.release_kind && (
                  <>
                    {" "}
                    <Label outline>{releaseLabel(entry)}</Label>
                  </>
                )}
                {entry.monitoring_reason && (
                  <span className="plan-sub">{entry.monitoring_reason}</span>
                )}
                {details.length > 0 && (
                  <span className="plan-sub">{details.join(" · ")}</span>
                )}
              </td>
              <td className="plan-code">{tv ? entryCode(entry) : "Movie"}</td>
              <td className="plan-col-episode-title">
                {tv ? entry.episode_title || "—" : ""}
              </td>
              <td className="plan-date">
                {entry.date ? (
                  <time dateTime={entry.date}>{dateLabel(entry.date)}</time>
                ) : (
                  <span className="rs-warning-text">Date unavailable</span>
                )}
              </td>
              <td>
                <StatusLabels entry={entry} />
                {entry.download_id && (
                  <span className="plan-sub">{downloadText(entry)}</span>
                )}
              </td>
              <td className="is-actions">
                <EntryAction entry={entry} />
              </td>
            </tr>
          );
        })}
      </tbody>
    </Table>
  );
}

function MonthGrid({
  month,
  today,
  items,
  onOpen,
}: {
  month: string;
  today: string;
  items: PlanningEntry[];
  onOpen: (entry: PlanningEntry) => void;
}) {
  const days = monthDays(month);
  const byDay = new Map<string, PlanningEntry[]>();
  for (const item of items)
    byDay.set(item.date, [...(byDay.get(item.date) ?? []), item]);
  const placed = new Set(days.map((d) => d.key));
  const outside = items.filter((item) => !placed.has(item.date));
  return (
    <>
      <div
        className="plan-calendar"
        role="table"
        aria-label={dateLabel(month, true)}
      >
        <div className="plan-weekdays" role="row">
          {weekdays.map((name) => (
            <div key={name} role="columnheader" className="plan-weekday">
              {name}
            </div>
          ))}
        </div>
        {Array.from({ length: days.length / 7 }, (_, week) => (
          <div className="plan-week" role="row" key={week}>
            {days.slice(week * 7, week * 7 + 7).map((day) => {
              const entries = byDay.get(day.key) ?? [];
              return (
                <div
                  key={day.key}
                  role="cell"
                  aria-label={dateLabel(day.key)}
                  className={[
                    "plan-day",
                    day.inMonth ? "" : "is-outside",
                    day.key === today ? "is-today" : "",
                  ]
                    .filter(Boolean)
                    .join(" ")}
                >
                  <span className="plan-day-number">{day.day}</span>
                  {entries.map((entry) => (
                    <EventChip
                      key={entryKey(entry)}
                      entry={entry}
                      onOpen={() => onOpen(entry)}
                    />
                  ))}
                </div>
              );
            })}
          </div>
        ))}
      </div>
      <div className="plan-legend rs-small" aria-label="Legend">
        {["available", "missing", "upcoming", "downloading", "attention"].map(
          (key) => (
            <span key={key} className="plan-legend-item">
              <span
                className={`plan-dot is-${kinds[key]}`}
                aria-hidden="true"
              />
              {labels[key]}
            </span>
          ),
        )}
        <span className="plan-legend-item">
          <span className="plan-dot is-disabled" aria-hidden="true" />
          Not monitored
        </span>
      </div>
      {outside.length > 0 && (
        <Alert kind="warning">
          {outside.length} {outside.length === 1 ? "item has" : "items have"} a
          date outside this month and {outside.length === 1 ? "is" : "are"}{" "}
          listed in the Agenda view.
        </Alert>
      )}
    </>
  );
}

function EventChip({
  entry,
  onOpen,
}: {
  entry: PlanningEntry;
  onOpen: () => void;
}) {
  const tv = entry.kind === "series";
  const kind = entry.monitored
    ? (kinds[entry.status] ?? "default")
    : "disabled";
  const code = entryCode(entry);
  return (
    <button
      type="button"
      className={`plan-event is-${kind}`}
      onClick={onOpen}
      title={`${entry.title}${code ? ` ${code}` : ""} · ${labels[entry.status] ?? entry.status}${entry.monitored ? "" : " · Not monitored"}`}
    >
      <span className="plan-event-title">
        {tv ? (
          <Tv size={12} aria-hidden="true" />
        ) : (
          <Film size={12} aria-hidden="true" />
        )}
        {entry.title}
      </span>
      <span className="plan-event-sub">
        {tv ? (
          `${code}${entry.episode_title ? ` · ${entry.episode_title}` : ""}`
        ) : (
          <>
            {[
              "Movie",
              entry.movie_version
                ? entry.movie_version === "uhd"
                  ? "4K"
                  : "HD"
                : "",
            ]
              .filter(Boolean)
              .join(" · ")}
            {entry.release_kind && (
              <>
                {" "}
                <span className="plan-release-tag">{releaseLabel(entry)}</span>
              </>
            )}
          </>
        )}
      </span>
    </button>
  );
}

function EntryModal({
  entry,
  onClose,
}: {
  entry: PlanningEntry;
  onClose: () => void;
}) {
  const tv = entry.kind === "series";
  const search = useItemSearch();
  function automatic() {
    search.trigger(entry.media_id, entry.episode_id ?? undefined);
  }
  return (
    <Modal
      open
      onOpenChange={(open) => !open && onClose()}
      title={`${entry.title}${entry.year ? ` (${entry.year})` : ""}`}
      footer={
        <>
          <Button asChild variant="soft">
            <Link to={entryPath(entry)}>{tv ? "View show" : "View movie"}</Link>
          </Button>
          <Button asChild variant="soft">
            <Link to={releasePath(entry)}>Manual search</Link>
          </Button>
          <Button disabled={!!search.pending} onClick={automatic}>
            <Search size={15} aria-hidden="true" />
            {search.pending ? "Searching…" : "Automatic search"}
          </Button>
          <ModalClose>
            <Button variant="soft" color="gray">
              Close
            </Button>
          </ModalClose>
        </>
      }
    >
      {tv && entry.episode_title && (
        <p className="plan-modal-episode">
          {entryCode(entry)} · {entry.episode_title}
        </p>
      )}
      {entry.overview ? (
        <p className="plan-modal-synopsis">{entry.overview}</p>
      ) : (
        <p className="rs-muted">No synopsis is saved for this title yet.</p>
      )}
      <DescriptionList>
        <DescriptionItem title={tv ? "Episode" : "Type"}>
          {tv
            ? `${entryCode(entry)}${entry.episode_title ? ` · ${entry.episode_title}` : ""}`
            : [
                "Movie",
                releaseLabel(entry),
                entry.movie_version
                  ? entry.movie_version === "uhd"
                    ? "4K"
                    : "HD"
                  : "",
              ]
                .filter(Boolean)
                .join(" · ")}
        </DescriptionItem>
        <DescriptionItem title="Date">
          {entry.date ? dateLabel(entry.date) : "Date unavailable"}
        </DescriptionItem>
        {!tv &&
          (knownReleaseDates(entry).length ? (
            knownReleaseDates(entry).map((release) => (
              <DescriptionItem key={release.kind} title={release.kind}>
                {release.date ? dateLabel(release.date) : "—"}
              </DescriptionItem>
            ))
          ) : (
            <DescriptionItem title="Release dates">—</DescriptionItem>
          ))}
        <DescriptionItem title="Status">
          <StatusLabels entry={entry} />
        </DescriptionItem>
        {entry.monitoring_reason && (
          <DescriptionItem title="Monitoring">
            {entry.monitoring_reason}
          </DescriptionItem>
        )}
        {entry.download_id && (
          <DescriptionItem title="Download">
            <Link to={`/activity?job=${encodeURIComponent(entry.download_id)}`}>
              {downloadText(entry)}
            </Link>
          </DescriptionItem>
        )}
        {(entry.runtime_minutes || entry.series_status) && (
          <DescriptionItem title="Details">
            {[
              entry.runtime_minutes ? `${entry.runtime_minutes} min` : "",
              entry.series_status ?? "",
            ]
              .filter(Boolean)
              .join(" · ")}
          </DescriptionItem>
        )}
        <DescriptionItem title="Metadata">
          {metadataNote(entry)}
        </DescriptionItem>
      </DescriptionList>
      <ItemSearchResultAlert result={search.result} onDismiss={search.reset} />
    </Modal>
  );
}
