import { Fragment } from "react";
import {
  CalendarDays,
  Download,
  FolderInput,
  ListTodo,
  Plus,
  RefreshCw,
  Tv,
} from "lucide-react";
import { Link } from "react-router-dom";
import {
  type Dashboard as DashboardData,
  type DownloadPage,
  type System,
} from "../api";
import { PageHeading, RequestState } from "../components";
import { EventRow, type EventPage } from "../components/EventRow";
import {
  Label,
  PageBody,
  PageToolbar,
  Panel,
  ProgressBar,
  Table,
  ToolbarButton,
  ToolbarSection,
  ToolbarSeparator,
  episodeCode,
  formatBytes,
  type Kind,
} from "../ui";
import { useResource } from "../useResource";
import type { PlanningData, PlanningEntry } from "./Planning";
import "./Dashboard.css";

const releaseLabels: Record<string, string> = {
  theatrical: "Theatrical",
  digital: "Digital",
  physical: "Physical",
};

function entryKindText(entry: PlanningEntry) {
  if (entry.kind === "series")
    return entry.season !== null && entry.episode !== null
      ? `${episodeCode(entry.season, entry.episode)} · ${entry.episode_title || "Untitled"}`
      : "TV episode";
  return ["Movie", entry.release_kind ? releaseLabels[entry.release_kind] : ""]
    .filter(Boolean)
    .join(" · ");
}

function PlanningTable({
  label,
  items,
}: {
  label: string;
  items: PlanningEntry[];
}) {
  return (
    <Table label={label}>
      <thead>
        <tr>
          <th scope="col">Title</th>
          <th scope="col">Episode</th>
          <th scope="col" className="is-numeric">
            Date
          </th>
        </tr>
      </thead>
      <tbody>
        {items.map((item) => (
          <tr
            key={`${item.media_id}-${item.episode_id}-${item.release_kind ?? ""}`}
          >
            <th scope="row">
              <Link
                to={`/${item.kind === "movie" ? "movies" : "series"}/${item.media_id}`}
              >
                {item.title}
              </Link>
            </th>
            <td className="dash-episode">{entryKindText(item)}</td>
            <td className="is-numeric">{item.date || "—"}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

const wantedCounts: { key: string; label: string; to: string; kind: Kind }[] = [
  { key: "missing", label: "Missing", to: "/wanted", kind: "danger" },
  {
    key: "downloading",
    label: "In progress",
    to: "/wanted?status=downloading",
    kind: "primary",
  },
  {
    key: "attention",
    label: "Needs attention",
    to: "/wanted?status=attention",
    kind: "warning",
  },
];

function dayShift(value: string, days: number) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return "";
  const date = new Date(`${value}T12:00:00Z`);
  if (!Number.isFinite(date.getTime())) return "";
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}

function progressKind(state: string): "primary" | "warning" | "danger" {
  if (state === "failed") return "danger";
  if (state === "blocked") return "warning";
  return "primary";
}

export function Dashboard() {
  const dashboard = useResource<DashboardData>("/dashboard");
  const system = useResource<System>("/system");
  const wanted = useResource<PlanningData>("/wanted");
  const upcoming = useResource<PlanningData>("/calendar?status=upcoming");
  const downloads = useResource<DownloadPage>("/downloads?page=1");
  const events = useResource<EventPage>("/events");
  const error = dashboard.error || system.error;
  const loading = dashboard.loading || system.loading;
  function reload() {
    dashboard.reload();
    system.reload();
    wanted.reload();
    upcoming.reload();
    downloads.reload();
    events.reload();
  }
  const counts = dashboard.data?.counts;
  const libraryLinks = counts
    ? [
        {
          to: "/movies",
          text: `${counts.movies} ${counts.movies === 1 ? "movie" : "movies"}`,
        },
        {
          to: "/series",
          text: `${counts.series} ${counts.series === 1 ? "TV show" : "TV shows"}`,
        },
        { to: "/anime", text: `${counts.anime ?? 0} anime` },
      ]
    : [];
  const active = downloads.data?.items ?? [];
  const activeTotal = downloads.data?.total ?? active.length;
  const missing = wanted.data?.counts?.missing;
  const asOf = upcoming.data?.as_of_date ?? "";
  const weekEnd = dayShift(asOf, 7);
  const upcomingItems = upcoming.data?.items ?? [];
  const weekItems =
    asOf && weekEnd
      ? upcomingItems.filter(
          (item) => item.date && item.date >= asOf && item.date <= weekEnd,
        )
      : [];
  const laterCount = upcomingItems.length - weekItems.length;
  const eventItems = events.data?.items ?? [];
  const titleEvents = eventItems.filter((event) => event.kind === "title");
  const additions = titleEvents.filter((event) => /added/i.test(event.state));
  const recentLibrary = (additions.length ? additions : titleEvents).slice(
    0,
    5,
  );
  const health =
    system.data?.database === "connected"
      ? `Connected · ${system.data.version}`
      : "Database unavailable";
  return (
    <>
      <PageToolbar label="Dashboard actions">
        <ToolbarSection>
          <ToolbarButton icon={Plus} label="Add Movie" to="/search" />
          <ToolbarButton icon={Tv} label="Add Show" to="/search?kind=series" />
          <ToolbarButton
            icon={FolderInput}
            label="Library Import"
            to="/import"
          />
          <ToolbarSeparator />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={loading}
            onClick={reload}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title={dashboard.data?.library_name ?? "Library"} />
        <RequestState loading={loading} error={error} retry={reload} />
        {!loading && !error && dashboard.data && system.data && (
          <>
            <section className="dash-status" aria-label="Status">
              <Link className="dash-status-item" to="/activity">
                <Download size={16} aria-hidden="true" />
                <span className="dash-status-value">
                  {downloads.loading
                    ? "…"
                    : downloads.error
                      ? "—"
                      : activeTotal}
                </span>
                <span className="dash-status-label">
                  active download{activeTotal === 1 ? "" : "s"}
                </span>
              </Link>
              <Link className="dash-status-item" to="/wanted">
                <ListTodo size={16} aria-hidden="true" />
                <span className="dash-status-value">
                  {wanted.error ? "—" : (missing ?? "—")}
                </span>
                <span className="dash-status-label">missing</span>
              </Link>
              <Link className="dash-status-item" to="/calendar">
                <CalendarDays size={16} aria-hidden="true" />
                <span className="dash-status-value">
                  {upcoming.error
                    ? "—"
                    : upcoming.loading
                      ? "…"
                      : weekItems.length}
                </span>
                <span className="dash-status-label">this week</span>
              </Link>
              <Link className="dash-status-item" to="/system">
                <span className="dash-status-value">{health}</span>
                <span className="dash-status-label">System</span>
              </Link>
            </section>

            <div className="dash-columns">
              <Panel
                title="Activity"
                actions={<Link to="/activity">Queue</Link>}
              >
                <RequestState
                  loading={downloads.loading && !downloads.data}
                  error={downloads.data ? "" : downloads.error}
                  retry={downloads.reload}
                />
                {downloads.data && (
                  <div className="dash-panel-body">
                    {active.length ? (
                      <>
                        <p className="rs-muted rs-small">
                          {activeTotal} active download
                          {activeTotal === 1 ? "" : "s"}; completed items are in{" "}
                          <Link to="/timeline">History</Link>.
                        </p>
                        <ul
                          className="dash-queue"
                          aria-label="Active downloads"
                        >
                          {active.slice(0, 5).map((job) => (
                            <li key={job.id}>
                              <div className="dash-queue-head">
                                <Link
                                  to={`/activity?job=${encodeURIComponent(job.id)}`}
                                >
                                  {job.movie_title || job.title}
                                </Link>
                                <span className="rs-muted rs-small">
                                  {job.state.replaceAll("_", " ")}
                                </span>
                              </div>
                              <ProgressBar
                                value={job.progress}
                                kind={progressKind(job.state)}
                                label={`${job.movie_title || job.title} progress`}
                                text={`${Math.round(job.progress * 100)}%`}
                              />
                              <span className="dash-queue-meta rs-muted rs-small">
                                {formatBytes(job.downloaded_bytes)} of{" "}
                                {formatBytes(job.total_bytes)}
                              </span>
                            </li>
                          ))}
                        </ul>
                      </>
                    ) : (
                      <>
                        <p className="rs-muted">No active downloads.</p>
                        {events.loading && !events.data ? (
                          <p className="rs-muted rs-small">
                            Loading recent history…
                          </p>
                        ) : recentLibrary.length || eventItems.length ? (
                          <div className="dash-events">
                            <h3 className="dash-subheading">Recent history</h3>
                            {eventItems.slice(0, 4).map((event) => (
                              <EventRow key={event.id} event={event} />
                            ))}
                            <Link className="rs-small" to="/timeline">
                              View history
                            </Link>
                          </div>
                        ) : (
                          <p className="rs-muted rs-small">
                            Completed downloads and imports appear in{" "}
                            <Link to="/timeline">History</Link>.
                          </p>
                        )}
                      </>
                    )}
                  </div>
                )}
              </Panel>

              <Panel
                title="Wanted"
                actions={<Link to="/wanted">View all</Link>}
              >
                <RequestState
                  loading={wanted.loading}
                  error={wanted.error}
                  retry={wanted.reload}
                />
                {wanted.data && !wanted.error && !wanted.loading && (
                  <div className="dash-panel-body">
                    <div className="dash-counts">
                      {wantedCounts.map((count) => (
                        <Link key={count.key} to={count.to}>
                          <Label kind={count.kind}>
                            {wanted.data!.counts?.[count.key] ?? 0}{" "}
                            {count.label}
                          </Label>
                        </Link>
                      ))}
                    </div>
                    {wanted.data.items?.length ? (
                      <PlanningTable
                        label="Wanted titles"
                        items={wanted.data.items.slice(0, 6)}
                      />
                    ) : (
                      <p className="rs-muted">
                        No monitored, dated items are missing.
                      </p>
                    )}
                  </div>
                )}
              </Panel>

              <Panel
                title="Upcoming"
                actions={<Link to="/calendar">Calendar</Link>}
              >
                <RequestState
                  loading={upcoming.loading}
                  error={upcoming.error}
                  retry={upcoming.reload}
                />
                {upcoming.data && !upcoming.error && !upcoming.loading && (
                  <div className="dash-panel-body">
                    {weekItems.length ? (
                      <PlanningTable
                        label="Upcoming this week"
                        items={weekItems.slice(0, 6)}
                      />
                    ) : (
                      <p className="rs-muted">
                        No saved releases in the next seven days.
                      </p>
                    )}
                    {laterCount > 0 && (
                      <p className="rs-muted rs-small">
                        {laterCount} more saved{" "}
                        {laterCount === 1 ? "date is" : "dates are"} later this
                        month.
                      </p>
                    )}
                    <p className="rs-muted rs-small">
                      Release dates follow the server timezone (TZ); TV dates
                      follow saved episodes. Movie entries show the release kind
                      when it is saved.
                    </p>
                  </div>
                )}
              </Panel>

              <Panel title="Library" actions={<Link to="/movies">Browse</Link>}>
                <div className="dash-panel-body">
                  <p className="dash-library-counts">
                    {libraryLinks.map((entry, index) => (
                      <Fragment key={entry.to}>
                        {index > 0 && <span aria-hidden="true"> · </span>}
                        <Link to={entry.to}>{entry.text}</Link>
                      </Fragment>
                    ))}
                  </p>
                  {events.loading && !events.data ? (
                    <p className="rs-muted rs-small">Loading recent titles…</p>
                  ) : recentLibrary.length ? (
                    <div className="dash-events">
                      <h3 className="dash-subheading">
                        {additions.length
                          ? "Recent additions"
                          : "Recent library activity"}
                      </h3>
                      {recentLibrary.map((event) => (
                        <EventRow key={event.id} event={event} />
                      ))}
                    </div>
                  ) : events.error ? (
                    <p className="rs-muted rs-small">
                      Recent additions are unavailable right now.
                    </p>
                  ) : (
                    <p className="rs-muted rs-small">
                      No recent additions are recorded yet. Added titles appear
                      here with their latest library events.
                    </p>
                  )}
                  <Link className="rs-small" to="/timeline">
                    View library history
                  </Link>
                </div>
              </Panel>
            </div>
          </>
        )}
      </PageBody>
    </>
  );
}
