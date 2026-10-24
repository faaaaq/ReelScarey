import { Button } from "@radix-ui/themes";
import { ArrowUpRight, Film, Tv, Plus } from "lucide-react";
import { Link } from "react-router-dom";
import { type Dashboard as DashboardData, type System } from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import type { PlanningData, PlanningEntry } from "./Planning";

function CatalogLine({ item }: { item: PlanningEntry }) {
  return (
    <li>
      <Link
        to={`/${item.kind === "movie" ? "movies" : "series"}/${item.media_id}`}
      >
        <strong>{item.title}</strong>
        <span>
          {item.kind === "series"
            ? `S${String(item.season).padStart(2, "0")}E${String(item.episode).padStart(2, "0")} · `
            : ""}
          {item.episode_title || item.year || "Year unknown"}
        </span>
      </Link>
      <time>{item.date}</time>
    </li>
  );
}
export function Dashboard() {
  const dashboard = useResource<DashboardData>("/dashboard");
  const system = useResource<System>("/system");
  const wanted = useResource<PlanningData>("/wanted");
  const upcoming = useResource<PlanningData>("/calendar?status=upcoming");
  const error = dashboard.error || system.error,
    loading = dashboard.loading || system.loading;
  return (
    <>
      <PageHeading
        title={dashboard.data?.library_name ?? "Library"}
        description="Library overview"
      >
        <Button asChild>
          <Link to="/search">
            <Plus size={16} />
            Add a title
          </Link>
        </Button>
      </PageHeading>
      <RequestState
        loading={loading}
        error={error}
        retry={() => {
          dashboard.reload();
          system.reload();
        }}
      />
      {!loading && !error && dashboard.data && system.data && (
        <>
          <section className="library-register" aria-label="Catalog totals">
            <Link to="/movies">
              <Film size={23} />
              <div>
                <span>Movies</span>
                <strong>{dashboard.data.counts.movies}</strong>
              </div>
              <ArrowUpRight size={20} />
            </Link>
            <Link to="/series">
              <Tv size={23} />
              <div>
                <span>TV shows</span>
                <strong>{dashboard.data.counts.series}</strong>
              </div>
              <ArrowUpRight size={20} />
            </Link>
          </section>
          <div className="overview-columns">
            <section className="overview-section">
              <header>
                <h2>Wanted</h2>
                <Link className="text-link" to="/wanted">
                  View all <ArrowUpRight size={14} />
                </Link>
              </header>
              <RequestState
                loading={wanted.loading}
                error={wanted.error}
                retry={wanted.reload}
              />
              {wanted.data && !wanted.error && !wanted.loading && (
                <>
                  <div className="work-counts">
                    <Link to="/wanted">
                      <strong>{wanted.data.counts?.missing ?? 0}</strong>
                      <span>Missing</span>
                    </Link>
                    <Link to="/wanted?status=downloading">
                      <strong>{wanted.data.counts?.downloading ?? 0}</strong>
                      <span>In progress</span>
                    </Link>
                    <Link to="/wanted?status=attention">
                      <strong>{wanted.data.counts?.attention ?? 0}</strong>
                      <span>Needs attention</span>
                    </Link>
                  </div>
                  {wanted.data.items?.length ? (
                    <ul className="catalog-lines">
                      {wanted.data.items.slice(0, 4).map((item) => (
                        <CatalogLine
                          key={`${item.media_id}-${item.episode_id}`}
                          item={item}
                        />
                      ))}
                    </ul>
                  ) : (
                    <p className="overview-empty">
                      No monitored, dated items are missing.
                    </p>
                  )}
                </>
              )}
            </section>
            <section className="overview-section">
              <header>
                <h2>Later this month</h2>
                <Link className="text-link" to="/calendar">
                  Calendar <ArrowUpRight size={14} />
                </Link>
              </header>
              <RequestState
                loading={upcoming.loading}
                error={upcoming.error}
                retry={upcoming.reload}
              />
              {upcoming.data && !upcoming.error && !upcoming.loading && (
                <>
                  {upcoming.data.items?.length ? (
                    <ul className="catalog-lines">
                      {upcoming.data.items.slice(0, 4).map((item) => (
                        <CatalogLine
                          key={`${item.media_id}-${item.episode_id}`}
                          item={item}
                        />
                      ))}
                    </ul>
                  ) : (
                    <p className="overview-empty">
                      No upcoming dates in the saved catalog for this month.
                    </p>
                  )}
                  <p className="catalog-note">
                    Saved release dates, in UTC. Load season episodes to include
                    them. Movie dates may be theatrical releases.
                  </p>
                </>
              )}
            </section>
          </div>
          <section className="storage-register">
            <header>
              <h2>Storage</h2>
              <Link className="text-link" to="/settings">
                Folder settings <ArrowUpRight size={14} />
              </Link>
            </header>
            <dl>
              {Object.entries(system.data.paths).map(([key, value]) => (
                <div key={key}>
                  <dt>
                    {key === "config"
                      ? "Configuration"
                      : key === "media"
                        ? "Media library"
                        : "Downloads"}
                  </dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
            <div className="overview-server">
              <span>Connected · {system.data.version}</span>
              <Link className="text-link" to="/system">
                Run system checks
              </Link>
            </div>
          </section>
        </>
      )}
    </>
  );
}
