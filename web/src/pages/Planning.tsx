import { useEffect, useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import {
  ChevronLeft,
  ChevronRight,
  Film,
  RefreshCw,
  Search,
  Tv,
} from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { PageHeading, RequestState } from "../components";
import { Pagination } from "../movieComponents";
import { useResource } from "../useResource";

export type PlanningEntry = {
  kind: "movie" | "series";
  media_id: number;
  episode_id: number | null;
  title: string;
  year: number | null;
  episode_title: string;
  season: number | null;
  episode: number | null;
  date: string;
  monitored: boolean;
  monitoring_reason: string;
  available: boolean;
  status: string;
  download_id: string;
  download_state: string;
  protocol: string;
  poster_url: string;
};
export type PlanningData = {
  items: PlanningEntry[];
  counts: Record<string, number>;
  coverage: { unloaded_seasons: number; undated: number };
  total: number;
  page: number;
  limit: number;
  as_of_date: string;
  month: string;
  kind: string;
  status: string;
  query: string;
  monitored_only: boolean;
};
const labels: Record<string, string> = {
  missing: "Missing",
  upcoming: "Upcoming",
  undated: "No date",
  downloading: "In progress",
  attention: "Needs attention",
  available: "In library",
};
const wantedStatuses = [
  "missing",
  "downloading",
  "attention",
  "upcoming",
  "undated",
];
function dateLabel(value: string, month = false) {
  const date = new Date(`${value}${month ? "-01" : ""}T12:00:00Z`);
  if (!Number.isFinite(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    timeZone: "UTC",
    ...(month
      ? { month: "long", year: "numeric" }
      : { month: "short", day: "numeric", year: "numeric" }),
  }).format(date);
}
function shiftedMonth(month: string, step: number) {
  if (!/^\d{4}-\d{2}$/.test(month)) return "";
  const value = new Date(`${month}-01T12:00:00Z`);
  value.setUTCMonth(value.getUTCMonth() + step);
  if (
    !Number.isFinite(value.getTime()) ||
    value.getUTCFullYear() < 1 ||
    value.getUTCFullYear() > 9999
  )
    return "";
  return value.toISOString().slice(0, 7);
}
export function Planning({ calendar = false }: { calendar?: boolean }) {
  const [params, setParams] = useSearchParams();
  const [input, setInput] = useState(params.get("query") ?? "");
  useEffect(() => setInput(params.get("query") ?? ""), [params]);
  const base = calendar ? "/calendar" : "/wanted";
  const resource = useResource<PlanningData>(`${base}?${params.toString()}`);
  const data = resource.data;
  const status = params.get("status") || (calendar ? "all" : "missing");
  const statuses = calendar
    ? [...wantedStatuses.filter((v) => v !== "undated"), "available"]
    : wantedStatuses;
  function change(key: string, value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    next.delete("page");
    setParams(next);
  }
  function submit(e: FormEvent) {
    e.preventDefault();
    change("query", input.trim());
  }
  const groups = new Map<string, PlanningEntry[]>();
  for (const item of data?.items ?? []) {
    const group = calendar ? item.date : "wanted";
    groups.set(group, [...(groups.get(group) ?? []), item]);
  }
  const previous = shiftedMonth(data?.month ?? "", -1),
    next = shiftedMonth(data?.month ?? "", 1);
  const undated = new URLSearchParams(params);
  undated.delete("month");
  undated.delete("page");
  undated.set("status", "undated");
  return (
    <>
      <PageHeading
        title={calendar ? "Release calendar" : "Wanted"}
        description={
          calendar
            ? "Saved movie releases and loaded TV episodes, month by month."
            : "Missing movies and episodes, with dates and download status in one place."
        }
      >
        <Button asChild variant="soft">
          <Link to={calendar ? "/wanted" : "/calendar"}>
            {calendar ? "View wanted" : "View calendar"}
          </Link>
        </Button>
        <Button
          variant="soft"
          onClick={resource.reload}
          disabled={resource.loading}
        >
          <RefreshCw size={16} />
          Refresh
        </Button>
      </PageHeading>
      <div className="planning-filters panel">
        <form className="planning-search" onSubmit={submit}>
          <TextField.Root
            aria-label="Find in saved catalog"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            maxLength={200}
            placeholder="Find a title or episode…"
          />
          <Button type="submit">
            <Search size={16} />
            Filter titles
          </Button>
        </form>
        <div className="planning-selects">
          <label>
            Media type
            <select
              aria-label="Media type"
              value={params.get("kind") ?? "all"}
              onChange={(e) => change("kind", e.target.value)}
            >
              <option value="all">Movies & TV</option>
              <option value="movie">Movies</option>
              <option value="series">TV episodes</option>
            </select>
          </label>
          <label>
            Status
            <select
              aria-label="Planning status"
              value={status}
              onChange={(e) => change("status", e.target.value)}
            >
              <option value="all">
                {calendar ? "All statuses" : "All not in library"}
              </option>
              {statuses.map((v) => (
                <option value={v} key={v}>
                  {labels[v]}
                </option>
              ))}
            </select>
          </label>
          <label>
            Monitoring
            <select
              aria-label="Monitoring filter"
              value={params.get("monitored") ?? "monitored"}
              onChange={(e) => change("monitored", e.target.value)}
            >
              <option value="monitored">Monitored only</option>
              <option value="all">Include paused</option>
            </select>
          </label>
          <Link to={base} className="text-link">
            Reset filters
          </Link>
        </div>
      </div>
      {calendar && (
        <section className="planning-month panel" aria-label="Month navigation">
          <Button
            variant="soft"
            aria-label="Previous month"
            disabled={!previous || resource.loading}
            onClick={() => change("month", previous)}
          >
            <ChevronLeft size={18} />
          </Button>
          <div>
            <h2>
              {data ? dateLabel(data.month, true) : "Saved release dates"}
            </h2>
            <label>
              Choose month
              <input
                aria-label="Calendar month"
                type="month"
                min="0001-01"
                max="9999-12"
                value={params.get("month") ?? data?.month ?? ""}
                onChange={(e) => change("month", e.target.value)}
              />
            </label>
          </div>
          <Button
            variant="soft"
            aria-label="Next month"
            disabled={!next || resource.loading}
            onClick={() => change("month", next)}
          >
            <ChevronRight size={18} />
          </Button>
          <Button
            variant="ghost"
            disabled={!data || data.month === data.as_of_date.slice(0, 7)}
            onClick={() => change("month", data!.as_of_date.slice(0, 7))}
          >
            Current month
          </Button>
        </section>
      )}
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {data && (
        <>
          <div className="planning-counts" aria-label="Planning counts">
            {statuses.map((key) => (
              <button
                key={key}
                className={`panel ${status === key ? "is-selected" : ""}`}
                aria-pressed={status === key}
                aria-label={`${labels[key]}: ${data.counts[key] ?? 0}`}
                onClick={() => change("status", key)}
              >
                <span>{labels[key]}</span>
                <strong>{data.counts[key] ?? 0}</strong>
              </button>
            ))}
          </div>
          <div className="planning-notes">
            <p>
              Based on your saved catalog, as of {dateLabel(data.as_of_date)}{" "}
              (UTC). Availability reflects registered files and tracked jobs.
            </p>
            {calendar && (
              <p>
                Movie dates may be theatrical releases. These dates do not
                establish download availability or broadcast times.
              </p>
            )}
            {data.coverage.unloaded_seasons > 0 && (
              <p>
                {data.coverage.unloaded_seasons}{" "}
                {data.coverage.unloaded_seasons === 1
                  ? "season needs"
                  : "seasons need"}{" "}
                episode metadata. Load episodes from{" "}
                <Link to="/series" className="text-link">
                  TV shows
                </Link>{" "}
                to include them.
              </p>
            )}
            {calendar && data.coverage.undated > 0 && (
              <p>
                {data.coverage.undated} missing{" "}
                {data.coverage.undated === 1 ? "item has" : "items have"} no
                valid date and {data.coverage.undated === 1 ? "is" : "are"}{" "}
                outside the calendar.{" "}
                <Link
                  to={`/wanted?${undated.toString()}`}
                  className="text-link"
                >
                  Review undated items
                </Link>
              </p>
            )}
          </div>
          <p className="search-result-count">
            {data.total} matching {data.total === 1 ? "item" : "items"}
            {data.total > 0
              ? ` · ${Math.min((data.page - 1) * data.limit + 1, data.total)}–${Math.min(data.page * data.limit, data.total)} shown`
              : ""}
            .
          </p>
          {data.items.length === 0 ? (
            <section className="panel search-intro">
              <h2>
                {data.total > 0
                  ? "This page has no items."
                  : calendar
                    ? "No saved releases match this month."
                    : "No items match these filters."}
              </h2>
              <p>
                {data.total > 0
                  ? "Return to the first page to see the current results."
                  : "Try another status, include paused titles, or add titles and load TV episodes."}
              </p>
              {data.total > 0 ? (
                <Button
                  onClick={() => {
                    const p = new URLSearchParams(params);
                    p.delete("page");
                    setParams(p);
                  }}
                >
                  First page
                </Button>
              ) : (
                <Link className="text-link" to="/search">
                  Add movies or shows
                </Link>
              )}
            </section>
          ) : (
            <div className="planning-results">
              {Array.from(groups, ([day, entries]) => (
                <section
                  className="planning-day"
                  key={day}
                  aria-label={calendar ? dateLabel(day) : "Wanted results"}
                >
                  {calendar && (
                    <h2 className="planning-day-heading">
                      <time dateTime={day}>{dateLabel(day)}</time>
                    </h2>
                  )}
                  <div className="planning-items">
                    {entries.map((entry) => (
                      <PlanningCard
                        key={`${entry.kind}-${entry.media_id}-${entry.episode_id ?? "movie"}`}
                        entry={entry}
                        calendar={calendar}
                      />
                    ))}
                  </div>
                </section>
              ))}
            </div>
          )}
          <Pagination
            page={data.page}
            pages={Math.ceil(data.total / data.limit)}
            onPage={(page) => {
              const p = new URLSearchParams(params);
              p.set("page", String(page));
              setParams(p);
            }}
          />
        </>
      )}
    </>
  );
}
function PlanningCard({
  entry,
  calendar,
}: {
  entry: PlanningEntry;
  calendar: boolean;
}) {
  const tv = entry.kind === "series";
  const Heading = calendar ? "h3" : "h2";
  const path = tv ? `/series/${entry.media_id}` : `/movies/${entry.media_id}`;
  const release = tv
    ? `/releases?series=${entry.media_id}&season=${entry.season}&episode=${entry.episode}`
    : `/releases?movie=${entry.media_id}`;
  const code = tv
    ? `S${String(entry.season).padStart(2, "0")}E${String(entry.episode).padStart(2, "0")}`
    : "Movie";
  return (
    <article className="panel planning-row">
      <Link
        to={path}
        className="planning-poster"
        aria-label={`Open ${entry.title} details`}
      >
        {entry.poster_url ? (
          <img src={entry.poster_url} alt="" loading="lazy" />
        ) : tv ? (
          <Tv size={25} />
        ) : (
          <Film size={25} />
        )}
      </Link>
      <div className="planning-copy">
        <Heading>
          <Link to={path}>{entry.title}</Link>
          {entry.year ? <span> ({entry.year})</span> : null}
        </Heading>
        <p>
          {code}
          {tv && entry.episode_title ? ` · ${entry.episode_title}` : ""}
        </p>
        <div className="planning-row-meta">
          <Badge
            color={
              entry.status === "available"
                ? "green"
                : entry.status === "attention"
                  ? "orange"
                  : "gray"
            }
          >
            {labels[entry.status] ?? entry.status}
          </Badge>
          {!entry.monitored && <Badge color="gray">Not monitored</Badge>}
          {!calendar && (
            <span>
              {entry.date ? (
                <time dateTime={entry.date}>{dateLabel(entry.date)}</time>
              ) : (
                "Date unavailable"
              )}
            </span>
          )}
          {entry.download_id && (
            <span>
              {entry.protocol === "usenet" ? "SABnzbd" : "qBittorrent"} ·{" "}
              {entry.download_state.replaceAll("_", " ")}
            </span>
          )}
        </div>
        {entry.monitoring_reason && (
          <p className="field-help">{entry.monitoring_reason}</p>
        )}
      </div>
      <div className="planning-row-actions">
        {entry.download_id ? (
          <Button asChild variant="soft">
            <Link to={`/activity?job=${encodeURIComponent(entry.download_id)}`}>
              View activity
            </Link>
          </Button>
        ) : !entry.available ? (
          <Button asChild variant="soft">
            <Link to={release}>Find releases</Link>
          </Button>
        ) : null}
        <Link className="text-link" to={path}>
          {tv ? "View show" : "View movie"}
        </Link>
      </div>
    </article>
  );
}
