import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { KeyRound, Link2, Pencil, UserPlus, Users } from "lucide-react";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  Check,
  EmptyState,
  FieldSet,
  FormActions,
  FormGroup,
  Label,
  Modal,
  ModalClose,
  PageBody,
  Table,
  formatTimestamp,
} from "../ui";
import { useResource } from "../useResource";
import "./Admin.css";

type Account = {
  id: number;
  username: string;
  disabled: boolean;
  created_at: number;
  requests: boolean;
  music_player: boolean;
  subsonic: boolean;
  password_set: boolean;
  request_account_id?: number;
  music_server_user_id?: number;
  discord_user_id?: string;
};

type AccountList = { items: Account[] };
type AccessOptions = Pick<Account, "requests" | "music_player" | "subsonic">;
type AccountSettings = AccessOptions & Pick<Account, "disabled">;
type EditState = { account: Account; settings: AccountSettings };

const initialAccess: AccessOptions = {
  requests: false,
  music_player: false,
  subsonic: false,
};

function AccessChecks({
  value,
  onChange,
  disabled,
  idPrefix,
}: {
  value: AccessOptions;
  onChange: (value: AccessOptions) => void;
  disabled?: boolean;
  idPrefix: string;
}) {
  return (
    <FormGroup
      label="Access"
      help="These options are independent. Browser music access does not enable Subsonic, and neither option enables Requests."
    >
      <div className="rs-check-grid">
        <Check
          id={`${idPrefix}-requests`}
          label="Requests"
          description="Submit and manage movie and series requests."
          checked={value.requests}
          disabled={disabled}
          onChange={(requests) => onChange({ ...value, requests })}
        />
        <Check
          id={`${idPrefix}-music-player`}
          label="Music player"
          description="Browse and play music in the browser."
          checked={value.music_player}
          disabled={disabled}
          onChange={(music_player) => onChange({ ...value, music_player })}
        />
        <Check
          id={`${idPrefix}-subsonic`}
          label="Subsonic"
          description="Sign in with Subsonic-compatible music clients."
          checked={value.subsonic}
          disabled={disabled}
          onChange={(subsonic) => onChange({ ...value, subsonic })}
        />
      </div>
    </FormGroup>
  );
}

export function Accounts() {
  const resource = useResource<AccountList>("/accounts", { retain: true });
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [createAccess, setCreateAccess] =
    useState<AccessOptions>(initialAccess);
  const [editing, setEditing] = useState<EditState>();
  const [passwordAccount, setPasswordAccount] = useState<Account>();
  const [newPassword, setNewPassword] = useState("");
  const [discordAccount, setDiscordAccount] = useState<Account>();
  const [discordUserID, setDiscordUserID] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const items = resource.data?.items ?? [];
  const modalOpen = !!(editing || passwordAccount || discordAccount);
  const needsPassword = items.some((account) => !account.password_set);

  function startMutation() {
    setBusy(true);
    setError("");
    setNotice("");
  }

  function openEdit(account: Account) {
    setError("");
    setNotice("");
    setEditing({
      account,
      settings: {
        disabled: account.disabled,
        requests: account.requests,
        music_player: account.music_player,
        subsonic: account.subsonic,
      },
    });
  }

  function openPassword(account: Account) {
    setError("");
    setNotice("");
    setNewPassword("");
    setPasswordAccount(account);
  }

  function openDiscord(account: Account) {
    setError("");
    setNotice("");
    setDiscordUserID(account.discord_user_id ?? "");
    setDiscordAccount(account);
  }

  async function create(event: FormEvent) {
    event.preventDefault();
    const cleanUsername = username.trim();
    if (busy || !cleanUsername || !password) return;
    startMutation();
    try {
      await api<unknown>("/accounts", {
        method: "POST",
        body: JSON.stringify({
          username: cleanUsername,
          password,
          ...createAccess,
        }),
      });
      setUsername("");
      setPassword("");
      setCreateAccess(initialAccess);
      setNotice(`Created account “${cleanUsername}”.`);
      resource.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function saveEdit(event: FormEvent) {
    event.preventDefault();
    if (busy || !editing) return;
    startMutation();
    try {
      await api<unknown>(`/accounts/${editing.account.id}`, {
        method: "PUT",
        body: JSON.stringify(editing.settings),
      });
      setNotice(`Updated access for “${editing.account.username}”.`);
      setEditing(undefined);
      resource.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function savePassword(event: FormEvent) {
    event.preventDefault();
    if (busy || !passwordAccount || !newPassword) return;
    const settingPassword = !passwordAccount.password_set;
    startMutation();
    try {
      await api<unknown>(`/accounts/${passwordAccount.id}/password`, {
        method: "POST",
        body: JSON.stringify({ password: newPassword }),
      });
      setNotice(
        `${settingPassword ? "Set" : "Reset"} the password for “${passwordAccount.username}”. The new password applies to browser and Subsonic sign-in.`,
      );
      setPasswordAccount(undefined);
      setNewPassword("");
      resource.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function saveDiscord(event: FormEvent) {
    event.preventDefault();
    const cleanID = discordUserID.trim();
    if (busy || !discordAccount) return;
    startMutation();
    try {
      await api<unknown>(`/accounts/${discordAccount.id}/discord`, {
        method: "PUT",
        body: JSON.stringify({ discord_user_id: cleanID }),
      });
      setNotice(`Updated the Discord link for “${discordAccount.username}”.`);
      setDiscordAccount(undefined);
      resource.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <PageBody>
        <PageHeading
          title="Accounts"
          description="Each account uses one username and password for both browser and Subsonic sign-in. Requests, Music player and Subsonic access are controlled independently."
        />
        <RequestState
          loading={resource.loading}
          error={resource.error}
          code={resource.code}
          retry={resource.reload}
        />
        {error && !modalOpen && <Alert kind="danger">{error}</Alert>}
        {notice && <Alert kind="success">{notice}</Alert>}
        {resource.data && (
          <>
            {needsPassword && (
              <Alert kind="warning" title="Password setup required">
                <p>
                  Accounts marked <strong>Password required</strong> were
                  migrated with Subsonic-only credentials. An administrator must
                  set a password before they can sign in through a browser.
                  Setting or resetting a password applies to both browser and
                  Subsonic sign-in.
                </p>
              </Alert>
            )}
            <FieldSet
              legend="Create account"
              description="Choose the access this person needs. You can change access, disable the account, or reset its password later."
            >
              <form
                className="rs-stack"
                onSubmit={(event) => void create(event)}
              >
                <FormGroup label="Username" htmlFor="account-create-username">
                  <TextField.Root
                    id="account-create-username"
                    value={username}
                    onChange={(event) => setUsername(event.target.value)}
                    autoComplete="off"
                    maxLength={40}
                    required
                  />
                </FormGroup>
                <FormGroup
                  label="Password"
                  htmlFor="account-create-password"
                  help="Required for browser sign-in. Subsonic clients use this same password when Subsonic access is enabled."
                >
                  <TextField.Root
                    id="account-create-password"
                    type="password"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    autoComplete="new-password"
                    minLength={12}
                    maxLength={256}
                    required
                  />
                </FormGroup>
                <AccessChecks
                  idPrefix="account-create"
                  value={createAccess}
                  onChange={setCreateAccess}
                  disabled={busy}
                />
                <FormActions>
                  <Button
                    type="submit"
                    disabled={busy || !username.trim() || !password}
                  >
                    <UserPlus size={16} aria-hidden="true" />
                    {busy ? "Creating…" : "Create account"}
                  </Button>
                </FormActions>
              </form>
            </FieldSet>
            {items.length === 0 ? (
              <EmptyState icon={Users} title="No accounts">
                Create an account to give someone request or music access.
              </EmptyState>
            ) : (
              <Table label="Accounts">
                <thead>
                  <tr>
                    <th>Username</th>
                    <th>Created</th>
                    <th>Access</th>
                    <th>Status</th>
                    <th>Password</th>
                    <th aria-label="Actions" />
                  </tr>
                </thead>
                <tbody>
                  {items.map((account) => {
                    const access = [
                      account.requests ? "Requests" : "",
                      account.music_player ? "Music player" : "",
                      account.subsonic ? "Subsonic" : "",
                    ].filter(Boolean);
                    return (
                      <tr key={account.id}>
                        <td>
                          <strong>{account.username}</strong>
                          {account.music_server_user_id !== undefined && (
                            <div className="rs-muted rs-small">
                              Music server user #{account.music_server_user_id}
                            </div>
                          )}
                        </td>
                        <td>{formatTimestamp(account.created_at)}</td>
                        <td>
                          {access.length > 0 ? (
                            <div className="rs-row">
                              {access.map((label) => (
                                <Label key={label} kind="primary">
                                  {label}
                                </Label>
                              ))}
                            </div>
                          ) : (
                            <span className="rs-muted">None</span>
                          )}
                        </td>
                        <td>
                          <Label kind={account.disabled ? "danger" : "success"}>
                            {account.disabled ? "Disabled" : "Active"}
                          </Label>
                        </td>
                        <td>
                          <Label
                            kind={account.password_set ? "success" : "warning"}
                          >
                            {account.password_set
                              ? "Password set"
                              : "Password required"}
                          </Label>
                        </td>
                        <td>
                          <div className="rs-row">
                            <Button
                              type="button"
                              size="1"
                              variant="soft"
                              aria-label={`Edit access for ${account.username}`}
                              onClick={() => openEdit(account)}
                            >
                              <Pencil size={14} aria-hidden="true" /> Edit
                            </Button>
                            <Button
                              type="button"
                              size="1"
                              variant="soft"
                              aria-label={`${account.password_set ? "Reset" : "Set"} password for ${account.username}`}
                              onClick={() => openPassword(account)}
                            >
                              <KeyRound size={14} aria-hidden="true" />
                              {account.password_set
                                ? "Reset password"
                                : "Set password"}
                            </Button>
                            {account.request_account_id !== undefined && (
                              <Button
                                type="button"
                                size="1"
                                variant="soft"
                                aria-label={`${account.discord_user_id ? "Edit" : "Link"} Discord for ${account.username}`}
                                onClick={() => openDiscord(account)}
                              >
                                <Link2 size={14} aria-hidden="true" />
                                {account.discord_user_id
                                  ? "Discord linked"
                                  : "Link Discord"}
                              </Button>
                            )}
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </Table>
            )}
          </>
        )}
      </PageBody>

      <Modal
        open={!!editing}
        onOpenChange={(open) => {
          if (!open && !busy) setEditing(undefined);
        }}
        title={editing ? `Edit ${editing.account.username}` : "Edit account"}
        description="Change account status and access without changing its username or password."
        size="small"
        footer={
          <>
            <ModalClose>
              <Button type="button" variant="soft" color="gray" disabled={busy}>
                Cancel
              </Button>
            </ModalClose>
            <Button type="submit" form="account-edit-form" disabled={busy}>
              {busy ? "Saving…" : "Save changes"}
            </Button>
          </>
        }
      >
        {editing && (
          <form
            id="account-edit-form"
            className="rs-stack"
            onSubmit={(event) => void saveEdit(event)}
          >
            <Check
              id="account-edit-disabled"
              label="Disable account"
              description="Prevent sign-in and access without removing the account or its linked identities."
              checked={editing.settings.disabled}
              disabled={busy}
              onChange={(disabled) =>
                setEditing((current) =>
                  current
                    ? {
                        ...current,
                        settings: { ...current.settings, disabled },
                      }
                    : current,
                )
              }
            />
            <AccessChecks
              idPrefix="account-edit"
              value={editing.settings}
              onChange={(access) =>
                setEditing((current) =>
                  current
                    ? {
                        ...current,
                        settings: { ...current.settings, ...access },
                      }
                    : current,
                )
              }
              disabled={busy}
            />
            {error && <Alert kind="danger">{error}</Alert>}
          </form>
        )}
      </Modal>

      <Modal
        open={!!passwordAccount}
        onOpenChange={(open) => {
          if (!open && !busy) setPasswordAccount(undefined);
        }}
        title={
          passwordAccount?.password_set
            ? `Reset password for ${passwordAccount.username}`
            : `Set password for ${passwordAccount?.username ?? "account"}`
        }
        description={
          passwordAccount && !passwordAccount.password_set
            ? "This migrated Subsonic-only account needs an administrator-set password before browser sign-in."
            : "Choose a new password for this account."
        }
        size="small"
        footer={
          <>
            <ModalClose>
              <Button type="button" variant="soft" color="gray" disabled={busy}>
                Cancel
              </Button>
            </ModalClose>
            <Button
              type="submit"
              form="account-password-form"
              disabled={busy || !newPassword}
            >
              {busy
                ? "Saving…"
                : passwordAccount?.password_set
                  ? "Reset password"
                  : "Set password"}
            </Button>
          </>
        }
      >
        {passwordAccount && (
          <form
            id="account-password-form"
            className="rs-stack"
            onSubmit={(event) => void savePassword(event)}
          >
            <Alert kind="info">
              <p>
                The new password applies everywhere: use it for both browser and
                Subsonic sign-in. Access settings do not change.
              </p>
            </Alert>
            <FormGroup
              label="New password"
              htmlFor="account-new-password"
              help="Share the new password with the account owner through a secure channel."
            >
              <TextField.Root
                id="account-new-password"
                type="password"
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
                autoComplete="new-password"
                minLength={12}
                maxLength={256}
                required
                autoFocus
              />
            </FormGroup>
            {error && <Alert kind="danger">{error}</Alert>}
          </form>
        )}
      </Modal>

      <Modal
        open={!!discordAccount}
        onOpenChange={(open) => {
          if (!open && !busy) setDiscordAccount(undefined);
        }}
        title={
          discordAccount
            ? `Discord link for ${discordAccount.username}`
            : "Discord link"
        }
        description="Link the Discord user to this account's request profile. Accounts without a request profile do not have this option."
        size="small"
        footer={
          <>
            <ModalClose>
              <Button type="button" variant="soft" color="gray" disabled={busy}>
                Cancel
              </Button>
            </ModalClose>
            <Button type="submit" form="account-discord-form" disabled={busy}>
              {busy ? "Saving…" : "Save Discord link"}
            </Button>
          </>
        }
      >
        {discordAccount && (
          <form
            id="account-discord-form"
            className="rs-stack"
            onSubmit={(event) => void saveDiscord(event)}
          >
            <FormGroup
              label="Discord user ID"
              htmlFor="account-discord-user-id"
              help="Enter the numeric user ID from Discord, or leave it empty to remove the current link."
            >
              <TextField.Root
                id="account-discord-user-id"
                value={discordUserID}
                onChange={(event) => setDiscordUserID(event.target.value)}
                inputMode="numeric"
                autoComplete="off"
                placeholder="123456789012345678"
                maxLength={64}
                autoFocus
              />
            </FormGroup>
            {error && <Alert kind="danger">{error}</Alert>}
          </form>
        )}
      </Modal>
    </>
  );
}
