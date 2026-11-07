import { useId, useState } from "react";
import { Button } from "@radix-ui/themes";
import {
  Alert,
  Check,
  DescriptionItem,
  DescriptionList,
  FieldSet,
  FormActions,
  FormGroup,
  Label,
  Modal,
  Table,
  formatBytes,
} from "../ui";
import type { Kind } from "../ui";
import { api, messageFor } from "../api";
import { useLiveResource } from "../useLiveResource";
import type { SubtitleTrack } from "./Subtitles";
import "./Subtitles.css";

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
const variantName: Record<SubtitleTrack["variant"], string> = {
  full: "Full subtitles",
  signs: "Signs / songs",
  forced: "Forced",
};
function readable(value: string) {
  return value.replaceAll("_", " ");
}
function stateKind(state: string): Kind {
  if (state === "blocked") return "danger";
  if (state === "completed") return "success";
  if (state === "review") return "warning";
  return "info";
}
export function SubtitleEdits({
  fileID,
  tracks,
  reload,
  embedded = false,
}: {
  fileID: number;
  tracks: Track[];
  reload: () => void;
  embedded?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const editable = tracks.filter(
    (track) => track.source !== "embedded" && !!track.path,
  );
  const editor = <Editor fileID={fileID} tracks={editable} reload={reload} />;
  if (embedded) return editor;
  return (
    <div className="rs-stack">
      <Button
        variant="soft"
        disabled={!editable.length}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        {open ? "Hide subtitle editing" : "Edit or replace a saved subtitle"}
      </Button>
      {open && editor}
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
  const trackID = useId();
  const actionID = useId();
  const offsetID = useId();
  const fileIDInput = useId();
  const track = tracks[selected] ?? tracks[0];
  const history = resource.data?.items.filter(
    (edit) => edit.state !== "review",
  );
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
  async function apply(edit: Edit) {
    await perform(async () => {
      await api(`/subtitles/edits/${edit.id}/apply`, {
        method: "POST",
        body: JSON.stringify({ reviewed: true }),
      });
      setReview(undefined);
      setChecked(false);
      setNotice("Reviewed subtitle change queued.");
      resource.reload();
      reload();
    });
  }
  return (
    <FieldSet
      legend="Edits"
      headingLevel={3}
      className="sub-edits-fieldset"
      description="SRT, VTT, ASS or SSA up to 4 MiB. The current version stays available for rollback."
    >
      <fieldset disabled={busy || !!review} className="rs-stack">
        <FormGroup label="Saved track" htmlFor={trackID}>
          <select
            id={trackID}
            value={selected}
            onChange={(event) => setSelected(Number(event.target.value))}
          >
            {tracks.map((value, index) => (
              <option key={value.language + value.variant} value={index}>
                {value.language} · {variantName[value.variant]} · {value.path}
              </option>
            ))}
          </select>
        </FormGroup>
        <FormGroup label="Change" htmlFor={actionID}>
          <select
            id={actionID}
            value={action}
            onChange={(event) => {
              setAction(event.target.value);
              setContent("");
              setReferenceFormat("");
            }}
          >
            <option value="offset">Adjust timing</option>
            <option value="replace">Upload replacement</option>
            <option value="align">Align with a reference subtitle</option>
          </select>
        </FormGroup>
        {action === "offset" ? (
          <FormGroup
            label="Timing offset (ms)"
            htmlFor={offsetID}
            help="Positive values delay subtitles; negative values show them earlier. Negative cue times are refused."
          >
            <input
              id={offsetID}
              type="number"
              required
              min={-600000}
              max={600000}
              value={offset}
              onChange={(event) => setOffset(Number(event.target.value))}
            />
          </FormGroup>
        ) : (
          <FormGroup
            label={
              action === "align"
                ? "Reference subtitle with correct timing"
                : "Replacement subtitle in the same format"
            }
            htmlFor={fileIDInput}
            help={
              action === "align"
                ? "Same language and cut; at least six unique matching cues are required. Weak matches are refused, and only cue times change."
                : "The new file is validated before anything is written."
            }
          >
            <input
              id={fileIDInput}
              type="file"
              accept=".srt,.vtt,.ass,.ssa"
              onChange={(event) =>
                void (async () => {
                  const file = event.target.files?.[0];
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
          </FormGroup>
        )}
        <div className="sub-form-actions">
          <FormActions>
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
          </FormActions>
        </div>
      </fieldset>
      {error && !review && <Alert kind="danger">{error}</Alert>}
      {resource.error && !review && (
        <Alert kind="danger">{resource.error}</Alert>
      )}
      {notice && <Alert kind="success">{notice}</Alert>}
      {history && history.length > 0 ? (
        <Table label="Subtitle edit history">
          <thead>
            <tr>
              <th scope="col">Track</th>
              <th scope="col">Change</th>
              <th scope="col">Status</th>
              <th scope="col">Created</th>
              <th scope="col" className="is-actions">
                Action
              </th>
            </tr>
          </thead>
          <tbody>
            {history.map((edit) => (
              <tr key={edit.id}>
                <th scope="row">
                  {edit.language} · {variantName[edit.variant]}
                </th>
                <td>{readable(edit.action)}</td>
                <td>
                  <Label kind={stateKind(edit.state)}>
                    {readable(edit.state)}
                  </Label>
                  <span className="sub-edit-message">{edit.message}</span>
                </td>
                <td className="sub-muted">
                  {new Date(edit.created_at * 1000).toLocaleString()}
                </td>
                <td className="is-actions">
                  {edit.state === "blocked" ? (
                    <Button
                      size="1"
                      variant="soft"
                      disabled={busy}
                      onClick={() =>
                        void perform(async () => {
                          await api(`/subtitles/edits/${edit.id}/retry`, {
                            method: "POST",
                            body: "{}",
                          });
                          resource.reload();
                        })
                      }
                    >
                      Retry subtitle recovery
                    </Button>
                  ) : edit.phase === "committed" ? (
                    <Button
                      size="1"
                      variant="soft"
                      disabled={busy || !!review}
                      onClick={() => void preview(edit)}
                    >
                      Review previous subtitle
                    </Button>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      ) : (
        <p className="rs-muted">No completed subtitle edits yet.</p>
      )}
      {review && (
        <Modal
          open
          onOpenChange={(open) => {
            if (!open && !busy) {
              setReview(undefined);
              setChecked(false);
            }
          }}
          title="Review subtitle change"
          size="small"
          footer={
            <>
              <Button
                variant="soft"
                disabled={busy}
                onClick={() => {
                  setReview(undefined);
                  setChecked(false);
                }}
              >
                Cancel review
              </Button>
              <Button
                disabled={busy || !checked}
                onClick={() => void apply(review)}
              >
                Apply reviewed subtitle change
              </Button>
            </>
          }
        >
          <DescriptionList>
            <DescriptionItem title="Change">
              {review.action === "offset"
                ? `${review.offset_ms} ms timing adjustment`
                : readable(review.action)}
            </DescriptionItem>
            <DescriptionItem title="Cues">
              {review.cues} timed cues
            </DescriptionItem>
            <DescriptionItem title="Size">
              {formatBytes(review.size_bytes)}
            </DescriptionItem>
            <DescriptionItem title="Destination">
              {review.destination}
            </DescriptionItem>
            <DescriptionItem title="Checksum">
              {review.old_sha256.slice(0, 12)}… →{" "}
              {review.new_sha256.slice(0, 12)}…
            </DescriptionItem>
          </DescriptionList>
          <p>{review.message}</p>
          <Check
            label="I reviewed this subtitle change and understand the current version will be retained for rollback."
            checked={checked}
            disabled={busy}
            onChange={setChecked}
          />
          {error && <Alert kind="danger">{error}</Alert>}
        </Modal>
      )}
    </FieldSet>
  );
}
