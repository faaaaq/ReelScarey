import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  PlaylistCreateDialog,
  PlaylistRenameDialog,
  PlaylistTrackPickerDialog,
} from "../components/MusicPlaylistDialogs";
import {
  APIError,
  api,
  type MusicArtistTrack,
  type MusicPlaylistSummary,
} from "../api";

vi.mock("../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api")>();
  return { ...actual, api: vi.fn() };
});

const apiMock = vi.mocked(api);

const track: MusicArtistTrack = {
  id: 42,
  musicbrainz_id: "track-mbid",
  recording_id: "recording-mbid",
  disc: 1,
  number: 3,
  title: "A Good Night",
  duration_ms: 185000,
  album_title: "Test Album",
  album_year: 2025,
  cover_url: "",
};

const playlist: MusicPlaylistSummary = {
  id: 7,
  name: "Evening",
  track_count: 2,
  duration_ms: 370000,
  revision: 4,
  created_at: 100,
  updated_at: 200,
};

describe("music playlist dialogs", () => {
  beforeEach(() => {
    apiMock.mockReset();
  });

  it("creates a trimmed playlist and closes only after the refresh", async () => {
    let finishRefresh: (() => void) | undefined;
    const onCreated = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finishRefresh = resolve;
        }),
    );
    const onOpenChange = vi.fn();
    apiMock.mockResolvedValue({});
    const user = userEvent.setup();
    render(
      <PlaylistCreateDialog
        open
        onOpenChange={onOpenChange}
        onCreated={onCreated}
      />,
    );

    await user.type(screen.getByLabelText("Playlist name"), "  Road trip  ");
    await user.click(screen.getByRole("button", { name: "Create playlist" }));

    expect(apiMock).toHaveBeenCalledWith("/library/music/playlists", {
      method: "POST",
      body: JSON.stringify({ name: "Road trip" }),
    });
    expect(onCreated).toHaveBeenCalledTimes(1);
    expect(
      await screen.findByRole("button", { name: "Creating…" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(onOpenChange).not.toHaveBeenCalled();

    finishRefresh?.();
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("adds a track to an existing playlist with its revision", async () => {
    const onChanged = vi.fn().mockResolvedValue(undefined);
    const onOpenChange = vi.fn();
    apiMock.mockResolvedValue({});
    const user = userEvent.setup();
    render(
      <PlaylistTrackPickerDialog
        track={track}
        playlists={[playlist]}
        onOpenChange={onOpenChange}
        onChanged={onChanged}
      />,
    );

    expect(
      screen.getByRole("heading", { name: "Add A Good Night to playlist" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Evening 2 tracks" }));

    expect(apiMock).toHaveBeenCalledWith("/library/music/playlists/7/tracks", {
      method: "POST",
      body: JSON.stringify({ track_id: 42, revision: 4 }),
    });
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("creates a playlist with the selected track", async () => {
    const onChanged = vi.fn().mockResolvedValue(undefined);
    const onOpenChange = vi.fn();
    apiMock.mockResolvedValue({});
    const user = userEvent.setup();
    render(
      <PlaylistTrackPickerDialog
        track={track}
        playlists={[]}
        onOpenChange={onOpenChange}
        onChanged={onChanged}
      />,
    );

    expect(screen.getByText("No playlists yet")).toBeInTheDocument();
    await user.type(screen.getByLabelText("New playlist name"), "  New mix  ");
    await user.click(
      screen.getByRole("button", { name: "Create playlist with track" }),
    );

    expect(apiMock).toHaveBeenCalledWith("/library/music/playlists", {
      method: "POST",
      body: JSON.stringify({ name: "New mix", track_id: 42 }),
    });
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("renames a playlist with its revision", async () => {
    const onChanged = vi.fn().mockResolvedValue(undefined);
    const onOpenChange = vi.fn();
    apiMock.mockResolvedValue({});
    const user = userEvent.setup();
    render(
      <PlaylistRenameDialog
        playlist={playlist}
        onOpenChange={onOpenChange}
        onChanged={onChanged}
      />,
    );

    const input = screen.getByLabelText("Playlist name");
    expect(input).toHaveValue("Evening");
    await user.clear(input);
    await user.type(input, "  Late evening  ");
    await user.click(screen.getByRole("button", { name: "Rename playlist" }));

    expect(apiMock).toHaveBeenCalledWith("/library/music/playlists/7", {
      method: "PUT",
      body: JSON.stringify({ name: "Late evening", revision: 4 }),
    });
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("keeps a duplicate track error open and refreshes the summaries", async () => {
    const onChanged = vi.fn().mockResolvedValue(undefined);
    const onOpenChange = vi.fn();
    apiMock.mockRejectedValue(
      new APIError(
        "This track is already in the playlist.",
        409,
        "music_playlist_track_exists",
      ),
    );
    const user = userEvent.setup();
    render(
      <PlaylistTrackPickerDialog
        track={track}
        playlists={[playlist]}
        onOpenChange={onOpenChange}
        onChanged={onChanged}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Evening 2 tracks" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This track is already in the playlist.",
    );
    expect(
      screen.getByRole("heading", { name: "Add A Good Night to playlist" }),
    ).toBeInTheDocument();
    expect(onOpenChange).not.toHaveBeenCalled();
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
  });

  it("keeps a rename revision failure open with the server message", async () => {
    const onChanged = vi.fn();
    const onOpenChange = vi.fn();
    apiMock.mockRejectedValue(
      new APIError(
        "The playlist changed. Reload and try again.",
        409,
        "music_revision_conflict",
      ),
    );
    const user = userEvent.setup();
    render(
      <PlaylistRenameDialog
        playlist={playlist}
        onOpenChange={onOpenChange}
        onChanged={onChanged}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Rename playlist" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The playlist changed. Reload and try again.",
    );
    expect(screen.getByLabelText("Playlist name")).toHaveValue("Evening");
    expect(onChanged).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("disables playlist mutations for a provider-only track", () => {
    render(
      <PlaylistTrackPickerDialog
        track={{ ...track, id: undefined }}
        playlists={[playlist]}
        onOpenChange={vi.fn()}
        onChanged={vi.fn()}
      />,
    );

    expect(
      screen.getByRole("button", { name: "Evening 2 tracks" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Create playlist with track" }),
    ).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "This track is not in the library",
    );
  });

  it("resets the create form when reopened", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <PlaylistCreateDialog open onOpenChange={vi.fn()} onCreated={vi.fn()} />,
    );
    await user.type(screen.getByLabelText("Playlist name"), "Temporary");

    rerender(
      <PlaylistCreateDialog
        open={false}
        onOpenChange={vi.fn()}
        onCreated={vi.fn()}
      />,
    );
    rerender(
      <PlaylistCreateDialog open onOpenChange={vi.fn()} onCreated={vi.fn()} />,
    );

    expect(screen.getByLabelText("Playlist name")).toHaveValue("");
  });
});
