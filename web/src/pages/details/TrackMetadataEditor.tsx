import { useEffect, useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor } from "../../api";
import { Alert, Check, Label, Modal } from "../../ui";
import { useLiveResource } from "../../useLiveResource";
import type { DetailFile } from "./useItemDetails";

type TrackRow = {
  index: number;
  kind: "audio" | "subtitles";
  codec?: string;
  hearingImpaired?: boolean;
  title: string;
  language: string;
  default: boolean;
  forced: boolean;
  initial: {
    title: string;
    language: string;
    default: boolean;
    forced: boolean;
  };
};

type SubmittedEdit = {
  index: number;
  title?: string;
  language?: string;
  default?: boolean;
  forced?: boolean;
};

type TrackPlan = {
  id: string;
  state: string;
  message: string;
  replacement: {
    track_edits: SubmittedEdit[];
    before: { path: string };
  };
};

function rowsFor(file: DetailFile): TrackRow[] {
  const audio = (file.probe.audio_streams ?? []).map((stream) => ({
    index: stream.index ?? 0,
    kind: "audio" as const,
    codec: stream.codec,
    title: stream.title ?? "",
    language: stream.language || "und",
    default: !!stream.default,
    forced: !!stream.forced,
  }));
  const subtitles = (file.probe.subtitle_streams ?? []).map((stream) => ({
    index: stream.index ?? 0,
    kind: "subtitles" as const,
    hearingImpaired: !!stream.hearing_impaired,
    title: stream.title ?? "",
    language: stream.language || "und",
    default: !!stream.default,
    forced: !!stream.forced,
  }));
  return [...audio, ...subtitles].map((row) => ({
    ...row,
    initial: {
      title: row.title,
      language: row.language,
      default: row.default,
      forced: row.forced,
    },
  }));
}

function changes(rows: TrackRow[]): SubmittedEdit[] {
  return rows.flatMap((row) => {
    const edit: SubmittedEdit = { index: row.index };
    if (row.title !== row.initial.title) edit.title = row.title;
    if (row.language !== row.initial.language) edit.language = row.language;
    if (row.default !== row.initial.default) edit.default = row.default;
    if (row.forced !== row.initial.forced) edit.forced = row.forced;
    return Object.keys(edit).length > 1 ? [edit] : [];
  });
}

export function TrackMetadataEditor({
  file,
  onDone,
}: {
  file: DetailFile;
  onDone: () => void;
}) {
  const supported = /\.(mkv|webm)$/i.test(file.path);
  const initialRows = rowsFor(file);
  const inspected =
    initialRows.length > 0 && initialRows.every((row) => row.index > 0);
  const [open, setOpen] = useState(false);
  const [rows, setRows] = useState<TrackRow[]>(initialRows);
  const [review, setReview] = useState<TrackPlan>();
  const [jobID, setJobID] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const status = useLiveResource<TrackPlan>(
    jobID ? `/organization/plans/${jobID}` : null,
    1500,
  );
  const running =
    !!jobID &&
    !["completed", "failed", "cancelled"].includes(status.data?.state ?? "");

  useEffect(() => {
    if (status.data?.state !== "completed") return;
    setOpen(false);
    setJobID("");
    setReview(undefined);
    onDone();
  }, [status.data?.state, onDone]);

  if (!supported) return null;

  function openEditor() {
    setRows(rowsFor(file));
    setReview(undefined);
    setError("");
    setOpen(true);
  }

  function update<K extends keyof TrackRow>(
    index: number,
    key: K,
    value: TrackRow[K],
  ) {
    setRows((current) =>
      current.map((row) =>
        row.index === index ? { ...row, [key]: value } : row,
      ),
    );
    setReview(undefined);
  }

  async function preview() {
    const edits = changes(rows);
    if (edits.length === 0) {
      setError("Change at least one track field before reviewing.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      setReview(
        await api<TrackPlan>("/organization/track-metadata", {
          method: "POST",
          body: JSON.stringify({ media_file_id: file.id, edits }),
          signal: AbortSignal.timeout(60000),
        }),
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function apply() {
    if (!review || busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`/organization/plans/${review.id}/apply`, {
        method: "POST",
        body: "{}",
        signal: AbortSignal.timeout(300000),
      });
      setJobID(review.id);
      status.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  const pendingChanges = changes(rows);
  return (
    <Modal
      open={open}
      onOpenChange={(next) => {
        if (!busy && !running) setOpen(next);
      }}
      title="Edit Matroska track fields"
      description={`Target file: ${file.path}. Only audio and subtitle header fields change; unrelated metadata and streams are re-read and verified.`}
      trigger={
        <Button
          size="1"
          variant="soft"
          disabled={!inspected}
          title={
            inspected
              ? "Edit track titles, languages, default and forced flags"
              : "Inspect embedded tags first so stream indexes and current flags are verified"
          }
          onClick={openEditor}
        >
          Edit track fields
        </Button>
      }
      footer={
        <>
          <Button
            variant="soft"
            color="gray"
            disabled={busy || running}
            onClick={() => setOpen(false)}
          >
            Cancel
          </Button>
          {review && !jobID && (
            <Button
              variant="soft"
              disabled={busy}
              onClick={() => setReview(undefined)}
            >
              Back to fields
            </Button>
          )}
          {!jobID && (
            <Button
              disabled={busy || (!review && pendingChanges.length === 0)}
              onClick={() => void (review ? apply() : preview())}
            >
              {busy
                ? "Working…"
                : review
                  ? "Confirm & edit tracks"
                  : "Review track changes"}
            </Button>
          )}
        </>
      }
    >
      {!inspected ? (
        <Alert kind="warning">
          Inspect embedded tags first. Older inspection records do not contain
          the stream indexes needed for a safe edit.
        </Alert>
      ) : review ? (
        <div className="rs-stack">
          <Alert kind="warning" title="Review the exact disk write">
            This edits the registered file’s Matroska header in place. It does
            not rename or move the file. Seeding hardlinks are refused; the
            server reports the independent-copy space required.
          </Alert>
          <code>{review.replacement.before.path}</code>
          <ul aria-label="Reviewed track field changes">
            {review.replacement.track_edits.map((edit) => (
              <li key={edit.index}>
                Stream {edit.index}:{" "}
                {Object.entries(edit)
                  .filter(([key]) => key !== "index")
                  .map(([key, value]) => `${key}=${String(value)}`)
                  .join(", ")}
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <div className="rs-stack">
          <p className="rs-form-help">
            Audio and subtitles are shown together. Use <code>und</code> for an
            unknown language. Full subtitles, signs/songs, and other roles stay
            in the track title; SDH and forced flags remain separate facts.
          </p>
          {rows.map((row) => (
            <fieldset key={`${row.kind}-${row.index}`} className="rs-stack">
              <legend>
                {row.kind === "audio" ? "Audio" : "Subtitle"} stream {row.index}
              </legend>
              <div>
                <Label outline>{row.kind}</Label>{" "}
                <Label outline>{row.codec || "codec unknown"}</Label>{" "}
                {row.hearingImpaired && (
                  <Label outline>SDH / hearing impaired</Label>
                )}
              </div>
              <label>
                Track title
                <TextField.Root
                  aria-label={`Title for ${row.kind} stream ${row.index}`}
                  value={row.title}
                  maxLength={1024}
                  onChange={(event) =>
                    update(row.index, "title", event.target.value)
                  }
                  placeholder="Unknown or untitled"
                />
              </label>
              <label>
                Language
                <TextField.Root
                  aria-label={`Language for ${row.kind} stream ${row.index}`}
                  value={row.language}
                  maxLength={64}
                  onChange={(event) =>
                    update(row.index, "language", event.target.value)
                  }
                />
              </label>
              <Check
                label={`Default ${row.kind} stream ${row.index}`}
                checked={row.default}
                onChange={(value) => update(row.index, "default", value)}
              />
              <Check
                label={`Forced ${row.kind} stream ${row.index}`}
                checked={row.forced}
                onChange={(value) => update(row.index, "forced", value)}
              />
            </fieldset>
          ))}
        </div>
      )}
      {jobID && (
        <p role="status">
          {status.data?.state === "failed"
            ? status.data.message ||
              "The edit failed; review the retained file and recovery state."
            : "Editing and verifying track fields…"}
        </p>
      )}
      {error && <Alert kind="danger">{error}</Alert>}
    </Modal>
  );
}
