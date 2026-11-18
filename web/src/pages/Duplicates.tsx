import { Files } from "lucide-react";
import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import type { DuplicateGroup, DuplicateReport } from "../api";
import { PageHeading, RequestState } from "../components";
import { LibraryDeletion } from "../components/LibraryDeletion";
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
import { useLiveResource } from "../useLiveResource";
import "./Admin.css";

function groupKey(group: DuplicateGroup) {
  return `${group.kind}-${group.media_id}-${group.episode_id ?? 0}`;
}

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
  const [showDismissed, setShowDismissed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const resource = useLiveResource<DuplicateReport>(
    `/duplicates?limit=100${showDismissed ? "&include_dismissed=1" : ""}`,
    15000,
  );
  const groups = resource.data?.groups ?? [];

  async function changeDismissal(group: DuplicateGroup, dismiss: boolean) {
    setBusy(true);
    setError("");
    try {
      await api(
        dismiss ? "/duplicates/dismiss" : "/duplicates/dismiss/restore",
        {
          method: "POST",
          body: JSON.stringify({
            kind: group.kind,
            media_id: group.media_id,
            ...(group.episode_id ? { episode_id: group.episode_id } : {}),
          }),
        },
      );
      resource.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <PageBody>
      <PageHeading
        title="Duplicate review"
        description="Review of library files that may be duplicates. Probable duplicates match size, SHA-256 and inspected resolution. Needs attention groups name the first check that failed, so different files are never silently treated as copies. Dismiss a group you reviewed and want to keep, or delete selected files through a reviewed deletion."
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
            Dismissing only changes this page; deleting selected files uses a
            reviewed deletion that names every path first.
          </p>
          <label className="rs-small">
            <input
              type="checkbox"
              checked={showDismissed}
              onChange={(event) => setShowDismissed(event.target.checked)}
            />{" "}
            Show dismissed groups
          </label>
          {resource.data.truncated && (
            <Alert kind="warning" title="Review is truncated">
              Showing {groups.length} of {resource.data.total} groups because
              the review is limited to {resource.data.limit}. Resolve or rename
              files outside ReelScarey, then reload this page.
            </Alert>
          )}
          {error && <Alert kind="danger">{error}</Alert>}
          {!groups.length && (
            <EmptyState icon={Files} title="No duplicate groups">
              No library files currently share enough evidence to review. This
              page compares registered files only; it never scans disks.
            </EmptyState>
          )}
          {groups.map((group) => (
            <DuplicateGroupCard
              key={groupKey(group)}
              group={group}
              busy={busy}
              onDismiss={() => void changeDismissal(group, true)}
              onRestore={() => void changeDismissal(group, false)}
              onChanged={() => resource.reload()}
            />
          ))}
        </>
      )}
    </PageBody>
  );
}

function DuplicateGroupCard({
  group,
  busy,
  onDismiss,
  onRestore,
  onChanged,
}: {
  group: DuplicateGroup;
  busy: boolean;
  onDismiss: () => void;
  onRestore: () => void;
  onChanged: () => void;
}) {
  const [selected, setSelected] = useState<number[]>([]);
  const paths = group.files.map((file) => file.relative_path);
  const allSelected =
    group.files.length > 0 && selected.length === group.files.length;
  function toggle(id: number) {
    setSelected((current) =>
      current.includes(id)
        ? current.filter((value) => value !== id)
        : [...current, id],
    );
  }
  return (
    <FieldSet
      legend={groupTitle(group)}
      className="dup-group"
      actions={
        group.dismissed ? (
          <>
            <Label kind="info">Dismissed</Label>
            <Button size="1" variant="soft" disabled={busy} onClick={onRestore}>
              Review again
            </Button>
          </>
        ) : (
          <>
            <Label
              kind={group.status === "probable_duplicate" ? "info" : "warning"}
            >
              {statusLabel(group)}
            </Label>
            <Button size="1" variant="soft" disabled={busy} onClick={onDismiss}>
              Dismiss
            </Button>
          </>
        )
      }
      description={
        <>
          {group.reason}. {group.file_count}{" "}
          {group.file_count === 1 ? "file" : "files"} in this group.
        </>
      }
    >
      {!group.dismissed && (
        <Table label={`Files for ${groupTitle(group)}`}>
          <thead>
            <tr>
              <th scope="col" className="is-actions">
                <span className="rs-small">
                  <label>
                    <input
                      type="checkbox"
                      checked={allSelected}
                      aria-label="Select every file in this group"
                      onChange={(event) =>
                        setSelected(
                          event.target.checked
                            ? group.files.map((file) => file.id)
                            : [],
                        )
                      }
                    />{" "}
                    Select
                  </label>
                </span>
              </th>
              <th scope="col">Path</th>
              <th scope="col">Size</th>
              <th scope="col">Resolution</th>
              <th scope="col">SHA-256</th>
            </tr>
          </thead>
          <tbody>
            {group.files.map((file) => (
              <tr key={file.id}>
                <td className="is-actions">
                  <input
                    type="checkbox"
                    aria-label={`Select ${file.relative_path}`}
                    checked={selected.includes(file.id)}
                    onChange={() => toggle(file.id)}
                  />
                </td>
                <td className="dup-path">{file.relative_path}</td>
                <td className="dup-size">{formatBytes(file.size_bytes)}</td>
                <td>{resolution(file.resolution)}</td>
                <td className="dup-sha">{file.sha256 || "Not recorded"}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {!group.dismissed && (
        <div className="rs-stack">
          <LibraryDeletion
            selection="video_files"
            ids={selected}
            label="Delete selected files"
            name={
              selected.length === 1
                ? paths[group.files.findIndex((f) => f.id === selected[0])]
                : undefined
            }
            onDone={() => {
              setSelected([]);
              onChanged();
            }}
          />
          {selected.length > 0 && (
            <p className="rs-muted rs-small">
              {selected.length} of {group.files.length} files selected. The
              reviewed deletion names every path before anything is removed.
            </p>
          )}
        </div>
      )}
    </FieldSet>
  );
}
