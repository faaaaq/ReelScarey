import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { useLiveResource } from "../useLiveResource";
import type { SubtitleTrack } from "./Subtitles";
type Track = SubtitleTrack & { path: string; source: string };
type Edit = SubtitleTrack & {
  id: string;
  media_file_id: number;
  action: string;
  offset_ms: number;
  old_sha256: string;
  new_sha256: string;
  destination: string;
  previous_path: string;
  size_bytes: number;
  cues: number;
  state: string;
  phase: string;
  message: string;
  expires_at: number;
  created_at: number;
};
export function SubtitleEdits({
  fileID,
  tracks,
  reload,
}: {
  fileID: number;
  tracks: Track[];
  reload: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <Button
        variant="soft"
        disabled={!tracks.some((t) => t.source !== "embedded" && !!t.path)}
        onClick={() => setOpen(!open)}
      >
        {open ? "Hide subtitle editing" : "Edit or replace a saved subtitle"}
      </Button>
      {open && (
        <Editor
          fileID={fileID}
          tracks={tracks.filter((t) => t.source !== "embedded" && !!t.path)}
          reload={reload}
        />
      )}
    </div>
  );
}
function Editor({
  fileID,
  tracks,
  reload,
}: {
  fileID: number;
  tracks: Track[];
  reload: () => void;
}) {
  const resource = useLiveResource<{ items: Edit[] }>(
      `/subtitles/files/${fileID}/edits`,
      2000,
    ),
    [selected, setSelected] = useState(0),
    [action, setAction] = useState("offset"),
    [offset, setOffset] = useState(1000),
    [content, setContent] = useState(""),
    [referenceFormat, setReferenceFormat] = useState(""),
    [review, setReview] = useState<Edit>(),
    [checked, setChecked] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const track = tracks[selected] ?? tracks[0];
  async function perform(task: () => Promise<void>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await task();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function preview(previous?: Edit) {
    if (!track) return;
    await perform(async () => {
      setReview(
        await api<Edit>(`/subtitles/files/${fileID}/edits`, {
          method: "POST",
          body: JSON.stringify({
            language: previous?.language ?? track.language,
            variant: previous?.variant ?? track.variant,
            action: previous ? "rollback" : action,
            offset_ms: previous ? 0 : action === "offset" ? offset : 0,
            content: previous
              ? ""
              : action === "replace" || action === "align"
                ? content
                : "",
            ...(!previous && action === "align"
              ? { reference_format: referenceFormat }
              : {}),
            previous_edit_id: previous?.id ?? "",
          }),
          signal: AbortSignal.timeout(120000),
        }),
      );
      setChecked(false);
    });
  }
  return (
    <section className="system-panel" aria-label="Subtitle editing">
      <h3>Subtitle replacement and timing</h3>
      <p>
        Supported UTF-8 SRT, VTT, ASS and SSA files up to 4 MiB. The previous
        version is retained privately inside the media root. Positive offsets
        delay subtitles; negative offsets show them earlier. Changes that would
        create negative cue times are refused.
      </p>
      <fieldset disabled={busy || !!review} className="backup-fields">
        <label>
          Saved track
          <select
            value={selected}
            onChange={(e) => setSelected(Number(e.target.value))}
          >
            {tracks.map((t, i) => (
              <option key={t.language + t.variant} value={i}>
                {t.language} · {t.variant} · {t.path}
              </option>
            ))}
          </select>
        </label>
        <label>
          Change
          <select
            value={action}
            onChange={(e) => {
              setAction(e.target.value);
              setContent("");
              setReferenceFormat("");
            }}
          >
            <option value="offset">Adjust timing</option>
            <option value="replace">Upload replacement</option>
            <option value="align">Align with a reference subtitle</option>
          </select>
        </label>
        {action === "offset" ? (
          <label>
            Timing offset (milliseconds)
            <input
              type="number"
              required
              min={-600000}
              max={600000}
              value={offset}
              onChange={(e) => setOffset(Number(e.target.value))}
            />
          </label>
        ) : (
          <label>
            {action === "align"
              ? "Reference subtitle with correct timing"
              : "Replacement file in the same format"}
            <input
              type="file"
              accept=".srt,.vtt,.ass,.ssa"
              onChange={(e) =>
                void (async () => {
                  const file = e.target.files?.[0];
                  setContent("");
                  setReferenceFormat("");
                  if (!file) return;
                  if (file.size > 4 * 1024 * 1024) {
                    setError("Subtitles are limited to 4 MiB.");
                    return;
                  }
                  try {
                    setContent(await file.text());
                    setReferenceFormat(
                      file.name.split(".").pop()?.toLowerCase() ?? "",
                    );
                  } catch {
                    setError("The subtitle file could not be read.");
                  }
                })()
              }
            />
          </label>
        )}
        {action === "align" && (
          <p>
            Use a correctly timed subtitle in the same language and cut. At
            least six unique matching dialogue cues spanning one minute are
            needed. The app estimates the offset and small timing drift locally,
            preserving the saved track's text and styles. Weak matches are
            refused.
          </p>
        )}
        <Button
          disabled={
            !track ||
            ((action === "replace" || action === "align") && !content) ||
            (!offset && action === "offset")
          }
          onClick={() => void preview()}
        >
          Review subtitle change
        </Button>
      </fieldset>
      {review && (
        <section className="panel system-panel">
          <h3>Review subtitle change</h3>
          <p>
            {review.cues} timed cues · {review.size_bytes} bytes ·{" "}
            {review.action === "offset"
              ? `${review.offset_ms} ms timing adjustment`
              : review.action}
          </p>
          <p>{review.destination}</p>
          <p>{review.message}</p>
          <p>
            Old checksum: {review.old_sha256.slice(0, 12)}… · New:{" "}
            {review.new_sha256.slice(0, 12)}…
          </p>
          <label>
            <input
              type="checkbox"
              checked={checked}
              disabled={busy}
              onChange={(e) => setChecked(e.target.checked)}
            />{" "}
            I reviewed this subtitle change and understand the current version
            will be retained for rollback.
          </label>
          <div className="button-row">
            <Button
              variant="soft"
              disabled={busy}
              onClick={() => setReview(undefined)}
            >
              Cancel review
            </Button>
            <Button
              disabled={busy || !checked}
              onClick={() =>
                void perform(async () => {
                  await api(`/subtitles/edits/${review.id}/apply`, {
                    method: "POST",
                    body: JSON.stringify({ reviewed: true }),
                  });
                  setReview(undefined);
                  setNotice("Reviewed subtitle change queued.");
                  resource.reload();
                  reload();
                })
              }
            >
              Apply reviewed subtitle change
            </Button>
          </div>
        </section>
      )}
      {(error || resource.error) && (
        <p role="alert" className="error-box">
          {error || resource.error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
      <h3>Subtitle edit history</h3>
      {resource.data?.items
        .filter((v) => v.state !== "review")
        .map((v) => (
          <div className="backup-row" key={v.id}>
            <div>
              <strong>
                {v.language} · {v.variant} · {v.action}
              </strong>
              <p>
                {v.message} ({v.state})
              </p>
              <small>{new Date(v.created_at * 1000).toLocaleString()}</small>
            </div>
            {v.state === "blocked" ? (
              <Button
                disabled={busy}
                onClick={() =>
                  void perform(async () => {
                    await api(`/subtitles/edits/${v.id}/retry`, {
                      method: "POST",
                      body: "{}",
                    });
                    resource.reload();
                  })
                }
              >
                Retry subtitle recovery
              </Button>
            ) : v.phase === "committed" ? (
              <Button
                disabled={busy || !!review}
                variant="soft"
                onClick={() => void preview(v)}
              >
                Review previous subtitle
              </Button>
            ) : null}
          </div>
        ))}
    </section>
  );
}
