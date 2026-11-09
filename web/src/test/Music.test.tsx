import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Music, MusicAlbumPage, MusicArtistPage } from "../pages/Music";

function mockFetch(
  handler: (path: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const path = new URL(url, "http://local").pathname;
    return (
      handler(path, options) ??
      Response.json({ items: [], page: 1, total: 0, limit: 48 })
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("music", () => {
  it("adds an artist from MusicBrainz search", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/artists" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 48 });
      }
      if (path === "/api/v1/metadata/music/artists") {
        return Response.json({
          items: [
            {
              musicbrainz_id: "mbid",
              name: "Radiohead",
              type: "Group",
              country: "GB",
              in_library: false,
              album_count: 0,
              status: "missing",
            },
          ],
          page: 1,
          total_pages: 1,
          total_results: 1,
        });
      }
      if (
        path === "/api/v1/library/music/artists" &&
        options?.method === "POST"
      ) {
        return Response.json(
          { item: { id: 1, name: "Radiohead" }, created: true },
          { status: 201 },
        );
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music"]}>
          <Music />
        </MemoryRouter>
      </Theme>,
    );

    expect(await screen.findByRole("heading", { name: "Music" })).toBeVisible();
    expect(screen.getByText("No artists yet")).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Library Import" }),
    ).toHaveAttribute("href", "/music/import");
    expect(
      screen.getByRole("link", { name: "Import existing music" }),
    ).toHaveAttribute("href", "/music/import");
    await userEvent.click(
      screen.getAllByRole("button", { name: "Add artist" })[0],
    );
    await userEvent.type(screen.getByLabelText("Artist name"), "radiohead");
    await userEvent.click(screen.getByRole("button", { name: /Search/ }));
    expect(await screen.findByText("Radiohead")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/music/artists" && options?.method === "POST",
        ),
      ).toBe(true),
    );
  });

  it("renders saved artists as tiles", async () => {
    mockFetch((path) => {
      if (path === "/api/v1/library/music/artists") {
        return Response.json({
          items: [
            {
              id: 1,
              name: "Radiohead",
              type: "Group",
              country: "GB",
              status: "available",
              monitored: true,
              album_count: 3,
            },
          ],
          page: 1,
          total: 1,
          limit: 48,
        });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music"]}>
          <Music />
        </MemoryRouter>
      </Theme>,
    );

    expect(
      await screen.findByRole("link", { name: "Open Radiohead" }),
    ).toHaveAttribute("href", "/music/artists/1");
    expect(screen.getByRole("list", { name: "Music artists" })).toBeVisible();
    expect(screen.getByText("3 albums")).toBeVisible();
  });

  it("renders an artist's albums and top tracks", async () => {
    mockFetch((path) => {
      if (path === "/api/v1/library/music/artists/1") {
        return Response.json({
          artist: {
            id: 1,
            name: "Radiohead",
            type: "Group",
            country: "GB",
            status: "missing",
            album_count: 1,
          },
          albums: [
            {
              id: 2,
              title: "OK Computer",
              year: 1997,
              primary_type: "Album",
              secondary_types: [],
              track_count: 12,
              status: "missing",
              cover_url: "",
            },
          ],
        });
      }
      if (path === "/api/v1/library/music/artists/1/tracks") {
        return Response.json({
          items: [
            {
              id: 3,
              album_id: 2,
              disc: 1,
              number: 1,
              title: "Airbag",
              duration_ms: 284000,
              album_title: "OK Computer",
              album_year: 1997,
              cover_url: "",
            },
          ],
          page: 1,
          total: 20,
          limit: 15,
        });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/artists/1"]}>
          <Routes>
            <Route path="/music/artists/:id" element={<MusicArtistPage />} />
          </Routes>
        </MemoryRouter>
      </Theme>,
    );

    const albumLinks = await screen.findAllByRole("link", {
      name: "OK Computer",
    });
    expect(albumLinks[0]).toHaveAttribute("href", "/music/albums/2");
    expect(screen.getByRole("heading", { name: "Radiohead" })).toBeVisible();
    expect(
      await screen.findByRole("table", { name: "Top tracks" }),
    ).toBeVisible();
    expect(screen.getByText("Airbag")).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: /See every track/ }),
    );
    expect(
      await screen.findByRole("table", { name: "All tracks" }),
    ).toBeVisible();
  });

  it("renders album tracks with durations", async () => {
    mockFetch((path) => {
      if (path === "/api/v1/library/music/albums/2") {
        return Response.json({
          album: {
            id: 2,
            artist_id: 1,
            artist_name: "Radiohead",
            title: "OK Computer",
            year: 1997,
            primary_type: "Album",
            track_count: 1,
            status: "missing",
          },
          tracks: [
            {
              disc: 1,
              number: 1,
              title: "Airbag",
              duration_ms: 284000,
            },
          ],
        });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/albums/2"]}>
          <Routes>
            <Route path="/music/albums/:id" element={<MusicAlbumPage />} />
          </Routes>
        </MemoryRouter>
      </Theme>,
    );

    expect(
      await screen.findByRole("table", { name: "Music tracks" }),
    ).toBeVisible();
    expect(screen.getByText("Airbag")).toBeVisible();
    expect(screen.getByText("4:44")).toBeVisible();
  });

  it("searches releases and imports a completed folder", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/albums/2" && !options?.method) {
        return Response.json({
          album: {
            id: 2,
            artist_id: 1,
            artist_name: "Radiohead",
            title: "OK Computer",
            year: 1997,
            primary_type: "Album",
            track_count: 1,
            status: "missing",
          },
          tracks: [
            { disc: 1, number: 1, title: "Airbag", duration_ms: 284000 },
          ],
        });
      }
      if (
        path === "/api/v1/library/music/albums/2/releases" &&
        options?.method === "POST"
      ) {
        return Response.json({
          items: [
            {
              id: "release-1",
              protocol: "torrent",
              indexer_id: "fixture",
              indexer_name: "Fixture",
              freeleech: false,
              title: "Radiohead - OK Computer (1997) [FLAC]",
              size: 300000000,
              seeders: 12,
              peers: 2,
              published: "",
              info_url: "https://tracker.example/details/1",
              parsed_artist: "Radiohead",
              parsed_album: "OK Computer",
              year: 1997,
              format: "FLAC",
              bitrate: 0,
              lossless: true,
            },
          ],
        });
      }
      if (
        path === "/api/v1/library/music/albums/2/import" &&
        options?.method === "POST"
      ) {
        return Response.json(
          {
            id: 1,
            album_id: 2,
            root: "Music",
            source_path: "Album",
            state: "running",
            file_count: 1,
            imported_count: 0,
            error: "",
            created_at: 1,
            updated_at: 1,
          },
          { status: 202 },
        );
      }
      if (path === "/api/v1/library/music/imports/1" && !options?.method) {
        return Response.json({
          id: 1,
          album_id: 2,
          root: "Music",
          source_path: "Album",
          state: "completed",
          file_count: 1,
          imported_count: 1,
          error: "",
          created_at: 1,
          updated_at: 1,
          files: [
            {
              id: 1,
              source_path: "Album/01.flac",
              track_id: 1,
              destination_path: "Music/Radiohead/OK Computer/01 Airbag.flac",
              state: "imported",
              error: "",
            },
          ],
        });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/albums/2"]}>
          <Routes>
            <Route path="/music/albums/:id" element={<MusicAlbumPage />} />
          </Routes>
        </MemoryRouter>
      </Theme>,
    );

    expect(
      await screen.findByRole("table", { name: "Music tracks" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Find releases" }),
    );
    await userEvent.click(screen.getByRole("button", { name: /Search/ }));
    expect(
      await screen.findByRole("table", { name: "Music release results" }),
    ).toBeVisible();
    expect(
      screen.getByText("Radiohead - OK Computer (1997) [FLAC]"),
    ).toBeVisible();
    await userEvent.keyboard("{Escape}");
    await userEvent.click(
      screen.getByRole("button", { name: "Import folder" }),
    );
    await userEvent.type(screen.getByLabelText("Completed folder"), "Album");
    await userEvent.click(screen.getByRole("button", { name: "Import" }));
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/music/albums/2/import" &&
            options?.method === "POST",
        ),
      ).toBe(true),
    );
    expect(
      await screen.findByText("Music/Radiohead/OK Computer/01 Airbag.flac"),
    ).toBeVisible();
  });
});
