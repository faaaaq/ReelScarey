import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { AddMovieButton } from "../movieComponents";
import { Alert, FieldSet, Label, Table } from "../ui";

type Member = {
  id: number;
  tmdb_id: number;
  title: string;
  year: number | null;
  status: string;
};
type Collection = { name: string; items: Member[] };
export function MovieCollection({ id }: { id: number }) {
  const [collection, setCollection] = useState<Collection>();
  const [added, setAdded] = useState<Set<number>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function load() {
    setBusy(true);
    setError("");
    try {
      setCollection(await api<Collection>(`/library/items/${id}/collection`));
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <FieldSet
      legend="Collection"
      actions={
        <Button
          size="1"
          variant="soft"
          onClick={() => void load()}
          disabled={busy}
        >
          {busy ? "Loading…" : collection ? "Reload" : "Load related movies"}
        </Button>
      }
    >
      {!collection && !error && (
        <p className="rs-muted">
          Load the TMDb collection to see which movies are in your library.
        </p>
      )}
      {error && <Alert kind="danger">{error}</Alert>}
      {collection &&
        (collection.name ? (
          <>
            <h3>{collection.name}</h3>
            <Table label={`${collection.name} movies`}>
              <thead>
                <tr>
                  <th scope="col">Title</th>
                  <th scope="col" className="is-shrink">
                    Year
                  </th>
                  <th scope="col" className="is-actions">
                    Library
                  </th>
                </tr>
              </thead>
              <tbody>
                {collection.items.map((item) => (
                  <tr key={item.tmdb_id}>
                    <td>
                      {item.id ? (
                        <Link to={`/movies/${item.id}`}>{item.title}</Link>
                      ) : (
                        item.title
                      )}
                    </td>
                    <td className="is-shrink">{item.year ?? "—"}</td>
                    <td className="is-actions">
                      {item.id ? (
                        <Label kind="success">
                          In library{item.status ? ` · ${item.status}` : ""}
                        </Label>
                      ) : (
                        <AddMovieButton
                          id={item.tmdb_id}
                          added={added.has(item.tmdb_id)}
                          onAdded={(value) =>
                            setAdded((current) => new Set(current).add(value))
                          }
                        />
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </>
        ) : (
          <p className="rs-muted">
            TMDb does not list this movie in a collection.
          </p>
        ))}
    </FieldSet>
  );
}
