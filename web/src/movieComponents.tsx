import { useId, useState, type MouseEvent, type ReactNode } from "react";
import { Button, DropdownMenu, Switch } from "@radix-ui/themes";
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Film,
  Plus,
} from "lucide-react";
import { Link } from "react-router-dom";
import { api, messageFor, type Movie } from "./api";
import { RequestState } from "./components";
import { FormGroup, Label, Modal } from "./ui";
import { useResource } from "./useResource";
import "./movieComponents.css";

export function Poster({
  movie,
  className,
}: {
  movie: Pick<Movie, "poster_url" | "title">;
  className?: string;
}) {
  const [failed, setFailed] = useState(false);
  return (
    <div className={className ? `lib-poster ${className}` : "lib-poster"}>
      {movie.poster_url && !failed ? (
        <img
          src={movie.poster_url}
          alt={`${movie.title} poster`}
          loading="lazy"
          referrerPolicy="no-referrer"
          onError={() => setFailed(true)}
        />
      ) : (
        <div className="lib-poster-fallback">
          <Film size={32} strokeWidth={1} aria-hidden="true" />
          <span>Artwork unavailable</span>
        </div>
      )}
    </div>
  );
}

/**
 * Add New result row: poster thumbnail, title and year, labels, a clamped
 * overview and actions. `onOpen` makes the whole row open the details;
 * buttons and links inside the row keep their own behaviour.
 */
export function MovieCard({
  movie,
  children,
  href,
  labels,
  onOpen,
}: {
  movie: Movie;
  children: ReactNode;
  href?: string;
  labels?: ReactNode;
  onOpen?: () => void;
}) {
  function open(event: MouseEvent<HTMLElement>) {
    if (!onOpen) return;
    if ((event.target as HTMLElement).closest("button, a, input, select"))
      return;
    onOpen();
  }
  return (
    <article
      className={onOpen ? "add-result is-interactive" : "add-result"}
      onClick={open}
    >
      {href ? (
        <Link
          className="add-result-poster"
          to={href}
          aria-label={`Open ${movie.title} details`}
        >
          <Poster movie={movie} />
        </Link>
      ) : (
        <div className="add-result-poster">
          <Poster movie={movie} />
        </div>
      )}
      <div className="add-result-body">
        <div className="add-result-head">
          <h2 className="add-result-title">
            {href ? <Link to={href}>{movie.title}</Link> : movie.title}
          </h2>
          {movie.year && (
            <span className="add-result-year">({movie.year})</span>
          )}
        </div>
        <div className="add-result-meta">
          {!movie.year && <span>Release date unknown</span>}
          {movie.runtime_minutes ? (
            <span>{movie.runtime_minutes} min</span>
          ) : null}
          {labels}
        </div>
        <p className="add-result-overview">
          {movie.overview || "No synopsis is available yet."}
        </p>
      </div>
      <div className="add-result-actions">{children}</div>
    </article>
  );
}

export type SeriesMonitorMode = "none" | "missing" | "current" | "future";

const seriesMonitorOptions: { mode: SeriesMonitorMode; label: string }[] = [
  { mode: "none", label: "Add without monitoring" },
  { mode: "missing", label: "Add monitoring missing" },
  { mode: "current", label: "Add monitoring current season" },
  { mode: "future", label: "Add monitoring future episodes" },
];

export function AddMovieButton({
  id,
  added,
  onAdded,
  kind = "movie",
  category,
  createFolder = false,
  variant = "soft",
}: {
  id: number;
  added: boolean;
  onAdded: (id: number) => void;
  kind?: "movie" | "series";
  category?: "anime";
  createFolder?: boolean;
  variant?: "solid" | "soft";
}) {
  const [pending, setPending] = useState<
    "" | "monitored" | "unmonitored" | SeriesMonitorMode
  >("");
  const [error, setError] = useState("");
  const [folderMessage, setFolderMessage] = useState("");
  async function add(choice: "monitored" | "unmonitored" | SeriesMonitorMode) {
    if (pending) return;
    setPending(choice);
    setError("");
    try {
      const result = await api<{ item: { id: number } }>(
        kind === "series" ? "/library/series" : "/library/movies",
        {
          method: "POST",
          body: JSON.stringify({
            tmdb_id: id,
            ...(kind === "series"
              ? { monitor: choice }
              : { monitored: choice === "monitored" }),
            ...(category ? { category } : {}),
          }),
        },
      );
      if (createFolder) {
        try {
          if (!result?.item) throw new Error("The new title was not returned.");
          const folder = await api<{ path: string }>(
            `/library/items/${result.item.id}/create-folder`,
            { method: "POST", body: "{}" },
          );
          setFolderMessage(`Folder ready: ${folder.path}`);
        } catch (e) {
          setFolderMessage(
            `Title added, but the folder could not be created: ${messageFor(e)}`,
          );
        }
      }
      onAdded(id);
    } catch (error) {
      setError(messageFor(error));
    } finally {
      setPending("");
    }
  }
  if (added)
    return (
      <div className="add-button">
        <Label kind="success" large>
          In library
        </Label>
        {folderMessage && (
          <p role="status" className="add-button-message">
            {folderMessage}
          </p>
        )}
      </div>
    );
  const adding = pending !== "";
  return (
    <div className="add-button">
      {kind === "series" ? (
        <DropdownMenu.Root>
          <DropdownMenu.Trigger>
            <Button variant={variant} disabled={adding}>
              <Plus size={16} aria-hidden="true" />
              {adding ? "Adding…" : "Add show"}
              <ChevronDown size={14} aria-hidden="true" />
            </Button>
          </DropdownMenu.Trigger>
          <DropdownMenu.Content size="1">
            {seriesMonitorOptions.map((option) => (
              <DropdownMenu.Item
                key={option.mode}
                disabled={adding}
                onSelect={() => void add(option.mode)}
              >
                {option.label}
              </DropdownMenu.Item>
            ))}
          </DropdownMenu.Content>
        </DropdownMenu.Root>
      ) : (
        <div className="add-button-row">
          <Button
            variant="soft"
            disabled={adding}
            onClick={() => void add("unmonitored")}
          >
            {pending === "unmonitored" ? "Adding…" : "Add unmonitored"}
          </Button>
          <Button
            variant={variant}
            disabled={adding}
            onClick={() => void add("monitored")}
          >
            {pending === "monitored" ? "Adding…" : "Add monitored"}
          </Button>
        </div>
      )}
      {error && (
        <p role="alert" className="add-button-error">
          {error}
        </p>
      )}
    </div>
  );
}

/** Sonarr-style add modal: summary on top, add options, then actions. */
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
  const details = useResource<Movie & { seasons?: { air_date: string }[] }>(
    `/metadata/${kind === "series" ? "series" : "movies"}/${id}`,
  );
  const [createFolder, setCreateFolder] = useState(false);
  const folderId = useId();
  const data = details.data;

  return (
    <Modal
      open
      onOpenChange={(value) => {
        if (!value) close();
      }}
      size="large"
      title={
        data ? `${data.title}${data.year ? ` (${data.year})` : ""}` : title
      }
      description={`${kind === "series" ? "TV show" : "Movie"} details from TMDb.`}
      footer={
        <>
          {data && added && (
            <Button asChild variant="soft" className="is-left">
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
          <Button variant="soft" color="gray" onClick={close}>
            Close
          </Button>
          {data && (
            <AddMovieButton
              kind={kind}
              category={category}
              id={id}
              added={added}
              onAdded={onAdded}
              createFolder={createFolder}
              variant="solid"
            />
          )}
        </>
      }
    >
      <RequestState
        loading={details.loading}
        error={details.error}
        retry={details.reload}
      />
      {data && (
        <>
          <div className="add-modal-summary">
            <Poster movie={data} />
            <div>
              <div className="add-modal-meta">
                <span>{data.year ?? "Release date unknown"}</span>
                {data.runtime_minutes ? (
                  <span>{data.runtime_minutes} min</span>
                ) : null}
                {category === "anime" && <span>Anime</span>}
              </div>
              <p className="add-modal-overview">
                {data.overview || "No synopsis is available yet."}
              </p>
              <a
                href={`https://www.themoviedb.org/${kind === "series" ? "tv" : "movie"}/${id}`}
                target="_blank"
                rel="noreferrer"
              >
                View on TMDb <ExternalLink size={12} aria-hidden="true" />
              </a>
            </div>
          </div>
          {!added && (
            <div className="add-modal-form">
              <FormGroup
                label="Create folder"
                htmlFor={folderId}
                help="Create the destination folder in the library now."
              >
                <span className="add-modal-switch">
                  <Switch
                    id={folderId}
                    aria-label="Create the destination folder now"
                    checked={createFolder}
                    onCheckedChange={setCreateFolder}
                  />
                </span>
              </FormGroup>
            </div>
          )}
        </>
      )}
    </Modal>
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
    <nav className="rs-pagination" aria-label="Pagination">
      <Button
        variant="soft"
        color="gray"
        size="1"
        disabled={page <= 1}
        onClick={() => onPage(page - 1)}
      >
        <ChevronLeft size={14} aria-hidden="true" /> Previous
      </Button>
      <span>
        Page {page} of {pages}
      </span>
      <Button
        variant="soft"
        color="gray"
        size="1"
        disabled={page >= pages}
        onClick={() => onPage(page + 1)}
      >
        Next <ChevronRight size={14} aria-hidden="true" />
      </Button>
    </nav>
  );
}
