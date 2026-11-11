import { useEffect, useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Activity, RefreshCw, Server, Trash2 } from "lucide-react";
import {
  api,
  messageFor,
  type MediaServerActivityResult,
  type MediaServerStatus,
} from "../api";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  EmptyState,
  FieldSet,
  FormActions,
  FormGroup,
  PageBody,
  Table,
  formatTimestamp,
} from "../ui";
import { useResource } from "../useResource";
import "./Admin.css";

type ActivityRow = MediaServerActivityResult["items"][number];

function ActivityTable({ items }: { items: ActivityRow[] }) {
  if (!items.length) {
    return (
      <p className="rs-muted">
        The server reported no recent movie or episode activity.
      </p>
    );
  }
  return (
    <Table label="Recent media-server activity">
      <thead>
        <tr>
          <th>Title</th>
          <th>Kind</th>
          <th>Played</th>
          <th>Progress</th>
          <th>Last played</th>
        </tr>
      </thead>
      <tbody>
        {items.map((item, index) => (
          <tr key={`${item.title}-${index}`}>
            <td>
              {item.series ? `${item.series} — ` : ""}
              {item.title}
              {item.season != null && item.episode != null
                ? ` (S${item.season}E${item.episode})`
                : ""}
            </td>
            <td>{item.kind}</td>
            <td>{item.play_count}</td>
            <td>{Math.round(item.progress * 100)}%</td>
            <td>
              {item.last_played
                ? formatTimestamp(Date.parse(item.last_played) / 1000)
                : "Unknown"}
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

function JellyfinPanel() {
  const status = useResource<MediaServerStatus>("/jellyfin");
  const [url, setUrl] = useState("");
  const [key, setKey] = useState("");
  const [activity, setActivity] = useState<ActivityRow[]>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    if (status.data) setUrl(status.data.url);
  }, [status.data]);

  async function save() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api("/jellyfin", {
        method: "PUT",
        body: JSON.stringify({ url: url.trim(), api_key: key.trim() }),
      });
      setKey("");
      setNotice("Jellyfin connection saved.");
      status.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function loadActivity() {
    setBusy(true);
    setError("");
    try {
      const result = await api<MediaServerActivityResult>("/jellyfin/activity");
      setActivity(result.items);
      setNotice(
        result.truncated
          ? "Showing the most recent 200 items; the server has more."
          : "Recent Jellyfin activity loaded.",
      );
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
      await api("/jellyfin/refresh", { method: "POST", body: "{}" });
      setNotice("Jellyfin library refresh requested.");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    setError("");
    try {
      await api("/jellyfin", { method: "DELETE", body: "{}" });
      setKey("");
      setActivity(undefined);
      setNotice("Jellyfin connection removed.");
      status.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <FieldSet
      legend="Jellyfin"
      description="Read recent playback and request a library refresh. The API key is encrypted and never returned."
    >
      <RequestState
        loading={status.loading}
        error={status.error}
        retry={status.reload}
      />
      {error && <Alert kind="danger">{error}</Alert>}
      {notice && <Alert kind="success">{notice}</Alert>}
      <FormGroup label="Server URL" htmlFor="jellyfin-url">
        <TextField.Root
          id="jellyfin-url"
          value={url}
          placeholder="http://jellyfin:8096"
          onChange={(event) => setUrl(event.target.value)}
        />
      </FormGroup>
      <FormGroup
        label="API key"
        htmlFor="jellyfin-key"
        help="Leave empty to keep the saved key."
      >
        <TextField.Root
          id="jellyfin-key"
          type="password"
          value={key}
          onChange={(event) => setKey(event.target.value)}
        />
      </FormGroup>
      <FormActions>
        <Button
          type="button"
          disabled={busy || !url.trim()}
          onClick={() => void save()}
        >
          Save
        </Button>
        <Button
          type="button"
          variant="soft"
          disabled={busy || !status.data?.configured}
          onClick={() => void loadActivity()}
        >
          <Activity size={16} aria-hidden="true" /> Recent activity
        </Button>
        <Button
          type="button"
          variant="soft"
          disabled={busy || !status.data?.configured}
          onClick={() => void refresh()}
        >
          <RefreshCw size={16} aria-hidden="true" /> Refresh library
        </Button>
        {status.data?.configured && (
          <Button
            type="button"
            color="red"
            variant="soft"
            disabled={busy}
            onClick={() => void remove()}
          >
            <Trash2 size={16} aria-hidden="true" /> Remove
          </Button>
        )}
      </FormActions>
      {activity && <ActivityTable items={activity} />}
    </FieldSet>
  );
}

function PlexPanel() {
  const status = useResource<MediaServerStatus>("/plex");
  const [url, setUrl] = useState("");
  const [token, setToken] = useState("");
  const [section, setSection] = useState("");
  const [activity, setActivity] = useState<ActivityRow[]>();
  const [sections, setSections] = useState<string[]>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    if (status.data) {
      setUrl(status.data.url);
      setSection(status.data.section_id ?? "");
    }
  }, [status.data]);

  async function save() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api("/plex", {
        method: "PUT",
        body: JSON.stringify({
          url: url.trim(),
          token: token.trim(),
          section_id: section.trim(),
        }),
      });
      setToken("");
      setNotice("Plex connection saved.");
      status.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function loadActivity() {
    setBusy(true);
    setError("");
    try {
      const result = await api<MediaServerActivityResult>("/plex/activity");
      setActivity(result.items);
      setNotice(
        result.truncated
          ? "Showing the most recent 200 items; the server has more."
          : "Recent Plex activity loaded.",
      );
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
      await api("/plex/refresh", { method: "POST", body: "{}" });
      setNotice("Plex library refresh requested.");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function loadSections() {
    setBusy(true);
    setError("");
    try {
      const result = await api<{ items: string[] }>("/plex/sections");
      setSections(result.items);
      setNotice(
        "Plex sections loaded. Enter the numeric section ID you want to refresh.",
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    setError("");
    try {
      await api("/plex", { method: "DELETE", body: "{}" });
      setToken("");
      setActivity(undefined);
      setSections(undefined);
      setNotice("Plex connection removed.");
      status.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <FieldSet
      legend="Plex"
      description="Read recently added or played items and refresh one library section. The token is encrypted and never returned."
    >
      <RequestState
        loading={status.loading}
        error={status.error}
        retry={status.reload}
      />
      {error && <Alert kind="danger">{error}</Alert>}
      {notice && <Alert kind="success">{notice}</Alert>}
      <FormGroup label="Server URL" htmlFor="plex-url">
        <TextField.Root
          id="plex-url"
          value={url}
          placeholder="http://plex:32400"
          onChange={(event) => setUrl(event.target.value)}
        />
      </FormGroup>
      <FormGroup
        label="X-Plex-Token"
        htmlFor="plex-token"
        help="Leave empty to keep the saved token."
      >
        <TextField.Root
          id="plex-token"
          type="password"
          value={token}
          onChange={(event) => setToken(event.target.value)}
        />
      </FormGroup>
      <FormGroup
        label="Library section ID"
        htmlFor="plex-section"
        help="Optional. Without one, Plex refreshes the whole library."
      >
        <TextField.Root
          id="plex-section"
          value={section}
          placeholder="1"
          onChange={(event) => setSection(event.target.value)}
        />
      </FormGroup>
      <FormActions>
        <Button
          type="button"
          disabled={busy || !url.trim()}
          onClick={() => void save()}
        >
          Save
        </Button>
        <Button
          type="button"
          variant="soft"
          disabled={busy || !status.data?.configured}
          onClick={() => void loadSections()}
        >
          <Server size={16} aria-hidden="true" /> List sections
        </Button>
        <Button
          type="button"
          variant="soft"
          disabled={busy || !status.data?.configured}
          onClick={() => void loadActivity()}
        >
          <Activity size={16} aria-hidden="true" /> Recent activity
        </Button>
        <Button
          type="button"
          variant="soft"
          disabled={busy || !status.data?.configured}
          onClick={() => void refresh()}
        >
          <RefreshCw size={16} aria-hidden="true" /> Refresh library
        </Button>
        {status.data?.configured && (
          <Button
            type="button"
            color="red"
            variant="soft"
            disabled={busy}
            onClick={() => void remove()}
          >
            <Trash2 size={16} aria-hidden="true" /> Remove
          </Button>
        )}
      </FormActions>
      {sections && sections.length > 0 && (
        <ul>
          {sections.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      )}
      {activity && <ActivityTable items={activity} />}
    </FieldSet>
  );
}

export function MediaServers() {
  return (
    <PageBody>
      <PageHeading
        title="Media servers"
        description="Connect Jellyfin and Plex for recent playback and explicit library refreshes. Emby connection and opt-in playback recording remain on their own settings."
      />
      <JellyfinPanel />
      <PlexPanel />
      <EmptyState icon={Server} title="No server writes">
        These integrations only read activity and call the server's own refresh
        endpoint. They never change playback state or media files.
      </EmptyState>
    </PageBody>
  );
}
