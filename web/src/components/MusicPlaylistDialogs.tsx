import { useEffect, useId, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  api,
  messageFor,
  type MusicArtistTrack,
  type MusicPlaylistDetails,
  type MusicPlaylistSummary,
} from "../api";
import { EmptyState, Modal } from "../ui";
import "./MusicPlaylistDialogs.css";

function playlistNameError(value: string): string {
  const length = Array.from(value.trim()).length;
  if (length === 0) return "Enter a playlist name.";
  if (length > 100) return "Playlist names must be 100 characters or fewer.";
  return "";
}

function DialogError({ id, message }: { id: string; message: string }) {
  if (!message) return null;
  return (
    <p
      id={id}
      role="alert"
      className="rs-form-error mmp-playlist-dialog__error"
    >
      {message}
    </p>
  );
}

export function PlaylistCreateDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (details: MusicPlaylistDetails) => Promise<void> | void;
}) {
  const formID = useId();
  const errorID = `${formID}-error`;
  const [name, setName] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setName("");
      setError("");
    }
  }, [open]);

  async function createPlaylist(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    const trimmedName = name.trim();
    const validationError = playlistNameError(trimmedName);
    if (validationError) {
      setError(validationError);
      return;
    }

    setPending(true);
    setError("");
    try {
      const details = await api<MusicPlaylistDetails>(
        "/library/music/playlists",
        {
          method: "POST",
          body: JSON.stringify({ name: trimmedName }),
        },
      );
      await onCreated(details);
      onOpenChange(false);
    } catch (caught) {
      setError(messageFor(caught));
    } finally {
      setPending(false);
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={(nextOpen) => {
        if (!pending) onOpenChange(nextOpen);
      }}
      title="Create playlist"
      size="small"
      footer={
        <>
          <Button
            type="button"
            variant="soft"
            color="gray"
            disabled={pending}
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button type="submit" form={formID} disabled={pending}>
            {pending ? "Creating…" : "Create playlist"}
          </Button>
        </>
      }
    >
      <div className="mmp-playlist-dialog">
        <form
          id={formID}
          className="mmp-playlist-dialog__form"
          onSubmit={(event) => void createPlaylist(event)}
          noValidate
        >
          <label className="mmp-playlist-dialog__field">
            Playlist name
            <TextField.Root
              aria-label="Playlist name"
              aria-describedby={error ? errorID : undefined}
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={100}
              required
              autoComplete="off"
              disabled={pending}
              autoFocus
            />
          </label>
        </form>
        <DialogError id={errorID} message={error} />
      </div>
    </Modal>
  );
}

export function PlaylistTrackPickerDialog({
  track,
  playlists,
  onOpenChange,
  onChanged,
}: {
  track: MusicArtistTrack | null;
  playlists: MusicPlaylistSummary[];
  onOpenChange: (open: boolean) => void;
  onChanged: (details?: MusicPlaylistDetails) => Promise<void> | void;
}) {
  const open = track !== null;
  const formID = useId();
  const errorID = `${formID}-error`;
  const [name, setName] = useState("");
  const [pendingAction, setPendingAction] = useState<string | null>(null);
  const [error, setError] = useState("");
  const trackID = track?.id;
  const hasTrackID = trackID !== undefined;
  const pending = pendingAction !== null;

  useEffect(() => {
    if (open) {
      setName("");
      setError("");
    }
  }, [open, track?.id]);

  async function performChange(
    action: string,
    mutation: () => Promise<MusicPlaylistDetails>,
  ) {
    if (pendingAction || !track || !hasTrackID) return;
    setPendingAction(action);
    setError("");
    try {
      let details: MusicPlaylistDetails;
      try {
        details = await mutation();
      } catch (caught) {
        setError(messageFor(caught));
        // A conflict or an ambiguous network failure may have changed the
        // server. Refresh the visible summaries, but retain the original error
        // and never make a failed or uncertain mutation look successful.
        try {
          await onChanged();
        } catch {
          // The mutation error remains the most useful actionable message.
        }
        return;
      }

      try {
        await onChanged(details);
      } catch (caught) {
        setError(messageFor(caught));
        return;
      }
      onOpenChange(false);
    } finally {
      setPendingAction(null);
    }
  }

  async function addToPlaylist(playlist: MusicPlaylistSummary) {
    if (!track || trackID === undefined) return;
    await performChange(`playlist:${playlist.id}`, () =>
      api<MusicPlaylistDetails>(
        `/library/music/playlists/${encodeURIComponent(String(playlist.id))}/tracks`,
        {
          method: "POST",
          body: JSON.stringify({
            track_id: trackID,
            revision: playlist.revision,
          }),
        },
      ),
    );
  }

  async function createWithTrack(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pendingAction || !track || trackID === undefined) return;
    const trimmedName = name.trim();
    const validationError = playlistNameError(trimmedName);
    if (validationError) {
      setError(validationError);
      return;
    }
    await performChange("create", () =>
      api<MusicPlaylistDetails>("/library/music/playlists", {
        method: "POST",
        body: JSON.stringify({ name: trimmedName, track_id: trackID }),
      }),
    );
  }

  return (
    <Modal
      open={open}
      onOpenChange={(nextOpen) => {
        if (!pending) onOpenChange(nextOpen);
      }}
      title={`Add ${track?.title ?? "track"} to playlist`}
      size="small"
      footer={
        <Button
          type="button"
          variant="soft"
          color="gray"
          disabled={pending}
          onClick={() => onOpenChange(false)}
        >
          Cancel
        </Button>
      }
    >
      <div className="mmp-playlist-dialog">
        {!hasTrackID && (
          <p role="alert" className="mmp-playlist-dialog__warning">
            This track is not in the library and cannot be added to a playlist.
          </p>
        )}
        <section
          className="mmp-playlist-dialog__section"
          aria-label="Existing playlists"
        >
          <h3 className="mmp-playlist-dialog__heading">Existing playlists</h3>
          {playlists.length === 0 ? (
            <EmptyState title="No playlists yet">
              <p>Create a playlist below to add this track.</p>
            </EmptyState>
          ) : (
            <div className="mmp-playlist-dialog__list">
              {playlists.map((playlist) => {
                const action = `playlist:${playlist.id}`;
                const adding = pendingAction === action;
                return (
                  <Button
                    key={playlist.id}
                    type="button"
                    variant="soft"
                    className="mmp-playlist-dialog__playlist"
                    disabled={pending || !hasTrackID}
                    onClick={() => void addToPlaylist(playlist)}
                  >
                    <span className="mmp-playlist-dialog__playlist-name">
                      {playlist.name}
                    </span>
                    <span className="mmp-playlist-dialog__playlist-count">
                      {adding
                        ? "Adding…"
                        : `${playlist.track_count} ${playlist.track_count === 1 ? "track" : "tracks"}`}
                    </span>
                  </Button>
                );
              })}
            </div>
          )}
        </section>

        <section
          className="mmp-playlist-dialog__section mmp-playlist-dialog__new"
          aria-label="Create a new playlist"
        >
          <h3 className="mmp-playlist-dialog__heading">New playlist</h3>
          <form
            id={formID}
            className="mmp-playlist-dialog__form"
            onSubmit={(event) => void createWithTrack(event)}
            noValidate
          >
            <label className="mmp-playlist-dialog__field">
              New playlist name
              <TextField.Root
                aria-label="New playlist name"
                aria-describedby={error ? errorID : undefined}
                value={name}
                onChange={(event) => setName(event.target.value)}
                maxLength={100}
                required
                autoComplete="off"
                disabled={pending}
              />
            </label>
            <Button
              type="submit"
              className="mmp-playlist-dialog__create"
              disabled={pending || !hasTrackID}
            >
              {pendingAction === "create"
                ? "Creating…"
                : "Create playlist with track"}
            </Button>
          </form>
        </section>
        <DialogError id={errorID} message={error} />
      </div>
    </Modal>
  );
}

export function PlaylistRenameDialog({
  playlist,
  onOpenChange,
  onChanged,
}: {
  playlist: MusicPlaylistSummary | null;
  onOpenChange: (open: boolean) => void;
  onChanged: (details?: MusicPlaylistDetails) => Promise<void> | void;
}) {
  const open = playlist !== null;
  const formID = useId();
  const errorID = `${formID}-error`;
  const [name, setName] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (playlist) {
      setName(playlist.name);
      setError("");
    }
  }, [open, playlist?.id]);

  async function renamePlaylist(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending || !playlist) return;
    const trimmedName = name.trim();
    const validationError = playlistNameError(trimmedName);
    if (validationError) {
      setError(validationError);
      return;
    }

    const current = playlist;
    setPending(true);
    setError("");
    try {
      const details = await api<MusicPlaylistDetails>(
        `/library/music/playlists/${encodeURIComponent(String(current.id))}`,
        {
          method: "PUT",
          body: JSON.stringify({
            name: trimmedName,
            revision: current.revision,
          }),
        },
      );
      await onChanged(details);
      onOpenChange(false);
    } catch (caught) {
      setError(messageFor(caught));
    } finally {
      setPending(false);
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={(nextOpen) => {
        if (!pending) onOpenChange(nextOpen);
      }}
      title={`Rename ${playlist?.name ?? "playlist"}`}
      size="small"
      footer={
        <>
          <Button
            type="button"
            variant="soft"
            color="gray"
            disabled={pending}
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button type="submit" form={formID} disabled={pending}>
            {pending ? "Renaming…" : "Rename playlist"}
          </Button>
        </>
      }
    >
      <div className="mmp-playlist-dialog">
        <form
          id={formID}
          className="mmp-playlist-dialog__form"
          onSubmit={(event) => void renamePlaylist(event)}
          noValidate
        >
          <label className="mmp-playlist-dialog__field">
            Playlist name
            <TextField.Root
              aria-label="Playlist name"
              aria-describedby={error ? errorID : undefined}
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={100}
              required
              autoComplete="off"
              disabled={pending}
              autoFocus
            />
          </label>
        </form>
        <DialogError id={errorID} message={error} />
      </div>
    </Modal>
  );
}
