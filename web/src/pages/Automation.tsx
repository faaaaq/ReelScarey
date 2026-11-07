import { PageBody } from "../ui";
import { useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { Pagination } from "../movieComponents";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
import "./Admin.css";

export type Profile = {
  id: number;
  name: string;
  qualities: number[];
  cutoff: number;
  language: string;
  min_seeders: number;
  max_size_gib: number;
  required_terms: string;
  rejected_terms: string;
  revision: number;
  scoring?: {
    sources: string[] | null;
    codecs: string[] | null;
    preferred_terms: string;
    max_usenet_age_days: number;
    min_usenet_age_minutes: number;
  };
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
type Assignment = {
  media_id: number;
  title: string;
  kind: string;
  profile_id: number | null;
  revision: number;
  monitored: boolean;
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
          <RunHistory />
          <Profiles profiles={profiles.data.items} reload={profiles.reload} />
          <Assignments profiles={profiles.data.items} />
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
            Only monitored, released movies and loaded episodes are searched.
            Anime requires per-show consent. Saved provider mappings can include
            approved specials and range packs.
          </p>
          {form.mode === "download" && (
            <p className="automation-notice">
              This mode queues matching missing items through qBittorrent or
              SABnzbd. TV imports still need episode-to-file review. Upgrade
              suggestions preserve existing files.
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
                  checked · {r.queued} queued
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
  required_terms: "",
  rejected_terms: "",
  revision: 0,
};
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
  const [form, setForm] = useState(profile);
  const [sourceOrderText, setSourceOrderText] = useState(
    (profile.scoring?.sources ?? []).join(","),
  );
  const [codecOrderText, setCodecOrderText] = useState(
    (profile.scoring?.codecs ?? []).join(","),
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
    try {
      const p = await api<Profile>(
        form.id ? `/quality-profiles/${form.id}` : "/quality-profiles",
        { method: form.id ? "PUT" : "POST", body: JSON.stringify(form) },
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
function Assignments({ profiles }: { profiles: Profile[] }) {
  const [page, setPage] = useState(1);
  const items = useResource<{
    items: Assignment[];
    page: number;
    total: number;
    limit: number;
  }>(`/automation/items?page=${page}`);
  return (
    <section className="rs-panel automation-panel">
      <h2>Library preferences</h2>
      <RequestState
        loading={items.loading}
        error={items.error}
        retry={items.reload}
      />
      {items.data && !items.data.items.length && (
        <p>Add a movie or show to choose its profile here.</p>
      )}
      {items.data?.items.map((item) => (
        <AssignmentForm
          key={`${item.media_id}-${item.revision}`}
          item={item}
          profiles={profiles}
          reload={items.reload}
        />
      ))}
      {items.data && (
        <Pagination
          page={page}
          pages={Math.ceil(items.data.total / items.data.limit)}
          onPage={setPage}
        />
      )}
    </section>
  );
}
function AssignmentForm({
  item,
  profiles,
  reload,
}: {
  item: Assignment;
  profiles: Profile[];
  reload: () => void;
}) {
  const [profile, setProfile] = useState(item.profile_id ?? 0);
  const [monitored, setMonitored] = useState(item.monitored);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api(`/automation/items/${item.media_id}`, {
        method: "PUT",
        body: JSON.stringify({
          profile_id: profile || null,
          revision: item.revision,
          ...(item.kind === "movie" ? { monitored } : {}),
        }),
      });
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="automation-assignment" onSubmit={save}>
      <div>
        <strong>{item.title}</strong>
        <p className="rs-form-help">
          {item.kind === "movie" ? (
            "Movie"
          ) : (
            <Link to={`/series/${item.media_id}`} className="rs-link-button">
              TV monitoring
            </Link>
          )}
        </p>
      </div>
      <label>
        <span className="sr-only">Quality profile for {item.title}</span>
        <select
          disabled={busy}
          value={profile}
          onChange={(e) => setProfile(Number(e.target.value))}
        >
          <option value={0}>Schedule default</option>
          {profiles.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
      {item.kind === "movie" && (
        <label className="rs-check">
          <input
            disabled={busy}
            type="checkbox"
            checked={monitored}
            onChange={(e) => setMonitored(e.target.checked)}
          />
          Monitor
        </label>
      )}
      <Button disabled={busy} type="submit" variant="soft">
        Save {item.title}
      </Button>
      {error && (
        <p role="alert" className="rs-form-error">
          {error}
        </p>
      )}
    </form>
  );
}
