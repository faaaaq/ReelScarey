import { useEffect, useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import {
  ArrowLeft,
  KeyRound,
  Pencil,
  RefreshCw,
  Server,
  ShieldCheck,
  Trash2,
  UserPlus,
  Users,
} from "lucide-react";
import { Link } from "react-router-dom";
import {
  Alert,
  Check,
  ConfirmDialog,
  EmptyState,
  FieldSet,
  FormActions,
  FormGroup,
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

  // Shown-once password from a create or a reset.
  const [issued, setIssued] = useState<{
    user: MusicServerUser;
    password: string;
  }>();

  // Create form.
  const [newUsername, setNewUsername] = useState("");
  const [newEmail, setNewEmail] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [newRoles, setNewRoles] = useState<Roles>(defaultRoles);

  // Edit dialog.
  const [editing, setEditing] = useState<MusicServerUser>();
  const [editUsername, setEditUsername] = useState("");
  const [editEmail, setEditEmail] = useState("");
  const [editPassword, setEditPassword] = useState("");
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

  async function createAccount(event: FormEvent) {
    event.preventDefault();
    if (pending) return;
    setPending(true);
    setError("");
    setMessage("");
    setIssued(undefined);
    setCopied(false);
    try {
      const result = await api<{ user: MusicServerUser; password: string }>(
        "/library/music/server/users",
        {
          method: "POST",
          body: JSON.stringify({
            username: newUsername.trim(),
            email: newEmail.trim(),
            password: newPassword,
            ...newRoles,
          }),
        },
      );
      setNewUsername("");
      setNewEmail("");
      setNewPassword("");
      setNewRoles(defaultRoles);
      if (result.password) {
        setIssued(result);
      } else {
        setMessage(`Created the account \u201c${result.user.username}\u201d.`);
      }
      accounts.reload();
      server.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  function startEdit(user: MusicServerUser) {
    setEditing(user);
    setEditUsername(user.username);
    setEditEmail(user.email);
    setEditPassword("");
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
        username: editUsername.trim(),
        email: editEmail.trim(),
        ...editRoles,
      };
      if (editPassword) body.password = editPassword;
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

  async function resetPassword(user: MusicServerUser) {
    if (pending) return;
    setPending(true);
    setError("");
    setMessage("");
    setIssued(undefined);
    setCopied(false);
    try {
      const result = await api<{ user: MusicServerUser; password: string }>(
        `/library/music/server/users/${user.id}/password`,
        { method: "POST" },
      );
      setIssued(result);
      accounts.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  async function removeAccount(user: MusicServerUser) {
    setError("");
    setMessage("");
    try {
      await api(`/library/music/server/users/${user.id}`, { method: "DELETE" });
      setMessage(`Removed the account \u201c${user.username}\u201d.`);
      accounts.reload();
      server.reload();
    } catch (e) {
      setError(messageFor(e));
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

            {issued && (
              <Alert kind="warning" title="Copy this password now">
                <div className="mus-server-token">
                  <TextField.Root
                    aria-label="Account password"
                    value={issued.password}
                    readOnly
                    onFocus={(event) => event.currentTarget.select()}
                  />
                  <Button
                    type="button"
                    variant="soft"
                    onClick={() => void copy(issued.password)}
                  >
                    {copied ? "Copied" : "Copy password"}
                  </Button>
                </div>
                <p className="rs-muted">
                  Sign in to the Subsonic client as{" "}
                  <code>{issued.user.username}</code> with this password. It is
                  shown once and cannot be retrieved later; reset it if it is
                  lost.
                </p>
              </Alert>
            )}

            <FieldSet
              legend="Accounts"
              description="Each account signs in with its own username and password. Permissions apply to that account only."
              actions={
                <Button
                  variant="soft"
                  size="1"
                  disabled={pending || accounts.loading}
                  onClick={() => void accounts.reload()}
                >
                  <RefreshCw size={14} aria-hidden="true" /> Refresh
                </Button>
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
                  Add an account below so a Subsonic client can sign in with a
                  username and password.
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
                            </Button>
                            <Button
                              type="button"
                              variant="soft"
                              size="1"
                              disabled={pending}
                              onClick={() => void resetPassword(user)}
                            >
                              <KeyRound size={14} aria-hidden="true" /> Reset
                              password
                            </Button>
                            <ConfirmDialog
                              title={`Remove ${user.username}?`}
                              description="Clients signed in as this account stop working immediately. This cannot be undone."
                              confirmLabel="Remove account"
                              trigger={
                                <Button
                                  type="button"
                                  color="red"
                                  variant="soft"
                                  size="1"
                                >
                                  <Trash2 size={14} aria-hidden="true" /> Remove
                                </Button>
                              }
                              onConfirm={() => removeAccount(user)}
                            />
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </FieldSet>

            <FieldSet
              legend="Add an account"
              description="Give each person their own credentials. Leave the password blank to generate a strong one."
            >
              <form onSubmit={(event) => void createAccount(event)}>
                <FormGroup
                  label="Username"
                  htmlFor="mus-user-name"
                  help="1-64 characters, not case sensitive."
                >
                  <TextField.Root
                    id="mus-user-name"
                    value={newUsername}
                    maxLength={64}
                    placeholder="alex"
                    autoComplete="off"
                    onChange={(event) => setNewUsername(event.target.value)}
                  />
                </FormGroup>
                <FormGroup label="Email (optional)" htmlFor="mus-user-email">
                  <TextField.Root
                    id="mus-user-email"
                    type="email"
                    value={newEmail}
                    maxLength={254}
                    placeholder="alex@example.com"
                    autoComplete="off"
                    onChange={(event) => setNewEmail(event.target.value)}
                  />
                </FormGroup>
                <FormGroup
                  label="Password (optional)"
                  htmlFor="mus-user-password"
                  help="Leave blank to generate a strong 20-character password."
                >
                  <TextField.Root
                    id="mus-user-password"
                    type="password"
                    value={newPassword}
                    maxLength={256}
                    autoComplete="new-password"
                    onChange={(event) => setNewPassword(event.target.value)}
                  />
                </FormGroup>
                <RoleChecks
                  roles={newRoles}
                  onChange={setNewRoles}
                  disabled={pending}
                />
                <FormActions>
                  <Button
                    type="submit"
                    disabled={pending || !newUsername.trim()}
                  >
                    <UserPlus size={16} aria-hidden="true" /> Add account
                  </Button>
                </FormActions>
              </form>
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
        title="Edit account"
        description={editing ? `Update ${editing.username}.` : undefined}
        size="medium"
        footer={
          <>
            <ModalClose>
              <Button variant="soft" color="gray" disabled={pending}>
                Cancel
              </Button>
            </ModalClose>
            <Button
              disabled={pending || !editUsername.trim()}
              onClick={() => void saveEdit()}
            >
              Save changes
            </Button>
          </>
        }
      >
        <form onSubmit={() => void saveEdit()}>
          <FormGroup label="Username" htmlFor="mus-edit-name">
            <TextField.Root
              id="mus-edit-name"
              value={editUsername}
              maxLength={64}
              autoComplete="off"
              onChange={(event) => setEditUsername(event.target.value)}
            />
          </FormGroup>
          <FormGroup label="Email (optional)" htmlFor="mus-edit-email">
            <TextField.Root
              id="mus-edit-email"
              type="email"
              value={editEmail}
              maxLength={254}
              autoComplete="off"
              onChange={(event) => setEditEmail(event.target.value)}
            />
          </FormGroup>
          <FormGroup
            label="New password (optional)"
            htmlFor="mus-edit-password"
            help="Leave blank to keep the current password."
          >
            <TextField.Root
              id="mus-edit-password"
              type="password"
              value={editPassword}
              maxLength={256}
              autoComplete="new-password"
              onChange={(event) => setEditPassword(event.target.value)}
            />
          </FormGroup>
          <RoleChecks
            roles={editRoles}
            onChange={setEditRoles}
            disabled={pending}
          />
        </form>
      </Modal>
    </>
  );
}
