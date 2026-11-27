import {
  lazy,
  Suspense,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Button, DropdownMenu } from "@radix-ui/themes";
import {
  ChevronDown,
  Disc3,
  Inbox,
  LogOut,
  Menu,
  Plus,
  User,
  X,
} from "lucide-react";
import {
  Link,
  Navigate,
  NavLink,
  Route,
  Routes,
  useLocation,
} from "react-router-dom";
import { GlobalSearch } from "./components/GlobalSearch";
import { PageLoadBoundary } from "./components/PageLoadBoundary";
import { HistoryTray } from "./components/HistoryTray";
import { ThemeToggle } from "./components/ThemeToggle";
import { MusicPlayerBar, MusicPlayerProvider } from "./musicPlayer";
import { AuthGate, useAuth } from "./auth";
import { hasAccess, type Session } from "./api";
import { childActive, navigation, pageTitle, type Place } from "./navigation";
import { YouTubeReviewBadge } from "./pages/youtube/ReviewBadge";
import { MediaWorkBanner } from "./components/MediaWorkBanner";
import { Alert, EmptyState, LoadingIndicator, PageBody } from "./ui";

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
const Usenet = lazy(() =>
  import("./pages/Usenet").then((m) => ({ default: m.Usenet })),
);
const Releases = lazy(() =>
  import("./pages/Releases").then((m) => ({ default: m.Releases })),
);
const ReleaseBlacklist = lazy(() =>
  import("./pages/Blacklist").then((m) => ({
    default: m.ReleaseBlacklist,
  })),
);
const Duplicates = lazy(() =>
  import("./pages/Duplicates").then((m) => ({ default: m.Duplicates })),
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
const Diagnostics = lazy(() =>
  import("./pages/Diagnostics").then((m) => ({ default: m.Diagnostics })),
);
const Backups = lazy(() =>
  import("./pages/Backups").then((m) => ({ default: m.Backups })),
);
const Storage = lazy(() =>
  import("./pages/Storage").then((m) => ({ default: m.Storage })),
);
const StorageInsights = lazy(() =>
  import("./pages/StorageInsights").then((m) => ({
    default: m.StorageInsights,
  })),
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
const Notifications = lazy(() =>
  import("./pages/Notifications").then((m) => ({ default: m.Notifications })),
);
const APIKeys = lazy(() =>
  import("./pages/APIKeys").then((m) => ({ default: m.APIKeys })),
);
const MediaServers = lazy(() =>
  import("./pages/MediaServers").then((m) => ({ default: m.MediaServers })),
);
const LanguageProfiles = lazy(() =>
  import("./pages/LanguageProfiles").then((m) => ({
    default: m.LanguageProfiles,
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
const YouTubeDownloads = lazy(() =>
  import("./pages/youtube/Downloads").then((m) => ({ default: m.Downloads })),
);
const YouTubeShows = lazy(() =>
  import("./pages/youtube/ShowsOverview").then((m) => ({
    default: m.ShowsOverview,
  })),
);
const YouTubeLibrary = lazy(() =>
  import("./pages/youtube/LibraryFiles").then((m) => ({
    default: m.LibraryFiles,
  })),
);
const AdminRequests = lazy(() =>
  import("./pages/Requests").then((m) => ({ default: m.AdminRequests })),
);
const RequesterHome = lazy(() =>
  import("./pages/Requests").then((m) => ({ default: m.RequesterHome })),
);
const ChangeAccountPassword = lazy(() =>
  import("./pages/Requests").then((m) => ({
    default: m.ChangeAccountPassword,
  })),
);
const Accounts = lazy(() =>
  import("./pages/Accounts").then((m) => ({ default: m.Accounts })),
);
const WatchActivity = lazy(() =>
  import("./pages/WatchActivity").then((m) => ({ default: m.WatchActivity })),
);
const Timeline = lazy(() =>
  import("./pages/Timeline").then((m) => ({ default: m.Timeline })),
);
const Music = lazy(() =>
  import("./pages/MusicLibrary").then((m) => ({ default: m.MusicLibrary })),
);
const MusicFullPlayer = lazy(() =>
  import("./pages/MusicPlayerWorkspace").then((m) => ({
    default: m.MusicPlayerWorkspace,
  })),
);
const MusicArtist = lazy(() =>
  import("./pages/Music").then((m) => ({ default: m.MusicArtistPage })),
);
const MusicAlbum = lazy(() =>
  import("./pages/Music").then((m) => ({ default: m.MusicAlbumPage })),
);
const MusicImport = lazy(() =>
  import("./pages/MusicImport").then((m) => ({ default: m.MusicImport })),
);
const MusicServer = lazy(() =>
  import("./pages/MusicServer").then((m) => ({ default: m.MusicServer })),
);
const MusicDiscover = lazy(() =>
  import("./pages/MusicDiscover").then((m) => ({ default: m.MusicDiscover })),
);
const MusicOrganize = lazy(() =>
  import("./pages/MusicOrganize").then((m) => ({ default: m.MusicOrganize })),
);
const MusicWanted = lazy(() =>
  import("./pages/MusicWanted").then((m) => ({ default: m.MusicWanted })),
);
const MusicSettings = lazy(() =>
  import("./pages/MusicSettings").then((m) => ({ default: m.MusicSettings })),
);

export default function App() {
  return (
    <AuthGate>
      <PageLoadBoundary>
        <Suspense fallback={<LoadingIndicator />}>
          <RoleApp />
        </Suspense>
      </PageLoadBoundary>
    </AuthGate>
  );
}

function RoleApp() {
  const { session } = useAuth();
  // Responses from before centralized accounts had no role and represented
  // the single administrator. Every explicitly limited role uses the small
  // access-controlled shell below.
  return !session.role || session.role === "admin" ? (
    <AppShell />
  ) : (
    <LimitedShell />
  );
}

function firstLimitedRoute(session: Session) {
  if (hasAccess(session, "requests")) return "/requests";
  if (hasAccess(session, "music_player")) return "/music/player";
  return "/";
}

function LimitedShell() {
  const { session } = useAuth();
  const music = hasAccess(session, "music_player");
  const shell = (
    <LimitedShellFrame
      requests={hasAccess(session, "requests")}
      music={music}
      home={firstLimitedRoute(session)}
    />
  );
  // Requests-only and no-access accounts do not need any player state. In
  // particular, none of the administrator shell's polling components are
  // mounted for any limited account.
  const identity = session.user_account_id ?? session.account_id;
  return music ? (
    <MusicPlayerProvider
      key={identity}
      identity={identity}
      canManageSettings={false}
    >
      {shell}
    </MusicPlayerProvider>
  ) : (
    shell
  );
}

function LimitedShellFrame({
  requests,
  music,
  home,
}: {
  requests: boolean;
  music: boolean;
  home: string;
}) {
  const { signOut } = useAuth();
  const [accountError, setAccountError] = useState("");
  const [routeAnnouncement, setRouteAnnouncement] = useState("");
  const location = useLocation();
  const previousPath = useRef("");

  useEffect(() => {
    const title =
      home === "/" && location.pathname === "/"
        ? "No web access"
        : location.pathname === "/requests"
          ? "My requests"
          : pageTitle(location.pathname);
    document.title = `${title} — ReelScarey`;
    if (previousPath.current !== location.pathname) {
      document.getElementById("main-content")?.focus({ preventScroll: true });
      previousPath.current = location.pathname;
      setRouteAnnouncement(title);
    }
  }, [home, location.pathname]);

  const noAccess = (
    <PageBody>
      <EmptyState
        title="No web pages available"
        actions={
          <Button
            variant="soft"
            onClick={() => {
              setAccountError("");
              void signOut().catch(() =>
                setAccountError("Could not sign out. Try again."),
              );
            }}
          >
            <LogOut size={15} aria-hidden="true" /> Sign out
          </Button>
        }
      >
        <p>
          This account has no Requests or Music player access. Contact an
          administrator to change its access, or sign out.
        </p>
      </EmptyState>
    </PageBody>
  );
  const defaultPage = home === "/" ? noAccess : <Navigate to={home} replace />;
  const limitedRoute = (allowed: boolean, page: ReactNode) =>
    allowed ? page : <Navigate to={home} replace />;

  return (
    <div className="rs-app">
      <a href="#main-content" className="skip-link">
        Skip to content
      </a>
      <p className="sr-only" aria-live="polite">
        {routeAnnouncement}
      </p>
      <header className="rs-header">
        <div className="rs-header-logo" style={{ width: "auto" }}>
          <Link to={home} className="rs-logo" aria-label="ReelScarey home">
            <span className="rs-logo-mark">
              <img src="/favicon.svg" alt="" width="32" height="32" />
            </span>
            <span className="rs-logo-text">ReelScarey</span>
          </Link>
        </div>
        <nav
          aria-label="Main navigation"
          style={{
            display: "flex",
            flex: 1,
            alignItems: "center",
            gap: 4,
            padding: "0 10px",
          }}
        >
          {requests && (
            <NavLink
              to="/requests"
              className={({ isActive }) =>
                `rs-nav-link ${isActive ? "active" : ""}`
              }
              style={({ isActive }) => ({
                color: isActive ? "var(--rs-brand)" : undefined,
                fontWeight: isActive ? 650 : undefined,
              })}
            >
              <Inbox aria-hidden="true" />
              <span>Requests</span>
            </NavLink>
          )}
          {music && (
            <NavLink
              to="/music/player"
              className={({ isActive }) =>
                `rs-nav-link ${isActive ? "active" : ""}`
              }
              style={({ isActive }) => ({
                color: isActive ? "var(--rs-brand)" : undefined,
                fontWeight: isActive ? 650 : undefined,
              })}
            >
              <Disc3 aria-hidden="true" />
              <span>Music player</span>
            </NavLink>
          )}
        </nav>
        <div className="rs-header-actions">
          <ThemeToggle />
          <AccountMenu onError={setAccountError} />
        </div>
      </header>
      <main
        id="main-content"
        className="rs-main"
        style={{ marginLeft: 0 }}
        tabIndex={-1}
      >
        {accountError && (
          <PageBody>
            <Alert kind="danger">{accountError}</Alert>
          </PageBody>
        )}
        <Suspense fallback={<LoadingIndicator />}>
          <Routes>
            <Route path="/login" element={<Navigate to={home} replace />} />
            <Route
              path="/account/password"
              element={
                <PageBody>
                  <ChangeAccountPassword />
                </PageBody>
              }
            />
            <Route path="/" element={defaultPage} />
            <Route
              path="/requests"
              element={limitedRoute(requests, <RequesterHome embedded />)}
            />
            <Route
              path="/music/player"
              element={limitedRoute(music, <MusicFullPlayer />)}
            />
            <Route path="*" element={<Navigate to={home} replace />} />
          </Routes>
        </Suspense>
      </main>
      {music && <MusicPlayerBar />}
    </div>
  );
}

function AccountMenu({ onError }: { onError: (message: string) => void }) {
  const { session, signOut } = useAuth();
  const [signingOut, setSigningOut] = useState(false);
  async function logout() {
    setSigningOut(true);
    onError("");
    try {
      await signOut();
    } catch {
      onError("Could not sign out. Try again.");
    } finally {
      setSigningOut(false);
    }
  }
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        <button
          type="button"
          className="rs-account-button"
          aria-label={`Account: ${session.username}`}
        >
          <User size={18} aria-hidden="true" />
          <span className="rs-account-name">{session.username}</span>
          <ChevronDown size={14} aria-hidden="true" />
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end">
        <DropdownMenu.Label>Signed in as {session.username}</DropdownMenu.Label>
        {session.role && session.role !== "admin" && (
          <DropdownMenu.Item asChild>
            <Link to="/account/password">Change password</Link>
          </DropdownMenu.Item>
        )}
        <DropdownMenu.Separator />
        <DropdownMenu.Item disabled={signingOut} onSelect={() => void logout()}>
          <LogOut size={15} aria-hidden="true" />
          {signingOut ? "Signing out…" : "Sign out"}
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  );
}

function Sidebar({ place, open }: { place: Place; open: boolean }) {
  return (
    <aside
      className={`rs-sidebar ${open ? "is-open" : ""}`}
      id="main-navigation"
    >
      <nav className="rs-nav" aria-label="Main navigation">
        {navigation.map((item) => {
          const active = item.match(place);
          const Icon = item.icon;
          return (
            <div
              key={item.to}
              className={`rs-nav-item ${active ? "is-active" : ""}`}
            >
              <Link
                className="rs-nav-link"
                to={item.to}
                aria-current={
                  active && place.pathname === item.to ? "page" : undefined
                }
              >
                <Icon aria-hidden="true" />
                <span>{item.label}</span>
                {item.to === "/youtube" && <YouTubeReviewBadge />}
              </Link>
              {active && item.children && (
                <div className="rs-nav-children">
                  {item.children.map((child) => {
                    const current = childActive(child, place);
                    return (
                      <NavLink
                        key={child.to}
                        to={child.to}
                        end
                        className={() =>
                          `rs-nav-child ${current ? "active" : ""}`
                        }
                        aria-current={current ? "page" : undefined}
                      >
                        {child.label}
                      </NavLink>
                    );
                  })}
                </div>
              )}
            </div>
          );
        })}
      </nav>
    </aside>
  );
}

function AppShell() {
  const [menuOpen, setMenuOpen] = useState(false);
  const [accountError, setAccountError] = useState("");
  const [routeAnnouncement, setRouteAnnouncement] = useState("");
  const location = useLocation();
  const place = { pathname: location.pathname, search: location.search };
  const previousPath = useRef(location.pathname);
  const menuButton = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    setMenuOpen(false);
    const title = pageTitle(location.pathname);
    document.title = `${title} · ReelScarey`;
    if (previousPath.current !== location.pathname) {
      document.getElementById("main-content")?.focus({ preventScroll: true });
      previousPath.current = location.pathname;
      // Announce the new page for screen readers (A11Y-006): the focused main
      // landmark alone is silent in several common reader modes.
      setRouteAnnouncement(title);
    }
  }, [location.pathname, location.search]);

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
    <MusicPlayerProvider>
      <div className="rs-app">
        <a href="#main-content" className="skip-link">
          Skip to content
        </a>
        <p className="sr-only" aria-live="polite">
          {routeAnnouncement}
        </p>
        <header className="rs-header">
          <div className="rs-header-logo">
            <button
              ref={menuButton}
              type="button"
              className="rs-header-button rs-menu-toggle"
              aria-label={menuOpen ? "Close navigation" : "Open navigation"}
              aria-expanded={menuOpen}
              aria-controls="main-navigation"
              onClick={() => setMenuOpen(!menuOpen)}
            >
              {menuOpen ? <X size={20} /> : <Menu size={20} />}
            </button>
            <Link to="/" className="rs-logo" aria-label="ReelScarey dashboard">
              <span className="rs-logo-mark">
                <img src="/favicon.svg" alt="" width="32" height="32" />
              </span>
              <span className="rs-logo-text">ReelScarey</span>
            </Link>
          </div>
          <GlobalSearch />
          <div className="rs-header-actions">
            <Link className="rs-header-add" to="/search" aria-label="Add media">
              <Plus size={17} aria-hidden="true" />
              <span>Add media</span>
            </Link>
            <HistoryTray />
            <ThemeToggle />
            <AccountMenu onError={setAccountError} />
          </div>
        </header>
        <Sidebar place={place} open={menuOpen} />
        <button
          type="button"
          className={`rs-sidebar-backdrop ${menuOpen ? "is-open" : ""}`}
          aria-hidden="true"
          tabIndex={-1}
          onClick={() => setMenuOpen(false)}
        />
        <main id="main-content" className="rs-main" tabIndex={-1}>
          {accountError && (
            <PageBody>
              <Alert kind="danger">{accountError}</Alert>
            </PageBody>
          )}
          <Suspense fallback={<LoadingIndicator />}>
            <Routes>
              <Route path="/login" element={<Navigate to="/" replace />} />
              <Route path="/" element={<Dashboard />} />
              <Route path="/youtube" element={<YouTube />} />
              <Route path="/youtube/shows" element={<YouTubeShows />} />
              <Route path="/youtube/downloads" element={<YouTubeDownloads />} />
              <Route path="/youtube/library" element={<YouTubeLibrary />} />
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
              <Route path="/settings/:section" element={<Settings />} />
              <Route path="/requests" element={<AdminRequests />} />
              <Route path="/accounts" element={<Accounts />} />
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
              <Route path="/diagnostics" element={<Diagnostics />} />
              <Route path="/backups" element={<Backups />} />
              <Route path="/storage" element={<Storage />} />
              <Route path="/storage/insights" element={<StorageInsights />} />
              <Route path="/download-clients" element={<DownloadClients />} />
              <Route path="/metadata-refresh" element={<MetadataRefresh />} />
              <Route path="/notifications" element={<Notifications />} />
              <Route path="/api-keys" element={<APIKeys />} />
              <Route path="/media-servers" element={<MediaServers />} />
              <Route path="/language-profiles" element={<LanguageProfiles />} />
              <Route path="/series/:id" element={<Series />} />
              <Route path="/music" element={<Music />} />
              <Route path="/music/player" element={<MusicFullPlayer />} />
              <Route path="/music/artists/:id" element={<MusicArtist />} />
              <Route path="/music/albums/:id" element={<MusicAlbum />} />
              <Route path="/music/import" element={<MusicImport />} />
              <Route path="/music/server" element={<MusicServer />} />
              <Route path="/music/discover" element={<MusicDiscover />} />
              <Route path="/music/organize" element={<MusicOrganize />} />
              <Route path="/music/wanted" element={<MusicWanted />} />
              <Route path="/music/settings" element={<MusicSettings />} />
              <Route path="/search" element={<MovieSearch />} />
              <Route path="/releases" element={<Releases />} />
              <Route
                path="/releases/blacklist"
                element={<ReleaseBlacklist />}
              />
              <Route path="/duplicates" element={<Duplicates />} />
              <Route path="/activity" element={<DownloadActivity />} />
              <Route path="/torrents" element={<Torrents />} />
              <Route path="/usenet" element={<Usenet />} />
              <Route path="/watch-activity" element={<WatchActivity />} />
              <Route path="/timeline" element={<Timeline />} />
              <Route path="/automation" element={<Automation />} />
              <Route path="/subtitles" element={<Subtitles />} />
              <Route
                path="*"
                element={
                  <PageBody>
                    <EmptyState
                      title="Page not found"
                      actions={
                        <Button asChild variant="soft">
                          <Link to="/">Back to dashboard</Link>
                        </Button>
                      }
                    >
                      This address does not match a page in ReelScarey.
                    </EmptyState>
                  </PageBody>
                }
              />
            </Routes>
          </Suspense>
        </main>
        <MediaWorkBanner />
        <MusicPlayerBar />
      </div>
    </MusicPlayerProvider>
  );
}
