import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
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
  download_source: "",
  tidal_quality: "",
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
          direct_streams: 3,
          live_streams: 4,
          offset_streams: 2,
          active_transcodes: 1,
          active_cache_fills: 1,
          cache_fill_failures: 0,
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
    expect(screen.getByText(/Direct 3/)).toBeVisible();
    expect(screen.getByText(/Active conversions 1/)).toBeVisible();

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
          direct_streams: 0,
          live_streams: 0,
          offset_streams: 0,
          active_transcodes: 0,
          active_cache_fills: 0,
          cache_fill_failures: 0,
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

describe("TIDAL device login", () => {
  it("shows the device link, polls while waiting and reports completion", async () => {
    let loginReads = 0;
    let loginPosts = 0;
    let authorized = false;
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/preferences" && !options?.method) {
        return Response.json(preferences);
      }
      if (path === "/api/v1/library/music/profiles") {
        return Response.json(profiles);
      }
      if (path === "/api/v1/library/music/cache") {
        return Response.json({
          entries: 0,
          size_bytes: 0,
          limit_bytes: 0,
          hits: 0,
          misses: 0,
          direct_streams: 0,
          live_streams: 0,
          offset_streams: 0,
          active_transcodes: 0,
          active_cache_fills: 0,
          cache_fill_failures: 0,
        });
      }
      if (path === "/api/v1/library/music/tidal/status") {
        return Response.json({
          available: true,
          logged_in: false,
          token_present: false,
          verification_state: "not_checked",
          quality: "LOSSLESS",
        });
      }
      if (path === "/api/v1/library/music/tidal/login") {
        if (options?.method === "POST") {
          loginPosts += 1;
          return Response.json(
            { phase: "waiting", lines: [], result: "" },
            { status: 202 },
          );
        }
        loginReads += 1;
        return Response.json(
          authorized
            ? {
                phase: "done",
                result: "success",
                lines: [
                  "Visit https://tidal.test/device to log in, the code will expire in 300 seconds",
                  "Code: OFFLINE-TEST",
                ],
              }
            : {
                phase: "waiting",
                result: "",
                lines: [
                  "Visit https://tidal.test/device to log in, the code will expire in 300 seconds",
                  "Code: OFFLINE-TEST",
                ],
              },
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

    const connect = await screen.findByRole("button", {
      name: "Connect TIDAL",
    });
    await userEvent.click(connect);
    expect(loginPosts).toBe(1);
    const link = await screen.findByRole("link", {
      name: "https://tidal.test/device",
    });
    expect(link).toHaveAttribute("href", "https://tidal.test/device");
    expect(screen.getByText("Code: OFFLINE-TEST")).toBeVisible();
    expect(
      screen.getByRole("button", { name: /Waiting for login/ }),
    ).toBeDisabled();

    await waitFor(() => expect(loginReads).toBeGreaterThanOrEqual(2), {
      timeout: 4000,
    });
    authorized = true;
    expect(
      await screen.findByText(/TIDAL login succeeded\./, undefined, {
        timeout: 5000,
      }),
    ).toBeVisible();
    expect(fetch).toHaveBeenCalled();
    expect(
      screen.queryByText("Connected — session verified"),
    ).not.toBeInTheDocument();
  });

  it("does not equate a token file with login and saves visible source settings", async () => {
    let verified = false;
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/preferences" && !options?.method) {
        return Response.json(preferences);
      }
      if (
        path === "/api/v1/library/music/preferences" &&
        options?.method === "PUT"
      ) {
        return Response.json({
          ...preferences,
          ...JSON.parse(String(options.body)),
          revision: 4,
        });
      }
      if (path === "/api/v1/library/music/profiles")
        return Response.json(profiles);
      if (path === "/api/v1/library/music/cache") {
        return Response.json({ entries: 0, size_bytes: 0, limit_bytes: 0 });
      }
      if (path === "/api/v1/library/music/tidal/status") {
        return Response.json(
          verified
            ? {
                available: true,
                logged_in: true,
                token_present: true,
                verification_state: "verified",
                verified_at: 2000000000,
                quality: "LOSSLESS",
                detail: "TIDAL accepted the stored session.",
              }
            : {
                available: true,
                logged_in: false,
                token_present: true,
                verification_state: "failed",
                verified_at: 2000000000,
                quality: "HI_RES_LOSSLESS",
                detail: "TIDAL rejected the stored session.",
              },
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

    expect(
      await screen.findByText("TIDAL session is not verified"),
    ).toBeVisible();
    expect(screen.getByText("Present — not proof of login")).toBeVisible();
    expect(
      screen.getAllByText(/TIDAL rejected the stored session/).length,
    ).toBeGreaterThan(0);
    expect(
      screen.queryByText("Connected — session verified"),
    ).not.toBeInTheDocument();

    await userEvent.selectOptions(
      screen.getByLabelText("Default download source"),
      "tidal",
    );
    await userEvent.selectOptions(
      screen.getByLabelText("TIDAL requested quality"),
      "LOSSLESS",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save TIDAL settings" }),
    );
    await waitFor(() => {
      const bodies = bodiesFor(
        fetch,
        "/api/v1/library/music/preferences",
        "PUT",
      );
      expect(bodies).toHaveLength(1);
      expect(bodies[0]).toMatchObject({
        download_source: "tidal",
        tidal_quality: "LOSSLESS",
      });
    });

    verified = true;
    await userEvent.click(screen.getByRole("button", { name: "Verify again" }));
    expect(
      await screen.findByText("Connected — session verified"),
    ).toBeVisible();
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
    await userEvent.selectOptions(
      within(dialog).getByLabelText("Album audio source"),
      "youtube",
    );
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
      expect(bodies[0]?.audio_source).toBe("youtube");
    });
  });

  it("lets the owner resolve an ambiguous match with an eligible candidate", async () => {
    let retried = false;
    const failedJob = {
      id: 14,
      source_url: "",
      source_kind: "album",
      title: "Kid A",
      root: "Music",
      format: "",
      audio_source: "",
      state: "failed",
      track_count: 1,
      completed_count: 0,
      error: "No tracks completed.",
      created_at: 1,
      updated_at: 1,
      acknowledged_at: 0,
      tracks: [
        {
          id: 31,
          job_id: 14,
          position: 1,
          title: "Song",
          artist: "Artist",
          album: "Kid A",
          album_artist: "Artist",
          track_number: 1,
          disc_number: 1,
          duration_ms: 200000,
          cover_url: "",
          source_url: "",
          search_query: "Artist Song",
          state: "failed",
          error:
            "Multiple candidates have equally strong evidence. Choose a source explicitly; ReelScarey will not use search order to decide.",
          music_file_id: null,
          import_job_id: null,
          candidate_evidence: [
            {
              candidate: {
                id: "abcdefghijk",
                title: "Official Song",
                channel: "Artist",
                duration_seconds: 201,
              },
              score: 179,
              eligible: true,
              reasons: ["Track title matches on token boundaries."],
              duration_difference_seconds: 1,
            },
            {
              candidate: {
                id: "lmnopqrstuv",
                title: "Wrong Song",
                channel: "Other",
                duration_seconds: 250,
              },
              score: 40,
              eligible: false,
              reasons: ["Track title does not match the requested recording."],
              duration_difference_seconds: 50,
            },
          ],
        },
      ],
    };
    const queuedJob = {
      ...failedJob,
      state: "queued",
      error: "",
      tracks: [{ ...failedJob.tracks[0], state: "queued", error: "" }],
    };
    const onJob = vi.fn();
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/profiles")
        return Response.json(profiles);
      if (path === "/api/v1/library/music/preferences")
        return Response.json(preferences);
      if (path === "/api/v1/library/music/tidal/status") {
        return Response.json({
          available: false,
          logged_in: false,
          token_present: false,
        });
      }
      if (
        path === "/api/v1/library/music/albums/9/acquire" &&
        options?.method === "POST"
      ) {
        return Response.json({ ...queuedJob, tracks: undefined });
      }
      if (path === "/api/v1/library/music/acquisitions/14") {
        return Response.json(retried ? queuedJob : failedJob);
      }
      if (
        path === "/api/v1/library/music/acquisitions/14/tracks/31/retry" &&
        options?.method === "POST"
      ) {
        retried = true;
        return Response.json(queuedJob);
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter>
          <AlbumDownloadModal
            target={{ albumID: 9, title: "Kid A", artist: "Radiohead" }}
            onClose={() => {}}
            onJob={onJob}
          />
        </MemoryRouter>
      </Theme>,
    );
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Download album" }),
    );
    expect(
      await within(dialog).findByText(/Review candidates \(1 eligible of 2\)/),
    ).toBeVisible();
    expect(
      within(dialog).getAllByRole("button", { name: /Download candidate/ }),
    ).toHaveLength(1);
    expect(
      within(dialog).queryByRole("button", {
        name: /Download candidate Wrong Song/,
      }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      within(dialog).getByRole("button", {
        name: "Download candidate Official Song for Song",
      }),
    );
    await waitFor(() => {
      const bodies = bodiesFor(
        fetch,
        "/api/v1/library/music/acquisitions/14/tracks/31/retry",
        "POST",
      );
      expect(bodies).toHaveLength(1);
      expect(bodies[0]).toEqual({ source_url: "abcdefghijk", reviewed: true });
    });
    expect(onJob).toHaveBeenCalledWith(
      expect.objectContaining({ id: 14, state: "queued" }),
    );
  });

  it("lets explicit Automatic override a provider-only saved default", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/profiles")
        return Response.json(profiles);
      if (path === "/api/v1/library/music/preferences") {
        return Response.json({ ...preferences, download_source: "youtube" });
      }
      if (path === "/api/v1/library/music/tidal/status") {
        return Response.json({
          available: false,
          logged_in: false,
          token_present: false,
        });
      }
      if (
        path === "/api/v1/library/music/albums/9/acquire" &&
        options?.method === "POST"
      ) {
        return Response.json({
          id: 13,
          source_url: "",
          source_kind: "album",
          title: "Kid A",
          root: "Music",
          format: "",
          audio_source: "",
          state: "queued",
          track_count: 1,
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
    const source = (await within(dialog).findByLabelText(
      "Album audio source",
    )) as HTMLSelectElement;
    await waitFor(() => expect(source.value).toBe("youtube"));
    await userEvent.selectOptions(source, "");
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Download album" }),
    );
    await waitFor(() => {
      const bodies = bodiesFor(
        fetch,
        "/api/v1/library/music/albums/9/acquire",
        "POST",
      );
      expect(bodies).toHaveLength(1);
      expect(bodies[0].audio_source).toBe("automatic");
    });
  });

  it("blocks TIDAL-only submission until the session is verified", async () => {
    const fetch = mockFetch((path) => {
      if (path === "/api/v1/library/music/profiles")
        return Response.json(profiles);
      if (path === "/api/v1/library/music/preferences")
        return Response.json(preferences);
      if (path === "/api/v1/library/music/tidal/status") {
        return Response.json({
          available: true,
          logged_in: false,
          token_present: true,
          verification_state: "failed",
          detail: "TIDAL rejected the stored session.",
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
    await userEvent.selectOptions(
      await within(dialog).findByLabelText("Album audio source"),
      "tidal",
    );
    expect(
      await within(dialog).findByText(
        "TIDAL-only download requires verification",
      ),
    ).toBeVisible();
    expect(
      within(dialog).getByRole("button", { name: "Download album" }),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("link", { name: "Open Music settings" }),
    ).toHaveAttribute("href", "/music/settings");
    expect(fetch).not.toHaveBeenCalledWith(
      "/api/v1/library/music/albums/9/acquire",
      expect.anything(),
    );
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

  it("splits a large optimization into bounded apply batches", async () => {
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
    await screen.findByText(/Queued 510 files for conversion across 2 batches/);
    const applies = bodiesFor(
      fetch,
      "/api/v1/library/music/optimize/apply",
      "POST",
    );
    expect(applies).toHaveLength(2);
    expect(applies[0].file_ids).toHaveLength(500);
    expect(applies[1].file_ids).toHaveLength(10);
    for (const body of applies) {
      expect(body.profile).toBe("opus-balanced");
    }
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

  it.each([
    { rangeSeekable: false, paused: false },
    { rangeSeekable: true, paused: true },
  ])(
    "preserves the position and playback intent when quality changes (%j)",
    async ({ rangeSeekable, paused }) => {
      const saved = {
        ...preferences,
        streaming_quality: rangeSeekable ? "original" : "high",
        revision: 4,
      };
      let quality = preferences;
      let oldID: string | null = null;
      mockFetch((path, options) => {
        if (path === "/api/v1/library/music/preferences") {
          if (options?.method === "PUT") quality = saved;
          return Response.json(quality);
        }
        if (path.includes("/stream-info?")) {
          const id = new URL(path, "http://local").searchParams.get(
            "stream_id",
          );
          return Response.json(
            id === oldID
              ? {
                  codec: "opus",
                  bitrate_kbps: 96,
                  mode: "live-miss",
                  range_seekable: false,
                }
              : {
                  codec: rangeSeekable ? "flac" : "opus",
                  bitrate_kbps: rangeSeekable ? undefined : 128,
                  mode: rangeSeekable ? "direct" : "offset-live",
                  range_seekable: rangeSeekable,
                },
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
      const select = await screen.findByLabelText("Streaming quality");
      await waitFor(() => expect(select).toBeEnabled());
      const audio = document.querySelector("audio")!;
      const firstSource = audio.src;
      oldID = new URL(firstSource).searchParams.get("stream_id");
      fireEvent.loadedMetadata(audio);
      expect(await screen.findByText("Playing · Opus 96 kbps")).toBeVisible();
      Object.defineProperty(audio, "paused", {
        configurable: true,
        value: paused,
      });
      if (paused) fireEvent.pause(audio);
      audio.currentTime = 42.5;
      fireEvent.timeUpdate(audio);
      const play = vi.mocked(HTMLMediaElement.prototype.play);
      play.mockClear();

      await userEvent.selectOptions(select, saved.streaming_quality);
      await waitFor(() => expect(audio.src).not.toBe(firstSource));
      expect(screen.getByText("Opening stream…")).toBeVisible();
      expect(audio.src).not.toContain("timeOffset=");
      const reopenedSource = audio.src;
      audio.currentTime = 0;
      fireEvent.loadedMetadata(audio);
      if (rangeSeekable) {
        expect(await screen.findByText("Stream · Original FLAC")).toBeVisible();
        expect(audio.currentTime).toBe(42.5);
        expect(audio.src).toBe(reopenedSource);
        expect(play).not.toHaveBeenCalled();
      } else {
        await waitFor(() => expect(audio.src).toContain("timeOffset=42"));
        expect(audio.src).not.toBe(reopenedSource);
        expect(play).toHaveBeenCalledOnce();
        fireEvent.loadedMetadata(audio);
        expect(
          await screen.findByText("Playing · Opus 128 kbps"),
        ).toBeVisible();
      }
    },
  );

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
    const audio = document.querySelector("audio")!;
    const source = audio.src;
    await userEvent.selectOptions(quality, "high");
    expect(await screen.findByText(/changed elsewhere/)).toBeVisible();
    // The control rebased onto the saved server value instead of keeping
    // the rejected selection.
    await waitFor(() => expect(quality.value).toBe("balanced"));
    expect(audio.src).toBe(source);
    expect(reads).toBeGreaterThan(1);
  });
});
