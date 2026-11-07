import {
  Bot,
  Captions,
  Download,
  Film,
  FolderTree,
  Inbox,
  List,
  Play,
  Plug,
  Rss,
  ScanSearch,
  Settings,
  SlidersHorizontal,
  Users,
  Video,
  type LucideIcon,
} from "lucide-react";
import { Link } from "react-router-dom";
import { Label, formatRelative, formatTimestamp, type Kind } from "../ui";
import "../pages/History.css";

export type Event = {
  id: number;
  occurred_at: number;
  kind: string;
  object_id: string;
  title: string;
  state: string;
};
export type EventPage = { items: Event[]; next: string | null };
export const typeNames: Record<string, string> = {
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
const icons: Record<string, LucideIcon> = {
  title: Film,
  download: Download,
  youtube: Video,
  request: Inbox,
  organization: FolderTree,
  subtitle: Captions,
  playback: Play,
  automation: Bot,
  scan: ScanSearch,
  related: List,
  series_rules: SlidersHorizontal,
  request_account: Users,
  request_list: List,
  connection: Plug,
  indexer: Rss,
  settings: Settings,
};
export function eventPath(event: Event): string | undefined {
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
  return paths[event.kind];
}
export function eventStateText(event: Event) {
  return event.state.replaceAll("_", " ");
}
/** Status color from the recorded state text; unknown states stay neutral. */
export function eventStateKind(state: string): Kind {
  const s = state.toLowerCase();
  if (/fail|error|reject|denied|invalid/.test(s)) return "danger";
  if (/block|review|attention|uncertain|warning|stale/.test(s))
    return "warning";
  if (/complet|import|approv|available|success|verified|saved|done/.test(s))
    return "success";
  if (/download|queue|submit|running|pending|active|searching/.test(s))
    return "info";
  if (/cancel|stop|paus|disabled|removed/.test(s)) return "disabled";
  return "default";
}
function isoTime(seconds: number) {
  const date = new Date(seconds * 1000);
  return Number.isFinite(date.getTime()) ? date.toISOString() : undefined;
}
export function EventIcon({ kind }: { kind: string }) {
  const Icon = icons[kind] ?? List;
  return <Icon size={16} aria-hidden="true" />;
}

/** Compact event entry used by the header's recent activity tray. */
export function EventRow({
  event,
  onNavigate,
}: {
  event: Event;
  onNavigate?: () => void;
}) {
  const dest = eventPath(event);
  return (
    <article className="hist-event">
      <span className="hist-event-icon">
        <EventIcon kind={event.kind} />
      </span>
      <div className="hist-event-body">
        <strong className="hist-event-type">
          {typeNames[event.kind] ?? event.kind} · {eventStateText(event)}
        </strong>
        <span className="hist-event-title">
          {dest ? (
            <Link to={dest} onClick={onNavigate}>
              {event.title}
            </Link>
          ) : (
            event.title
          )}
        </span>
      </div>
      <time
        className="hist-event-time"
        dateTime={isoTime(event.occurred_at)}
        title={formatTimestamp(event.occurred_at)}
      >
        {formatRelative(event.occurred_at)}
      </time>
    </article>
  );
}

/** Table row for the full History page. */
export function EventTableRow({ event }: { event: Event }) {
  const dest = eventPath(event);
  return (
    <tr>
      <td className="hist-icon">
        <EventIcon kind={event.kind} />
      </td>
      <td className="hist-type">{typeNames[event.kind] ?? event.kind}</td>
      <td className="hist-title">
        {dest ? <Link to={dest}>{event.title}</Link> : event.title}
      </td>
      <td>
        <Label kind={eventStateKind(event.state)}>
          {eventStateText(event)}
        </Label>
      </td>
      <td className="hist-date" title={formatTimestamp(event.occurred_at)}>
        <time dateTime={isoTime(event.occurred_at)}>
          {formatRelative(event.occurred_at)}
        </time>
      </td>
    </tr>
  );
}
