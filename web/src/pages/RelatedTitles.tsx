import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor, type MediaItem } from "../api";

export function RelatedTitles({ id }: { id: number }) {
  const [items, setItems] = useState<MediaItem[]>();
  const [results, setResults] = useState<MediaItem[]>([]);
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState("movie");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function load() {
    setBusy(true);
    setError("");
    try {
      setItems(
        (await api<{ items: MediaItem[] }>(`/library/items/${id}/related`))
          .items,
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function search(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      setResults(
        (
          await api<{ items: MediaItem[] }>(
            `/library/imports/catalog?kind=${kind}&query=${encodeURIComponent(query)}`,
          )
        ).items.filter((v) => v.id !== id),
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function change(other: number, method: "POST" | "DELETE") {
    setBusy(true);
    setError("");
    try {
      await api(`/library/items/${id}/related`, {
        method,
        body: JSON.stringify({ other_id: other }),
      });
      setItems(
        (await api<{ items: MediaItem[] }>(`/library/items/${id}/related`))
          .items,
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel system-panel">
      <h2>Related library titles</h2>
      <p>
        Link saved movies and shows that belong together. Links are mutual and
        require an explicit selection.
      </p>
      {!items ? (
        <Button variant="soft" onClick={() => void load()} disabled={busy}>
          Manage related titles
        </Button>
      ) : (
        <>
          {items.length ? (
            <ul>
              {items.map((item) => (
                <li key={item.id}>
                  <Link
                    to={`/${item.kind === "series" ? "series" : "movies"}/${item.id}`}
                  >
                    {item.title}
                  </Link>{" "}
                  · {item.kind === "series" ? "TV show" : "Movie"}{" "}
                  <Button
                    size="1"
                    variant="ghost"
                    disabled={busy}
                    onClick={() => void change(item.id, "DELETE")}
                  >
                    Unlink
                  </Button>
                </li>
              ))}
            </ul>
          ) : (
            <p>No manually linked titles.</p>
          )}
          <form onSubmit={search}>
            <label>
              Title type{" "}
              <select value={kind} onChange={(e) => setKind(e.target.value)}>
                <option value="movie">Movies</option>
                <option value="series">TV shows</option>
              </select>
            </label>
            <label>
              Search saved titles
              <TextField.Root
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                required
                maxLength={200}
              />
            </label>
            <Button disabled={busy}>Search library</Button>
          </form>
          {results.map((item) => (
            <p key={item.id}>
              {item.title} {item.year && `(${item.year})`}{" "}
              <Button
                size="1"
                variant="soft"
                disabled={busy || items.some((v) => v.id === item.id)}
                onClick={() => void change(item.id, "POST")}
              >
                {items.some((v) => v.id === item.id) ? "Linked" : "Link title"}
              </Button>
            </p>
          ))}
        </>
      )}
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
    </section>
  );
}
