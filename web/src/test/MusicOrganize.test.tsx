import { render, screen, waitFor, within } from "@testing-library/react";
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

const otherFile = {
  ...file,
  id: 2,
  track_id: 4,
  artist_name: "Second Artist",
  album_title: "Second Album",
  track_title: "Other Song",
  number: 2,
  relative_path: "Music/Second Artist/Second Album/02 Other Song.flac",
};

const tags = {
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
};

const plan = {
  files: [
    {
      music_file_id: 1,
      source_path: "Music/Artist/Album/Song.flac",
      destination_path: "Music/Artist/Album/01 Song.flac",
      action: "move_retag",
      tags,
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
  handler: (
    path: string,
    url: URL,
    options?: RequestInit,
  ) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const parsed = new URL(url, "http://local");
    return handler(parsed.pathname, parsed, options) ?? Response.json({});
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

function bodiesFor(fetch: ReturnType<typeof mockFetch>, path: string) {
  return fetch.mock.calls
    .filter(([url, options]) => {
      const parsed = new URL(url as string, "http://local");
      return parsed.pathname === path && options?.method === "POST";
    })
    .map(([, options]) => JSON.parse(String(options?.body)));
}

function renderPage() {
  return render(
    <Theme>
      <MemoryRouter initialEntries={["/music/organize"]}>
        <MusicOrganize />
      </MemoryRouter>
    </Theme>,
  );
}

function filesResponse(items: unknown[], total = items.length, limit = 100) {
  return Response.json({ items, page: 1, total, limit });
}

describe("music organize", () => {
  it("previews and applies a reviewed organization plan", async () => {
    const fetch = mockFetch((path, _url, options) => {
      if (path === "/api/v1/library/music/files" && !options?.method) {
        return filesResponse([file]);
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

    renderPage();

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
            new URL(url as string, "http://local").pathname ===
              "/api/v1/library/music/edits/apply" && options?.method === "POST",
        ),
      ).toBe(true),
    );
  });

  it("shows the proposed tags of every planned file in the review table", async () => {
    mockFetch((path, _url, options) => {
      if (path === "/api/v1/library/music/files" && !options?.method) {
        return filesResponse([file]);
      }
      if (path === "/api/v1/library/music/edits" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (path === "/api/v1/library/music/edits/preview") {
        return Response.json(plan);
      }
      return undefined;
    });

    renderPage();

    await screen.findByRole("table", { name: "Registered music files" });
    await userEvent.click(
      screen.getByLabelText("Select Music/Artist/Album/Song.flac"),
    );
    await userEvent.click(screen.getByRole("button", { name: "Preview" }));
    const planTable = await screen.findByRole("table", {
      name: "Music organization plan",
    });
    const row = within(planTable).getByRole("row", {
      name: /Song\.flac/,
    });
    expect(within(row).getByText("Song")).toBeVisible();
    expect(within(row).getByText("Artist")).toBeVisible();
    expect(within(row).getByText("Album")).toBeVisible();
    expect(within(row).getByText("1")).toBeVisible();
  });

  it("selects and clears every visible file", async () => {
    const fetch = mockFetch((path, _url, options) => {
      if (path === "/api/v1/library/music/files" && !options?.method) {
        return filesResponse([file, otherFile]);
      }
      if (path === "/api/v1/library/music/edits" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (path === "/api/v1/library/music/edits/preview") {
        return Response.json({ files: [], plan_hash: "hash" });
      }
      return undefined;
    });

    renderPage();

    await screen.findByRole("table", { name: "Registered music files" });
    await userEvent.click(screen.getByRole("button", { name: "Select all" }));
    expect(screen.getByText(/2 selected/)).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Clear" }));
    expect(screen.getByText(/0 selected/)).toBeVisible();

    await userEvent.click(screen.getByRole("button", { name: "Select all" }));
    await userEvent.click(screen.getByRole("button", { name: "Preview" }));
    await waitFor(() => {
      const bodies = bodiesFor(fetch, "/api/v1/library/music/edits/preview");
      expect(bodies).toHaveLength(1);
      expect(bodies[0].file_ids).toEqual([1, 2]);
    });
  });

  it("narrows the file list with the filter before selecting", async () => {
    const fetch = mockFetch((path, _url, options) => {
      if (path === "/api/v1/library/music/files" && !options?.method) {
        return filesResponse([file, otherFile]);
      }
      if (path === "/api/v1/library/music/edits" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (path === "/api/v1/library/music/edits/preview") {
        return Response.json({ files: [], plan_hash: "hash" });
      }
      return undefined;
    });

    renderPage();

    await screen.findByRole("table", { name: "Registered music files" });
    expect(
      screen.getByLabelText("Select Music/Artist/Album/Song.flac"),
    ).toBeVisible();
    expect(
      screen.getByLabelText(
        "Select Music/Second Artist/Second Album/02 Other Song.flac",
      ),
    ).toBeVisible();
    await userEvent.type(
      screen.getByLabelText("Filter registered files"),
      "Second",
    );
    expect(
      screen.queryByLabelText("Select Music/Artist/Album/Song.flac"),
    ).toBeNull();
    expect(
      screen.getByLabelText(
        "Select Music/Second Artist/Second Album/02 Other Song.flac",
      ),
    ).toBeVisible();

    await userEvent.click(screen.getByRole("button", { name: "Select all" }));
    await userEvent.click(screen.getByRole("button", { name: "Preview" }));
    await waitFor(() => {
      const bodies = bodiesFor(fetch, "/api/v1/library/music/edits/preview");
      expect(bodies).toHaveLength(1);
      expect(bodies[0].file_ids).toEqual([2]);
    });
  });

  it("loads further pages of registered files", async () => {
    mockFetch((path, url, options) => {
      if (path === "/api/v1/library/music/files" && !options?.method) {
        if (url.searchParams.get("page") === "2") {
          return Response.json({
            items: [otherFile],
            page: 2,
            total: 2,
            limit: 1,
          });
        }
        return Response.json({ items: [file], page: 1, total: 2, limit: 1 });
      }
      if (path === "/api/v1/library/music/edits" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      return undefined;
    });

    renderPage();

    expect(
      await screen.findByRole("table", { name: "Registered music files" }),
    ).toBeVisible();
    expect(
      screen.getByText(/Page 1 of 2 · 2 registered files · 0 selected/),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(
      await screen.findByLabelText(
        "Select Music/Second Artist/Second Album/02 Other Song.flac",
      ),
    ).toBeVisible();
    expect(
      screen.queryByLabelText("Select Music/Artist/Album/Song.flac"),
    ).toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Previous page" }),
    );
    expect(
      await screen.findByLabelText("Select Music/Artist/Album/Song.flac"),
    ).toBeVisible();
  });

  it("sends the metadata corrections with the preview and apply requests", async () => {
    const fetch = mockFetch((path, _url, options) => {
      if (path === "/api/v1/library/music/files" && !options?.method) {
        return filesResponse([file]);
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

    renderPage();

    await screen.findByRole("table", { name: "Registered music files" });
    await userEvent.type(
      screen.getByLabelText("Corrected artist"),
      "Real Artist",
    );
    await userEvent.type(
      screen.getByLabelText("Corrected album"),
      "Real Album",
    );
    await userEvent.type(screen.getByLabelText("Corrected year"), "1984");
    await userEvent.click(
      screen.getByLabelText("Select Music/Artist/Album/Song.flac"),
    );
    await userEvent.click(screen.getByRole("button", { name: "Preview" }));

    await waitFor(() => {
      const previews = bodiesFor(fetch, "/api/v1/library/music/edits/preview");
      expect(previews).toHaveLength(1);
      expect(previews[0]).toEqual({
        file_ids: [1],
        artist: "Real Artist",
        album: "Real Album",
        year: 1984,
      });
      expect(previews[0]).not.toHaveProperty("album_id");
    });

    await userEvent.click(
      screen.getByRole("button", { name: "Apply reviewed plan" }),
    );
    await waitFor(() => {
      const applies = bodiesFor(fetch, "/api/v1/library/music/edits/apply");
      expect(applies).toHaveLength(1);
      expect(applies[0]).toEqual({
        file_ids: [1],
        artist: "Real Artist",
        album: "Real Album",
        year: 1984,
        plan_hash: "hash",
        reviewed: true,
      });
    });
  });

  it("previews a whole album by album ID without selecting files", async () => {
    const fetch = mockFetch((path, _url, options) => {
      if (path === "/api/v1/library/music/files" && !options?.method) {
        return filesResponse([file]);
      }
      if (path === "/api/v1/library/music/edits" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (path === "/api/v1/library/music/edits/preview") {
        return Response.json(plan);
      }
      return undefined;
    });

    renderPage();

    await screen.findByRole("table", { name: "Registered music files" });
    await userEvent.type(screen.getByLabelText("Album ID"), "7");
    await userEvent.click(screen.getByRole("button", { name: "Preview" }));

    await waitFor(() => {
      const previews = bodiesFor(fetch, "/api/v1/library/music/edits/preview");
      expect(previews).toHaveLength(1);
      expect(previews[0].album_id).toBe(7);
      expect(previews[0].file_ids).toEqual([]);
    });
  });
});
