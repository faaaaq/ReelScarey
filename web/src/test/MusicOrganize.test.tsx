import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicOrganize } from "../pages/MusicOrganize";

const file = {
  id: 1,
  artist_id: 1,
  album_id: 2,
  track_id: 3,
  artist_name: "Artist",
  album_title: "Album",
  track_title: "Song",
  disc: 1,
  number: 1,
  relative_path: "Music/Artist/Album/Song.flac",
  format: "flac",
  duration_ms: 1000,
  size_bytes: 7,
};

const plan = {
  files: [
    {
      music_file_id: 1,
      source_path: "Music/Artist/Album/Song.flac",
      destination_path: "Music/Artist/Album/01 Song.flac",
      action: "move_retag",
      tags: {
        artist: "Artist",
        album_artist: "Artist",
        album: "Album",
        title: "Song",
        track: 1,
        track_total: 1,
        disc: 1,
        disc_total: 1,
        year: 0,
        musicbrainz_artist_id: "",
        musicbrainz_album_id: "",
        musicbrainz_release_group_id: "",
        musicbrainz_track_id: "",
        musicbrainz_recording_id: "",
      },
    },
  ],
  plan_hash: "hash",
};

const job = {
  id: 1,
  state: "running",
  file_count: 1,
  error: "",
  created_at: 1,
  updated_at: 1,
};

function mockFetch(
  handler: (path: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const path = new URL(url, "http://local").pathname;
    return handler(path, options) ?? Response.json({});
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("music organize", () => {
  it("previews and applies a reviewed organization plan", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/files" && !options?.method) {
        return Response.json({ items: [file], page: 1, total: 1, limit: 100 });
      }
      if (path === "/api/v1/library/music/edits" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (
        path === "/api/v1/library/music/edits/preview" &&
        options?.method === "POST"
      ) {
        return Response.json(plan);
      }
      if (
        path === "/api/v1/library/music/edits/apply" &&
        options?.method === "POST"
      ) {
        return Response.json(job, { status: 202 });
      }
      if (path === "/api/v1/library/music/edits/1" && !options?.method) {
        return Response.json({ ...job, files: [] });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/organize"]}>
          <MusicOrganize />
        </MemoryRouter>
      </Theme>,
    );

    expect(
      await screen.findByRole("table", { name: "Registered music files" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByLabelText("Select Music/Artist/Album/Song.flac"),
    );
    await userEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(
      await screen.findByRole("table", { name: "Music organization plan" }),
    ).toBeVisible();
    expect(screen.getByText("move_retag")).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Apply reviewed plan" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/music/edits/apply" && options?.method === "POST",
        ),
      ).toBe(true),
    );
  });
});
