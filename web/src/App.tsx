import { lazy, Suspense, useEffect, useState } from "react";
import {
  Activity,
  CheckCircle2,
  Captions,
  Clapperboard,
  Film as BrandIcon,
  Clock3,
  CalendarDays,
  ListFilter,
  Film,
  FolderSearch,
  LayoutDashboard,
  LogOut,
  Menu,
  MonitorPlay,
  Search,
  Settings2,
  ShieldCheck,
  Radio,
  X,
} from "lucide-react";
import {
  Link,
  NavLink,
  Navigate,
  Route,
  Routes,
  useLocation,
} from "react-router-dom";
import { PageHeading } from "./components";
import { PageLoadBoundary } from "./components/PageLoadBoundary";
import { HistoryTray } from "./components/HistoryTray";
import { AuthGate, useAuth } from "./auth";
const Dashboard = lazy(() =>
  import("./pages/Dashboard").then((m) => ({ default: m.Dashboard })),
);
const Library = lazy(() =>
  import("./pages/Library").then((m) => ({ default: m.Library })),
);
const Settings = lazy(() =>
  import("./pages/Settings").then((m) => ({ default: m.Settings })),
);
const MovieSearch = lazy(() =>
  import("./pages/Search").then((m) => ({ default: m.Search })),
);
const DownloadActivity = lazy(() =>
  import("./pages/Activity").then((m) => ({ default: m.Activity })),
);
const Releases = lazy(() =>
  import("./pages/Releases").then((m) => ({ default: m.Releases })),
);
const Series = lazy(() =>
  import("./pages/Series").then((m) => ({ default: m.Series })),
);
const Automation = lazy(() =>
  import("./pages/Automation").then((m) => ({ default: m.Automation })),
);
const Subtitles = lazy(() =>
  import("./pages/Subtitles").then((m) => ({ default: m.Subtitles })),
);
const Setup = lazy(() =>
  import("./pages/System").then((m) => ({ default: m.Setup })),
);
const SystemChecks = lazy(() =>
  import("./pages/System").then((m) => ({ default: m.SystemChecks })),
);
const Indexers = lazy(() =>
  import("./pages/Indexers").then((m) => ({ default: m.Indexers })),
);
const ItemDetails = lazy(() =>
  import("./pages/ItemDetails").then((m) => ({ default: m.ItemDetails })),
);
const ExistingLibrary = lazy(() =>
  import("./pages/ExistingLibrary").then((m) => ({
    default: m.ExistingLibrary,
  })),
);
const Planning = lazy(() =>
  import("./pages/Planning").then((m) => ({ default: m.Planning })),
);
const Organization = lazy(() =>
  import("./pages/Organization").then((m) => ({ default: m.Organization })),
);
const YouTube = lazy(() =>
  import("./pages/YouTube").then((m) => ({ default: m.YouTube })),
);
const AdminRequests = lazy(() =>
  import("./pages/Requests").then((m) => ({ default: m.AdminRequests })),
);
const RequesterHome = lazy(() =>
  import("./pages/Requests").then((m) => ({ default: m.RequesterHome })),
);
const WatchActivity = lazy(() =>
  import("./pages/WatchActivity").then((m) => ({ default: m.WatchActivity })),
);
const Timeline = lazy(() =>
  import("./pages/Timeline").then((m) => ({ default: m.Timeline })),
);

const navigation = [
  { path: "/", name: "Dashboard", icon: LayoutDashboard },
  { path: "/movies", name: "Movies", icon: Film },
  { path: "/series", name: "TV shows", icon: MonitorPlay },
  { path: "/anime", name: "Anime", icon: Clapperboard },
  { path: "/youtube", name: "YouTube", icon: MonitorPlay },
  { path: "/wanted", name: "Wanted", icon: ListFilter },
  { path: "/calendar", name: "Calendar", icon: CalendarDays },
  { path: "/search", name: "Search", icon: Search },
  { path: "/activity", name: "Activity", icon: Activity },
  { path: "/watch-activity", name: "Emby activity", icon: MonitorPlay },
  { path: "/automation", name: "Automation", icon: Clock3 },
  { path: "/subtitles", name: "Subtitles", icon: Captions },
];

export default function App() {
  return (
    <AuthGate>
      <PageLoadBoundary>
        <Suspense
          fallback={
            <div role="status" className="panel">
              Loading…
            </div>
          }
        >
          <RoleApp />
        </Suspense>
      </PageLoadBoundary>
    </AuthGate>
  );
}

function RoleApp() {
  const { session } = useAuth();
  return session.role === "requester" ? <RequesterHome /> : <AppShell />;
}

function AppShell() {
  const { session, signOut } = useAuth();
  const [signOutError, setSignOutError] = useState("");
  const [signingOut, setSigningOut] = useState(false);
  async function logout() {
    setSigningOut(true);
    setSignOutError("");
    try {
      await signOut();
    } catch {
      setSignOutError("Could not sign out. Try again.");
    } finally {
      setSigningOut(false);
    }
  }
  const [menuOpen, setMenuOpen] = useState(false);
  const location = useLocation();
  useEffect(() => {
    setMenuOpen(false);
    document.title = `${location.pathname === "/organization" ? "Organize library" : location.pathname === "/setup" ? "Setup" : location.pathname === "/system" ? "System checks" : location.pathname.startsWith("/series/") ? "TV show" : location.pathname.startsWith("/movies/") ? "Movie details" : location.pathname === "/import" ? "Import library" : location.pathname === "/indexers" ? "Indexers" : location.pathname === "/settings" ? "Settings" : location.pathname === "/releases" ? "Releases" : (navigation.find((item) => item.path === location.pathname)?.name ?? "Page")} · ReelScarey`;
  }, [location.pathname]);
  useEffect(() => {
    function close(event: KeyboardEvent) {
      if (event.key === "Escape") setMenuOpen(false);
    }
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, []);
  return (
    <div className="app-shell">
      <a href="#main-content" className="skip-link">
        Skip to content
      </a>
      <header className="mobile-bar">
        <Link to="/" className="wordmark">
          <BrandIcon size={21} />
          ReelScarey
        </Link>
        <button
          aria-label={menuOpen ? "Close navigation" : "Open navigation"}
          aria-expanded={menuOpen}
          aria-controls="main-navigation"
          onClick={() => setMenuOpen(!menuOpen)}
        >
          {menuOpen ? <X /> : <Menu />}
        </button>
      </header>
      <aside
        className={`sidebar ${menuOpen ? "is-open" : ""}`}
        id="main-navigation"
      >
        <Link to="/" className="brand">
          <span className="brand-mark">
            <BrandIcon size={21} />
          </span>
          <div className="wordmark">ReelScarey</div>
        </Link>
        <p className="nav-label">Library</p>
        <nav aria-label="Main navigation">
          {navigation.map(({ path, name, icon: Icon }) => (
            <NavLink key={path} to={path} end={path === "/"}>
              <Icon size={19} strokeWidth={1.7} />
              <span>{name}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <p className="nav-label">Manage</p>
          <nav aria-label="Administration">
            <NavLink to="/requests">
              <ListFilter size={19} strokeWidth={1.7} />
              <span>Requests</span>
            </NavLink>
            <NavLink to="/import">
              <FolderSearch size={19} strokeWidth={1.7} />
              <span>Find unmatched files</span>
            </NavLink>
            <NavLink to="/organization">
              <FolderSearch size={19} strokeWidth={1.7} />
              <span>Organize library</span>
            </NavLink>
            <NavLink to="/indexers">
              <Radio size={19} strokeWidth={1.7} />
              <span>Indexers</span>
            </NavLink>
            <NavLink to="/setup">
              <CheckCircle2 size={19} strokeWidth={1.7} />
              <span>Setup</span>
            </NavLink>
            <NavLink to="/system">
              <ShieldCheck size={19} strokeWidth={1.7} />
              <span>System checks</span>
            </NavLink>
            <NavLink to="/settings">
              <Settings2 size={19} strokeWidth={1.7} />
              <span>Settings</span>
            </NavLink>
          </nav>
          <div className="account-control">
            <span>
              {session.username} <small>v0.20</small>
            </span>
            <button onClick={() => void logout()} disabled={signingOut}>
              <LogOut size={15} />
              {signingOut ? "Signing out…" : "Sign out"}
            </button>
            {signOutError && <p role="alert">{signOutError}</p>}
          </div>
        </div>
      </aside>
      <main id="main-content" tabIndex={-1}>
        <div className="page-content">
          <div className="history-tray-trigger">
            <HistoryTray />
          </div>
          <Routes>
            <Route path="/login" element={<Navigate to="/" replace />} />
            <Route path="/" element={<Dashboard />} />
            <Route path="/youtube" element={<YouTube />} />
            <Route
              path="/movies"
              element={<Library key="movie" kind="movie" />}
            />
            <Route
              path="/series"
              element={<Library key="series" kind="series" />}
            />
            <Route
              path="/anime"
              element={<Library key="anime" kind="anime" />}
            />
            <Route path="/settings" element={<Settings />} />
            <Route path="/requests" element={<AdminRequests />} />
            <Route path="/organization" element={<Organization />} />
            <Route path="/indexers" element={<Indexers />} />
            <Route path="/import" element={<ExistingLibrary />} />
            <Route path="/wanted" element={<Planning key="wanted" />} />
            <Route
              path="/calendar"
              element={<Planning key="calendar" calendar />}
            />
            <Route path="/movies/:id" element={<ItemDetails />} />
            <Route path="/setup" element={<Setup />} />
            <Route path="/system" element={<SystemChecks />} />
            <Route path="/series/:id" element={<Series />} />
            <Route path="/search" element={<MovieSearch />} />
            <Route path="/releases" element={<Releases />} />
            <Route path="/activity" element={<DownloadActivity />} />
            <Route path="/watch-activity" element={<WatchActivity />} />
            <Route path="/timeline" element={<Timeline />} />
            <Route path="/automation" element={<Automation />} />
            <Route path="/subtitles" element={<Subtitles />} />
            <Route
              path="*"
              element={
                <>
                  <PageHeading
                    title="Page not found"
                    description="Check the address or return to your library."
                  />
                  <Link to="/" className="text-link">
                    Back to dashboard
                  </Link>
                </>
              }
            />
          </Routes>
        </div>
      </main>
    </div>
  );
}
