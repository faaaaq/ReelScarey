import { Button, TextArea, TextField } from "@radix-ui/themes";
import { Pencil, Search } from "lucide-react";
import { useEffect, useId, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { APIError, api, messageFor, type MusicEditJob } from "../api";
import { RequestState } from "../components";
import { Alert, Modal } from "../ui";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";
import "./MusicLibraryEditor.css";

export const musicTrackEditLimit = 200;

export function useTrackEditSelection(scope: string | number | undefined) {
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);
  useEffect(() => {
    setSelected([]);
    setSelecting(false);
  }, [scope]);
  function selection(id: number) {
    return {
      checked: selected.includes(id),
      toggle: () =>
        setSelected((current) =>
          current.includes(id)
            ? current.filter((value) => value !== id)
            : [...current, id],
        ),
    };
  }
  return { selecting, setSelecting, selected, setSelected, selection };
}

export type MusicEditorTarget = {
  kind: "artists" | "albums" | "tracks" | "files";
  ids: number[];
  label: string;
  fileID?: number;
};

type ArtworkChoice = { url: string } | { data: string } | { reset: true };
type ArtworkCandidate = { title: string; artist?: string; url: string };
type Fields = Record<string, string>;
type Item = {
  id: number;
  fields: Fields;
  artwork_url?: string;
  custom_artwork?: boolean;
};
type FileChoice = { id: number; track_id: number; name: string; role: string };
type Snapshot = { items: Item[]; files?: FileChoice[]; revision: string };

const route = "/library/music/editor";

const fieldLabels = {
  artists: [
    ["name", "Name"],
    ["sort_name", "Sort name"],
    ["type", "Artist type"],
    ["country", "Country"],
    ["disambiguation", "Disambiguation"],
    ["overview", "Biography"],
  ],
  albums: [
    ["title", "Title"],
    ["album_artist", "Album artist"],
    ["year", "Release year"],
    ["release_date", "Release date"],
    ["type", "Release type"],
    ["secondary_types", "Secondary types"],
  ],
  tracks: [
    ["title", "Title"],
    ["artist", "Artist"],
    ["album", "Album"],
    ["album_artist", "Album artist"],
    ["year", "Year"],
    ["genre", "Genre"],
    ["disc", "Disc number"],
    ["number", "Track number"],
    ["comment", "Comment"],
  ],
  files: [],
};

export function MusicEditButton({
  label,
  onClick,
  showLabel = false,
}: {
  label: string;
  onClick: () => void;
  showLabel?: boolean;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      color="gray"
      size="1"
      className="mus-edit-button"
      title={label}
      aria-label={label}
      onClick={onClick}
    >
      <Pencil size={14} aria-hidden="true" />
      {showLabel && "Edit"}
    </Button>
  );
}

export function TrackEditSelectionBar({
  visibleIDs,
  selecting,
  setSelecting,
  selected,
  setSelected,
  onEdit,
}: {
  visibleIDs: number[];
  selecting: boolean;
  setSelecting: (value: boolean) => void;
  selected: number[];
  setSelected: (ids: number[]) => void;
  onEdit: (target: MusicEditorTarget) => void;
}) {
  const allSelected = visibleIDs.every((id) => selected.includes(id));
  if (!visibleIDs.length) return null;
  return (
    <div className="mus-track-edit-bar" aria-label="Track selection">
      <Button
        type="button"
        variant="soft"
        color="gray"
        size="1"
        aria-pressed={selecting}
        onClick={() => {
          setSelecting(!selecting);
          setSelected([]);
        }}
      >
        {selecting ? "Done selecting" : "Select tracks"}
      </Button>
      {selecting && (
        <>
          <label>
            <input
              type="checkbox"
              checked={allSelected}
              onChange={(event) =>
                setSelected(event.target.checked ? visibleIDs : [])
              }
            />{" "}
            Select all shown
          </label>
          <span role="status">{selected.length} selected</span>
          <Button
            size="1"
            disabled={!selected.length || selected.length > musicTrackEditLimit}
            onClick={() =>
              onEdit({
                kind: "tracks",
                ids: selected,
                label: "",
              })
            }
          >
            Edit selected
          </Button>
          {selected.length > musicTrackEditLimit && (
            <span>Choose up to {musicTrackEditLimit} tracks per edit.</span>
          )}
        </>
      )}
    </div>
  );
}

export function MusicLibraryEditor({
  target,
  onClose,
  onSaved,
}: {
  target: MusicEditorTarget;
  onClose: () => void;
  onSaved: () => void;
}) {
  const query = new URLSearchParams({
    kind: target.kind,
    ids: target.ids.join(","),
  });
  const snapshot = useResource<Snapshot>(`${route}?${query}`);
  const singular = target.kind.slice(0, -1);
  const title =
    target.ids.length === 1
      ? `Edit ${singular}`
      : `Edit ${target.ids.length} tracks`;
  const [jobID, setJobID] = useState(0);
  const [saving, setSaving] = useState(false);
  const job = useLiveResource<MusicEditJob>(
    jobID ? `/library/music/edits/${jobID}` : null,
    1200,
  );
  const formID = useId();
  const complete = job.data?.state === "completed";
  const failed = job.data?.state === "failed";
  function close() {
    if (saving) return;
    if (jobID) onSaved();
    onClose();
  }
  return (
    <Modal
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
      title={title}
      description={target.label || undefined}
      footer={
        <>
          <Button color="gray" variant="soft" onClick={close} disabled={saving}>
            {jobID ? "Done" : "Cancel"}
          </Button>
          {!jobID && (
            <Button
              type="submit"
              form={formID}
              disabled={saving || !snapshot.data || snapshot.loading}
            >
              {saving ? "Saving…" : "Save changes"}
            </Button>
          )}
        </>
      }
    >
      <RequestState
        loading={snapshot.loading}
        error={snapshot.error}
        retry={snapshot.reload}
      />
      {jobID ? (
        <>
          <Alert
            kind={failed ? "danger" : complete ? "success" : "info"}
            title={
              failed
                ? "Some file changes could not be saved"
                : complete
                  ? "File changes finished"
                  : "Applying file changes"
            }
          >
            {job.data
              ? `${job.data.files?.filter((file) => file.state === "completed").length ?? 0} of ${job.data.file_count} files updated.`
              : "The file edit is queued."}
            {job.data?.error && <p>{job.data.error}</p>}
            {job.error && <p>{job.error}</p>}
            <p>
              <Link to="/music/organize">View edit history</Link>
            </p>
          </Alert>
          {job.data?.files?.some((file) => file.error) && (
            <ul>
              {job.data.files
                .filter((file) => file.error)
                .map((file) => (
                  <li key={file.music_file_id}>
                    {file.source_path.split("/").at(-1)} — {file.error}
                  </li>
                ))}
            </ul>
          )}
        </>
      ) : (
        snapshot.data &&
        !snapshot.loading && (
          <EditorForm
            formID={formID}
            snapshot={snapshot.data}
            target={target}
            saving={saving}
            setSaving={setSaving}
            onReload={snapshot.reload}
            onComplete={(id) => {
              if (id) setJobID(id);
              else {
                onSaved();
                onClose();
              }
            }}
          />
        )
      )}
    </Modal>
  );
}

function EditorForm({
  formID,
  snapshot,
  target,
  saving,
  setSaving,
  onReload,
  onComplete,
}: {
  formID: string;
  snapshot: Snapshot;
  target: MusicEditorTarget;
  saving: boolean;
  setSaving: (state: boolean) => void;
  onReload: () => void;
  onComplete: (jobID?: number) => void;
}) {
  const [patch, setPatch] = useState<Fields>({});
  const [artwork, setArtwork] = useState<ArtworkChoice>();
  const [writeTags, setWriteTags] = useState(target.kind === "tracks");
  const [filename, setFilename] = useState("");
  const [fileID, setFileID] = useState(
    target.fileID ?? snapshot.files?.[0]?.id ?? 0,
  );
  const [error, setError] = useState("");
  const [stale, setStale] = useState(false);
  const batch = target.ids.length > 1;
  const item = snapshot.items[0];
  const { kind } = target;
  const trackEditor = kind === "tracks";
  const artworkEditor = kind === "artists" || kind === "albums";
  const currentFile = snapshot.files?.find((file) => file.id === fileID);
  const tagFields =
    target.kind === "artists"
      ? ["name"]
      : target.kind === "albums"
        ? ["title", "album_artist", "year"]
        : Object.keys(patch);
  const canWriteTags =
    (!trackEditor || !!snapshot.files?.length) &&
    tagFields.some((field) => field in patch);

  function change(field: string, value: string | undefined) {
    setPatch((current) => {
      const next = { ...current };
      if (value === undefined) delete next[field];
      else next[field] = value;
      return next;
    });
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    if (saving || stale) return;
    const fields = Object.fromEntries(
      Object.entries(patch).filter(
        ([field, value]) => batch || value !== item.fields[field],
      ),
    );
    const newName = filename.trim();
    if (!Object.keys(fields).length && !artwork && !newName) {
      setError("Change at least one field, filename or image.");
      return;
    }
    setError("");
    setSaving(true);
    try {
      const result = await api<{ edit_job_id?: number }>(route, {
        method: "PUT",
        body: JSON.stringify({
          kind: target.kind,
          ids: target.ids,
          revision: snapshot.revision,
          fields,
          artwork,
          write_tags: canWriteTags && writeTags,
          filename: newName || undefined,
          file_id: newName && !batch ? fileID : undefined,
        }),
      });
      onComplete(result.edit_job_id);
    } catch (e) {
      if (e instanceof APIError && e.code === "plan_changed") {
        setStale(true);
        setError(
          "These items changed while this editor was open. Reload the editor to get the latest values.",
        );
      } else setError(messageFor(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <form id={formID} onSubmit={save} className="mus-editor-form">
      {batch && (
        <p>Check the fields you want to change for all selected tracks.</p>
      )}
      {error && (
        <Alert kind="danger" title="Changes could not be saved">
          {error}
          {stale && (
            <Button type="button" variant="soft" onClick={onReload}>
              Reload editor
            </Button>
          )}
        </Alert>
      )}
      {target.kind !== "files" && (
        <fieldset disabled={saving || stale} className="mus-editor-fields">
          {fieldLabels[target.kind].map(([key, label]) => {
            const checked = key in patch;
            const mixed =
              batch &&
              snapshot.items.some(
                (other) => other.fields[key] !== item.fields[key],
              );
            const value = patch[key] ?? (mixed ? "" : (item.fields[key] ?? ""));
            const id = `${formID}-${key}`;
            return (
              <div
                key={key}
                className={`mus-editor-field${key === "overview" || key === "comment" ? " mus-editor-field-wide" : ""}`}
              >
                {batch && (
                  <input
                    type="checkbox"
                    aria-label={`Change ${label.toLowerCase()}`}
                    checked={checked}
                    onChange={(event) =>
                      change(
                        key,
                        event.target.checked
                          ? mixed
                            ? ""
                            : item.fields[key]
                          : undefined,
                      )
                    }
                  />
                )}
                <div>
                  <label htmlFor={id}>{label}</label>
                  {key === "overview" || key === "comment" ? (
                    <TextArea
                      id={id}
                      value={value}
                      placeholder={mixed ? "Mixed values" : undefined}
                      disabled={batch && !checked}
                      maxLength={4000}
                      onChange={(event) => change(key, event.target.value)}
                    />
                  ) : (
                    <TextField.Root
                      id={id}
                      value={value}
                      placeholder={mixed ? "Mixed values" : undefined}
                      disabled={batch && !checked}
                      inputMode={
                        ["year", "disc", "number"].includes(key)
                          ? "numeric"
                          : undefined
                      }
                      maxLength={300}
                      onChange={(event) => change(key, event.target.value)}
                    />
                  )}
                </div>
              </div>
            );
          })}
        </fieldset>
      )}
      {trackEditor &&
        ("album" in patch || "album_artist" in patch || "artist" in patch) && (
          <p className="rs-muted">
            Track credits update here. To rename a library artist or album, use
            its own Edit button.
          </p>
        )}
      {!artworkEditor && !!snapshot.files?.length && (
        <fieldset disabled={saving || stale} className="mus-editor-section">
          <legend>{batch ? "Rename files" : "Rename file"}</legend>
          {!batch && snapshot.files.length > 1 && (
            <label className="mus-editor-stacked">
              Registered file
              <select
                value={fileID}
                onChange={(event) => {
                  setFileID(Number(event.target.value));
                  setFilename("");
                }}
              >
                {snapshot.files.map((file) => (
                  <option key={file.id} value={file.id}>
                    {file.name} · {file.role}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label className="mus-editor-stacked">
            {batch ? "Filename pattern" : "New filename"}
            <TextField.Root
              value={filename}
              placeholder={batch ? "{track} - {title}" : currentFile?.name}
              maxLength={240}
              onChange={(event) => setFilename(event.target.value)}
            />
          </label>
          <small className="rs-muted">
            {batch
              ? "Renames one registered file per selected track, preferring the primary copy. Use {title}, {artist}, {album}, {track}, {disc} or {filename}. Extensions and folders stay the same."
              : `Current file: ${currentFile?.name ?? ""}. The extension and folder are kept.`}
          </small>
        </fieldset>
      )}
      {artworkEditor && (
        <ArtworkEditor
          kind={kind}
          item={item}
          fields={{ ...item.fields, ...patch }}
          choice={artwork}
          onChange={setArtwork}
          disabled={saving || stale}
        />
      )}
      {canWriteTags && (
        <label className="mus-editor-check">
          <input
            type="checkbox"
            checked={writeTags}
            disabled={saving || stale}
            onChange={(event) => setWriteTags(event.target.checked)}
          />
          Also write changed tags to registered audio files
        </label>
      )}
    </form>
  );
}

function ArtworkEditor({
  kind,
  item,
  fields,
  choice,
  onChange,
  disabled,
}: {
  kind: "artists" | "albums";
  item: Item;
  fields: Fields;
  choice?: ArtworkChoice;
  onChange: (choice?: ArtworkChoice) => void;
  disabled: boolean;
}) {
  const [query, setQuery] = useState<string>();
  const [results, setResults] = useState<ArtworkCandidate[]>();
  const [searching, setSearching] = useState(false);
  const [reading, setReading] = useState(false);
  const [error, setError] = useState("");
  const [imageError, setImageError] = useState("");
  const defaultQuery =
    kind === "artists" ? fields.name : `${fields.album_artist} ${fields.title}`;
  const label = kind === "artists" ? "Artist thumbnail" : "Album artwork";
  const preview =
    choice && "url" in choice
      ? choice.url
      : choice && "data" in choice
        ? `data:image/${choice.data.startsWith("iVBOR") ? "png" : "jpeg"};base64,${choice.data}`
        : choice && "reset" in choice
          ? undefined
          : item.artwork_url;

  async function search() {
    if (searching || !(query ?? defaultQuery).trim()) return;
    setSearching(true);
    setError("");
    try {
      const params = new URLSearchParams({
        kind,
        query: (query ?? defaultQuery).trim(),
      });
      const found = await api<{ items: ArtworkCandidate[] }>(
        `${route}/artwork?${params}`,
      );
      setResults(found.items);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setSearching(false);
    }
  }

  function upload(file?: File) {
    if (!file) return;
    setError("");
    if (
      !["image/jpeg", "image/png"].includes(file.type) ||
      file.size > 5 * 1024 * 1024
    ) {
      setError("Choose a JPEG or PNG image up to 5 MB.");
      return;
    }
    setReading(true);
    const reader = new FileReader();
    reader.onload = () => {
      const data = String(reader.result).split(",")[1];
      if (data) onChange({ data });
      setReading(false);
    };
    reader.onerror = () => {
      setError("The image could not be read.");
      setReading(false);
    };
    reader.readAsDataURL(file);
  }

  return (
    <fieldset disabled={disabled} className="mus-editor-section">
      <legend>{label}</legend>
      <div className="mus-editor-artwork">
        <div className="mus-editor-art-preview">
          {preview && preview !== imageError ? (
            <img
              src={preview}
              alt={
                choice ? "Selected artwork" : `Current ${label.toLowerCase()}`
              }
              referrerPolicy="no-referrer"
              onError={() => setImageError(preview)}
            />
          ) : (
            <span>
              {choice && "reset" in choice ? "Provider artwork" : "No preview"}
            </span>
          )}
        </div>
        <div className="mus-editor-artwork-options">
          <div className="mus-editor-artwork-search">
            <TextField.Root
              aria-label="Artwork search"
              placeholder="Search Deezer artwork"
              value={query ?? defaultQuery}
              maxLength={200}
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  void search();
                }
              }}
            />
            <Button
              type="button"
              variant="soft"
              disabled={searching || reading}
              onClick={search}
            >
              <Search size={14} aria-hidden="true" />
              {searching ? "Searching…" : "Search artwork"}
            </Button>
          </div>
          <label className="mus-editor-upload">
            <span>Upload image</span>
            <input
              type="file"
              accept="image/jpeg,image/png"
              disabled={reading}
              onChange={(event) => {
                upload(event.target.files?.[0]);
                event.target.value = "";
              }}
            />
          </label>
          {choice ? (
            <Button
              type="button"
              size="1"
              variant="ghost"
              onClick={() => onChange()}
            >
              Undo artwork change
            </Button>
          ) : item.custom_artwork ? (
            <Button
              type="button"
              size="1"
              variant="ghost"
              onClick={() => onChange({ reset: true })}
            >
              Restore provider artwork
            </Button>
          ) : null}
        </div>
      </div>
      {error && <Alert kind="danger">{error}</Alert>}
      {results?.length === 0 && (
        <p role="status">No artwork matched this search.</p>
      )}
      {!!results?.length && (
        <div
          className="mus-editor-artwork-results"
          aria-label="Artwork results"
        >
          {results.map((result) => (
            <button
              type="button"
              key={result.url}
              aria-pressed={
                !!choice && "url" in choice && choice.url === result.url
              }
              onClick={() => onChange({ url: result.url })}
            >
              <img
                src={result.url}
                alt=""
                loading="lazy"
                referrerPolicy="no-referrer"
              />
              <span>
                {result.title}
                {result.artist ? ` · ${result.artist}` : ""}
              </span>
            </button>
          ))}
        </div>
      )}
    </fieldset>
  );
}
