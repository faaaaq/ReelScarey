import { useEffect, useState } from "react";
import { Button } from "@radix-ui/themes";
import {
  CircleAlert,
  CircleCheck,
  CircleSlash,
  Clock,
  Download,
  History,
  Info,
  ListFilter,
  RefreshCw,
  TriangleAlert,
  Upload,
} from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { api, messageFor, type DownloadJob, type DownloadPage } from "../api";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  ConfirmDialog,
  DescriptionItem,
  DescriptionList,
  EmptyState,
  Label,
  LoadingIndicator,
  Modal,
  ModalClose,
  PageBody,
  PageToolbar,
  ProgressBar,
  Table,
  ToolbarButton,
  ToolbarSection,
  episodeCode,
  formatRelative,
  formatTimestamp,
  pageParam,
  type Kind,
} from "../ui";
import { releaseBytes as bytes } from "../ui";
import { EpisodeMappingReview } from "./activity/EpisodeMappingReview";
import { Pager } from "./activity/Pager";
import { ToolbarMenu } from "./activity/ToolbarMenu";
import { useLiveResource } from "../useLiveResource";
import "./Activity.css";

const states: Record<string, { label: string; kind: Kind }> = {
  queued: { label: "Queued", kind: "info" },
  submitting: { label: "Sending to download client", kind: "info" },
  downloading: { label: "Downloading", kind: "primary" },
  importing: { label: "Verifying & importing", kind: "purple" },
  blocked: { label: "Needs attention", kind: "warning" },
  failed: { label: "Retry needed", kind: "danger" },
  completed: { label: "In library", kind: "success" },
  cancelled: { label: "Tracking stopped", kind: "disabled" },
};
const versionNames: Record<string, string> = {
  hd: "HD",
  uhd: "4K",
  additional: "Additional copy",
  replacement: "Reviewed replacement",
};
const filters = [
  { value: "all", label: "All on queue" },
  { value: "attention", label: "Needs attention" },
  { value: "active", label: "In progress" },
  { value: "cancelled", label: "Tracking stopped" },
];
const active = new Set(["queued", "submitting", "downloading", "importing"]);

function needsAttention(job: DownloadJob) {
  return job.state === "blocked" || job.state === "failed";
}
function matchesFilter(job: DownloadJob, filter: string) {
  if (filter === "attention") return needsAttention(job);
  if (filter === "active") return active.has(job.state);
  if (filter === "cancelled") return job.state === "cancelled";
  // Completed downloads leave the queue and live in History.
  if (filter === "completed") return false;
  return true;
}
function stateOf(job: DownloadJob) {
  return states[job.state] ?? { label: job.state, kind: "default" as Kind };
}
function StateIcon({ job }: { job: DownloadJob }) {
  const props = { size: 16, "aria-hidden": true as const };
  if (job.state === "completed") return <CircleCheck {...props} />;
  if (job.state === "failed") return <CircleAlert {...props} />;
  if (job.state === "blocked") return <TriangleAlert {...props} />;
  if (job.state === "cancelled") return <CircleSlash {...props} />;
  if (job.state === "importing") return <Upload {...props} />;
  if (job.state === "downloading") return <Download {...props} />;
  return <Clock {...props} />;
}
function mediaPath(job: DownloadJob) {
  return job.kind === "series"
    ? `/series/${job.movie_id}`
    : `/movies/${job.movie_id}`;
}
function targetSummary(job: DownloadJob) {
  const targets = job.targets ?? [];
  if (!targets.length) return "";
  const first = targets[0];
  if (targets.length === 1) return episodeCode(first.season, first.number);
  const last = targets[targets.length - 1];
  return `${episodeCode(first.season, first.number)}–${episodeCode(last.season, last.number)} · ${targets.length} episodes`;
}
function clientName(job: DownloadJob) {
  return (
    job.client_label ?? (job.protocol === "usenet" ? "SABnzbd" : "qBittorrent")
  );
}
function reviewLabel(job: DownloadJob) {
  if (job.error_code === "map_episodes") return "Review files";
  if (job.error_code === "choose_file") return "Choose file";
  return "Review";
}

export function Activity() {
  const [params, setParams] = useSearchParams();
  const page = pageParam(params.get("page"));
  const focus = params.get("job");
  // Completed is no longer a queue filter; old bookmarks fall back to all.
  const requestedFilter = params.get("state") ?? "all";
  const filter = filters.some((option) => option.value === requestedFilter)
    ? requestedFilter
    : "all";
  const [data, setData] = useState<DownloadPage>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const [openID, setOpenID] = useState("");
  function reload() {
    setAttempt((n) => n + 1);
  }
  useEffect(() => {
    setData(undefined);
  }, [page]);
  useEffect(() => {
    const controller = new AbortController();
    let busy = false;
    setLoading(true);
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
  function setParam(key: string, value: string | null) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next);
  }
  const focusValid = !!focus;
  const items = data?.items ?? [];
  const visible = items.filter((job) => matchesFilter(job, filter));
  const attention = items.filter(needsAttention).length;
  const opened = items.find((job) => job.id === openID);
  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            spinning={loading && !!data}
            onClick={reload}
          />
          <ToolbarButton icon={History} label="History" to="/timeline" />
        </ToolbarSection>
        <ToolbarSection align="right">
          <ToolbarMenu
            icon={ListFilter}
            label="Filter"
            menuLabel="Queue filter"
            value={filter}
            options={filters}
            onChange={(value) => {
              const next = new URLSearchParams(params);
              if (value === "all") next.delete("state");
              else next.set("state", value);
              setParams(next);
            }}
          />
        </ToolbarSection>
      </PageToolbar>
      <PageBody>
        <PageHeading
          title="Activity"
          description="Active downloads only. Completed items appear in History."
        />
        <p className="rs-muted rs-small">
          Finished downloads are kept in <Link to="/timeline">History</Link>,
          never on this queue.
        </p>
        {focus && !focusValid && (
          <Alert kind="danger" role="alert">
            Choose a valid download from its library item.
          </Alert>
        )}
        {!data && (
          <RequestState loading={loading} error={error} retry={reload} />
        )}
        {data && error && (
          <Alert kind="warning">
            Could not refresh the queue: {error} Showing the last loaded state.
          </Alert>
        )}
        {data && attention > 0 && filter !== "attention" && (
          <Alert kind="warning" title="Downloads need attention">
            <p>
              {attention}{" "}
              {attention === 1 ? "download needs" : "downloads need"} your
              review before anything is imported.{" "}
              <button
                type="button"
                className="rs-link-button"
                onClick={() => setParam("state", "attention")}
              >
                Show only these
              </button>
            </p>
          </Alert>
        )}
        {data &&
          (items.length === 0 ? (
            <EmptyState
              icon={Download}
              title="No active downloads"
              actions={
                <>
                  <Button asChild variant="soft">
                    <Link to="/wanted">Open Wanted</Link>
                  </Button>
                  <Button asChild variant="soft">
                    <Link to="/timeline">View history</Link>
                  </Button>
                </>
              }
            >
              <p>
                Downloads appear here after you choose a release. Completed
                items are kept in History.
              </p>
            </EmptyState>
          ) : (
            <>
              {filter !== "all" && (
                <p className="rs-muted rs-small">
                  {visible.length} of {items.length} downloads on this page
                  match “{filters.find((f) => f.value === filter)?.label}”.{" "}
                  <button
                    type="button"
                    className="rs-link-button"
                    onClick={() => setParam("state", null)}
                  >
                    Show all
                  </button>
                </p>
              )}
              <QueueTable
                jobs={visible}
                focus={focusValid ? focus : ""}
                onOpen={setOpenID}
              />
            </>
          ))}
        {data && (
          <Pager
            page={data.page}
            pages={Math.ceil(data.total / data.limit)}
            onPage={(n) => setParam("page", String(n))}
          />
        )}
      </PageBody>
      {openID && (
        <JobModal
          open
          job={opened}
          onClose={() => setOpenID("")}
          reload={reload}
        />
      )}
      {focusValid && !openID && (
        <FocusedJob id={focus} onClose={() => setParam("job", null)} />
      )}
    </>
  );
}

function QueueTable({
  jobs,
  focus,
  onOpen,
}: {
  jobs: DownloadJob[];
  focus: string;
  onOpen: (id: string) => void;
}) {
  return (
    <Table label="Queue" className="act-table">
      <thead>
        <tr>
          <th scope="col" className="is-shrink">
            <span className="sr-only">State icon</span>
          </th>
          <th scope="col">Status</th>
          <th scope="col">Title</th>
          <th scope="col" className="act-col-protocol">
            Protocol
          </th>
          <th scope="col" className="act-col-client">
            Client
          </th>
          <th scope="col" className="is-numeric act-col-size">
            Size
          </th>
          <th scope="col" className="act-col-progress">
            Progress
          </th>
          <th scope="col" className="act-col-updated">
            Updated
          </th>
          <th scope="col" className="is-actions">
            <span className="sr-only">Actions</span>
          </th>
        </tr>
      </thead>
      <tbody>
        {jobs.length === 0 && (
          <tr>
            <td colSpan={9} className="rs-muted">
              No downloads on this page match the filter.
            </td>
          </tr>
        )}
        {jobs.map((job) => (
          <QueueRow
            key={job.id}
            job={job}
            selected={job.id === focus}
            onOpen={() => onOpen(job.id)}
          />
        ))}
      </tbody>
    </Table>
  );
}

function QueueRow({
  job,
  selected,
  onOpen,
}: {
  job: DownloadJob;
  selected: boolean;
  onOpen: () => void;
}) {
  const state = stateOf(job);
  const attention = needsAttention(job);
  const percent = Math.min(100, Math.max(0, Math.round(job.progress * 100)));
  const episodes = job.kind === "series" ? targetSummary(job) : "";
  return (
    <tr
      className={[
        selected ? "is-selected" : "",
        attention
          ? `act-row-${job.state === "failed" ? "danger" : "warning"}`
          : "",
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <td className={`act-icon is-${state.kind}`} title={state.label}>
        <StateIcon job={job} />
      </td>
      <td className="act-col-status">
        <Label kind={state.kind}>{state.label}</Label>
        {job.client_state && (
          <span className="act-sub rs-muted">{job.client_state}</span>
        )}
      </td>
      <td className="act-col-title">
        <Link to={mediaPath(job)} className="act-media">
          {job.movie_title}
        </Link>
        {episodes && <span className="act-episodes">{episodes}</span>}
        {job.movie_version && (
          <Label outline>
            {versionNames[job.movie_version] || job.movie_version}
          </Label>
        )}
        <span className="act-release" title={job.title}>
          {job.title}
        </span>
        {job.error_message && (
          <span
            className={
              job.state === "failed"
                ? "act-message rs-danger-text"
                : attention
                  ? "act-message rs-warning-text"
                  : "act-message rs-muted"
            }
          >
            {job.error_message}
          </span>
        )}
      </td>
      <td className="act-col-protocol">
        <Label kind={job.protocol === "usenet" ? "purple" : "info"} outline>
          {job.protocol === "usenet" ? "Usenet" : "Torrent"}
        </Label>
      </td>
      <td className="act-col-client">{clientName(job)}</td>
      <td className="is-numeric act-col-size">
        {job.total_bytes
          ? `${bytes(job.downloaded_bytes)} of ${bytes(job.total_bytes)}`
          : "—"}
      </td>
      <td className="act-col-progress">
        {job.state !== "cancelled" ? (
          <ProgressBar
            value={job.progress}
            kind={
              job.state === "completed"
                ? "success"
                : job.state === "failed"
                  ? "danger"
                  : job.state === "blocked"
                    ? "warning"
                    : job.state === "importing"
                      ? "purple"
                      : "primary"
            }
            text={`${percent}%`}
            label={`Download progress for ${job.movie_title}`}
          />
        ) : (
          <span className="rs-muted">—</span>
        )}
      </td>
      <td className="act-col-updated" title={formatTimestamp(job.updated_at)}>
        {formatRelative(job.updated_at)}
      </td>
      <td className="is-actions">
        <div className="act-actions">
          {attention && (
            <Button
              size="1"
              color="orange"
              onClick={onOpen}
              aria-label={`${reviewLabel(job)} for ${job.movie_title}`}
            >
              {reviewLabel(job)}
            </Button>
          )}
          <button
            type="button"
            className="act-icon-button"
            onClick={onOpen}
            aria-label={`Details for ${job.movie_title}`}
            title="Details"
          >
            <Info size={16} aria-hidden="true" />
          </button>
        </div>
      </td>
    </tr>
  );
}

function FocusedJob({ id, onClose }: { id: string; onClose: () => void }) {
  const job = useLiveResource<DownloadJob>(`/downloads/${id}`, 5000);
  return (
    <JobModal
      open
      job={job.data}
      loading={job.loading}
      error={job.error}
      onClose={onClose}
      reload={job.reload}
    />
  );
}

function JobModal({
  open,
  job,
  loading,
  error,
  onClose,
  reload,
}: {
  open: boolean;
  job?: DownloadJob;
  loading?: boolean;
  error?: string;
  onClose: () => void;
  reload: () => void;
}) {
  return (
    <Modal
      open={open}
      onOpenChange={(next) => !next && onClose()}
      size="large"
      title={job ? job.movie_title : "Download details"}
      footer={
        job ? (
          <JobActions job={job} reload={reload} onClose={onClose} />
        ) : (
          <ModalClose>
            <Button variant="soft" color="gray">
              Close
            </Button>
          </ModalClose>
        )
      }
    >
      {!job &&
        (loading ? (
          <LoadingIndicator />
        ) : error ? (
          <Alert kind="danger" role="alert">
            {error}
          </Alert>
        ) : (
          <p className="rs-muted">
            This download is no longer on the current page.
          </p>
        ))}
      {job && <JobDetails job={job} reload={reload} onClose={onClose} />}
    </Modal>
  );
}

function JobDetails({
  job,
  reload,
  onClose,
}: {
  job: DownloadJob;
  reload: () => void;
  onClose: () => void;
}) {
  const state = stateOf(job);
  const attention = needsAttention(job);
  const percent = Math.min(100, Math.max(0, Math.round(job.progress * 100)));
  const [chosen, setChosen] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function importFile() {
    if (pending || chosen === "") return;
    setPending(true);
    setError("");
    try {
      await api(`/downloads/${job.id}/file`, {
        method: "POST",
        body: JSON.stringify({ index: Number(chosen) }),
      });
      reload();
      onClose();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <>
      {job.error_message && (
        <Alert
          kind={
            job.state === "failed" ? "danger" : attention ? "warning" : "info"
          }
        >
          {job.error_message}
        </Alert>
      )}
      <DescriptionList>
        <DescriptionItem title="Status">
          <Label kind={state.kind}>{state.label}</Label>
          {job.client_state ? (
            <span className="rs-muted"> · {job.client_state}</span>
          ) : null}
        </DescriptionItem>
        <DescriptionItem title="Release">
          <span className="act-release-full">{job.title}</span>
        </DescriptionItem>
        <DescriptionItem title="Title">
          <Link to={mediaPath(job)}>{job.movie_title}</Link>
          {job.kind === "series" && targetSummary(job)
            ? ` · ${targetSummary(job)}`
            : ""}
        </DescriptionItem>
        {job.movie_version && (
          <DescriptionItem title="Movie version">
            {versionNames[job.movie_version] || job.movie_version}
          </DescriptionItem>
        )}
        <DescriptionItem title="Client">
          {job.protocol === "usenet" ? "Usenet" : "Torrent"} · {clientName(job)}
          {job.client_id ? ` · ${job.client_id}` : ""}
        </DescriptionItem>
        {job.state !== "cancelled" && (
          <DescriptionItem title="Progress">
            {percent}%
            {job.total_bytes
              ? ` · ${bytes(job.downloaded_bytes)} of ${bytes(job.total_bytes)}`
              : ""}
          </DescriptionItem>
        )}
        {job.attempts > 0 && (
          <DescriptionItem title="Attempts">{job.attempts}</DescriptionItem>
        )}
        <DescriptionItem title="Added">
          {formatTimestamp(job.created_at)}
        </DescriptionItem>
        <DescriptionItem title="Updated">
          {formatTimestamp(job.updated_at)}
        </DescriptionItem>
      </DescriptionList>
      {job.error_code === "replacement_review" && (
        <p>
          <Link
            to={`/organization?acquisition_kind=download&acquisition_job=${job.id}`}
          >
            Review acquired replacement
          </Link>
        </p>
      )}
      {job.error_code === "choose_file" && attention && (
        <section className="act-choose" aria-label="Choose the movie file">
          <h3>Choose the movie file</h3>
          <div className="act-choose-row">
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
              onClick={() => void importFile()}
            >
              Import selected file
            </Button>
          </div>
          {error && <Alert kind="danger">{error}</Alert>}
        </section>
      )}
      {job.anime_search && (
        <p className="rs-muted rs-small">
          Anime match:{" "}
          {job.anime_search.mode === "absolute"
            ? `absolute ${job.anime_search.absolute_number}`
            : job.anime_search.mode === "mapped"
              ? "saved provider mappings"
              : "season and episode"}{" "}
          · {job.anime_search.search_title} · search revision{" "}
          {job.anime_search.policy_revision}. Episode file mapping still
          requires review.
        </p>
      )}
      {job.kind === "series" &&
        job.error_code === "map_episodes" &&
        attention && (
          <EpisodeMappingReview
            key={`${job.id}-${job.updated_at}`}
            job={job}
            onDone={() => {
              reload();
              onClose();
            }}
          />
        )}
      {job.anime_order && (
        <section aria-label="Saved Anime episode order" className="act-order">
          <h3>Reviewed Anime order · revision {job.anime_order.revision}</h3>
          <p className="rs-muted rs-small">
            This download keeps its saved identities and episode labels after
            later edits. Completed files require mapping review before import.
          </p>
          <ul>
            {job.anime_order.entries.map((entry) => {
              const target = job.targets?.find(
                (target) => target.episode_id === entry.episode_id,
              );
              return (
                <li key={entry.episode_id}>
                  {entry.release_title} · {entry.label}
                  {target
                    ? ` → ${episodeCode(target.season, target.number)} · ${target.title}`
                    : ""}
                </li>
              );
            })}
          </ul>
          <ul>
            {job.anime_order.identities.map((identity) => (
              <li key={identity.id}>
                <a href={identity.url} target="_blank" rel="noreferrer">
                  AniList: {identity.title}
                </a>
              </li>
            ))}
          </ul>
        </section>
      )}
      {job.kind === "series" &&
        !!job.targets?.length &&
        job.error_code !== "map_episodes" && (
          <p className="rs-muted rs-small">
            {job.targets.filter((t) => t.imported && !t.skipped).length}{" "}
            imported · {job.targets.filter((t) => t.skipped).length} skipped ·{" "}
            {job.targets.length} target episodes
          </p>
        )}
    </>
  );
}

function JobActions({
  job,
  reload,
  onClose,
}: {
  job: DownloadJob;
  reload: () => void;
  onClose: () => void;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const attention = needsAttention(job);
  async function action(kind: "retry" | "stop") {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/downloads/${job.id}/${kind}`, {
        method: "POST",
        body: JSON.stringify({}),
      });
      reload();
      onClose();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <>
      {error && (
        <span role="alert" className="is-left rs-danger-text">
          {error}
        </span>
      )}
      {attention && (
        <ConfirmDialog
          trigger={
            <Button variant="soft" color="red" disabled={pending}>
              Stop tracking
            </Button>
          }
          title="Stop tracking this download?"
          description="Tracking will stop and this job will no longer be retried. You can start a new download for this title later."
          confirmLabel="Stop tracking"
          onConfirm={() => action("stop")}
        />
      )}
      {attention &&
        job.error_code !== "replacement_review" &&
        job.error_code !== "choose_file" &&
        job.error_code !== "map_episodes" && (
          <Button disabled={pending} onClick={() => void action("retry")}>
            Retry this download
          </Button>
        )}
      {job.state === "cancelled" && (
        <Button disabled={pending} onClick={() => void action("retry")}>
          Resume tracking
        </Button>
      )}
      {job.state === "completed" && (
        <Button asChild variant="soft">
          <Link to={mediaPath(job)}>Open library</Link>
        </Button>
      )}
      <ModalClose>
        <Button variant="soft" color="gray">
          Close
        </Button>
      </ModalClose>
    </>
  );
}
