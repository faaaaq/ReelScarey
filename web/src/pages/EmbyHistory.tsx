import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";

type Status = {
  enabled: boolean;
  revision: number;
  last_success: number;
  status: string;
};
type Play = {
  id: number;
  user: string;
  title: string;
  series: string;
  started_at: number;
  last_seen: number;
  ended_at: number | null;
  state: string;
  paused: boolean;
};
export function EmbyHistory() {
  const [status, setStatus] = useState<Status>();
  const [items, setItems] = useState<Play[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function load() {
    setBusy(true);
    setError("");
    try {
      const [next, history] = await Promise.all([
        api<Status>("/emby/monitor"),
        api<{ items: Play[] }>("/emby/history"),
      ]);
      setStatus(next);
      setItems(history.items);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function toggle() {
    if (!status) return;
    setBusy(true);
    setError("");
    try {
      setStatus(
        await api<Status>("/emby/monitor", {
          method: "PUT",
          body: JSON.stringify({
            enabled: !status.enabled,
            revision: status.revision,
          }),
        }),
      );
      setItems((await api<{ items: Play[] }>("/emby/history")).items);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel system-panel">
      <h2>Playback history</h2>
      <p>
        Optionally record playback sessions observed every 30 seconds. Short
        plays can be missed, and outages or restarts may leave uncertain ends.
        Recording starts paused.
      </p>
      {!status ? (
        <Button variant="soft" onClick={() => void load()} disabled={busy}>
          Open playback history
        </Button>
      ) : (
        <>
          <p>
            Recording: {status.enabled ? "On" : "Paused"} · {status.status}
            {status.last_success > 0 &&
              ` · Last checked ${new Date(status.last_success * 1000).toLocaleString()}`}
          </p>
          <div className="import-actions">
            <Button onClick={() => void toggle()} disabled={busy}>
              {status.enabled ? "Pause recording" : "Start recording"}
            </Button>
            <Button variant="soft" onClick={() => void load()} disabled={busy}>
              Refresh history
            </Button>
          </div>
          {items.length ? (
            <div className="file-details-list">
              {items.map((p) => (
                <article className="discovery-file" key={p.id}>
                  <h3>{p.series ? `${p.series} — ${p.title}` : p.title}</h3>
                  <p>
                    {p.user} · observed{" "}
                    {new Date(p.started_at * 1000).toLocaleString()}
                  </p>
                  <p>
                    {p.state === "active"
                      ? p.paused
                        ? "Paused"
                        : "Playing"
                      : p.state === "uncertain"
                        ? "End unknown after a monitoring gap"
                        : "No longer observed"}{" "}
                    · last observed{" "}
                    {new Date(p.last_seen * 1000).toLocaleString()}
                  </p>
                </article>
              ))}
            </div>
          ) : (
            <p>No recorded playback yet.</p>
          )}
        </>
      )}
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
    </section>
  );
}
