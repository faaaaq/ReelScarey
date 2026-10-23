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
  const provider = useResource<ProviderStatus>("/providers/tmdb");
  const results = useResource<SearchResults>(
    query && provider.data?.configured
      ? `/metadata/movies?query=${encodeURIComponent(query)}&page=${page}`
      : null,
  );
  function submit(event: FormEvent) {
    event.preventDefault();
    const trimmed = input.trim();
    if (!trimmed) return;
    if (query === trimmed && page === 1) results.reload();
    else setParams({ query: trimmed, page: "1" });
  }
  function onAdded(id: number) {
    setAdded((current) => new Set(current).add(id));
  }
  return (
    <>
      <PageHeading
        eyebrow="FIND YOUR NEXT FEATURE"
        title="Search movies"
        description="Explore TMDb and make room for your next movie night."
      />
      <RequestState
        loading={provider.loading}
        error={provider.error}
        retry={provider.reload}
      />
      {provider.data && !provider.data.configured && (
        <section className="panel connect-prompt">
          <Settings2 size={28} />
          <h2>Connect your movie metadata.</h2>
          <p>
            Add your TMDb API Read Access Token in Settings to search real movie
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
              aria-label="Movie title"
              placeholder="Search by movie title…"
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
              <h2>What’s on your watchlist?</h2>
              <p>
                Search a title to see real movie details, artwork, and release
                dates.
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
                  <h2>No movies found.</h2>
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
                onPage={(page) => setParams({ query, page: String(page) })}
              />
            </>
          )}
          <p className="search-disclaimer">
            Adding a movie saves its metadata and monitoring preference.
            Downloading and importing are still upcoming.
          </p>
        </>
      )}
      {selected && (
        <MovieDetails
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
