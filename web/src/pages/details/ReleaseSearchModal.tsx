import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Search } from "lucide-react";
import { Link } from "react-router-dom";
import {
  api,
  messageFor,
  type IntegrationStatus,
  type Release,
} from "../../api";
import {
  eventStateKind,
  type Event as HistoryEvent,
} from "../../components/EventRow";
import {
  Alert,
  EmptyState,
  Modal,
  ModalClose,
  episodeCode,
  type SortState,
} from "../../ui";
import { useResource } from "../../useResource";
import {
  ReleaseResultsTable,
  normaliseTitle,
  type ReleaseSortKey,
} from "./ReleaseResults";
import "../Releases.css";

/**
 * In-page release search for one episode or one season of a series, so the
 * detail page never has to leave for a manual search. It reuses the same
 * search API and results table as the /releases page; the only difference is
 * that a triggered download keeps the modal open with an Activity link
 * instead of navigating away.
 */
export function ReleaseSearchModal({
  open,
  onOpenChange,
  seriesID,
  seriesTitle,
  season,
  seasonTitle,
  episode,
  episodeTitle,
  fileID,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  seriesID: number;
  seriesTitle: string;
  season: number;
  seasonTitle: string;
  episode?: number;
  episodeTitle?: string;
  /** Registered file the chosen release will replace after review. */
  fileID?: number;
}) {
  const indexer = useResource<IntegrationStatus>("/indexers/status");
  const client = useResource<IntegrationStatus>("/integrations/qbittorrent");
  const sab = useResource<IntegrationStatus>("/integrations/sabnzbd");
  // Best effort: recent failed download events warn before a repeat attempt.
  // A missing or slow history never blocks the search results.
  const history = useResource<{ items: HistoryEvent[] }>("/events");
  const failedTitles = useMemo(() => {
    const failed = new Set<string>();
    for (const event of history.data?.items ?? []) {
      if (event.kind !== "download") continue;
      if (eventStateKind(event.state) !== "danger") continue;
      failed.add(normaliseTitle(event.title));
    }
    return failed;
  }, [history.data]);
  // The included client makes torrent downloads ready without any setup.
  const clients = useResource<{
    items: { builtin?: boolean; enabled: boolean }[];
  }>("/download-clients");
  const builtinReady = (clients.data?.items ?? []).some(
    (item) => item.builtin && item.enabled,
  );
  const [input, setInput] = useState("");
  const [items, setItems] = useState<Release[]>();
  const [searching, setSearching] = useState(false);
  const [pending, setPending] = useState("");
  const [error, setError] = useState("");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [downloaded, setDownloaded] = useState(false);
  const [sort, setSort] = useState<SortState<ReleaseSortKey>>({
    key: "rank",
    direction: "asc",
  });
  const searched = useRef(false);
  const target = episode
    ? `${episodeCode(season, episode)}${episodeTitle ? ` · ${episodeTitle}` : ""}`
    : seasonTitle;
  const suggested = `${seriesTitle} S${String(season).padStart(2, "0")}${episode ? `E${String(episode).padStart(2, "0")}` : ""}`;

  async function search(e?: FormEvent) {
    e?.preventDefault();
    if (searching) return;
    setSearching(true);
    setError("");
    setItems(undefined);
    try {
      const out = await api<{ items: Release[]; warnings?: string[] }>(
        `/library/series/${seriesID}/seasons/${season}/releases?${fileID ? `replacement_file_id=${fileID}&` : ""}${episode ? `episode=${episode}&` : ""}query=${encodeURIComponent(input.trim() || suggested)}`,
      );
      setItems(out.items);
      setWarnings(out.warnings ?? []);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setSearching(false);
    }
  }

  // The modal opens with the search already running, like a prefilled
  // /releases visit, so the first results need no extra click.
  useEffect(() => {
    if (!open || searched.current) return;
    searched.current = true;
    void search();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  async function download(id: string) {
    if (pending) return;
    setPending(id);
    setError("");
    try {
      await api("/downloads", {
        method: "POST",
        body: JSON.stringify({
          release_id: id,
          ...(fileID ? { replacement_file_id: fileID } : {}),
        }),
      });
      setDownloaded(true);
    } catch (e) {
      setError(
        `${messageFor(e)} Check Activity before retrying if the connection was interrupted.`,
      );
    } finally {
      setPending("");
    }
  }

  const configuredFor = (release: Release) =>
    release.protocol === "usenet"
      ? sab.data?.configured
      : client.data?.configured || builtinReady;

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      size="xlarge"
      title={`Releases for ${seriesTitle}`}
      description={target}
      footer={
        <ModalClose>
          <Button variant="soft" color="gray">
            Close
          </Button>
        </ModalClose>
      }
    >
      {indexer.data && !indexer.data.configured && (
        <EmptyState
          icon={Search}
          title="No indexer connected"
          actions={
            <Button asChild>
              <Link to="/indexers">Connect indexer</Link>
            </Button>
          }
        >
          <p>Add a tracker, RSS feed, Torznab or Newznab endpoint to search.</p>
        </EmptyState>
      )}
      {indexer.data?.configured && (
        <form className="rel-search" onSubmit={(e) => void search(e)}>
          <div className="rel-query">
            <TextField.Root
              aria-label="Release search"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={suggested}
              maxLength={200}
              disabled={searching || !!pending}
            />
            <Button type="submit" disabled={searching || !!pending}>
              <Search size={16} aria-hidden="true" />
              {searching ? "Searching…" : "Search releases"}
            </Button>
          </div>
          <p className="rs-muted rs-small">
            {fileID
              ? "The chosen release replaces this episode's registered file after a reviewed import."
              : "Existing library files are never replaced."}
          </p>
        </form>
      )}
      {client.data &&
        sab.data &&
        !client.data.configured &&
        !sab.data.configured &&
        !builtinReady && (
          <Alert kind="warning">
            <p>
              <Link to="/download-clients">Turn on the built-in client</Link> or
              connect qBittorrent or SABnzbd to download.
            </p>
          </Alert>
        )}
      {error && (
        <Alert kind="danger" role="alert">
          {error}
        </Alert>
      )}
      {warnings.length > 0 && (
        <Alert kind="warning" title="Some indexers need attention">
          <p>Results from the available sources are shown.</p>
          {warnings.map((warning) => (
            <p key={warning}>{warning}</p>
          ))}
          <p>
            <Link to="/indexers">Review indexer connections</Link>
          </p>
        </Alert>
      )}
      {downloaded && (
        <Alert kind="success" title="Download queued" role="status">
          <p>
            The release was sent to your download client. Follow its progress
            and the mapping review in Activity.
          </p>
          <div className="rs-row">
            <Link className="rs-link-button rs-small" to="/activity">
              Open Activity
            </Link>
          </div>
        </Alert>
      )}
      {items && (
        <ReleaseResultsTable
          items={items}
          protocol="all"
          sort={sort}
          onSort={setSort}
          pending={pending}
          onDownload={(id) => void download(id)}
          readyFor={configuredFor}
          failedTitles={failedTitles}
          tv
        />
      )}
    </Modal>
  );
}
