import { useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import { KeyRound, Trash2 } from "lucide-react";
import {
  api,
  messageFor,
  type APIKey,
  type APIKeyCreated,
  type APIKeyList,
} from "../api";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  Check,
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

const scopeLabels: Record<string, string> = {
  "compat-sonarr": "Sonarr-compatible API",
  "compat-radarr": "Radarr-compatible API",
  "compat-prowlarr": "Prowlarr-compatible API",
};

export function APIKeys() {
  const resource = useResource<APIKeyList>("/api-keys");
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<string[]>(["compat-sonarr"]);
  const [created, setCreated] = useState<APIKeyCreated>();
  const [revoking, setRevoking] = useState<APIKey>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [copied, setCopied] = useState(false);

  function toggleScope(scope: string, checked: boolean) {
    setScopes((current) =>
      checked
        ? [...current.filter((item) => item !== scope), scope]
        : current.filter((item) => item !== scope),
    );
  }

  async function create(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const result = await api<APIKeyCreated>("/api-keys", {
        method: "POST",
        body: JSON.stringify({
          name: name.trim(),
          scopes,
          expires_at: 0,
        }),
      });
      setCreated(result);
      setName("");
      setCopied(false);
      resource.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function revoke() {
    if (!revoking || busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`/api-keys/${revoking.id}`, { method: "DELETE" });
      setNotice(
        `Revoked “${revoking.name}”. Integrations using it stop working.`,
      );
      setRevoking(undefined);
      resource.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function copyToken() {
    if (!created) return;
    try {
      await navigator.clipboard.writeText(created.token);
      setCopied(true);
    } catch {
      setError(
        "Could not copy automatically. Select the token and copy it manually.",
      );
    }
  }

  const items = resource.data?.items ?? [];
  return (
    <PageBody>
      <PageHeading
        title="API keys"
        description="Scoped tokens for the Sonarr, Radarr and Prowlarr compatibility APIs. Keys are shown once and stored only as a one-way digest."
      />
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {error && <Alert kind="danger">{error}</Alert>}
      {notice && <Alert kind="success">{notice}</Alert>}
      {created && (
        <Alert kind="warning" title="Copy this token now">
          <p>
            This token is shown once and cannot be recovered. Send it only to
            the integration that needs it.
          </p>
          <code className="rs-token">{created.token}</code>
          <Button type="button" variant="soft" onClick={() => void copyToken()}>
            {copied ? "Copied" : "Copy token"}
          </Button>
        </Alert>
      )}
      <FieldSet
        legend="Create a key"
        description="A key can carry more than one compatibility scope. Revoke it here at any time."
      >
        <form onSubmit={(event) => void create(event)}>
          <FormGroup label="Key name" htmlFor="api-key-name">
            <TextField.Root
              id="api-key-name"
              value={name}
              maxLength={80}
              placeholder="Overseerr / autobrr"
              onChange={(event) => setName(event.target.value)}
            />
          </FormGroup>
          <div className="rs-check-grid">
            {(resource.data?.scopes ?? Object.keys(scopeLabels)).map(
              (scope) => (
                <Check
                  key={scope}
                  label={scopeLabels[scope] ?? scope}
                  checked={scopes.includes(scope)}
                  onChange={(checked) => toggleScope(scope, checked)}
                />
              ),
            )}
          </div>
          <FormActions>
            <Button
              type="submit"
              disabled={busy || !name.trim() || scopes.length === 0}
            >
              <KeyRound size={16} aria-hidden="true" /> Create key
            </Button>
          </FormActions>
        </form>
      </FieldSet>
      {resource.data && items.length === 0 && (
        <EmptyState icon={KeyRound} title="No API keys">
          Create a scoped key to connect a Sonarr/Radarr/Prowlarr-compatible
          client. The key only reaches the compatibility endpoints.
        </EmptyState>
      )}
      {items.length > 0 && (
        <Table label="API keys">
          <thead>
            <tr>
              <th>Name</th>
              <th>Prefix</th>
              <th>Scopes</th>
              <th>Last used</th>
              <th>State</th>
              <th aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.id}>
                <td>{item.name}</td>
                <td>
                  <code>{item.prefix}…</code>
                </td>
                <td>
                  {item.scopes.map((scope) => (
                    <Badge
                      key={scope}
                      variant="soft"
                      style={{ marginRight: 4 }}
                    >
                      {scopeLabels[scope] ?? scope}
                    </Badge>
                  ))}
                </td>
                <td>
                  {item.last_used_at
                    ? formatTimestamp(item.last_used_at)
                    : "Never"}
                </td>
                <td>{item.revoked_at ? "Revoked" : "Active"}</td>
                <td>
                  {!item.revoked_at && (
                    <Button
                      type="button"
                      color="red"
                      variant="soft"
                      size="1"
                      onClick={() => setRevoking(item)}
                    >
                      <Trash2 size={14} aria-hidden="true" /> Revoke
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {revoking && (
        <Alert kind="warning" title="Revoke this key?">
          <p>
            Integrations using “{revoking.name}” will receive authentication
            errors immediately.
          </p>
          <FormActions>
            <Button
              type="button"
              color="red"
              disabled={busy}
              onClick={() => void revoke()}
            >
              Revoke key
            </Button>
            <Button
              type="button"
              variant="soft"
              onClick={() => setRevoking(undefined)}
            >
              Cancel
            </Button>
          </FormActions>
        </Alert>
      )}
    </PageBody>
  );
}
