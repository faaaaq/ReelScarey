import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { EventRow, type Event, type EventPage } from "../components/EventRow";
import { api, messageFor } from "../api";
import { PageHeading } from "../components";
import { useResource } from "../useResource";

export function Timeline() {
  const first = useResource<EventPage>("/events");
  const [more, setMore] = useState<Event[]>([]);
  const [cursor, setCursor] = useState<string | null | undefined>();
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
      <PageHeading title="Event timeline" />
      <p>
        Older saved creation times and last-known states are shown where
        available. Earlier intermediate changes may be missing.
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
