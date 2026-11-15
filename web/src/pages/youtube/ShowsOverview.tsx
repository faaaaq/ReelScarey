import { useState } from "react";
import { Link } from "react-router-dom";
import { Badge, Button } from "@radix-ui/themes";
import { api, messageFor } from "../../api";
import { PageHeading, RequestState } from "../../components";
import { PageBody } from "../../ui";
import { useLiveResource } from "../../useLiveResource";
import { mappingLabel, sourceState } from "./status";
import type { SourcesOverviewItem, SourcesOverviewResponse } from "./types";
import "../Admin.css";

function checkedAt(seconds: number) {
  return new Date(seconds * 1000).toLocaleString();
}

function countChips(source: SourcesOverviewItem) {
  const counts = source.counts;
  if (!counts) return [];
  const entries: { label: string; value: number }[] = [
    { label: "Missing", value: counts.missing },
    { label: "Queued", value: counts.queued },
    { label: "Downloading", value: counts.downloading },
    { label: "Review needed", value: counts.awaiting_review },
    { label: "Completed", value: counts.completed },
    { label: "Downloaded", value: counts.downloaded },
    { label: "Entries", value: counts.entries },
  ];
  return entries.filter((entry) => entry.value > 0);
}

/**
 * Cross-show overview of per-show YouTube sources. Read-only feed;
 * Enable/Pause and Check now use the existing per-show source APIs.
 */
export function ShowsOverview() {
  const sources = useLiveResource<SourcesOverviewResponse>(
    "/youtube/sources",
    15000,
  );
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function run(
    operation: string,
    task: () => Promise<unknown>,
    done: string,
  ) {
    if (busy) return;
    setBusy(operation);
    setError("");
    try {
      await task();
      setNotice(done);
      sources.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }
  /** Round-trip the full source body: every editable field is serialized by
   * the overview endpoint, so toggling enabled cannot clobber saved values. */
  function toggleBody(source: SourcesOverviewItem) {
    return {
      source_url: source.source_url,
      mapping: source.mapping,
      season_number: source.season_number,
      start_episode: source.start_episode,
      quality: source.quality,
      subtitles: source.subtitles,
      auto_import: source.auto_import,
      interval_hours: source.interval_hours,
      enabled: !source.enabled,
      revision: source.revision,
      reviewed: true,
    };
  }

  return (
    <PageBody>
      <PageHeading
        title="YouTube shows"
        description="Saved YouTube sources and the episodes they watch."
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
      <RequestState
        loading={sources.loading}
        error={sources.error}
        retry={sources.reload}
      />
      {(sources.data?.items.length ?? 0) === 0 &&
        !sources.loading &&
        !sources.error && (
          <p className="yt-empty">
            No YouTube sources yet. Open a show and add a source from its
            YouTube tab, or paste a playlist on the Discover page and choose
            "Add to a show".
          </p>
        )}
      <ul className="yt-overview">
        {sources.data?.items.map((source) => {
          const badge = sourceState(source);
          return (
            <li key={source.id} className="yt-overview-card">
              <div className="yt-source-head">
                <div>
                  <strong>
                    <Link to={`/series/${source.show?.id ?? source.media_id}`}>
                      {source.show_title}
                    </Link>
                  </strong>
                  <span>{source.source_url}</span>
                </div>
                <Badge color={badge.color}>{badge.label}</Badge>
              </div>
              <span className="yt-overview-meta">
                {mappingLabel(source.mapping, source.season_number)} ·{" "}
                {source.quality === "best" ? "Best" : `${source.quality}p`} ·{" "}
                {source.auto_import
                  ? "auto-import exact matches"
                  : "review imports"}
                {source.last_checked
                  ? ` · checked ${checkedAt(source.last_checked)}`
                  : ""}
                {source.next_run
                  ? ` · next run ${checkedAt(source.next_run)}`
                  : ""}
              </span>
              {source.message && (
                <span className="yt-job-message">{source.message}</span>
              )}
              <div className="yt-counts" aria-label="Episode counts">
                {countChips(source).map((chip) => (
                  <span key={chip.label} className="yt-count-chip">
                    {chip.label}: {chip.value}
                  </span>
                ))}
              </div>
              <div className="rs-form-actions yt-row-actions">
                <Button
                  size="1"
                  variant="soft"
                  disabled={!!busy}
                  onClick={() =>
                    run(
                      `enable-${source.id}`,
                      () =>
                        api(
                          `/library/series/${source.media_id}/youtube-sources`,
                          {
                            method: "POST",
                            body: JSON.stringify(toggleBody(source)),
                          },
                        ),
                      source.enabled ? "Source paused." : "Source enabled.",
                    )
                  }
                >
                  {source.enabled ? "Pause" : "Enable"}
                </Button>
                <Button
                  size="1"
                  variant="soft"
                  disabled={!!busy || source.state === "requested"}
                  onClick={() =>
                    run(
                      `check-${source.id}`,
                      () =>
                        api(
                          `/library/series/${source.media_id}/youtube-sources/${source.id}`,
                          {
                            method: "POST",
                            body: JSON.stringify({ revision: source.revision }),
                          },
                        ),
                      "Check requested.",
                    )
                  }
                >
                  Check now
                </Button>
              </div>
            </li>
          );
        })}
      </ul>
    </PageBody>
  );
}
