import { useLiveResource } from "../../useLiveResource";
import type { JobsResponse } from "./types";

/** Cross-page signal: how many YouTube downloads are waiting for review.
 * Hidden entirely when the feed is unavailable or nothing needs review. */
export function YouTubeReviewBadge() {
  const jobs = useLiveResource<JobsResponse>("/youtube/jobs", 15000);
  const data = jobs.data;
  if (!data || jobs.error) return null;
  const count =
    data.counts?.awaiting_review ??
    data.items?.filter((job) => job.state === "awaiting_review").length ??
    0;
  if (count <= 0) return null;
  return (
    <span className="yt-nav-count">
      {count}
      <span className="sr-only"> YouTube downloads need review</span>
    </span>
  );
}
