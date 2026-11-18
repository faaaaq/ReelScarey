import { useEffect, useState, type MouseEvent } from "react";
import { Button } from "@radix-ui/themes";
import { Trash2 } from "lucide-react";
import { api, messageFor } from "../api";
import { Alert, Modal } from "../ui";
import { useLiveResource } from "../useLiveResource";

type Selection =
  "titles" | "video_files" | "artists" | "albums" | "tracks" | "music_files";
type DeletionPlan = {
  id: string;
  state: string;
  message: string;
  removal: { paths: string[] };
};

export function selectionClick<T extends HTMLElement>(toggle: () => void) {
  return (event: MouseEvent<T>) => {
    if (
      event.target instanceof HTMLInputElement &&
      event.target.type === "checkbox"
    )
      return;
    event.preventDefault();
    event.stopPropagation();
    toggle();
  };
}

export function LibraryDeletion({
  selection,
  ids,
  label,
  name,
  onDone,
}: {
  selection: Selection;
  ids: number[];
  label: string;
  name?: string;
  onDone: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [keep, setKeep] = useState(false);
  const [review, setReview] = useState<DeletionPlan | null>(null);
  const [jobID, setJobID] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const status = useLiveResource<DeletionPlan>(
    jobID ? `/organization/plans/${jobID}` : null,
    1500,
  );
  const state = status.data?.state;
  const running =
    !!jobID &&
    state !== "completed" &&
    state !== "failed" &&
    state !== "cancelled";

  useEffect(() => {
    if (state !== "completed") return;
    setOpen(false);
    setJobID("");
    onDone();
  }, [state, onDone]);

  function changeOpen(value: boolean) {
    if (busy) return;
    setOpen(value);
    if (running) return;
    setReview(null);
    setJobID("");
    setError("");
    setKeep(false);
  }

  async function submit() {
    setBusy(true);
    setError("");
    try {
      if (review) {
        await api(`/organization/plans/${review.id}/apply`, {
          method: "POST",
          body: "{}",
          signal: AbortSignal.timeout(300000),
        });
        setJobID(review.id);
        status.reload();
      } else {
        setReview(
          await api<DeletionPlan>("/library/deletion", {
            method: "POST",
            body: JSON.stringify({
              selection,
              ids,
              mode: keep ? "keep" : "delete",
            }),
            signal: AbortSignal.timeout(300000),
          }),
        );
      }
    } catch (e) {
      const timedOut =
        typeof e === "object" &&
        e !== null &&
        "name" in e &&
        e.name === "TimeoutError";
      setError(
        !review && timedOut
          ? "The deletion review timed out. No files were deleted. Try again after active media work finishes."
          : messageFor(e),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={changeOpen}
      title={name ? `Delete ${name}?` : label}
      trigger={
        <Button size="1" color="red" variant="soft" disabled={!ids.length}>
          <Trash2 size={14} aria-hidden="true" />
          {label}
        </Button>
      }
      footer={
        <>
          <Button
            variant="soft"
            color="gray"
            disabled={busy}
            onClick={() => changeOpen(false)}
          >
            {jobID ? "Close" : "Cancel"}
          </Button>
          {(!jobID || state === "failed") && (
            <Button color="red" disabled={busy} onClick={() => void submit()}>
              {busy
                ? "Working…"
                : state === "failed"
                  ? "Resume deletion"
                  : review
                    ? "Delete"
                    : "Review deletion"}
            </Button>
          )}
        </>
      }
    >
      <p>
        {ids.length === 1
          ? "The selected entry will be removed from your library."
          : `${ids.length} selected entries will be removed from your library.`}
      </p>
      {selection === "video_files" && (
        <p>All library references to this file will be removed.</p>
      )}
      {!review && (
        <label className="lib-select-page">
          <input
            type="checkbox"
            checked={keep}
            disabled={busy}
            onChange={(event) => setKeep(event.target.checked)}
          />
          Keep files on disk
        </label>
      )}
      <p>
        {keep
          ? "The library registrations will be removed and the files will remain on disk."
          : "Registered files and their recognized subtitle files will be permanently deleted. This cannot be undone."}
      </p>
      {review && (
        <>
          <p>{review.removal.paths.length} file paths in this review.</p>
          {review.removal.paths.length > 0 && (
            <details>
              <summary>Show file paths</summary>
              <ul>
                {review.removal.paths.map((path) => (
                  <li key={path}>
                    <code>{path}</code>
                  </li>
                ))}
              </ul>
            </details>
          )}
        </>
      )}
      {running && <p role="status">Deleting…</p>}
      {(error ||
        status.error ||
        state === "failed" ||
        state === "cancelled") && (
        <Alert kind="danger" role="alert">
          {error ||
            status.error ||
            status.data?.message ||
            "Deletion stopped. Review the plan in Organization."}
        </Alert>
      )}
    </Modal>
  );
}
