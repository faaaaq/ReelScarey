import { useState } from "react";
import { Badge, Button, Switch } from "@radix-ui/themes";
import { Link, useParams } from "react-router-dom";
import {
  api,
  messageFor,
  type TVSeries,
  type TVSeason,
  type TVSeasonDetails,
} from "../api";
import { PageHeading, RequestState } from "../components";
import { Poster } from "../movieComponents";
import { useResource } from "../useResource";
import { ItemInformation } from "./ItemDetails";
import { SeriesReleaseRules } from "./SeriesReleaseRules";
import { ShowSubtitlePreferences } from "./ShowSubtitlePreferences";

export function Series() {
  const { id } = useParams();
  const valid =
    !!id && /^[1-9][0-9]*$/.test(id) && Number.isSafeInteger(Number(id));
  const resource = useResource<TVSeries>(
    valid ? `/library/series/${id}` : null,
  );
  return (
    <>
      {!valid ? (
        <p role="alert">Choose a show from your library.</p>
      ) : (
        <RequestState
          loading={resource.loading}
          error={resource.error}
          retry={resource.reload}
        />
      )}
      {resource.data && <SeriesView key={id} initial={resource.data} />}
    </>
  );
}
function SeriesView({ initial }: { initial: TVSeries }) {
  const [show, setShow] = useState(initial);
  const [selected, setSelected] = useState<number>();
  const [selectedSeasons, setSelectedSeasons] = useState<number[]>([]);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const base = `/library/series/${show.id}`;
  async function update(
    path: string,
    method: "PUT" | "POST",
    monitored?: boolean,
    episodeIDs?: number[],
    seasonIDs?: number[],
  ) {
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
  return (
    <>
      <PageHeading
        title={show.title}
        description={[show.year, show.series_status]
          .filter(Boolean)
          .join(" · ")}
      >
        <Button
          variant="soft"
          disabled={pending}
          onClick={() => void update("/refresh", "POST")}
        >
          {pending ? "Saving…" : "Refresh show details"}
        </Button>
      </PageHeading>
      <Link
        className="text-link"
        to={show.category === "anime" ? "/anime" : "/series"}
      >
        {show.category === "anime" ? "Back to Anime" : "Back to TV shows"}
      </Link>
      <section className="panel series-summary">
        <Poster movie={show} />
        <div>
          <p>{show.overview || "No synopsis is available yet."}</p>
          <label className="monitor-choice">
            <Switch
              checked={show.monitored}
              disabled={pending}
              onCheckedChange={(v) => void update("/monitor", "PUT", v)}
            />{" "}
            Monitor this show
          </label>
          <p className="field-help">
            Pausing a show or season keeps your episode choices. Regular seasons
            start selected; specials start unselected.
          </p>
          <p className="field-help">
            Find releases for an episode or selected missing episodes in a
            season. Completed TV files wait for your mapping review in Activity.
            Scheduled searches follow your automation settings.
          </p>
          <a
            className="text-link"
            href={`https://www.themoviedb.org/tv/${show.tmdb_id}`}
            target="_blank"
            rel="noreferrer"
          >
            View on TMDb
          </a>
        </div>
      </section>
      <SeriesReleaseRules id={show.id} />
      <ShowSubtitlePreferences id={show.id} />
      <ItemInformation
        key={`details-${show.id}-${show.revision}`}
        id={show.id}
      />
      {error && (
        <div role="alert" className="movie-error">
          <p>{error}</p>
          <Button
            variant="soft"
            onClick={() => void reload()}
            disabled={pending}
          >
            Reload saved show
          </Button>
        </div>
      )}
      <h2 className="series-heading">Seasons</h2>
      {!!show.seasons.length && (
        <div className="system-panel">
          <p className="field-help">
            Select up to 100 seasons to change their monitoring together.
            Episode choices stay saved.
          </p>
          <Button
            variant="soft"
            disabled={pending || !show.monitored}
            onClick={() =>
              setSelectedSeasons(
                show.seasons.slice(0, 100).map((season) => season.id),
              )
            }
          >
            {show.seasons.length > 100
              ? "Select first 100 seasons"
              : "Select all seasons"}
          </Button>{" "}
          <Button
            variant="soft"
            disabled={pending || !selectedSeasons.length}
            onClick={() => setSelectedSeasons([])}
          >
            Clear season selection
          </Button>{" "}
          <Button
            variant="soft"
            disabled={pending || !show.monitored || !selectedSeasons.length}
            onClick={() => {
              if (
                window.confirm(
                  `Monitor ${selectedSeasons.length} selected seasons?`,
                )
              )
                void update(
                  "/seasons/monitor",
                  "PUT",
                  true,
                  undefined,
                  selectedSeasons,
                );
            }}
          >
            Monitor selected seasons ({selectedSeasons.length})
          </Button>{" "}
          <Button
            variant="soft"
            disabled={pending || !show.monitored || !selectedSeasons.length}
            onClick={() => {
              if (
                window.confirm(
                  `Stop monitoring ${selectedSeasons.length} selected seasons?`,
                )
              )
                void update(
                  "/seasons/monitor",
                  "PUT",
                  false,
                  undefined,
                  selectedSeasons,
                );
            }}
          >
            Unmonitor selected seasons ({selectedSeasons.length})
          </Button>
        </div>
      )}
      {!show.monitored && (
        <p className="field-help">
          Monitoring is paused for this show. Your season and episode selections
          are saved.
        </p>
      )}
      {show.seasons.length === 0 ? (
        <p>No seasons are listed yet. Refresh the show details later.</p>
      ) : (
        <div className="season-list">
          {show.seasons.map((season) => (
            <section className="panel season-card" key={season.id}>
              <div className="season-header">
                <div>
                  <label>
                    <input
                      type="checkbox"
                      aria-label={`Select season ${season.title}`}
                      checked={selectedSeasons.includes(season.id)}
                      disabled={
                        pending ||
                        !show.monitored ||
                        (!selectedSeasons.includes(season.id) &&
                          selectedSeasons.length >= 100)
                      }
                      onChange={(event) => {
                        const checked = event.target.checked;
                        setSelectedSeasons((current) =>
                          checked
                            ? [...current, season.id]
                            : current.filter((id) => id !== season.id),
                        );
                      }}
                    />{" "}
                    Select for batch
                  </label>
                  <h3>{season.title}</h3>
                  <p>
                    {season.episode_count} episodes
                    {season.air_date ? ` · ${season.air_date}` : ""}
                    {season.loaded_at
                      ? ` · ${season.selected_episodes} of ${season.known_episodes} loaded episodes selected`
                      : ""}
                    {season.available_episodes
                      ? ` · ${season.available_episodes} available`
                      : ""}
                  </p>
                </div>
                <label className="monitor-choice">
                  <Switch
                    aria-label={`Monitor ${season.title}`}
                    checked={season.monitored}
                    disabled={pending || !show.monitored}
                    onCheckedChange={(v) =>
                      void update(`/seasons/${season.number}/monitor`, "PUT", v)
                    }
                  />{" "}
                  Monitor
                </label>
                <Button
                  variant="soft"
                  aria-expanded={selected === season.number}
                  aria-controls={`season-${season.id}`}
                  onClick={() =>
                    setSelected(
                      selected === season.number ? undefined : season.number,
                    )
                  }
                >
                  {selected === season.number
                    ? "Hide episodes"
                    : "View episodes"}
                </Button>
                {season.loaded_at &&
                  show.monitored &&
                  season.monitored &&
                  season.selected_episodes > 0 && (
                    <Button asChild variant="soft">
                      <Link
                        to={`/releases?series=${show.id}&season=${season.number}`}
                      >
                        Find season releases
                      </Link>
                    </Button>
                  )}
              </div>
              {selected === season.number && (
                <div id={`season-${season.id}`}>
                  <SeasonEpisodes
                    key={`${season.number}-${attempt}`}
                    show={show}
                    season={season}
                    pending={pending}
                    update={update}
                  />
                </div>
              )}
            </section>
          ))}
        </div>
      )}
    </>
  );
}
function SeasonEpisodes({
  show,
  season,
  pending,
  update,
}: {
  show: TVSeries;
  season: TVSeason;
  pending: boolean;
  update: (
    path: string,
    method: "PUT" | "POST",
    monitored?: boolean,
    episodeIDs?: number[],
  ) => Promise<void>;
}) {
  const [selectedEpisodes, setSelectedEpisodes] = useState<number[]>([]);
  const resource = useResource<TVSeasonDetails>(
    `/library/series/${show.id}/seasons/${season.number}`,
  );
  return (
    <div className="season-episodes">
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {resource.data && (
        <>
          <p>{season.overview || "No season synopsis is available yet."}</p>
          <Button
            variant="soft"
            disabled={pending}
            onClick={() =>
              void update(`/seasons/${season.number}/refresh`, "POST")
            }
          >
            {season.loaded_at ? "Refresh episodes" : "Load episodes from TMDb"}
          </Button>
          {season.loaded_at && resource.data.episodes.length === 0 && (
            <p>No episodes are listed yet. Refresh later for updates.</p>
          )}
          {!season.loaded_at && (
            <p className="field-help">
              Load this season once to save its episode list. Saved episodes
              stay available when TMDb is offline.
            </p>
          )}
          {season.loaded_at && (!show.monitored || !season.monitored) && (
            <p className="field-help">
              Monitoring is paused. Turn on the show and season to change
              episode choices.
            </p>
          )}
          {resource.data.episodes.length > 0 && (
            <div>
              <p className="field-help">
                Select up to 100 episodes to change their monitoring together.
                Existing files stay in place.
              </p>
              <Button
                variant="soft"
                disabled={pending || !show.monitored || !season.monitored}
                onClick={() =>
                  setSelectedEpisodes(
                    resource
                      .data!.episodes.slice(0, 100)
                      .map((episode) => episode.id),
                  )
                }
              >
                {resource.data.episodes.length > 100
                  ? "Select first 100 episodes"
                  : "Select all episodes"}
              </Button>{" "}
              <Button
                variant="soft"
                disabled={pending || !selectedEpisodes.length}
                onClick={() => setSelectedEpisodes([])}
              >
                Clear selection
              </Button>{" "}
              <Button
                variant="soft"
                disabled={
                  pending ||
                  !show.monitored ||
                  !season.monitored ||
                  !selectedEpisodes.length
                }
                onClick={() => {
                  if (
                    window.confirm(
                      `Monitor ${selectedEpisodes.length} selected episodes?`,
                    )
                  )
                    void update(
                      `/seasons/${season.number}/episodes/monitor`,
                      "PUT",
                      true,
                      selectedEpisodes,
                    );
                }}
              >
                Monitor selected ({selectedEpisodes.length})
              </Button>{" "}
              <Button
                variant="soft"
                disabled={
                  pending ||
                  !show.monitored ||
                  !season.monitored ||
                  !selectedEpisodes.length
                }
                onClick={() => {
                  if (
                    window.confirm(
                      `Stop monitoring ${selectedEpisodes.length} selected episodes?`,
                    )
                  )
                    void update(
                      `/seasons/${season.number}/episodes/monitor`,
                      "PUT",
                      false,
                      selectedEpisodes,
                    );
                }}
              >
                Unmonitor selected ({selectedEpisodes.length})
              </Button>
              <ul className="episode-list">
                {resource.data.episodes.map((e) => (
                  <li key={e.id}>
                    <div>
                      <label>
                        <input
                          type="checkbox"
                          aria-label={`Select episode ${e.number}: ${e.title}`}
                          checked={selectedEpisodes.includes(e.id)}
                          disabled={
                            pending ||
                            !show.monitored ||
                            !season.monitored ||
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
                        />{" "}
                        Select for batch
                      </label>
                      <h4>{`S${String(season.number).padStart(2, "0")}E${String(e.number).padStart(2, "0")} · ${e.title}`}</h4>
                      <p className="field-help">
                        {e.air_date || "Air date unknown"}
                        {e.runtime_minutes ? ` · ${e.runtime_minutes} min` : ""}
                      </p>
                      {e.overview && <p>{e.overview}</p>}
                      <Badge color={e.available ? "green" : "gray"}>
                        {e.available
                          ? "Available"
                          : e.air_date &&
                              e.air_date >
                                new Date().toLocaleDateString("en-CA")
                            ? "Upcoming"
                            : "Not imported"}
                      </Badge>
                      {e.file_path && (
                        <p className="field-help episode-path">{e.file_path}</p>
                      )}
                      {!e.available && (
                        <p>
                          <Link
                            className="text-link"
                            to={`/releases?series=${show.id}&season=${season.number}&episode=${e.number}`}
                          >
                            Find episode releases
                          </Link>
                        </p>
                      )}
                    </div>
                    <label className="monitor-choice">
                      <Switch
                        aria-label={`Monitor episode ${e.number}: ${e.title}`}
                        checked={e.monitored}
                        disabled={
                          pending || !show.monitored || !season.monitored
                        }
                        onCheckedChange={(v) =>
                          void update(
                            `/seasons/${season.number}/episodes/${e.number}/monitor`,
                            "PUT",
                            v,
                          )
                        }
                      />{" "}
                      Monitor
                    </label>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </div>
  );
}
