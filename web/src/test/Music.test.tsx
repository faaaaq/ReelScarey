import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicAlbumPage, MusicArtistPage } from "../pages/Music";
import { MusicLibrary } from "../pages/MusicLibrary";
import { MusicPlayerProvider } from "../musicPlayer";

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
  it("renders the unified music library views", async () => {
    mockFetch((path) => {
      if (path === "/api/v1/library/music/albums") {
        return Response.json({
          items: [
            {
              id: 2,
              artist_id: 1,
              artist_name: "Radiohead",
              title: "OK Computer",
              year: 1997,
              primary_type: "Album",
              secondary_types: [],
              track_count: 12,
              status: "available",
              cover_url: "",
            },
          ],
          page: 1,
          total: 1,
          limit: 48,
        });
      }
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
              album_count: 1,
            },
          ],
          page: 1,
          total: 1,
          limit: 48,
        });
      }
      if (path === "/api/v1/library/music/tracks") {
        return Response.json({
          items: [
            {
              id: 3,
              album_id: 2,
              artist_name: "Radiohead",
              disc: 1,
              number: 1,
              title: "Airbag",
              duration_ms: 284000,
              album_title: "OK Computer",
              album_year: 1997,
              cover_url: "",
              file_id: 9,
            },
          ],
          page: 1,
          total: 1,
          limit: 100,
        });
      }
      if (path === "/api/v1/library/music/acquisitions") {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      return undefined;
    });

    render(
      <Theme>
        <MusicPlayerProvider>
          <MemoryRouter initialEntries={["/music"]}>
            <MusicLibrary />
          </MemoryRouter>
        </MusicPlayerProvider>
      </Theme>,
    );

    // The library opens on the artist list, like the movie library.
    expect(
      await screen.findByRole("link", { name: "Open Radiohead" }),
    ).toHaveAttribute("href", "/music/artists/1");
    await userEvent.click(screen.getByRole("button", { name: "Albums" }));
    expect(
      (await screen.findAllByRole("link", { name: "OK Computer" }))[0],
    ).toHaveAttribute("href", "/music/albums/2");
    await userEvent.click(screen.getByRole("button", { name: "Tracks" }));
    expect(
      await screen.findByRole("table", { name: "Music tracks" }),
    ).toBeVisible();
    expect(screen.getByText("Airbag")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Artists" }));
    expect(
      await screen.findByRole("link", { name: "Open Radiohead" }),
    ).toHaveAttribute("href", "/music/artists/1");
  });

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
        <MusicPlayerProvider>
          <MemoryRouter initialEntries={["/music?view=artists"]}>
            <MusicLibrary />
          </MemoryRouter>
        </MusicPlayerProvider>
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
        <MusicPlayerProvider>
          <MemoryRouter initialEntries={["/music?view=artists"]}>
            <MusicLibrary />
          </MemoryRouter>
        </MusicPlayerProvider>
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
      if (path === "/api/v1/library/music/artists/1/discography") {
        return Response.json({
          source: "Deezer",
          message: "",
          items: [
            {
              provider_id: "deezer-album-1",
              title: "OK Computer",
              artist: "Radiohead",
              year: 1997,
              cover_url: "",
              track_count: 12,
              in_library: false,
              album_id: 2,
              status: "Album",
            },
          ],
        });
      }
      if (path === "/api/v1/library/music/artists/1/top-tracks") {
        return Response.json({
          source: "Deezer",
          message: "",
          items: [
            {
              provider_id: "deezer-1",
              title: "Airbag",
              artist: "Radiohead",
              album: "OK Computer",
              cover_url: "",
              duration_ms: 284000,
              in_library: false,
              file_id: null,
              album_id: null,
              track_id: null,
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
              file_id: 9,
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
        <MusicPlayerProvider>
          <MemoryRouter initialEntries={["/music/artists/1"]}>
            <Routes>
              <Route path="/music/artists/:id" element={<MusicArtistPage />} />
            </Routes>
          </MemoryRouter>
        </MusicPlayerProvider>
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
    expect(
      screen.getByRole("button", { name: "Search and download Airbag" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: /All library tracks/ }),
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
              file_id: 9,
            },
          ],
        });
      }
      return undefined;
    });

    render(
      <Theme>
        <MusicPlayerProvider>
          <MemoryRouter initialEntries={["/music/albums/2"]}>
            <Routes>
              <Route path="/music/albums/:id" element={<MusicAlbumPage />} />
            </Routes>
          </MemoryRouter>
        </MusicPlayerProvider>
      </Theme>,
    );

    expect(
      await screen.findByRole("table", { name: "Music tracks" }),
    ).toBeVisible();
    expect(screen.getByText("Airbag")).toBeVisible();
    expect(screen.getByText("4:44")).toBeVisible();
    expect(screen.getByRole("button", { name: "Play Airbag" })).toBeVisible();
  });

  it("corrects a mistaken album year through the reviewed edit plan", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/albums/2") {
        return Response.json({
          album: {
            id: 2,
            artist_id: 1,
            artist_name: "XG",
            title: "DEPENDS",
            year: 6666,
            primary_type: "Album",
            track_count: 1,
            status: "available",
          },
          tracks: [
            {
              disc: 1,
              number: 1,
              title: "S.A.D.",
              duration_ms: 202000,
              file_id: 9,
            },
          ],
        });
      }
      if (
        path === "/api/v1/library/music/edits/preview" &&
        options?.method === "POST"
      ) {
        const body = JSON.parse(String(options.body));
        expect(body.album_id).toBe(2);
        expect(body.year).toBe(2022);
        return Response.json({
          files: [
            {
              music_file_id: 9,
              source_path: "Music/XG/DEPENDS/01 S.A.D..flac",
              destination_path: "Music/XG/DEPENDS/01 S.A.D..flac",
              action: "retag",
              tags: {},
            },
          ],
          plan_hash: "abc123",
        });
      }
      if (
        path === "/api/v1/library/music/edits/apply" &&
        options?.method === "POST"
      ) {
        const body = JSON.parse(String(options.body));
        expect(body.plan_hash).toBe("abc123");
        expect(body.reviewed).toBe(true);
        return Response.json({
          id: 7,
          state: "completed",
          file_count: 1,
          error: "",
          created_at: 0,
          updated_at: 0,
        });
      }
      return undefined;
    });

    render(
      <Theme>
        <MusicPlayerProvider>
          <MemoryRouter initialEntries={["/music/albums/2"]}>
            <Routes>
              <Route path="/music/albums/:id" element={<MusicAlbumPage />} />
            </Routes>
          </MemoryRouter>
        </MusicPlayerProvider>
      </Theme>,
    );

    await screen.findByRole("table", { name: "Music tracks" });
    await userEvent.click(screen.getByRole("button", { name: "Fix metadata" }));
    const year = await screen.findByRole("textbox", { name: "Album year" });
    await userEvent.clear(year);
    await userEvent.type(year, "2022");
    await userEvent.click(
      screen.getByRole("button", { name: "Preview changes" }),
    );

    await screen.findByRole("table", {
      name: "Planned metadata corrections",
    });
    await userEvent.click(
      screen.getByRole("button", { name: /Apply to 1 file/ }),
    );

    await screen.findByText("Updated 1 file.");
    const calls = fetch.mock.calls.map(([url]) => String(url));
    expect(calls.some((url) => url.includes("/edits/preview"))).toBe(true);
    expect(calls.some((url) => url.includes("/edits/apply"))).toBe(true);
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
        <MusicPlayerProvider>
          <MemoryRouter initialEntries={["/music/albums/2"]}>
            <Routes>
              <Route path="/music/albums/:id" element={<MusicAlbumPage />} />
            </Routes>
          </MemoryRouter>
        </MusicPlayerProvider>
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

  it("shows album download progress in the popup and on the page", async () => {
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
            track_count: 2,
            status: "missing",
          },
          tracks: [
            { disc: 1, number: 1, title: "Airbag", duration_ms: 284000 },
            {
              disc: 1,
              number: 2,
              title: "Paranoid Android",
              duration_ms: 383000,
            },
          ],
        });
      }
      if (
        path === "/api/v1/library/music/albums/2/acquire" &&
        options?.method === "POST"
      ) {
        return Response.json(
          {
            id: 77,
            source_url: "",
            source_kind: "album",
            title: "OK Computer",
            root: "Music",
            format: "source",
            state: "queued",
            track_count: 2,
            completed_count: 0,
            error: "",
            created_at: 1,
            updated_at: 1,
          },
          { status: 202 },
        );
      }
      if (
        path === "/api/v1/library/music/acquisitions/77" &&
        !options?.method
      ) {
        return Response.json({
          id: 77,
          source_url: "",
          source_kind: "album",
          title: "OK Computer",
          root: "Music",
          format: "source",
          state: "running",
          track_count: 2,
          completed_count: 1,
          error: "",
          created_at: 1,
          updated_at: 2,
          tracks: [
            {
              id: 1,
              job_id: 77,
              position: 0,
              title: "Airbag",
              artist: "Radiohead",
              album: "OK Computer",
              album_artist: "Radiohead",
              year: 1997,
              track_number: 1,
              disc_number: 1,
              duration_ms: 284000,
              cover_url: "",
              source_url: "",
              search_query: "Radiohead Airbag",
              state: "completed",
              error: "",
              music_file_id: 5,
              import_job_id: null,
            },
            {
              id: 2,
              job_id: 77,
              position: 1,
              title: "Paranoid Android",
              artist: "Radiohead",
              album: "OK Computer",
              album_artist: "Radiohead",
              year: 1997,
              track_number: 2,
              disc_number: 1,
              duration_ms: 383000,
              cover_url: "",
              source_url: "",
              search_query: "Radiohead Paranoid Android",
              state: "downloading",
              error: "",
              music_file_id: null,
              import_job_id: null,
            },
          ],
        });
      }
      return undefined;
    });

    render(
      <Theme>
        <MusicPlayerProvider>
          <MemoryRouter initialEntries={["/music/albums/2"]}>
            <Routes>
              <Route path="/music/albums/:id" element={<MusicAlbumPage />} />
            </Routes>
          </MemoryRouter>
        </MusicPlayerProvider>
      </Theme>,
    );

    expect(
      await screen.findByRole("table", { name: "Music tracks" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Download album" }),
    );

    const dialog = await screen.findByRole("dialog");
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Download album" }),
    );
    expect(
      fetch.mock.calls.some(
        ([url, options]) =>
          new URL(url, "http://local").pathname ===
            "/api/v1/library/music/albums/2/acquire" &&
          options?.method === "POST",
      ),
    ).toBe(true);

    // The popup confirms the match and shows live progress.
    expect(await within(dialog).findByText("Matching queued")).toBeVisible();
    expect(within(dialog).getByText("1/2 tracks completed")).toBeVisible();
    expect(
      within(dialog).getByText(/Downloading 2 of 2 . Paranoid Android/),
    ).toBeVisible();
    expect(
      within(dialog).getByRole("progressbar", {
        name: "Album download progress",
      }),
    ).toHaveAttribute("aria-valuenow", "50");

    // Closing the popup keeps the progress on the page.
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(
      screen.getByRole("heading", { name: /Album download/ }),
    ).toBeVisible();
    expect(screen.getByText("1/2 tracks completed")).toBeVisible();
    expect(
      screen.getByText(/Downloading 2 of 2 . Paranoid Android/),
    ).toBeVisible();
  });
});
