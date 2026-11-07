import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { AddMovieButton } from "../movieComponents";
import { Alert, FieldSet, Label, LoadingIndicator, Table } from "../ui";
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
    <FieldSet
      legend="Franchise"
      description="Suggestions from Wikidata. Nothing changes until you add a title."
    >
      {found.loading && <LoadingIndicator label="Checking Wikidata…" />}
      {found.error && (
        <Alert kind="danger">
          <p>{found.error}</p>
          <div>
            <Button size="1" variant="soft" onClick={found.reload}>
              Retry discovery
            </Button>
          </div>
        </Alert>
      )}
      {found.data &&
        (items.length ? (
          <Table label="Franchise titles">
            <thead>
              <tr>
                <th scope="col">Title</th>
                <th scope="col">Type</th>
                <th scope="col">Franchise</th>
                <th scope="col" className="is-actions">
                  Library
                </th>
              </tr>
            </thead>
            <tbody>
              {items.slice(0, more ? items.length : 12).map((item) => {
                const key = `${item.kind}:${item.tmdb_id}`;
                return (
                  <tr key={`${item.franchise_id}:${key}`}>
                    <td>
                      {item.library_id ? (
                        <Link
                          to={`/${item.kind === "movie" ? "movies" : "series"}/${item.library_id}`}
                        >
                          {item.title}
                        </Link>
                      ) : (
                        item.title
                      )}
                    </td>
                    <td>{item.kind === "movie" ? "Movie" : "TV show"}</td>
                    <td>
                      <a
                        href={`https://www.wikidata.org/wiki/${item.franchise_id}`}
                        target="_blank"
                        rel="noreferrer"
                      >
                        {item.franchise}
                      </a>
                    </td>
                    <td className="is-actions">
                      {item.library_id ? (
                        <Label kind="success">
                          In library{item.status ? ` · ${item.status}` : ""}
                        </Label>
                      ) : (
                        <AddMovieButton
                          id={item.tmdb_id}
                          kind={item.kind}
                          added={added.has(key)}
                          onAdded={() =>
                            setAdded((current) => new Set(current).add(key))
                          }
                        />
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        ) : (
          <p className="rs-muted">
            No shared franchise found. Wikidata may lack these links or the
            title identity may be ambiguous.
          </p>
        ))}
      {items.length > 12 && !more && (
        <div>
          <Button size="1" variant="soft" onClick={() => setMore(true)}>
            Show {items.length - 12} more
          </Button>
        </div>
      )}
      {found.data?.truncated && (
        <p className="rs-form-help">
          Limited to the first 200 provider records.
        </p>
      )}
    </FieldSet>
  );
}
