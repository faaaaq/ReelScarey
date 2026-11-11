import {
  Activity,
  ArrowDownUp,
  CalendarDays,
  Clapperboard,
  CloudDownload,
  Disc3,
  Film,
  Inbox,
  LayoutDashboard,
  ListTodo,
  MonitorPlay,
  Server,
  Settings2,
  Tv,
  type LucideIcon,
} from "lucide-react";

export type Place = { pathname: string; search: string };

export type NavChild = {
  label: string;
  to: string;
  /** Active test; defaults to an exact pathname match. */
  match?: (place: Place) => boolean;
};

export type NavItem = {
  label: string;
  to: string;
  icon: LucideIcon;
  match: (place: Place) => boolean;
  children?: NavChild[];
};

function param(place: Place, key: string) {
  return new URLSearchParams(place.search).get(key);
}
const starts = (place: Place, ...prefixes: string[]) =>
  prefixes.some(
    (prefix) =>
      place.pathname === prefix || place.pathname.startsWith(`${prefix}/`),
  );
const searchKind = (place: Place) => {
  if (place.pathname !== "/search") return null;
  const kind = param(place, "kind");
  return kind === "series" || kind === "anime" ? kind : "movie";
};
const importKind = (place: Place) => {
  if (place.pathname !== "/import") return null;
  const collection = param(place, "collection");
  return collection === "series" || collection === "anime"
    ? collection
    : "movie";
};

export const navigation: NavItem[] = [
  {
    label: "Dashboard",
    to: "/",
    icon: LayoutDashboard,
    match: (p) => p.pathname === "/",
  },
  {
    label: "Movies",
    to: "/movies",
    icon: Film,
    match: (p) =>
      starts(p, "/movies") ||
      searchKind(p) === "movie" ||
      importKind(p) === "movie",
    children: [
      {
        label: "Add New",
        to: "/search",
        match: (p) => searchKind(p) === "movie",
      },
      {
        label: "Library Import",
        to: "/import",
        match: (p) => importKind(p) === "movie",
      },
    ],
  },
  {
    label: "TV Shows",
    to: "/series",
    icon: Tv,
    match: (p) =>
      starts(p, "/series") ||
      searchKind(p) === "series" ||
      importKind(p) === "series",
    children: [
      {
        label: "Add New",
        to: "/search?kind=series",
        match: (p) => searchKind(p) === "series",
      },
      {
        label: "Library Import",
        to: "/import?collection=series",
        match: (p) => importKind(p) === "series",
      },
    ],
  },
  {
    label: "Anime",
    to: "/anime",
    icon: Clapperboard,
    match: (p) =>
      starts(p, "/anime") ||
      searchKind(p) === "anime" ||
      importKind(p) === "anime",
    children: [
      {
        label: "Add New",
        to: "/search?kind=anime&format=series",
        match: (p) => searchKind(p) === "anime",
      },
      {
        label: "Library Import",
        to: "/import?collection=anime",
        match: (p) => importKind(p) === "anime",
      },
    ],
  },
  {
    label: "Music",
    to: "/music",
    icon: Disc3,
    match: (p) => starts(p, "/music"),
    children: [
      { label: "Library", to: "/music" },
      { label: "Library Import", to: "/music/import" },
      { label: "Organize", to: "/music/organize" },
      { label: "Wanted", to: "/music/wanted" },
    ],
  },
  {
    label: "YouTube",
    to: "/youtube",
    icon: MonitorPlay,
    match: (p) => starts(p, "/youtube"),
    children: [
      { label: "Library", to: "/youtube" },
      { label: "Subscriptions", to: "/youtube/subscriptions" },
    ],
  },
  {
    label: "Calendar",
    to: "/calendar",
    icon: CalendarDays,
    match: (p) => starts(p, "/calendar"),
  },
  {
    label: "Activity",
    to: "/activity",
    icon: Activity,
    match: (p) =>
      starts(p, "/activity", "/timeline", "/watch-activity", "/releases"),
    children: [
      { label: "Queue", to: "/activity" },
      { label: "History", to: "/timeline" },
      { label: "Playback", to: "/watch-activity" },
      { label: "Release blacklist", to: "/releases/blacklist" },
    ],
  },
  {
    label: "Wanted",
    to: "/wanted",
    icon: ListTodo,
    match: (p) => starts(p, "/wanted"),
  },
  {
    label: "Torrents",
    to: "/torrents",
    icon: ArrowDownUp,
    match: (p) => starts(p, "/torrents"),
  },
  {
    label: "Usenet",
    to: "/usenet",
    icon: CloudDownload,
    match: (p) => starts(p, "/usenet"),
  },
  {
    label: "Requests",
    to: "/requests",
    icon: Inbox,
    match: (p) => starts(p, "/requests"),
  },
  {
    label: "Settings",
    to: "/settings",
    icon: Settings2,
    match: (p) =>
      starts(
        p,
        "/settings",
        "/organization",
        "/indexers",
        "/announces",
        "/download-clients",
        "/automation",
        "/subtitles",
        "/metadata-refresh",
        "/duplicates",
        "/notifications",
        "/api-keys",
        "/media-servers",
        "/language-profiles",
      ),
    children: [
      { label: "Media Management", to: "/settings/media" },
      { label: "Duplicate review", to: "/duplicates" },
      { label: "Indexers", to: "/indexers" },
      { label: "Announce Feeds", to: "/announces" },
      { label: "Download Clients", to: "/download-clients" },
      { label: "Automation", to: "/automation" },
      { label: "Subtitles", to: "/subtitles" },
      { label: "Metadata", to: "/metadata-refresh" },
      { label: "Notifications", to: "/notifications" },
      { label: "API keys", to: "/api-keys" },
      { label: "Media servers", to: "/media-servers" },
      { label: "Language profiles", to: "/language-profiles" },
      { label: "Connect", to: "/settings/connect" },
      { label: "General", to: "/settings/general" },
      { label: "Security", to: "/settings/security" },
    ],
  },
  {
    label: "System",
    to: "/system",
    icon: Server,
    match: (p) =>
      starts(p, "/system", "/backups", "/storage", "/setup", "/diagnostics"),
    children: [
      { label: "Status", to: "/system" },
      { label: "Diagnostics", to: "/diagnostics" },
      { label: "Backups", to: "/backups" },
      { label: "Storage", to: "/storage" },
      { label: "Storage insights", to: "/storage/insights" },
      { label: "Setup Guide", to: "/setup" },
    ],
  },
];

export function childActive(child: NavChild, place: Place) {
  if (child.match) return child.match(place);
  const [path] = child.to.split("?");
  return place.pathname === path;
}

const titles: Record<string, string> = {
  "/": "Dashboard",
  "/movies": "Movies",
  "/series": "TV Shows",
  "/anime": "Anime",
  "/music": "Music",
  "/music/import": "Music import",
  "/music/organize": "Organize music",
  "/music/wanted": "Music wanted",
  "/music/discover": "Music discover",
  "/music/server": "Music server",
  "/youtube": "YouTube",
  "/youtube/subscriptions": "YouTube subscriptions",
  "/search": "Add New",
  "/import": "Library Import",
  "/calendar": "Calendar",
  "/activity": "Activity",
  "/timeline": "History",
  "/watch-activity": "Playback",
  "/releases": "Releases",
  "/releases/blacklist": "Release blacklist",
  "/duplicates": "Duplicate review",
  "/wanted": "Wanted",
  "/torrents": "Torrents",
  "/usenet": "Usenet",
  "/requests": "Requests",
  "/settings": "Settings",
  "/settings/general": "General settings",
  "/settings/media": "Media management",
  "/settings/connect": "Connect",
  "/settings/security": "Security",
  "/settings/clients": "Primary connections",
  "/settings/backup-import": "Sonarr / Radarr import",
  "/organization": "Organize files",
  "/indexers": "Indexers",
  "/announces": "Announce feeds",
  "/download-clients": "Download clients",
  "/automation": "Automation",
  "/subtitles": "Subtitles",
  "/metadata-refresh": "Metadata refresh",
  "/notifications": "Notifications",
  "/api-keys": "API keys",
  "/media-servers": "Media servers",
  "/language-profiles": "Language profiles",
  "/system": "System",
  "/diagnostics": "Diagnostics",
  "/backups": "Backups",
  "/storage": "Storage",
  "/storage/insights": "Storage insights",
  "/setup": "Setup",
};

export function pageTitle(pathname: string) {
  if (titles[pathname]) return titles[pathname];
  if (/^\/series\/[^/]+$/.test(pathname)) return "TV show";
  if (/^\/movies\/[^/]+$/.test(pathname)) return "Movie details";
  if (/^\/music\/artists\/[^/]+$/.test(pathname)) return "Music artist";
  if (/^\/music\/albums\/[^/]+$/.test(pathname)) return "Music album";
  return "Page not found";
}
