import { useEffect, useState } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import {
  ArrowLeft,
  KeyRound,
  Pencil,
  RefreshCw,
  Server,
  ShieldCheck,
  Trash2,
  Users,
} from "lucide-react";
import { Link } from "react-router-dom";
import {
  Alert,
  Check,
  ConfirmDialog,
  EmptyState,
  FieldSet,
  Label,
  Modal,
  ModalClose,
  PageBody,
  PageToolbar,
  Table,
  ToolbarButton,
  ToolbarSection,
  formatTimestamp,
} from "../ui";
import { PageHeading, RequestState } from "../components";
import {
  api,
  messageFor,
  type MusicServerStatus,
  type MusicServerUser,
  type MusicServerUserInput,
} from "../api";
import { useResource } from "../useResource";
import "./Music.css";

type RoleKey =
  | "admin_role"
  | "settings_role"
  | "download_role"
  | "upload_role"
  | "playlist_role"
  | "cover_art_role"
  | "share_role"
  | "scrobbling_enabled";

type Roles = Record<RoleKey, boolean>;

const roleFields: { key: RoleKey; label: string; description: string }[] = [
  {
    key: "admin_role",
    label: "Administrator",
    description: "Full access to every action, including server settings.",
  },
  {
    key: "settings_role",
    label: "Change settings",
    description: "May change server settings and manage shares.",
  },
  {
    key: "download_role",
    label: "Play and download",
    description: "May stream and download songs.",
  },
  {
    key: "upload_role",
    label: "Upload",
    description: "May upload media to the server.",
  },
  {
    key: "playlist_role",
    label: "Playlists",
    description: "May create and edit playlists.",
  },
  {
    key: "cover_art_role",
    label: "Cover art",
    description: "May fetch cover art and avatars.",
  },
  {
    key: "share_role",
    label: "Sharing",
    description: "May create and view shares.",
  },
  {
    key: "scrobbling_enabled",
    label: "Scrobbling",
    description: "Reports played tracks back to the server.",
  },
];

/** Household defaults: a new account can play and build playlists, nothing more. */
const defaultRoles: Roles = {
  admin_role: false,
  settings_role: false,
  download_role: true,
  upload_role: false,
  playlist_role: true,
  cover_art_role: true,
  share_role: false,
  scrobbling_enabled: true,
};

function rolesFromUser(user: MusicServerUser): Roles {
  return {
    admin_role: user.admin_role,
    settings_role: user.settings_role,
    download_role: user.download_role,
    upload_role: user.upload_role,
    playlist_role: user.playlist_role,
    cover_art_role: user.cover_art_role,
    share_role: user.share_role,
    scrobbling_enabled: user.scrobbling_enabled,
  };
}

/** Short chips for the accounts table. An administrator implies everything. */
function roleChips(user: MusicServerUser): string[] {
  if (user.admin_role) return ["Administrator"];
  const chips: string[] = [];
  if (user.download_role) chips.push("Play");
  if (user.upload_role) chips.push("Upload");
  if (user.playlist_role) chips.push("Playlists");
  if (user.cover_art_role) chips.push("Cover art");
  if (user.share_role) chips.push("Sharing");
  if (user.settings_role) chips.push("Settings");
  if (user.scrobbling_enabled) chips.push("Scrobbling");
  return chips.length ? chips : ["No access"];
}

function RoleChecks({
  roles,
  onChange,
  disabled,
}: {
  roles: Roles;
  onChange: (roles: Roles) => void;
  disabled?: boolean;
}) {
  return (
    <div className="mus-role-grid">
      {roleFields.map((field) => (
        <Check
          key={field.key}
          label={field.label}
          description={field.description}
          checked={roles[field.key]}
          disabled={disabled}
          onChange={(checked) => onChange({ ...roles, [field.key]: checked })}
        />
      ))}
    </div>
  );
}

export function MusicServer() {
  const server = useResource<MusicServerStatus>("/library/music/server");
  const accounts = useResource<{ users: MusicServerUser[] }>(
    "/library/music/server/users",
  );
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [copied, setCopied] = useState(false);
  const [enabled, setEnabled] = useState(false);

  // Legacy shared token (one password any username may use).
  const [token, setToken] = useState("");

  // Protocol-role edit dialog. Identity and credentials live in Accounts.
  const [editing, setEditing] = useState<MusicServerUser>();
  const [editRoles, setEditRoles] = useState<Roles>(defaultRoles);

  useEffect(() => {
    if (server.data) setEnabled(server.data.enabled);
  }, [server.data]);

  const users = accounts.data?.users ?? [];
  const hasSharedToken = (server.data?.token_hint ?? "") !== "";

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
    } catch {
      setError(
        "Could not copy automatically. Select the value and copy it manually.",
      );
    }
  }

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

  function startEdit(user: MusicServerUser) {
    setEditing(user);
    setEditRoles(rolesFromUser(user));
    setError("");
    setMessage("");
  }

  async function saveEdit() {
    if (!editing || pending) return;
    setPending(true);
    setError("");
    setMessage("");
    try {
      const body: MusicServerUserInput = {
        username: editing.username,
        email: editing.email,
        ...editRoles,
      };
      const updated = await api<MusicServerUser>(
        `/library/music/server/users/${editing.id}`,
        { method: "PUT", body: JSON.stringify(body) },
      );
      setMessage(`Saved changes to \u201c${updated.username}\u201d.`);
      setEditing(undefined);
      accounts.reload();
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
    setCopied(false);
    try {
      const result = await api<{ token: string; server: MusicServerStatus }>(
        "/library/music/server/token",
        { method: "POST" },
      );
      setToken(result.token);
      setMessage(
        "A new shared token was created. Copy it now; it is shown only once.",
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
    setCopied(false);
    try {
      await api("/library/music/server/token", { method: "DELETE" });
      setToken("");
      setMessage(
        "The shared token was removed. Accounts above keep working; clients using the shared token cannot sign in.",
      );
      server.reload();
    } catch (e) {
      setError(messageFor(e));
      // Let the confirm dialog keep itself open and show the failure.
      throw e;
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
          description="Optional Subsonic-compatible access for music clients, with one account per person and per-account permissions."
        />
        <RequestState
          loading={server.loading}
          error={server.error}
          code={server.code}
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
                    Every account below is refused while this is off, even if
                    credentials exist.
                  </small>
                </span>
              </label>
              <div className="mus-scan-summary">
                <Label kind={server.data.enabled ? "success" : "disabled"}>
                  {server.data.enabled ? "Enabled" : "Disabled"}
                </Label>
                <Label kind={server.data.users > 0 ? "success" : "disabled"}>
                  {server.data.users === 1
                    ? "1 account"
                    : `${server.data.users} accounts`}
                </Label>
              </div>
            </FieldSet>

            {message && <Alert kind="success">{message}</Alert>}
            {error && <Alert kind="danger">{error}</Alert>}

            <FieldSet
              legend="Accounts"
              description="Protocol permissions for centrally managed accounts. Manage identities, passwords, and system access in Accounts."
              actions={
                <>
                  <Button asChild variant="soft" size="1">
                    <Link to="/accounts">Manage accounts</Link>
                  </Button>
                  <Button
                    variant="soft"
                    size="1"
                    disabled={pending || accounts.loading}
                    onClick={() => void accounts.reload()}
                  >
                    <RefreshCw size={14} aria-hidden="true" /> Refresh
                  </Button>
                </>
              }
            >
              <RequestState
                loading={accounts.loading}
                error={accounts.error}
                code={accounts.code}
                retry={accounts.reload}
              />
              {accounts.data && users.length === 0 && (
                <EmptyState icon={Users} title="No accounts yet">
                  Grant Subsonic access from Accounts so a music client can sign
                  in with its centrally managed identity.
                </EmptyState>
              )}
              {accounts.data && users.length > 0 && (
                <Table label="Music server accounts">
                  <thead>
                    <tr>
                      <th>Username</th>
                      <th>Email</th>
                      <th>Permissions</th>
                      <th>Last used</th>
                      <th aria-label="Actions" />
                    </tr>
                  </thead>
                  <tbody>
                    {users.map((user) => (
                      <tr key={user.id}>
                        <td>
                          <span className="mus-account-name">
                            {user.username}
                            {user.password_generated && (
                              <Badge variant="soft" color="gray">
                                Generated
                              </Badge>
                            )}
                          </span>
                        </td>
                        <td>{user.email || "\u2014"}</td>
                        <td>
                          {roleChips(user).map((chip) => (
                            <Badge
                              key={chip}
                              variant="soft"
                              style={{ marginRight: 4 }}
                            >
                              {chip}
                            </Badge>
                          ))}
                        </td>
                        <td>
                          {user.last_used_at
                            ? formatTimestamp(user.last_used_at)
                            : "Never"}
                        </td>
                        <td>
                          <div className="mus-account-actions">
                            <Button
                              type="button"
                              variant="soft"
                              size="1"
                              disabled={pending}
                              onClick={() => startEdit(user)}
                            >
                              <Pencil size={14} aria-hidden="true" /> Edit
                              permissions
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </FieldSet>

            <FieldSet
              legend="Shared token (legacy)"
              description="A single password any username may use. It is kept so clients configured before per-user accounts keep working; prefer the named accounts above."
            >
              <div className="mus-scan-summary">
                <Label kind={hasSharedToken ? "success" : "disabled"}>
                  {hasSharedToken
                    ? `Token ${server.data.token_hint}`
                    : "No shared token"}
                </Label>
                <Button
                  variant="soft"
                  disabled={pending}
                  onClick={() => void generateToken()}
                >
                  <KeyRound size={15} aria-hidden="true" />
                  {hasSharedToken ? "Rotate token" : "Generate token"}
                </Button>
                {hasSharedToken && (
                  <ConfirmDialog
                    trigger={
                      <Button variant="soft" color="gray" disabled={pending}>
                        <Trash2 size={15} aria-hidden="true" /> Remove token
                      </Button>
                    }
                    title="Remove the shared music-server token?"
                    description="Every client signed in with the shared token immediately loses access. Named accounts above keep working. Create a new token when it is needed again."
                    confirmLabel="Remove token"
                    color="red"
                    onConfirm={() => removeToken()}
                  />
                )}
              </div>
              {token && (
                <Alert kind="success" title="Copy this token now">
                  <div className="mus-server-token">
                    <TextField.Root
                      aria-label="Shared music server token"
                      value={token}
                      readOnly
                      onFocus={(event) => event.currentTarget.select()}
                    />
                    <Button
                      type="button"
                      variant="soft"
                      onClick={() => void copy(token)}
                    >
                      {copied ? "Copied" : "Copy token"}
                    </Button>
                  </div>
                  <p className="rs-muted">
                    Use this token as the password with any username. It is 20
                    characters; type or paste it exactly as shown.
                  </p>
                </Alert>
              )}
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
                    <strong>Username</strong> an account username from the list
                    above
                  </span>
                </li>
                <li>
                  <KeyRound size={16} aria-hidden="true" />
                  <span>
                    <strong>Password</strong> that account&apos;s password, or
                    the shared token if you use one
                  </span>
                </li>
              </ul>
              <p className="rs-muted">
                Supported actions: ping, license, music folders, artists,
                indexes, artist, album, album list, song, search, random songs,
                cover art, playlists, scrobble, and stream/download with Range
                support. Write operations such as ratings are not implemented
                yet.
              </p>
              <p className="rs-muted rs-small">
                Continue to <Link to="/music">the music library</Link> to browse
                and play registered tracks.
              </p>
            </FieldSet>
          </>
        )}
      </PageBody>

      <Modal
        open={editing !== undefined}
        onOpenChange={(open) => {
          if (!open && !pending) setEditing(undefined);
        }}
        title="Edit music server permissions"
        description={
          editing
            ? `Update Subsonic protocol permissions for ${editing.username}.`
            : undefined
        }
        size="medium"
        footer={
          <>
            <ModalClose>
              <Button variant="soft" color="gray" disabled={pending}>
                Cancel
              </Button>
            </ModalClose>
            <Button disabled={pending} onClick={() => void saveEdit()}>
              Save permissions
            </Button>
          </>
        }
      >
        {editing && (
          <>
            <p>
              Account identity, password, and access are managed in{" "}
              <Link to="/accounts">Accounts</Link>. This dialog changes only
              music-client protocol permissions for{" "}
              <strong>{editing.username}</strong>.
            </p>
            <RoleChecks
              roles={editRoles}
              onChange={setEditRoles}
              disabled={pending}
            />
          </>
        )}
      </Modal>
    </>
  );
}
