import { PageBody } from "../ui";
import { useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { Pagination } from "../movieComponents";
import { useLiveResource } from "../useLiveResource";
import { SubtitleEdits } from "./SubtitleEdits";
import "./Admin.css";

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
function readable(value: string) {
  return value.replaceAll("_", " ");
}
function time(value: number) {
  return value ? new Date(value * 1000).toLocaleString() : "Not checked yet";
}

export function Subtitles() {
  const prefs = useLiveResource<PreferenceResponse>("/subtitles/preferences");
  const provider = useLiveResource<SubtitleProvider>(
    "/providers/opensubtitles",
    10000,
  );
  return (
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
          <div className="subtitle-setup-grid">
            <PreferenceForm
              data={prefs.data}
              provider={provider.data}
              saved={prefs.reload}
            />
            <ProviderForm
              key={provider.data.provider}
              provider={provider.data}
              reload={provider.reload}
            />
          </div>
          <MediaFiles
            languages={prefs.data.languages}
            wanted={prefs.data.preferences.tracks}
          />
        </>
      )}
    </PageBody>
  );
}
function PreferenceForm({
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
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);
  const selectedLanguages = [...new Set(form.tracks.map((t) => t.language))];
  const addable = data.languages.filter(
    (l) => !selectedLanguages.includes(l.code),
  );
  const addLanguage = addable.some((l) => l.code === adding)
    ? adding
    : (addable[0]?.code ?? "");
  function toggle(
    language: string,
    variant: SubtitleTrack["variant"],
    checked: boolean,
  ) {
    setSuccess(false);
    setForm({
      ...form,
      tracks: checked
        ? [...form.tracks, { language, variant }]
        : form.tracks.filter(
            (t) => t.language !== language || t.variant !== variant,
          ),
    });
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setSuccess(false);
    try {
      const result = await api<PreferenceResponse>("/subtitles/preferences", {
        method: "PUT",
        body: JSON.stringify(form),
      });
      setForm(result.preferences);
      setSuccess(true);
      saved();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function reloadSaved() {
    setBusy(true);
    setError("");
    setSuccess(false);
    try {
      const result = await api<PreferenceResponse>("/subtitles/preferences");
      setForm(result.preferences);
      saved();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form
      className="rs-panel automation-panel subtitle-preferences"
      onSubmit={save}
    >
      <div className="rs-panel-header">
        <h2>Language & track preferences</h2>
        <Badge color={data.preferences.enabled ? "green" : "gray"}>
          {data.preferences.enabled ? "Automatic" : "Manual"}
        </Badge>
      </div>
      <fieldset disabled={busy} className="automation-fields">
        <label className="rs-check">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(e) => {
              setForm({ ...form, enabled: e.target.checked });
              setSuccess(false);
            }}
          />
          Automatically download matching subtitles
        </label>
        {!provider.configured && (
          <p className="automation-notice">
            Save your preferences now. Automatic downloads will wait for
            OpenSubtitles; Torznab is not required.
          </p>
        )}
        <div className="subtitle-language-list">
          {selectedLanguages.map((code) => (
            <div key={code} className="subtitle-language">
              <strong>
                {data.languages.find((l) => l.code === code)?.name ?? code}
              </strong>
              <div className="quality-options">
                {(["full", "signs", "forced"] as const).map((variant) => (
                  <label key={variant} className="rs-check">
                    <input
                      type="checkbox"
                      aria-label={`${data.languages.find((l) => l.code === code)?.name ?? code} ${variantName[variant]}`}
                      checked={form.tracks.some(
                        (t) => t.language === code && t.variant === variant,
                      )}
                      disabled={
                        form.tracks.length === 1 &&
                        form.tracks[0].language === code &&
                        form.tracks[0].variant === variant
                      }
                      onChange={(e) => toggle(code, variant, e.target.checked)}
                    />
                    {variantName[variant]}
                  </label>
                ))}
              </div>
              <Button
                type="button"
                variant="ghost"
                size="1"
                disabled={selectedLanguages.length === 1}
                onClick={() => {
                  setForm({
                    ...form,
                    tracks: form.tracks.filter((t) => t.language !== code),
                  });
                  setSuccess(false);
                }}
              >
                Remove{" "}
                {data.languages.find((l) => l.code === code)?.name ?? code}
              </Button>
            </div>
          ))}
        </div>
        <div className="rs-form-actions subtitle-add-language">
          <label>
            Add a language
            <select
              value={addLanguage}
              onChange={(e) => setAdding(e.target.value)}
            >
              {data.languages
                .filter((l) => !selectedLanguages.includes(l.code))
                .map((l) => (
                  <option key={l.code} value={l.code}>
                    {l.name}
                  </option>
                ))}
            </select>
          </label>
          <Button
            type="button"
            variant="soft"
            disabled={
              selectedLanguages.length >= 3 ||
              selectedLanguages.includes(addLanguage)
            }
            onClick={() => {
              setForm({
                ...form,
                tracks: [
                  ...form.tracks,
                  { language: addLanguage, variant: "full" },
                ],
              });
              setSuccess(false);
            }}
          >
            Add language
          </Button>
        </div>
        <div className="automation-form-grid">
          <label>
            Download format
            <select
              value={form.download_format ?? "srt"}
              onChange={(e) => {
                setForm({
                  ...form,
                  download_format: e.target.value as "srt" | "original",
                });
                setSuccess(false);
              }}
            >
              <option value="srt">SRT (compatible text)</option>
              <option value="original">
                Keep original SRT, VTT, ASS or SSA
              </option>
            </select>
            <span className="rs-form-help">
              Original format preserves provider styles. Existing sidecars and
              staged imports keep their format.
            </span>
          </label>
          <label>
            Automatic timing match
            <select
              value={form.match_policy}
              onChange={(e) => {
                setForm({
                  ...form,
                  match_policy: e.target
                    .value as SubtitlePreferences["match_policy"],
                });
                setSuccess(false);
              }}
            >
              <option value="hash_or_release">
                File hash or exact release
              </option>
              <option value="hash">File hash only</option>
            </select>
          </label>
          <label>
            Hearing-impaired subtitles
            <select
              value={form.hearing_impaired}
              onChange={(e) => {
                setForm({
                  ...form,
                  hearing_impaired: e.target
                    .value as SubtitlePreferences["hearing_impaired"],
                });
                setSuccess(false);
              }}
            >
              <option value="any">Allow standard and SDH tracks</option>
              <option value="exclude">Exclude SDH tracks</option>
              <option value="prefer">Prefer SDH tracks</option>
            </select>
          </label>
          <label>
            Check missing tracks every (minutes)
            <input
              type="number"
              required
              min={60}
              max={10080}
              value={form.interval_minutes}
              onChange={(e) => {
                setForm({ ...form, interval_minutes: Number(e.target.value) });
                setSuccess(false);
              }}
            />
          </label>
        </div>
        <label className="rs-check">
          <input
            type="checkbox"
            checked={form.only_monitored}
            onChange={(e) => {
              setForm({ ...form, only_monitored: e.target.checked });
              setSuccess(false);
            }}
          />
          Automatically check monitored items only
        </label>
        <p className="rs-form-help">
          Existing embedded tracks and sidecars are checked first.
          Provider-labeled AI and machine translations are excluded. Signs/songs
          requires an explicit label; forced tracks use a provider flag or
          label. A provider file is assigned one type, so it is not downloaded
          twice as both signs and forced.
        </p>
        <div className="rs-form-actions">
          <Button type="submit" disabled={form.tracks.length === 0}>
            Save subtitle preferences
          </Button>
          <Button
            type="button"
            variant="ghost"
            onClick={() => void reloadSaved()}
          >
            Reload saved preferences
          </Button>
        </div>
      </fieldset>
      {error && (
        <p role="alert" className="rs-form-error">
          {error}
        </p>
      )}
      {success && (
        <p role="status" className="rs-alert is-success">
          Subtitle preferences saved.
        </p>
      )}
    </form>
  );
}
function ProviderForm({
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
    <section className="rs-panel automation-panel subtitle-provider">
      <div className="rs-panel-header">
        <h2>Subtitle provider</h2>
        <Badge color={provider.configured ? "green" : "gray"}>
          {provider.configured ? "Connected" : "Not connected"}
        </Badge>
      </div>
      <p className="rs-muted">
        Changing providers clears saved subtitle choices.
      </p>
      {provider.configured && (
        <div className="subtitle-provider-state">
          <strong>{readable(provider.state)}</strong>
          <p>{provider.message}</p>
          {provider.cooldown_until > 0 && (
            <p>Retry after {time(provider.cooldown_until)}</p>
          )}
          {provider.remaining !== null && (
            <p>Last reported download allowance: {provider.remaining}</p>
          )}
        </div>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void action("save");
        }}
      >
        <fieldset disabled={busy} className="automation-fields">
          <label>
            Provider
            <select
              value={selectedProvider}
              onChange={(e) => {
                setSelectedProvider(
                  e.target.value as "opensubtitles" | "subdl",
                );
                setKey("");
                setPassword("");
              }}
            >
              <option value="opensubtitles">OpenSubtitles</option>
              <option value="subdl">SubDL</option>
            </select>
          </label>
          {selectedProvider === "opensubtitles" && (
            <>
              <label>
                OpenSubtitles username
                <TextField.Root
                  autoComplete="username"
                  required
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  maxLength={200}
                />
              </label>
              <label>
                OpenSubtitles password
                <TextField.Root
                  type="password"
                  autoComplete="new-password"
                  required={!provider.configured || changedProvider}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder={
                    provider.configured
                      ? "Leave blank to keep saved password"
                      : "Account password"
                  }
                  maxLength={2048}
                />
              </label>
            </>
          )}
          <label>
            Application API key
            <TextField.Root
              type="password"
              autoComplete="off"
              required={!provider.configured || changedProvider}
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder={
                provider.configured
                  ? "Leave blank to keep saved key"
                  : "Your application API key"
              }
              maxLength={2048}
            />
          </label>
          {selectedProvider === "opensubtitles" && (
            <>
              <label>
                Registered application name & version
                <TextField.Root
                  required
                  value={agent}
                  onChange={(e) => setAgent(e.target.value)}
                  maxLength={120}
                />
              </label>
              <p className="rs-form-help">
                Use the application name and version associated with your API
                key. Account download limits apply; ReelScarey waits when the
                provider reports a quota or cooldown.
              </p>
            </>
          )}
          {selectedProvider === "subdl" && (
            <p className="rs-form-help">
              SubDL uses an account API key. Individual SRT/VTT/ASS/SSA files
              require review; shared multi-episode video files and ZIP-only
              results are not selected. Choose original format for styled files.
              Public downloads use the provider's IP allowance.
            </p>
          )}
          <div className="rs-form-actions">
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
                <Button
                  type="button"
                  variant="ghost"
                  color="red"
                  onClick={() => void action("remove")}
                >
                  Disconnect
                </Button>
              </>
            )}
          </div>
        </fieldset>
        {error && (
          <p role="alert" className="rs-form-error">
            {error}
          </p>
        )}
        {success && (
          <p role="status" className="rs-alert is-success">
            {success}
          </p>
        )}
      </form>
      <a
        className="rs-link-button"
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
    </section>
  );
}
function MediaFiles({
  languages,
  wanted,
}: {
  languages: Language[];
  wanted: SubtitleTrack[];
}) {
  const [page, setPage] = useState(1);
  return (
    <section>
      <PageHeading title="Subtitle activity" />
      <FilePage
        key={page}
        page={page}
        setPage={setPage}
        languages={languages}
        wanted={wanted}
      />
    </section>
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
  const files = useLiveResource<{
    items: SubtitleFile[];
    page: number;
    total: number;
    limit: number;
  }>(`/subtitles/files?page=${page}`, 5000);
  return (
    <>
      <RequestState
        loading={files.loading}
        error={files.error}
        retry={files.reload}
      />
      {files.data?.items.length === 0 && (
        <div className="rs-panel search-intro">
          <h2>Subtitles follow your imported media.</h2>
          <p>
            Once a movie or episode has been safely imported, it appears here
            for existing-subtitle checks, provider search and downloads. Torznab
            setup can wait while you prepare your subtitle preferences.
          </p>
          <Button asChild variant="soft">
            <Link to="/movies">Open library</Link>
          </Button>
        </div>
      )}
      {files.data?.items.map((file) => (
        <FileCard
          key={file.id}
          file={file}
          languages={languages}
          wanted={wanted}
          reload={files.reload}
        />
      ))}
      {files.data && (
        <Pagination
          page={page}
          pages={Math.ceil(files.data.total / files.data.limit)}
          onPage={setPage}
        />
      )}
    </>
  );
}
function FileCard({
  file,
  languages,
  wanted,
  reload,
}: {
  file: SubtitleFile;
  languages: Language[];
  wanted: SubtitleTrack[];
  reload: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const active =
    file.job &&
    (file.job.state === "running" ||
      (file.job.requested && ["queued", "retrying"].includes(file.job.state)));
  const desired = file.wanted_tracks ?? wanted;
  const available = desired.filter((t) =>
    file.tracks.some(
      (v) =>
        v.language === t.language &&
        v.variant === t.variant &&
        v.state === "available",
    ),
  ).length;
  async function request(action: "search" | "retry") {
    setBusy(true);
    setError("");
    try {
      await api(`/subtitles/files/${file.id}/${action}`, {
        method: "POST",
        body: "{}",
      });
      setOpen(true);
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <article className="rs-panel automation-panel subtitle-file">
      <div className="rs-panel-header">
        <h2>
          {file.title}
          {file.episodes && ` · ${file.episodes}`}
        </h2>
        <Badge color={available === desired.length ? "green" : "gray"}>
          {available} of {desired.length} tracks available
        </Badge>
      </div>
      <p className="subtitle-path">{file.path}</p>
      <SubtitleEdits fileID={file.id} tracks={file.tracks} reload={reload} />
      {file.job && (
        <div className="subtitle-job">
          <strong>{readable(file.job.state)}</strong>
          <p>{file.job.message}</p>
          <small>Last checked {time(file.job.updated_at)}</small>
        </div>
      )}
      {file.tracks.length > 0 && (
        <div className="subtitle-track-list">
          {file.tracks.map((t) => (
            <div key={`${t.language}-${t.variant}`}>
              <Badge color={t.state === "available" ? "green" : "amber"}>
                {languages.find((l) => l.code === t.language)?.name ??
                  (t.language === "und" ? "Unknown language" : t.language)}{" "}
                · {variantName[t.variant]}
              </Badge>
              <span>
                {readable(t.source)}
                {t.provider
                  ? ` (${t.provider === "subdl" ? "SubDL" : "OpenSubtitles"})`
                  : ""}{" "}
                · {readable(t.state)}
              </span>
              <p>{t.message}</p>
            </div>
          ))}
        </div>
      )}
      <div className="rs-form-actions">
        <Button
          variant="soft"
          disabled={!!active || busy}
          onClick={() => void request("search")}
        >
          Search subtitles for {file.title}
        </Button>
        {file.job &&
          ["blocked", "waiting_provider", "retrying", "paused"].includes(
            file.job.state,
          ) && (
            <Button
              variant="ghost"
              disabled={busy || file.job.state === "running"}
              onClick={() => void request("retry")}
            >
              Retry subtitle job
            </Button>
          )}
        <Button variant="ghost" onClick={() => setOpen(!open)}>
          {open ? "Hide matches" : "Review matches"}
        </Button>
      </div>
      {error && (
        <p role="alert" className="rs-form-error">
          {error}
        </p>
      )}
      {open && (
        <Matches
          key={file.id}
          id={file.id}
          languages={languages}
          active={!!active}
          reload={reload}
        />
      )}
    </article>
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
    <div className="subtitle-matches">
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
      {result.data?.candidates?.map((c) => (
        <Candidate
          key={c.id}
          candidate={c}
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
  candidate: c,
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
      await api(`/subtitles/candidates/${c.id}/download`, {
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
    <div className="subtitle-candidate">
      <div className="rs-panel-header">
        <strong>{c.release}</strong>
        <Badge color={c.hash_match ? "green" : c.automatic ? "blue" : "amber"}>
          {c.hash_match
            ? "File hash match"
            : c.automatic
              ? "Release match"
              : "Review timing"}
        </Badge>
      </div>
      <p>
        {languages.find((l) => l.code === c.language)?.name ?? c.language} ·{" "}
        {variantName[c.variant]}
        {c.hearing_impaired ? " · SDH" : ""} · {c.file_name}
        {" · Output: "}
        {(c.format ?? "srt").toUpperCase() || "Unsupported"}
      </p>
      {c.reasons.length > 0 && (
        <ul>
          {c.reasons.map((r, i) => (
            <li key={i}>{r}</li>
          ))}
        </ul>
      )}
      {c.can_download && (
        <>
          <label className="rs-check">
            <input
              type="checkbox"
              checked={reviewed}
              disabled={active || busy || success}
              onChange={(e) => setReviewed(e.target.checked)}
            />
            I reviewed the language, track type and timing match
          </label>
          <Button
            disabled={!reviewed || active || busy || success}
            onClick={() => void download()}
          >
            Download subtitle
          </Button>
        </>
      )}
      {error && (
        <p role="alert" className="rs-form-error">
          {error}
        </p>
      )}
      {success && (
        <p role="status" className="rs-alert is-success">
          Subtitle download queued.
        </p>
      )}
    </div>
  );
}
