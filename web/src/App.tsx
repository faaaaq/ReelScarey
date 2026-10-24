import { useEffect, useState } from "react";
import { Badge } from "@radix-ui/themes";
import {
  Activity,
  CheckCircle2,
  Captions,
  Clapperboard,
  Clock3,
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
import { Dashboard } from "./pages/Dashboard";
import { Library } from "./pages/Library";
import { Settings } from "./pages/Settings";
import { Search as MovieSearch } from "./pages/Search";
import { AuthGate, useAuth } from "./auth";
import { Activity as DownloadActivity } from "./pages/Activity";
import { Releases } from "./pages/Releases";
import { Series } from "./pages/Series";
import { Automation } from "./pages/Automation";
import { Subtitles } from "./pages/Subtitles";
import { Setup, SystemChecks } from "./pages/System";
import { Indexers } from "./pages/Indexers";
import { ItemDetails } from "./pages/ItemDetails";
import { ExistingLibrary } from "./pages/ExistingLibrary";

const navigation = [
  { path: "/", name: "Dashboard", icon: LayoutDashboard },
  { path: "/movies", name: "Movies", icon: Film },
  { path: "/series", name: "TV shows", icon: MonitorPlay },
  { path: "/search", name: "Search", icon: Search },
  { path: "/activity", name: "Activity", icon: Activity },
  { path: "/automation", name: "Automation", icon: Clock3 },
  { path: "/subtitles", name: "Subtitles", icon: Captions },
];

export default function App() {
  return (
    <AuthGate>
      <AppShell />
    </AuthGate>
  );
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
    document.title = `${location.pathname === "/setup" ? "Setup" : location.pathname === "/system" ? "System checks" : location.pathname.startsWith("/series/") ? "TV show" : location.pathname.startsWith("/movies/") ? "Movie details" : location.pathname === "/import" ? "Import library" : location.pathname === "/indexers" ? "Indexers" : location.pathname === "/settings" ? "Settings" : location.pathname === "/releases" ? "Releases" : (navigation.find((item) => item.path === location.pathname)?.name ?? "Page")} · ReelScarey`;
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
          <Clapperboard size={21} />
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
            <Clapperboard size={23} />
          </span>
          <div className="wordmark">
            ReelScarey<small>YOUR MEDIA. ONE HOME.</small>
          </div>
        </Link>
        <p className="nav-label">LIBRARY</p>
        <nav aria-label="Main navigation">
          {navigation.map(({ path, name, icon: Icon }) => (
            <NavLink key={path} to={path} end={path === "/"}>
              <Icon size={19} strokeWidth={1.7} />
              <span>{name}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <nav aria-label="Administration">
            <NavLink to="/import">
              <FolderSearch size={19} strokeWidth={1.7} />
              <span>Import library</span>
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
          <div className="foundation-label">
            <span className="status-dot" />
            <span>Movies & TV</span>
            <Badge color="gray" size="1">
              0.11
            </Badge>
          </div>
          <div className="account-control">
            <span>Signed in as {session.username}</span>
            <button onClick={() => void logout()} disabled={signingOut}>
              <LogOut size={15} />
              {signingOut ? "Signing out…" : "Sign out"}
            </button>
            {signOutError && <p role="alert">{signOutError}</p>}
          </div>
        </div>
      </aside>
      <main id="main-content" tabIndex={-1}>
        <div className="topbar">
          <span>THE HOME SCREEN, REIMAGINED.</span>
          <span className="topbar-note">Self-hosted. All yours.</span>
        </div>
        <div className="page-content">
          <Routes>
            <Route path="/login" element={<Navigate to="/" replace />} />
            <Route path="/" element={<Dashboard />} />
            <Route
              path="/movies"
              element={<Library key="movie" kind="movie" />}
            />
            <Route
              path="/series"
              element={<Library key="series" kind="series" />}
            />
            <Route path="/settings" element={<Settings />} />
            <Route path="/indexers" element={<Indexers />} />
            <Route path="/import" element={<ExistingLibrary />} />
            <Route path="/movies/:id" element={<ItemDetails />} />
            <Route path="/setup" element={<Setup />} />
            <Route path="/system" element={<SystemChecks />} />
            <Route path="/series/:id" element={<Series />} />
            <Route path="/search" element={<MovieSearch />} />
            <Route path="/releases" element={<Releases />} />
            <Route path="/activity" element={<DownloadActivity />} />
            <Route path="/automation" element={<Automation />} />
            <Route path="/subtitles" element={<Subtitles />} />
            <Route
              path="*"
              element={
                <>
                  <PageHeading
                    eyebrow="SCENE NOT FOUND"
                    title="This page isn’t here."
                    description="Return to your library to pick up where you left off."
                  />
                  <Link to="/" className="text-link">
                    Back to dashboard
                  </Link>
                </>
              }
            />
          </Routes>
          <footer className="page-footer">
            <span>ReelScarey</span>
            <span>Less managing. More watching.</span>
          </footer>
        </div>
      </main>
    </div>
  );
}
