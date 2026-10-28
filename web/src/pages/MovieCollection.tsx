import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { AddMovieButton } from "../movieComponents";

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
    <section className="panel system-panel">
      <h2>Related movies</h2>
      <p>
        Browse the collection reported by TMDb and see which movies are in your
        library.
      </p>
      <Button variant="soft" onClick={() => void load()} disabled={busy}>
        {busy ? "Loading…" : "Load related movies"}
      </Button>
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
      {collection &&
        (collection.name ? (
          <>
            <h3>{collection.name}</h3>
            <div className="file-details-list">
              {collection.items.map((item) => (
                <article className="discovery-file" key={item.tmdb_id}>
                  <h3>
                    {item.id ? (
                      <Link to={`/movies/${item.id}`}>{item.title}</Link>
                    ) : (
                      item.title
                    )}{" "}
                    {item.year && `(${item.year})`}
                  </h3>
                  {item.id ? (
                    <p>In library · {item.status}</p>
                  ) : (
                    <AddMovieButton
                      id={item.tmdb_id}
                      added={added.has(item.tmdb_id)}
                      onAdded={(value) =>
                        setAdded((current) => new Set(current).add(value))
                      }
                    />
                  )}
                </article>
              ))}
            </div>
          </>
        ) : (
          <p>TMDb does not list this movie in a collection.</p>
        ))}
    </section>
  );
}
