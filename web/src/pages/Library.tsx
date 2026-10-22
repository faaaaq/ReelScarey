import { Badge } from "@radix-ui/themes";
import { Film, MonitorPlay } from "lucide-react";
import { type MediaItem } from "../api";
import { FuturePage, PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";

export function Library({ kind }: { kind: "movie" | "series" }) {
  const resource = useResource<{ items: MediaItem[]; limit: number }>(
    `/library?kind=${kind}`,
  );
  const movies = kind === "movie";
  return (
    <>
      <PageHeading
        eyebrow="YOUR COLLECTION"
        title={movies ? "Movies" : "TV shows"}
        description={
          movies
            ? "A home for every feature in your collection."
            : "Every season, all in one place."
        }
      />
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {!resource.loading &&
        !resource.error &&
        resource.data &&
        (resource.data.items.length ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {resource.data.items.map((item) => (
              <article className="panel media-item" key={item.id}>
                <h2>{item.title}</h2>
                <p>{item.year ?? "Year unknown"}</p>
                <Badge color="gray">{item.status}</Badge>
              </article>
            ))}
          </div>
        ) : (
          <FuturePage
            icon={movies ? Film : MonitorPlay}
            title={
              movies
                ? "Your first feature is still to come."
                : "The next season starts here."
            }
            description={
              movies
                ? "Your library is empty. Adding movies from TMDb, finding releases, and importing downloads are part of the next milestone."
                : "Your library is empty. Show monitoring and episode tracking follow the complete movie workflow."
            }
            phase={movies ? "Planned · Phase 2" : "Planned · Phase 3"}
          />
        ))}
    </>
  );
}
