import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicImport } from "../pages/MusicImport";

const scan = {
  id: 1,
  root: "Music",
  state: "review",
  file_count: 2,
  matched_count: 1,
  unresolved_count: 1,
  registered_count: 0,
  error: "",
  created_at: 1,
  updated_at: 1,
};

const matched = {
  id: 11,
  scan_id: 1,
  relative_path: "Music/Artist/Album/01 - Song.flac",
  size_bytes: 100,
  modified_at: 1,
  sha256: "",
  format: "flac",
  duration_ms: 1000,
  bitrate: 0,
  sample_rate: 44100,
  channels: 2,
  tag_artist: "Artist",
  tag_album_artist: "Artist",
  tag_album: "Album",
  tag_title: "Song",
  tag_track: 1,
  tag_disc: 1,
  tag_year: 2020,
  tag_musicbrainz_artist: "",
  tag_musicbrainz_album: "",
  tag_musicbrainz_release_group: "",
  tag_musicbrainz_track: "",
  tag_musicbrainz_recording: "",
  tag_json: "{}",
  artist_id: 1,
  album_id: 2,
  track_id: 3,
  state: "matched",
  error: "",
};

const unresolved = {
  ...matched,
  id: 12,
  relative_path: "Music/Unknown/02 - Other.mp3",
  tag_artist: "",
  tag_album_artist: "",
  tag_album: "",
  tag_title: "Other",
  tag_track: 2,
  artist_id: null,
  album_id: null,
  track_id: null,
  state: "unresolved",
  error: "No catalog album matches this file's tags.",
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

describe("music import", () => {
  it("registers selected matched files", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return Response.json({ items: [scan], page: 1, total: 1, limit: 20 });
      }
      if (path === "/api/v1/library/music/scans/1" && !options?.method) {
        return Response.json({
          scan,
          items: [matched, unresolved],
          page: 1,
          total: 2,
          limit: 100,
        });
      }
      if (
        path === "/api/v1/library/music/scans/1/register" &&
        options?.method === "POST"
      ) {
        return Response.json({ registered: 1 });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/import"]}>
          <MusicImport />
        </MemoryRouter>
      </Theme>,
    );

    expect(
      await screen.findByRole("table", { name: "Scanned music files" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByLabelText("Select Music/Artist/Album/01 - Song.flac"),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /Register selected/ }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/music/scans/1/register" &&
            options?.method === "POST",
        ),
      ).toBe(true),
    );
  });

  it("starts a scan from the empty state", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (
        path === "/api/v1/library/music/scans" &&
        options?.method === "POST"
      ) {
        return Response.json(scan, { status: 202 });
      }
      if (path === "/api/v1/library/music/scans/1" && !options?.method) {
        return Response.json({
          scan,
          items: [],
          page: 1,
          total: 0,
          limit: 100,
        });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/import"]}>
          <MusicImport />
        </MemoryRouter>
      </Theme>,
    );

    expect(await screen.findByText("No scans yet")).toBeVisible();
    await userEvent.click(
      screen.getAllByRole("button", { name: "Scan folder" })[0],
    );
    await userEvent.clear(screen.getByLabelText("Music folder"));
    await userEvent.type(screen.getByLabelText("Music folder"), "Music");
    await userEvent.click(screen.getByRole("button", { name: "Start scan" }));
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/music/scans" && options?.method === "POST",
        ),
      ).toBe(true),
    );
  });
});
