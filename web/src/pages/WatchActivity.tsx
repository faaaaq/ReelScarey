import { useEffect, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import { EmbyHistory } from "./EmbyHistory";

type Settings = { url: string; configured: boolean };
type Activity = {
  user: string;
  title: string;
  series: string;
  kind: string;
  season: number | null;
  episode: number | null;
  last_played: string;
  play_count: number;
  played: boolean;
  progress: number;
};
type Result = { items: Activity[]; truncated: boolean; checked_at: string };

export function WatchActivity() {
  const settings = useResource<Settings>("/emby");
  const [url, setURL] = useState("");
  const [key, setKey] = useState("");
  const [result, setResult] = useState<Result>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (settings.data) setURL(settings.data.url);
  }, [settings.data]);
  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/emby", {
        method: "PUT",
        body: JSON.stringify({ url, api_key: key }),
      });
      setKey("");
      setResult(undefined);
      settings.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function refresh() {
    setBusy(true);
    setError("");
    try {
      setResult(await api<Result>("/emby/activity"));
    } catch (e) {
      setResult(undefined);
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    setBusy(true);
    setError("");
    try {
      await api("/emby", { method: "DELETE", body: "{}" });
      setURL("");
      setKey("");
      setResult(undefined);
      settings.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeading
        title="Emby watch activity"
        description="See the latest playback state reported by your Emby server."
      />
      <RequestState
        loading={settings.loading}
        error={settings.error}
        retry={settings.reload}
      />
      <EmbyHistory />
      <section className="panel system-panel">
        <h2>Emby connection</h2>
        <p>
          Enter the server address reachable from the ReelScarey container and
          an Emby API key. The key is stored encrypted and is never shown again.
        </p>
        <form onSubmit={save}>
          <label>
            Server URL
            <TextField.Root
              value={url}
              onChange={(e) => setURL(e.target.value)}
              placeholder="http://emby:8096/emby"
              required
            />
          </label>
          <label>
            API key
            <TextField.Root
              type="password"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder={
                settings.data?.configured
                  ? "Leave blank to keep the saved key"
                  : "Emby API key"
              }
            />
          </label>
          <div className="import-actions">
            <Button disabled={busy}>Save connection</Button>
            {settings.data?.configured && (
              <Button
                type="button"
                variant="soft"
                disabled={busy}
                onClick={() => void remove()}
              >
                Remove connection
              </Button>
            )}
          </div>
        </form>
      </section>
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
      <section className="panel system-panel">
        <h2>Recently watched</h2>
        <p>
          Shows the most recent playback for each movie or episode and user,
          including play counts and progress reported by Emby. This is a
          snapshot, not a complete play-by-play history.
        </p>
        <Button
          disabled={busy || !settings.data?.configured}
          onClick={() => void refresh()}
        >
          {busy ? "Working…" : "Refresh activity"}
        </Button>
        {result && (
          <>
            <p>
              Checked {new Date(result.checked_at).toLocaleString()}.
              {result.truncated &&
                " Results are limited to the first 20 users and 100 items per user."}
            </p>
            {result.items.length ? (
              <div className="file-details-list">
                {result.items.map((item, index) => (
                  <article
                    key={`${item.user}-${item.title}-${index}`}
                    className="discovery-file"
                  >
                    <h3>
                      {item.series
                        ? `${item.series} — ${item.title}`
                        : item.title}
                    </h3>
                    <p>
                      {item.user} ·{" "}
                      {new Date(item.last_played).toLocaleString()} ·{" "}
                      {item.play_count} plays
                      {item.season !== null && item.episode !== null
                        ? ` · S${item.season} E${item.episode}`
                        : ""}
                    </p>
                    <p>
                      {item.played
                        ? "Marked played"
                        : `In progress (${Math.round(item.progress || 0)}%)`}
                    </p>
                  </article>
                ))}
              </div>
            ) : (
              <p>No recent playback was reported by Emby.</p>
            )}
          </>
        )}
      </section>
    </>
  );
}
