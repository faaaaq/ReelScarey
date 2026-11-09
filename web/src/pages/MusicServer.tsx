import { useEffect, useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { ArrowLeft, KeyRound, Server, ShieldCheck, Trash2 } from "lucide-react";
import { Link } from "react-router-dom";
import {
  Alert,
  FieldSet,
  Label,
  PageBody,
  PageToolbar,
  ToolbarButton,
  ToolbarSection,
} from "../ui";
import { PageHeading, RequestState } from "../components";
import { api, messageFor, type MusicServerStatus } from "../api";
import { useResource } from "../useResource";

export function MusicServer() {
  const server = useResource<MusicServerStatus>("/library/music/server");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [token, setToken] = useState("");
  const [enabled, setEnabled] = useState(false);

  useEffect(() => {
    if (server.data) setEnabled(server.data.enabled);
  }, [server.data]);

  async function saveEnabled(value: boolean) {
    if (!server.data || pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      await api("/library/music/server", {
        method: "PUT",
        body: JSON.stringify({
          enabled: value,
          revision: server.data.revision,
        }),
      });
      setEnabled(value);
      setMessage(
        value
          ? "The Subsonic-compatible endpoint is enabled on the existing ReelScarey port."
          : "The Subsonic-compatible endpoint is disabled.",
      );
      server.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function generateToken() {
    if (pending) return;
    setPending(true);
    setError("");
    setMessage("");
    setToken("");
    try {
      const result = await api<{ token: string; server: MusicServerStatus }>(
        "/library/music/server/token",
        { method: "POST" },
      );
      setToken(result.token);
      setMessage(
        "A new music server token was created. Copy it now; it is shown only once.",
      );
      server.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function removeToken() {
    if (pending) return;
    setPending(true);
    setError("");
    setMessage("");
    setToken("");
    try {
      await api("/library/music/server/token", { method: "DELETE" });
      setMessage(
        "The music server token was removed. Subsonic clients can no longer sign in.",
      );
      server.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  return (
    <>
      <PageToolbar label="Music server actions">
        <ToolbarSection>
          <ToolbarButton icon={ArrowLeft} label="Music" to="/music" />
        </ToolbarSection>
      </PageToolbar>
      <PageBody className="mus-body">
        <PageHeading
          title="Music server"
          description="Optional Subsonic-compatible access for existing music clients. It is off until you enable it."
        />
        <RequestState
          loading={server.loading}
          error={server.error}
          retry={server.reload}
        />
        {server.data && (
          <>
            <Alert kind="info" title="Opt-in and same port">
              This endpoint is disabled by default. Enabling it does not open a
              new port: Subsonic clients use the existing ReelScarey HTTP or
              HTTPS address and port. If you use a reverse proxy or firewall,
              expose the same address and port you already use for ReelScarey.
            </Alert>
            <FieldSet legend="Server access">
              <label className="rs-check">
                <input
                  type="checkbox"
                  checked={enabled}
                  disabled={pending}
                  onChange={(event) => void saveEnabled(event.target.checked)}
                />
                <span>
                  <strong>Enable Subsonic-compatible access</strong>
                  <small>
                    The endpoint remains unavailable to clients while this is
                    off, even if a token exists.
                  </small>
                </span>
              </label>
              <div className="mus-scan-summary">
                <Label kind={server.data.enabled ? "success" : "disabled"}>
                  {server.data.enabled ? "Enabled" : "Disabled"}
                </Label>
                <Label kind={server.data.configured ? "success" : "disabled"}>
                  {server.data.configured
                    ? `Token ${server.data.token_hint}`
                    : "No token"}
                </Label>
                <Button
                  variant="soft"
                  disabled={pending}
                  onClick={() => void generateToken()}
                >
                  <KeyRound size={15} aria-hidden="true" />
                  {server.data.configured ? "Rotate token" : "Generate token"}
                </Button>
                {server.data.configured && (
                  <Button
                    variant="soft"
                    color="gray"
                    disabled={pending}
                    onClick={() => void removeToken()}
                  >
                    <Trash2 size={15} aria-hidden="true" />
                    Remove token
                  </Button>
                )}
              </div>
              {token && (
                <Alert kind="success" title="Copy this token now">
                  <div className="mus-server-token">
                    <TextField.Root
                      aria-label="Music server token"
                      value={token}
                      readOnly
                      onFocus={(event) => event.currentTarget.select()}
                    />
                  </div>
                  <p className="rs-muted">
                    Use this token as the password in your Subsonic client. The
                    username can be any label, for example{" "}
                    <code>reelscarey</code>. The administrator account password
                    is not accepted.
                  </p>
                </Alert>
              )}
              {message && <Alert kind="success">{message}</Alert>}
              {error && <Alert kind="danger">{error}</Alert>}
            </FieldSet>
            <FieldSet legend="Connection details">
              <ul className="mus-server-details">
                <li>
                  <Server size={16} aria-hidden="true" />
                  <span>
                    <strong>Server URL</strong>{" "}
                    <code>https://your-reelscarey-host/rest</code> (use the same
                    port as ReelScarey)
                  </span>
                </li>
                <li>
                  <ShieldCheck size={16} aria-hidden="true" />
                  <span>
                    <strong>Username</strong> any label, e.g.{" "}
                    <code>reelscarey</code>
                  </span>
                </li>
                <li>
                  <KeyRound size={16} aria-hidden="true" />
                  <span>
                    <strong>Password</strong> the generated token, not your
                    administrator password
                  </span>
                </li>
              </ul>
              <p className="rs-muted">
                Supported actions: ping, license, music folders, artists,
                indexes, artist, album, album list, song, search, random songs,
                cover art, playlists (empty), scrobble no-op, and
                stream/download with Range support. Write operations such as
                ratings are not implemented yet.
              </p>
              <p className="rs-muted rs-small">
                Continue to <Link to="/music">the music library</Link> to browse
                and play registered tracks.
              </p>
            </FieldSet>
          </>
        )}
      </PageBody>
    </>
  );
}
