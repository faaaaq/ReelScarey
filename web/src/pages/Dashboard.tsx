import { Fragment } from "react";
import { FolderInput, Plus, RefreshCw, Tv } from "lucide-react";
import { Link } from "react-router-dom";
import { type Dashboard as DashboardData, type System } from "../api";
import { PageHeading, RequestState } from "../components";
import {
  DescriptionItem,
  DescriptionList,
  Label,
  PageBody,
  PageToolbar,
  Panel,
  Table,
  ToolbarButton,
  ToolbarSection,
  ToolbarSeparator,
  episodeCode,
  type Kind,
} from "../ui";
import { useResource } from "../useResource";
import type { PlanningData, PlanningEntry } from "./Planning";
import "./Dashboard.css";

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
          <tr key={`${item.media_id}-${item.episode_id}`}>
            <th scope="row">
              <Link
                to={`/${item.kind === "movie" ? "movies" : "series"}/${item.media_id}`}
              >
                {item.title}
              </Link>
            </th>
            <td className="dash-episode">
              {item.kind === "series" &&
              item.season !== null &&
              item.episode !== null
                ? `${episodeCode(item.season, item.episode)} · ${item.episode_title || "Untitled"}`
                : item.year || "Movie"}
            </td>
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

const pathNames: Record<string, string> = {
  config: "Configuration",
  media: "Media library",
  downloads: "Downloads",
};

export function Dashboard() {
  const dashboard = useResource<DashboardData>("/dashboard");
  const system = useResource<System>("/system");
  const wanted = useResource<PlanningData>("/wanted");
  const upcoming = useResource<PlanningData>("/calendar?status=upcoming");
  const error = dashboard.error || system.error;
  const loading = dashboard.loading || system.loading;
  function reload() {
    dashboard.reload();
    system.reload();
    wanted.reload();
    upcoming.reload();
  }
  const counts = dashboard.data?.counts;
  const summary = counts
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
            <p className="dash-summary">
              {summary.map((entry, index) => (
                <Fragment key={entry.to}>
                  {index > 0 && <span aria-hidden="true"> · </span>}
                  <Link to={entry.to}>{entry.text}</Link>
                </Fragment>
              ))}
            </p>

            <div className="dash-columns">
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
                    {upcoming.data.items?.length ? (
                      <PlanningTable
                        label="Upcoming this month"
                        items={upcoming.data.items.slice(0, 6)}
                      />
                    ) : (
                      <p className="rs-muted">
                        No upcoming dates in the saved catalog this month.
                      </p>
                    )}
                    <p className="rs-muted rs-small">
                      Saved release dates in UTC; load season episodes to
                      include them. Movie dates may be theatrical.
                    </p>
                  </div>
                )}
              </Panel>
            </div>

            <Panel
              title="System"
              actions={<Link to="/system">System status</Link>}
            >
              <DescriptionList>
                <DescriptionItem title="Status">
                  Connected · {system.data.version}
                </DescriptionItem>
                {Object.entries(system.data.paths ?? {}).map(([key, value]) => (
                  <DescriptionItem key={key} title={pathNames[key] ?? key}>
                    <span className="rs-mono">{value}</span>
                  </DescriptionItem>
                ))}
              </DescriptionList>
              <p className="dash-system-links rs-small">
                <Link to="/settings">Folder settings</Link>
              </p>
            </Panel>
          </>
        )}
      </PageBody>
    </>
  );
}
