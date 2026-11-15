import type { FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import type { LibraryPage, TVSeries, TVSeasonDetails } from "../../api";
import { RequestState } from "../../components";
import type { Collection, Resource, Target } from "./types";

/** Destination chooser for a reviewed single-video download. */
export function TargetPicker(props: {
  kind: Target["kind"];
  setKind: (value: Target["kind"]) => void;
  busy: boolean;
  collections: Resource<{ items: Collection[] }>;
  collection: string;
  setCollection: (value: string) => void;
  createCollection: (e: FormEvent) => void;
  collectionTitle: string;
  setCollectionTitle: (value: string) => void;
  query: string;
  setQuery: (value: string) => void;
  searched: string;
  setSearched: (value: string) => void;
  page: number;
  setPage: (value: number) => void;
  library: Resource<LibraryPage>;
  media: string;
  setMedia: (value: string) => void;
  show: Resource<TVSeries>;
  season: string;
  setSeason: (value: string) => void;
  episodes: Resource<TVSeasonDetails>;
  episode: string;
  setEpisode: (value: string) => void;
}) {
  const {
    kind,
    setKind,
    busy,
    collections,
    collection,
    setCollection,
    createCollection,
    collectionTitle,
    setCollectionTitle,
    query,
    setQuery,
    searched,
    setSearched,
    page,
    setPage,
    library,
    media,
    setMedia,
    show,
    season,
    setSeason,
    episodes,
    episode,
    setEpisode,
  } = props;
  return (
    <div className="yt-target">
      <label>
        Destination
        <select
          value={kind}
          disabled={busy}
          onChange={(e) => setKind(e.target.value as Target["kind"])}
        >
          <option value="custom">Custom collection</option>
          <option value="movie">Movie</option>
          <option value="episode">TV episode</option>
        </select>
      </label>
      {kind === "custom" && (
        <>
          <RequestState
            loading={collections.loading}
            error={collections.error}
            retry={collections.reload}
          />
          <label>
            Collection
            <select
              value={collection}
              disabled={busy}
              onChange={(e) => setCollection(e.target.value)}
            >
              <option value="">Choose a collection</option>
              {collections.data?.items.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.title}
                </option>
              ))}
            </select>
          </label>
          <form className="yt-inline-form" onSubmit={createCollection}>
            <TextField.Root
              value={collectionTitle}
              onChange={(e) => setCollectionTitle(e.target.value)}
              placeholder="New collection"
              aria-label="New collection name"
              maxLength={200}
            />
            <Button
              type="submit"
              variant="soft"
              disabled={busy || !collectionTitle.trim()}
            >
              Create
            </Button>
          </form>
        </>
      )}
      {kind !== "custom" && (
        <>
          <form
            className="yt-inline-form"
            onSubmit={(e) => {
              e.preventDefault();
              setSearched(query.trim());
              setPage(1);
              setMedia("");
              setSeason("");
              setEpisode("");
            }}
          >
            <TextField.Root
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={`Search ${kind === "movie" ? "movies" : "TV shows"}`}
              aria-label="Search library"
            />
            <Button
              type="submit"
              variant="soft"
              disabled={busy || !query.trim()}
            >
              Search
            </Button>
          </form>
          {searched && (
            <>
              <RequestState
                loading={library.loading}
                error={library.error}
                retry={library.reload}
              />
              <label>
                {kind === "movie" ? "Movie" : "Show"}
                <select
                  value={media}
                  disabled={busy}
                  onChange={(e) => setMedia(e.target.value)}
                >
                  <option value="">Choose a title</option>
                  {library.data?.items.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.title}
                      {item.year ? ` (${item.year})` : ""}
                    </option>
                  ))}
                </select>
              </label>
              <div className="rs-form-actions">
                <Button
                  size="1"
                  variant="soft"
                  disabled={busy || page <= 1}
                  onClick={() => setPage(page - 1)}
                >
                  Previous
                </Button>
                <Button
                  size="1"
                  variant="soft"
                  disabled={
                    busy ||
                    !library.data ||
                    page * library.data.limit >= library.data.total
                  }
                  onClick={() => setPage(page + 1)}
                >
                  More
                </Button>
              </div>
            </>
          )}
          {kind === "episode" && media && (
            <>
              <RequestState
                loading={show.loading}
                error={show.error}
                retry={show.reload}
              />
              <label>
                Season
                <select
                  value={season}
                  disabled={busy}
                  onChange={(e) => setSeason(e.target.value)}
                >
                  <option value="">Choose a season</option>
                  {show.data?.seasons
                    .filter((item) => item.loaded_at)
                    .map((item) => (
                      <option key={item.id} value={item.number}>
                        Season {item.number}
                      </option>
                    ))}
                </select>
              </label>
            </>
          )}
          {kind === "episode" && season !== "" && (
            <>
              <RequestState
                loading={episodes.loading}
                error={episodes.error}
                retry={episodes.reload}
              />
              <label>
                Episode
                <select
                  value={episode}
                  disabled={busy}
                  onChange={(e) => setEpisode(e.target.value)}
                >
                  <option value="">Choose an episode</option>
                  {episodes.data?.episodes.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.number}. {item.title}
                    </option>
                  ))}
                </select>
              </label>
            </>
          )}
        </>
      )}
    </div>
  );
}
