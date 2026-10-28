import { useState } from "react";
import { Button, Dialog } from "@radix-ui/themes";
import { Clock3 } from "lucide-react";
import { Link } from "react-router-dom";
import { useResource } from "../useResource";
import { RequestState } from "../components";
import { EventRow, type EventPage } from "../pages/Timeline";
export function HistoryTray() {
  const [open, setOpen] = useState(false);
  const history = useResource<EventPage>(open ? "/events" : null);
  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Trigger>
        <Button variant="soft">
          <Clock3 size={16} /> Recent activity
        </Button>
      </Dialog.Trigger>
      <Dialog.Content maxWidth="420px">
        <Dialog.Title>Recent activity</Dialog.Title>
        <Dialog.Description>
          Recent recorded changes across your library and connected services.
        </Dialog.Description>
        <RequestState
          loading={history.loading}
          error={history.error}
          retry={history.reload}
        />
        {history.data &&
          (history.data.items.length ? (
            <div className="file-details-list">
              {history.data.items.slice(0, 8).map((event) => (
                <EventRow
                  key={event.id}
                  event={event}
                  onNavigate={() => setOpen(false)}
                />
              ))}
            </div>
          ) : (
            <p>No events recorded yet.</p>
          ))}
        <div className="import-actions">
          <Button asChild variant="soft">
            <Link to="/timeline" onClick={() => setOpen(false)}>
              View event timeline
            </Link>
          </Button>
          <Dialog.Close>
            <Button variant="ghost">Close</Button>
          </Dialog.Close>
        </div>
      </Dialog.Content>
    </Dialog.Root>
  );
}
