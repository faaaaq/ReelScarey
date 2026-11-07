import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { History } from "lucide-react";
import { Link } from "react-router-dom";
import { useResource } from "../useResource";
import { RequestState } from "../components";
import { Modal, ModalClose } from "../ui";
import { EventRow, type EventPage } from "./EventRow";

export function HistoryTray() {
  const [open, setOpen] = useState(false);
  const history = useResource<EventPage>(open ? "/events" : null);
  return (
    <Modal
      open={open}
      onOpenChange={setOpen}
      title="Recent activity"
      description="Newest first."
      size="small"
      trigger={
        <button
          type="button"
          className="rs-header-button"
          aria-label="Recent activity"
          title="Recent activity"
        >
          <History size={19} aria-hidden="true" />
        </button>
      }
      footer={
        <>
          <Button asChild variant="soft">
            <Link to="/timeline" onClick={() => setOpen(false)}>
              View full history
            </Link>
          </Button>
          <ModalClose>
            <Button variant="soft" color="gray">
              Close
            </Button>
          </ModalClose>
        </>
      }
    >
      <RequestState
        loading={history.loading}
        error={history.error}
        retry={history.reload}
      />
      {history.data &&
        (history.data.items.length ? (
          <div className="rs-stack" style={{ gap: 0 }}>
            {history.data.items.slice(0, 8).map((event) => (
              <EventRow
                key={event.id}
                event={event}
                onNavigate={() => setOpen(false)}
              />
            ))}
          </div>
        ) : (
          <p className="rs-muted">No events recorded yet.</p>
        ))}
    </Modal>
  );
}
