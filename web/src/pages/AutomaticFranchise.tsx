import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { AddMovieButton } from "../movieComponents";
import { useResource } from "../useResource";

type Member = {
  kind: "movie" | "series";
  tmdb_id: number;
  title: string;
  wikidata_id: string;
  franchise_id: string;
  franchise: string;
  library_id: number;
  status: string;
};
type Results = { source: string; items: Member[]; truncated: boolean };
export function AutomaticFranchise({ id }: { id: number }) {
  const found = useResource<Results>(`/library/items/${id}/franchise`);
  const [more, setMore] = useState(false);
  const [added, setAdded] = useState<Set<string>>(new Set());
  const items = Array.isArray(found.data?.items) ? found.data.items : [];
  return (
    <section className="panel system-panel">
      <h2>Franchise connections</h2>
      <p>
        Movies and shows linked to the same media franchise in Wikidata.
        Coverage depends on Wikidata; associations are suggestions and do not
        change your library.
      </p>
      {found.loading && <p>Checking Wikidata…</p>}
      {found.error && (
        <div role="alert">
          <p>{found.error}</p>
          <Button variant="soft" onClick={found.reload}>
            Retry discovery
          </Button>
        </div>
      )}
      {found.data &&
        (items.length ? (
          <div className="file-details-list">
            {items.slice(0, more ? items.length : 12).map((item) => {
              const key = `${item.kind}:${item.tmdb_id}`;
              return (
                <article
                  className="discovery-file"
                  key={`${item.franchise_id}:${key}`}
                >
                  <h3>
                    {item.library_id ? (
                      <Link
                        to={`/${item.kind === "movie" ? "movies" : "series"}/${item.library_id}`}
                      >
                        {item.title}
                      </Link>
                    ) : (
                      item.title
                    )}
                  </h3>
                  <p>
                    {item.kind === "movie" ? "Movie" : "TV show"} ·{" "}
                    <a
                      href={`https://www.wikidata.org/wiki/${item.franchise_id}`}
                      target="_blank"
                      rel="noreferrer"
                    >
                      {item.franchise}
                    </a>
                    {item.library_id ? ` · In library (${item.status})` : ""}
                  </p>
                  {!item.library_id && (
                    <AddMovieButton
                      id={item.tmdb_id}
                      kind={item.kind}
                      added={added.has(key)}
                      onAdded={() =>
                        setAdded((current) => new Set(current).add(key))
                      }
                    />
                  )}
                </article>
              );
            })}
          </div>
        ) : (
          <p>
            No shared franchise was found. Wikidata may not yet contain these
            links or may have an ambiguous title identity.
          </p>
        ))}
      {items.length > 12 && !more && (
        <Button variant="soft" onClick={() => setMore(true)}>
          Show {items.length - 12} more
        </Button>
      )}
      {found.data?.truncated && (
        <p>Results are limited to the first 200 provider records.</p>
      )}
    </section>
  );
}
