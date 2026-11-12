import { useMemo } from "react";
import { IconButton } from "@radix-ui/themes";
import { Download, Search } from "lucide-react";
import { Link } from "react-router-dom";
import { type Release } from "../../api";
import {
  EmptyState,
  Label,
  SortHeader,
  Table,
  releaseBytes as bytes,
  type Kind,
  type SortState,
} from "../../ui";
import "../Releases.css";

export type ReleaseSortKey =
  "rank" | "age" | "title" | "indexer" | "size" | "peers";

/**
 * Release dates arrive as RFC3339, RFC1123Z or Go's default time layout
 * depending on the indexer. Parse each explicitly so an unreadable date stays
 * an honest em dash instead of an empty cell.
 */
export function publishedTime(release: Release) {
  const raw =
    typeof release.published_at === "string" ? release.published_at.trim() : "";
  if (!raw) return null;
  if (/^\d{9,13}$/.test(raw)) {
    const epoch = Number(raw);
    return epoch > 0 ? (raw.length <= 10 ? epoch * 1000 : epoch) : null;
  }
  const go = raw.match(
    /^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s*([+-]\d{2}:?\d{2}|Z|UTC|GMT)?/,
  );
  let value = NaN;
  if (go) {
    const zone = go[3] ?? "Z";
    const normalisedZone = zone === "UTC" || zone === "GMT" ? "Z" : zone;
    value = Date.parse(`${go[1]}T${go[2]}${normalisedZone}`);
  }
  if (!Number.isFinite(value)) value = Date.parse(raw);
  return Number.isFinite(value) && value > 0 ? value : null;
}

export function age(release: Release) {
  const time = publishedTime(release);
  if (time === null) return "—";
  const minutes = Math.max(0, Math.round((Date.now() - time) / 60000));
  if (minutes < 60) return `${minutes} ${minutes === 1 ? "minute" : "minutes"}`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} ${hours === 1 ? "hour" : "hours"}`;
  const days = Math.round(hours / 24);
  return `${days} ${days === 1 ? "day" : "days"}`;
}

/** Only trust an absolute web URL from the tracker; never invent one. */
export function releaseInfoURL(release: Release) {
  const value = release.info_url?.trim();
  if (!value) return null;
  try {
    const url = new URL(value);
    if (url.username || url.password) return null;
    return url.protocol === "http:" || url.protocol === "https:"
      ? url.toString()
      : null;
  } catch {
    return null;
  }
}

export function normaliseTitle(value: string) {
  return value.trim().toLocaleLowerCase();
}

function seedKind(seeders: number | null): Kind {
  if (seeders === null || seeders === undefined) return "default";
  if (seeders === 0) return "danger";
  if (seeders < 5) return "warning";
  return "success";
}

/**
 * The release results table shared by the /releases page and the in-page
 * release search modal. The caller owns the search, the download action and
 * which releases are download-ready; this component only renders, filters
 * and sorts what it is given.
 */
export function ReleaseResultsTable({
  items,
  protocol,
  sort,
  onSort,
  pending,
  onDownload,
  readyFor,
  failedTitles,
  tv,
}: {
  items: Release[];
  protocol: string;
  sort: SortState<ReleaseSortKey>;
  onSort: (sort: SortState<ReleaseSortKey>) => void;
  pending: string;
  onDownload: (id: string) => void;
  readyFor: (release: Release) => boolean | undefined;
  failedTitles: Set<string>;
  tv: boolean;
}) {
  const rows = useMemo(() => {
    const list = items.filter(
      (r) =>
        protocol === "all" ||
        (protocol === "usenet"
          ? r.protocol === "usenet"
          : r.protocol !== "usenet"),
    );
    if (sort.key === "rank") return list;
    const value = (r: Release): number | string => {
      switch (sort.key) {
        case "age":
          return -(publishedTime(r) ?? -Infinity);
        case "title":
          return r.title.toLowerCase();
        case "indexer":
          return (r.indexer ?? "").toLowerCase();
        case "size":
          return r.size_bytes;
        default:
          return r.seeders ?? -1;
      }
    };
    const sign = sort.direction === "asc" ? 1 : -1;
    return [...list].sort((a, b) => {
      const x = value(a),
        y = value(b);
      return (x < y ? -1 : x > y ? 1 : 0) * sign;
    });
  }, [items, protocol, sort]);
  if (items.length === 0)
    return (
      <EmptyState icon={Search} title="No supported releases found">
        <p>
          Try another title or check the indexer’s {tv ? "TV" : "movie"}{" "}
          categories and API endpoint.
        </p>
      </EmptyState>
    );
  return (
    <>
      <p className="rs-muted rs-small">
        {items.length} {items.length === 1 ? "release" : "releases"} from your
        enabled indexers
        {rows.length !== items.length
          ? ` · ${rows.length} shown by the protocol filter`
          : ""}
        .{" "}
        {tv &&
          "Labeled single episodes and packs import automatically; only unmatched or ambiguous files need mapping review."}
      </p>
      <Table label="Releases" className="rel-table">
        <thead>
          <tr>
            <th scope="col" className="is-shrink">
              Source
            </th>
            <SortHeader
              column="age"
              label="Age"
              sort={sort}
              onSort={onSort}
              className="is-shrink"
            />
            <SortHeader
              column="title"
              label="Title"
              sort={sort}
              onSort={onSort}
            />
            <SortHeader
              column="indexer"
              label="Indexer"
              sort={sort}
              onSort={onSort}
              className="rel-col-indexer"
            />
            <SortHeader
              column="size"
              label="Size"
              sort={sort}
              onSort={onSort}
              className="is-numeric"
            />
            <SortHeader
              column="peers"
              label="Peers"
              sort={sort}
              onSort={onSort}
              className="is-shrink"
            />
            <th scope="col" className="is-actions">
              <span className="sr-only">Download</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((release) => {
            const ready = readyFor(release);
            const usenet = release.protocol === "usenet";
            const published = publishedTime(release);
            const info = releaseInfoURL(release);
            const failedBefore = failedTitles.has(
              normaliseTitle(release.title),
            );
            return (
              <tr key={release.id}>
                <td>
                  <Label kind={usenet ? "purple" : "info"} outline>
                    {usenet ? "nzb" : "torrent"}
                  </Label>
                </td>
                <td
                  className="rel-age"
                  title={
                    published !== null
                      ? `Published ${new Date(published).toLocaleString()}`
                      : "Publication date unknown"
                  }
                >
                  {age(release)}
                </td>
                <td className="rel-title">
                  <span className="rel-title-text">
                    {info ? (
                      <a href={info} target="_blank" rel="noreferrer">
                        {release.title}
                      </a>
                    ) : (
                      release.title
                    )}
                  </span>
                  {release.pack && <Label kind="info">Pack</Label>}
                  {release.freeleech && <Label kind="success">Freeleech</Label>}
                  {failedBefore && (
                    <Label
                      kind="danger"
                      title="A previous download with this release title failed."
                    >
                      Failed before
                    </Label>
                  )}
                </td>
                <td className="rel-col-indexer">
                  {release.indexer ?? "Torznab"}
                </td>
                <td className="is-numeric">{bytes(release.size_bytes)}</td>
                <td className="is-numeric">
                  {usenet ? (
                    <span className="rs-muted">—</span>
                  ) : (
                    <span
                      title={`Seeders: ${release.seeders ?? "unknown"} · Peers: ${release.peers ?? "unknown"}`}
                    >
                      <Label kind={seedKind(release.seeders)}>
                        {release.seeders ?? "?"} / {release.peers ?? "?"}
                      </Label>
                    </span>
                  )}
                </td>
                <td className="is-actions">
                  <div className="rel-actions">
                    {!ready && (
                      <Link
                        className="rs-small"
                        to={usenet ? "/settings#sabnzbd" : "/download-clients"}
                      >
                        {usenet
                          ? "Connect SABnzbd"
                          : "Set up a download client"}
                      </Link>
                    )}
                    <IconButton
                      size="1"
                      variant="soft"
                      aria-label="Download"
                      title={
                        ready
                          ? "Send to download client"
                          : usenet
                            ? "Connect SABnzbd first"
                            : "Set up a download client first"
                      }
                      loading={pending === release.id}
                      disabled={!!pending || !ready}
                      onClick={() => onDownload(release.id)}
                    >
                      <Download size={15} aria-hidden="true" />
                    </IconButton>
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </Table>
    </>
  );
}
