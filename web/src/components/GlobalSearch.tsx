import { useEffect, useId, useMemo, useRef, useState } from "react";
import { ArrowRight, Film, Search, Tv } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { api, type LibraryPage } from "../api";
import { navigation } from "../navigation";

type Option = {
  key: string;
  group: "Library" | "Pages" | "Add new";
  label: string;
  meta?: string;
  icon: typeof Film;
  path: string;
};

const pages = navigation.flatMap((item) => [
  { label: item.label, path: item.to, icon: item.icon },
  ...(item.children ?? [])
    .filter((child) => child.to !== item.to)
    .map((child) => ({
      label: `${item.label}: ${child.label}`,
      path: child.to,
      icon: item.icon,
    })),
]);

function useLibraryMatches(query: string) {
  const [items, setItems] = useState<Option[]>([]);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const value = query.trim();
    if (value.length < 2) {
      setItems([]);
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
      ).then((results) => {
        if (cancelled) return;
        const seen = new Set<number>();
        const found: Option[] = [];
        for (const page of results)
          for (const item of page?.items ?? []) {
            if (seen.has(item.id)) continue;
            seen.add(item.id);
            found.push({
              key: `item-${item.id}`,
              group: "Library",
              label: item.title,
              meta: [
                item.year ?? "",
                item.category === "anime"
                  ? "Anime"
                  : item.kind === "movie"
                    ? "Movie"
                    : "TV",
              ]
                .filter(Boolean)
                .join(" · "),
              icon: item.kind === "movie" ? Film : Tv,
              path: `/${item.kind === "movie" ? "movies" : "series"}/${item.id}`,
            });
          }
        setItems(found.slice(0, 10));
        setBusy(false);
      });
    }, 200);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [query]);
  return { items, busy };
}

/**
 * Header search: finds saved titles, jumps to pages and offers adding new
 * titles from TMDb. Ctrl/Cmd+K or "/" focuses it.
 */
export function GlobalSearch() {
  const navigate = useNavigate();
  const input = useRef<HTMLInputElement>(null);
  const listId = useId();
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const library = useLibraryMatches(query);

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      const target = event.target as HTMLElement | null;
      const typing =
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable);
      if (
        ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") ||
        (event.key === "/" && !typing)
      ) {
        event.preventDefault();
        input.current?.focus();
        setOpen(true);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const options = useMemo(() => {
    const value = query.trim();
    const lower = value.toLowerCase();
    if (!value) return [] as Option[];
    const pageMatches: Option[] = pages
      .filter((page) => page.label.toLowerCase().includes(lower))
      .slice(0, 5)
      .map((page) => ({
        key: `page-${page.path}`,
        group: "Pages",
        label: page.label,
        icon: page.icon,
        path: page.path,
      }));
    const add: Option[] = [
      {
        key: "add-movie",
        group: "Add new",
        label: `Search TMDb movies for “${value}”`,
        icon: ArrowRight,
        path: `/search?query=${encodeURIComponent(value)}`,
      },
      {
        key: "add-series",
        group: "Add new",
        label: `Search TMDb TV shows for “${value}”`,
        icon: ArrowRight,
        path: `/search?kind=series&query=${encodeURIComponent(value)}`,
      },
    ];
    return [...library.items, ...pageMatches, ...add];
  }, [query, library.items]);

  useEffect(() => setActive(0), [query]);

  function choose(option: Option | undefined) {
    if (!option) return;
    setOpen(false);
    setQuery("");
    input.current?.blur();
    navigate(option.path);
  }

  const showList = open && query.trim().length > 0;
  let lastGroup = "";
  return (
    <div
      className="rs-header-search"
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node))
          setOpen(false);
      }}
    >
      <label className="rs-header-search-field">
        <Search size={17} aria-hidden="true" />
        <input
          ref={input}
          role="combobox"
          aria-label="Search the library or pages"
          aria-expanded={showList}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={
            showList && options[active]
              ? `${listId}-${options[active].key}`
              : undefined
          }
          placeholder="Search"
          value={query}
          maxLength={200}
          onFocus={() => setOpen(true)}
          onChange={(event) => {
            setQuery(event.target.value);
            setOpen(true);
          }}
          onKeyDown={(event) => {
            if (event.key === "ArrowDown") {
              event.preventDefault();
              setActive((n) => Math.min(n + 1, options.length - 1));
            } else if (event.key === "ArrowUp") {
              event.preventDefault();
              setActive((n) => Math.max(n - 1, 0));
            } else if (event.key === "Enter") {
              event.preventDefault();
              choose(options[active]);
            } else if (event.key === "Escape") {
              setOpen(false);
              setQuery("");
            }
          }}
        />
        <kbd aria-hidden="true">Ctrl K</kbd>
      </label>
      {showList && (
        <div className="rs-search-results" id={listId} role="listbox">
          {library.busy && (
            <p className="rs-search-empty" role="status">
              Searching the library…
            </p>
          )}
          {options.map((option, index) => {
            const heading = option.group !== lastGroup;
            lastGroup = option.group;
            const Icon = option.icon;
            return (
              <div key={option.key} role="presentation">
                {heading && (
                  <p className="rs-search-group" role="presentation">
                    {option.group}
                  </p>
                )}
                <button
                  type="button"
                  id={`${listId}-${option.key}`}
                  role="option"
                  aria-selected={index === active}
                  className="rs-search-option"
                  tabIndex={-1}
                  onMouseEnter={() => setActive(index)}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => choose(option)}
                >
                  <Icon size={15} aria-hidden="true" />
                  <span className="rs-truncate">{option.label}</span>
                  {option.meta && (
                    <span className="rs-search-option-meta">{option.meta}</span>
                  )}
                </button>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
