import { useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Download, Search } from "lucide-react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import {
  api,
  messageFor,
  type MediaItem,
  type IntegrationStatus,
  type Release,
} from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";

export function bytes(size: number) {
  if (size <= 0) return "Size unknown";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const n = Math.min(Math.floor(Math.log(size) / Math.log(1024)), 4);
  return `${(size / 1024 ** n).toLocaleString(undefined, { maximumFractionDigits: n ? 1 : 0 })} ${units[n]}`;
}
export function Releases() {
  const [params] = useSearchParams();
  const movieID = Number(params.get("movie"));
  const valid = Number.isSafeInteger(movieID) && movieID > 0;
  const movie = useResource<MediaItem>(
    valid ? `/library/movies/${movieID}` : null,
  );
  const indexer = useResource<IntegrationStatus>("/integrations/torznab");
  const client = useResource<IntegrationStatus>("/integrations/qbittorrent");
  const [input, setInput] = useState("");
  const [items, setItems] = useState<Release[]>();
  const [searching, setSearching] = useState(false);
  const [pending, setPending] = useState("");
  const [error, setError] = useState("");
  const navigate = useNavigate();
  const suggested = movie.data
    ? `${movie.data.title}${movie.data.year ? ` ${movie.data.year}` : ""}`
    : "";
  async function search(e: FormEvent) {
    e.preventDefault();
    if (searching || !valid) return;
    setSearching(true);
    setError("");
    setItems(undefined);
    try {
      const out = await api<{ items: Release[] }>(
        `/releases?movie_id=${movieID}&query=${encodeURIComponent(input.trim() || suggested)}`,
      );
      setItems(out.items);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setSearching(false);
    }
  }
  async function download(id: string) {
    if (pending) return;
    setPending(id);
    setError("");
    try {
      await api("/downloads", {
        method: "POST",
        body: JSON.stringify({ release_id: id }),
      });
      navigate("/activity");
    } catch (e) {
      setError(
        `${messageFor(e)} Check Activity before retrying if the connection was interrupted.`,
      );
    } finally {
      setPending("");
    }
  }
  return (
    <>
      <PageHeading
        eyebrow="FIND A RELEASE"
        title={
          movie.data ? `Releases for ${movie.data.title}` : "Movie releases"
        }
        description={
          movie.data?.year
            ? `For your ${movie.data.year} movie. Review the release title before downloading.`
            : "Choose a release from your connected indexer."
        }
      />
      <Link className="text-link" to="/movies">
        Back to movies
      </Link>
      {!valid && <p role="alert">Choose a movie from your library first.</p>}
      <RequestState
        loading={movie.loading || indexer.loading || client.loading}
        error={movie.error || indexer.error || client.error}
        retry={() => {
          movie.reload();
          indexer.reload();
          client.reload();
        }}
      />
      {indexer.data && !indexer.data.configured && (
        <section className="panel connect-prompt">
          <h2>Connect an indexer to find releases.</h2>
          <p>
            Add a Torznab endpoint from Prowlarr, Jackett, or another compatible
            service.
          </p>
          <Button asChild>
            <Link to="/settings">Connect indexer</Link>
          </Button>
        </section>
      )}
      {movie.data && indexer.data?.configured && (
        <>
          <form className="movie-search-form release-search" onSubmit={search}>
            <TextField.Root
              aria-label="Release search"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={suggested}
              maxLength={200}
              disabled={searching || !!pending}
            />
            <Button type="submit" disabled={searching || !!pending}>
              <Search size={16} />
              {searching ? "Searching…" : "Search releases"}
            </Button>
          </form>
          {client.data && !client.data.configured && (
            <p className="search-disclaimer">
              You can browse releases now.{" "}
              <Link to="/settings" className="text-link">
                Connect qBittorrent
              </Link>{" "}
              to download.
            </p>
          )}
          {items && (
            <>
              <p className="search-result-count">
                {items.length} {items.length === 1 ? "release" : "releases"}{" "}
                from your indexer.
              </p>
              {items.length === 0 ? (
                <section className="panel search-intro">
                  <h2>No supported torrent releases found.</h2>
                  <p>
                    Try another title or check the indexer’s movie categories.
                    Releases need a v1 or hybrid magnet, or a torrent URL served
                    by your indexer.
                  </p>
                </section>
              ) : (
                <div className="release-list">
                  {items.map((release) => (
                    <article className="panel release-row" key={release.id}>
                      <div>
                        <h2>{release.title}</h2>
                        <p>
                          {bytes(release.size_bytes)} ·{" "}
                          {release.seeders ?? "Unknown"} seeders ·{" "}
                          {release.peers ?? "Unknown"} peers
                          {release.published_at
                            ? ` · ${new Date(release.published_at).toLocaleDateString()}`
                            : ""}
                        </p>
                      </div>
                      <Button
                        disabled={!!pending || !client.data?.configured}
                        onClick={() => void download(release.id)}
                      >
                        <Download size={16} />
                        {pending === release.id ? "Sending…" : "Download"}
                      </Button>
                    </article>
                  ))}
                </div>
              )}
            </>
          )}
          <p className="search-disclaimer">
            Selected releases download into a dedicated qBittorrent folder.
            Completed movie files are verified before importing. Torrent sources
            stay in place for seeding; existing library files are never
            replaced.
          </p>
        </>
      )}
      {error && (
        <p role="alert" className="movie-error">
          {error}
        </p>
      )}
    </>
  );
}
