import { lazy, Suspense, useEffect, useRef, useState } from "react";
import {
  Activity,
  ArrowDownToLine,
  Captions,
  Clapperboard,
  Film as BrandIcon,
  Clock3,
  CalendarDays,
  ListFilter,
  Film,
  LayoutDashboard,
  LogOut,
  Menu,
  MonitorPlay,
  PanelLeftClose,
  PanelLeftOpen,
  Radio,
  Search,
  Settings2,
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
import { CommandPalette } from "./components/CommandPalette";
import { PageLoadBoundary } from "./components/PageLoadBoundary";
import { HistoryTray } from "./components/HistoryTray";
import { ThemeToggle } from "./components/ThemeToggle";
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
const Torrents = lazy(() =>
  import("./pages/Torrents").then((m) => ({ default: m.Torrents })),
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
const Backups = lazy(() =>
  import("./pages/Backups").then((m) => ({ default: m.Backups })),
);
const Storage = lazy(() =>
  import("./pages/Storage").then((m) => ({ default: m.Storage })),
);
const DownloadClients = lazy(() =>
  import("./pages/DownloadClients").then((m) => ({
    default: m.DownloadClients,
  })),
);
const MetadataRefresh = lazy(() =>
  import("./pages/MetadataRefresh").then((m) => ({
    default: m.MetadataRefresh,
  })),
);
const YouTubeSubscriptions = lazy(() =>
  import("./pages/YouTubeSubscriptions").then((m) => ({
    default: m.YouTubeSubscriptions,
  })),
);
const Indexers = lazy(() =>
  import("./pages/Indexers").then((m) => ({ default: m.Indexers })),
);
const Announces = lazy(() =>
  import("./pages/Announces").then((m) => ({ default: m.Announces })),
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

const libraryNavigation = [
  { path: "/", name: "Dashboard", icon: LayoutDashboard },
  { path: "/movies", name: "Movies", icon: Film },
  { path: "/series", name: "TV shows", icon: MonitorPlay },
  { path: "/anime", name: "Anime", icon: Clapperboard },
  { path: "/youtube", name: "YouTube", icon: MonitorPlay },
];

const browseNavigation = [
  { path: "/search", name: "Search", icon: Search },
  { path: "/wanted", name: "Wanted", icon: ListFilter },
  { path: "/calendar", name: "Calendar", icon: CalendarDays },
  { path: "/activity", name: "Activity", icon: Activity },
];

const toolsNavigation = [
  { path: "/torrents", name: "Torrents", icon: ArrowDownToLine },
  { path: "/automation", name: "Automation", icon: Clock3 },
  { path: "/subtitles", name: "Subtitles", icon: Captions },
  { path: "/announces", name: "Announces", icon: Radio },
];

const allNavigation = [
  ...libraryNavigation,
  ...browseNavigation,
  ...toolsNavigation,
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
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return window.localStorage.getItem("reelscarey-sidebar") === "collapsed";
    } catch {
      return false;
    }
  });
  useEffect(() => {
    try {
      window.localStorage.setItem(
        "reelscarey-sidebar",
        collapsed ? "collapsed" : "expanded",
      );
    } catch {
      /* Keep the current sidebar state for this session. */
    }
  }, [collapsed]);
  const location = useLocation();
  const previousPath = useRef(location.pathname);
  const menuButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    setMenuOpen(false);
    const titles: Record<string, string> = {
      "/backups": "Backups",
      "/organization": "Organize library",
      "/setup": "Setup",
      "/system": "System checks",
      "/import": "Import library",
      "/indexers": "Indexers",
      "/settings": "Settings",
      "/releases": "Releases",
      "/requests": "Requests",
      "/metadata-refresh": "Metadata refresh",
      "/storage": "Storage",
      "/download-clients": "Download clients",
      "/announces": "Announce feeds",
      "/timeline": "Event timeline",
      "/youtube/subscriptions": "YouTube subscriptions",
    };
    const title =
      titles[location.pathname] ??
      allNavigation.find((item) => item.path === location.pathname)?.name ??
      (/^\/series\/[^/]+$/.test(location.pathname)
        ? "TV show"
        : /^\/movies\/[^/]+$/.test(location.pathname)
          ? "Movie details"
          : "Page not found");
    document.title = `${title} · ReelScarey`;
    if (previousPath.current !== location.pathname) {
      document.getElementById("main-content")?.focus({ preventScroll: true });
      previousPath.current = location.pathname;
    }
  }, [location.pathname]);
  useEffect(() => {
    function close(event: KeyboardEvent) {
      if (event.key === "Escape" && menuOpen) {
        if (
          document
            .getElementById("main-navigation")
            ?.contains(document.activeElement)
        ) {
          menuButton.current?.focus();
        }
        setMenuOpen(false);
      }
    }
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [menuOpen]);
  return (
    <div className={`app-shell ${collapsed ? "is-collapsed" : ""}`}>
      <a href="#main-content" className="skip-link">
        Skip to content
      </a>
      <header className="mobile-bar">
        <Link to="/" className="wordmark">
          <BrandIcon size={21} />
          ReelScarey
        </Link>
        <div className="mobile-actions">
          <ThemeToggle />
          <button
            ref={menuButton}
            aria-label={menuOpen ? "Close navigation" : "Open navigation"}
            aria-expanded={menuOpen}
            aria-controls="main-navigation"
            onClick={() => setMenuOpen(!menuOpen)}
          >
            {menuOpen ? <X /> : <Menu />}
          </button>
        </div>
      </header>
      <aside
        className={`sidebar ${menuOpen ? "is-open" : ""}`}
        id="main-navigation"
      >
        <div className="sidebar-brand">
          <Link to="/" className="brand" aria-label="ReelScarey dashboard">
            <span className="brand-mark">
              <BrandIcon size={21} />
            </span>
            <div className="wordmark">ReelScarey</div>
          </Link>
          <button
            type="button"
            className="sidebar-toggle"
            aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            onClick={() => setCollapsed(!collapsed)}
          >
            {collapsed ? (
              <PanelLeftOpen size={16} />
            ) : (
              <PanelLeftClose size={16} />
            )}
          </button>
        </div>
        <p className="nav-label">Library</p>
        <nav aria-label="Main navigation">
          {libraryNavigation.map(({ path, name, icon: Icon }) => (
            <NavLink
              key={path}
              to={path}
              end={path === "/"}
              aria-label={name}
              title={name}
            >
              <Icon size={18} strokeWidth={1.7} />
              <span>{name}</span>
            </NavLink>
          ))}
          <p className="nav-label">Browse</p>
          {browseNavigation.map(({ path, name, icon: Icon }) => (
            <NavLink key={path} to={path} aria-label={name} title={name}>
              <Icon size={18} strokeWidth={1.7} />
              <span>{name}</span>
            </NavLink>
          ))}
          <p className="nav-label">Tools</p>
          {toolsNavigation.map(({ path, name, icon: Icon }) => (
            <NavLink key={path} to={path} aria-label={name} title={name}>
              <Icon size={18} strokeWidth={1.7} />
              <span>{name}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <nav aria-label="Administration">
            <NavLink to="/settings" aria-label="Settings" title="Settings">
              <Settings2 size={18} strokeWidth={1.7} />
              <span>Settings</span>
            </NavLink>
          </nav>
          <div className="account-control">
            <span>{session.username}</span>
            <button
              aria-label={signingOut ? "Signing out" : "Sign out"}
              title={signingOut ? "Signing out" : "Sign out"}
              onClick={() => void logout()}
              disabled={signingOut}
            >
              <LogOut size={15} />
              <span>{signingOut ? "Signing out…" : "Sign out"}</span>
            </button>
            {signOutError && <p role="alert">{signOutError}</p>}
          </div>
        </div>
      </aside>
      <main id="main-content" tabIndex={-1}>
        <div className="topbar">
          <CommandPalette />
          <div className="topbar-spacer" />
          <div className="topbar-actions">
            <ThemeToggle />
            <HistoryTray />
          </div>
        </div>
        <div className="page-content">
          <Routes>
            <Route path="/login" element={<Navigate to="/" replace />} />
            <Route path="/" element={<Dashboard />} />
            <Route path="/youtube" element={<YouTube />} />
            <Route
              path="/youtube/subscriptions"
              element={<YouTubeSubscriptions />}
            />
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
            <Route path="/announces" element={<Announces />} />
            <Route path="/import" element={<ExistingLibrary />} />
            <Route path="/wanted" element={<Planning key="wanted" />} />
            <Route
              path="/calendar"
              element={<Planning key="calendar" calendar />}
            />
            <Route path="/movies/:id" element={<ItemDetails />} />
            <Route path="/setup" element={<Setup />} />
            <Route path="/system" element={<SystemChecks />} />
            <Route path="/backups" element={<Backups />} />
            <Route path="/storage" element={<Storage />} />
            <Route path="/download-clients" element={<DownloadClients />} />
            <Route path="/metadata-refresh" element={<MetadataRefresh />} />
            <Route path="/series/:id" element={<Series />} />
            <Route path="/search" element={<MovieSearch />} />
            <Route path="/releases" element={<Releases />} />
            <Route path="/activity" element={<DownloadActivity />} />
            <Route path="/torrents" element={<Torrents />} />
            <Route path="/watch-activity" element={<WatchActivity />} />
            <Route path="/timeline" element={<Timeline />} />
            <Route path="/automation" element={<Automation />} />
            <Route path="/subtitles" element={<Subtitles />} />
            <Route
              path="*"
              element={
                <>
                  <PageHeading title="Page not found" />
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
