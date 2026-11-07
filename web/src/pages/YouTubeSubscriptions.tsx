import { ArrowLeft, RefreshCw } from "lucide-react";
import { PageBody, PageToolbar, ToolbarButton, ToolbarSection } from "../ui";
import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useLiveResource } from "../useLiveResource";
import { YouTubeBulk } from "./YouTubeBulk";
import "./Admin.css";
type Sub = {
  id: string;
  source_url: string;
  title: string;
  collection_id: number;
  enabled: boolean;
  mode: "review" | "acquire";
  interval_hours: number;
  subtitles: { languages: string[]; automatic: boolean };
  initialized: boolean;
  revision: number;
  state: string;
  message: string;
  last_checked: number;
  next_run: number;
};
type Entry = {
  id: string;
  title: string;
  channel: string;
  state: string;
  job_id: string;
  discovered_at: number;
};
export function YouTubeSubscriptions() {
  const subs = useLiveResource<{ items: Sub[] }>(
      "/youtube/subscriptions",
      5000,
    ),
    collections = useLiveResource<{ items: { id: number; title: string }[] }>(
      "/youtube/collections",
    );
  const [url, setURL] = useState(""),
    [title, setTitle] = useState(""),
    [collection, setCollection] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function add(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/youtube/subscriptions", {
        method: "POST",
        body: JSON.stringify({
          source_url: url,
          title,
          collection_id: Number(collection),
          enabled: false,
          mode: "review",
          interval_hours: 24,
          subtitles: { languages: [], automatic: false },
        }),
      });
      setURL("");
      setTitle("");
      subs.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton icon={ArrowLeft} label="YouTube" to="/youtube" />
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={subs.loading}
            onClick={subs.reload}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading title="YouTube subscriptions" />
        <RequestState
          loading={subs.loading}
          error={subs.error}
          retry={subs.reload}
        />
        <form className="rs-panel rs-stack" onSubmit={(e) => void add(e)}>
          <h2>Add a subscription</h2>
          <fieldset disabled={busy} className="backup-fields">
            <label>
              Name
              <TextField.Root
                required
                maxLength={200}
                value={title}
                onChange={(e) => setTitle(e.target.value)}
              />
            </label>
            <label>
              Public channel or playlist URL
              <TextField.Root
                required
                type="url"
                value={url}
                onChange={(e) => setURL(e.target.value)}
              />
            </label>
            <label>
              Custom collection
              <select
                required
                value={collection}
                onChange={(e) => setCollection(e.target.value)}
              >
                <option value="">Choose a collection</option>
                {collections.data?.items.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.title}
                  </option>
                ))}
              </select>
            </label>
            <Button type="submit">Add paused subscription</Button>
          </fieldset>
          <p>
            Create a custom collection on the YouTube page first. The first
            source check records existing videos as a baseline; it never
            automatically downloads that backlog. Checks inspect at most the
            latest 100 entries. Older uploads may fall outside this window.
          </p>
          {error && (
            <p role="alert" className="rs-alert is-danger">
              {error}
            </p>
          )}
        </form>
        {subs.data?.items.map((v) => (
          <Subscription
            key={`${v.id}-${v.revision}`}
            initial={v}
            reload={subs.reload}
          />
        ))}
      </PageBody>
    </>
  );
}
function Subscription({
  initial,
  reload,
}: {
  initial: Sub;
  reload: () => void;
}) {
  const [form, setForm] = useState(initial),
    [checked, setChecked] = useState(false),
    [open, setOpen] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function action(check = false) {
    setBusy(true);
    setError("");
    try {
      await api(
        check
          ? `/youtube/subscriptions/${initial.id}`
          : "/youtube/subscriptions",
        {
          method: check ? "POST" : "PUT",
          body: JSON.stringify(
            check
              ? { revision: initial.revision }
              : {
                  id: form.id,
                  source_url: form.source_url,
                  title: form.title,
                  collection_id: form.collection_id,
                  enabled: form.enabled,
                  mode: form.mode,
                  interval_hours: form.interval_hours,
                  subtitles: form.subtitles,
                  revision: form.revision,
                  reviewed: checked,
                },
          ),
        },
      );
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="rs-panel rs-stack">
      <h2>{initial.title}</h2>
      <p>{initial.source_url}</p>
      <p>
        {initial.message} ({initial.state})
      </p>
      <p>
        Last checked:{" "}
        {initial.last_checked
          ? new Date(initial.last_checked * 1000).toLocaleString()
          : "Never"}
      </p>
      <fieldset disabled={busy} className="backup-fields">
        <label>
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
          />{" "}
          Enable source checks
        </label>
        <div className="backup-grid">
          <label>
            Every (hours)
            <input
              type="number"
              min={1}
              max={168}
              value={form.interval_hours}
              onChange={(e) =>
                setForm({ ...form, interval_hours: Number(e.target.value) })
              }
            />
          </label>
          <label>
            New videos
            <select
              value={form.mode}
              onChange={(e) =>
                setForm({ ...form, mode: e.target.value as Sub["mode"] })
              }
            >
              <option value="review">Discover for review</option>
              <option value="acquire" disabled={!initial.initialized}>
                Acquire automatically after baseline check
              </option>
            </select>
          </label>
        </div>
        <label>
          <input
            type="checkbox"
            checked={form.subtitles.languages?.includes("en") ?? false}
            onChange={(e) =>
              setForm({
                ...form,
                subtitles: {
                  ...form.subtitles,
                  languages: e.target.checked ? ["en"] : [],
                },
              })
            }
          />{" "}
          Acquire English VTT subtitles
        </label>
        <label>
          <input
            type="checkbox"
            checked={form.subtitles.automatic}
            onChange={(e) =>
              setForm({
                ...form,
                subtitles: { ...form.subtitles, automatic: e.target.checked },
              })
            }
          />{" "}
          Use automatically generated captions
        </label>
        {form.mode === "acquire" && (
          <label>
            <input
              type="checkbox"
              checked={checked}
              onChange={(e) => setChecked(e.target.checked)}
            />{" "}
            I authorize acquisition of new videos into this collection. Existing
            videos stay excluded until selected explicitly; imports still
            require review.
          </label>
        )}
        <div className="rs-form-actions">
          <Button
            disabled={form.mode === "acquire" && !checked}
            onClick={() => void action()}
          >
            Save subscription
          </Button>
          <Button variant="soft" onClick={() => void action(true)}>
            Check source now
          </Button>
          <Button variant="ghost" onClick={() => setOpen(!open)}>
            {open ? "Hide discovered videos" : "Review discovered videos"}
          </Button>
          <Button
            variant="ghost"
            color="red"
            onClick={() =>
              void (async () => {
                if (
                  !window.confirm(
                    "Remove this subscription? Existing acquisitions and imported files will be retained.",
                  )
                )
                  return;
                setBusy(true);
                setError("");
                try {
                  await api(`/youtube/subscriptions/${initial.id}`, {
                    method: "DELETE",
                    body: JSON.stringify({ revision: initial.revision }),
                  });
                  reload();
                } catch (e) {
                  setError(messageFor(e));
                } finally {
                  setBusy(false);
                }
              })()
            }
          >
            Remove subscription
          </Button>
        </div>
      </fieldset>
      {error && (
        <p role="alert" className="rs-alert is-danger">
          {error}
        </p>
      )}
      {open && <Entries sub={initial} reload={reload} />}
    </section>
  );
}
function Entries({ sub, reload }: { sub: Sub; reload: () => void }) {
  const resource = useLiveResource<{ items: Entry[] }>(
    `/youtube/subscriptions/${sub.id}`,
    5000,
  );
  return (
    <>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {resource.data && (
        <>
          <p>
            {resource.data.items.length} saved source entries. Queued videos are
            shown in YouTube activity. Selection here is limited to entries
            still present in the latest source window.
          </p>
          <YouTubeBulk
            url={sub.source_url}
            entries={resource.data.items.filter((v) => !v.job_id)}
            collectionID={sub.collection_id}
            subtitles={sub.subtitles}
            reload={() => {
              resource.reload();
              reload();
            }}
          />
        </>
      )}
    </>
  );
}
