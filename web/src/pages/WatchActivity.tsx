import { useEffect, useId, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { RefreshCw } from "lucide-react";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  EmptyState,
  FieldSet,
  FormActions,
  FormGroup,
  Label,
  PageBody,
  PageToolbar,
  Table,
  ToolbarButton,
  ToolbarSection,
  episodeCode,
} from "../ui";
import { useResource } from "../useResource";
import { EmbyHistory } from "./EmbyHistory";
import "./History.css";

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
  const urlID = useId();
  const keyID = useId();
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
  const configured = !!settings.data?.configured;
  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh activity"
            spinning={busy && configured}
            disabled={busy || !configured}
            title={configured ? undefined : "Connect Emby first"}
            onClick={() => void refresh()}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title="Playback" />
        <RequestState
          loading={settings.loading}
          error={settings.error}
          retry={settings.reload}
        />
        {error && (
          <Alert kind="danger" role="alert">
            {error}
          </Alert>
        )}
        <FieldSet
          legend="Recently watched"
          description="Emby's latest playback per movie or episode and user, with play counts and progress. This is a snapshot, not a complete play-by-play history."
        >
          {!configured && settings.data && (
            <p className="rs-muted">
              Connect Emby below to read its recent playback.
            </p>
          )}
          {configured && !result && (
            <p className="rs-muted">
              Use Refresh activity to ask Emby for its latest playback.
            </p>
          )}
          {result && (
            <>
              <p className="rs-muted rs-small">
                Checked {new Date(result.checked_at).toLocaleString()}.
                {result.truncated &&
                  " Results are limited to the first 20 users and 100 items per user."}
              </p>
              {result.items.length ? (
                <Table label="Recently watched" className="hist-table">
                  <thead>
                    <tr>
                      <th scope="col">Title</th>
                      <th scope="col" className="is-shrink">
                        Episode
                      </th>
                      <th scope="col" className="is-shrink">
                        User
                      </th>
                      <th scope="col" className="is-shrink">
                        Last played
                      </th>
                      <th scope="col" className="is-numeric">
                        Plays
                      </th>
                      <th scope="col">Progress</th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.items.map((item, index) => (
                      <tr key={`${item.user}-${item.title}-${index}`}>
                        <td className="hist-title">
                          {item.series
                            ? `${item.series} — ${item.title}`
                            : item.title}
                        </td>
                        <td className="hist-date">
                          {item.season !== null && item.episode !== null
                            ? episodeCode(item.season, item.episode)
                            : "—"}
                        </td>
                        <td className="hist-date">{item.user}</td>
                        <td className="hist-date">
                          {new Date(item.last_played).toLocaleString()}
                        </td>
                        <td className="is-numeric">{item.play_count}</td>
                        <td>
                          {item.played ? (
                            <Label kind="success">Marked played</Label>
                          ) : (
                            <Label kind="info">
                              {`In progress (${Math.round(item.progress || 0)}%)`}
                            </Label>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              ) : (
                <EmptyState title="No recent playback">
                  <p>Emby did not report any recent playback.</p>
                </EmptyState>
              )}
            </>
          )}
        </FieldSet>
        <EmbyHistory />
        <FieldSet
          legend="Emby connection"
          description="Server URL and an Emby API key. The key is stored encrypted."
        >
          <form onSubmit={save} className="rs-stack">
            <FormGroup label="Server URL" htmlFor={urlID}>
              <TextField.Root
                id={urlID}
                value={url}
                onChange={(e) => setURL(e.target.value)}
                placeholder="http://emby:8096/emby"
                required
              />
            </FormGroup>
            <FormGroup
              label="API key"
              htmlFor={keyID}
              help={
                configured ? "Leave blank to keep the saved key." : undefined
              }
            >
              <TextField.Root
                id={keyID}
                type="password"
                value={key}
                onChange={(e) => setKey(e.target.value)}
                placeholder={configured ? "Saved key retained" : "Emby API key"}
              />
            </FormGroup>
            <FormActions>
              <Button type="submit" disabled={busy}>
                Save connection
              </Button>
              {configured && (
                <Button
                  type="button"
                  variant="soft"
                  color="red"
                  disabled={busy}
                  onClick={() => void remove()}
                >
                  Remove connection
                </Button>
              )}
            </FormActions>
          </form>
        </FieldSet>
      </PageBody>
    </>
  );
}
