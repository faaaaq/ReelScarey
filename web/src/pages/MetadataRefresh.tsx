import { PageBody } from "../ui";
import { useState, type FormEvent } from "react";
import { Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useLiveResource } from "../useLiveResource";
import "./Admin.css";
type Schedule = {
  enabled: boolean;
  interval_hours: number;
  batch_size: number;
  only_monitored: boolean;
  revision: number;
  requested: boolean;
  next_run: number;
  last_run: number;
  state: string;
  message: string;
  cursor: number;
};
export function MetadataRefresh() {
  const resource = useLiveResource<Schedule>("/system/metadata-refresh", 5000);
  return (
    <PageBody>
      <PageHeading title="Metadata refresh" />
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {resource.data && (
        <RefreshForm
          key={resource.data.revision}
          initial={resource.data}
          reload={resource.reload}
        />
      )}
    </PageBody>
  );
}
function RefreshForm({
  initial,
  reload,
}: {
  initial: Schedule;
  reload: () => void;
}) {
  const [form, setForm] = useState(initial),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  async function action(run = false) {
    setBusy(true);
    setError("");
    try {
      if (run) {
        await api("/system/metadata-refresh/run", {
          method: "POST",
          body: "{}",
        });
        setNotice("Metadata refresh queued.");
      } else {
        await api("/system/metadata-refresh", {
          method: "PUT",
          body: JSON.stringify({
            enabled: form.enabled,
            interval_hours: form.interval_hours,
            batch_size: form.batch_size,
            only_monitored: form.only_monitored,
            revision: form.revision,
          }),
        });
        setNotice("Metadata schedule saved.");
      }
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <form
        className="rs-panel rs-stack"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          void action();
        }}
      >
        <h2>Refresh schedule</h2>
        <fieldset className="backup-fields" disabled={busy}>
          <label>
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
            />{" "}
            Enable scheduled metadata refresh
          </label>
          <div className="backup-grid">
            <label>
              Every (hours)
              <input
                type="number"
                required
                min={1}
                max={720}
                value={form.interval_hours}
                onChange={(e) =>
                  setForm({ ...form, interval_hours: Number(e.target.value) })
                }
              />
            </label>
            <label>
              Titles per batch
              <input
                type="number"
                required
                min={1}
                max={25}
                value={form.batch_size}
                onChange={(e) =>
                  setForm({ ...form, batch_size: Number(e.target.value) })
                }
              />
            </label>
          </div>
          <label>
            <input
              type="checkbox"
              checked={form.only_monitored}
              onChange={(e) =>
                setForm({ ...form, only_monitored: e.target.checked })
              }
            />{" "}
            Only monitored titles
          </label>
          <Button type="submit">Save refresh schedule</Button>{" "}
          <Button
            type="button"
            variant="soft"
            disabled={initial.requested || initial.state === "running"}
            onClick={() => void action(true)}
          >
            Refresh now
          </Button>
        </fieldset>
        {error && (
          <p role="alert" className="rs-alert is-danger">
            {error}
          </p>
        )}
        {notice && <p role="status">{notice}</p>}
      </form>
      <section className="rs-panel rs-stack">
        <h2>Current status</h2>
        <p>{initial.message}</p>
        <p>
          Last batch:{" "}
          {initial.last_run
            ? new Date(initial.last_run * 1000).toLocaleString()
            : "Never"}
          . Next:{" "}
          {initial.enabled
            ? new Date(initial.next_run * 1000).toLocaleString()
            : "Paused"}
          .
        </p>
        <p>
          TMDb must be connected in <Link to="/settings#tmdb">Settings</Link>.
          Each batch refreshes at most five seasons per show, rotating through
          loaded seasons and monitored seasons due within 90 days. Provider
          identity changes are held for review; saved episodes and files are
          retained. Newly added, registered and imported titles also enter a
          durable refresh queue automatically, and each library page has a
          refresh action for every title in that collection.
        </p>
      </section>
    </>
  );
}
