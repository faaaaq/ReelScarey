import { useEffect, useId, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { RefreshCw } from "lucide-react";
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
  WorkInProgress,
} from "../ui";
import type { Kind } from "../ui";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { Pagination } from "../movieComponents";
import { useLiveResource } from "../useLiveResource";
import { SubtitleEdits } from "./SubtitleEdits";
import "./Subtitles.css";

export type SubtitleTrack = {
  language: string;
  variant: "full" | "signs" | "forced";
};
export type SubtitlePreferences = {
  download_format?: "srt" | "original";
  enabled: boolean;
  tracks: SubtitleTrack[];
  hearing_impaired: "any" | "exclude" | "prefer";
  match_policy: "hash" | "hash_or_release";
  interval_minutes: number;
  only_monitored: boolean;
  revision: number;
};
export type SubtitleProvider = {
  provider?: "opensubtitles" | "subdl";
  configured: boolean;
  username: string;
  user_agent: string;
  state: string;
  message: string;
  cooldown_until: number;
  remaining: number | null;
};
type Language = { code: string; name: string };
type PreferenceResponse = {
  preferences: SubtitlePreferences;
  languages: Language[];
};
export type SubtitleCandidate = SubtitleTrack & {
  format?: string;
  id: string;
  file_id: number;
  release: string;
  file_name: string;
  hearing_impaired: boolean;
  hash_match: boolean;
  trusted: boolean;
  automatic: boolean;
  can_download: boolean;
  score: number;
  reasons: string[];
};
export type SubtitleFile = {
  id: number;
  media_id: number;
  kind: string;
  title: string;
  path: string;
  episodes: string;
  tracks: (SubtitleTrack & {
    provider?: string;
    source: string;
    path: string;
    state: string;
    message: string;
    updated_at: number;
  })[];
  job: null | {
    state: string;
    message: string;
    mode: string;
    requested: boolean;
    next_run: number;
    updated_at: number;
  };
  candidates?: SubtitleCandidate[];
  wanted_tracks?: SubtitleTrack[];
};
const variantName: Record<SubtitleTrack["variant"], string> = {
  full: "Full subtitles",
  signs: "Signs / songs",
  forced: "Forced",
};
const variants = ["full", "signs", "forced"] as const;
const jobStates: Record<string, { label: string; kind: Kind }> = {
  queued: { label: "Queued", kind: "info" },
  running: { label: "Downloading", kind: "primary" },
  retrying: { label: "Retrying", kind: "warning" },
  waiting_provider: { label: "Waiting for provider", kind: "warning" },
  paused: { label: "Paused", kind: "disabled" },
  blocked: { label: "Failed", kind: "danger" },
  needs_review: { label: "Awaiting review", kind: "warning" },
  no_match: { label: "No match", kind: "disabled" },
  completed: { label: "Done", kind: "success" },
  cancelled: { label: "Cancelled", kind: "disabled" },
};
function readable(value: string) {
  return value.replaceAll("_", " ");
}
function capitalize(value: string) {
  const text = readable(value);
  return text ? text[0].toUpperCase() + text.slice(1) : text;
}
function time(value: number) {
  return value ? new Date(value * 1000).toLocaleString() : "Not checked yet";
}
function providerKind(state: string): Kind {
  if (state === "ready") return "success";
  if (state === "not_connected") return "disabled";
  if (state.includes("quota") || state.includes("rate_limit")) return "warning";
  if (state.includes("auth") || state.includes("access")) return "danger";
  return "info";
}
function mediaPath(file: SubtitleFile) {
  return file.kind === "series"
    ? `/series/${file.media_id}`
    : `/movies/${file.media_id}`;
}
function languageName(languages: Language[], code: string) {
  return (
    languages.find((language) => language.code === code)?.name ??
    (code === "und" ? "Unknown language" : code)
  );
}

export function Subtitles() {
  const prefs = useLiveResource<PreferenceResponse>("/subtitles/preferences");
  const provider = useLiveResource<SubtitleProvider>(
    "/providers/opensubtitles",
    10000,
  );
  const providerLabel =
    provider.data?.provider === "subdl" ? "SubDL" : "OpenSubtitles";
  return (
    <>
      <WorkInProgress noticeKey="work-in-progress:subtitles">
        <p>
          Subtitle checks, provider downloads and track choices are still being
          built out. Existing subtitle files and saved preferences are kept, and
          downloads only start after you request them.
        </p>
      </WorkInProgress>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={prefs.loading || provider.loading}
            onClick={() => {
              prefs.reload();
              provider.reload();
            }}
          />
        </ToolbarSection>
        <ToolbarSection align="right">
          <span className="sub-status" role="status">
            {provider.data && (
              <Label kind={provider.data.configured ? "success" : "disabled"}>
                {providerLabel}{" "}
                {provider.data.configured ? "configured" : "not connected"}
              </Label>
            )}
          </span>
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title="Subtitles" />
        <RequestState
          loading={prefs.loading || provider.loading}
          error={prefs.error || provider.error}
          retry={() => {
            prefs.reload();
            provider.reload();
          }}
        />
        {prefs.data && provider.data && (
          <>
            <ProviderSection
              key={provider.data.provider}
              provider={provider.data}
              reload={provider.reload}
            />
            <Preferences
              data={prefs.data}
              provider={provider.data}
              saved={prefs.reload}
            />
            <MediaFiles
              languages={prefs.data.languages}
              wanted={prefs.data.preferences.tracks}
              provider={provider.data}
            />
          </>
        )}
      </PageBody>
    </>
  );
}

function ProviderSection({
  provider,
  reload,
}: {
  provider: SubtitleProvider;
  reload: () => void;
}) {
  const [username, setUsername] = useState(provider.username);
  const [selectedProvider, setSelectedProvider] = useState(
    provider.provider || "opensubtitles",
  );
  const providerLabel =
    selectedProvider === "subdl" ? "SubDL" : "OpenSubtitles";
  const changedProvider =
    selectedProvider !== (provider.provider || "opensubtitles");
  const [agent, setAgent] = useState(
    provider.user_agent || "ReelScarey v0.7.0",
  );
  const [key, setKey] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const providerID = useId();
  const usernameID = useId();
  const passwordID = useId();
  const keyID = useId();
  const agentID = useId();
  async function action(kind: "save" | "test" | "remove") {
    setBusy(true);
    setError("");
    setSuccess("");
    try {
      if (kind === "save") {
        await api("/providers/opensubtitles", {
          method: "PUT",
          body: JSON.stringify({
            provider: selectedProvider,
            username,
            user_agent: agent,
            ...(key ? { api_key: key } : {}),
            ...(password ? { password } : {}),
          }),
        });
        setKey("");
        setPassword("");
        setSuccess(`${providerLabel} connection verified and saved.`);
      } else if (kind === "test") {
        await api("/providers/opensubtitles/test", {
          method: "POST",
          body: "{}",
        });
        setSuccess("Saved subtitle connection verified.");
      } else {
        await api("/providers/opensubtitles", { method: "DELETE" });
        setKey("");
        setPassword("");
        setSuccess(
          "Subtitle provider disconnected. Existing subtitles remain.",
        );
      }
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <FieldSet
      legend="Provider"
      actions={
        <a
          className="rs-link-button rs-small"
          href={
            selectedProvider === "subdl"
              ? "https://subdl.com/panel"
              : "https://www.opensubtitles.com/"
          }
          target="_blank"
          rel="noreferrer"
        >
          Open {providerLabel}
        </a>
      }
    >
      {provider.configured && (
        <div className="sub-provider-state">
          <Label kind={providerKind(provider.state)}>
            {capitalize(provider.state)}
          </Label>
          {!provider.state.includes("quota") &&
            !provider.state.includes("rate_limit") && (
              <span className="sub-provider-message">{provider.message}</span>
            )}
          {provider.remaining !== null && (
            <span className="sub-muted">
              Provider reports {provider.remaining} downloads remaining.
            </span>
          )}
        </div>
      )}
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void action("save");
        }}
      >
        <fieldset disabled={busy} className="rs-stack">
          <FormGroup
            label="Provider"
            htmlFor={providerID}
            help="Changing providers clears saved subtitle choices."
          >
            <select
              id={providerID}
              value={selectedProvider}
              onChange={(event) => {
                setSelectedProvider(
                  event.target.value as "opensubtitles" | "subdl",
                );
                setKey("");
                setPassword("");
              }}
            >
              <option value="opensubtitles">OpenSubtitles</option>
              <option value="subdl">SubDL</option>
            </select>
          </FormGroup>
          {selectedProvider === "opensubtitles" && (
            <>
              <FormGroup label="OpenSubtitles username" htmlFor={usernameID}>
                <TextField.Root
                  id={usernameID}
                  autoComplete="username"
                  required
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  maxLength={200}
                />
              </FormGroup>
              <FormGroup label="OpenSubtitles password" htmlFor={passwordID}>
                <TextField.Root
                  id={passwordID}
                  type="password"
                  autoComplete="new-password"
                  required={!provider.configured || changedProvider}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder={
                    provider.configured
                      ? "Leave blank to keep saved password"
                      : "Account password"
                  }
                  maxLength={2048}
                />
              </FormGroup>
            </>
          )}
          <FormGroup
            label="Application API key"
            htmlFor={keyID}
            help={
              selectedProvider === "subdl"
                ? "Every SubDL download requires review; choose Keep original for styled ASS, SSA or VTT."
                : "Account download limits apply; ReelScarey waits during a provider quota or cooldown."
            }
          >
            <TextField.Root
              id={keyID}
              type="password"
              autoComplete="off"
              required={!provider.configured || changedProvider}
              value={key}
              onChange={(event) => setKey(event.target.value)}
              placeholder={
                provider.configured
                  ? "Leave blank to keep saved key"
                  : "Your application API key"
              }
              maxLength={2048}
            />
          </FormGroup>
          {selectedProvider === "opensubtitles" && (
            <FormGroup
              label="Registered application name & version"
              htmlFor={agentID}
            >
              <TextField.Root
                id={agentID}
                required
                value={agent}
                onChange={(event) => setAgent(event.target.value)}
                maxLength={120}
              />
            </FormGroup>
          )}
          <div className="sub-form-actions">
            <FormActions>
              <Button type="submit">Save & test {providerLabel}</Button>
              {provider.configured && (
                <>
                  <Button
                    type="button"
                    variant="soft"
                    onClick={() => void action("test")}
                  >
                    Test saved connection
                  </Button>
                  <ConfirmDialog
                    trigger={
                      <Button type="button" variant="ghost" color="red">
                        Disconnect
                      </Button>
                    }
                    title={`Disconnect ${providerLabel}?`}
                    description="This removes the saved subtitle provider connection and its stored credentials, including the account password and application API key. Existing subtitle files stay in the library; automatic downloads stop until a provider is connected again."
                    confirmLabel="Disconnect"
                    onConfirm={() => action("remove")}
                  />
                </>
              )}
            </FormActions>
          </div>
        </fieldset>
        {error && <Alert kind="danger">{error}</Alert>}
        {success && <Alert kind="success">{success}</Alert>}
      </form>
    </FieldSet>
  );
}

type PreferenceSection = "tracks" | "matching";
type PreferenceFeedback = {
  section: PreferenceSection;
  kind: "success" | "danger";
  text: string;
} | null;

function Preferences({
  data,
  provider,
  saved,
}: {
  data: PreferenceResponse;
  provider: SubtitleProvider;
  saved: () => void;
}) {
  const [form, setForm] = useState(data.preferences);
  const [adding, setAdding] = useState("es");
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState<PreferenceFeedback>(null);
  const dirty = JSON.stringify(form) !== JSON.stringify(data.preferences);
  useEffect(() => {
    if (!dirty) return;
    function handler(event: BeforeUnloadEvent) {
      event.preventDefault();
      event.returnValue = "";
    }
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [dirty]);
  const addID = useId();
  const formatID = useId();
  const timingID = useId();
  const hearingID = useId();
  const intervalID = useId();
  const selectedLanguages = [...new Set(form.tracks.map((t) => t.language))];
  const addable = data.languages.filter(
    (language) => !selectedLanguages.includes(language.code),
  );
  const addLanguage = addable.some((language) => language.code === adding)
    ? adding
    : (addable[0]?.code ?? "");
  function update(next: SubtitlePreferences) {
    setForm(next);
    setFeedback(null);
  }
  function toggle(
    language: string,
    variant: SubtitleTrack["variant"],
    checked: boolean,
  ) {
    update({
      ...form,
      tracks: checked
        ? [...form.tracks, { language, variant }]
        : form.tracks.filter(
            (track) => track.language !== language || track.variant !== variant,
          ),
    });
  }
  async function save(section: PreferenceSection) {
    setBusy(true);
    setFeedback(null);
    try {
      const result = await api<PreferenceResponse>("/subtitles/preferences", {
        method: "PUT",
        body: JSON.stringify(form),
      });
      setForm(result.preferences);
      setFeedback({
        section,
        kind: "success",
        text: "Subtitle preferences saved.",
      });
      saved();
    } catch (e) {
      setFeedback({ section, kind: "danger", text: messageFor(e) });
    } finally {
      setBusy(false);
    }
  }
  async function reset(section: PreferenceSection) {
    setBusy(true);
    setFeedback(null);
    try {
      const result = await api<PreferenceResponse>("/subtitles/preferences");
      setForm(result.preferences);
      setFeedback({
        section,
        kind: "success",
        text: "Loaded the saved subtitle preferences.",
      });
      saved();
    } catch (e) {
      setFeedback({ section, kind: "danger", text: messageFor(e) });
    } finally {
      setBusy(false);
    }
  }
  function status(section: PreferenceSection) {
    if (!feedback || feedback.section !== section) return null;
    return <Alert kind={feedback.kind}>{feedback.text}</Alert>;
  }
  return (
    <>
      <FieldSet
        legend="Languages & tracks"
        description="Up to three languages; each can request full, signs/songs and forced tracks."
      >
        <form
          onSubmit={(event: FormEvent) => {
            event.preventDefault();
            void save("tracks");
          }}
        >
          <fieldset disabled={busy} className="rs-stack">
            <Table label="Subtitle languages and tracks">
              <thead>
                <tr>
                  <th scope="col">Language</th>
                  {variants.map((variant) => (
                    <th scope="col" key={variant} className="is-shrink">
                      {variantName[variant]}
                    </th>
                  ))}
                  <th scope="col" className="is-actions">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {selectedLanguages.map((code) => {
                  const name = languageName(data.languages, code);
                  return (
                    <tr key={code}>
                      <th scope="row">{name}</th>
                      {variants.map((variant) => {
                        const checked = form.tracks.some(
                          (track) =>
                            track.language === code &&
                            track.variant === variant,
                        );
                        return (
                          <td key={variant} className="is-shrink">
                            <input
                              type="checkbox"
                              aria-label={`${name} ${variantName[variant]}`}
                              checked={checked}
                              disabled={form.tracks.length === 1 && checked}
                              onChange={(event) =>
                                toggle(code, variant, event.target.checked)
                              }
                            />
                          </td>
                        );
                      })}
                      <td className="is-actions">
                        <Button
                          type="button"
                          size="1"
                          variant="ghost"
                          color="gray"
                          aria-label={`Remove ${name}`}
                          disabled={selectedLanguages.length === 1}
                          onClick={() =>
                            update({
                              ...form,
                              tracks: form.tracks.filter(
                                (track) => track.language !== code,
                              ),
                            })
                          }
                        >
                          Remove
                        </Button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </Table>
            <div className="sub-add-language">
              <label htmlFor={addID}>Add a language</label>
              <select
                id={addID}
                value={addLanguage}
                disabled={selectedLanguages.length >= 3 || addable.length === 0}
                onChange={(event) => setAdding(event.target.value)}
              >
                {addable.map((language) => (
                  <option key={language.code} value={language.code}>
                    {language.name}
                  </option>
                ))}
              </select>
              <Button
                type="button"
                variant="soft"
                disabled={
                  selectedLanguages.length >= 3 ||
                  !addLanguage ||
                  selectedLanguages.includes(addLanguage)
                }
                onClick={() =>
                  update({
                    ...form,
                    tracks: [
                      ...form.tracks,
                      { language: addLanguage, variant: "full" },
                    ],
                  })
                }
              >
                Add language
              </Button>
            </div>
            <FormGroup
              label="Download format"
              htmlFor={formatID}
              help="Keep original preserves provider styling for ASS, SSA and VTT; existing sidecars are never replaced."
            >
              <select
                id={formatID}
                value={form.download_format ?? "srt"}
                onChange={(event) =>
                  update({
                    ...form,
                    download_format: event.target.value as "srt" | "original",
                  })
                }
              >
                <option value="srt">SRT (compatible text)</option>
                <option value="original">
                  Keep original SRT, VTT, ASS or SSA
                </option>
              </select>
            </FormGroup>
            <div className="sub-form-actions">
              <FormActions>
                <Button type="submit" disabled={form.tracks.length === 0}>
                  Save languages & tracks
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => void reset("tracks")}
                >
                  Discard changes
                </Button>
              </FormActions>
            </div>
          </fieldset>
          {status("tracks")}
        </form>
      </FieldSet>
      <FieldSet
        legend="Automatic matching"
        description={
          provider.configured
            ? undefined
            : "Preferences can be saved now; automatic work waits until a provider is connected."
        }
      >
        <form
          onSubmit={(event: FormEvent) => {
            event.preventDefault();
            void save("matching");
          }}
        >
          <fieldset disabled={busy} className="rs-stack">
            <FormGroup label="Automatic">
              <Check
                label="Automatically download matching subtitles"
                checked={form.enabled}
                onChange={(enabled) => update({ ...form, enabled })}
              />
            </FormGroup>
            <FormGroup
              label="Automatic timing match"
              htmlFor={timingID}
              help="Hash only accepts provider hash matches; the exact release option also accepts the original release name."
            >
              <select
                id={timingID}
                value={form.match_policy}
                onChange={(event) =>
                  update({
                    ...form,
                    match_policy: event.target
                      .value as SubtitlePreferences["match_policy"],
                  })
                }
              >
                <option value="hash_or_release">
                  File hash or exact release
                </option>
                <option value="hash">File hash only</option>
              </select>
            </FormGroup>
            <FormGroup label="Hearing-impaired subtitles" htmlFor={hearingID}>
              <select
                id={hearingID}
                value={form.hearing_impaired}
                onChange={(event) =>
                  update({
                    ...form,
                    hearing_impaired: event.target
                      .value as SubtitlePreferences["hearing_impaired"],
                  })
                }
              >
                <option value="any">Allow standard and SDH tracks</option>
                <option value="exclude">Exclude SDH tracks</option>
                <option value="prefer">Prefer SDH tracks</option>
              </select>
            </FormGroup>
            <FormGroup
              label="Check missing tracks every (minutes)"
              htmlFor={intervalID}
            >
              <input
                id={intervalID}
                type="number"
                required
                min={60}
                max={10080}
                value={form.interval_minutes}
                onChange={(event) =>
                  update({
                    ...form,
                    interval_minutes: Number(event.target.value),
                  })
                }
              />
            </FormGroup>
            <FormGroup label="Scope">
              <Check
                label="Only check monitored items"
                checked={form.only_monitored}
                onChange={(only_monitored) =>
                  update({ ...form, only_monitored })
                }
              />
            </FormGroup>
            <details className="sub-details">
              <summary>How tracks are matched</summary>
              <p>
                Existing embedded tracks and same-name sidecars are checked
                first. Provider machine translations are excluded. A signs label
                takes precedence, so one provider file is never used for two
                track types. Signs/songs and forced tracks depend on what the
                provider supplies.
              </p>
            </details>
            <div className="sub-form-actions">
              <FormActions>
                <Button type="submit" disabled={form.tracks.length === 0}>
                  Save matching rules
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => void reset("matching")}
                >
                  Discard changes
                </Button>
              </FormActions>
            </div>
          </fieldset>
          {status("matching")}
        </form>
      </FieldSet>
    </>
  );
}

function MediaFiles({
  languages,
  wanted,
  provider,
}: {
  languages: Language[];
  wanted: SubtitleTrack[];
  provider: SubtitleProvider;
}) {
  const [page, setPage] = useState(1);
  const quota =
    provider.configured &&
    (provider.cooldown_until * 1000 > Date.now() ||
      provider.state === "provider_quota" ||
      provider.state === "provider_rate_limit");
  return (
    <FieldSet
      legend="Media files"
      description="Registered imports and their subtitle jobs."
    >
      {quota && (
        <Alert kind="warning" title="Provider cooldown">
          {provider.message}{" "}
          {provider.cooldown_until > 0 &&
            `Retry after ${time(provider.cooldown_until)}.`}
        </Alert>
      )}
      <FilePage
        key={page}
        page={page}
        setPage={setPage}
        languages={languages}
        wanted={wanted}
      />
    </FieldSet>
  );
}

function FilePage({
  page,
  setPage,
  languages,
  wanted,
}: {
  page: number;
  setPage: (n: number) => void;
  languages: Language[];
  wanted: SubtitleTrack[];
}) {
  const [editing, setEditing] = useState<SubtitleFile | null>(null);
  const files = useLiveResource<{
    items: SubtitleFile[];
    page: number;
    total: number;
    limit: number;
  }>(`/subtitles/files?page=${page}`, 5000);
  function changePage(next: number) {
    setEditing(null);
    setPage(next);
  }
  return (
    <>
      <RequestState
        loading={files.loading}
        error={files.error}
        retry={files.reload}
      />
      {files.data?.items.length === 0 && (
        <EmptyState
          title="Subtitles follow your imported media."
          actions={
            <Button asChild variant="soft">
              <Link to="/movies">Open library</Link>
            </Button>
          }
        >
          <p>
            Imported movies and episodes appear here for existing-track checks,
            provider search and downloads.
          </p>
        </EmptyState>
      )}
      {files.data && files.data.items.length > 0 && (
        <Table label="Subtitle media files">
          <thead>
            <tr>
              <th scope="col">File</th>
              <th scope="col">Tracks</th>
              <th scope="col">Status</th>
              <th scope="col" className="is-actions">
                Actions
              </th>
            </tr>
          </thead>
          <tbody>
            {files.data.items.map((file) => (
              <FileRow
                key={file.id}
                file={file}
                languages={languages}
                wanted={wanted}
                reload={files.reload}
                onEdit={() => setEditing(file)}
              />
            ))}
          </tbody>
        </Table>
      )}
      {editing && (
        <section
          className="sub-edits-section"
          aria-label={`Subtitle edits for ${editing.title}`}
        >
          <div className="sub-edits-head">
            <span className="sub-muted">
              {editing.title}
              {editing.episodes && ` · ${editing.episodes}`}
            </span>
            <Button
              size="1"
              variant="ghost"
              color="gray"
              onClick={() => setEditing(null)}
            >
              Close edits
            </Button>
          </div>
          <SubtitleEdits
            embedded
            fileID={editing.id}
            tracks={editing.tracks}
            reload={files.reload}
          />
        </section>
      )}
      {files.data && (
        <Pagination
          page={page}
          pages={Math.ceil(files.data.total / Math.max(1, files.data.limit))}
          onPage={changePage}
        />
      )}
    </>
  );
}

function FileRow({
  file,
  languages,
  wanted,
  reload,
  onEdit,
}: {
  file: SubtitleFile;
  languages: Language[];
  wanted: SubtitleTrack[];
  reload: () => void;
  onEdit: () => void;
}) {
  const [reviewOpen, setReviewOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const active =
    file.job &&
    (file.job.state === "running" ||
      (file.job.requested && ["queued", "retrying"].includes(file.job.state)));
  const desired = file.wanted_tracks ?? wanted;
  const available = desired.filter((track) =>
    file.tracks.some(
      (saved) =>
        saved.language === track.language &&
        saved.variant === track.variant &&
        saved.state === "available",
    ),
  ).length;
  const state = file.job ? jobStates[file.job.state] : undefined;
  const editable = file.tracks.some(
    (track) => track.source !== "embedded" && !!track.path,
  );
  async function request(action: "search" | "retry") {
    setBusy(true);
    setError("");
    try {
      await api(`/subtitles/files/${file.id}/${action}`, {
        method: "POST",
        body: "{}",
      });
      if (action === "search") setReviewOpen(true);
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <tr>
      <td>
        <div className="sub-file">
          <Link className="sub-file-title" to={mediaPath(file)}>
            {file.title}
            {file.episodes && ` · ${file.episodes}`}
          </Link>
          <span className="sub-file-path">{file.path}</span>
        </div>
      </td>
      <td>
        {file.tracks.length > 0 ? (
          <div className="sub-tracks">
            {file.tracks.map((track) => (
              <Label
                key={`${track.language}-${track.variant}`}
                kind={track.state === "available" ? "success" : "warning"}
                title={track.message}
              >
                {languageName(languages, track.language)} ·{" "}
                {variantName[track.variant]}
              </Label>
            ))}
          </div>
        ) : (
          <span className="sub-muted">None found</span>
        )}
        <div className="sub-muted">
          {available} of {desired.length} requested tracks
        </div>
      </td>
      <td>
        {file.job ? (
          <div className="sub-job">
            <Label kind={state?.kind ?? "default"}>
              {state?.label ?? capitalize(file.job.state)}
            </Label>
            <span className="sub-job-message">{file.job.message}</span>
            <span className="sub-job-time">
              Last checked {time(file.job.updated_at)}
            </span>
            {file.job.next_run > 0 && (
              <span className="sub-job-time">
                Next check {time(file.job.next_run)}
              </span>
            )}
          </div>
        ) : (
          <span className="sub-muted">Not checked yet</span>
        )}
      </td>
      <td className="is-actions">
        <div className="sub-actions">
          <Button
            size="1"
            variant="soft"
            aria-label={`Search subtitles for ${file.title}`}
            disabled={!!active || busy}
            onClick={() => void request("search")}
          >
            Search
          </Button>
          <Button
            size="1"
            variant="soft"
            aria-label="Review matches"
            onClick={() => setReviewOpen(true)}
          >
            Review
          </Button>
          <Button
            size="1"
            variant="ghost"
            aria-label="Edit subtitle"
            disabled={!editable}
            onClick={onEdit}
          >
            Edit
          </Button>
          {file.job &&
            ["blocked", "waiting_provider", "retrying", "paused"].includes(
              file.job.state,
            ) && (
              <Button
                size="1"
                variant="ghost"
                aria-label="Retry subtitle job"
                disabled={busy || file.job.state === "running"}
                onClick={() => void request("retry")}
              >
                Retry
              </Button>
            )}
        </div>
        {error && (
          <span role="alert" className="sub-row-error">
            {error}
          </span>
        )}
        <Modal
          open={reviewOpen}
          onOpenChange={setReviewOpen}
          title="Review subtitle matches"
          description={`${file.title}${file.episodes ? ` · ${file.episodes}` : ""}`}
          size="large"
          footer={
            <ModalClose>
              <Button variant="soft">Close</Button>
            </ModalClose>
          }
        >
          {reviewOpen && (
            <Matches
              key={file.id}
              id={file.id}
              languages={languages}
              active={!!active}
              reload={reload}
            />
          )}
        </Modal>
      </td>
    </tr>
  );
}

function Matches({
  id,
  languages,
  active,
  reload,
}: {
  id: number;
  languages: Language[];
  active: boolean;
  reload: () => void;
}) {
  const result = useLiveResource<SubtitleFile>(`/subtitles/files/${id}`, 5000);
  return (
    <div>
      <RequestState
        loading={result.loading}
        error={result.error}
        retry={result.reload}
      />
      {result.data?.candidates?.length === 0 && (
        <p className="rs-muted">
          No saved matches. Results expire after 30 minutes.
        </p>
      )}
      {result.data?.candidates?.map((candidate) => (
        <Candidate
          key={candidate.id}
          candidate={candidate}
          languages={languages}
          active={active}
          queued={() => {
            result.reload();
            reload();
          }}
        />
      ))}
    </div>
  );
}

function Candidate({
  candidate,
  languages,
  active,
  queued,
}: {
  candidate: SubtitleCandidate;
  languages: Language[];
  active: boolean;
  queued: () => void;
}) {
  const [reviewed, setReviewed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);
  async function download() {
    setBusy(true);
    setError("");
    try {
      await api(`/subtitles/candidates/${candidate.id}/download`, {
        method: "POST",
        body: JSON.stringify({ reviewed }),
      });
      setSuccess(true);
      setReviewed(false);
      queued();
    } catch (e) {
      setError(messageFor(e));
      setReviewed(false);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="sub-candidate">
      <div className="sub-candidate-head">
        <strong>{candidate.release}</strong>
        <Label
          kind={
            candidate.hash_match
              ? "success"
              : candidate.automatic
                ? "info"
                : "warning"
          }
        >
          {candidate.hash_match
            ? "File hash match"
            : candidate.automatic
              ? "Release match"
              : "Review timing"}
        </Label>
      </div>
      <span className="sub-candidate-meta">
        {languageName(languages, candidate.language)} ·{" "}
        {variantName[candidate.variant]}
        {candidate.hearing_impaired ? " · SDH" : ""} · {candidate.file_name}
        {" · Output: "}
        {(candidate.format ?? "srt").toUpperCase() || "Unsupported"}
      </span>
      {candidate.reasons.length > 0 && (
        <ul className="sub-candidate-reasons">
          {candidate.reasons.map((reason, index) => (
            <li key={index}>{reason}</li>
          ))}
        </ul>
      )}
      {candidate.can_download && (
        <div className="sub-candidate-actions">
          <Check
            label="I reviewed the language, track type and timing match"
            checked={reviewed}
            disabled={active || busy || success}
            onChange={setReviewed}
          />
          <Button
            disabled={!reviewed || active || busy || success}
            onClick={() => void download()}
          >
            Download subtitle
          </Button>
        </div>
      )}
      {error && <Alert kind="danger">{error}</Alert>}
      {success && <Alert kind="success">Subtitle download queued.</Alert>}
    </div>
  );
}
