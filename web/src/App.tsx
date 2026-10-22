import { useEffect, useState } from "react";
import { Badge } from "@radix-ui/themes";
import {
  Activity,
  Captions,
  Clapperboard,
  Film,
  LayoutDashboard,
  Menu,
  MonitorPlay,
  Search,
  Settings2,
  X,
} from "lucide-react";
import { Link, NavLink, Route, Routes, useLocation } from "react-router-dom";
import { FuturePage, PageHeading } from "./components";
import { Dashboard } from "./pages/Dashboard";
import { Library } from "./pages/Library";
import { Settings } from "./pages/Settings";

const navigation = [
  { path: "/", name: "Dashboard", icon: LayoutDashboard },
  { path: "/movies", name: "Movies", icon: Film },
  { path: "/series", name: "TV shows", icon: MonitorPlay },
  { path: "/search", name: "Search", icon: Search },
  { path: "/activity", name: "Activity", icon: Activity },
  { path: "/subtitles", name: "Subtitles", icon: Captions },
];

export default function App() {
  const [menuOpen, setMenuOpen] = useState(false);
  const location = useLocation();
  useEffect(() => {
    setMenuOpen(false);
    document.title = `${location.pathname === "/settings" ? "Settings" : (navigation.find((item) => item.path === location.pathname)?.name ?? "Page")} · ReelScarey`;
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
            <NavLink to="/settings">
              <Settings2 size={19} strokeWidth={1.7} />
              <span>Settings</span>
            </NavLink>
          </nav>
          <div className="foundation-label">
            <span className="status-dot" />
            <span>Foundation release</span>
            <Badge color="gray" size="1">
              0.1
            </Badge>
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
            <Route
              path="/search"
              element={
                <>
                  <PageHeading
                    eyebrow="FIND YOUR NEXT FEATURE"
                    title="Search"
                    description="Metadata and releases, together."
                  />
                  <FuturePage
                    icon={Search}
                    title="Discovery is up next."
                    description="Real TMDb movie search and Torznab release results arrive in the movie workflow. There are no connected search providers in this release."
                    phase="Planned · Phase 2"
                  />
                </>
              }
            />
            <Route
              path="/activity"
              element={
                <>
                  <PageHeading
                    eyebrow="BEHIND THE SCENES"
                    title="Activity"
                    description="A clear view of what your library is doing."
                  />
                  <FuturePage
                    icon={Activity}
                    title="No download workflow is running."
                    description="Download tracking, persistent background jobs, and safe imports arrive with the movie workflow. No download clients are connected in this release."
                    phase="Planned · Phase 2"
                  />
                </>
              }
            />
            <Route
              path="/subtitles"
              element={
                <>
                  <PageHeading
                    eyebrow="DON’T MISS A WORD"
                    title="Subtitles"
                    description="The right words for every scene."
                  />
                  <FuturePage
                    icon={Captions}
                    title="Subtitles have a place here."
                    description="Provider connections, language preferences, and automatic subtitle matching will follow the core movie and TV workflows."
                    phase="Planned · Phase 5"
                  />
                </>
              }
            />
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
