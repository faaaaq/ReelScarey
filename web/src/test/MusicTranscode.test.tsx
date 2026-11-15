import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MusicSettings } from "../pages/MusicSettings";
import { MusicOrganize } from "../pages/MusicOrganize";
import { AlbumDownloadModal, MusicExportMenu } from "../musicDownload";
import {
  MusicPlayerBar,
  MusicPlayerProvider,
  useMusicPlayer,
} from "../musicPlayer";

const profiles = [
  {
    id: 1,
    name: "Original",
    key: "original",
    codec: "original",
    bitrate_kbps: 0,
    vbr: false,
    require_lossless: false,
    keep_original: false,
  },
  {
    id: 2,
    name: "Opus balanced",
    key: "opus-balanced",
    codec: "opus",
    bitrate_kbps: 128,
    vbr: true,
    require_lossless: false,
    keep_original: false,
  },
  {
    id: 3,
    name: "MP3 compatible",
    key: "mp3-compatible",
    codec: "mp3",
    bitrate_kbps: 192,
    vbr: false,
    require_lossless: false,
    keep_original: false,
  },
];

const preferences = {
  allow_lossless: true,
  min_bitrate: 0,
  preferred_formats: ["FLAC"],
  download_profile: "opus-balanced",
  streaming_quality: "balanced",
  original_retention: "keep",
  fallback_policy: "best",
  transcoding_enabled: true,
  cache_limit_mb: 512,
  max_conversions: 2,
  revision: 3,
};

function mockFetch(
  handler: (path: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const path =
      new URL(url, "http://local").pathname +
      new URL(url, "http://local").search;
    return handler(path, options) ?? Response.json({});
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

function bodiesFor(
  fetch: ReturnType<typeof mockFetch>,
  path: string,
  method: string,
) {
  return fetch.mock.calls
    .filter(
      ([url, options]) =>
        String(url).startsWith(path) && options?.method === method,
    )
    .map(([, options]) => JSON.parse(String(options?.body)));
}

/** Starts playback so the player bar (with the quality control) mounts. */
function Harness() {
  const player = useMusicPlayer();
  return (
    <button
      type="button"
      onClick={() =>
        player.play({
          fileID: 7,
          title: "Airbag",
          artist: "Radiohead",
          album: "OK Computer",
        })
      }
    >
      Start
    </button>
  );
}

describe("music transcoding settings", () => {
  it("renders the conversion settings and saves the new fields", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/preferences" && !options?.method) {
        return Response.json(preferences);
      }
      if (path === "/api/v1/library/music/profiles") {
        return Response.json(profiles);
      }
      if (path === "/api/v1/library/music/cache") {
        return Response.json({
          entries: 2,
          size_bytes: 1024,
          limit_bytes: 536870912,
          hits: 5,
          misses: 1,
        });
      }
      if (
        path === "/api/v1/library/music/preferences" &&
        options?.method === "PUT"
      ) {
        return Response.json({ ...preferences, revision: 4 });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/settings"]}>
          <MusicSettings />
        </MemoryRouter>
      </Theme>,
    );

    expect(await screen.findByText("Conversion settings")).toBeVisible();
    // Builtin profiles feed the default download profile selector.
    const profileSelect = screen.getByLabelText(
      "Default download profile",
    ) as HTMLSelectElement;
    await waitFor(() => expect(profileSelect.value).toBe("opus-balanced"));
    expect(
      Array.from(profileSelect.options).map((option) => option.value),
    ).toEqual(["original", "opus-balanced", "mp3-compatible"]);

    expect(screen.getByLabelText("Cache size limit in megabytes")).toHaveValue(
      "512",
    );
    expect(screen.getByText(/2 cached streams/)).toBeVisible();

    await userEvent.selectOptions(
      screen.getByLabelText("Streaming quality"),
      "high",
    );
    await userEvent.selectOptions(
      screen.getByLabelText("Max concurrent conversions"),
      "3",
    );
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    const bodies = bodiesFor(fetch, "/api/v1/library/music/preferences", "PUT");
    expect(bodies.length).toBe(1);
    expect(bodies[0]).toMatchObject({
      download_profile: "opus-balanced",
      streaming_quality: "high",
      original_retention: "keep",
      fallback_policy: "best",
      transcoding_enabled: true,
      cache_limit_mb: 512,
      max_conversions: 3,
      revision: 3,
    });
  });

  it("rebases the form and prompts a review when the revision conflicts", async () => {
    let reads = 0;
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/preferences" && !options?.method) {
        reads += 1;
        return Response.json(preferences);
      }
      if (path === "/api/v1/library/music/profiles") {
        return Response.json(profiles);
      }
      if (path === "/api/v1/library/music/cache") {
        return Response.json({
          entries: 0,
          size_bytes: 0,
          limit_bytes: 536870912,
          hits: 0,
          misses: 0,
        });
      }
      if (
        path === "/api/v1/library/music/preferences" &&
        options?.method === "PUT"
      ) {
        return Response.json(
          {
            error: {
              message: "The music settings changed.",
              code: "revision_conflict",
            },
          },
          { status: 409 },
        );
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/settings"]}>
          <MusicSettings />
        </MemoryRouter>
      </Theme>,
    );

    await screen.findByText("Conversion settings");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(
      await screen.findByText(/The saved settings have been reloaded/),
    ).toBeVisible();
    // The form reloaded the saved values from the server.
    await waitFor(() => expect(reads).toBeGreaterThan(1));
  });
});

describe("download profile selector", () => {
  it("marks the default profile and sends the chosen key", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/profiles") {
        return Response.json(profiles);
      }
      if (path === "/api/v1/library/music/preferences") {
        return Response.json(preferences);
      }
      if (
        path === "/api/v1/library/music/albums/9/acquire" &&
        options?.method === "POST"
      ) {
        return Response.json({
          id: 12,
          source_url: "",
          source_kind: "album",
          title: "Kid A",
          root: "Music",
          format: "mp3-compatible",
          state: "queued",
          track_count: 0,
          completed_count: 0,
          error: "",
          created_at: 1,
          updated_at: 1,
          acknowledged_at: 0,
        });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter>
          <AlbumDownloadModal
            target={{ albumID: 9, title: "Kid A", artist: "Radiohead" }}
            onClose={() => {}}
          />
        </MemoryRouter>
      </Theme>,
    );

    const dialog = await screen.findByRole("dialog");
    const select = (await within(dialog).findByLabelText(
      "Album download profile",
    )) as HTMLSelectElement;
    await waitFor(() => expect(select.options.length).toBeGreaterThan(1));
    // The saved global profile is marked as the default choice.
    expect(
      Array.from(select.options).some(
        (option) => option.textContent === "Opus balanced (default)",
      ),
    ).toBe(true);
    expect(
      Array.from(select.options).some(
        (option) => option.textContent === "Original (Original)",
      ),
    ).toBe(true);

    await userEvent.selectOptions(select, "mp3-compatible");
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Download album" }),
    );
    await waitFor(() => {
      const bodies = bodiesFor(
        fetch,
        "/api/v1/library/music/albums/9/acquire",
        "POST",
      );
      expect(bodies[0]?.format).toBe("mp3-compatible");
    });
  });
});

describe("conversion queue", () => {
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

  function conversion(id: number, state: string) {
    return {
      id,
      kind: "library",
      file_id: 1,
      profile: "opus-balanced",
      state,
      priority: 8,
      progress: state === "completed" ? 100 : 0,
      attempts: state === "failed" ? 1 : 0,
      error: state === "failed" ? "Conversion output failed verification." : "",
      created_at: 1,
      updated_at: 2,
      completed_at: null,
      source_path: "Music/Artist/Album/Song.flac",
      output_path: "",
    };
  }

  function organizeFetch(
    conversions: unknown[],
    handlers: Record<string, Response | object> = {},
  ) {
    return mockFetch((path, options) => {
      if (path === "/api/v1/library/music/files?page=1" && !options?.method) {
        return Response.json({ items: [file], page: 1, total: 1, limit: 100 });
      }
      if (path === "/api/v1/library/music/edits" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (
        path === "/api/v1/library/music/conversions?page=1" &&
        !options?.method
      ) {
        return Response.json({
          items: conversions,
          page: 1,
          total: conversions.length,
          limit: 20,
        });
      }
      if (path === "/api/v1/library/music/profiles") {
        return Response.json(profiles);
      }
      const hit = handlers[path];
      if (hit) return hit instanceof Response ? hit : Response.json(hit);
      return undefined;
    });
  }

  it("lists jobs with state chips and calls retry and cancel", async () => {
    const fetch = organizeFetch(
      [conversion(7, "failed"), conversion(8, "queued")],
      {
        "/api/v1/library/music/conversions/7/retry": {},
        "/api/v1/library/music/conversions/8/cancel": {},
      },
    );

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/organize"]}>
          <MusicOrganize />
        </MemoryRouter>
      </Theme>,
    );

    const table = await screen.findByRole("table", {
      name: "Music conversion queue",
    });
    const failedRow = within(table)
      .getByText("Conversion output failed verification.")
      .closest("tr");
    expect(failedRow).not.toBeNull();
    expect(within(failedRow as HTMLElement).getByText("failed")).toBeVisible();
    await userEvent.click(
      within(failedRow as HTMLElement).getByRole("button", { name: "Retry" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            String(url).includes("/conversions/7/retry") &&
            options?.method === "POST",
        ),
      ).toBe(true),
    );

    const queuedRow = within(table).getByText("queued").closest("tr");
    expect(queuedRow).not.toBeNull();
    await userEvent.click(
      within(queuedRow as HTMLElement).getByRole("button", { name: "Cancel" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            String(url).includes("/conversions/8/cancel") &&
            options?.method === "POST",
        ),
      ).toBe(true),
    );
  });

  it("shows an honest empty state when no conversions exist", async () => {
    organizeFetch([]);
    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/organize"]}>
          <MusicOrganize />
        </MemoryRouter>
      </Theme>,
    );
    expect(await screen.findByText(/No conversion jobs/)).toBeVisible();
  });
});

describe("library optimization", () => {
  it("previews a dry run and applies after an explicit confirm", async () => {
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
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/files?page=1" && !options?.method) {
        return Response.json({ items: [file], page: 1, total: 1, limit: 100 });
      }
      if (path === "/api/v1/library/music/edits" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (path === "/api/v1/library/music/conversions?page=1") {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (path === "/api/v1/library/music/profiles") {
        return Response.json(profiles);
      }
      if (
        path === "/api/v1/library/music/optimize/preview" &&
        options?.method === "POST"
      ) {
        return Response.json({
          profile: "opus-balanced",
          items: [
            {
              file_id: 1,
              eligible: true,
              reason: "",
              current_bytes: 70_000,
              estimated_bytes: 10_000,
              saved_bytes: 60_000,
            },
          ],
          eligible_count: 1,
          current_bytes: 70_000,
          estimated_bytes: 10_000,
          saved_bytes: 60_000,
        });
      }
      if (
        path === "/api/v1/library/music/optimize/apply" &&
        options?.method === "POST"
      ) {
        return Response.json({ queued: 1 });
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

    await screen.findByRole("table", { name: "Registered music files" });
    await userEvent.selectOptions(
      screen.getByLabelText("Optimize target profile"),
      "opus-balanced",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Preview savings" }),
    );

    const preview = await screen.findByRole("table", {
      name: "Optimization preview",
    });
    expect(within(preview).getAllByText("Eligible").length).toBeGreaterThan(0);
    expect(within(preview).getByText("Artist — Song")).toBeVisible();

    await userEvent.click(
      screen.getByRole("button", { name: /Convert 1 file/ }),
    );
    await screen.findByText(/Queued 1 file for conversion/);
    const applies = bodiesFor(
      fetch,
      "/api/v1/library/music/optimize/apply",
      "POST",
    );
    expect(applies[0]).toEqual({
      profile: "opus-balanced",
      file_ids: [1],
    });
  });

  it("caps one apply at the backend limit and says so", async () => {
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
    const items = Array.from({ length: 510 }, (_, index) => ({
      file_id: index + 1,
      eligible: true,
      reason: "",
      current_bytes: 1000,
      estimated_bytes: 100,
      saved_bytes: 900,
    }));
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/files?page=1" && !options?.method) {
        return Response.json({ items: [file], page: 1, total: 1, limit: 100 });
      }
      if (path === "/api/v1/library/music/edits" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (path === "/api/v1/library/music/conversions?page=1") {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (path === "/api/v1/library/music/profiles") {
        return Response.json(profiles);
      }
      if (
        path === "/api/v1/library/music/optimize/preview" &&
        options?.method === "POST"
      ) {
        return Response.json({ profile: "opus-balanced", items });
      }
      if (
        path === "/api/v1/library/music/optimize/apply" &&
        options?.method === "POST"
      ) {
        return Response.json({ queued: 500 });
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

    await screen.findByRole("table", { name: "Registered music files" });
    await userEvent.selectOptions(
      screen.getByLabelText("Optimize target profile"),
      "opus-balanced",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Preview savings" }),
    );
    const convert = await screen.findByRole("button", {
      name: "Convert 510 files",
    });
    await userEvent.click(convert);
    await screen.findByText(/more eligible files than one apply allows/);
    const applies = bodiesFor(
      fetch,
      "/api/v1/library/music/optimize/apply",
      "POST",
    );
    expect(applies[0].file_ids).toHaveLength(500);
    expect(applies[0].profile).toBe("opus-balanced");
  });
});

describe("file export menu", () => {
  it("links to implemented builtin profile keys", async () => {
    render(
      <Theme>
        <MusicExportMenu fileID={7} title="Airbag" />
      </Theme>,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Export Airbag" }),
    );
    const menu = await screen.findByRole("menu");
    const original = within(menu).getByRole("menuitem", { name: "Original" });
    const opus = within(menu).getByRole("menuitem", { name: "Opus 128" });
    const mp3 = within(menu).getByRole("menuitem", { name: "MP3 192" });
    // The profile query value must be a key the backend resolves; unknown
    // keys would make export fail with a 400.
    expect(original).toHaveAttribute(
      "href",
      "/api/v1/library/music/files/7/export?profile=original",
    );
    expect(opus).toHaveAttribute(
      "href",
      "/api/v1/library/music/files/7/export?profile=opus-high",
    );
    expect(mp3).toHaveAttribute(
      "href",
      "/api/v1/library/music/files/7/export?profile=mp3-compatible",
    );
  });
});

describe("player streaming quality", () => {
  beforeEach(() => {
    vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  });

  it("persists the chosen quality through the preferences revision fence", async () => {
    let reads = 0;
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/preferences" && !options?.method) {
        reads += 1;
        // The first read is the pre-save state; the reload after the save
        // returns the server's new quality.
        return Response.json(
          reads === 1
            ? preferences
            : { ...preferences, streaming_quality: "premium", revision: 4 },
        );
      }
      if (
        path === "/api/v1/library/music/preferences" &&
        options?.method === "PUT"
      ) {
        return Response.json({
          ...preferences,
          streaming_quality: "premium",
          revision: 4,
        });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter>
          <MusicPlayerProvider>
            <Harness />
            <MusicPlayerBar />
          </MusicPlayerProvider>
        </MemoryRouter>
      </Theme>,
    );

    await userEvent.click(screen.getByRole("button", { name: "Start" }));
    const quality = (await screen.findByLabelText(
      "Streaming quality",
    )) as HTMLSelectElement;
    await waitFor(() => expect(quality.disabled).toBe(false));
    await userEvent.selectOptions(quality, "premium");
    await waitFor(() => {
      const bodies = bodiesFor(
        fetch,
        "/api/v1/library/music/preferences",
        "PUT",
      );
      expect(bodies[0]).toMatchObject({
        streaming_quality: "premium",
        revision: 3,
      });
    });
    // The selector settled on the saved server value.
    await waitFor(() => expect(quality.value).toBe("premium"));
    expect(reads).toBeGreaterThan(1);
  });

  it("rebases and reports when the quality changed elsewhere", async () => {
    let reads = 0;
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/preferences" && !options?.method) {
        reads += 1;
        return Response.json(preferences);
      }
      if (
        path === "/api/v1/library/music/preferences" &&
        options?.method === "PUT"
      ) {
        return Response.json(
          {
            error: {
              message: "The music settings changed.",
              code: "music_revision_conflict",
            },
          },
          { status: 409 },
        );
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter>
          <MusicPlayerProvider>
            <Harness />
            <MusicPlayerBar />
          </MusicPlayerProvider>
        </MemoryRouter>
      </Theme>,
    );

    await userEvent.click(screen.getByRole("button", { name: "Start" }));
    const quality = (await screen.findByLabelText(
      "Streaming quality",
    )) as HTMLSelectElement;
    await waitFor(() => expect(quality.disabled).toBe(false));
    await userEvent.selectOptions(quality, "high");
    expect(await screen.findByText(/changed elsewhere/)).toBeVisible();
    // The control rebased onto the saved server value instead of keeping
    // the rejected selection.
    await waitFor(() => expect(quality.value).toBe("balanced"));
    expect(reads).toBeGreaterThan(1);
  });
});
