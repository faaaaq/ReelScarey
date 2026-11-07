import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { History as HistoryIcon, ListFilter, RefreshCw } from "lucide-react";
import {
  EventTableRow,
  typeNames,
  type Event,
  type EventPage,
} from "../components/EventRow";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  EmptyState,
  PageBody,
  PageToolbar,
  Table,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { useResource } from "../useResource";
import { ToolbarMenu } from "./activity/ToolbarMenu";
import "./History.css";

export function Timeline() {
  const first = useResource<EventPage>("/events");
  const [more, setMore] = useState<Event[]>([]);
  const [cursor, setCursor] = useState<string | null | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [type, setType] = useState("all");
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
  function refresh() {
    setMore([]);
    setCursor(undefined);
    setError("");
    first.reload();
  }
  const events = [...(first.data?.items ?? []), ...more];
  const kinds = Array.from(new Set(events.map((e) => e.kind)));
  const visible =
    type === "all" ? events : events.filter((e) => e.kind === type);
  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={first.loading}
            disabled={first.loading || busy}
            onClick={refresh}
          />
        </ToolbarSection>
        <ToolbarSection align="right">
          <ToolbarMenu
            icon={ListFilter}
            label="Filter"
            menuLabel="Event type"
            value={type}
            options={[
              { value: "all", label: "All events" },
              ...(kinds.includes(type) || type === "all"
                ? []
                : [{ value: type, label: typeNames[type] ?? type }]),
              ...kinds.map((kind) => ({
                value: kind,
                label: typeNames[kind] ?? kind,
              })),
            ]}
            onChange={setType}
            disabled={!events.length}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title="History" />
        <RequestState
          loading={first.loading}
          error={first.error}
          retry={first.reload}
        />
        {error && (
          <Alert kind="danger" role="alert">
            {error}
          </Alert>
        )}
        {first.data &&
          (events.length === 0 ? (
            <EmptyState icon={HistoryIcon} title="No events recorded yet" />
          ) : (
            <>
              {type !== "all" && (
                <p className="rs-muted rs-small">
                  Showing {visible.length} of {events.length} loaded events.
                  Load older events to search further back.
                </p>
              )}
              <Table label="History" className="hist-table">
                <thead>
                  <tr>
                    <th scope="col" className="is-shrink">
                      <span className="sr-only">Icon</span>
                    </th>
                    <th scope="col" className="is-shrink">
                      Event
                    </th>
                    <th scope="col">Title</th>
                    <th scope="col">State</th>
                    <th scope="col" className="is-shrink">
                      Date
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {visible.map((event) => (
                    <EventTableRow key={event.id} event={event} />
                  ))}
                  {visible.length === 0 && (
                    <tr>
                      <td colSpan={5} className="rs-muted">
                        No loaded events of this type.
                      </td>
                    </tr>
                  )}
                </tbody>
              </Table>
            </>
          ))}
        {next && (
          <div className="rs-pagination">
            <Button
              variant="soft"
              disabled={busy}
              onClick={() => void loadMore()}
            >
              Load older events
            </Button>
          </div>
        )}
        {first.data && events.length > 0 && (
          <p className="rs-muted rs-small">
            Older records may only show their creation time and last-known
            state; earlier intermediate changes can be missing.
          </p>
        )}
      </PageBody>
    </>
  );
}
