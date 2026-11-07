import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor, type MediaItem } from "../api";
import { Alert, FieldSet, Label } from "../ui";

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
    <FieldSet
      legend="Linked titles"
      description="Mutual links between saved movies and shows that belong together."
      actions={
        !items && (
          <Button
            size="1"
            variant="soft"
            onClick={() => void load()}
            disabled={busy}
          >
            Manage related titles
          </Button>
        )
      }
    >
      {items && (
        <>
          {items.length ? (
            <ul className="det-link-list" aria-label="Linked titles">
              {items.map((item) => (
                <li key={item.id}>
                  <Link
                    to={`/${item.kind === "series" ? "series" : "movies"}/${item.id}`}
                  >
                    {item.title}
                  </Link>
                  <Label outline>
                    {item.kind === "series" ? "TV show" : "Movie"}
                  </Label>
                  <Button
                    size="1"
                    variant="ghost"
                    color="red"
                    disabled={busy}
                    onClick={() => void change(item.id, "DELETE")}
                  >
                    Unlink
                  </Button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="rs-muted">No linked titles.</p>
          )}
          <form className="det-inline-form" onSubmit={search}>
            <label>
              Title type
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
            <Button variant="soft" disabled={busy}>
              Search library
            </Button>
          </form>
          {results.length > 0 && (
            <ul className="det-link-list" aria-label="Search results">
              {results.map((item) => {
                const linked = items.some((v) => v.id === item.id);
                return (
                  <li key={item.id}>
                    <span>
                      {item.title} {item.year && `(${item.year})`}
                    </span>
                    <Button
                      size="1"
                      variant="soft"
                      disabled={busy || linked}
                      onClick={() => void change(item.id, "POST")}
                    >
                      {linked ? "Linked" : "Link title"}
                    </Button>
                  </li>
                );
              })}
            </ul>
          )}
        </>
      )}
      {error && <Alert kind="danger">{error}</Alert>}
    </FieldSet>
  );
}
