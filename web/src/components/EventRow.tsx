import { Link } from "react-router-dom";

export type Event = {
  id: number;
  occurred_at: number;
  kind: string;
  object_id: string;
  title: string;
  state: string;
};
export type EventPage = { items: Event[]; next: string | null };
const typeNames: Record<string, string> = {
  title: "Title",
  download: "Download",
  youtube: "YouTube",
  request: "Request",
  organization: "Organization",
  subtitle: "Subtitle",
  playback: "Emby playback",
  automation: "Automation",
  scan: "Library scan",
  related: "Related titles",
  series_rules: "Show settings",
  request_account: "Request account",
  request_list: "Request list",
  connection: "Connection",
  indexer: "Indexer",
  settings: "Settings",
};
export function EventRow({
  event,
  onNavigate,
}: {
  event: Event;
  onNavigate?: () => void;
}) {
  const paths: Record<string, string> = {
    download: `/activity?job=${encodeURIComponent(event.object_id)}`,
    youtube: "/youtube",
    request: "/requests",
    organization: "/organization",
    subtitle: "/subtitles",
    playback: "/watch-activity",
    automation: "/automation",
    scan: `/import?scan=${encodeURIComponent(event.object_id)}`,
    series_rules: `/series/${encodeURIComponent(event.object_id)}`,
    request_account: "/requests",
    request_list: "/requests",
    connection: "/settings",
    indexer: "/indexers",
    settings: "/settings",
  };
  const dest = paths[event.kind];
  return (
    <article className="discovery-file">
      <strong>
        {typeNames[event.kind] ?? event.kind} ·{" "}
        {event.state.replaceAll("_", " ")}
      </strong>
      <p>
        {dest ? (
          <Link to={dest} onClick={onNavigate}>
            {event.title}
          </Link>
        ) : (
          event.title
        )}
      </p>
      <p>{new Date(event.occurred_at * 1000).toLocaleString()}</p>
    </article>
  );
}
