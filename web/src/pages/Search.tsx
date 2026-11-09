import { useEffect, useState, type FormEvent } from "react";
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
import { EmptyState, Label, PageBody, pageParam } from "../ui";
import { useResource } from "../useResource";
import "./Search.css";

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

function KindLink({
  to,
  current,
  children,
}: {
  to: string;
  current: boolean;
  children: string;
}) {
  return (
    <Link
      className={current ? "add-kind-link is-current" : "add-kind-link"}
      to={to}
      aria-current={current ? "page" : undefined}
    >
      {children}
    </Link>
  );
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
  const page = pageParam(params.get("page"));
  const [input, setInput] = useState(query);
  // The header search navigates here with a new query; keep the field in step.
  useEffect(() => setInput(query), [query]);
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
  const noun = tv ? "show" : "movie";
  const category = anime && tv ? "anime" : undefined;
  return (
    <PageBody className="add-page">
      <PageHeading
        title={
          anime
            ? `Search anime ${tv ? "shows" : "movies"}`
            : tv
              ? "Search TV shows"
              : "Search movies"
        }
      />
      <div className="add-kinds">
        <nav className="add-kind-group" aria-label="Search type">
          <KindLink to="/search" current={!anime && !tv}>
            Movies
          </KindLink>
          <KindLink to="/search?kind=series" current={!anime && tv}>
            TV Shows
          </KindLink>
          <KindLink to="/search?kind=anime" current={anime}>
            Anime
          </KindLink>
        </nav>
        {anime && (
          <nav className="add-kind-group" aria-label="Anime format">
            <KindLink to="/search?kind=anime&format=series" current={tv}>
              Shows
            </KindLink>
            <KindLink to="/search?kind=anime&format=movie" current={!tv}>
              Movies
            </KindLink>
          </nav>
        )}
      </div>
      <RequestState
        loading={provider.loading}
        error={provider.error}
        retry={provider.reload}
      />
      {provider.data && !provider.data.configured && (
        <EmptyState
          icon={Settings2}
          title="TMDb is not connected"
          actions={
            <Button asChild>
              <Link to="/settings">Connect TMDb</Link>
            </Button>
          }
        >
          Searching for new titles needs a TMDb API Read Access Token in
          Settings.
        </EmptyState>
      )}
      {provider.data?.configured && (
        <>
          <form onSubmit={submit} className="add-search" role="search">
            <TextField.Root
              className="add-search-field"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              aria-label={tv ? "TV show title" : "Movie title"}
              placeholder={
                anime
                  ? `Search for an anime ${noun}`
                  : tv
                    ? "Search for a show"
                    : "Search for a movie"
              }
              maxLength={200}
              required
              size="3"
            >
              <TextField.Slot>
                <SearchIcon size={18} aria-hidden="true" />
              </TextField.Slot>
            </TextField.Root>
            <Button type="submit" size="3" disabled={!input.trim()}>
              Search
            </Button>
          </form>
          {!query && (
            <p className="rs-muted add-hint">
              Search TMDb by title. Titles already in your library are marked.
            </p>
          )}
          <RequestState
            loading={results.loading && !!query}
            error={results.error}
            retry={results.reload}
          />
          {results.data && (
            <>
              <p className="rs-muted" role="status">
                {results.data.total_results.toLocaleString()}{" "}
                {results.data.total_results === 1 ? "result" : "results"} for “
                {query}”
              </p>
              {results.data.items.length === 0 ? (
                <EmptyState
                  icon={SearchIcon}
                  title={tv ? "No shows found" : "No movies found"}
                >
                  Try another title or spelling.
                </EmptyState>
              ) : (
                <div className="add-results">
                  {results.data.items.map((movie) => {
                    const inLibrary =
                      movie.in_library || added.has(movie.tmdb_id);
                    const open = () =>
                      setSelected({
                        id: movie.tmdb_id,
                        title: movie.title,
                        inLibrary: movie.in_library,
                      });
                    return (
                      <MovieCard
                        movie={movie}
                        key={movie.tmdb_id}
                        onOpen={open}
                        labels={
                          category ? <Label outline>Anime</Label> : undefined
                        }
                      >
                        <AddMovieButton
                          kind={kind}
                          category={category}
                          id={movie.tmdb_id}
                          added={inLibrary}
                          onAdded={onAdded}
                        />
                        <Button variant="ghost" size="1" onClick={open}>
                          View details
                        </Button>
                      </MovieCard>
                    );
                  })}
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
          category={category}
          id={selected.id}
          title={selected.title}
          added={selected.inLibrary || added.has(selected.id)}
          onAdded={onAdded}
          close={() => setSelected(undefined)}
        />
      )}
    </PageBody>
  );
}
