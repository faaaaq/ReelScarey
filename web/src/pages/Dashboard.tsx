import { Button } from "@radix-ui/themes";
import { ArrowUpRight, Clapperboard, Film, Plus, Tv } from "lucide-react";
import { Link } from "react-router-dom";
import { type Dashboard as DashboardData, type System } from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import type { PlanningData, PlanningEntry } from "./Planning";

function CatalogLine({ item }: { item: PlanningEntry }) {
  return (
    <li className="ui-list-row">
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
  const error = dashboard.error || system.error;
  const loading = dashboard.loading || system.loading;
  return (
    <>
      <PageHeading
        eyebrow="Overview"
        title={dashboard.data?.library_name ?? "Library"}
      >
        <Button asChild size="2">
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
        <div className="ui-stack">
          <section className="ui-stat-grid" aria-label="Catalog totals">
            <Link className="ui-stat" to="/movies">
              <span className="ui-stat-top">
                <Film size={15} aria-hidden="true" /> Movies
              </span>
              <strong>{dashboard.data.counts.movies}</strong>
              <span>In catalog</span>
            </Link>
            <Link className="ui-stat" to="/series">
              <span className="ui-stat-top">
                <Tv size={15} aria-hidden="true" /> TV shows
              </span>
              <strong>{dashboard.data.counts.series}</strong>
              <span>In catalog</span>
            </Link>
            <Link className="ui-stat" to="/anime">
              <span className="ui-stat-top">
                <Clapperboard size={15} aria-hidden="true" /> Anime
              </span>
              <strong>{dashboard.data.counts.anime ?? 0}</strong>
              <span>In catalog</span>
            </Link>
          </section>

          <div className="ui-grid-2">
            <section className="panel">
              <div className="ui-section-head">
                <div>
                  <h2>Wanted</h2>
                </div>
                <Link className="text-link" to="/wanted">
                  View all <ArrowUpRight size={13} />
                </Link>
              </div>
              <RequestState
                loading={wanted.loading}
                error={wanted.error}
                retry={wanted.reload}
              />
              {wanted.data && !wanted.error && !wanted.loading && (
                <>
                  <div className="ui-count-row">
                    <Link className="ui-count" to="/wanted">
                      <strong>{wanted.data.counts?.missing ?? 0}</strong>
                      <span>Missing</span>
                    </Link>
                    <Link className="ui-count" to="/wanted?status=downloading">
                      <strong>{wanted.data.counts?.downloading ?? 0}</strong>
                      <span>In progress</span>
                    </Link>
                    <Link className="ui-count" to="/wanted?status=attention">
                      <strong>{wanted.data.counts?.attention ?? 0}</strong>
                      <span>Needs attention</span>
                    </Link>
                  </div>
                  {wanted.data.items?.length ? (
                    <ul className="ui-list">
                      {wanted.data.items.slice(0, 4).map((item) => (
                        <CatalogLine
                          key={`${item.media_id}-${item.episode_id}`}
                          item={item}
                        />
                      ))}
                    </ul>
                  ) : (
                    <p className="ui-empty-note">
                      No monitored, dated items are missing.
                    </p>
                  )}
                </>
              )}
            </section>

            <section className="panel">
              <div className="ui-section-head">
                <div>
                  <h2>Later this month</h2>
                </div>
                <Link className="text-link" to="/calendar">
                  Calendar <ArrowUpRight size={13} />
                </Link>
              </div>
              <RequestState
                loading={upcoming.loading}
                error={upcoming.error}
                retry={upcoming.reload}
              />
              {upcoming.data && !upcoming.error && !upcoming.loading && (
                <>
                  {upcoming.data.items?.length ? (
                    <ul className="ui-list">
                      {upcoming.data.items.slice(0, 4).map((item) => (
                        <CatalogLine
                          key={`${item.media_id}-${item.episode_id}`}
                          item={item}
                        />
                      ))}
                    </ul>
                  ) : (
                    <p className="ui-empty-note">
                      No upcoming dates in the saved catalog for this month.
                    </p>
                  )}
                  <p className="ui-note">
                    Saved release dates, in UTC. Load season episodes to include
                    them. Movie dates may be theatrical releases.
                  </p>
                </>
              )}
            </section>
          </div>

          <section className="panel">
            <div className="ui-section-head">
              <div>
                <h2>Storage</h2>
              </div>
              <Link className="text-link" to="/settings">
                Folder settings <ArrowUpRight size={13} />
              </Link>
            </div>
            <dl className="ui-path-list">
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
            <div className="ui-footer-line">
              <span>Connected · {system.data.version}</span>
              <Link className="text-link" to="/system">
                Run system checks
              </Link>
            </div>
          </section>
        </div>
      )}
    </>
  );
}
