import { PageBody } from "../ui";
import { useEffect, useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import {
  api,
  messageFor,
  type LibraryDefault,
  type LibraryProfileDefaults,
} from "../api";
import { PageHeading, RequestState } from "../components";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";
import "./Admin.css";

export type Profile = {
  id: number;
  name: string;
  qualities: number[];
  cutoff: number;
  language: string;
  min_seeders: number;
  max_size_gib: number;
  /** Automatic grabs wait this long before submission; 0 disables the delay. */
  grab_delay_minutes: number;
  required_terms: string;
  rejected_terms: string;
  revision: number;
  scoring?: {
    sources: string[] | null;
    codecs: string[] | null;
    preferred_terms: string;
    max_usenet_age_days: number;
    min_usenet_age_minutes: number;
    /** Profile-local regex rules. Required/excluded rules can block a release. */
    custom_formats?: CustomFormat[];
  };
};
export type CustomFormat = {
  name: string;
  patterns: string[];
  weight: number;
  required: boolean;
  excluded: boolean;
};
export type Schedule = {
  enabled: boolean;
  mode: "search" | "download";
  interval_minutes: number;
  batch_size: number;
  default_profile_id: number;
  revision: number;
  next_run: number;
  requested: boolean;
  state: string;
  message: string;
  indexer_connected: boolean;
  client_connected: boolean;
};
type Run = {
  id: string;
  started_at: number;
  finished_at: number;
  state: string;
  message: string;
  mode: string;
  checked: number;
  queued: number;
  matched: number;
};
export type RSSRun = {
  id: string;
  started_at: number;
  finished_at: number;
  state: string;
  message: string;
  indexers: number;
  checked: number;
  queued: number;
};
export type RSSResult = {
  id: number;
  indexer: string;
  media_id: number | null;
  episode_id: number | null;
  target: string;
  release_title: string;
  outcome: string;
  message: string;
  download_id: string;
  created_at: number;
};
export type RSSSettings = {
  enabled: boolean;
  interval_minutes: number;
  revision: number;
  next_run: number;
  requested: boolean;
  last_checked: number;
  checked: number;
  queued: number;
  state: string;
  message: string;
  indexer_connected: boolean;
  client_connected: boolean;
};
export type RSSStatus = RSSSettings & {
  last_run: RSSRun | null;
  results: RSSResult[];
};
type Result = {
  kind: string;
  season: number;
  episode: number;
  id: number;
  media_id: number;
  episode_id: number | null;
  target: string;
  outcome: string;
  message: string;
  download_id: string | null;
  decisions: {
    release_id: string;
    title: string;
    quality: number;
    language: string;
    score: number;
    eligible: boolean;
    reasons: string[];
    source?: string;
    codec?: string;
    age_days?: number | null;
    scoring_reasons?: string[];
  }[];
};
const languages = [
  "any",
  "english",
  "french",
  "german",
  "spanish",
  "italian",
  "japanese",
  "korean",
  "chinese",
];
function readable(value: string) {
  return value.replaceAll("_", " ");
}
function date(value: number) {
  return value ? new Date(value * 1000).toLocaleString() : "Not scheduled";
}
export function Automation() {
  const schedule = useLiveResource<Schedule>("/automation", 10000);
  const profiles = useLiveResource<{ items: Profile[] }>("/quality-profiles");
  return (
    <PageBody>
      <PageHeading title="Automation" />
      <RequestState
        loading={(!schedule.data && schedule.loading) || profiles.loading}
        error={schedule.error || profiles.error}
        retry={() => {
          schedule.reload();
          profiles.reload();
        }}
      />
      {schedule.data && profiles.data && (
        <>
          <ScheduleForm
            schedule={schedule.data}
            profiles={profiles.data.items}
            reload={schedule.reload}
          />
          <ReleaseDetection />
          <RunHistory />
          <Profiles profiles={profiles.data.items} reload={profiles.reload} />
          <LibraryDefaults profiles={profiles.data.items} />
          <p className="rs-muted rs-small">
            Per-title quality profiles live with the library: choose{" "}
            <Link to="/movies">Movies</Link>, <Link to="/series">TV Shows</Link>{" "}
            or <Link to="/anime">Anime</Link>, then use Select and{" "}
            <strong>Bulk profiles</strong>.
          </p>
        </>
      )}
    </PageBody>
  );
}
function ScheduleForm({
  schedule,
  profiles,
  reload,
}: {
  schedule: Schedule;
  profiles: Profile[];
  reload: () => void;
}) {
  const [form, setForm] = useState(schedule);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setSuccess("");
    try {
      const out = await api<Schedule>("/automation", {
        method: "PUT",
        body: JSON.stringify({
          enabled: form.enabled,
          mode: form.mode,
          interval_minutes: form.interval_minutes,
          batch_size: form.batch_size,
          default_profile_id: form.default_profile_id,
          revision: form.revision,
        }),
      });
      setForm(out);
      setSuccess("Automation preferences saved.");
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function run() {
    setBusy(true);
    setError("");
    setSuccess("");
    try {
      await api("/automation/run", { method: "POST", body: "{}" });
      setSuccess("Search requested.");
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function reloadSaved() {
    setBusy(true);
    setError("");
    setSuccess("");
    try {
      setForm(await api<Schedule>("/automation"));
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="rs-panel automation-panel">
      <div className="rs-panel-header">
        <h2>Search schedule</h2>
        <Badge color={schedule.enabled ? "green" : "gray"}>
          {schedule.enabled ? "Enabled" : "Paused"}
        </Badge>
      </div>
      {!schedule.indexer_connected ? (
        <div className="automation-notice">
          <strong>Searches are waiting for an indexer.</strong>
          <p>
            Save profiles and schedule preferences now. Searches will wait for
            an indexer connection. You can keep managing your library.
          </p>
        </div>
      ) : (
        <p className="rs-muted">{schedule.message}</p>
      )}
      <form onSubmit={save}>
        <fieldset disabled={busy} className="automation-fields">
          <label className="rs-check">
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
            />
            Enable scheduled searches
          </label>
          <div className="automation-form-grid">
            <label>
              When a release matches
              <select
                value={form.mode}
                onChange={(e) =>
                  setForm({ ...form, mode: e.target.value as Schedule["mode"] })
                }
              >
                <option value="search">Save search results for review</option>
                <option value="download">
                  Download missing items automatically
                </option>
              </select>
            </label>
            <label>
              Search every (minutes)
              <input
                type="number"
                min={15}
                max={10080}
                required
                value={form.interval_minutes}
                onChange={(e) =>
                  setForm({ ...form, interval_minutes: Number(e.target.value) })
                }
              />
            </label>
            <label>
              Targets per run
              <input
                type="number"
                min={1}
                max={25}
                required
                value={form.batch_size}
                onChange={(e) =>
                  setForm({ ...form, batch_size: Number(e.target.value) })
                }
              />
            </label>
            <label>
              Default quality profile
              <select
                value={form.default_profile_id}
                onChange={(e) =>
                  setForm({
                    ...form,
                    default_profile_id: Number(e.target.value),
                  })
                }
              >
                {profiles.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <p className="rs-form-help">
            Every monitored, missing movie and loaded episode is searched.
            Movies do not wait for their saved release date, so an early release
            is still found. Anime requires per-show consent. Saved provider
            mappings can include approved specials and range packs.
          </p>
          {form.mode === "download" && (
            <p className="automation-notice">
              This mode queues matching missing items through qBittorrent or
              SABnzbd. A single-episode TV download or a labeled season pack
              imports automatically when every file maps to a reserved episode;
              unmatched or ambiguous files wait for episode-to-file review.
              Upgrade suggestions preserve existing files.
              {!schedule.client_connected &&
                " Connect the matching download client before automatic downloads can run."}
            </p>
          )}
          <div className="rs-form-actions">
            <Button type="submit">Save automation</Button>
            <Button
              type="button"
              variant="soft"
              disabled={
                !schedule.indexer_connected ||
                schedule.requested ||
                schedule.state === "running"
              }
              onClick={() => void run()}
            >
              Search now
            </Button>
            <Button
              type="button"
              variant="ghost"
              onClick={() => void reloadSaved()}
            >
              Reload saved schedule
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
            {success}
          </p>
        )}
      </form>
      <p className="rs-form-help">
        {schedule.enabled
          ? `Next search: ${date(schedule.next_run)}`
          : "Scheduled searches are paused. Search now runs once using the saved preferences."}{" "}
        Pausing stops further searches; existing downloads continue.
      </p>
    </section>
  );
}
function ReleaseDetection() {
  const status = useLiveResource<RSSStatus>("/automation/rss", 10000);
  return (
    <section className="rs-panel automation-panel">
      <div className="rs-panel-header">
        <h2>Release detection</h2>
        <Badge color={status.data?.enabled ? "green" : "gray"}>
          {status.data?.enabled ? "Enabled" : "Paused"}
        </Badge>
      </div>
      {status.error && <p role="alert">{status.error}</p>}
      {!status.data && status.loading && (
        <p className="rs-muted">Loading release detection…</p>
      )}
      {status.data && (
        <>
          {!status.data.indexer_connected ? (
            <div className="automation-notice">
              <strong>Release checks are waiting for an indexer.</strong>
              <p>
                Add and enable a direct tracker, RSS feed, Torznab or Newznab
                connection in Indexers.
              </p>
            </div>
          ) : (
            <p className="rs-muted">{status.data.message}</p>
          )}
          <RSSForm
            key={status.data.revision}
            status={status.data}
            reload={status.reload}
          />
          <p className="rs-form-help">
            {status.data.enabled
              ? `Next check: ${date(status.data.next_run)}`
              : "Release checks are paused. Check now runs one pass using the saved interval."}
            {status.data.last_checked
              ? ` Last check: ${date(status.data.last_checked)}.`
              : ""}
          </p>
          {status.data.last_run && (
            <p className="rs-form-help">
              Last pass: {readable(status.data.last_run.state)} ·{" "}
              {status.data.last_run.indexers} indexers ·{" "}
              {status.data.last_run.checked} releases checked ·{" "}
              {status.data.last_run.queued} grabbed
            </p>
          )}
          <h3>Recent feed results</h3>
          {!status.data.results.length ? (
            <p className="rs-muted">
              No feed entries have matched a monitored missing title yet.
            </p>
          ) : (
            <div className="automation-results">
              {status.data.results.map((r) => (
                <article key={r.id} className="automation-result">
                  <div className="rs-panel-header">
                    <h4>{r.target || r.release_title}</h4>
                    <Badge
                      color={
                        r.outcome === "queued"
                          ? "green"
                          : r.outcome === "attention" ||
                              r.outcome === "needs_attention"
                            ? "amber"
                            : "gray"
                      }
                    >
                      {readable(r.outcome)}
                    </Badge>
                  </div>
                  <p className="rs-small">
                    {r.release_title} · {r.indexer} · {date(r.created_at)}
                  </p>
                  <p>{r.message}</p>
                  {r.download_id && (
                    <Link to="/activity" className="rs-link-button">
                      Open download activity
                    </Link>
                  )}
                </article>
              ))}
            </div>
          )}
        </>
      )}
    </section>
  );
}

function RSSForm({
  status,
  reload,
}: {
  status: RSSStatus;
  reload: () => void;
}) {
  const [form, setForm] = useState(status);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setSuccess("");
    try {
      const out = await api<RSSSettings>("/automation/rss", {
        method: "PUT",
        body: JSON.stringify({
          enabled: form.enabled,
          interval_minutes: form.interval_minutes,
          revision: form.revision,
        }),
      });
      setForm({ ...form, ...out });
      setSuccess("Release detection preferences saved.");
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function run() {
    setBusy(true);
    setError("");
    setSuccess("");
    try {
      await api("/automation/rss/run", { method: "POST", body: "{}" });
      setSuccess("Release check requested.");
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form onSubmit={save}>
      <fieldset disabled={busy} className="automation-fields">
        <label className="rs-check">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
          />
          Check indexer feeds automatically
        </label>
        <div className="automation-form-grid">
          <label>
            Check every (minutes)
            <input
              type="number"
              min={5}
              max={1440}
              required
              value={form.interval_minutes}
              onChange={(e) =>
                setForm({ ...form, interval_minutes: Number(e.target.value) })
              }
            />
          </label>
        </div>
        <p className="rs-form-help">
          Every passing feed entry is matched against monitored, missing movies
          and episodes using the existing exact title, episode and quality
          rules. A match is grabbed automatically, even while the scheduled
          search above is paused or in search-only mode. Registered files are
          never replaced; upgrades remain suggestions. Manual release searches
          still show results for review.
        </p>
        {!form.client_connected && (
          <p className="automation-notice">
            Connect a download client before automatic grabs can start. Feed
            checks will record waiting matches until then.
          </p>
        )}
        <div className="rs-form-actions">
          <Button type="submit">Save release detection</Button>
          <Button
            type="button"
            variant="soft"
            disabled={
              !form.enabled ||
              status.requested ||
              status.state === "running" ||
              !form.indexer_connected
            }
            onClick={() => void run()}
          >
            Check now
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
          {success}
        </p>
      )}
    </form>
  );
}

function RunHistory() {
  const runs = useLiveResource<{ items: Run[] }>("/automation/runs", 5000);
  const [selected, setSelected] = useState("");
  const id = selected || runs.data?.items[0]?.id || "";
  return (
    <section className="rs-panel automation-panel">
      <div className="rs-panel-header">
        <h2>Search history</h2>
        <Button variant="ghost" onClick={runs.reload}>
          Refresh history
        </Button>
      </div>
      {runs.error && <p role="alert">{runs.error}</p>}
      {!runs.data?.items.length ? (
        <p className="rs-muted">No searches have run yet.</p>
      ) : (
        <>
          <label>
            Search run
            <select value={id} onChange={(e) => setSelected(e.target.value)}>
              {runs.data.items.map((r) => (
                <option key={r.id} value={r.id}>
                  {date(r.started_at)} · {readable(r.state)} · {r.checked}{" "}
                  checked · {r.matched} matched · {r.queued} queued
                </option>
              ))}
            </select>
          </label>
          {runs.data.items
            .filter((r) => r.id === id)
            .map((r) => (
              <p key={r.id} className="rs-form-help">
                {r.message || "Search in progress…"} Mode:{" "}
                {r.mode === "download"
                  ? "Automatic downloads"
                  : "Results for review"}
                .
              </p>
            ))}
          <RunResults key={id} id={id} />
        </>
      )}
    </section>
  );
}
function RunResults({ id }: { id: string }) {
  const result = useLiveResource<{ items: Result[] }>(
    `/automation/runs/${id}`,
    5000,
  );
  return (
    <div className="automation-results">
      {result.error && <p role="alert">{result.error}</p>}
      {result.data?.items.map((r) => (
        <article key={r.id} className="automation-result">
          <div className="rs-panel-header">
            <h3>{r.target}</h3>
            <Badge
              color={
                r.outcome === "queued"
                  ? "green"
                  : r.outcome === "upgrade_available"
                    ? "amber"
                    : "gray"
              }
            >
              {readable(r.outcome)}
            </Badge>
          </div>
          <p>{r.message}</p>
          {r.download_id && (
            <Link to="/activity" className="rs-link-button">
              Open download activity
            </Link>
          )}
          {r.outcome === "match" && (
            <Link
              className="rs-link-button"
              to={
                r.kind === "series"
                  ? `/releases?series=${r.media_id}&season=${r.season}&episode=${r.episode}`
                  : `/releases?movie=${r.media_id}`
              }
            >
              Review fresh releases
            </Link>
          )}
          {r.decisions.length > 0 && (
            <details>
              <summary>Why these releases?</summary>
              {r.decisions.map((d, i) => (
                <div className="release-decision" key={`${d.release_id}-${i}`}>
                  <strong>{d.title}</strong>
                  <p>
                    {d.quality ? `${d.quality}p` : "Unknown resolution"} ·{" "}
                    {d.language} · {d.eligible ? "Matches profile" : "Excluded"}{" "}
                    · Score {d.score}
                  </p>
                  {(d.source || d.codec) && (
                    <p className="rs-form-help">
                      {d.source ?? "Unknown source"} ·{" "}
                      {d.codec ?? "Unknown codec"}
                      {d.age_days != null
                        ? ` · ${d.age_days.toFixed(1)} days old`
                        : ""}
                    </p>
                  )}
                  {!!d.scoring_reasons?.length && (
                    <ul>
                      {d.scoring_reasons.map((reason, i) => (
                        <li key={i}>{reason}</li>
                      ))}
                    </ul>
                  )}
                  {d.reasons.length > 0 && (
                    <ul>
                      {d.reasons.map((reason, i) => (
                        <li key={i}>{reason}</li>
                      ))}
                    </ul>
                  )}
                </div>
              ))}
            </details>
          )}
        </article>
      ))}
    </div>
  );
}
const blankProfile: Profile = {
  id: 0,
  name: "",
  qualities: [720, 1080],
  cutoff: 1080,
  language: "any",
  min_seeders: 1,
  max_size_gib: 40,
  grab_delay_minutes: 0,
  required_terms: "",
  rejected_terms: "",
  revision: 0,
};
const libraryLabels: Record<LibraryDefault["library_key"], string> = {
  movies: "Movies",
  tv: "TV Shows",
  anime: "Anime",
};

function LibraryDefaults({ profiles }: { profiles: Profile[] }) {
  const resource = useResource<LibraryProfileDefaults>(
    "/library/profile-defaults",
  );
  const [items, setItems] = useState<LibraryDefault[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);
  useEffect(() => {
    if (resource.data) setItems(resource.data.items);
  }, [resource.data]);
  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setSuccess(false);
    try {
      const out = await api<{ items: LibraryDefault[] }>(
        "/library/profile-defaults",
        { method: "PUT", body: JSON.stringify({ items }) },
      );
      setItems(out.items);
      setSuccess(true);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="rs-panel automation-panel">
      <div className="rs-panel-header">
        <h2>Library profile defaults</h2>
      </div>
      <p className="rs-muted">
        New titles inherit these profiles. A title without an explicit profile
        keeps using the search schedule default. Existing titles are unchanged.
      </p>
      {error && <p role="alert">{error}</p>}
      {success && <p role="status">Library defaults saved.</p>}
      <form onSubmit={save}>
        <fieldset
          disabled={busy || resource.loading}
          className="automation-form-grid"
        >
          {items.map((item) => (
            <label key={item.library_key}>
              {libraryLabels[item.library_key]}
              <select
                value={item.profile_id ?? ""}
                onChange={(event) =>
                  setItems((current) =>
                    current.map((candidate) =>
                      candidate.library_key === item.library_key
                        ? {
                            ...candidate,
                            profile_id: event.target.value
                              ? Number(event.target.value)
                              : null,
                          }
                        : candidate,
                    ),
                  )
                }
              >
                <option value="">Use search schedule default</option>
                {profiles.map((profile) => (
                  <option key={profile.id} value={profile.id}>
                    {profile.name}
                  </option>
                ))}
              </select>
            </label>
          ))}
        </fieldset>
        <div className="automation-actions">
          <Button type="submit" disabled={busy || items.length === 0}>
            Save library defaults
          </Button>
        </div>
      </form>
    </section>
  );
}

function Profiles({
  profiles,
  reload,
}: {
  profiles: Profile[];
  reload: () => void;
}) {
  const [selected, setSelected] = useState(profiles[0]?.id ?? 0);
  const [creating, setCreating] = useState(false);
  const profile = creating
    ? blankProfile
    : (profiles.find((p) => p.id === selected) ?? profiles[0]);
  return (
    <section className="rs-panel automation-panel">
      <div className="rs-panel-header">
        <h2>Quality profiles</h2>
        <Button variant="soft" onClick={() => setCreating(true)}>
          New profile
        </Button>
        <Button variant="ghost" onClick={reload}>
          Reload saved profiles
        </Button>
      </div>
      <label>
        Choose profile
        <select
          value={creating ? "new" : profile.id}
          onChange={(e) => {
            setCreating(false);
            setSelected(Number(e.target.value));
          }}
        >
          {creating && <option value="new">New profile</option>}
          {profiles.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
      <ProfileForm
        key={`${profile.id}-${profile.revision}`}
        profile={profile}
        saved={(p) => {
          setCreating(false);
          setSelected(p.id);
          reload();
        }}
      />
    </section>
  );
}
function ProfileForm({
  profile,
  saved,
}: {
  profile: Profile;
  saved: (p: Profile) => void;
}) {
  const [form, setForm] = useState<Profile>({
    ...profile,
    grab_delay_minutes: profile.grab_delay_minutes ?? 0,
  });
  const [sourceOrderText, setSourceOrderText] = useState(
    (profile.scoring?.sources ?? []).join(","),
  );
  const [codecOrderText, setCodecOrderText] = useState(
    (profile.scoring?.codecs ?? []).join(","),
  );
  const [customFormatsText, setCustomFormatsText] = useState(
    JSON.stringify(profile.scoring?.custom_formats ?? [], null, 2),
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);
  function set<T extends keyof Profile>(key: T, value: Profile[T]) {
    setForm({ ...form, [key]: value });
    setSuccess(false);
  }
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    let customFormats: CustomFormat[] = [];
    try {
      const parsed: unknown = JSON.parse(customFormatsText || "[]");
      if (!Array.isArray(parsed)) throw new Error("not an array");
      customFormats = parsed as CustomFormat[];
    } catch {
      setBusy(false);
      setError(
        "Custom formats must be a JSON array of {name, patterns, weight, required, excluded} objects.",
      );
      return;
    }
    const body: Profile = {
      ...form,
      scoring: {
        ...(form.scoring ?? {
          sources: [],
          codecs: [],
          preferred_terms: "",
          max_usenet_age_days: 0,
          min_usenet_age_minutes: 0,
        }),
        custom_formats: customFormats,
      },
    };
    try {
      const p = await api<Profile>(
        form.id ? `/quality-profiles/${form.id}` : "/quality-profiles",
        { method: form.id ? "PUT" : "POST", body: JSON.stringify(body) },
      );
      setForm(p);
      setSuccess(true);
      saved(p);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form onSubmit={save}>
      <fieldset disabled={busy} className="automation-fields">
        <label>
          Profile name
          <TextField.Root
            required
            maxLength={60}
            value={form.name}
            onChange={(e) => set("name", e.target.value)}
          />
        </label>
        <div role="group" aria-label="Allowed resolutions">
          <span className="form-label">Allowed resolutions</span>
          <div className="quality-options">
            {[480, 720, 1080, 2160].map((q) => (
              <label className="rs-check" key={q}>
                <input
                  type="checkbox"
                  checked={form.qualities.includes(q)}
                  onChange={(e) => {
                    const qs = e.target.checked
                      ? [...form.qualities, q].sort((a, b) => a - b)
                      : form.qualities.filter((n) => n !== q);
                    setForm({
                      ...form,
                      qualities: qs,
                      cutoff: qs.includes(form.cutoff)
                        ? form.cutoff
                        : (qs.at(-1) ?? 0),
                    });
                    setSuccess(false);
                  }}
                />
                {q}p
              </label>
            ))}
          </div>
        </div>
        <div className="automation-form-grid">
          <label>
            Resolution cutoff
            <select
              value={form.cutoff}
              onChange={(e) => set("cutoff", Number(e.target.value))}
            >
              {form.qualities.map((q) => (
                <option key={q} value={q}>
                  {q}p
                </option>
              ))}
            </select>
          </label>
          <label>
            Release language
            <select
              value={form.language}
              onChange={(e) => set("language", e.target.value)}
            >
              {languages.map((l) => (
                <option key={l} value={l}>
                  {l === "any"
                    ? "Any language"
                    : l[0].toUpperCase() + l.slice(1)}
                </option>
              ))}
            </select>
          </label>
          <label>
            Minimum seeders
            <input
              required
              type="number"
              min={0}
              max={10000}
              value={form.min_seeders}
              onChange={(e) => set("min_seeders", Number(e.target.value))}
            />
          </label>
          <label>
            Maximum size (GiB)
            <input
              required
              type="number"
              min={1}
              max={500}
              value={form.max_size_gib}
              onChange={(e) => set("max_size_gib", Number(e.target.value))}
            />
          </label>
          <label>
            Grab delay (minutes)
            <input
              required
              type="number"
              min={0}
              max={1440}
              value={form.grab_delay_minutes ?? 0}
              onChange={(e) =>
                set("grab_delay_minutes", Number(e.target.value))
              }
            />
          </label>
          <label>
            Required terms
            <TextField.Root
              maxLength={200}
              value={form.required_terms}
              onChange={(e) => set("required_terms", e.target.value)}
            />
          </label>
          <label>
            Excluded terms
            <TextField.Root
              maxLength={200}
              value={form.rejected_terms}
              onChange={(e) => set("rejected_terms", e.target.value)}
            />
          </label>
          <label>
            Preferred sources, in order
            <TextField.Root
              value={sourceOrderText}
              placeholder="remux,bluray,webdl,webrip,hdtv,dvd"
              onChange={(e) => {
                setSourceOrderText(e.target.value);
                set("scoring", {
                  codecs: [],
                  preferred_terms: "",
                  max_usenet_age_days: 0,
                  min_usenet_age_minutes: 0,
                  ...form.scoring,
                  sources: e.target.value
                    .split(",")
                    .map((v) => v.trim())
                    .filter(Boolean),
                });
              }}
            />
          </label>
          <label>
            Preferred codecs, in order
            <TextField.Root
              value={codecOrderText}
              placeholder="av1,h265,h264"
              onChange={(e) => {
                setCodecOrderText(e.target.value);
                set("scoring", {
                  sources: [],
                  preferred_terms: "",
                  max_usenet_age_days: 0,
                  min_usenet_age_minutes: 0,
                  ...form.scoring,
                  codecs: e.target.value
                    .split(",")
                    .map((v) => v.trim())
                    .filter(Boolean),
                });
              }}
            />
          </label>
          <label>
            Preferred release terms
            <TextField.Root
              maxLength={200}
              value={form.scoring?.preferred_terms ?? ""}
              onChange={(e) =>
                set("scoring", {
                  sources: [],
                  codecs: [],
                  max_usenet_age_days: 0,
                  min_usenet_age_minutes: 0,
                  ...form.scoring,
                  preferred_terms: e.target.value,
                })
              }
            />
          </label>
          <label>
            Custom formats (JSON)
            <textarea
              rows={6}
              className="rs-code-input"
              value={customFormatsText}
              onChange={(e) => {
                setCustomFormatsText(e.target.value);
                setSuccess(false);
              }}
              placeholder={`[\n  {"name":"Proper","patterns":["\\\\bproper\\\\b"],"weight":75,"required":false,"excluded":false}\n]`}
            />
            <span className="rs-muted rs-small">
              Patterns are case-insensitive regular expressions. Required and
              excluded formats can block a release; other weights only reorder
              eligible releases.
            </span>
          </label>
          <label>
            Usenet retention (days; 0 disables)
            <input
              type="number"
              min={0}
              max={10000}
              value={form.scoring?.max_usenet_age_days ?? 0}
              onChange={(e) =>
                set("scoring", {
                  sources: [],
                  codecs: [],
                  preferred_terms: "",
                  min_usenet_age_minutes: 0,
                  ...form.scoring,
                  max_usenet_age_days: Number(e.target.value),
                })
              }
            />
          </label>
          <label>
            Usenet propagation delay (minutes)
            <input
              type="number"
              min={0}
              max={10080}
              value={form.scoring?.min_usenet_age_minutes ?? 0}
              onChange={(e) =>
                set("scoring", {
                  sources: [],
                  codecs: [],
                  preferred_terms: "",
                  max_usenet_age_days: 0,
                  ...form.scoring,
                  min_usenet_age_minutes: Number(e.target.value),
                })
              }
            />
          </label>
        </div>
        <p className="rs-form-help">
          Automatic grabs wait for the grab delay before submission so better
          releases can appear. Manual release selections are submitted
          immediately.
        </p>
        <p className="rs-form-help">
          Torrent seeders dominate within a resolution: among eligible releases,
          the best-seeded one wins when the search runs. Source, codec and
          preferred-term bonuses break near-ties below that. They use release
          labels, not verified stream quality. Usenet age filters reject unknown
          publication dates when enabled; torrent ranking is unaffected by
          retention.
        </p>
        <p className="rs-form-help">
          Separate terms with commas. Every required term must match. Camera
          recordings, screeners, samples, ambiguous identities and unknown
          resolution, size or seed counts are excluded. Manual release
          selections remain your choice.
        </p>
        <Button type="submit" disabled={!form.qualities.length}>
          Save profile
        </Button>
      </fieldset>
      {error && (
        <p role="alert" className="rs-form-error">
          {error}
        </p>
      )}
      {success && (
        <p role="status" className="rs-alert is-success">
          Profile saved.
        </p>
      )}
    </form>
  );
}
