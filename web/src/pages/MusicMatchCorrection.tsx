import { useId, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor, type MusicScanFile, type MusicTrack } from "../api";
import { Alert, FormGroup, Modal, Table } from "../ui";
import "./MusicMatchCorrection.css";

type AlbumCandidate = {
  id: string;
  artist: string;
  title: string;
  year: number | null;
  track_count?: number;
};
type AlbumPreview = AlbumCandidate & { tracks: MusicTrack[] };

function basename(path: string) {
  return path.split("/").at(-1) ?? path;
}

function titleKey(value: string) {
  return value
    .replace(/\s*[([](?:feat\.?|ft\.?)\s+[^)\]]+[)\]]\s*$/i, "")
    .normalize("NFKD")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]/gu, "");
}

function duration(ms: number) {
  if (!ms) return "";
  const seconds = Math.round(ms / 1000);
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

function albumLabel(album: AlbumCandidate) {
  return [
    album.title,
    album.artist,
    album.year,
    album.track_count ? `${album.track_count} tracks` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

function groupFromInput(value: string) {
  const match =
    /^(?:https:\/\/(?:www\.)?musicbrainz\.org\/release-group\/)?([a-f\d]{8}(?:-[a-f\d]{4}){3}-[a-f\d]{12})(?:[/?#].*)?$/i.exec(
      value,
    );
  return match?.[1];
}

function unique<T>(values: T[]) {
  return new Set(values).size === values.length;
}

export function MusicMatchCorrection({
  scanID,
  files,
  onClose,
  onSaved,
}: {
  scanID: number;
  files: MusicScanFile[];
  onClose: () => void;
  onSaved: (matched: number) => void;
}) {
  const first = files[0];
  const fieldID = useId();
  const [mode, setMode] = useState<"provider" | "itunes" | "local">("provider");
  const [artist, setArtist] = useState(
    first.tag_album_artist || first.tag_artist || "",
  );
  const [album, setAlbum] = useState(first.tag_album || "");
  const [query, setQuery] = useState(
    first.tag_album || first.tag_musicbrainz_release_group || "",
  );
  const [results, setResults] = useState<AlbumCandidate[] | null>(null);
  const [complete, setComplete] = useState(true);
  const [preview, setPreview] = useState<AlbumPreview | null>(null);
  const [assignments, setAssignments] = useState<Record<number, string>>({});
  const [rows, setRows] = useState(() =>
    files.map((file) => ({
      file_id: file.id,
      title:
        file.tag_title || basename(file.relative_path).replace(/\.[^.]+$/, ""),
      disc: String(file.tag_disc || 1),
      number: file.tag_track ? String(file.tag_track) : "",
    })),
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const endpoint = `/library/music/scans/${scanID}/corrections`;
  const fromITunes = mode === "itunes";
  const providerName = fromITunes ? "iTunes" : "MusicBrainz";
  const trackKey = (track: MusicTrack) =>
    fromITunes ? `${track.disc}.${track.number}` : track.musicbrainz_id;
  const providerRows = files.map((file) => ({
    file_id: file.id,
    track_id: assignments[file.id] ?? "",
  }));
  const localRows = rows.map((row) => ({
    ...row,
    disc: Number(row.disc),
    number: Number(row.number),
  }));
  const providerDuplicates = !unique(
    providerRows.map((row) => row.track_id).filter(Boolean),
  );
  const localDuplicates = !unique(
    localRows
      .filter((row) => row.disc && row.number)
      .map((row) => `${row.disc}:${row.number}`),
  );
  const localValid =
    artist.trim() &&
    album.trim() &&
    !localDuplicates &&
    localRows.every(
      (row) =>
        row.title.trim() &&
        [row.disc, row.number].every(
          (n) => Number.isInteger(n) && n >= 1 && n <= 100000,
        ),
    );
  const canSave =
    mode !== "local"
      ? preview &&
        !providerDuplicates &&
        providerRows.every((row) => row.track_id)
      : localValid;

  function changeMode(next: typeof mode) {
    if (next === mode) return;
    setMode(next);
    setError("");
    setPreview(null);
    setAssignments({});
    setResults(null);
    if (next === "itunes") {
      setQuery(
        [
          first.tag_album_artist || first.tag_artist,
          first.tag_album || first.tag_title,
        ]
          .filter(Boolean)
          .join(" "),
      );
    } else if (next === "provider") {
      setQuery(first.tag_album || first.tag_musicbrainz_release_group || "");
    }
  }

  async function loadAlbum(id: string) {
    const album = await api<AlbumPreview>(
      `${endpoint}?${new URLSearchParams(fromITunes ? { source: "itunes", collection_id: id } : { release_group_id: id })}`,
      { signal: AbortSignal.timeout(35000) },
    );
    const suggested = files.map((file) => {
      const key = titleKey(file.tag_title || "");
      const matches = album.tracks.filter(
        (track) => key && titleKey(track.title) === key,
      );
      return {
        id: file.id,
        track: matches.length === 1 ? trackKey(matches[0]) : "",
      };
    });
    setAssignments(
      Object.fromEntries(
        suggested
          .filter(
            (item) =>
              item.track &&
              suggested.filter((other) => other.track === item.track).length ===
                1,
          )
          .map((item) => [item.id, item.track]),
      ),
    );
    setPreview(album);
  }

  async function lookUp(value: string, direct = false) {
    if (busy || !value.trim()) return;
    setError("");
    setBusy(true);
    setPreview(null);
    setAssignments({});
    if (!direct) setResults(null);
    try {
      const group = direct
        ? value
        : fromITunes
          ? /^(?:https:\/\/(?:music|itunes)\.apple\.com\/[^?#]*\/album\/[^?#]*\/)?([1-9]\d*)(?:[?#].*)?$/i.exec(
              value.trim(),
            )?.[1]
          : groupFromInput(value.trim());
      if (group) {
        await loadAlbum(group);
      } else {
        const data = await api<{ items: AlbumCandidate[]; complete: boolean }>(
          `${endpoint}?${new URLSearchParams({ album: value.trim(), ...(fromITunes ? { source: "itunes" } : {}) })}`,
          { signal: AbortSignal.timeout(35000) },
        );
        setResults(data.items);
        setComplete(data.complete);
      }
    } catch (err) {
      setError(messageFor(err));
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    if (!canSave || busy) return;
    setBusy(true);
    setError("");
    try {
      const appleTracks =
        preview && fromITunes
          ? new Map(preview.tracks.map((track) => [trackKey(track), track]))
          : null;
      const appleRows = appleTracks
        ? providerRows.flatMap((row) => {
            const track = appleTracks.get(row.track_id);
            return track
              ? [
                  {
                    file_id: row.file_id,
                    title: track.title,
                    disc: track.disc,
                    number: track.number,
                  },
                ]
              : [];
          })
        : [];
      if (appleTracks && appleRows.length !== files.length) {
        setError("Review the iTunes track assignments before saving.");
        return;
      }
      const data = await api<{ matched: number }>(endpoint, {
        method: "POST",
        body: JSON.stringify({
          reviewed: true,
          ...(mode === "provider"
            ? { release_group_id: preview?.id, files: providerRows }
            : fromITunes
              ? {
                  artist: preview?.artist,
                  album: preview?.title,
                  files: appleRows,
                }
              : { artist, album, files: localRows }),
        }),
        signal: AbortSignal.timeout(35000),
      });
      onSaved(data.matched);
    } catch (err) {
      setError(messageFor(err));
    } finally {
      setBusy(false);
    }
  }

  const recordingQuery = new URLSearchParams({
    type: "recording",
    query: first.tag_title || rows[0].title,
  });
  return (
    <Modal
      open
      size="large"
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
      title={
        files.length === 1
          ? "Correct match"
          : `Correct matches for ${files.length} files`
      }
      description="Review the track assignments. Saving updates the scan. Finalize applies the catalog metadata to your files."
      footer={
        <>
          <Button color="gray" variant="soft" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={busy || !canSave} onClick={() => void save()}>
            Save {files.length === 1 ? "match" : "matches"}
          </Button>
        </>
      }
    >
      <div className="mus-correction">
        <div
          className="mus-correction-modes"
          role="group"
          aria-label="Match method"
        >
          <Button
            variant={mode === "provider" ? "solid" : "soft"}
            aria-pressed={mode === "provider"}
            disabled={busy}
            onClick={() => changeMode("provider")}
          >
            Match to MusicBrainz
          </Button>
          <Button
            variant={fromITunes ? "solid" : "soft"}
            aria-pressed={fromITunes}
            disabled={busy}
            onClick={() => changeMode("itunes")}
          >
            Match to iTunes
          </Button>
          <Button
            variant={mode === "local" ? "solid" : "soft"}
            aria-pressed={mode === "local"}
            disabled={busy}
            onClick={() => changeMode("local")}
          >
            Enter local metadata
          </Button>
        </div>
        {error && (
          <Alert kind="danger" title="Could not complete the request">
            {error}
          </Alert>
        )}
        {busy && (
          <span role="status" className="rs-muted">
            Working…
          </span>
        )}
        {mode !== "local" ? (
          <>
            <form
              className="mus-correction-search"
              onSubmit={(event: FormEvent) => {
                event.preventDefault();
                void lookUp(query);
              }}
            >
              <FormGroup
                label={
                  fromITunes
                    ? "Artist and album or Apple Music album link"
                    : "Album title or MusicBrainz release-group link"
                }
                htmlFor={`${fieldID}-search`}
              >
                <TextField.Root
                  id={`${fieldID}-search`}
                  value={query}
                  maxLength={500}
                  disabled={busy}
                  onChange={(event) => setQuery(event.target.value)}
                />
              </FormGroup>
              <Button
                type="submit"
                variant="soft"
                disabled={busy || !query.trim()}
              >
                Find album
              </Button>
            </form>
            {!fromITunes && (
              <a
                href={`https://musicbrainz.org/search?${recordingQuery}`}
                target="_blank"
                rel="noreferrer"
              >
                Search MusicBrainz by track title (opens in a new tab)
              </a>
            )}
            {results && results.length === 0 && (
              <Alert kind="info" title={`No ${providerName} albums found`}>
                Check the album title or enter local metadata.
              </Alert>
            )}
            {results && results.length > 0 && (
              <div
                className="mus-correction-results"
                aria-label="Album results"
              >
                {!complete && (
                  <p className="rs-muted">
                    Showing the first results. Paste a direct album link if your
                    album is missing.
                  </p>
                )}
                {results.map((result) => (
                  <Button
                    key={result.id}
                    variant={preview?.id === result.id ? "solid" : "soft"}
                    disabled={busy}
                    onClick={() => void lookUp(result.id, true)}
                  >
                    {albumLabel(result)}
                  </Button>
                ))}
              </div>
            )}
            {preview && (
              <>
                <h3>{albumLabel(preview)}</h3>
                {fromITunes && (
                  <p className="rs-muted">
                    The iTunes tracklist will be saved as local catalog
                    metadata.
                  </p>
                )}
                {providerDuplicates && (
                  <Alert
                    kind="warning"
                    title="Choose a different track for each file."
                  />
                )}
                <Table label={`Review ${providerName} track assignments`}>
                  <thead>
                    <tr>
                      <th scope="col">File</th>
                      <th scope="col">{providerName} track</th>
                    </tr>
                  </thead>
                  <tbody>
                    {files.map((file) => (
                      <tr key={file.id}>
                        <td>
                          <span className="mus-path" title={file.relative_path}>
                            {basename(file.relative_path)}
                          </span>{" "}
                          <span className="rs-muted">
                            {duration(file.duration_ms)}
                          </span>
                        </td>
                        <td>
                          <select
                            className="mus-correction-select"
                            aria-label={`Track for ${basename(file.relative_path)}`}
                            disabled={busy}
                            value={assignments[file.id] ?? ""}
                            onChange={(event) =>
                              setAssignments({
                                ...assignments,
                                [file.id]: event.target.value,
                              })
                            }
                          >
                            <option value="">Choose a track</option>
                            {preview.tracks.map((track) => (
                              <option
                                key={trackKey(track)}
                                value={trackKey(track)}
                              >
                                {track.disc}.{track.number} {track.title}
                                {track.duration_ms
                                  ? ` · ${duration(track.duration_ms)}`
                                  : ""}
                              </option>
                            ))}
                          </select>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              </>
            )}
          </>
        ) : (
          <>
            <div className="mus-correction-fields">
              <FormGroup label="Album artist" htmlFor={`${fieldID}-artist`}>
                <TextField.Root
                  id={`${fieldID}-artist`}
                  maxLength={500}
                  value={artist}
                  disabled={busy}
                  onChange={(event) => setArtist(event.target.value)}
                />
              </FormGroup>
              <FormGroup label="Album" htmlFor={`${fieldID}-album`}>
                <TextField.Root
                  id={`${fieldID}-album`}
                  maxLength={500}
                  value={album}
                  disabled={busy}
                  onChange={(event) => setAlbum(event.target.value)}
                />
              </FormGroup>
            </div>
            <p className="rs-muted">
              Enter the track order from the release. These tracks will use
              local catalog metadata.
            </p>
            {localDuplicates && (
              <Alert
                kind="warning"
                title="Each track needs a unique disc and track number."
              />
            )}
            <Table label="Local track metadata">
              <thead>
                <tr>
                  <th scope="col">File</th>
                  <th scope="col">Track title</th>
                  <th scope="col">Disc</th>
                  <th scope="col">Track no.</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, index) => {
                  const name = basename(files[index].relative_path);
                  const update = (
                    key: "title" | "disc" | "number",
                    value: string,
                  ) => {
                    setRows(
                      rows.map((entry) =>
                        entry.file_id === row.file_id
                          ? { ...entry, [key]: value }
                          : entry,
                      ),
                    );
                  };
                  return (
                    <tr key={row.file_id}>
                      <td>
                        <span
                          className="mus-path"
                          title={files[index].relative_path}
                        >
                          {name}
                        </span>
                      </td>
                      <td>
                        <TextField.Root
                          aria-label={`Title for ${name}`}
                          maxLength={500}
                          value={row.title}
                          disabled={busy}
                          onChange={(event) =>
                            update("title", event.target.value)
                          }
                        />
                      </td>
                      <td>
                        <TextField.Root
                          className="mus-correction-number"
                          aria-label={`Disc for ${name}`}
                          type="number"
                          min={1}
                          max={100000}
                          value={row.disc}
                          disabled={busy}
                          onChange={(event) =>
                            update("disc", event.target.value)
                          }
                        />
                      </td>
                      <td>
                        <TextField.Root
                          className="mus-correction-number"
                          aria-label={`Track number for ${name}`}
                          type="number"
                          min={1}
                          max={100000}
                          value={row.number}
                          disabled={busy}
                          onChange={(event) =>
                            update("number", event.target.value)
                          }
                        />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </Table>
          </>
        )}
      </div>
    </Modal>
  );
}
