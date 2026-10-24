import { useEffect, useState } from "react";
import { Badge, Button } from "@radix-ui/themes";
import { Link, useSearchParams } from "react-router-dom";
import { api, messageFor, type DownloadJob, type DownloadPage } from "../api";
import { PageHeading, RequestState } from "../components";
import { Pagination } from "../movieComponents";
import { bytes } from "./Releases";
import { TVMapping } from "./TVMapping";
import { useLiveResource } from "../useLiveResource";

const labels: Record<string, string> = {
  queued: "Queued",
  submitting: "Sending to download client",
  downloading: "Downloading",
  importing: "Verifying & importing",
  blocked: "Needs attention",
  failed: "Retry needed",
  completed: "In library",
  cancelled: "Tracking stopped",
};
export function Activity() {
  const [params] = useSearchParams();
  const id = params.get("job");
  return id ? <FocusedJob key={id} id={id} /> : <ActivityList />;
}
function FocusedJob({ id }: { id: string }) {
  const valid = /^[a-f0-9]{32}$/.test(id);
  return valid ? (
    <FocusedJobContent id={id} />
  ) : (
    <p role="alert">Choose a valid download from its library item.</p>
  );
}
function FocusedJobContent({ id }: { id: string }) {
  const job = useLiveResource<DownloadJob>(`/downloads/${id}`, 5000);
  return (
    <>
      <PageHeading
        title="Download details"
        description="Release, progress, files and safe import status."
      />
      <Link className="text-link" to="/activity">
        View all activity
      </Link>
      <RequestState
        loading={job.loading}
        error={job.error}
        retry={job.reload}
      />
      {job.data && <JobCard job={job.data} reload={job.reload} />}
    </>
  );
}
function ActivityList() {
  const [params, setParams] = useSearchParams();
  const page = Number(params.get("page") ?? 1);
  const [data, setData] = useState<DownloadPage>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  function reload() {
    setAttempt((n) => n + 1);
  }
  useEffect(() => {
    const controller = new AbortController();
    let busy = false;
    setLoading(true);
    setData(undefined);
    async function refresh() {
      if (busy) return;
      busy = true;
      try {
        const out = await api<DownloadPage>(`/downloads?page=${page}`, {
          signal: AbortSignal.any([
            controller.signal,
            AbortSignal.timeout(15000),
          ]),
        });
        if (!controller.signal.aborted) {
          setData(out);
          setError("");
        }
      } catch (e) {
        if (!controller.signal.aborted) setError(messageFor(e));
      } finally {
        busy = false;
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void refresh();
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [page, attempt]);
  return (
    <>
      <PageHeading
        title="Activity"
        description="Follow your movies and episodes from download to library."
      >
        <Button variant="soft" onClick={reload}>
          Refresh
        </Button>
      </PageHeading>
      <RequestState loading={loading} error={error} retry={reload} />
      {data && (
        <>
          {data.items.length ? (
            <div className="activity-list">
              {data.items.map((job) => (
                <JobCard key={job.id} job={job} reload={reload} />
              ))}
            </div>
          ) : (
            <section className="panel search-intro">
              <h2>Your next download starts in Movies.</h2>
              <p>
                Choose a movie, search your connected indexer, and select a
                release. Only downloads started by ReelScarey appear here.
              </p>
              <Button asChild>
                <Link to="/movies">Open movies</Link>
              </Button>
            </section>
          )}
          <Pagination
            page={data.page}
            pages={Math.ceil(data.total / data.limit)}
            onPage={(n) => setParams({ page: String(n) })}
          />
        </>
      )}
      <p className="search-disclaimer">
        Progress is saved on the server and resumes after a restart. Stopping
        tracking leaves the client job and its source files unchanged.
      </p>
    </>
  );
}
function JobCard({ job, reload }: { job: DownloadJob; reload: () => void }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [chosen, setChosen] = useState("");
  const percent = Math.min(100, Math.max(0, Math.round(job.progress * 100)));
  const attention = job.state === "blocked" || job.state === "failed";
  async function action(kind: "retry" | "stop" | "file") {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/downloads/${job.id}/${kind}`, {
        method: "POST",
        body: JSON.stringify(kind === "file" ? { index: Number(chosen) } : {}),
      });
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <article className="panel activity-card">
      <div className="section-title">
        <h2>{job.movie_title}</h2>
        <Badge
          color={
            job.state === "completed" ? "green" : attention ? "orange" : "gray"
          }
        >
          {labels[job.state] ?? job.state}
        </Badge>
      </div>
      <p className="release-title">{job.title}</p>
      <p className="field-help">
        {job.protocol === "usenet"
          ? "Usenet · SABnzbd"
          : "Torrent · qBittorrent"}
        {job.client_id ? ` · ${job.client_id}` : ""}
      </p>
      {job.state !== "cancelled" && (
        <>
          <progress
            aria-label={`Download progress for ${job.movie_title}`}
            value={job.progress}
            max={1}
          />
          <p className="activity-progress">
            {percent}%
            {job.total_bytes
              ? ` · ${bytes(job.downloaded_bytes)} of ${bytes(job.total_bytes)}`
              : ""}
            {job.client_state ? ` · ${job.client_state}` : ""}
          </p>
        </>
      )}
      {job.error_message && (
        <p className={attention ? "movie-error" : "field-help"}>
          {job.error_message}
        </p>
      )}
      {job.error_code === "choose_file" && attention && (
        <div className="file-choices">
          <label htmlFor={`file-${job.id}`}>Movie file to import</label>
          <select
            id={`file-${job.id}`}
            value={chosen}
            onChange={(e) => setChosen(e.target.value)}
            disabled={pending}
          >
            <option value="">Choose a movie file…</option>
            {job.choices.map((file) => (
              <option key={file.index} value={file.index}>
                {file.name} ({bytes(file.size_bytes)})
              </option>
            ))}
          </select>
          <Button
            disabled={pending || chosen === ""}
            onClick={() => void action("file")}
          >
            Import selected file
          </Button>
        </div>
      )}
      {job.kind === "series" &&
        job.error_code === "map_episodes" &&
        attention && (
          <TVMapping
            key={`${job.id}-${job.updated_at}`}
            job={job}
            reload={reload}
          />
        )}
      {job.kind === "series" &&
        !!job.targets?.length &&
        job.error_code !== "map_episodes" && (
          <p className="field-help">
            {job.targets.filter((t) => t.imported && !t.skipped).length}{" "}
            imported · {job.targets.filter((t) => t.skipped).length} skipped ·{" "}
            {job.targets.length} target episodes
          </p>
        )}
      <div className="provider-actions">
        {attention &&
          job.error_code !== "choose_file" &&
          job.error_code !== "map_episodes" && (
            <Button disabled={pending} onClick={() => void action("retry")}>
              Retry this download
            </Button>
          )}
        {attention && (
          <Button
            variant="soft"
            color="gray"
            disabled={pending}
            onClick={() => void action("stop")}
          >
            Stop tracking
          </Button>
        )}
        {job.state === "cancelled" && (
          <Button
            variant="soft"
            disabled={pending}
            onClick={() => void action("retry")}
          >
            Resume tracking
          </Button>
        )}
        {job.state === "completed" && (
          <Button asChild variant="soft">
            <Link
              to={job.kind === "series" ? `/series/${job.movie_id}` : "/movies"}
            >
              Open library
            </Link>
          </Button>
        )}
      </div>
      {error && (
        <p role="alert" className="movie-error">
          {error}
        </p>
      )}
    </article>
  );
}
