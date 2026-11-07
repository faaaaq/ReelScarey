import { memo, useMemo, useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import {
  matchesStatus,
  originOf,
  readStored,
  sourceFilters,
  statusFilters,
  tagsOf,
  trackerHost,
  writeStored,
  type Filters,
  type Torrent,
} from "./model";

type Section = "status" | "source" | "categories" | "tags" | "trackers";
const collapsedKey = "reelscarey.torrents.sidebar.collapsed";

type Entry = { key: string; label: string; count: number; active: boolean };

function countBy(items: Torrent[], pick: (t: Torrent) => string[]) {
  const counts = new Map<string, number>();
  for (const t of items)
    for (const value of pick(t))
      counts.set(value, (counts.get(value) ?? 0) + 1);
  return [...counts.entries()].sort((a, b) =>
    a[0].localeCompare(b[0], undefined, { sensitivity: "base" }),
  );
}

export const FilterSidebar = memo(function FilterSidebar({
  items,
  filters,
  onChange,
}: {
  items: Torrent[];
  filters: Filters;
  onChange: (next: Filters) => void;
}) {
  const [collapsed, setCollapsed] = useState<Section[]>(() =>
    readStored<Section[]>(collapsedKey, []),
  );
  function toggle(section: Section) {
    setCollapsed((current) => {
      const next = current.includes(section)
        ? current.filter((s) => s !== section)
        : [...current, section];
      writeStored(collapsedKey, next);
      return next;
    });
  }

  const groups = useMemo(() => {
    const status: Entry[] = statusFilters.map((f) => ({
      key: f.key,
      label: f.label,
      count: items.filter((t) => matchesStatus(t, f.key)).length,
      active: filters.status === f.key,
    }));
    const source: Entry[] = sourceFilters.map((f) => ({
      key: f.key,
      label: f.label,
      count:
        f.key === "all"
          ? items.length
          : items.filter((t) => originOf(t) === f.key).length,
      active: filters.source === f.key,
    }));
    const categoryCounts = countBy(items, (t) =>
      t.category ? [t.category] : [],
    );
    const categories: Entry[] = [
      {
        key: "\u0000all",
        label: "All",
        count: items.length,
        active: filters.category === null,
      },
      {
        key: "",
        label: "Uncategorized",
        count: items.filter((t) => !t.category).length,
        active: filters.category === "",
      },
      ...categoryCounts.map(([name, count]) => ({
        key: name,
        label: name,
        count,
        active: filters.category === name,
      })),
    ];
    const tagCounts = countBy(items, tagsOf);
    const tags: Entry[] = [
      {
        key: "\u0000all",
        label: "All",
        count: items.length,
        active: filters.tag === null,
      },
      {
        key: "",
        label: "Untagged",
        count: items.filter((t) => tagsOf(t).length === 0).length,
        active: filters.tag === "",
      },
      ...tagCounts.map(([name, count]) => ({
        key: name,
        label: name,
        count,
        active: filters.tag === name,
      })),
    ];
    const trackerCounts = countBy(items, (t) => {
      const host = trackerHost(t);
      return host ? [host] : [];
    });
    const trackers: Entry[] = [
      {
        key: "\u0000all",
        label: "All",
        count: items.length,
        active: filters.tracker === null,
      },
      {
        key: "",
        label: "Trackerless",
        count: items.filter((t) => !t.tracker).length,
        active: filters.tracker === "",
      },
      ...trackerCounts.map(([name, count]) => ({
        key: name,
        label: name,
        count,
        active: filters.tracker === name,
      })),
    ];
    return { status, source, categories, tags, trackers };
  }, [items, filters]);

  const pickNullable = (key: string) => (key === "\u0000all" ? null : key);
  const sections: {
    id: Section;
    title: string;
    entries: Entry[];
    select: (key: string) => void;
  }[] = [
    {
      id: "status",
      title: "Status",
      entries: groups.status,
      select: (key) =>
        onChange({ ...filters, status: key as Filters["status"] }),
    },
    {
      id: "source",
      title: "Source",
      entries: groups.source,
      select: (key) =>
        onChange({ ...filters, source: key as Filters["source"] }),
    },
    {
      id: "categories",
      title: "Categories",
      entries: groups.categories,
      select: (key) => onChange({ ...filters, category: pickNullable(key) }),
    },
    {
      id: "tags",
      title: "Tags",
      entries: groups.tags,
      select: (key) => onChange({ ...filters, tag: pickNullable(key) }),
    },
    {
      id: "trackers",
      title: "Trackers",
      entries: groups.trackers,
      select: (key) => onChange({ ...filters, tracker: pickNullable(key) }),
    },
  ];

  return (
    <nav className="tor-filters" aria-label="Torrent filters">
      {sections.map((section) => {
        const open = !collapsed.includes(section.id);
        const listId = `tor-filter-${section.id}`;
        return (
          <section key={section.id} className="tor-filter-section">
            <button
              type="button"
              className="tor-filter-heading"
              aria-expanded={open}
              aria-controls={listId}
              onClick={() => toggle(section.id)}
            >
              {open ? (
                <ChevronDown size={14} aria-hidden="true" />
              ) : (
                <ChevronRight size={14} aria-hidden="true" />
              )}
              <h2>{section.title}</h2>
            </button>
            {open && (
              <ul
                id={listId}
                className="tor-filter-list"
                aria-label={`${section.title} filters`}
              >
                {section.entries.map((entry) => (
                  <li key={entry.key}>
                    <button
                      type="button"
                      className={`tor-filter-item ${entry.active ? "is-active" : ""}`}
                      aria-pressed={entry.active}
                      onClick={() => section.select(entry.key)}
                    >
                      <span className="tor-filter-label" title={entry.label}>
                        {entry.label}
                      </span>
                      <span className="tor-filter-count">{entry.count}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </section>
        );
      })}
    </nav>
  );
});
