import { Link } from "react-router-dom";
import { PageHeading, RequestState } from "../../components";
import { PageBody } from "../../ui";
import { useResource } from "../../useResource";
import { bytes } from "./status";
import type { CustomFile } from "./types";
import "../Admin.css";

/** Completed YouTube media in custom collections. TV and movie imports stay
 * on their show and movie pages; this list never claims to be complete. */
export function LibraryFiles() {
  const files = useResource<{ items: CustomFile[] }>("/youtube/files");
  return (
    <PageBody>
      <PageHeading
        title="YouTube library"
        description="Imported videos from custom collections."
      />
      <p className="rs-form-help">
        This list covers custom collections only. TV episodes and movies
        downloaded from YouTube appear on their show or movie page.
      </p>
      <RequestState
        loading={files.loading}
        error={files.error}
        retry={files.reload}
      />
      {(files.data?.items.length ?? 0) === 0 &&
        !files.loading &&
        !files.error && <p className="yt-empty">No imported videos.</p>}
      <ul>
        {files.data?.items.map((file) => (
          <li key={file.job_id}>
            <strong>{file.title}</strong>
            <span>
              {file.collection} · {file.relative_path} ·{" "}
              {bytes(file.size_bytes)}
            </span>
          </li>
        ))}
      </ul>
      <p className="rs-form-help">
        Downloads in progress are on the{" "}
        <Link to="/youtube/downloads">downloads page</Link>.
      </p>
    </PageBody>
  );
}
