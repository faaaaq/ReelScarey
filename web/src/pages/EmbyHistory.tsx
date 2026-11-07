import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import {
  Alert,
  FieldSet,
  Label,
  Table,
  formatRelative,
  formatTimestamp,
  type Kind,
} from "../ui";
import "./History.css";

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
function playState(p: Play): { label: string; kind: Kind } {
  if (p.state === "active")
    return p.paused
      ? { label: "Paused", kind: "disabled" }
      : { label: "Playing", kind: "success" };
  if (p.state === "uncertain")
    return { label: "End unknown after a monitoring gap", kind: "warning" };
  return { label: "No longer observed", kind: "default" };
}
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
    <FieldSet
      legend="Playback history"
      description="Records playback sessions observed every 30 seconds. Short plays can be missed and outages may leave an uncertain end. Recording starts paused."
      actions={
        status ? (
          <>
            <Button
              size="1"
              variant={status.enabled ? "soft" : "solid"}
              onClick={() => void toggle()}
              disabled={busy}
            >
              {status.enabled ? "Pause recording" : "Start recording"}
            </Button>
            <Button
              size="1"
              variant="soft"
              color="gray"
              onClick={() => void load()}
              disabled={busy}
            >
              Refresh history
            </Button>
          </>
        ) : (
          <Button
            size="1"
            variant="soft"
            onClick={() => void load()}
            disabled={busy}
          >
            Open playback history
          </Button>
        )
      }
    >
      {status && (
        <p className="hist-status">
          <Label kind={status.enabled ? "success" : "disabled"}>
            {status.enabled ? "Recording" : "Paused"}
          </Label>
          <span className="rs-muted">
            {status.status}
            {status.last_success > 0 &&
              ` · Last checked ${formatTimestamp(status.last_success)}`}
          </span>
        </p>
      )}
      {status &&
        (items.length ? (
          <Table label="Playback history" className="hist-table">
            <thead>
              <tr>
                <th scope="col">Title</th>
                <th scope="col" className="is-shrink">
                  User
                </th>
                <th scope="col">State</th>
                <th scope="col" className="is-shrink">
                  Observed
                </th>
                <th scope="col" className="is-shrink">
                  Last observed
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((p) => {
                const state = playState(p);
                return (
                  <tr key={p.id}>
                    <td className="hist-title">
                      {p.series ? `${p.series} — ${p.title}` : p.title}
                    </td>
                    <td className="hist-date">{p.user}</td>
                    <td>
                      <Label kind={state.kind}>{state.label}</Label>
                    </td>
                    <td
                      className="hist-date"
                      title={formatTimestamp(p.started_at)}
                    >
                      {formatRelative(p.started_at)}
                    </td>
                    <td
                      className="hist-date"
                      title={formatTimestamp(p.last_seen)}
                    >
                      {formatRelative(p.last_seen)}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        ) : (
          <p className="rs-muted">No recorded playback yet.</p>
        ))}
      {error && (
        <Alert kind="danger" role="alert">
          {error}
        </Alert>
      )}
    </FieldSet>
  );
}
