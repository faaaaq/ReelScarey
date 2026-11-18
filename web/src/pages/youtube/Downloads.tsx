import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../../api";
import { PageHeading, RequestState } from "../../components";
import {
  ImportReviewFacts,
  ImportReviewSummary,
} from "../../components/ImportReview";
import { PageBody } from "../../ui";
import { useLiveResource } from "../../useLiveResource";
import { JobCard } from "./JobCard";
import { bytes } from "./status";
import type { ImportReview, Job, JobsResponse } from "./types";
import "../Admin.css";

type FilterKey = "all" | "review" | "subs" | "failed" | "active";

const FILTERS: { key: FilterKey; label: string }[] = [
  { key: "all", label: "All" },
  { key: "review", label: "Review needed" },
  { key: "subs", label: "Missing English subtitles" },
  { key: "failed", label: "Failed" },
  { key: "active", label: "Active" },
];

function matches(job: Job, filter: FilterKey) {
  switch (filter) {
    case "review":
      return job.state === "awaiting_review";
    case "subs":
      return (
        !job.english_subtitles &&
        (job.state === "awaiting_review" || job.state === "completed")
      );
    case "failed":
      return job.state === "failed";
    case "active":
      return ["queued", "downloading", "importing"].includes(job.state);
    default:
      return true;
  }
}

/** Group label for a job: its per-show source when known, else its target. */
function groupKey(job: Job) {
  return job.series_source_id
    ? `source:${job.series_source_id}`
    : `target:${job.target_label || "other"}`;
}

/** Deep link for a group's owning title, when the target carries one. */
function groupLink(jobs: Job[]) {
  const withTarget = jobs.find((job) => job.target?.media_id);
  const target = withTarget?.target;
  if (!target?.media_id) return undefined;
  if (target.kind === "episode") return `/series/${target.media_id}`;
  if (target.kind === "movie") return `/movies/${target.media_id}`;
  return undefined;
}

export function Downloads() {
  const [params, setParams] = useSearchParams();
  const filter = (params.get("filter") as FilterKey) ?? "all";
  const valid = FILTERS.some((item) => item.key === filter)
    ? filter
    : ("all" as FilterKey);
  const jobs = useLiveResource<JobsResponse>("/youtube/jobs", 5000);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [importReview, setImportReview] = useState<ImportReview>();

  const items = jobs.data?.items ?? [];
  const counts = jobs.data?.counts;
  const chipCounts: Record<FilterKey, number> = {
    all: items.length,
    review:
      counts?.awaiting_review ??
      items.filter((job) => matches(job, "review")).length,
    subs: items.filter((job) => matches(job, "subs")).length,
    failed:
      counts?.failed ?? items.filter((job) => matches(job, "failed")).length,
    active:
      counts && counts.queued !== undefined
        ? counts.queued + counts.downloading
        : items.filter((job) => matches(job, "active")).length,
  };
  const visible = items.filter((job) => matches(job, valid));

  // Group in first-appearance order so the newest jobs lead.
  const groups: { key: string; jobs: Job[] }[] = [];
  for (const job of visible) {
    const key = groupKey(job);
    const group = groups.find((candidate) => candidate.key === key);
    if (group) group.jobs.push(job);
    else groups.push({ key, jobs: [job] });
  }

  async function run<T>(
    operation: string,
    task: () => Promise<T>,
    complete: (result: T) => void,
  ) {
    if (busy) return;
    setBusy(operation);
    setError("");
    try {
      complete(await task());
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }
  function reviewImport(job: Job) {
    void run(
      `review-${job.id}`,
      () =>
        api<ImportReview>(
          `/youtube/jobs/${encodeURIComponent(job.id)}/import-review`,
        ),
      setImportReview,
    );
  }
  /** Subtitle-only retry: the verified video is never redownloaded. */
  function retrySubtitles(job: Job) {
    void run(
      `subtitles-${job.id}`,
      () =>
        api<{ status: string }>(
          `/youtube/jobs/${encodeURIComponent(job.id)}/subtitles`,
          { method: "POST", body: "{}" },
        ),
      () => {
        setNotice("Subtitle retry started. The video is not downloaded again.");
        jobs.reload();
      },
    );
  }
  function cancelJob(job: Job) {
    void run(
      `cancel-${job.id}`,
      () =>
        api<{ status: string }>(
          `/youtube/jobs/${encodeURIComponent(job.id)}/cancel`,
          { method: "POST", body: "{}" },
        ),
      () => {
        setNotice("Cancelled. Isolated files, if any, were retained.");
        jobs.reload();
      },
    );
  }
  function importJob() {
    if (!importReview) return;
    void run(
      `import-${importReview.job_id}`,
      () =>
        api<{ status: string }>(
          `/youtube/jobs/${encodeURIComponent(importReview.job_id)}/import`,
          { method: "POST", body: JSON.stringify(importReview) },
        ),
      () => {
        setImportReview(undefined);
        setNotice("Import requested.");
        jobs.reload();
      },
    );
  }

  return (
    <>
      <PageBody>
        <PageHeading
          title="YouTube downloads"
          description="Verified YouTube downloads and their import reviews."
        />
        {error && (
          <p className="rs-alert is-danger" role="alert">
            {error}
          </p>
        )}
        {notice && (
          <p className="rs-alert is-success" role="status">
            {notice}
          </p>
        )}
        <div
          className="yt-chips"
          role="group"
          aria-label="Filter downloads by state"
        >
          {FILTERS.map((item) => (
            <button
              key={item.key}
              type="button"
              className="yt-chip"
              aria-pressed={valid === item.key}
              onClick={() =>
                setParams(
                  (current) => {
                    const next = new URLSearchParams(current);
                    if (item.key === "all") next.delete("filter");
                    else next.set("filter", item.key);
                    return next;
                  },
                  { replace: true },
                )
              }
            >
              {item.label} ({chipCounts[item.key]})
            </button>
          ))}
        </div>
        {!counts && items.length >= 200 && (
          <p className="rs-form-help">Showing the latest 200 downloads.</p>
        )}
        <ImportReviewSummary
          ready={
            counts?.awaiting_review ??
            items.filter((job) => job.state === "awaiting_review").length
          }
          needsDecision={
            counts?.failed ??
            items.filter((job) => job.state === "failed").length
          }
          skipped={items.filter((job) => job.state === "cancelled").length}
        />
        <section className="rs-panel yt-queue" aria-label="YouTube downloads">
          <div className="rs-panel-header">
            <h2>Downloads</h2>
            <Button variant="ghost" onClick={jobs.reload}>
              Refresh
            </Button>
          </div>
          <RequestState
            loading={jobs.loading}
            error={jobs.error}
            retry={jobs.reload}
          />
          {items.length === 0 && !jobs.loading && !jobs.error && (
            <p className="yt-empty">No downloads yet.</p>
          )}
          {items.length > 0 && visible.length === 0 && (
            <p className="yt-empty">No downloads match this filter.</p>
          )}
          {groups.map((group) => {
            const href = groupLink(group.jobs);
            return (
              <section key={group.key} className="yt-job-group">
                <h3>
                  {group.jobs[0].target_label || "Downloads"}
                  {href && (
                    <>
                      {" "}
                      <Link className="yt-group-link" to={href}>
                        Open in library
                      </Link>
                    </>
                  )}
                </h3>
                <ul className="yt-jobs">
                  {group.jobs.map((job) => (
                    <JobCard
                      key={job.id}
                      job={job}
                      busy={!!busy}
                      reviewImport={reviewImport}
                      cancelJob={cancelJob}
                      retrySubtitles={retrySubtitles}
                    />
                  ))}
                </ul>
              </section>
            );
          })}
          {importReview && (
            <div className="review-box" aria-label="Import review">
              <h3>{importReview.target_label}</h3>
              <p className="rs-form-help">
                Source file:{" "}
                {importReview.source_file ??
                  "Verified staging file (source path unavailable in this older review)"}
              </p>
              <ImportReviewFacts
                identity={importReview.target_label}
                destination={importReview.destination}
                operation="Publish the already downloaded, verified video and selected subtitles through the durable import journal; do not redownload the video."
                evidence={[
                  `Verified video SHA-256: ${importReview.sha256}`,
                  `${bytes(importReview.size_bytes)} downloaded and inspected.`,
                  ...importReview.subtitles.map(
                    (sub) =>
                      `${sub.language} subtitles verified at ${sub.destination} (SHA-256 ${sub.sha256}).`,
                  ),
                ]}
              />
              <p>
                {bytes(importReview.size_bytes)} · SHA-256{" "}
                {importReview.sha256.slice(0, 16)}…
              </p>
              {importReview.subtitles.map((sub) => (
                <p key={sub.language} className="rs-form-help">
                  {sub.language}: {sub.destination} · SHA-256 {sub.sha256}
                </p>
              ))}
              <div className="rs-form-actions">
                <Button disabled={!!busy} onClick={importJob}>
                  Import
                </Button>
                <Button
                  variant="soft"
                  onClick={() => setImportReview(undefined)}
                >
                  Cancel
                </Button>
              </div>
            </div>
          )}
        </section>
      </PageBody>
    </>
  );
}
