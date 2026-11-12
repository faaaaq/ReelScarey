import { useEffect, useRef, useState } from "react";
import { Button } from "@radix-ui/themes";
import {
  ChevronDown,
  FolderTree,
  ListChecks,
  RefreshCw,
  FileX,
  Search,
  SearchCheck,
  Settings2,
} from "lucide-react";
import { Link, useParams } from "react-router-dom";
import {
  api,
  messageFor,
  type TVSeries,
  type TVSeason,
  type TVSeasonDetails,
} from "../api";
import { RequestState } from "../components";
import { Pagination } from "../movieComponents";
import {
  Alert,
  ConfirmDialog,
  Label,
  PageBody,
  PageToolbar,
  Table,
  ToolbarButton,
  ToolbarSection,
  ToolbarSeparator,
  episodeCode,
  formatBytes,
  type Kind,
} from "../ui";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
import {
  ItemSearchResultAlert,
  TitleSearchButton,
  useItemSearch,
} from "./details/ItemSearch";
import { bulkSearchMessage, useBulkSearch } from "./details/bulkSearch";
import { ReleaseSearchModal } from "./details/ReleaseSearchModal";
import { AnimeSettings } from "./AnimeSettings";
import { AnimeSearch } from "./AnimeSearch";
import { AnimeOrder } from "./AnimeOrder";
import { QualityProfile } from "./details/QualityProfile";
import { ItemOrganization } from "./Organization";
import { AutomaticFranchise } from "./AutomaticFranchise";
import { RelatedTitles } from "./RelatedTitles";
import { SeriesReleaseRules } from "./SeriesReleaseRules";
import { SeriesYouTubeSource } from "./SeriesYouTubeSource";
import { ShowSubtitlePreferences } from "./ShowSubtitlePreferences";
import {
  CreditsSection,
  DetailFact,
  DetailHeader,
  DetailTabs,
  FilesSection,
  HistorySection,
  MetadataError,
  MonitorToggle,
  ProviderLinks,
  profileLabel,
} from "./details/DetailParts";
import { useItemDetails } from "./details/useItemDetails";

type Update = (
  path: string,
  method: "PUT" | "POST",
  monitored?: boolean,
  episodeIDs?: number[],
  seasonIDs?: number[],
) => Promise<void>;

export function Series() {
  const { id } = useParams();
  const valid =
    !!id && /^[1-9][0-9]*$/.test(id) && Number.isSafeInteger(Number(id));
  const resource = useResource<TVSeries>(
    valid ? `/library/series/${id}` : null,
  );
  if (resource.data) return <SeriesView key={id} initial={resource.data} />;
  return (
    <PageBody>
      {!valid ? (
        <Alert kind="danger">Choose a show from your library.</Alert>
      ) : (
        <RequestState
          loading={resource.loading}
          error={resource.error}
          retry={resource.reload}
        />
      )}
    </PageBody>
  );
}

function seasonOrder(a: TVSeason, b: TVSeason) {
  if (a.number === 0) return 1;
  if (b.number === 0) return -1;
  // Newest season first, like Sonarr, so the current season is at the top.
  return b.number - a.number;
}

type ReleaseSearchScope = {
  season: number;
  seasonTitle: string;
  episode?: number;
  episodeTitle?: string;
  /** Registered file the chosen release replaces after a reviewed import. */
  fileID?: number;
};

function SeriesView({ initial }: { initial: TVSeries }) {
  const [show, setShow] = useState(initial);
  const [expanded, setExpanded] = useState<number[]>([]);
  const [selectedSeasons, setSelectedSeasons] = useState<number[]>([]);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [releaseSearch, setReleaseSearch] = useState<ReleaseSearchScope | null>(
    null,
  );
  const details = useItemDetails(show.id);
  const firstRevision = useRef(show.revision);
  const base = `/library/series/${show.id}`;
  const { reload: reloadDetails } = details;
  useEffect(() => {
    if (show.revision !== firstRevision.current) {
      firstRevision.current = show.revision;
      reloadDetails();
    }
  }, [show.revision, reloadDetails]);

  const update: Update = async (
    path,
    method,
    monitored,
    episodeIDs,
    seasonIDs,
  ) => {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      const out = await api<TVSeries>(base + path, {
        method,
        body: JSON.stringify({
          revision: show.revision,
          ...(monitored === undefined ? {} : { monitored }),
          ...(episodeIDs === undefined ? {} : { episode_ids: episodeIDs }),
          ...(seasonIDs === undefined ? {} : { season_ids: seasonIDs }),
        }),
      });
      setShow(out);
      setSelectedSeasons([]);
      setAttempt((n) => n + 1);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  };
  async function removeFile(
    episodeID: number,
    _code: string,
    _filePath: string,
  ) {
    setPending(true);
    setError("");
    try {
      const out = await api<TVSeries>(
        `/library/series/${show.id}/episodes/${episodeID}/remove-file`,
        {
          method: "POST",
          body: JSON.stringify({
            revision: show.revision,
            reviewed: true,
            delete_file: true,
          }),
        },
      );
      setShow(out);
      setAttempt((n) => n + 1);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  async function reload() {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      setShow(await api<TVSeries>(base));
      setAttempt((n) => n + 1);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  function batchMonitor(monitored: boolean) {
    void update(
      "/seasons/monitor",
      "PUT",
      monitored,
      undefined,
      selectedSeasons,
    );
  }
  const seasons = [...show.seasons].sort(seasonOrder);
  const loaded = show.seasons.filter((season) => season.loaded_at);
  const available = loaded.reduce(
    (sum, season) => sum + (season.available_episodes ?? 0),
    0,
  );
  const known = loaded.reduce(
    (sum, season) => sum + (season.known_episodes ?? 0),
    0,
  );
  const anime = show.category === "anime";
  const data = details.data;
  const itemSearch = useItemSearch();
  const bulk = useBulkSearch();
  return (
    <>
      <PageToolbar>
        <ToolbarSection>
          <ToolbarButton
            icon={RefreshCw}
            label="Refresh"
            title="Refresh show and season details from TMDb"
            spinning={pending}
            disabled={pending}
            onClick={() => void update("/refresh", "POST")}
          />
          <ToolbarSeparator />
          <TitleSearchButton
            mediaID={show.id}
            pending={!!itemSearch.pending}
            onTrigger={(mediaID) => itemSearch.trigger(mediaID)}
          />
          <ToolbarButton
            icon={ListChecks}
            label="Search All Monitored"
            title="Try a season pack first, then search every missing monitored episode of this show"
            spinning={bulk.state.status === "running"}
            disabled={bulk.state.status === "running"}
            onClick={() =>
              void bulk.run(() =>
                api("/automation/search-item", {
                  method: "POST",
                  body: JSON.stringify({ media_id: show.id }),
                }),
              )
            }
          />
          <ToolbarButton
            icon={FolderTree}
            label="Organize"
            to={`/organization?media_id=${show.id}`}
          />
          <ToolbarButton icon={Settings2} label="Automation" to="/automation" />
        </ToolbarSection>
      </PageToolbar>
      {bulk.state.status !== "idle" && (
        <PageBody>
          <Alert
            kind={
              bulk.state.status === "error"
                ? "danger"
                : bulk.state.status === "running"
                  ? "info"
                  : "success"
            }
            role="status"
          >
            <p>{bulkSearchMessage(bulk.state)}</p>
            <div className="rs-row">
              <Link className="rs-link-button rs-small" to="/automation">
                Open Automation for details
              </Link>
              <button
                type="button"
                className="rs-link-button rs-small"
                onClick={bulk.reset}
              >
                Dismiss
              </button>
            </div>
          </Alert>
        </PageBody>
      )}
      {itemSearch.result && (
        <PageBody>
          <ItemSearchResultAlert
            result={itemSearch.result}
            onDismiss={itemSearch.reset}
          />
        </PageBody>
      )}
      <DetailHeader
        item={show}
        title={show.title}
        monitor={
          <MonitorToggle
            large
            label="Monitor this show"
            checked={show.monitored}
            disabled={pending}
            onChange={(value) => void update("/monitor", "PUT", value)}
          />
        }
        facts={[
          show.year,
          show.runtime_minutes ? `${show.runtime_minutes} min` : null,
          details.info?.genres.length ? details.info.genres.join(", ") : null,
        ]}
        labels={
          <>
            {show.series_status && (
              <Label
                kind={show.series_status === "Ended" ? "disabled" : "info"}
                large
              >
                {show.series_status}
              </Label>
            )}
            <Label kind={show.monitored ? "primary" : "disabled"}>
              {show.monitored ? "Monitored" : "Unmonitored"}
            </Label>
            {anime && <Label kind="purple">Anime</Label>}
            {profileLabel(show)}
            {details.info?.rating ? (
              <Label outline>TMDb {details.info.rating.toFixed(1)}</Label>
            ) : null}
          </>
        }
        details={
          <>
            <DetailFact title="Episodes">
              {loaded.length
                ? `${available} of ${known} on disk`
                : "Not loaded"}
              {loaded.length > 0 && loaded.length < show.seasons.length && (
                <span className="rs-muted">
                  {" "}
                  ({show.seasons.length - loaded.length} seasons not loaded)
                </span>
              )}
            </DetailFact>
            {data && (
              <>
                <DetailFact title="Path">
                  <code>{data.library_root}</code>
                </DetailFact>
                <DetailFact title="Size">
                  {data.total_size_bytes
                    ? formatBytes(data.total_size_bytes)
                    : "No files"}
                </DetailFact>
              </>
            )}
          </>
        }
        links={
          <ProviderLinks
            details={details}
            kind="series"
            tmdbID={show.tmdb_id}
          />
        }
      />
      <PageBody>
        {error && (
          <Alert kind="danger">
            <p>{error}</p>
            <div>
              <Button
                size="1"
                variant="soft"
                onClick={() => void reload()}
                disabled={pending}
              >
                Reload saved show
              </Button>
            </div>
          </Alert>
        )}
        <MetadataError details={details} />
        <DetailTabs
          label="Show sections"
          tabs={[
            {
              value: "episodes",
              label: "Episodes",
              content: (
                <>
                  {!show.monitored && (
                    <Alert kind="info">
                      Monitoring is paused for this show. Season and episode
                      choices are kept.
                    </Alert>
                  )}
                  {seasons.length === 0 ? (
                    <p className="rs-muted">
                      No seasons are listed yet. Refresh the show later.
                    </p>
                  ) : (
                    <>
                      <div className="det-season-tools">
                        <Button
                          size="1"
                          variant="soft"
                          color="gray"
                          disabled={pending || !show.monitored}
                          onClick={() =>
                            setSelectedSeasons(
                              seasons.slice(0, 100).map((season) => season.id),
                            )
                          }
                        >
                          {seasons.length > 100
                            ? "Select first 100 seasons"
                            : "Select all seasons"}
                        </Button>
                        <Button
                          size="1"
                          variant="soft"
                          color="gray"
                          disabled={pending || !selectedSeasons.length}
                          onClick={() => setSelectedSeasons([])}
                        >
                          Clear season selection
                        </Button>
                        <ConfirmDialog
                          trigger={
                            <Button
                              size="1"
                              variant="soft"
                              disabled={
                                pending ||
                                !show.monitored ||
                                !selectedSeasons.length
                              }
                            >
                              Monitor selected seasons ({selectedSeasons.length}
                              )
                            </Button>
                          }
                          title={`Monitor ${selectedSeasons.length} selected seasons?`}
                          onConfirm={() => batchMonitor(true)}
                        />
                        <ConfirmDialog
                          trigger={
                            <Button
                              size="1"
                              variant="soft"
                              disabled={
                                pending ||
                                !show.monitored ||
                                !selectedSeasons.length
                              }
                            >
                              Unmonitor selected seasons (
                              {selectedSeasons.length})
                            </Button>
                          }
                          title={`Stop monitoring ${selectedSeasons.length} selected seasons?`}
                          onConfirm={() => batchMonitor(false)}
                        />
                        <span className="rs-muted rs-small">
                          Up to 100 seasons. Episode choices are kept.
                        </span>
                      </div>
                      <div className="det-seasons">
                        {seasons.map((season) => (
                          <SeasonBlock
                            key={season.id}
                            show={show}
                            season={season}
                            pending={pending}
                            update={update}
                            attempt={attempt}
                            expanded={expanded.includes(season.number)}
                            onToggle={() =>
                              setExpanded((current) =>
                                current.includes(season.number)
                                  ? current.filter((n) => n !== season.number)
                                  : [...current, season.number],
                              )
                            }
                            selected={selectedSeasons.includes(season.id)}
                            selectDisabled={
                              pending ||
                              !show.monitored ||
                              (!selectedSeasons.includes(season.id) &&
                                selectedSeasons.length >= 100)
                            }
                            onSelect={(checked) =>
                              setSelectedSeasons((current) =>
                                checked
                                  ? [...current, season.id]
                                  : current.filter((id) => id !== season.id),
                              )
                            }
                            searchPending={!!itemSearch.pending}
                            onEpisodeSearch={(episodeID) =>
                              itemSearch.trigger(show.id, episodeID)
                            }
                            onSeasonSearch={(seasonNumber) =>
                              itemSearch.trigger(
                                show.id,
                                undefined,
                                seasonNumber,
                              )
                            }
                            onOpenReleaseSearch={setReleaseSearch}
                            onRemoveFile={removeFile}
                          />
                        ))}
                      </div>
                      <p className="rs-form-help">
                        Regular seasons start monitored; specials do not.
                        Ambiguous downloads wait for mapping review in Activity.
                      </p>
                    </>
                  )}
                </>
              ),
            },
            {
              value: "files",
              label: "Files & History",
              content: data ? (
                <>
                  <FilesSection details={details} savedTitle={show.title} />
                  <HistorySection details={details} />
                  <Pagination
                    page={details.page}
                    pages={Math.ceil(
                      Math.max(data.file_count, data.history_count) / 50,
                    )}
                    onPage={details.setPage}
                  />
                </>
              ) : (
                <RequestState
                  loading={details.loading}
                  error={details.loadError}
                  retry={details.reload}
                />
              ),
            },
            {
              value: "related",
              label: "Cast & Related",
              content: (
                <>
                  <CreditsSection details={details} refreshAction />
                  <AutomaticFranchise id={show.id} />
                  <RelatedTitles id={show.id} />
                </>
              ),
            },
            {
              value: "settings",
              label: "Settings",
              content: (
                <>
                  <QualityProfile
                    id={show.id}
                    assignment={details.data?.item.quality_profile}
                    context={details.data?.profile_context}
                    reload={details.reload}
                    onSaved={details.reload}
                  />
                  <SeriesReleaseRules id={show.id} />
                  <ShowSubtitlePreferences id={show.id} />
                  <SeriesYouTubeSource id={show.id} />
                  <AnimeSettings
                    id={show.id}
                    onSaved={() => {
                      void reload();
                      details.reload();
                    }}
                  />
                  {anime && <AnimeSearch id={show.id} />}
                  {anime && <AnimeOrder id={show.id} />}
                  <ItemOrganization id={show.id} />
                </>
              ),
            },
          ]}
        />
      </PageBody>
      {releaseSearch && (
        <ReleaseSearchModal
          open
          onOpenChange={(o) => {
            if (!o) setReleaseSearch(null);
          }}
          seriesID={show.id}
          seriesTitle={show.title}
          season={releaseSearch.season}
          seasonTitle={releaseSearch.seasonTitle}
          episode={releaseSearch.episode}
          episodeTitle={releaseSearch.episodeTitle}
          fileID={releaseSearch.fileID}
        />
      )}
    </>
  );
}

function seasonKind(show: TVSeries, season: TVSeason): Kind {
  if (!season.loaded_at || !season.known_episodes) return "default";
  if ((season.available_episodes ?? 0) >= season.known_episodes)
    return "success";
  if (!show.monitored || !season.monitored) return "disabled";
  return "danger";
}

function SeasonBlock({
  show,
  season,
  pending,
  update,
  attempt,
  expanded,
  onToggle,
  selected,
  selectDisabled,
  onSelect,
  searchPending,
  onEpisodeSearch,
  onSeasonSearch,
  onOpenReleaseSearch,
  onRemoveFile,
}: {
  show: TVSeries;
  season: TVSeason;
  pending: boolean;
  update: Update;
  attempt: number;
  expanded: boolean;
  onToggle: () => void;
  selected: boolean;
  selectDisabled: boolean;
  onSelect: (checked: boolean) => void;
  searchPending: boolean;
  onEpisodeSearch: (episodeID: number) => void;
  onSeasonSearch: (seasonNumber: number) => void;
  onOpenReleaseSearch: (scope: ReleaseSearchScope) => void;
  onRemoveFile?: (episodeID: number, code: string, filePath: string) => void;
}) {
  const panelID = `season-${season.id}`;
  return (
    <section className="det-season" aria-label={season.title}>
      <div className="det-season-header">
        <input
          type="checkbox"
          aria-label={`Select season ${season.title}`}
          checked={selected}
          disabled={selectDisabled}
          onChange={(event) => onSelect(event.target.checked)}
        />
        <MonitorToggle
          label={`Monitor ${season.title}`}
          checked={season.monitored}
          disabled={pending || !show.monitored}
          onChange={(value) =>
            void update(`/seasons/${season.number}/monitor`, "PUT", value)
          }
        />
        <h3 className="det-season-title">
          <button
            type="button"
            className="det-season-toggle"
            aria-expanded={expanded}
            aria-controls={panelID}
            onClick={onToggle}
          >
            {season.title}
            <ChevronDown size={18} aria-hidden="true" />
          </button>
        </h3>
        {season.loaded_at ? (
          <>
            <Label kind={seasonKind(show, season)}>
              {season.available_episodes ?? 0} / {season.known_episodes ?? 0} on
              disk
            </Label>
            <span className="rs-muted rs-small">
              {season.selected_episodes ?? 0} monitored
            </span>
          </>
        ) : (
          <span className="rs-muted rs-small">
            {season.episode_count} episodes · not loaded
          </span>
        )}
        <div className="det-season-actions">
          {season.loaded_at &&
            show.monitored &&
            season.monitored &&
            season.selected_episodes > 0 && (
              <>
                <button
                  type="button"
                  className="det-icon-link"
                  aria-label={`Automatic search for ${season.title}`}
                  title="Automatic search now: download mode queues the best match, search-only mode saves results for review"
                  disabled={searchPending}
                  onClick={() => onSeasonSearch(season.number)}
                >
                  <SearchCheck size={16} aria-hidden="true" />
                </button>
                <button
                  type="button"
                  className="det-icon-link"
                  aria-label={`Search releases for ${season.title}`}
                  title="Search releases for this season now"
                  onClick={() =>
                    onOpenReleaseSearch({
                      season: season.number,
                      seasonTitle: season.title,
                    })
                  }
                >
                  <Search size={16} aria-hidden="true" />
                </button>
              </>
            )}
        </div>
      </div>
      {expanded && (
        <div id={panelID} className="det-season-body">
          <SeasonEpisodes
            key={season.number}
            attempt={attempt}
            show={show}
            season={season}
            pending={pending}
            update={update}
            searchPending={searchPending}
            onEpisodeSearch={onEpisodeSearch}
            onOpenReleaseSearch={onOpenReleaseSearch}
            onRemoveFile={onRemoveFile}
          />
        </div>
      )}
    </section>
  );
}

function SeasonEpisodes({
  show,
  season,
  pending,
  update,
  attempt,
  searchPending,
  onEpisodeSearch,
  onOpenReleaseSearch,
  onRemoveFile,
}: {
  show: TVSeries;
  season: TVSeason;
  pending: boolean;
  update: Update;
  attempt: number;
  searchPending: boolean;
  onEpisodeSearch: (episodeID: number) => void;
  onOpenReleaseSearch: (scope: ReleaseSearchScope) => void;
  onRemoveFile?: (episodeID: number, code: string, filePath: string) => void;
}) {
  const today = new Date().toISOString().slice(0, 10);
  const [selectedEpisodes, setSelectedEpisodes] = useState<number[]>([]);
  // Keep the loaded episodes on screen while a change refreshes them, so the
  // page does not collapse and lose its scroll position.
  const resource = useLiveResource<TVSeasonDetails>(
    `/library/series/${show.id}/seasons/${season.number}`,
  );
  const { reload: reloadEpisodes } = resource;
  const seenAttempt = useRef(attempt);
  useEffect(() => {
    if (seenAttempt.current === attempt) return;
    seenAttempt.current = attempt;
    setSelectedEpisodes([]);
    reloadEpisodes();
  }, [attempt, reloadEpisodes]);
  const locked = pending || !show.monitored || !season.monitored;
  function batch(monitored: boolean) {
    void update(
      `/seasons/${season.number}/episodes/monitor`,
      "PUT",
      monitored,
      selectedEpisodes,
    );
  }
  if (!resource.data)
    return (
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
    );
  const episodes = resource.data.episodes;
  return (
    <>
      <div className="det-season-tools">
        <Button
          size="1"
          variant={season.loaded_at ? "soft" : "solid"}
          disabled={pending}
          onClick={() =>
            void update(`/seasons/${season.number}/refresh`, "POST")
          }
        >
          {season.loaded_at ? "Refresh episodes" : "Load episodes from TMDb"}
        </Button>
        {episodes.length > 0 && (
          <>
            <Button
              size="1"
              variant="soft"
              color="gray"
              disabled={locked}
              onClick={() =>
                setSelectedEpisodes(
                  episodes.slice(0, 100).map((episode) => episode.id),
                )
              }
            >
              {episodes.length > 100
                ? "Select first 100 episodes"
                : "Select all episodes"}
            </Button>
            <Button
              size="1"
              variant="soft"
              color="gray"
              disabled={pending || !selectedEpisodes.length}
              onClick={() => setSelectedEpisodes([])}
            >
              Clear selection
            </Button>
            <ConfirmDialog
              trigger={
                <Button
                  size="1"
                  variant="soft"
                  disabled={locked || !selectedEpisodes.length}
                >
                  Monitor selected ({selectedEpisodes.length})
                </Button>
              }
              title={`Monitor ${selectedEpisodes.length} selected episodes?`}
              onConfirm={() => batch(true)}
            />
            <ConfirmDialog
              trigger={
                <Button
                  size="1"
                  variant="soft"
                  disabled={locked || !selectedEpisodes.length}
                >
                  Unmonitor selected ({selectedEpisodes.length})
                </Button>
              }
              title={`Stop monitoring ${selectedEpisodes.length} selected episodes?`}
              onConfirm={() => batch(false)}
            />
          </>
        )}
      </div>
      {season.overview && <p className="rs-muted">{season.overview}</p>}
      {!season.loaded_at && (
        <p className="rs-form-help">
          Episodes are not loaded yet. Once loaded they stay available when TMDb
          is offline.
        </p>
      )}
      {season.loaded_at && episodes.length === 0 && (
        <p className="rs-muted">No episodes are listed yet.</p>
      )}
      {season.loaded_at && (!show.monitored || !season.monitored) && (
        <p className="rs-form-help">
          Monitoring is paused. Turn on the show and season to change episode
          choices.
        </p>
      )}
      {episodes.length > 0 && (
        <>
          <Table label={season.title + " episodes"}>
            <thead>
              <tr>
                <th scope="col" className="is-shrink">
                  <span className="sr-only">Select</span>
                </th>
                <th scope="col" className="is-shrink">
                  <span className="sr-only">Monitored</span>
                </th>
                <th scope="col" className="is-shrink">
                  #
                </th>
                <th scope="col">Title</th>
                <th scope="col">Air date</th>
                <th scope="col">Status</th>
                <th scope="col">File</th>
                <th scope="col" className="is-actions">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {episodes.map((e) => {
                const upcoming = !!e.air_date && e.air_date > today;
                return (
                  <tr key={e.id}>
                    <td className="is-shrink">
                      <input
                        type="checkbox"
                        aria-label={`Select episode ${e.number}: ${e.title}`}
                        checked={selectedEpisodes.includes(e.id)}
                        disabled={
                          locked ||
                          (!selectedEpisodes.includes(e.id) &&
                            selectedEpisodes.length >= 100)
                        }
                        onChange={(event) => {
                          const checked = event.target.checked;
                          setSelectedEpisodes((current) =>
                            checked
                              ? [...current, e.id]
                              : current.filter((id) => id !== e.id),
                          );
                        }}
                      />
                    </td>
                    <td className="is-shrink">
                      <MonitorToggle
                        label={`Monitor episode ${e.number}: ${e.title}`}
                        checked={e.monitored}
                        disabled={locked}
                        onChange={(value) =>
                          void update(
                            `/seasons/${season.number}/episodes/${e.number}/monitor`,
                            "PUT",
                            value,
                          )
                        }
                      />
                    </td>
                    <td
                      className="is-shrink"
                      title={episodeCode(season.number, e.number)}
                    >
                      {e.number}
                    </td>
                    <td className="det-episode-title">
                      <span>{e.title}</span>
                      {e.overview && (
                        <details>
                          <summary>Synopsis</summary>
                          <p>{e.overview}</p>
                        </details>
                      )}
                    </td>
                    <td>{e.air_date || "Unknown"}</td>
                    <td>
                      {e.available ? (
                        <Label kind="success">On disk</Label>
                      ) : upcoming ? (
                        <Label kind="default">Upcoming</Label>
                      ) : (
                        <Label
                          kind={
                            e.effective_monitored === false
                              ? "disabled"
                              : "danger"
                          }
                        >
                          Missing
                        </Label>
                      )}
                    </td>
                    <td>
                      {e.file_path ? (
                        <details className="det-episode-file">
                          <summary>{e.file_path.split("/").at(-1)}</summary>
                          <code>{e.file_path}</code>
                        </details>
                      ) : (
                        <span className="rs-muted">—</span>
                      )}
                    </td>
                    <td className="is-actions">
                      <span
                        className="rs-row"
                        style={{ justifyContent: "flex-end" }}
                      >
                        <button
                          type="button"
                          className="det-icon-link"
                          aria-label={`Automatic search for ${episodeCode(season.number, e.number)} ${e.title}`}
                          title="Automatic search now: download mode queues the best match or reports an available upgrade, search-only mode saves results for review"
                          disabled={searchPending}
                          onClick={() => onEpisodeSearch(e.id)}
                        >
                          <SearchCheck size={16} aria-hidden="true" />
                        </button>
                        <button
                          type="button"
                          className="det-icon-link"
                          aria-label={`Search releases for ${episodeCode(season.number, e.number)} ${e.title}`}
                          title={
                            e.available
                              ? "Search releases to replace this episode's file"
                              : "Search releases for this episode now"
                          }
                          onClick={() =>
                            onOpenReleaseSearch({
                              season: season.number,
                              seasonTitle: season.title,
                              episode: e.number,
                              episodeTitle: e.title,
                              ...(e.available && e.file_id
                                ? { fileID: e.file_id }
                                : {}),
                            })
                          }
                        >
                          <Search size={16} aria-hidden="true" />
                        </button>
                        {e.available && e.file_id && (
                          <ConfirmDialog
                            trigger={
                              <button
                                type="button"
                                className="det-icon-link"
                                aria-label={`Delete ${episodeCode(season.number, e.number)} ${e.title} from disk and the library`}
                                title="Delete this episode's file from disk and remove it from the library."
                                disabled={pending || onRemoveFile === undefined}
                              >
                                <FileX size={16} aria-hidden="true" />
                              </button>
                            }
                            title={`Delete ${episodeCode(season.number, e.number)}?`}
                            description={`${e.file_path ?? ""}\n\nThis permanently deletes the media file and cannot be undone. The download folder and seeding sources are never touched.`}
                            onConfirm={() => {
                              onRemoveFile &&
                                onRemoveFile(
                                  e.id,
                                  episodeCode(season.number, e.number),
                                  e.file_path ?? "",
                                );
                            }}
                          />
                        )}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
          <p className="rs-form-help">
            Status reflects registered files. Paths are relative to the media
            root.
          </p>
        </>
      )}
    </>
  );
}
