import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { PageHeading } from "../components";
import { useResource } from "../useResource";

export type Event = {
  id: number;
  occurred_at: number;
  kind: string;
  object_id: string;
  title: string;
  state: string;
};
export type EventPage = { items: Event[]; next: number | null };
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
export function Timeline() {
  const first = useResource<EventPage>("/events");
  const [more, setMore] = useState<Event[]>([]);
  const [cursor, setCursor] = useState<number | null | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const next = cursor === undefined ? first.data?.next : cursor;
  async function loadMore() {
    if (!next) return;
    setBusy(true);
    try {
      const page = await api<EventPage>(`/events?before=${next}`);
      setMore((current) => [...current, ...page.items]);
      setCursor(page.next);
      setError("");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeading
        title="Event timeline"
        description="Recorded changes across downloads, requests, playback and library tasks."
      />
      <p>
        Events are recorded from the version that introduced this timeline;
        earlier actions are not reconstructed.
      </p>
      {first.loading && <p>Loading events…</p>}
      {(first.error || error) && (
        <p role="alert" className="error-box">
          {first.error || error}
        </p>
      )}
      <section className="panel system-panel">
        {first.data?.items.length || more.length ? (
          <div className="file-details-list">
            {[...(first.data?.items ?? []), ...more].map((event) => (
              <EventRow key={event.id} event={event} />
            ))}
          </div>
        ) : (
          !first.loading && <p>No events recorded yet.</p>
        )}
        {next && (
          <Button
            variant="soft"
            disabled={busy}
            onClick={() => void loadMore()}
          >
            Load older events
          </Button>
        )}
      </section>
    </>
  );
}
