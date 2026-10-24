import { Badge, Button } from "@radix-ui/themes";
import { Plus, Film } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { type LibraryPage } from "../api";
import { PageHeading, RequestState } from "../components";
import { MovieCard, Pagination } from "../movieComponents";
import { useResource } from "../useResource";

export function Library({ kind }: { kind: "movie" | "series" }) {
  const [params, setParams] = useSearchParams();
  const page = Number(params.get("page") ?? 1);
  const resource = useResource<LibraryPage>(
    `/library?kind=${kind}&page=${page}`,
  );
  const movies = kind === "movie";
  return (
    <>
      <PageHeading
        eyebrow="YOUR COLLECTION"
        title={movies ? "Movies" : "TV shows"}
        description={
          movies
            ? `${resource.data?.total ?? 0} ${resource.data?.total === 1 ? "movie" : "movies"} · One place for every feature.`
            : "Every season, all in one place."
        }
      >
        {
          <Button asChild>
            <Link to={movies ? "/search" : "/search?kind=series"}>
              <Plus size={17} /> {movies ? "Add movies" : "Add shows"}
            </Link>
          </Button>
        }
      </PageHeading>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {!resource.loading && !resource.error && resource.data && (
        <>
          {resource.data.items.length ? (
            <div className="movie-grid">
              {resource.data.items.map((item) => (
                <MovieCard
                  movie={item}
                  key={item.id}
                  href={`/${movies ? "movies" : "series"}/${item.id}`}
                >
                  <div className="library-status">
                    <Badge
                      color={item.status === "available" ? "green" : "gray"}
                    >
                      {!movies
                        ? item.status === "downloading"
                          ? "Downloading"
                          : item.status === "available"
                            ? "Episode files available"
                            : "Catalogued"
                        : item.status === "missing"
                          ? "Not downloaded"
                          : item.status === "available"
                            ? "Available"
                            : "Downloading"}
                    </Badge>
                    <span>
                      {item.monitored ? "Monitored" : "Not monitored"}
                    </span>
                  </div>
                  {!movies && (
                    <div className="movie-card-actions">
                      <Button asChild variant="soft">
                        <Link to={`/series/${item.id}`}>
                          Seasons & episodes
                        </Link>
                      </Button>
                    </div>
                  )}
                  {movies && item.status !== "available" && (
                    <div className="movie-card-actions">
                      <Button asChild variant="soft">
                        <Link
                          to={
                            item.status === "downloading"
                              ? "/activity"
                              : `/releases?movie=${item.id}`
                          }
                        >
                          {item.status === "downloading"
                            ? "View progress"
                            : "Find releases"}
                        </Link>
                      </Button>
                    </div>
                  )}
                </MovieCard>
              ))}
            </div>
          ) : (
            <section className="panel empty-state">
              <span className="empty-icon">
                <Film size={32} strokeWidth={1.4} />
              </span>
              <h2>
                {page > 1
                  ? `No ${movies ? "movies" : "shows"} on this page.`
                  : movies
                    ? "Your next feature starts here."
                    : "Your next series starts here."}
              </h2>
              <p>
                {page > 1
                  ? "Return to the first page to browse your library."
                  : `Search TMDb for a ${movies ? "movie" : "show"} and add it to your collection. Metadata and artwork stay in your library across restarts.`}
              </p>
              {page > 1 ? (
                <Button onClick={() => setParams({ page: "1" })}>
                  Go to first page
                </Button>
              ) : (
                <Button asChild>
                  <Link to={movies ? "/search" : "/search?kind=series"}>
                    {movies ? "Find a movie" : "Find a show"}
                  </Link>
                </Button>
              )}
            </section>
          )}
          <Pagination
            page={resource.data.page}
            pages={Math.ceil(resource.data.total / resource.data.limit)}
            onPage={(page) => setParams({ page: String(page) })}
          />
        </>
      )}
    </>
  );
}
