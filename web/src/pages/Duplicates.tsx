import { Files } from "lucide-react";
import type { DuplicateGroup, DuplicateReport } from "../api";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  EmptyState,
  FieldSet,
  Label,
  PageBody,
  Table,
  episodeCode,
  formatBytes,
} from "../ui";
import { useResource } from "../useResource";
import "./Admin.css";

function groupTitle(group: DuplicateGroup) {
  if (group.kind === "episode") {
    const code =
      group.season !== undefined && group.episode !== undefined
        ? ` · ${episodeCode(group.season, group.episode)}`
        : "";
    return `${group.title}${code}${
      group.episode_title ? ` · ${group.episode_title}` : ""
    }`;
  }
  return group.title;
}

function statusLabel(group: DuplicateGroup) {
  return group.status === "probable_duplicate"
    ? "Probable duplicate"
    : "Needs attention";
}

function resolution(value: number) {
  return value > 0 ? `${value}p` : "Unknown";
}

export function Duplicates() {
  const resource = useResource<DuplicateReport>("/duplicates?limit=100");
  const groups = resource.data?.groups ?? [];
  return (
    <PageBody>
      <PageHeading
        title="Duplicate review"
        description="Read-only review of library files that may be duplicates. Probable duplicates match size, SHA-256 and inspected resolution. Needs attention groups name the first check that failed, so different files are never silently treated as copies."
      />
      <RequestState
        loading={resource.loading}
        error={resource.error}
        code={resource.code}
        retry={resource.reload}
      />
      {resource.data && (
        <>
          <p className="rs-muted rs-small">
            {resource.data.total === 1
              ? "1 group found."
              : `${resource.data.total} groups found.`}{" "}
            Reviewing never changes, moves or deletes files.
          </p>
          {resource.data.truncated && (
            <Alert kind="warning" title="Review is truncated">
              Showing {groups.length} of {resource.data.total} groups because
              the review is limited to {resource.data.limit}. Resolve or rename
              files outside ReelScarey, then reload this page.
            </Alert>
          )}
          {!groups.length && (
            <EmptyState icon={Files} title="No duplicate groups">
              No library files currently share enough evidence to review. This
              page compares registered files only; it never scans disks or
              changes the library.
            </EmptyState>
          )}
          {groups.map((group) => (
            <FieldSet
              key={`${group.kind}-${group.media_id}-${group.episode_id ?? "all"}-${group.files[0]?.id ?? 0}`}
              legend={groupTitle(group)}
              className="dup-group"
              actions={
                <Label
                  kind={
                    group.status === "probable_duplicate" ? "info" : "warning"
                  }
                >
                  {statusLabel(group)}
                </Label>
              }
              description={
                <>
                  {group.reason}. {group.file_count}{" "}
                  {group.file_count === 1 ? "file" : "files"} in this group.
                </>
              }
            >
              <Table label={`Files for ${groupTitle(group)}`}>
                <thead>
                  <tr>
                    <th scope="col">Path</th>
                    <th scope="col">Size</th>
                    <th scope="col">Resolution</th>
                    <th scope="col">SHA-256</th>
                  </tr>
                </thead>
                <tbody>
                  {group.files.map((file) => (
                    <tr key={file.id}>
                      <td className="dup-path">{file.relative_path}</td>
                      <td className="dup-size">
                        {formatBytes(file.size_bytes)}
                      </td>
                      <td>{resolution(file.resolution)}</td>
                      <td className="dup-sha">
                        {file.sha256 || "Not recorded"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </FieldSet>
          ))}
        </>
      )}
    </PageBody>
  );
}
