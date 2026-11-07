import { useState, type ReactNode } from "react";
import { Button, Tabs } from "@radix-ui/themes";
import { Bookmark, ExternalLink, Search } from "lucide-react";
import { Link } from "react-router-dom";
import type { MediaItem } from "../../api";
import { Poster } from "../../movieComponents";
import {
  Alert,
  FieldSet,
  Label,
  Table,
  formatBytes,
  formatTimestamp,
  type Kind,
} from "../../ui";
import type { DetailFile, ItemDetailsState } from "./useItemDetails";
import "./Details.css";

export function External({
  href,
  children,
}: {
  href: string;
  children: ReactNode;
}) {
  return (
    <a
      className="det-external"
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      referrerPolicy="no-referrer"
    >
      {children}
      <ExternalLink size={13} aria-hidden="true" />
    </a>
  );
}

/** Sonarr-style bookmark toggle; exposed as a switch for assistive tech. */
export function MonitorToggle({
  checked,
  onChange,
  disabled,
  label,
  large,
}: {
  checked: boolean;
  onChange: (value: boolean) => void;
  disabled?: boolean;
  label: string;
  large?: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      title={checked ? "Monitored" : "Unmonitored"}
      className={`det-monitor${checked ? " is-on" : ""}${large ? " is-large" : ""}`}
      disabled={disabled}
      onClick={() => onChange(!checked)}
    >
      <Bookmark
        size={large ? 24 : 17}
        fill={checked ? "currentColor" : "none"}
        aria-hidden="true"
      />
    </button>
  );
}

/** Small icon-only link button, used for per-row manual searches. */
export function IconLink({ to, label }: { to: string; label: string }) {
  return (
    <Link className="det-icon-link" to={to} aria-label={label} title={label}>
      <Search size={16} aria-hidden="true" />
    </Link>
  );
}

export function profileLabel(item: MediaItem) {
  const profile = item.quality_profile;
  if (!profile) return null;
  return (
    <Label
      outline
      title={
        profile.profile_id === null
          ? "Inherited from the default profile"
          : "Assigned to this title"
      }
    >
      {profile.effective_profile.name}
      {profile.profile_id === null ? " · default" : " · assigned"}
    </Label>
  );
}

/** Wide header with poster, title, facts, labels and overview. */
export function DetailHeader({
  item,
  title,
  monitor,
  facts,
  labels,
  details,
  links,
}: {
  item: Pick<MediaItem, "poster_url" | "title" | "overview">;
  title: string;
  monitor?: ReactNode;
  facts: (string | number | null | undefined | false)[];
  labels: ReactNode;
  details?: ReactNode;
  links?: ReactNode;
}) {
  return (
    <header className="det-header">
      {item.poster_url && (
        <div
          className="det-backdrop"
          aria-hidden="true"
          style={{ backgroundImage: `url(${item.poster_url})` }}
        />
      )}
      <div className="det-header-inner">
        <Poster movie={item} className="det-poster" />
        <div className="det-header-copy">
          <div className="det-title-row">
            {monitor}
            <h1 className="det-title">{title}</h1>
          </div>
          <div className="det-facts">
            {facts.filter(Boolean).map((fact) => (
              <span key={String(fact)}>{fact}</span>
            ))}
          </div>
          <div className="det-labels">{labels}</div>
          {details && <dl className="det-details">{details}</dl>}
          <p className="det-overview">
            {item.overview || "No synopsis is available yet."}
          </p>
          {links && <div className="det-links">{links}</div>}
        </div>
      </div>
    </header>
  );
}

export function DetailFact({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <div>
      <dt>{title}</dt>
      <dd>{children}</dd>
    </div>
  );
}

/** Header links shared by movies and shows: TMDb, IMDb and trailer. */
export function ProviderLinks({
  details,
  kind,
  tmdbID,
}: {
  details: ItemDetailsState;
  kind: "movie" | "series";
  tmdbID: number;
}) {
  const info = details.info;
  return (
    <>
      <External
        href={
          info?.tmdb_url ||
          `https://www.themoviedb.org/${kind === "series" ? "tv" : "movie"}/${tmdbID}`
        }
      >
        TMDb
      </External>
      {info?.imdb_url && <External href={info.imdb_url}>IMDb</External>}
      {info?.trailer_url && (
        <External href={info.trailer_url}>Trailer</External>
      )}
    </>
  );
}

export function MetadataError({ details }: { details: ItemDetailsState }) {
  if (!details.error) return null;
  return (
    <Alert kind="danger" title="Metadata refresh failed">
      <p>{details.error} Saved library and file details are still shown.</p>
      <div>
        <Button
          size="1"
          variant="soft"
          disabled={details.busy}
          onClick={() => void details.refresh()}
        >
          Retry metadata
        </Button>
      </div>
    </Alert>
  );
}

/**
 * Tabs whose panels mount on first visit and then stay mounted, so open
 * forms and expanded seasons survive switching tabs.
 */
export function DetailTabs({
  tabs,
  label,
}: {
  label: string;
  tabs: { value: string; label: string; content: ReactNode }[];
}) {
  const [value, setValue] = useState(tabs[0].value);
  const [seen, setSeen] = useState(() => new Set([tabs[0].value]));
  return (
    <Tabs.Root
      value={value}
      onValueChange={(next) => {
        setValue(next);
        setSeen((current) => new Set(current).add(next));
      }}
    >
      <Tabs.List aria-label={label}>
        {tabs.map((tab) => (
          <Tabs.Trigger key={tab.value} value={tab.value}>
            {tab.label}
          </Tabs.Trigger>
        ))}
      </Tabs.List>
      {tabs.map(
        (tab) =>
          seen.has(tab.value) && (
            <Tabs.Content
              key={tab.value}
              value={tab.value}
              forceMount
              hidden={value !== tab.value}
              className="det-tab-panel"
            >
              {tab.content}
            </Tabs.Content>
          ),
      )}
    </Tabs.Root>
  );
}

function mediaFacts(file: DetailFile) {
  return [
    file.probe.codec ? file.probe.codec.toUpperCase() : "Codec unknown",
    file.probe.height ? `${file.probe.width}×${file.probe.height}` : "",
    file.probe.duration_seconds
      ? `${Math.round(file.probe.duration_seconds / 60)} min`
      : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

function audioText(file: DetailFile) {
  const streams = file.probe.audio_streams;
  if (streams == null) return "Audio: not inspected";
  if (streams.length === 0) return "Audio: none reported";
  return `Audio: ${streams
    .map((stream) =>
      [
        stream.language && stream.language !== "und"
          ? stream.language
          : "language unknown",
        stream.codec,
        stream.title,
      ]
        .filter(Boolean)
        .join(" · "),
    )
    .join(", ")}`;
}

function originLabel(origin?: string) {
  if (origin === "existing") return "Existing file";
  if (origin === "youtube") return "YouTube";
  return "Release";
}

/** Registered files with stream facts, embedded tags and provenance. */
export function FilesSection({
  details,
  savedTitle,
}: {
  details: ItemDetailsState;
  savedTitle: string;
}) {
  const data = details.data!;
  const movie = data.item.kind === "movie";
  return (
    <FieldSet
      legend="Files"
      description={
        <>
          From import records, not a disk scan. Media root{" "}
          <code>{data.media_root}</code>
        </>
      }
    >
      {data.files.length ? (
        <>
          <Table label="Registered files">
            <thead>
              <tr>
                <th scope="col">File</th>
                <th scope="col">{movie ? "Version" : "Episodes"}</th>
                <th scope="col" className="is-numeric">
                  Size
                </th>
                <th scope="col">Media</th>
                <th scope="col">Embedded tags</th>
                <th scope="col">Source</th>
                <th scope="col" className="is-actions">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {data.files.map((file) => {
                const title = file.probe.embedded_title;
                const differs =
                  movie &&
                  !!title &&
                  title.trim().toLocaleLowerCase() !==
                    savedTitle.trim().toLocaleLowerCase();
                return (
                  <tr key={file.id}>
                    <td className="det-file-cell">
                      <span className="rs-strong">
                        {file.path.split("/").at(-1)}
                      </span>
                      <code className="det-path">{file.path}</code>
                    </td>
                    <td>
                      {movie ? (
                        <Label kind="success">
                          {file.version_label || "Movie"}
                        </Label>
                      ) : (
                        file.episodes || "—"
                      )}
                    </td>
                    <td className="is-numeric">
                      {formatBytes(file.size_bytes)}
                    </td>
                    <td>{mediaFacts(file)}</td>
                    <td className="det-tags-cell">
                      {title == null && file.probe.audio_streams == null ? (
                        <span className="rs-muted">Not inspected</span>
                      ) : (
                        <>
                          <span>
                            {title == null
                              ? "Title: not inspected"
                              : title === ""
                                ? "Title: none"
                                : `Title: ${title}`}
                          </span>
                          <span>{audioText(file)}</span>
                          {differs && (
                            <Label kind="warning" outline>
                              Differs from the saved title
                            </Label>
                          )}
                        </>
                      )}
                      {details.inspectionError?.id === file.id && (
                        <Alert kind="danger">
                          {details.inspectionError.message}
                        </Alert>
                      )}
                    </td>
                    <td className="det-source-cell">
                      <span>
                        <Label
                          kind={
                            file.origin === "existing" ? "disabled" : "default"
                          }
                        >
                          {originLabel(file.origin)}
                        </Label>{" "}
                        {file.release}
                      </span>
                      <span className="rs-muted rs-small">
                        {file.origin === "existing"
                          ? "Registered in place"
                          : "Imported"}{" "}
                        {formatTimestamp(file.imported_at)}
                      </span>
                      {file.download_id && (
                        <Link
                          to={`/activity?job=${encodeURIComponent(file.download_id)}`}
                        >
                          View download
                        </Link>
                      )}
                    </td>
                    <td className="is-actions">
                      <Button
                        size="1"
                        variant="soft"
                        disabled={!!details.inspecting}
                        onClick={() => void details.inspect(file.id)}
                      >
                        {details.inspecting === file.id
                          ? "Inspecting…"
                          : "Inspect embedded tags"}
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
          <p className="rs-form-help">
            Embedded tags come from the file and may be wrong; inspecting reads
            the whole file. Review title changes in{" "}
            <Link to={`/organization?media_id=${data.item.id}`}>Organize</Link>.
          </p>
        </>
      ) : (
        <p className="rs-muted">
          {data.file_count
            ? "No files on this page."
            : "No file is registered for this title."}
        </p>
      )}
    </FieldSet>
  );
}

function stateKind(state: string): Kind {
  if (state === "completed") return "success";
  if (state === "failed" || state === "blocked") return "danger";
  if (state === "downloading") return "info";
  if (state === "importing" || state === "queued") return "purple";
  if (state.includes("review") || state === "choose_file") return "warning";
  return "default";
}

/** Download jobs recorded for the title. */
export function HistorySection({ details }: { details: ItemDetailsState }) {
  const data = details.data!;
  return (
    <FieldSet legend="History">
      {data.history.length ? (
        <Table label="Download history">
          <thead>
            <tr>
              <th scope="col">Release</th>
              <th scope="col">Indexer</th>
              <th scope="col">Protocol</th>
              <th scope="col" className="is-numeric">
                Size
              </th>
              <th scope="col" className="is-numeric">
                Progress
              </th>
              <th scope="col">State</th>
              <th scope="col">Added</th>
              <th scope="col" className="is-actions">
                <span className="sr-only">Details</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {data.history.map((job) => (
              <tr key={job.id}>
                <td className="det-file-cell">
                  <span>{job.title}</span>
                  {job.error && (
                    <span className="rs-danger-text rs-small">{job.error}</span>
                  )}
                </td>
                <td>
                  {job.indexer || (
                    <span className="rs-muted">Not recorded</span>
                  )}
                </td>
                <td>{job.protocol === "usenet" ? "Usenet" : "Torrent"}</td>
                <td className="is-numeric">{formatBytes(job.size_bytes)}</td>
                <td className="is-numeric">
                  {Math.round(job.progress * 100)}%
                </td>
                <td>
                  <Label kind={stateKind(job.state)}>
                    {job.state.replaceAll("_", " ")}
                  </Label>
                </td>
                <td title={`Updated ${formatTimestamp(job.updated_at)}`}>
                  {formatTimestamp(job.created_at)}
                </td>
                <td className="is-actions">
                  <Link to={`/activity?job=${encodeURIComponent(job.id)}`}>
                    Details
                  </Link>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      ) : (
        <p className="rs-muted">
          {data.history_count
            ? "No downloads on this page."
            : "Nothing has been downloaded for this title yet."}
        </p>
      )}
    </FieldSet>
  );
}

const crewGroups = [
  { name: "Creators", roles: ["Creator", "Original Series Creator"] },
  { name: "Directors", roles: ["Director"] },
  {
    name: "Writers",
    roles: ["Writer", "Screenplay", "Story", "Novel", "Teleplay"],
  },
];

/** TMDb crew and cast cached with the title. */
export function CreditsSection({
  details,
  refreshAction,
}: {
  details: ItemDetailsState;
  refreshAction?: boolean;
}) {
  const info = details.info;
  const groups = info
    ? crewGroups
        .map((group) => ({
          name: group.name,
          names: [
            ...new Set(
              info.crew
                .filter((person) => group.roles.includes(person.role))
                .map((person) => person.name),
            ),
          ],
        }))
        .filter((group) => group.names.length)
    : [];
  return (
    <FieldSet
      legend="Cast & crew"
      actions={
        refreshAction && (
          <Button
            size="1"
            variant="soft"
            disabled={details.busy}
            onClick={() => void details.refresh()}
          >
            {details.busy ? "Refreshing…" : "Refresh credits"}
          </Button>
        )
      }
    >
      {details.busy && !info && (
        <p role="status" className="rs-muted">
          Loading cast and credits…
        </p>
      )}
      {!details.busy && !info && (
        <p className="rs-muted">No credits are saved yet.</p>
      )}
      {info && (
        <>
          {groups.length > 0 && (
            <dl className="det-crew">
              {groups.map((group) => (
                <div key={group.name}>
                  <dt>{group.name}</dt>
                  <dd>{group.names.join(", ")}</dd>
                </div>
              ))}
            </dl>
          )}
          {info.cast.length ? (
            <ul className="det-cast" aria-label="Cast">
              {info.cast.map((person) => (
                <li key={person.id}>
                  <a
                    href={`https://www.themoviedb.org/person/${person.id}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    referrerPolicy="no-referrer"
                    className="det-cast-card"
                  >
                    {person.image_url ? (
                      <img
                        src={person.image_url}
                        alt=""
                        loading="lazy"
                        referrerPolicy="no-referrer"
                      />
                    ) : (
                      <span className="det-cast-fallback" aria-hidden="true">
                        {person.name.charAt(0)}
                      </span>
                    )}
                    <span className="det-cast-name">{person.name}</span>
                    <span className="det-cast-role">
                      {person.role || "Cast"}
                    </span>
                  </a>
                </li>
              ))}
            </ul>
          ) : (
            <p className="rs-muted">TMDb lists no cast yet.</p>
          )}
          <p className="rs-form-help">
            From TMDb
            {details.updated
              ? ` · saved ${formatTimestamp(details.updated)}`
              : ""}
          </p>
        </>
      )}
    </FieldSet>
  );
}
