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
  return <ReleaseBrowser key={params.toString()} />;
}
function ReleaseBrowser() {
  const [params] = useSearchParams();
  const tv = params.has("series");
  const movieID = Number(params.get(tv ? "series" : "movie"));
  const season = Number(params.get("season"));
  const episode = params.has("episode") ? Number(params.get("episode")) : 0;
  const valid =
    Number.isSafeInteger(movieID) &&
    movieID > 0 &&
    (!tv ||
      (params.has("season") &&
        Number.isInteger(season) &&
        season >= 0 &&
        season <= 10000 &&
        Number.isInteger(episode) &&
        episode >= 0 &&
        episode <= 100000));
  const movie = useResource<MediaItem>(
    valid ? `/library/${tv ? "series" : "movies"}/${movieID}` : null,
  );
  const indexer = useResource<IntegrationStatus>("/indexers/status");
  const client = useResource<IntegrationStatus>("/integrations/qbittorrent");
  const sab = useResource<IntegrationStatus>("/integrations/sabnzbd");
  const replacementFiles = useResource<{
    items: { id: number; path: string }[];
  }>(`/organization/files?media_id=${movieID}`);
  const [replacementID, setReplacementID] = useState("");
  const [input, setInput] = useState("");
  const [items, setItems] = useState<Release[]>();
  const [searching, setSearching] = useState(false);
  const [pending, setPending] = useState("");
  const [error, setError] = useState("");
  const [warnings, setWarnings] = useState<string[]>([]);
  const navigate = useNavigate();
  const targetLabel = tv
    ? `S${String(season).padStart(2, "0")}${episode ? `E${String(episode).padStart(2, "0")}` : " · selected missing episodes"}`
    : "";
  const suggested = movie.data
    ? tv
      ? `${movie.data.title} S${String(season).padStart(2, "0")}${episode ? `E${String(episode).padStart(2, "0")}` : ""}`
      : `${movie.data.title}${movie.data.year ? ` ${movie.data.year}` : ""}`
    : "";
  async function search(e: FormEvent) {
    e.preventDefault();
    if (searching || !valid) return;
    setSearching(true);
    setError("");
    setItems(undefined);
    try {
      const out = await api<{ items: Release[]; warnings?: string[] }>(
        tv
          ? `/library/series/${movieID}/seasons/${season}/releases?${replacementID ? `replacement_file_id=${replacementID}&` : ""}${episode ? `episode=${episode}&` : ""}query=${encodeURIComponent(input.trim() || suggested)}`
          : `/releases?movie_id=${movieID}&query=${encodeURIComponent(input.trim() || suggested)}`,
      );
      setItems(out.items);
      setWarnings(out.warnings ?? []);
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
        body: JSON.stringify({
          release_id: id,
          ...(replacementID
            ? { replacement_file_id: Number(replacementID) }
            : {}),
        }),
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
        title={
          movie.data
            ? `Releases for ${movie.data.title}`
            : tv
              ? "TV releases"
              : "Movie releases"
        }
        description={
          tv
            ? `${targetLabel}. Review the release, then confirm episode files in Activity before importing.`
            : movie.data?.year
              ? `For your ${movie.data.year} movie. Review the release title before downloading.`
              : "Choose a release from your connected indexer."
        }
      />
      <Link className="text-link" to={tv ? `/series/${movieID}` : "/movies"}>
        {tv ? "Back to show" : "Back to movies"}
      </Link>
      {!!replacementFiles.data?.items.length && (
        <section className="panel">
          <label>
            Download purpose{" "}
            <select
              value={replacementID}
              onChange={(e) => setReplacementID(e.target.value)}
            >
              <option value="">
                Normal import (existing files are protected)
              </option>
              {replacementFiles.data.items.map((f) => (
                <option key={f.id} value={f.id}>
                  Acquire replacement for {f.path}
                </option>
              ))}
            </select>
          </label>
          <p>
            Replacement downloads are verified and retained for a separate
            review. TV mapping must cover every episode in the selected file.
          </p>
        </section>
      )}
      {!valid && (
        <p role="alert">
          Choose a valid {tv ? "episode or season" : "movie"} from your library
          first.
        </p>
      )}
      <RequestState
        loading={
          movie.loading || indexer.loading || client.loading || sab.loading
        }
        error={movie.error || indexer.error || client.error || sab.error}
        retry={() => {
          movie.reload();
          indexer.reload();
          client.reload();
          sab.reload();
        }}
      />
      {indexer.data && !indexer.data.configured && (
        <section className="panel connect-prompt">
          <h2>Release search is ready when you connect an indexer.</h2>
          <p>
            Add a direct tracker, a private RSS feed, Torznab or Newznab
            endpoint. Your library and monitoring choices remain available.
          </p>
          <Button asChild>
            <Link to="/indexers">Connect indexer</Link>
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
          {client.data &&
            sab.data &&
            !client.data.configured &&
            !sab.data.configured && (
              <p className="search-disclaimer">
                You can browse releases now.{" "}
                <Link to="/settings" className="text-link">
                  Connect qBittorrent or SABnzbd
                </Link>{" "}
                to download.
              </p>
            )}
          {items && (
            <>
              <p className="search-result-count">
                {items.length} {items.length === 1 ? "release" : "releases"}{" "}
                from your enabled indexers.
              </p>
              {items.length === 0 ? (
                <section className="panel search-intro">
                  <h2>No supported releases found.</h2>
                  <p>
                    Try another title or check the indexer’s{" "}
                    {tv ? "TV" : "movie"} categories and API endpoint.
                  </p>
                </section>
              ) : (
                <div className="release-list">
                  {items.map((release) => (
                    <article className="panel release-row" key={release.id}>
                      <div>
                        <h2>{release.title}</h2>
                        <p className="field-help">
                          {release.indexer ?? "Torznab"}
                          {release.protocol === "usenet"
                            ? " · Usenet · SABnzbd"
                            : " · Torrent · qBittorrent"}
                          {release.freeleech ? " · Freeleech" : ""}
                        </p>
                        <p>
                          {bytes(release.size_bytes)} ·{" "}
                          {release.protocol === "usenet"
                            ? "NZB"
                            : `${release.seeders ?? "Unknown"} seeders · ${release.peers ?? "Unknown"} peers`}
                          {release.published_at
                            ? ` · ${new Date(release.published_at).toLocaleDateString()}`
                            : ""}
                        </p>
                      </div>
                      <Button
                        disabled={
                          !!pending ||
                          !(release.protocol === "usenet"
                            ? sab.data?.configured
                            : client.data?.configured)
                        }
                        onClick={() => void download(release.id)}
                      >
                        <Download size={16} />
                        {pending === release.id ? "Sending…" : "Download"}
                      </Button>
                      {!(release.protocol === "usenet"
                        ? sab.data?.configured
                        : client.data?.configured) && (
                        <Link
                          className="text-link"
                          to={`/settings#${release.protocol === "usenet" ? "sabnzbd" : "qbittorrent"}`}
                        >
                          Connect{" "}
                          {release.protocol === "usenet"
                            ? "SABnzbd"
                            : "qBittorrent"}
                        </Link>
                      )}
                    </article>
                  ))}
                </div>
              )}
            </>
          )}
          <p className="search-disclaimer">
            Torrent releases use qBittorrent; NZB releases use SABnzbd.
            Completed video files are verified before importing.{" "}
            {tv
              ? "TV files require your episode mapping confirmation in Activity. "
              : ""}
            Torrent sources stay in place for seeding; existing library files
            are never replaced.
          </p>
        </>
      )}
      {error && (
        <p role="alert" className="movie-error">
          {error}
        </p>
      )}
      {warnings.length > 0 && (
        <div className="error-box" role="status">
          <div>
            <p>
              Some indexers need attention. Results from available sources are
              shown.
            </p>
            {warnings.map((warning) => (
              <p key={warning}>{warning}</p>
            ))}
            <Link className="text-link" to="/indexers">
              Review indexer connections
            </Link>
          </div>
        </div>
      )}
    </>
  );
}
