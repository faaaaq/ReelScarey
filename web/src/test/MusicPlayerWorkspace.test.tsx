import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { MusicAlbum, MusicArtist, MusicArtistTrack } from "../api";
import { MusicPlayerProvider } from "../musicPlayer";
import { MusicPlayerWorkspace } from "../pages/MusicPlayerWorkspace";

const ARTIST: MusicArtist = {
  id: 7,
  musicbrainz_id: "artist-7",
  name: "North Sea",
  sort_name: "North Sea",
  disambiguation: "",
  country: "GB",
  type: "Group",
  overview: "",
  image_url: "",
  monitored: true,
  status: "continuing",
  album_count: 1,
};

const ALBUM: MusicAlbum = {
  id: 11,
  artist_id: 7,
  artist_name: "North Sea",
  musicbrainz_id: "album-11",
  title: "Harbor Lights",
  primary_type: "Album",
  secondary_types: [],
  year: 2021,
  release_date: "2021-05-01",
  cover_url: "",
  monitored: true,
  status: "released",
  track_count: 4,
};

const SECOND_PAGE_ALBUM: MusicAlbum = {
  ...ALBUM,
  id: 12,
  musicbrainz_id: "album-12",
  title: "Second Page",
};

const TRACKS: MusicArtistTrack[] = [
  {
    id: 1001,
    album_id: 11,
    musicbrainz_id: "track-1001",
    recording_id: "recording-1001",
    disc: 1,
    number: 1,
    title: "First Light",
    artist_name: "North Sea",
    album_title: "Harbor Lights",
    album_year: 2021,
    cover_url: "",
    duration_ms: 183_000,
    file_id: 101,
    bitrate: 921_000,
    size_bytes: 1_048_576,
    format: "flac",
    relative_path: "North Sea/Harbor Lights/01 First Light.flac",
  },
  {
    id: 1002,
    album_id: 11,
    musicbrainz_id: "track-1002",
    recording_id: "recording-1002",
    disc: 1,
    number: 2,
    title: "Second Wind",
    artist_name: "North Sea",
    album_title: "Harbor Lights",
    album_year: 2021,
    cover_url: "",
    duration_ms: 184_000,
    file_id: 102,
    bitrate: 256,
    size_bytes: 2_097_152,
    format: "mp3",
    relative_path: "North Sea/Harbor Lights/02 Second Wind.mp3",
  },
  {
    id: 1003,
    album_id: 11,
    musicbrainz_id: "track-1003",
    recording_id: "recording-1003",
    disc: 1,
    number: 3,
    title: "Third Eye",
    artist_name: "North Sea",
    album_title: "Harbor Lights",
    album_year: 2021,
    cover_url: "",
    duration_ms: 185_000,
    file_id: 103,
    bitrate: 1_411_000,
    size_bytes: 3_145_728,
    format: "wav",
    relative_path: "North Sea/Harbor Lights/03 Third Eye.wav",
  },
  {
    id: 1004,
    album_id: 11,
    musicbrainz_id: "track-1004",
    recording_id: "recording-1004",
    disc: 1,
    number: 4,
    title: "Provider Preview",
    artist_name: "North Sea",
    album_title: "Harbor Lights",
    album_year: 2021,
    cover_url: "",
    duration_ms: 186_000,
    file_id: null,
  },
];

function responsePage(url: URL) {
  return Number(url.searchParams.get("page") || "1");
}

type WorkspaceFetchOptions = {
  globalTracks?: MusicArtistTrack[];
  artistTracks?: MusicArtistTrack[];
  albumTracks?: MusicArtistTrack[];
  failGlobalPageOnce?: number;
  globalPageTwoGate?: Promise<void>;
};

function generatedTracks(
  prefix: string,
  count: number,
  firstID = 2000,
): MusicArtistTrack[] {
  return Array.from({ length: count }, (_, index) => {
    const id = firstID + index;
    const title = `${prefix} ${String(index + 1).padStart(3, "0")}`;
    return {
      ...TRACKS[0],
      id,
      musicbrainz_id: `track-${id}`,
      recording_id: `recording-${id}`,
      number: index + 1,
      title,
      file_id: id + 10_000,
      relative_path: `North Sea/Harbor Lights/${title}.flac`,
    };
  });
}

function installWorkspaceFetch(options: WorkspaceFetchOptions = {}) {
  let globalFailureConsumed = false;
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const requestedURL = String(input);
    const url = new URL(requestedURL, "http://workspace.test");
    const path = url.pathname;

    if (path === "/api/v1/library/music/artists") {
      return Response.json({
        items: [ARTIST],
        page: responsePage(url),
        total: 1,
        limit: 48,
      });
    }
    if (path === "/api/v1/library/music/albums") {
      const page = responsePage(url);
      return Response.json({
        items: page === 2 ? [SECOND_PAGE_ALBUM] : [ALBUM],
        page,
        total: 96,
        limit: 48,
      });
    }
    if (path === "/api/v1/library/music/playlists") {
      return Response.json({ items: [] });
    }
    if (path === "/api/v1/library/music/tracks") {
      const items = options.globalTracks ?? TRACKS;
      const page = responsePage(url);
      if (page === options.failGlobalPageOnce && !globalFailureConsumed) {
        globalFailureConsumed = true;
        return Response.json(
          { error: { message: "The later track page is unavailable." } },
          { status: 503 },
        );
      }
      if (page === 2 && options.globalPageTwoGate) {
        await options.globalPageTwoGate;
      }
      const limit = Number(url.searchParams.get("limit") || "100");
      return Response.json({
        items: items.slice((page - 1) * limit, page * limit),
        page,
        total: items.length,
        limit,
      });
    }
    if (path === "/api/v1/library/music/artists/7") {
      return Response.json({ artist: ARTIST, albums: [ALBUM] });
    }
    if (path === "/api/v1/library/music/artists/7/tracks") {
      const items = options.artistTracks ?? [TRACKS[0]];
      const page = responsePage(url);
      const limit = Number(url.searchParams.get("limit") || "100");
      return Response.json({
        items: items.slice((page - 1) * limit, page * limit),
        page,
        total: items.length,
        limit,
      });
    }
    if (path === "/api/v1/library/music/albums/11") {
      const tracks = options.albumTracks ?? TRACKS;
      return Response.json({
        album: { ...ALBUM, track_count: tracks.length },
        tracks,
      });
    }
    if (
      /^\/api\/v1\/library\/music\/(albums\/\d+\/cover|artists\/\d+\/image)$/.test(
        path,
      )
    ) {
      return Response.json(
        { error: { message: "Artwork is unavailable." } },
        { status: 404 },
      );
    }
    throw new Error(`Unexpected workspace request: ${requestedURL}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function LocationProbe() {
  const location = useLocation();
  return (
    <output aria-label="Current URL">
      {location.pathname}
      {location.search}
    </output>
  );
}

function currentURL() {
  return new URL(
    screen.getByLabelText("Current URL").textContent || "",
    "http://workspace.test",
  );
}

function renderWorkspace(entry: string, options: WorkspaceFetchOptions = {}) {
  const fetchMock = installWorkspaceFetch(options);
  render(
    <Theme>
      <MusicPlayerProvider>
        <MemoryRouter initialEntries={[entry]}>
          <Routes>
            <Route path="/music/player" element={<MusicPlayerWorkspace />} />
          </Routes>
          <LocationProbe />
        </MemoryRouter>
      </MusicPlayerProvider>
    </Theme>,
  );
  return fetchMock;
}

function requestedURLs(fetchMock: ReturnType<typeof installWorkspaceFetch>) {
  return fetchMock.mock.calls.map(([input]) => String(input));
}

function queueTitles() {
  const queue = screen.getByRole("list", { name: "Playback queue" });
  return within(queue)
    .getAllByRole("listitem")
    .map(
      (item) =>
        within(item)
          .getByRole("button", { name: /^Play / })
          .getAttribute("aria-label")
          ?.replace(/^Play /, "") || "",
    );
}

describe("MusicPlayerWorkspace", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("plays registered tracks and maintains an honest shared queue", async () => {
    const user = userEvent.setup();
    renderWorkspace("/music/player");

    expect(
      screen.getByRole("complementary", { name: "Music library browser" }),
    ).toBeVisible();
    expect(
      await screen.findByRole("heading", { name: "All tracks", level: 1 }),
    ).toBeVisible();
    expect(
      screen.getByRole("complementary", { name: "Now playing and queue" }),
    ).toBeVisible();

    const table = screen.getByRole("table", { name: "Music tracks" });
    for (const heading of [
      "Title",
      "Artist",
      "Album",
      "Year",
      "Duration",
      "Bitrate",
      "File size",
      "File type",
    ]) {
      expect(
        within(table).getByRole("columnheader", { name: heading }),
      ).toBeVisible();
    }
    const firstRow = within(table).getByRole("row", { name: /First Light/ });
    for (const value of [
      "First Light",
      "North Sea",
      "Harbor Lights",
      "2021",
      "3:03",
      "921 kbps",
      "1.0 MiB",
      "FLAC",
    ]) {
      expect(within(firstRow).getByRole("cell", { name: value })).toBeVisible();
    }

    expect(screen.getByText("3 of 4 loaded tracks can play")).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: "Play Provider Preview",
      }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", {
        name: "Play Provider Preview next",
      }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", {
        name: "Add Provider Preview to queue",
      }),
    ).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Play First Light" }));
    const nowPlaying = screen.getByRole("complementary", {
      name: "Now playing and queue",
    });
    const current = await within(nowPlaying).findByRole("region", {
      name: "Current track",
    });
    expect(
      within(current).getByRole("heading", { name: "First Light" }),
    ).toBeVisible();
    expect(within(current).getByText("North Sea")).toBeVisible();
    expect(within(nowPlaying).getByText("3 tracks")).toBeVisible();
    expect(queueTitles()).toEqual(["First Light", "Second Wind", "Third Eye"]);

    // Isolate play-next and append behavior from the initial page-wide play.
    await user.click(
      screen.getByRole("button", { name: "Remove Second Wind" }),
    );
    await user.click(screen.getByRole("button", { name: "Remove Third Eye" }));
    expect(queueTitles()).toEqual(["First Light"]);

    await user.click(
      screen.getByRole("button", { name: "Play Third Eye next" }),
    );
    expect(queueTitles()).toEqual(["First Light", "Third Eye"]);
    await user.click(
      screen.getByRole("button", {
        name: "Add Second Wind to queue",
      }),
    );
    expect(queueTitles()).toEqual(["First Light", "Third Eye", "Second Wind"]);
    expect(within(nowPlaying).getByText("3 tracks")).toBeVisible();
    expect(
      within(current).getByRole("heading", { name: "First Light" }),
    ).toBeVisible();
  });

  it("navigates artist and album previews with detail links and working back controls", async () => {
    const user = userEvent.setup();
    const fetchMock = renderWorkspace("/music/player?view=artists");

    const artistsCenter = await screen.findByRole("region", {
      name: "Artists",
    });
    await user.click(
      within(artistsCenter).getByRole("button", { name: /North Sea/ }),
    );

    expect(
      await screen.findByRole("heading", { name: "North Sea", level: 2 }),
    ).toBeVisible();
    expect(currentURL().pathname).toBe("/music/player");
    expect(currentURL().searchParams.get("view")).toBe("artists");
    expect(currentURL().searchParams.get("artist")).toBe("7");
    await waitFor(() => {
      expect(requestedURLs(fetchMock)).toEqual(
        expect.arrayContaining([
          "/api/v1/library/music/artists/7",
          "/api/v1/library/music/artists/7/tracks?limit=100&page=1",
        ]),
      );
    });
    const artistTracks = screen.getByRole("table", {
      name: "North Sea tracks",
    });
    expect(
      within(artistTracks).getByRole("row", { name: /First Light/ }),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Open full artist page" }),
    ).toHaveAttribute("href", "/music/artists/7");
    expect(
      screen.getByRole("link", { name: "Artist details" }),
    ).toHaveAttribute("href", "/music/artists/7");

    await user.click(screen.getByRole("button", { name: "All artists" }));
    expect(
      await screen.findByRole("heading", { name: "Artists", level: 1 }),
    ).toBeVisible();
    expect(currentURL().search).toBe("?view=artists");

    const artistCards = screen.getByRole("region", { name: "Artists" });
    await user.click(
      within(artistCards).getByRole("button", { name: /North Sea/ }),
    );
    const artistAlbums = await screen.findByRole("region", {
      name: "Albums by this artist",
    });
    await user.click(
      within(artistAlbums).getByRole("button", { name: /Harbor Lights/ }),
    );

    expect(
      await screen.findByRole("heading", { name: "Harbor Lights", level: 2 }),
    ).toBeVisible();
    expect(currentURL().searchParams.get("view")).toBe("albums");
    expect(currentURL().searchParams.get("album")).toBe("11");
    expect(currentURL().searchParams.get("artist")).toBeNull();
    await waitFor(() =>
      expect(requestedURLs(fetchMock)).toContain(
        "/api/v1/library/music/albums/11",
      ),
    );
    expect(screen.getByText("North Sea · 2021 · 4 tracks")).toBeVisible();
    const albumTracks = screen.getByRole("table", {
      name: "Harbor Lights tracks",
    });
    expect(
      within(albumTracks).getByRole("row", { name: /Provider Preview/ }),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Open full album page" }),
    ).toHaveAttribute("href", "/music/albums/11");
    expect(screen.getByRole("link", { name: "Album details" })).toHaveAttribute(
      "href",
      "/music/albums/11",
    );

    await user.click(screen.getByRole("button", { name: "All albums" }));
    expect(
      await screen.findByRole("heading", { name: "Albums", level: 1 }),
    ).toBeVisible();
    expect(currentURL().search).toBe("?view=albums");
    const albumsCenter = screen.getByRole("region", { name: "Albums" });
    expect(
      within(albumsCenter).getByRole("button", { name: /Harbor Lights/ }),
    ).toBeVisible();
  });

  it("resets and encodes searches, paginates albums, and toggles the queue pane", async () => {
    const user = userEvent.setup();
    const fetchMock = renderWorkspace(
      "/music/player?view=artists&query=old&page=2",
    );

    expect(
      await screen.findByRole("heading", { name: "Artists", level: 1 }),
    ).toBeVisible();
    const queueToggle = screen.getByRole("button", {
      name: "Queue",
    });
    expect(queueToggle).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByRole("complementary", { name: "Now playing and queue" }),
    ).toBeVisible();
    await user.click(queueToggle);
    expect(queueToggle).toHaveAttribute("aria-expanded", "false");
    expect(
      screen.queryByRole("complementary", { name: "Now playing and queue" }),
    ).not.toBeInTheDocument();
    await user.click(queueToggle);
    expect(queueToggle).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByRole("complementary", { name: "Now playing and queue" }),
    ).toBeVisible();

    const search = screen.getByRole("textbox", { name: "Search music" });
    await user.clear(search);
    await user.type(search, "Björk & Co");
    await user.click(screen.getByRole("button", { name: "Search" }));

    expect(
      await screen.findByRole("heading", { name: "All tracks", level: 1 }),
    ).toBeVisible();
    expect(currentURL().searchParams.get("view")).toBeNull();
    expect(currentURL().searchParams.get("query")).toBe("Björk & Co");
    expect(currentURL().searchParams.get("page")).toBeNull();
    const collections = screen.getByRole("navigation", {
      name: "Music collections",
    });
    expect(
      within(collections).getByRole("button", { name: /^All tracks/ }),
    ).toHaveAttribute("aria-current", "page");
    const encodedQuery = "query=Bj%C3%B6rk+%26+Co";
    await waitFor(() =>
      expect(requestedURLs(fetchMock)).toContain(
        `/api/v1/library/music/tracks?page=1&limit=100&${encodedQuery}`,
      ),
    );

    await user.click(
      within(collections).getByRole("button", { name: /^Albums/ }),
    );
    expect(
      await screen.findByRole("heading", { name: "Albums", level: 1 }),
    ).toBeVisible();
    expect(currentURL().searchParams.get("view")).toBe("albums");
    expect(currentURL().searchParams.get("query")).toBe("Björk & Co");
    expect(currentURL().searchParams.get("page")).toBeNull();
    await waitFor(() =>
      expect(requestedURLs(fetchMock)).toContain(
        `/api/v1/library/music/albums?page=1&limit=48&${encodedQuery}`,
      ),
    );

    const pagination = await screen.findByRole("navigation", {
      name: "Pagination",
    });
    expect(within(pagination).getByText("Page 1 of 2")).toBeVisible();
    await user.click(within(pagination).getByRole("button", { name: "Next" }));
    expect(await screen.findByText("Page 2 of 2")).toBeVisible();
    expect(currentURL().searchParams.get("view")).toBe("albums");
    expect(currentURL().searchParams.get("query")).toBe("Björk & Co");
    expect(currentURL().searchParams.get("page")).toBe("2");
    await waitFor(() =>
      expect(requestedURLs(fetchMock)).toContain(
        `/api/v1/library/music/albums?page=2&limit=48&${encodedQuery}`,
      ),
    );
  });

  it("shuffles the complete filtered global list from a later visible page", async () => {
    const user = userEvent.setup();
    const tracks = generatedTracks("Global", 105);
    const fetchMock = renderWorkspace(
      "/music/player?query=Bj%C3%B6rk+%26+Co&page=2",
      { globalTracks: tracks },
    );

    expect(
      await screen.findByRole("row", { name: /Global 105/ }),
    ).toBeVisible();
    expect(screen.getByText("Page 2 of 2")).toBeVisible();
    fetchMock.mockClear();

    await user.click(screen.getByRole("button", { name: "Shuffle" }));

    await waitFor(() => expect(queueTitles()).toHaveLength(tracks.length));
    expect([...queueTitles()].sort()).toEqual(
      tracks.map((track) => track.title).sort(),
    );
    const encodedQuery = "query=Bj%C3%B6rk+%26+Co";
    expect(
      requestedURLs(fetchMock).filter((url) =>
        url.startsWith("/api/v1/library/music/tracks?"),
      ),
    ).toEqual([
      `/api/v1/library/music/tracks?page=1&limit=100&${encodedQuery}`,
      `/api/v1/library/music/tracks?page=2&limit=100&${encodedQuery}`,
    ]);
    expect(queueTitles()).toContain("Global 105");
  });

  it("loads every artist page for Shuffle", async () => {
    const user = userEvent.setup();
    const artistTracks = generatedTracks("Artist", 105, 4000);
    const artistFetch = renderWorkspace("/music/player?view=artists&artist=7", {
      artistTracks,
    });

    expect(
      await screen.findByRole("row", { name: /Artist 001/ }),
    ).toBeVisible();
    artistFetch.mockClear();
    await user.click(screen.getByRole("button", { name: "Shuffle" }));

    await waitFor(() =>
      expect(queueTitles()).toHaveLength(artistTracks.length),
    );
    expect(queueTitles()).toContain("Artist 105");
    expect(
      requestedURLs(artistFetch).filter((url) =>
        url.startsWith("/api/v1/library/music/artists/7/tracks?"),
      ),
    ).toEqual([
      "/api/v1/library/music/artists/7/tracks?page=1&limit=100",
      "/api/v1/library/music/artists/7/tracks?page=2&limit=100",
    ]);
  });

  it("shuffles album detail tracks without global or artist pagination", async () => {
    const user = userEvent.setup();
    const albumTracks = generatedTracks("Album", 3, 6000);
    const fetchMock = renderWorkspace("/music/player?view=albums&album=11", {
      albumTracks,
    });

    expect(await screen.findByRole("row", { name: /Album 003/ })).toBeVisible();
    fetchMock.mockClear();
    await user.click(screen.getByRole("button", { name: "Shuffle" }));

    await waitFor(() => expect(queueTitles()).toHaveLength(albumTracks.length));
    expect([...queueTitles()].sort()).toEqual(
      albumTracks.map((track) => track.title).sort(),
    );
    expect(requestedURLs(fetchMock)).toContain(
      "/api/v1/library/music/albums/11",
    );
    expect(
      requestedURLs(fetchMock).filter(
        (url) =>
          url.startsWith("/api/v1/library/music/tracks?") ||
          url.startsWith("/api/v1/library/music/artists/7/tracks?"),
      ),
    ).toEqual([]);
  });

  it("keeps the existing queue intact when a later Shuffle page fails", async () => {
    const user = userEvent.setup();
    const tracks = generatedTracks("Global", 105, 8000);
    const fetchMock = renderWorkspace("/music/player", {
      globalTracks: tracks,
      failGlobalPageOnce: 2,
    });

    await user.click(
      await screen.findByRole("button", { name: "Play Global 001" }),
    );
    const originalQueue = queueTitles();
    expect(originalQueue).toHaveLength(100);
    fetchMock.mockClear();

    await user.click(screen.getByRole("button", { name: "Shuffle" }));
    expect(
      await screen.findByText("The later track page is unavailable."),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled();
    expect(queueTitles()).toEqual(originalQueue);

    await user.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(queueTitles()).toHaveLength(tracks.length));
    expect([...queueTitles()].sort()).toEqual(
      tracks.map((track) => track.title).sort(),
    );
    expect(
      requestedURLs(fetchMock).filter((url) =>
        url.includes("/api/v1/library/music/tracks?page=1&limit=100"),
      ),
    ).toHaveLength(2);
  }, 15000);

  it("does not complete an older Shuffle after a Now Playing track change", async () => {
    const user = userEvent.setup();
    let releaseSecondPage!: () => void;
    const globalPageTwoGate = new Promise<void>((resolve) => {
      releaseSecondPage = resolve;
    });
    const tracks = generatedTracks("Global", 105, 11_000);
    const fetchMock = renderWorkspace("/music/player", {
      globalTracks: tracks,
      globalPageTwoGate,
    });

    await user.click(
      await screen.findByRole("button", { name: "Play Global 001" }),
    );
    const originalQueue = queueTitles();
    await user.click(screen.getByRole("button", { name: "Shuffle" }));
    await waitFor(() =>
      expect(requestedURLs(fetchMock)).toContain(
        "/api/v1/library/music/tracks?page=2&limit=100",
      ),
    );

    const queue = screen.getByRole("list", { name: "Playback queue" });
    await user.click(
      within(queue).getByRole("button", { name: "Play Global 002" }),
    );
    await act(async () => {
      releaseSecondPage();
      await globalPageTwoGate;
    });

    expect(queueTitles()).toEqual(originalQueue);
    expect(
      document.querySelector(".music-now-playing-current h3")?.textContent,
    ).toBe("Global 002");
  }, 15000);

  it("does not let a stale global Shuffle replace a newer album queue", async () => {
    const user = userEvent.setup();
    let releaseSecondPage!: () => void;
    const globalPageTwoGate = new Promise<void>((resolve) => {
      releaseSecondPage = resolve;
    });
    const tracks = generatedTracks("Global", 105, 10_000);
    const fetchMock = renderWorkspace("/music/player", {
      globalTracks: tracks,
      globalPageTwoGate,
    });

    expect(
      await screen.findByRole("row", { name: /Global 001/ }),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Shuffle" }));
    await waitFor(() =>
      expect(requestedURLs(fetchMock)).toContain(
        "/api/v1/library/music/tracks?page=2&limit=100",
      ),
    );

    const collections = screen.getByRole("navigation", {
      name: "Music collections",
    });
    await user.click(
      within(collections).getByRole("button", { name: /^Albums/ }),
    );
    const albums = await screen.findByRole("region", { name: "Albums" });
    await user.click(
      within(albums).getByRole("button", { name: /Harbor Lights/ }),
    );
    expect(
      await screen.findByRole("heading", { name: "Harbor Lights", level: 2 }),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Shuffle" }));
    await waitFor(() => expect(queueTitles()).toHaveLength(3));
    const albumQueue = queueTitles();

    await act(async () => {
      releaseSecondPage();
      await globalPageTwoGate;
    });

    expect(queueTitles()).toEqual(albumQueue);
    expect([...queueTitles()].sort()).toEqual(
      TRACKS.slice(0, 3)
        .map((track) => track.title)
        .sort(),
    );
  });
});
