import { useEffect, useRef, useState } from "react";
import { Badge, Button } from "@radix-ui/themes";
import { ExternalLink, Play, RefreshCw } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { api, messageFor, type MediaItem } from "../api";
import { PageHeading, RequestState } from "../components";
import { Poster, Pagination } from "../movieComponents";
import { useResource } from "../useResource";
import { bytes } from "./Releases";
import { AnimeSettings } from "./AnimeSettings";
import { AnimeSearch } from "./AnimeSearch";
import { AnimeOrder } from "./AnimeOrder";
import { ItemOrganization } from "./Organization";
import { MovieCollection } from "./MovieCollection";
import { RelatedTitles } from "./RelatedTitles";
import { AutomaticFranchise } from "./AutomaticFranchise";

export type Information = {
  tagline: string;
  genres: string[];
  rating: number;
  votes: number;
  imdb_url: string;
  tmdb_url: string;
  trailer_url: string;
  cast: { id: number; name: string; role: string; image_url: string }[];
  crew: { id: number; name: string; role: string; image_url: string }[];
};
export type LocalDetails = {
  item: MediaItem;
  information: Information | null;
  metadata_updated_at: number;
  media_root: string;
  library_root: string;
  total_size_bytes: number;
  file_count: number;
  history_count: number;
  page: number;
  files: {
    origin?: string;
    id: number;
    path: string;
    directory: string;
    size_bytes: number;
    imported_at: number;
    download_id: string;
    release: string;
    episodes: string;
    probe: {
      codec: string;
      width: number;
      height: number;
      duration_seconds: number;
      audio_streams?:
        { codec: string; language: string; title: string }[] | null;
      embedded_title?: string | null;
    };
  }[];
  history: {
    protocol?: "torrent" | "usenet";
    id: string;
    title: string;
    indexer: string;
    state: string;
    progress: number;
    size_bytes: number;
    created_at: number;
    updated_at: number;
    error: string;
  }[];
};
export function ItemDetails() {
  const { id } = useParams();
  return id && /^[1-9][0-9]*$/.test(id) && Number.isSafeInteger(Number(id)) ? (
    <ItemInformation key={id} id={Number(id)} hero />
  ) : (
    <p role="alert">Choose an item from your library.</p>
  );
}
function External({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  return (
    <a
      className="text-link"
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      referrerPolicy="no-referrer"
    >
      {children}
      <ExternalLink size={14} />
    </a>
  );
}
export function ItemInformation({
  id,
  hero = false,
}: {
  id: number;
  hero?: boolean;
}) {
  const [page, setPage] = useState(1);
  const resource = useResource<LocalDetails>(
    `/library/items/${id}?page=${page}`,
  );
  const [fresh, setFresh] = useState<{
    information: Information;
    metadata_updated_at: number;
  }>();
  const [busy, setBusy] = useState(false);
  const [inspecting, setInspecting] = useState<number>();
  const [inspectionError, setInspectionError] = useState<{
    id: number;
    message: string;
  }>();
  const [error, setError] = useState("");
  const attempted = useRef(false);
  async function inspectFile(fileID: number) {
    if (inspecting) return;
    setInspecting(fileID);
    setInspectionError(undefined);
    try {
      await api(`/library/files/${fileID}/inspect`, {
        method: "POST",
        body: "{}",
      });
      resource.reload();
    } catch (e) {
      setInspectionError({ id: fileID, message: messageFor(e) });
    } finally {
      setInspecting(undefined);
    }
  }
  async function refresh() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      setFresh(
        await api(`/library/items/${id}/metadata`, {
          method: "POST",
          body: "{}",
        }),
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    if (!resource.data || attempted.current) return;
    attempted.current = true;
    if (
      !resource.data.information ||
      Date.now() / 1000 - resource.data.metadata_updated_at > 86400
    )
      void refresh();
  }, [resource.data]);
  const data = resource.data;
  const info = fresh?.information ?? data?.information;
  const updated = fresh?.metadata_updated_at ?? data?.metadata_updated_at;
  const groups = [
    { name: "Creators", roles: ["Creator", "Original Series Creator"] },
    { name: "Directors", roles: ["Director"] },
    {
      name: "Writers",
      roles: ["Writer", "Screenplay", "Story", "Novel", "Teleplay"],
    },
  ];
  return (
    <>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {data && (
        <>
          {hero && (
            <>
              <PageHeading
                title={data.item.title}
                description={[
                  data.item.year,
                  data.item.runtime_minutes
                    ? `${data.item.runtime_minutes} min`
                    : "",
                ]
                  .filter(Boolean)
                  .join(" · ")}
              >
                <Button
                  variant="soft"
                  disabled={busy}
                  onClick={() => void refresh()}
                >
                  <RefreshCw size={16} />
                  {busy ? "Updating details…" : "Refresh metadata"}
                </Button>
              </PageHeading>
              <Link
                className="text-link"
                to={
                  data.item.category === "anime"
                    ? "/anime"
                    : data.item.kind === "series"
                      ? "/series"
                      : "/movies"
                }
              >
                Back to{" "}
                {data.item.category === "anime"
                  ? "Anime"
                  : data.item.kind === "series"
                    ? "TV shows"
                    : "movies"}
              </Link>
              <section className="panel item-hero">
                <Link
                  className="text-link"
                  to={`/organization?media_id=${data.item.id}`}
                >
                  Organize or remove this title
                </Link>
                <Poster movie={data.item} />
                <div>
                  <div className="item-badges">
                    <Badge
                      color={
                        data.item.status === "available" ? "green" : "gray"
                      }
                    >
                      {data.item.status === "available"
                        ? "Available"
                        : data.item.status === "downloading"
                          ? "Downloading"
                          : "Not downloaded"}
                    </Badge>
                    <Badge color="gray">
                      {data.item.monitored ? "Monitored" : "Not monitored"}
                    </Badge>
                    {info?.rating ? (
                      <Badge>{info.rating.toFixed(1)} / 10 · TMDb</Badge>
                    ) : null}
                  </div>
                  {info?.tagline && (
                    <p className="item-tagline">{info.tagline}</p>
                  )}
                  <p>{data.item.overview || "No synopsis is available yet."}</p>
                  <p className="field-help">{info?.genres.join(" · ")}</p>
                  <div className="item-links">
                    {info?.imdb_url && (
                      <External href={info.imdb_url}>IMDb</External>
                    )}
                    <External
                      href={
                        info?.tmdb_url ??
                        `https://www.themoviedb.org/${data.item.kind === "series" ? "tv" : "movie"}/${data.item.tmdb_id}`
                      }
                    >
                      TMDb
                    </External>
                    {info?.trailer_url && (
                      <External href={info.trailer_url}>
                        <Play size={16} />
                        Watch trailer
                      </External>
                    )}
                    {data.item.kind === "movie" && (
                      <Button asChild variant="soft">
                        <Link to={`/releases?movie=${id}`}>Find releases</Link>
                      </Button>
                    )}
                    <Button asChild variant="soft">
                      <Link to="/automation">Quality & automation</Link>
                    </Button>
                  </div>
                </div>
              </section>
            </>
          )}
          {!hero && (
            <div className="item-links">
              <Button
                variant="soft"
                disabled={busy}
                onClick={() => void refresh()}
              >
                <RefreshCw size={16} />
                {busy ? "Updating credits…" : "Refresh cast & links"}
              </Button>
              {info?.imdb_url && <External href={info.imdb_url}>IMDb</External>}
              {info?.trailer_url && (
                <External href={info.trailer_url}>
                  <Play size={16} />
                  Watch trailer
                </External>
              )}
              {info?.genres.length ? (
                <span className="field-help">{info.genres.join(" · ")}</span>
              ) : null}
              {info?.rating ? (
                <Badge>TMDb {info.rating.toFixed(1)} / 10</Badge>
              ) : null}
            </div>
          )}
          {error && (
            <p role="alert" className="error-box">
              {error} Your saved library and local file information remain
              available.{" "}
              <Button
                variant="soft"
                disabled={busy}
                onClick={() => void refresh()}
              >
                Retry metadata
              </Button>
            </p>
          )}
          {busy && !info && (
            <p role="status">Loading cast, credits and trailers…</p>
          )}
          {info && (
            <>
              <div className="credits-grid">
                {groups.map((group) => {
                  const names = [
                    ...new Set(
                      info.crew
                        .filter((p) => group.roles.includes(p.role))
                        .map((p) => p.name),
                    ),
                  ];
                  return (
                    <section className="panel system-panel" key={group.name}>
                      <h2>{group.name}</h2>
                      <p>
                        {names.length
                          ? names.join(", ")
                          : "Not listed by TMDb."}
                      </p>
                    </section>
                  );
                })}
              </div>
              <section className="panel system-panel">
                <h2>Main cast</h2>
                {info.cast.length ? (
                  <div className="cast-grid">
                    {info.cast.map((person) => (
                      <a
                        key={person.id}
                        href={`https://www.themoviedb.org/person/${person.id}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        referrerPolicy="no-referrer"
                        className="cast-card"
                      >
                        {person.image_url ? (
                          <img
                            src={person.image_url}
                            alt={person.name}
                            loading="lazy"
                            referrerPolicy="no-referrer"
                          />
                        ) : (
                          <span className="cast-fallback" aria-hidden="true">
                            {person.name.charAt(0)}
                          </span>
                        )}
                        <strong>{person.name}</strong>
                        <span>{person.role || "Cast"}</span>
                      </a>
                    ))}
                  </div>
                ) : (
                  <p>No cast is listed yet.</p>
                )}
                <p className="field-help">
                  Metadata from TMDb
                  {updated
                    ? ` · Saved ${new Date(updated * 1000).toLocaleString()}`
                    : ""}
                  .{" "}
                  {info.trailer_url
                    ? "The trailer opens on YouTube."
                    : "No supported trailer is listed yet."}
                </p>
              </section>
            </>
          )}
          <section className="panel system-panel">
            <h2>In your library</h2>
            <div className="item-storage-stats">
              <div>
                <span>Registered files</span>
                <strong>{data.file_count}</strong>
              </div>
              <div>
                <span>Total file size</span>
                <strong>
                  {data.total_size_bytes
                    ? bytes(data.total_size_bytes)
                    : "No files imported"}
                </strong>
              </div>
              <div>
                <span>Download history</span>
                <strong>
                  {data.history_count}{" "}
                  {data.history_count === 1 ? "release" : "releases"}
                </strong>
              </div>
            </div>
            <p className="field-help">
              Sizes and locations come from verified import records. They are
              not a current disk scan or a measure of physical space used by
              hardlinks.
            </p>
            <dl className="detail-paths">
              <dt>Media root</dt>
              <dd>
                <code>{data.media_root}</code>
              </dd>
              <dt>Current destination root</dt>
              <dd>
                <code>{data.library_root}</code>
              </dd>
            </dl>
            <p className="field-help">
              Existing files retain their original locations when folder
              preferences change.
            </p>
          </section>
          <section className="panel system-panel">
            <h2>Files & locations</h2>
            {data.files.length ? (
              <div className="file-details-list">
                {data.files.map((file) => (
                  <article key={file.id}>
                    <h3>
                      {file.episodes || "Movie file"} · {bytes(file.size_bytes)}
                    </h3>
                    <p>
                      <code>{file.path}</code>
                    </p>
                    <p className="field-help">
                      Directory: <code>{file.directory}</code>
                    </p>
                    <p>
                      {[
                        file.probe.codec
                          ? file.probe.codec.toUpperCase()
                          : "Codec unknown",
                        file.probe.height
                          ? `${file.probe.width} × ${file.probe.height}`
                          : "",
                        file.probe.duration_seconds
                          ? `${Math.round(file.probe.duration_seconds / 60)} min`
                          : "",
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </p>
                    <p>
                      {file.probe.embedded_title == null
                        ? "Embedded title has not been inspected yet."
                        : file.probe.embedded_title === ""
                          ? "No embedded title was reported."
                          : `Embedded title: ${file.probe.embedded_title}`}
                    </p>
                    {data.item.kind === "movie" &&
                      file.probe.embedded_title &&
                      file.probe.embedded_title.trim().toLocaleLowerCase() !==
                        data.item.title.trim().toLocaleLowerCase() && (
                        <p className="field-help">
                          The embedded title differs from the saved movie title:{" "}
                          {data.item.title}.
                        </p>
                      )}
                    <p className="field-help">
                      Embedded title changes can be reviewed in{" "}
                      <Link
                        className="text-link"
                        to={`/organization?media_id=${id}`}
                      >
                        Organize library
                      </Link>
                      .
                    </p>
                    <p>
                      {file.probe.audio_streams == null
                        ? "Audio tags have not been inspected yet."
                        : file.probe.audio_streams.length === 0
                          ? "No audio streams were reported."
                          : `Embedded audio tags: ${file.probe.audio_streams.map((stream) => [stream.language && stream.language !== "und" ? stream.language : "language unknown", stream.codec, stream.title].filter(Boolean).join(" · ")).join(", ")}`}
                    </p>
                    <p className="field-help">
                      Embedded tags come from the file and may be missing or
                      inaccurate. Rechecking reads the entire file.
                    </p>
                    <Button
                      variant="soft"
                      disabled={!!inspecting}
                      onClick={() => void inspectFile(file.id)}
                    >
                      {inspecting === file.id
                        ? "Inspecting…"
                        : "Inspect embedded tags"}
                    </Button>
                    {inspectionError?.id === file.id && (
                      <p role="alert" className="form-error">
                        {inspectionError.message}
                      </p>
                    )}
                    <p>
                      {file.origin === "existing"
                        ? "Existing file"
                        : file.origin === "youtube"
                          ? "YouTube video"
                          : "Release"}
                      : {file.release}
                    </p>
                    {file.origin === "existing" && (
                      <Badge color="gray">
                        Registered in its existing location
                      </Badge>
                    )}
                    <p className="field-help">
                      {file.origin === "existing" ? "Registered" : "Imported"}{" "}
                      {new Date(file.imported_at * 1000).toLocaleString()}
                    </p>
                    {file.download_id && (
                      <Link
                        className="text-link"
                        to={`/activity?job=${encodeURIComponent(file.download_id)}`}
                      >
                        View download
                      </Link>
                    )}
                  </article>
                ))}
              </div>
            ) : (
              <p>
                {data.file_count
                  ? "No files on this page."
                  : "No verified files have been imported for this item."}
              </p>
            )}
          </section>
          <section className="panel system-panel">
            <h2>Release & download history</h2>
            {data.history.length ? (
              <div className="file-details-list">
                {data.history.map((job) => (
                  <article key={job.id}>
                    <div className="indexer-title">
                      <h3>{job.title}</h3>
                      <Badge
                        color={
                          job.state === "completed"
                            ? "green"
                            : job.state === "failed" || job.state === "blocked"
                              ? "amber"
                              : "gray"
                        }
                      >
                        {job.state}
                      </Badge>
                    </div>
                    <p>
                      {job.indexer ||
                        "Indexer not recorded for this older download"}{" "}
                      · {job.protocol === "usenet" ? "Usenet" : "Torrent"} ·{" "}
                      {bytes(job.size_bytes)} · {Math.round(job.progress * 100)}
                      %
                    </p>
                    <p className="field-help">
                      Added {new Date(job.created_at * 1000).toLocaleString()} ·
                      Updated {new Date(job.updated_at * 1000).toLocaleString()}
                    </p>
                    {job.error && <p className="movie-error">{job.error}</p>}
                    <Link
                      className="text-link"
                      to={`/activity?job=${encodeURIComponent(job.id)}`}
                    >
                      Open download details
                    </Link>
                  </article>
                ))}
              </div>
            ) : (
              <p>
                {data.history_count
                  ? "No downloads on this page."
                  : "No release has been downloaded for this item yet."}
              </p>
            )}
          </section>
          <AnimeSettings id={id} onSaved={resource.reload} />
          {data.item.category === "anime" && data.item.kind === "series" && (
            <AnimeSearch id={id} />
          )}
          {data.item.category === "anime" && <AnimeOrder id={id} />}
          {data.item.kind === "movie" && <MovieCollection id={id} />}
          <AutomaticFranchise id={id} />
          <RelatedTitles id={id} />
          <ItemOrganization id={id} />
          <Pagination
            page={page}
            pages={Math.ceil(
              Math.max(data.file_count, data.history_count) / 50,
            )}
            onPage={setPage}
          />
        </>
      )}
    </>
  );
}
