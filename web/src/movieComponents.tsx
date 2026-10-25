import { useState, type ReactNode } from "react";
import { Badge, Button, Dialog, Switch } from "@radix-ui/themes";
import { Check, ChevronLeft, ChevronRight, Film, Plus } from "lucide-react";
import { Link } from "react-router-dom";
import { api, messageFor, type Movie } from "./api";
import { RequestState } from "./components";
import { useResource } from "./useResource";

export function Poster({
  movie,
}: {
  movie: Pick<Movie, "poster_url" | "title">;
}) {
  const [failed, setFailed] = useState(false);
  return (
    <div className="movie-poster">
      {movie.poster_url && !failed ? (
        <img
          src={movie.poster_url}
          alt={`${movie.title} poster`}
          loading="lazy"
          referrerPolicy="no-referrer"
          onError={() => setFailed(true)}
        />
      ) : (
        <div className="poster-fallback">
          <Film size={35} strokeWidth={1} />
          <span>Artwork unavailable</span>
        </div>
      )}
    </div>
  );
}
export function MovieCard({
  movie,
  children,
  href,
}: {
  movie: Movie;
  children: ReactNode;
  href?: string;
}) {
  return (
    <article className="panel movie-card">
      {href ? (
        <Link to={href} aria-label={`Open ${movie.title} details`}>
          <Poster movie={movie} />
        </Link>
      ) : (
        <Poster movie={movie} />
      )}
      <div className="movie-card-copy">
        <h2>{href ? <Link to={href}>{movie.title}</Link> : movie.title}</h2>
        <p className="movie-meta">
          {movie.year ?? "Release date unknown"}
          {movie.runtime_minutes ? ` · ${movie.runtime_minutes} min` : ""}
        </p>
        <p className="movie-overview">
          {movie.overview || "No synopsis is available yet."}
        </p>
        {children}
      </div>
    </article>
  );
}
export function AddMovieButton({
  id,
  added,
  onAdded,
  monitored = true,
  kind = "movie",
  category,
}: {
  id: number;
  added: boolean;
  onAdded: (id: number) => void;
  monitored?: boolean;
  kind?: "movie" | "series";
  category?: "anime";
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function add() {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api(kind === "series" ? "/library/series" : "/library/movies", {
        method: "POST",
        body: JSON.stringify({
          tmdb_id: id,
          monitored,
          ...(category ? { category } : {}),
        }),
      });
      onAdded(id);
    } catch (error) {
      setError(messageFor(error));
    } finally {
      setPending(false);
    }
  }
  if (added)
    return (
      <Badge color="green" size="2">
        <Check size={14} /> In library
      </Badge>
    );
  return (
    <div>
      <Button disabled={pending} onClick={() => void add()}>
        <Plus size={16} />
        {pending ? "Adding…" : kind === "series" ? "Add show" : "Add movie"}
      </Button>
      {error && (
        <p role="alert" className="movie-error">
          {error}
        </p>
      )}
    </div>
  );
}
export function MovieDetails({
  id,
  title,
  added,
  onAdded,
  close,
  kind = "movie",
  category,
}: {
  id: number;
  title: string;
  added: boolean;
  onAdded: (id: number) => void;
  close: () => void;
  kind?: "movie" | "series";
  category?: "anime";
}) {
  const details = useResource<Movie>(
    `/metadata/${kind === "series" ? "series" : "movies"}/${id}`,
  );
  const [monitored, setMonitored] = useState(true);
  return (
    <Dialog.Root
      open
      onOpenChange={(value) => {
        if (!value) close();
      }}
    >
      <Dialog.Content maxWidth="650px">
        <Dialog.Title>{details.data?.title ?? title}</Dialog.Title>
        <Dialog.Description size="2">
          {kind === "series" ? "TV show" : "Movie"} details from TMDb.
        </Dialog.Description>
        <RequestState
          loading={details.loading}
          error={details.error}
          retry={details.reload}
        />
        {details.data && (
          <>
            <div className="movie-details">
              <Poster movie={details.data} />
              <div>
                <p className="movie-meta">
                  {details.data.year ?? "Release date unknown"}
                  {details.data.runtime_minutes
                    ? ` · ${details.data.runtime_minutes} min`
                    : ""}
                </p>
                <p>
                  {details.data.overview || "No synopsis is available yet."}
                </p>
                <a
                  className="text-link"
                  href={`https://www.themoviedb.org/${kind === "series" ? "tv" : "movie"}/${id}`}
                  target="_blank"
                  rel="noreferrer"
                >
                  View on TMDb
                </a>
              </div>
            </div>
            {!added && (
              <>
                <label className="monitor-choice">
                  <Switch checked={monitored} onCheckedChange={setMonitored} />{" "}
                  Monitor this {kind === "series" ? "show" : "movie"}
                </label>
                <p className="field-help">
                  {kind === "series"
                    ? "Regular seasons start selected; specials start unselected. Choose seasons and episodes in your library, then find releases. Completed TV files need your mapping confirmation before import."
                    : "Saves your monitoring preference. Choose releases from the library or configure scheduled searches in Automation."}
                </p>
              </>
            )}
            <div className="movie-dialog-actions">
              <AddMovieButton
                kind={kind}
                category={category}
                id={id}
                added={added}
                onAdded={onAdded}
                monitored={monitored}
              />
              {added && (
                <Button asChild variant="soft">
                  <Link
                    to={
                      category === "anime"
                        ? "/anime"
                        : kind === "series"
                          ? "/series"
                          : "/movies"
                    }
                    onClick={close}
                  >
                    Open library
                  </Link>
                </Button>
              )}
              <Dialog.Close>
                <Button variant="soft" color="gray">
                  Close
                </Button>
              </Dialog.Close>
            </div>
          </>
        )}
        {!details.data && (
          <Dialog.Close>
            <Button variant="soft" color="gray">
              Close
            </Button>
          </Dialog.Close>
        )}
      </Dialog.Content>
    </Dialog.Root>
  );
}
export function Pagination({
  page,
  pages,
  onPage,
}: {
  page: number;
  pages: number;
  onPage: (page: number) => void;
}) {
  if (pages <= 1) return null;
  return (
    <nav className="pagination" aria-label="Pagination">
      <Button
        variant="soft"
        disabled={page <= 1}
        onClick={() => onPage(page - 1)}
      >
        <ChevronLeft size={16} /> Previous
      </Button>
      <span>
        Page {page} of {pages}
      </span>
      <Button
        variant="soft"
        disabled={page >= pages}
        onClick={() => onPage(page + 1)}
      >
        Next <ChevronRight size={16} />
      </Button>
    </nav>
  );
}
