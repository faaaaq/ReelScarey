import { PageBody, pageParam } from "../ui";
import { Button, Badge } from "@radix-ui/themes";
import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import "./Admin.css";

type Listing = {
  roots: { id: string; path: string }[];
  root: string;
  path: string;
  page: number;
  total: number;
  limit: number;
  items: {
    name: string;
    path: string;
    kind: string;
    size_bytes: number;
    modified_at: number;
  }[];
};
type Migration = {
  state: string;
  digest: string;
  source: string;
  destination: string;
  files: number;
  directories: number;
  bytes: number;
  backup: string;
  temporary_copy_folder: string;
};
const size = (bytes: number) =>
  bytes < 1024
    ? `${bytes} B`
    : bytes < 1024 ** 2
      ? `${(bytes / 1024).toFixed(1)} KiB`
      : bytes < 1024 ** 3
        ? `${(bytes / 1024 ** 2).toFixed(1)} MiB`
        : `${(bytes / 1024 ** 3).toFixed(1)} GiB`;
export function Storage() {
  const [search, setSearch] = useSearchParams();
  const root = search.get("root") ?? "media",
    path = search.get("path") ?? "",
    page = pageParam(search.get("page"));
  const listing = useResource<Listing>(
    `/system/storage?root=${encodeURIComponent(root)}&path=${encodeURIComponent(path)}&page=${page}`,
  );
  const migration = useResource<Migration>("/system/storage/migration");
  const [roots, setRoots] = useState<Listing["roots"]>([
    { id: "media", path: "/media" },
    { id: "downloads", path: "/downloads" },
  ]);
  useEffect(() => {
    if (listing.data) setRoots(listing.data.roots);
  }, [listing.data]);
  const navigate = (nextRoot: string, nextPath = "", nextPage = 1) =>
    setSearch({
      root: nextRoot,
      ...(nextPath ? { path: nextPath } : {}),
      ...(nextPage > 1 ? { page: String(nextPage) } : {}),
    });
  return (
    <PageBody>
      <PageHeading title="Storage" />
      <section className="rs-panel rs-stack">
        <h2>Folder browser</h2>
        <p>
          Read-only. To match files in a collection, open{" "}
          <Link to="/import">Import library</Link>.
        </p>
        <label>
          Mounted folder{" "}
          <select value={root} onChange={(e) => navigate(e.target.value)}>
            {!roots.some((v) => v.id === root) && (
              <option value={root}>{root} · unavailable</option>
            )}
            {roots.map((v) => (
              <option key={v.id} value={v.id}>
                {v.id} · {v.path}
              </option>
            ))}
          </select>
        </label>
        <RequestState
          loading={listing.loading}
          error={listing.error}
          code={listing.code}
          retry={listing.reload}
        />
        {listing.data && (
          <>
            <nav aria-label="Folder path" className="storage-breadcrumbs">
              <Button variant="soft" onClick={() => navigate(root)}>
                Root
              </Button>
              {path
                .split("/")
                .filter(Boolean)
                .map((part, index) => (
                  <Button
                    key={index}
                    variant="soft"
                    onClick={() =>
                      navigate(
                        root,
                        path
                          .split("/")
                          .slice(0, index + 1)
                          .join("/"),
                      )
                    }
                  >
                    {part}
                  </Button>
                ))}
            </nav>
            <p role="status">
              {listing.data.total} entries · {path || "/"}
            </p>
            <div className="rs-table-wrap">
              <table className="rs-table">
                <caption>
                  Folders and files in the selected mounted folder
                </caption>
                <thead>
                  <tr>
                    <th scope="col">Name</th>
                    <th scope="col">Type</th>
                    <th scope="col">Size</th>
                    <th scope="col">Modified</th>
                  </tr>
                </thead>
                <tbody>
                  {listing.data.items.map((v) => (
                    <tr key={v.path}>
                      <td>
                        {v.kind === "directory" ? (
                          <Button
                            variant="ghost"
                            aria-label={`Open folder ${v.name}`}
                            onClick={() => navigate(root, v.path)}
                          >
                            {v.name}
                          </Button>
                        ) : (
                          v.name
                        )}
                      </td>
                      <td>
                        {v.kind === "blocked" ? "Link or special file" : v.kind}
                      </td>
                      <td>{v.kind === "file" ? size(v.size_bytes) : "—"}</td>
                      <td>
                        {v.modified_at
                          ? new Date(v.modified_at * 1000).toLocaleDateString()
                          : "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {listing.data.total === 0 && <p>This folder is empty.</p>}
            <div className="storage-breadcrumbs">
              <Button
                variant="soft"
                disabled={page <= 1}
                onClick={() => navigate(root, path, page - 1)}
              >
                Previous page
              </Button>
              <span>
                Page {page} of{" "}
                {Math.max(
                  1,
                  Math.ceil(listing.data.total / listing.data.limit),
                )}
              </span>
              <Button
                variant="soft"
                disabled={page * listing.data.limit >= listing.data.total}
                onClick={() => navigate(root, path, page + 1)}
              >
                Next page
              </Button>
              <Button variant="soft" onClick={listing.reload}>
                Refresh folders
              </Button>
            </div>
          </>
        )}
        <details>
          <summary>Browse another disk or share</summary>
          <p>
            Mount its folder into the container read-only, then set{" "}
            <code>REELSCAREY_BROWSE_ROOTS</code> to a JSON object such as{" "}
            <code>{'{"archive":"/archive"}'}</code>. Restart the container to
            add it to this list. Only explicitly mounted folders are available;
            hidden files and private configuration/backup folders are excluded.
            Each folder is limited to 10,000 entries.
          </p>
        </details>
      </section>
      <section className="rs-panel rs-stack">
        <h2>Move the main library to another disk</h2>
        <p>
          The offline migration tool reviews the complete tree, creates a
          verified configuration backup and copies into a new folder. It
          preserves original media, subtitles, extras, retained versions and
          empty folders. The original disk stays available for rollback.
        </p>
        <RequestState
          loading={migration.loading}
          error={migration.error}
          code={migration.code}
          retry={migration.reload}
        />
        {migration.data && migration.data.state !== "none" && (
          <>
            <Badge>{migration.data.state}</Badge>
            <dl>
              <dt>Original folder</dt>
              <dd>{migration.data.source}</dd>
              <dt>Destination</dt>
              <dd>{migration.data.destination}</dd>
              <dt>Reviewed contents</dt>
              <dd>
                {migration.data.files} files · {migration.data.directories}{" "}
                folders · {size(migration.data.bytes)}
              </dd>
              <dt>Review digest</dt>
              <dd className="storage-digest">{migration.data.digest}</dd>
              {migration.data.backup && (
                <>
                  <dt>Configuration backup</dt>
                  <dd>{migration.data.backup}</dd>
                </>
              )}
            </dl>
          </>
        )}
        <ol>
          <li>
            Stop the app and mount the new disk's existing parent into a
            temporary maintenance container.
          </li>
          <li>
            Run <code>media-root-plan NEW_FOLDER</code>. Review its source,
            destination, counts and digest. The destination must be a new
            folder, separate from other app storage.
          </li>
          <li>
            Run <code>media-root-copy DIGEST</code> with the reviewed digest.
            The tool backs up configuration, copies and verifies every file.
            Retrying the same command resumes a stopped copy. Startup stays
            blocked while a migration is pending.
          </li>
          <li>
            Mount the copied folder at the same original container path,
            normally <code>/media</code>. Run{" "}
            <code>media-root-activate DIGEST</code>, then start the app. Review{" "}
            <Link to="/system">System checks</Link> before resuming workers.
          </li>
        </ol>
        <p>
          These commands run through{" "}
          <code>docker compose run --rm --no-deps reelscarey</code> while the
          app is stopped. Use <code>media-root-cancel DIGEST</code> with the
          original disk mounted to cancel and retain both copies. Review unknown
          files in the private temporary copy folder before deleting them.
        </p>
        <p>
          Copying supports up to 100,000 regular files/folders per review and
          refuses links, special files and case-colliding paths. Keep
          independent media backups and leave the original disk intact until you
          have checked the new mount.
        </p>
      </section>
    </PageBody>
  );
}
