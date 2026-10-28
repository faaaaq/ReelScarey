import { useState } from "react";
import { Button, Dialog } from "@radix-ui/themes";
import { Clock3 } from "lucide-react";
import { Link } from "react-router-dom";
import type { DownloadPage } from "../api";
import { useResource } from "../useResource";
import { RequestState } from "../components";

const states: Record<string, string> = {
  queued: "Queued",
  submitting: "Sending to download client",
  downloading: "Downloading",
  importing: "Importing",
  blocked: "Needs attention",
  failed: "Retry needed",
  completed: "In library",
  cancelled: "Tracking stopped",
};
export function HistoryTray() {
  const [open, setOpen] = useState(false);
  const history = useResource<DownloadPage>(open ? "/downloads?page=1" : null);
  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Trigger>
        <Button variant="soft">
          <Clock3 size={16} /> Recent activity
        </Button>
      </Dialog.Trigger>
      <Dialog.Content maxWidth="420px">
        <Dialog.Title>Recent download activity</Dialog.Title>
        <Dialog.Description>
          Latest status of downloads started in ReelScarey. Open Activity for
          details and recovery actions.
        </Dialog.Description>
        <RequestState
          loading={history.loading}
          error={history.error}
          retry={history.reload}
        />
        {history.data &&
          (history.data.items.length ? (
            <div className="file-details-list">
              {history.data.items.slice(0, 8).map((job) => (
                <article className="discovery-file" key={job.id}>
                  <strong>{states[job.state] ?? job.state}</strong>
                  <p>
                    <Link
                      to={`/activity?job=${job.id}`}
                      onClick={() => setOpen(false)}
                    >
                      {job.movie_title || job.title}
                    </Link>
                  </p>
                  <p>{new Date(job.updated_at * 1000).toLocaleString()}</p>
                </article>
              ))}
            </div>
          ) : (
            <p>No downloads yet.</p>
          ))}
        <div className="import-actions">
          <Button asChild variant="soft">
            <Link to="/activity" onClick={() => setOpen(false)}>
              View all activity
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
