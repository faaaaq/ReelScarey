import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  MusicAlbum,
  MusicArtist,
  MusicArtistTrack,
  MusicPlaylistDetails,
} from "../api";
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

function makeTrack(id: number, title: string): MusicArtistTrack {
  return {
    id,
    album_id: 11,
    musicbrainz_id: `track-${id}`,
    recording_id: `recording-${id}`,
    disc: 1,
    number: id - 1000,
    title,
    artist_name: "North Sea",
    album_title: "Harbor Lights",
    album_year: 2021,
    cover_url: "",
    duration_ms: 180_000 + id,
    file_id: id + 10_000,
    bitrate: 921_000,
    size_bytes: 1_048_576,
    format: "flac",
    relative_path: `North Sea/Harbor Lights/${title}.flac`,
  };
}

const TRACKS = [
  makeTrack(1001, "First Light"),
  makeTrack(1002, "Second Wind"),
  makeTrack(1003, "Third Eye"),
  makeTrack(1004, "Harbor Nights"),
];

function playlistDetails(
  name: string,
  tracks: MusicArtistTrack[],
  revision = 4,
): MusicPlaylistDetails {
  return {
    playlist: {
      id: 1,
      name,
      track_count: tracks.length,
      duration_ms: tracks.reduce(
        (total, track) => total + (track.duration_ms ?? 0),
        0,
      ),
      revision,
      created_at: 100,
      updated_at: 200,
    },
    tracks,
  };
}

function changedPlaylist(
  details: MusicPlaylistDetails,
  tracks: MusicArtistTrack[],
) {
  details.tracks = tracks;
  details.playlist = {
    ...details.playlist,
    track_count: tracks.length,
    duration_ms: tracks.reduce(
      (total, track) => total + (track.duration_ms ?? 0),
      0,
    ),
    revision: details.playlist.revision + 1,
    updated_at: details.playlist.updated_at + 1,
  };
  return details;
}

function installPlaylistFetch(playlists: MusicPlaylistDetails[]) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const requestedURL = String(input);
    const url = new URL(requestedURL, "http://workspace.test");
    const path = url.pathname;
    const method = init?.method ?? "GET";
    const body = init?.body
      ? (JSON.parse(String(init.body)) as Record<string, unknown>)
      : {};
    const page = Number(url.searchParams.get("page") || "1");
    const limit = Number(url.searchParams.get("limit") || "100");

    if (path === "/api/v1/library/music/artists") {
      return Response.json({
        items: [ARTIST],
        page,
        total: 1,
        limit: 48,
      });
    }
    if (path === "/api/v1/library/music/albums") {
      return Response.json({
        items: [ALBUM],
        page,
        total: 1,
        limit: 48,
      });
    }
    if (path === "/api/v1/library/music/tracks") {
      return Response.json({
        items: TRACKS.slice((page - 1) * limit, page * limit),
        page,
        total: TRACKS.length,
        limit,
      });
    }
    if (path === "/api/v1/library/music/playlists" && method === "GET") {
      return Response.json({
        items: playlists.map((details) => details.playlist),
      });
    }
    if (path === "/api/v1/library/music/playlists" && method === "POST") {
      const id = Math.max(0, ...playlists.map((item) => item.playlist.id)) + 1;
      const selected = body.track_id
        ? TRACKS.find((track) => track.id === body.track_id)
        : undefined;
      const details = playlistDetails(
        String(body.name),
        selected ? [selected] : [],
        0,
      );
      details.playlist.id = id;
      details.playlist.revision = 1;
      playlists.push(details);
      return Response.json(details, { status: 201 });
    }

    const detailMatch = path.match(
      /^\/api\/v1\/library\/music\/playlists\/(\d+)$/,
    );
    if (detailMatch) {
      const index = playlists.findIndex(
        (item) => item.playlist.id === Number(detailMatch[1]),
      );
      const details = playlists[index];
      if (!details) {
        return Response.json(
          { error: { message: "Playlist not found." } },
          { status: 404 },
        );
      }
      if (method === "GET") return Response.json(details);
      if (body.revision !== details.playlist.revision) {
        return Response.json(
          { error: { message: "The playlist changed. Reload and try again." } },
          { status: 409 },
        );
      }
      if (method === "PUT") {
        details.playlist = {
          ...details.playlist,
          name: String(body.name),
          revision: details.playlist.revision + 1,
          updated_at: details.playlist.updated_at + 1,
        };
        return Response.json(details);
      }
      if (method === "DELETE") {
        playlists.splice(index, 1);
        return Response.json({ deleted: true });
      }
    }

    const tracksMatch = path.match(
      /^\/api\/v1\/library\/music\/playlists\/(\d+)\/tracks$/,
    );
    if (tracksMatch) {
      const details = playlists.find(
        (item) => item.playlist.id === Number(tracksMatch[1]),
      );
      if (!details) {
        return Response.json(
          { error: { message: "Playlist not found." } },
          { status: 404 },
        );
      }
      if (body.revision !== details.playlist.revision) {
        return Response.json(
          { error: { message: "The playlist changed. Reload and try again." } },
          { status: 409 },
        );
      }
      if (method === "POST") {
        const selected = TRACKS.find((track) => track.id === body.track_id);
        if (!selected) {
          return Response.json(
            { error: { message: "Track not found." } },
            { status: 404 },
          );
        }
        return Response.json(
          changedPlaylist(details, [...details.tracks, selected]),
        );
      }
      if (method === "PUT") {
        const ids = body.track_ids as number[];
        return Response.json(
          changedPlaylist(
            details,
            ids.map((id) => details.tracks.find((track) => track.id === id)!),
          ),
        );
      }
      if (method === "DELETE") {
        return Response.json(
          changedPlaylist(
            details,
            details.tracks.filter((track) => track.id !== body.track_id),
          ),
        );
      }
    }
    throw new Error(`Unexpected workspace request: ${method} ${requestedURL}`);
  });
}

function renderWorkspace(
  playlists: MusicPlaylistDetails[],
  entry = "/music/player",
) {
  const fetchMock = installPlaylistFetch(playlists);
  vi.stubGlobal("fetch", fetchMock);
  render(
    <Theme>
      <MusicPlayerProvider>
        <MemoryRouter initialEntries={[entry]}>
          <Routes>
            <Route path="/music/player" element={<MusicPlayerWorkspace />} />
          </Routes>
        </MemoryRouter>
      </MusicPlayerProvider>
    </Theme>,
  );
  return fetchMock;
}

function expectMutation(
  fetchMock: ReturnType<typeof installPlaylistFetch>,
  method: string,
  path: string,
  body: Record<string, unknown>,
) {
  expect(fetchMock).toHaveBeenCalledWith(
    path,
    expect.objectContaining({ method, body: JSON.stringify(body) }),
  );
}

function playlistTrackTitles(label: string) {
  const table = screen.getByRole("table", { name: label });
  return within(table)
    .getAllByRole("button")
    .map((button) => button.getAttribute("aria-label") ?? "")
    .filter((label) => label.startsWith("Play ") && !label.endsWith(" next"))
    .map((label) => label.replace(/^Play /, ""));
}

function queueTitles() {
  return within(screen.getByRole("list", { name: "Playback queue" }))
    .getAllByRole("listitem")
    .map(
      (item) =>
        within(item)
          .getByRole("button", { name: /^Play / })
          .getAttribute("aria-label")
          ?.replace(/^Play /, "") || "",
    );
}

describe("MusicPlayerWorkspace playlists", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("creates a named playlist and keeps empty states actionable", async () => {
    const user = userEvent.setup();
    const playlists: MusicPlaylistDetails[] = [];
    const fetchMock = renderWorkspace(
      playlists,
      "/music/player?view=playlists",
    );
    const library = screen.getByRole("complementary", {
      name: "Music library browser",
    });

    expect(
      within(library).getByRole("heading", { name: "Playlists" }),
    ).toBeVisible();
    expect(within(library).queryByText("Artist index")).not.toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "No playlists yet" }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "Create a playlist, then add tracks from any track table.",
      ),
    ).toBeVisible();
    expect(
      within(library).getByText("No playlists yet. Use + to create one."),
    ).toBeVisible();

    await user.click(
      within(library).getByRole("button", { name: "New playlist" }),
    );
    await user.type(screen.getByLabelText("Playlist name"), "  Road trip  ");
    await user.click(screen.getByRole("button", { name: "Create playlist" }));

    expectMutation(fetchMock, "POST", "/api/v1/library/music/playlists", {
      name: "Road trip",
    });
    const created = await within(library).findByRole("button", {
      name: /Road trip/,
    });
    expect(playlists[0].playlist.revision).toBe(1);
    await user.click(created);

    expect(
      await screen.findByRole("heading", { name: "Road trip", level: 1 }),
    ).toBeVisible();
    expect(screen.getByText("Persistent library playlist")).toBeVisible();
    expect(
      screen.getByRole("heading", { name: "No tracks in this view" }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "This playlist is empty. Add tracks from another library view.",
      ),
    ).toBeVisible();
  });

  it("keeps All tracks visible after creating and adding to a playlist", async () => {
    const user = userEvent.setup();
    const playlists: MusicPlaylistDetails[] = [];
    renderWorkspace(playlists);
    const library = screen.getByRole("complementary", {
      name: "Music library browser",
    });

    expect(
      await screen.findByRole("table", { name: "Music tracks" }),
    ).toBeVisible();
    await user.click(
      within(library).getByRole("button", { name: "New playlist" }),
    );
    await user.type(screen.getByLabelText("Playlist name"), "Road trip");
    await user.click(screen.getByRole("button", { name: "Create playlist" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );

    for (const title of ["First Light", "Second Wind"]) {
      expect(
        screen.getByRole("heading", { name: "All tracks", level: 1 }),
      ).toBeVisible();
      const table = screen.getByRole("table", { name: "Music tracks" });
      await user.click(
        within(table).getByRole("button", {
          name: `Add ${title} to playlist`,
        }),
      );
      const picker = await screen.findByRole("dialog", {
        name: `Add ${title} to playlist`,
      });
      await user.click(
        within(picker).getByRole("button", { name: /Road trip/ }),
      );
      await waitFor(() => expect(picker).not.toBeInTheDocument());
    }

    expect(
      screen.getByRole("heading", { name: "All tracks", level: 1 }),
    ).toBeVisible();
    expect(screen.getByRole("table", { name: "Music tracks" })).toBeVisible();
    expect(screen.queryByText("Persistent library playlist")).toBeNull();

    await user.click(
      within(library).getByRole("button", { name: /Road trip/ }),
    );
    expect(
      await screen.findByRole("table", { name: "Road trip tracks" }),
    ).toBeVisible();
    expect(playlistTrackTitles("Road trip tracks")).toEqual([
      "First Light",
      "Second Wind",
    ]);
  });

  it("adds, plays, renames, reorders, removes, and deletes with current revisions", async () => {
    const user = userEvent.setup();
    const playlists = [playlistDetails("Night Drive", TRACKS.slice(0, 3))];
    const fetchMock = renderWorkspace(playlists);
    const library = screen.getByRole("complementary", {
      name: "Music library browser",
    });

    await user.click(
      await screen.findByRole("button", {
        name: "Add Harbor Nights to playlist",
      }),
    );
    const picker = await screen.findByRole("dialog", {
      name: "Add Harbor Nights to playlist",
    });
    await user.click(
      within(picker).getByRole("button", { name: /Night Drive/ }),
    );
    expectMutation(
      fetchMock,
      "POST",
      "/api/v1/library/music/playlists/1/tracks",
      { track_id: 1004, revision: 4 },
    );
    await waitFor(() => expect(picker).not.toBeInTheDocument());

    await user.click(
      within(library).getByRole("button", { name: /Night Drive/ }),
    );
    expect(
      await screen.findByRole("heading", { name: "Night Drive", level: 1 }),
    ).toBeVisible();
    expect(playlistTrackTitles("Night Drive tracks")).toEqual([
      "First Light",
      "Second Wind",
      "Third Eye",
      "Harbor Nights",
    ]);

    const table = screen.getByRole("table", { name: "Night Drive tracks" });
    await user.click(
      within(table).getByRole("button", { name: "Play First Light" }),
    );
    const nowPlaying = screen.getByRole("complementary", {
      name: "Now playing and queue",
    });
    const current = await within(nowPlaying).findByRole("region", {
      name: "Current track",
    });
    expect(
      within(current).getByRole("heading", { name: "First Light" }),
    ).toBeVisible();
    expect(queueTitles()).toEqual([
      "First Light",
      "Second Wind",
      "Third Eye",
      "Harbor Nights",
    ]);

    await user.click(screen.getByRole("button", { name: "Rename" }));
    const rename = screen.getByLabelText("Playlist name");
    await user.clear(rename);
    await user.type(rename, "  Late Night  ");
    await user.click(screen.getByRole("button", { name: "Rename playlist" }));
    expectMutation(fetchMock, "PUT", "/api/v1/library/music/playlists/1", {
      name: "Late Night",
      revision: 5,
    });
    expect(
      await screen.findByRole("heading", { name: "Late Night", level: 1 }),
    ).toBeVisible();

    await user.click(
      screen.getByRole("button", { name: "Move First Light down in playlist" }),
    );
    expectMutation(
      fetchMock,
      "PUT",
      "/api/v1/library/music/playlists/1/tracks",
      { track_ids: [1002, 1001, 1003, 1004], revision: 6 },
    );
    await waitFor(() =>
      expect(playlistTrackTitles("Late Night tracks")).toEqual([
        "Second Wind",
        "First Light",
        "Third Eye",
        "Harbor Nights",
      ]),
    );

    await user.click(
      screen.getByRole("button", { name: "Move First Light up in playlist" }),
    );
    expectMutation(
      fetchMock,
      "PUT",
      "/api/v1/library/music/playlists/1/tracks",
      { track_ids: [1001, 1002, 1003, 1004], revision: 7 },
    );
    await waitFor(() =>
      expect(playlistTrackTitles("Late Night tracks")).toEqual([
        "First Light",
        "Second Wind",
        "Third Eye",
        "Harbor Nights",
      ]),
    );

    await user.click(
      screen.getByRole("button", { name: "Remove Third Eye from playlist" }),
    );
    expectMutation(
      fetchMock,
      "DELETE",
      "/api/v1/library/music/playlists/1/tracks",
      { track_id: 1003, revision: 8 },
    );
    await waitFor(() =>
      expect(playlistTrackTitles("Late Night tracks")).toEqual([
        "First Light",
        "Second Wind",
        "Harbor Nights",
      ]),
    );

    await user.click(screen.getByRole("button", { name: "Delete playlist" }));
    const confirmation = await screen.findByRole("dialog", {
      name: "Delete Late Night?",
    });
    expect(
      within(confirmation).getByText(
        "This removes only the playlist. Tracks and audio files stay in the library.",
      ),
    ).toBeVisible();
    await user.click(
      within(confirmation).getByRole("button", { name: "Delete playlist" }),
    );
    expectMutation(fetchMock, "DELETE", "/api/v1/library/music/playlists/1", {
      revision: 9,
    });

    expect(
      await screen.findByRole("heading", { name: "Playlists", level: 1 }),
    ).toBeVisible();
    expect(
      screen.getByRole("heading", { name: "No playlists yet" }),
    ).toBeVisible();
    expect(
      within(library).getByText("No playlists yet. Use + to create one."),
    ).toBeVisible();
    expect(queueTitles()).toEqual([
      "First Light",
      "Second Wind",
      "Third Eye",
      "Harbor Nights",
    ]);
  });
});
