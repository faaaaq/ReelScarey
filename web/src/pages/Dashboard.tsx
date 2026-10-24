import { Badge, Button } from "@radix-ui/themes";
import {
  ArrowRight,
  Check,
  Clapperboard,
  Database,
  Film,
  FolderOpen,
  MonitorPlay,
  Search,
} from "lucide-react";
import { Link } from "react-router-dom";
import { type Dashboard as DashboardData, type System } from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";

export function Dashboard() {
  const dashboard = useResource<DashboardData>("/dashboard");
  const system = useResource<System>("/system");
  const error = dashboard.error || system.error;
  const loading = dashboard.loading || system.loading;
  return (
    <>
      <PageHeading
        eyebrow="THE BIG PICTURE"
        title={dashboard.data?.library_name ?? "Your library"}
        description="Media management, without the horror."
      >
        <Badge size="2" color={system.data && !error ? "green" : "gray"}>
          <span className="status-dot" />
          {system.data && !error ? "System online" : "Connecting"}
        </Badge>
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
          <section className="welcome-card">
            <div className="welcome-copy">
              <p className="eyebrow">WELCOME TO REELSCAREY</p>
              <h2>
                Set the scene.
                <br />
                <span>Make room for movie night.</span>
              </h2>
              <p>
                Find your next feature on TMDb, explore its details, and keep
                your growing movie and TV collection together.
              </p>
              <Button size="3" asChild>
                <Link to="/search">
                  <Search size={17} /> Find a movie <ArrowRight size={17} />
                </Link>
              </Button>
              <p className="foundation-note">
                Search releases, download, and bring movies home.
              </p>
              <p className="foundation-note">
                <Link className="text-link" to="/wanted">
                  Review wanted items
                </Link>{" "}
                ·{" "}
                <Link className="text-link" to="/calendar">
                  Open release calendar
                </Link>
              </p>
            </div>
            <div className="cinema-art" aria-hidden="true">
              <div className="film-frame frame-back">
                <Film />
              </div>
              <div className="film-frame frame-front">
                <Clapperboard strokeWidth={1} />
                <span>
                  THE NEXT FEATURE
                  <br />
                  <b>IS YOURS.</b>
                </span>
                <div className="frame-rule" />
                <small>REELSCAREY / EST. 2026</small>
              </div>
            </div>
          </section>
          <div className="stats-grid grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Link className="stat-card panel" to="/movies">
              <div className="stat-label">
                <Film size={18} />
                Movies
                <ArrowRight size={16} />
              </div>
              <strong>{dashboard.data.counts.movies}</strong>
              <span>In your library</span>
            </Link>
            <Link className="stat-card panel" to="/series">
              <div className="stat-label">
                <MonitorPlay size={18} />
                TV shows
                <ArrowRight size={16} />
              </div>
              <strong>{dashboard.data.counts.series}</strong>
              <span>In your library</span>
            </Link>
            <div className="stat-card panel">
              <div className="stat-label">
                <Database size={18} />
                Database
              </div>
              <strong className="database-value">Connected</strong>
              <span>SQLite · Schema {system.data.schema_version}</span>
            </div>
          </div>
          <div className="dashboard-bottom grid gap-5 lg:grid-cols-2">
            <section className="panel checklist">
              <div className="section-title">
                <h2>Your opening checklist</h2>
                <Badge color="gray">Movies & TV</Badge>
              </div>
              <div className="checklist-row">
                <span className="check-circle">
                  <Check size={15} />
                </span>
                <div>
                  <h3>Your library is protected</h3>
                  <p>
                    You’re signed in. Your server and database are connected.
                  </p>
                </div>
              </div>
              <div className="checklist-row">
                <span
                  className={
                    dashboard.data.settings_revision > 1
                      ? "check-circle"
                      : "step-circle"
                  }
                >
                  {dashboard.data.settings_revision > 1 ? (
                    <Check size={15} />
                  ) : (
                    "2"
                  )}
                </span>
                <div>
                  <h3>
                    {dashboard.data.settings_revision > 1
                      ? "Library preferences saved"
                      : "Make the library yours"}
                  </h3>
                  <p>
                    {dashboard.data.settings_revision > 1
                      ? "Your preferences are stored in the database."
                      : "Name your library and review folder preferences."}
                  </p>
                </div>
                <Link to="/settings" className="text-link">
                  Settings <ArrowRight size={14} />
                </Link>
              </div>
              <div className="checklist-row upcoming-row">
                <span className="step-circle">3</span>
                <div>
                  <h3>Add your first movies</h3>
                  <p>Connect TMDb in Settings, then search for a title.</p>
                </div>
                <Link to="/search" className="text-link">
                  Search <ArrowRight size={14} />
                </Link>
              </div>
            </section>
            <section className="panel storage-panel">
              <div className="section-title">
                <h2>Where everything lives</h2>
                <FolderOpen size={18} />
              </div>
              <p>
                Completed movies are imported under the media root. Mount the
                same download folder used by qBittorrent to make files
                available.
              </p>
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
              <Link className="text-link" to="/settings">
                Review settings <ArrowRight size={14} />
              </Link>
            </section>
          </div>
        </>
      )}
    </>
  );
}
