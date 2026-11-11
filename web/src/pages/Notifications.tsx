import { useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import { Bell, Pencil, Send, Trash2 } from "lucide-react";
import {
  api,
  messageFor,
  type NotificationChannel,
  type NotificationChannelInput,
  type NotificationChannelList,
  type NotificationKind,
  type NotificationOutboxItem,
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

const kindLabels: Record<NotificationKind, string> = {
  email: "Email (SMTP)",
  ntfy: "ntfy",
  gotify: "Gotify",
  pushover: "Pushover",
  telegram: "Telegram",
  discord: "Discord webhook",
  webhook: "Generic webhook",
};

const eventLabels: Record<string, string> = {
  "download.completed": "Download completed",
  "download.failed": "Download failed or blocked",
  "import.completed": "Import completed",
  "request.approved": "Request approved",
  "request.declined": "Request declined",
  "health.failed": "Automation or health failure",
  "subtitle.failed": "Subtitle failure",
  "backup.failed": "Backup failure",
  "organization.failed": "Organization failure",
  "youtube.completed": "YouTube acquisition completed",
};

type Draft = {
  id: number;
  name: string;
  kind: NotificationKind;
  enabled: boolean;
  config: Record<string, string>;
  events: string[];
  secret: string;
  revision: number;
};

function emptyDraft(): Draft {
  return {
    id: 0,
    name: "",
    kind: "ntfy",
    enabled: true,
    config: {},
    events: ["download.completed", "import.completed"],
    secret: "",
    revision: 0,
  };
}

function draftFrom(channel: NotificationChannel): Draft {
  return {
    id: channel.id,
    name: channel.name,
    kind: channel.kind,
    enabled: channel.enabled,
    config: { ...channel.config },
    events: [...channel.events],
    secret: "",
    revision: channel.revision,
  };
}

export function Notifications() {
  const channels = useResource<NotificationChannelList>("/notifications");
  const outbox = useResource<{ items: NotificationOutboxItem[] }>(
    "/notifications/outbox?limit=50",
  );
  const [draft, setDraft] = useState<Draft>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  function setConfig(key: string, value: string) {
    setDraft((current) =>
      current
        ? { ...current, config: { ...current.config, [key]: value } }
        : current,
    );
  }

  function toggleEvent(event: string, checked: boolean) {
    setDraft((current) =>
      current
        ? {
            ...current,
            events: checked
              ? [...current.events.filter((item) => item !== event), event]
              : current.events.filter((item) => item !== event),
          }
        : current,
    );
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!draft || busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    const body: NotificationChannelInput = {
      name: draft.name.trim(),
      kind: draft.kind,
      enabled: draft.enabled,
      config: draft.config,
      events: draft.events,
      revision: draft.revision,
    };
    if (draft.secret) body.secret = draft.secret;
    try {
      if (draft.id) {
        await api(`/notifications/${draft.id}`, {
          method: "PUT",
          body: JSON.stringify(body),
        });
      } else {
        await api("/notifications", {
          method: "POST",
          body: JSON.stringify(body),
        });
      }
      setNotice("Notification channel saved.");
      setDraft(undefined);
      channels.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove(channel: NotificationChannel) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`/notifications/${channel.id}`, {
        method: "DELETE",
        body: "{}",
      });
      setNotice(`Removed “${channel.name}”.`);
      if (draft?.id === channel.id) setDraft(undefined);
      channels.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function test(channel: NotificationChannel) {
    if (busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api(`/notifications/${channel.id}/test`, {
        method: "POST",
        body: "{}",
      });
      setNotice(`Test notification sent through “${channel.name}”.`);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function retry(item: NotificationOutboxItem) {
    setBusy(true);
    setError("");
    try {
      await api(`/notifications/outbox/${item.id}/retry`, {
        method: "POST",
        body: "{}",
      });
      setNotice("Delivery queued for retry.");
      outbox.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  function updateDraft(patch: Partial<Draft>) {
    setDraft((current) => (current ? { ...current, ...patch } : current));
  }

  const items = channels.data?.items ?? [];
  const deliveries = outbox.data?.items ?? [];
  return (
    <PageBody>
      <PageHeading
        title="Notifications"
        description="Owner notifications for completed downloads, imports, requests and failures. Provider credentials are encrypted and delivery is durable across restarts."
      />
      <RequestState
        loading={channels.loading}
        error={channels.error}
        retry={channels.reload}
      />
      {error && <Alert kind="danger">{error}</Alert>}
      {notice && <Alert kind="success">{notice}</Alert>}
      <FieldSet
        legend={draft?.id ? "Edit channel" : "Add a channel"}
        description="Choose the events this channel should receive. Channels are independent; disabling one never affects another."
        actions={
          !draft ? (
            <Button type="button" onClick={() => setDraft(emptyDraft())}>
              <Bell size={16} aria-hidden="true" /> Add channel
            </Button>
          ) : undefined
        }
      >
        {draft && (
          <form onSubmit={(event) => void save(event)}>
            <FormGroup label="Name" htmlFor="notification-name">
              <TextField.Root
                id="notification-name"
                value={draft.name}
                maxLength={80}
                placeholder="Owner alerts"
                onChange={(event) => updateDraft({ name: event.target.value })}
              />
            </FormGroup>
            <FormGroup label="Provider" htmlFor="notification-kind">
              <select
                id="notification-kind"
                value={draft.kind}
                onChange={(event) => {
                  const kind = event.target.value as NotificationKind;
                  setDraft({ ...draft, kind, config: {} });
                }}
              >
                {(channels.data?.kinds ?? Object.keys(kindLabels)).map(
                  (kind) => (
                    <option key={kind} value={kind}>
                      {kindLabels[kind as NotificationKind] ?? kind}
                    </option>
                  ),
                )}
              </select>
            </FormGroup>
            <Check
              label="Enabled"
              description="Disabled channels keep their settings but receive no new events."
              checked={draft.enabled}
              onChange={(enabled) => updateDraft({ enabled })}
            />
            {draft.kind === "email" && (
              <>
                <FormGroup label="SMTP host" htmlFor="notification-host">
                  <TextField.Root
                    id="notification-host"
                    value={draft.config.host ?? ""}
                    placeholder="smtp.example.com"
                    onChange={(event) => setConfig("host", event.target.value)}
                  />
                </FormGroup>
                <FormGroup label="Port" htmlFor="notification-port">
                  <TextField.Root
                    id="notification-port"
                    value={draft.config.port ?? ""}
                    placeholder="587"
                    onChange={(event) => setConfig("port", event.target.value)}
                  />
                </FormGroup>
                <FormGroup label="Username" htmlFor="notification-username">
                  <TextField.Root
                    id="notification-username"
                    value={draft.config.username ?? ""}
                    onChange={(event) =>
                      setConfig("username", event.target.value)
                    }
                  />
                </FormGroup>
                <FormGroup label="From address" htmlFor="notification-from">
                  <TextField.Root
                    id="notification-from"
                    value={draft.config.from ?? ""}
                    placeholder="reelscarey@example.com"
                    onChange={(event) => setConfig("from", event.target.value)}
                  />
                </FormGroup>
                <FormGroup label="Recipients" htmlFor="notification-to">
                  <TextField.Root
                    id="notification-to"
                    value={draft.config.to ?? ""}
                    placeholder="owner@example.com"
                    onChange={(event) => setConfig("to", event.target.value)}
                  />
                </FormGroup>
                <Check
                  label="Use implicit TLS (port 465)"
                  checked={draft.config.use_tls === "true"}
                  onChange={(checked) =>
                    setConfig("use_tls", checked ? "true" : "")
                  }
                />
              </>
            )}
            {(draft.kind === "ntfy" ||
              draft.kind === "gotify" ||
              draft.kind === "discord" ||
              draft.kind === "webhook") && (
              <FormGroup label="URL" htmlFor="notification-url">
                <TextField.Root
                  id="notification-url"
                  value={draft.config.url ?? ""}
                  placeholder={
                    draft.kind === "ntfy"
                      ? "https://ntfy.example.com/topic"
                      : draft.kind === "gotify"
                        ? "https://gotify.example.com"
                        : "https://example.com/hook"
                  }
                  onChange={(event) => setConfig("url", event.target.value)}
                />
              </FormGroup>
            )}
            {draft.kind === "pushover" && (
              <FormGroup label="Pushover user key" htmlFor="notification-user">
                <TextField.Root
                  id="notification-user"
                  value={draft.config.user ?? ""}
                  onChange={(event) => setConfig("user", event.target.value)}
                />
              </FormGroup>
            )}
            {draft.kind === "telegram" && (
              <FormGroup label="Telegram chat ID" htmlFor="notification-chat">
                <TextField.Root
                  id="notification-chat"
                  value={draft.config.chat_id ?? ""}
                  onChange={(event) => setConfig("chat_id", event.target.value)}
                />
              </FormGroup>
            )}
            <FormGroup
              label={
                draft.kind === "email"
                  ? "SMTP password"
                  : draft.kind === "telegram"
                    ? "Bot token"
                    : draft.kind === "pushover"
                      ? "Application token"
                      : "Provider token or webhook secret"
              }
              htmlFor="notification-secret"
              help="Leave empty to keep the saved secret. Secrets are encrypted with the instance key."
            >
              <TextField.Root
                id="notification-secret"
                type="password"
                value={draft.secret}
                onChange={(event) =>
                  updateDraft({ secret: event.target.value })
                }
              />
            </FormGroup>
            <FormGroup label="Events">
              <div className="rs-check-grid">
                {(channels.data?.events ?? []).map((event) => (
                  <Check
                    key={event}
                    label={eventLabels[event] ?? event}
                    checked={draft.events.includes(event)}
                    onChange={(checked) => toggleEvent(event, checked)}
                  />
                ))}
              </div>
            </FormGroup>
            <FormActions>
              <Button
                type="submit"
                disabled={
                  busy || !draft.name.trim() || draft.events.length === 0
                }
              >
                Save channel
              </Button>
              <Button
                type="button"
                variant="soft"
                onClick={() => setDraft(undefined)}
              >
                Cancel
              </Button>
            </FormActions>
          </form>
        )}
      </FieldSet>
      {channels.data && items.length === 0 && (
        <EmptyState icon={Bell} title="No notification channels">
          Add email, ntfy, Gotify, Pushover, Telegram, Discord or a signed
          webhook to hear about completed downloads, imports and failures.
        </EmptyState>
      )}
      {items.length > 0 && (
        <Table label="Notification channels">
          <thead>
            <tr>
              <th>Name</th>
              <th>Provider</th>
              <th>Events</th>
              <th>State</th>
              <th aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {items.map((channel) => (
              <tr key={channel.id}>
                <td>{channel.name}</td>
                <td>{kindLabels[channel.kind]}</td>
                <td>
                  {channel.events.map((event) => (
                    <Badge
                      key={event}
                      variant="soft"
                      style={{ marginRight: 4 }}
                    >
                      {eventLabels[event] ?? event}
                    </Badge>
                  ))}
                </td>
                <td>{channel.enabled ? "Enabled" : "Disabled"}</td>
                <td>
                  <div className="rs-actions">
                    <Button
                      type="button"
                      size="1"
                      variant="soft"
                      disabled={busy}
                      onClick={() => void test(channel)}
                    >
                      <Send size={14} aria-hidden="true" /> Test
                    </Button>
                    <Button
                      type="button"
                      size="1"
                      variant="soft"
                      onClick={() => setDraft(draftFrom(channel))}
                    >
                      <Pencil size={14} aria-hidden="true" /> Edit
                    </Button>
                    <Button
                      type="button"
                      size="1"
                      color="red"
                      variant="soft"
                      disabled={busy}
                      onClick={() => void remove(channel)}
                    >
                      <Trash2 size={14} aria-hidden="true" /> Remove
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <FieldSet
        legend="Recent deliveries"
        description="A delivery marked uncertain may have reached the provider. Retrying can send a duplicate, so it is never automatic."
      >
        <RequestState
          loading={outbox.loading}
          error={outbox.error}
          retry={outbox.reload}
        />
        {!outbox.loading && deliveries.length === 0 && (
          <p className="rs-muted">
            No notification deliveries have been recorded.
          </p>
        )}
        {deliveries.length > 0 && (
          <Table label="Recent notification deliveries">
            <thead>
              <tr>
                <th>Event</th>
                <th>Title</th>
                <th>State</th>
                <th>Attempts</th>
                <th>Created</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {deliveries.map((item) => (
                <tr key={item.id}>
                  <td>{eventLabels[item.event] ?? item.event}</td>
                  <td>{item.title}</td>
                  <td>
                    {item.state}
                    {item.last_error ? (
                      <span className="rs-muted rs-small">
                        {" "}
                        — {item.last_error}
                      </span>
                    ) : null}
                  </td>
                  <td>{item.attempts}</td>
                  <td>{formatTimestamp(item.created_at)}</td>
                  <td>
                    {(item.state === "uncertain" ||
                      item.state === "failed") && (
                      <Button
                        type="button"
                        size="1"
                        variant="soft"
                        disabled={busy}
                        onClick={() => void retry(item)}
                      >
                        Retry
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </FieldSet>
    </PageBody>
  );
}
