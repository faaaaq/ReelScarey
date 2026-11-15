import { Link } from "react-router-dom";
import { Badge, Button } from "@radix-ui/themes";
import type { Job } from "./types";
import {
  bytes,
  eta,
  qualityLabel,
  speed,
  stateColor,
  stateLabel,
  thumb,
} from "./status";

/** One download job card: progress, subtitle evidence and reviewed actions. */
export function JobCard({
  job,
  busy,
  reviewImport,
  cancelJob,
  retrySubtitles,
}: {
  job: Job;
  busy: boolean;
  reviewImport: (job: Job) => void;
  cancelJob: (job: Job) => void;
  retrySubtitles: (job: Job) => void;
}) {
  const percent = Math.min(100, Math.max(0, Math.round(job.progress * 100)));
  const replacement =
    job.state === "awaiting_review" &&
    job.message.includes("Review replacement");
  // A verified video without English captions is not a usable acquisition.
  const needsSubtitles =
    (job.state === "awaiting_review" || job.state === "completed") &&
    !job.english_subtitles &&
    job.message.includes("English subtitles");
  return (
    <li className="yt-job">
      <img src={thumb(job.video.id)} alt="" loading="lazy" />
      <div className="yt-job-main">
        <div className="yt-job-title">
          <strong>{job.video.title}</strong>
          <Badge color={stateColor(job.state)}>{stateLabel(job.state)}</Badge>
        </div>
        <span className="yt-job-meta">
          {job.video.channel}
          {job.target_label ? ` · ${job.target_label}` : ""}
          {job.quality ? ` · ${qualityLabel(job.quality)}` : ""}
        </span>
        {job.state === "downloading" && (
          <div className="yt-progress">
            <progress
              value={job.total_bytes > 0 ? job.progress : undefined}
              max={1}
              aria-label={`Download progress for ${job.video.title}`}
            />
            <span>
              {job.total_bytes > 0
                ? `${percent}% of ${bytes(job.total_bytes)}`
                : job.downloaded_bytes > 0
                  ? bytes(job.downloaded_bytes)
                  : "Starting…"}
              {speed(job.speed_bps) ? ` · ${speed(job.speed_bps)}` : ""}
              {eta(job.eta_seconds) ? ` · ${eta(job.eta_seconds)}` : ""}
            </span>
          </div>
        )}
        {job.message && <span className="yt-job-message">{job.message}</span>}
        {job.state !== "queued" && job.state !== "downloading" && (
          <span className="yt-job-subtitles">
            {job.english_subtitles ? (
              <Badge color="green">
                English subs
                {job.subtitle_languages?.length
                  ? ` · ${job.subtitle_languages.join(", ")}`
                  : ""}
              </Badge>
            ) : (
              <Badge color="red">English subs missing</Badge>
            )}
          </span>
        )}
        <div className="rs-form-actions">
          {replacement && (
            <Button asChild size="1" variant="soft">
              <Link
                to={`/organization?acquisition_kind=youtube&acquisition_job=${job.id}`}
              >
                Review replacement
              </Link>
            </Button>
          )}
          {needsSubtitles && (
            <Button
              size="1"
              variant="soft"
              disabled={busy}
              onClick={() => retrySubtitles(job)}
            >
              Retry subtitles
            </Button>
          )}
          {job.state === "awaiting_review" && !replacement && (
            <Button size="1" disabled={busy} onClick={() => reviewImport(job)}>
              Review import
            </Button>
          )}
          {["queued", "downloading", "awaiting_review", "failed"].includes(
            job.state,
          ) && (
            <Button
              size="1"
              variant="soft"
              disabled={busy}
              onClick={() => cancelJob(job)}
            >
              Cancel
            </Button>
          )}
        </div>
      </div>
    </li>
  );
}
