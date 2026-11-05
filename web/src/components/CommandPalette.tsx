import { useEffect, useMemo, useState } from "react";
import { Dialog } from "@radix-ui/themes";
import {
  Activity,
  CalendarDays,
  Captions,
  Clapperboard,
  Clock3,
  Film,
  LayoutDashboard,
  ListFilter,
  MonitorPlay,
  Search,
  Settings2,
  Tv,
} from "lucide-react";
import { useNavigate } from "react-router-dom";
import { api, type LibraryPage } from "../api";

type Result = {
  id: number;
  title: string;
  kind: "movie" | "series";
  category?: string;
  year?: number | null;
  status?: string;
};

const pages = [
  { path: "/", name: "Dashboard", icon: LayoutDashboard },
  { path: "/movies", name: "Movies", icon: Film },
  { path: "/series", name: "TV shows", icon: Tv },
  { path: "/anime", name: "Anime", icon: Clapperboard },
  { path: "/search", name: "Search", icon: Search },
  { path: "/wanted", name: "Wanted", icon: ListFilter },
  { path: "/calendar", name: "Calendar", icon: CalendarDays },
  { path: "/activity", name: "Activity", icon: Activity },
  { path: "/automation", name: "Automation", icon: Clock3 },
  { path: "/subtitles", name: "Subtitles", icon: Captions },
  { path: "/youtube", name: "YouTube", icon: MonitorPlay },
  { path: "/settings", name: "Settings", icon: Settings2 },
].map((page) => ({
  ...page,
  terms: page.name.toLowerCase(),
}));

function useResults(query: string) {
  const [results, setResults] = useState<Result[]>([]);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const value = query.trim();
    if (value.length < 2) {
      setResults([]);
      setBusy(false);
      return;
    }
    let cancelled = false;
    setBusy(true);
    const timer = window.setTimeout(() => {
      Promise.all(
        (["movie", "series", "anime"] as const).map((kind) =>
          api<LibraryPage>(
            `/library?kind=${kind}&page=1&query=${encodeURIComponent(value)}`,
          ).catch(() => null),
        ),
      )
        .then((pages) => {
          if (cancelled) return;
          const seen = new Set<number>();
          const found: Result[] = [];
          for (const page of pages) {
            if (!page) continue;
            for (const item of page.items ?? []) {
              if (seen.has(item.id)) continue;
              seen.add(item.id);
              found.push({
                id: item.id,
                title: item.title,
                kind: item.kind,
                category: item.category,
                year: item.year,
                status: item.status,
              });
            }
          }
          setResults(found.slice(0, 12));
          setBusy(false);
        })
        .catch(() => {
          if (!cancelled) {
            setResults([]);
            setBusy(false);
          }
        });
    }, 220);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      setBusy(false);
    };
  }, [query]);
  return { results, busy };
}

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const navigate = useNavigate();
  const { results, busy } = useResults(query);

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setOpen((current) => !current);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const matchingPages = useMemo(() => {
    const value = query.trim().toLowerCase();
    return pages
      .filter((page) => !value || page.terms.includes(value))
      .slice(0, value ? 4 : 8);
  }, [query]);

  function choose(path: string) {
    setOpen(false);
    setQuery("");
    navigate(path);
  }

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setQuery("");
      }}
    >
      <Dialog.Trigger>
        <button type="button" className="command-trigger">
          <Search size={15} aria-hidden="true" />
          <span>Search or jump to…</span>
          <kbd>Ctrl K</kbd>
        </button>
      </Dialog.Trigger>
      <Dialog.Content className="command-dialog">
        <Dialog.Title className="sr-only">Search and jump</Dialog.Title>
        <Dialog.Description className="sr-only">
          Search the saved library or jump to a page.
        </Dialog.Description>
        <label className="command-input">
          <Search size={17} aria-hidden="true" />
          <input
            autoFocus
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search movies, shows, anime or pages"
            aria-label="Search the library or pages"
          />
        </label>
        <div className="command-list" role="listbox" aria-busy={busy}>
          {matchingPages.length > 0 && (
            <>
              <p className="command-group">Go to</p>
              {matchingPages.map(({ path, name, icon: Icon }) => (
                <button
                  type="button"
                  className="command-item"
                  key={path}
                  role="option"
                  aria-selected="false"
                  onClick={() => choose(path)}
                >
                  <Icon size={16} aria-hidden="true" />
                  {name}
                </button>
              ))}
            </>
          )}
          {query.trim().length >= 2 && (
            <>
              <p className="command-group">
                {busy ? "Searching…" : "Saved titles"}
              </p>
              {!busy && results.length === 0 && (
                <p className="command-empty">
                  No saved titles match “{query}”.
                </p>
              )}
              {results.map((item) => (
                <button
                  type="button"
                  className="command-item"
                  key={item.id}
                  role="option"
                  aria-selected="false"
                  onClick={() =>
                    choose(
                      `/${item.kind === "movie" ? "movies" : "series"}/${item.id}`,
                    )
                  }
                >
                  {item.kind === "movie" ? (
                    <Film size={16} aria-hidden="true" />
                  ) : (
                    <Tv size={16} aria-hidden="true" />
                  )}
                  <span>{item.title}</span>
                  <span className="command-meta">
                    {item.year ?? "Year unknown"}
                    {item.category === "anime" ? " · Anime" : ""}
                  </span>
                </button>
              ))}
            </>
          )}
        </div>
      </Dialog.Content>
    </Dialog.Root>
  );
}
