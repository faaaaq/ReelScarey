import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Search as SearchIcon, Settings2 } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import type { ProviderStatus, SearchResults } from "../api";
import { PageHeading, RequestState } from "../components";
import {
  AddMovieButton,
  MovieCard,
  MovieDetails,
  Pagination,
} from "../movieComponents";
import { useResource } from "../useResource";

export function Search() {
  const [params] = useSearchParams();
  const anime = params.get("kind") === "anime";
  const kind = anime
    ? params.get("format") === "movie"
      ? "movie"
      : "series"
    : params.get("kind") === "series"
      ? "series"
      : "movie";
  return <MediaSearch key={`${anime}-${kind}`} kind={kind} anime={anime} />;
}
function MediaSearch({
  kind,
  anime,
}: {
  kind: "movie" | "series";
  anime: boolean;
}) {
  const tv = kind === "series";
  const [params, setParams] = useSearchParams();
  const query = params.get("query") ?? "";
  const page = Number(params.get("page") ?? 1);
  const [input, setInput] = useState(query);
  const [added, setAdded] = useState<Set<number>>(new Set());
  const [selected, setSelected] = useState<{
    id: number;
    title: string;
    inLibrary: boolean;
  }>();
  const searchType: Record<string, string> = anime
    ? { kind: "anime", format: kind }
    : tv
      ? { kind: "series" }
      : {};
  const provider = useResource<ProviderStatus>("/providers/tmdb");
  const results = useResource<SearchResults>(
    query && provider.data?.configured
      ? `/metadata/${tv ? "series" : "movies"}?query=${encodeURIComponent(query)}&page=${page}`
      : null,
  );
  function submit(event: FormEvent) {
    event.preventDefault();
    const trimmed = input.trim();
    if (!trimmed) return;
    if (query === trimmed && page === 1) results.reload();
    else
      setParams({
        ...searchType,
        query: trimmed,
        page: "1",
      });
  }
  function onAdded(id: number) {
    setAdded((current) => new Set(current).add(id));
  }
  return (
    <>
      <PageHeading
        title={
          anime
            ? `Search anime ${tv ? "shows" : "movies"}`
            : tv
              ? "Search TV shows"
              : "Search movies"
        }
      />
      <nav className="search-kind" aria-label="Search type">
        <Button asChild variant={!anime && !tv ? "solid" : "soft"}>
          <Link to="/search" aria-current={!anime && !tv ? "page" : undefined}>
            Movies
          </Link>
        </Button>
        <Button asChild variant={!anime && tv ? "solid" : "soft"}>
          <Link
            to="/search?kind=series"
            aria-current={!anime && tv ? "page" : undefined}
          >
            TV shows
          </Link>
        </Button>
        <Button asChild variant={anime ? "solid" : "soft"}>
          <Link
            to="/search?kind=anime"
            aria-current={anime ? "page" : undefined}
          >
            Anime
          </Link>
        </Button>
      </nav>
      {anime && (
        <nav className="search-kind" aria-label="Anime format">
          <Button asChild variant={tv ? "solid" : "soft"}>
            <Link
              to="/search?kind=anime&format=series"
              aria-current={tv ? "page" : undefined}
            >
              Shows
            </Link>
          </Button>
          <Button asChild variant={tv ? "soft" : "solid"}>
            <Link
              to="/search?kind=anime&format=movie"
              aria-current={!tv ? "page" : undefined}
            >
              Movies
            </Link>
          </Button>
        </nav>
      )}
      <RequestState
        loading={provider.loading}
        error={provider.error}
        retry={provider.reload}
      />
      {provider.data && !provider.data.configured && (
        <section className="panel connect-prompt">
          <Settings2 size={28} />
          <h2>Connect your {tv ? "TV" : "movie"} metadata.</h2>
          <p>
            Add your TMDb API Read Access Token in Settings to search real{" "}
            {tv ? "TV" : "movie"}
            titles and artwork.
          </p>
          <Button asChild>
            <Link to="/settings">Connect TMDb</Link>
          </Button>
        </section>
      )}
      {provider.data?.configured && (
        <>
          <form onSubmit={submit} className="movie-search-form">
            <TextField.Root
              value={input}
              onChange={(e) => setInput(e.target.value)}
              aria-label={tv ? "TV show title" : "Movie title"}
              placeholder={
                tv ? "Search by show title…" : "Search by movie title…"
              }
              maxLength={200}
              required
              size="3"
            >
              <TextField.Slot>
                <SearchIcon size={18} />
              </TextField.Slot>
            </TextField.Root>
            <Button type="submit" size="3" disabled={!input.trim()}>
              Search
            </Button>
          </form>
          {!query && (
            <section className="panel search-intro">
              <SearchIcon size={30} />
              <h2>Search by title</h2>
              <p>
                Search a title to see real {tv ? "show" : "movie"} details,
                artwork, and release dates.
              </p>
            </section>
          )}
          <RequestState
            loading={results.loading && !!query}
            error={results.error}
            retry={results.reload}
          />
          {results.data && (
            <>
              <p className="search-result-count">
                {results.data.total_results.toLocaleString()} results for “
                {query}”
              </p>
              {results.data.items.length === 0 ? (
                <section className="panel search-intro">
                  <h2>{tv ? "No shows found." : "No movies found."}</h2>
                  <p>Try another title or check the spelling.</p>
                </section>
              ) : (
                <div className="movie-grid">
                  {results.data.items.map((movie) => (
                    <MovieCard movie={movie} key={movie.tmdb_id}>
                      <div className="movie-card-actions">
                        <Button
                          variant="soft"
                          color="gray"
                          onClick={() =>
                            setSelected({
                              id: movie.tmdb_id,
                              title: movie.title,
                              inLibrary: movie.in_library,
                            })
                          }
                        >
                          View details
                        </Button>
                        <AddMovieButton
                          kind={kind}
                          category={anime && tv ? "anime" : undefined}
                          id={movie.tmdb_id}
                          added={movie.in_library || added.has(movie.tmdb_id)}
                          onAdded={onAdded}
                        />
                      </div>
                    </MovieCard>
                  ))}
                </div>
              )}
              <Pagination
                page={results.data.page}
                pages={results.data.total_pages}
                onPage={(page) =>
                  setParams({
                    ...searchType,
                    query,
                    page: String(page),
                  })
                }
              />
            </>
          )}
        </>
      )}
      {selected && (
        <MovieDetails
          kind={kind}
          category={anime && tv ? "anime" : undefined}
          id={selected.id}
          title={selected.title}
          added={selected.inLibrary || added.has(selected.id)}
          onAdded={onAdded}
          close={() => setSelected(undefined)}
        />
      )}
    </>
  );
}
