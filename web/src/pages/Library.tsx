import { Badge, Button } from "@radix-ui/themes";
import { MonitorPlay, Plus, Film } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { type LibraryPage } from "../api";
import { FuturePage, PageHeading, RequestState } from "../components";
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
        {movies && (
          <Button asChild>
            <Link to="/search">
              <Plus size={17} /> Add movies
            </Link>
          </Button>
        )}
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
                <MovieCard movie={item} key={item.id}>
                  <div className="library-status">
                    <Badge
                      color={item.status === "available" ? "green" : "gray"}
                    >
                      {item.status === "missing"
                        ? "Not downloaded"
                        : item.status}
                    </Badge>
                    <span>
                      {item.monitored ? "Monitored" : "Not monitored"}
                    </span>
                  </div>
                </MovieCard>
              ))}
            </div>
          ) : movies ? (
            <section className="panel empty-state">
              <span className="empty-icon">
                <Film size={32} strokeWidth={1.4} />
              </span>
              <h2>
                {page > 1
                  ? "No movies on this page."
                  : "Your next feature starts here."}
              </h2>
              <p>
                {page > 1
                  ? "Return to the first page to browse your library."
                  : "Search TMDb for a movie and add it to your collection. Metadata and artwork stay in your library across restarts."}
              </p>
              {page > 1 ? (
                <Button onClick={() => setParams({ page: "1" })}>
                  Go to first page
                </Button>
              ) : (
                <Button asChild>
                  <Link to="/search">Find a movie</Link>
                </Button>
              )}
            </section>
          ) : (
            <FuturePage
              icon={MonitorPlay}
              title="The next season starts here."
              description="Show monitoring and episode tracking follow the complete movie workflow."
              phase="Planned · Phase 3"
            />
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
