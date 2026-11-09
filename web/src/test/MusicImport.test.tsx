import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicImport } from "../pages/MusicImport";

const scan = {
  id: 1,
  root: "Music",
  state: "review",
  full_scan: false,
  phase: "ready",
  progress_total: 2,
  progress_done: 2,
  current_path: "",
  message: "",
  skipped_count: 0,
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

const acoustidUnconfigured = {
  configured: false,
  updated_at: "",
  fingerprint_available: false,
  message:
    "AcoustID is not configured. Save an application API key to enable fingerprint matching.",
};

const acoustidReady = {
  configured: true,
  updated_at: "2026-11-08T10:00:00Z",
  fingerprint_available: true,
  message: "AcoustID is ready and fpcalc is available.",
};

const rootsConfig = {
  revision: 1,
  media_root: "/media",
  libraries: [
    {
      key: "music",
      name: "Music",
      kind: "music",
      configured: false,
      roots: [
        { path: "Music", available: true, default: true },
        { path: "Music Archive", available: true, default: false },
      ],
    },
  ],
};

const noRootsConfig = {
  ...rootsConfig,
  libraries: [{ ...rootsConfig.libraries[0], roots: [] }],
};

function scanList(items: (typeof scan)[]) {
  return Response.json({
    items,
    page: 1,
    total: items.length,
    limit: 20,
  });
}

function scanPage(value: typeof scan, items = [matched, unresolved]) {
  return Response.json({
    scan: value,
    items,
    page: 1,
    total: items.length,
    limit: 100,
  });
}

function postCalls(fetch: ReturnType<typeof mockFetch>, suffix: string) {
  return fetch.mock.calls.filter(
    ([url, options]) =>
      new URL(url, "http://local").pathname.endsWith(suffix) &&
      options?.method === "POST",
  );
}

function mockFetch(
  handler: (
    path: string,
    options?: RequestInit,
  ) => Response | Promise<Response> | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const path = new URL(url, "http://local").pathname;
    return (
      handler(path, options) ??
      (path === "/api/v1/library/music/acoustid" && !options?.method
        ? Response.json(acoustidUnconfigured)
        : Response.json({}))
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

function mount() {
  render(
    <Theme>
      <MemoryRouter initialEntries={["/music/import"]}>
        <MusicImport />
      </MemoryRouter>
    </Theme>,
  );
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

  it("discovers catalog entries for selected unresolved files", async () => {
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
        path === "/api/v1/library/music/scans/1/discover" &&
        options?.method === "POST"
      ) {
        return Response.json({
          selected: 1,
          matched: 1,
          artists_added: 1,
          albums_added: 1,
          unresolved: 0,
          remaining: 0,
          deferred: 0,
          warnings: [],
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

    expect(
      await screen.findByRole("table", { name: "Scanned music files" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByLabelText("Select Music/Unknown/02 - Other.mp3"),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /Discover missing catalog/ }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /Discover catalog \(1\)/ }),
    );

    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/music/scans/1/discover" &&
            options?.method === "POST",
        ),
      ).toBe(true),
    );
    const call = fetch.mock.calls.find(
      ([url, options]) =>
        new URL(url, "http://local").pathname ===
          "/api/v1/library/music/scans/1/discover" &&
        options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      file_ids: [12],
      reviewed: true,
    });
    expect(
      await screen.findByText(/Added 1 artist and 1 album to the catalog/),
    ).toBeVisible();
  });

  it("does not claim catalog changes when discovery adds nothing", async () => {
    mockFetch((path, options) => {
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
        path === "/api/v1/library/music/scans/1/discover" &&
        options?.method === "POST"
      ) {
        return Response.json({
          selected: 1,
          matched: 0,
          artists_added: 0,
          albums_added: 0,
          unresolved: 1,
          remaining: 1,
          deferred: 0,
          warnings: [],
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

    expect(
      await screen.findByRole("table", { name: "Scanned music files" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByLabelText("Select Music/Unknown/02 - Other.mp3"),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /Discover missing catalog/ }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /Discover catalog \(1\)/ }),
    );

    expect(
      await screen.findByText(/No catalog entries were added/),
    ).toBeVisible();
    expect(screen.queryByText(/Added .* to the catalog/)).toBeNull();
  });

  it("starts a scan for configured music roots", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/roots") {
        return Response.json({
          revision: 1,
          media_root: "/media",
          libraries: [
            {
              key: "music",
              name: "Music",
              kind: "music",
              configured: false,
              roots: [
                { path: "Music", available: true, default: true },
                { path: "Music Archive", available: true, default: false },
              ],
            },
          ],
        });
      }
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
    expect(screen.getByRole("link", { name: "Music" })).toHaveAttribute(
      "href",
      "/music",
    );
    expect(screen.getByText(/case-sensitive/)).toBeVisible();
    await userEvent.click(
      screen.getAllByRole("button", { name: "Scan folder" })[0],
    );
    expect(await screen.findByLabelText("Music root Music")).toBeChecked();
    expect(screen.getByLabelText("Music root Music Archive")).toBeChecked();
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
    const call = fetch.mock.calls.find(
      ([url, options]) =>
        new URL(url, "http://local").pathname ===
          "/api/v1/library/music/scans" && options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      roots: ["Music", "Music Archive"],
      full: false,
    });
  });

  it("makes Add media folder the primary action when no root is configured", async () => {
    mockFetch((path, options) => {
      if (path === "/api/v1/library/roots") {
        return Response.json(noRootsConfig);
      }
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return scanList([]);
      }
      return undefined;
    });

    mount();

    const empty = (await screen.findByText("No scans yet")).closest("section");
    expect(empty).not.toBeNull();
    expect(
      within(empty as HTMLElement).getByRole("link", {
        name: "Add media folder",
      }),
    ).toHaveAttribute("href", "/settings/media");
    expect(
      within(empty as HTMLElement).getByText(
        /Settings → Media Management → Library roots/,
      ),
    ).toBeVisible();
  });

  it("sends the full rescan flag from the scan dialog", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/roots") {
        return Response.json(rootsConfig);
      }
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return scanList([]);
      }
      if (
        path === "/api/v1/library/music/scans" &&
        options?.method === "POST"
      ) {
        return Response.json({ ...scan, full_scan: true }, { status: 202 });
      }
      if (path === "/api/v1/library/music/scans/1" && !options?.method) {
        return scanPage({ ...scan, full_scan: true }, []);
      }
      return undefined;
    });

    mount();

    expect(await screen.findByText("No scans yet")).toBeVisible();
    await userEvent.click(
      screen.getAllByRole("button", { name: "Scan folder" })[0],
    );
    const full = await screen.findByLabelText("Full rescan");
    expect(full).not.toBeChecked();
    await userEvent.click(full);
    expect(full).toBeChecked();
    expect(screen.getByText(/never scanned again or duplicated/)).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Start scan" }));

    await waitFor(() =>
      expect(postCalls(fetch, "/library/music/scans")).toHaveLength(1),
    );
    const call = postCalls(fetch, "/library/music/scans")[0];
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      roots: ["Music", "Music Archive"],
      full: true,
    });
  });

  it("shows live scan progress, phase and current path", async () => {
    let activeScan = {
      ...scan,
      state: "scanning",
      phase: "matching",
      file_count: 12,
      matched_count: 3,
      unresolved_count: 7,
      progress_total: 10,
      progress_done: 4,
      current_path: "Music/Artist/Album/02 - Song.flac",
      skipped_count: 2,
      message: "Matching album tags.",
    };
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return scanList([activeScan]);
      }
      if (path === "/api/v1/library/music/scans/1" && !options?.method) {
        return scanPage(activeScan);
      }
      return undefined;
    });

    mount();

    expect(
      await screen.findByRole("table", { name: "Scanned music files" }),
    ).toBeVisible();
    const bar = await screen.findByRole("progressbar", {
      name: "Matching MusicBrainz progress",
    });
    expect(bar).toHaveAttribute("aria-valuenow", "40");
    expect(screen.getByText("4 of 10 files")).toBeVisible();
    expect(screen.getByText("Matching album tags.")).toBeVisible();
    expect(
      screen.getByText(/Current: Music\/Artist\/Album\/02 - Song.flac/),
    ).toBeVisible();
    expect(screen.getByText("2 skipped files")).toBeVisible();

    activeScan = {
      ...activeScan,
      phase: "fingerprinting",
      progress_done: 7,
      current_path: "Music/Artist/Album/03 - Song.flac",
    };
    await userEvent.click(screen.getByRole("button", { name: "Refresh" }));

    const updated = await screen.findByRole("progressbar", {
      name: "Fingerprinting with AcoustID progress",
    });
    expect(updated).toHaveAttribute("aria-valuenow", "70");
    expect(screen.getByText("7 of 10 files")).toBeVisible();
    expect(
      screen.getByText(/Current: Music\/Artist\/Album\/03 - Song.flac/),
    ).toBeVisible();
    expect(
      screen.getByRole("table", { name: "Scanned music files" }),
    ).toBeVisible();
  });

  it("shows an indeterminate discovery bar with the discovered count", async () => {
    const scanning = {
      ...scan,
      state: "scanning",
      phase: "scanning",
      file_count: 5,
      matched_count: 0,
      unresolved_count: 0,
      progress_total: 0,
      progress_done: 0,
      current_path: "Music/New Folder",
    };
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return scanList([scanning]);
      }
      if (path === "/api/v1/library/music/scans/1" && !options?.method) {
        return scanPage(scanning, []);
      }
      return undefined;
    });

    mount();

    const bar = await screen.findByRole("progressbar", {
      name: "Scanning files progress",
    });
    expect(bar).toHaveAttribute("aria-busy", "true");
    expect(bar).not.toHaveAttribute("aria-valuenow");
    expect(
      screen.getByText(/Discovering files… 5 files found so far/),
    ).toBeVisible();
    expect(screen.getByText(/Current: Music\/New Folder/)).toBeVisible();
  });

  it("requires consent before matching all unmatched files", async () => {
    let resolveMatch: ((response: Response) => void) | null = null;
    const pendingMatch = new Promise<Response>((resolve) => {
      resolveMatch = resolve;
    });
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return scanList([scan]);
      }
      if (path === "/api/v1/library/music/scans/1" && !options?.method) {
        return scanPage(scan);
      }
      if (
        path === "/api/v1/library/music/scans/1/match-all" &&
        options?.method === "POST"
      ) {
        return pendingMatch;
      }
      return undefined;
    });

    mount();

    expect(
      await screen.findByRole("table", { name: "Scanned music files" }),
    ).toBeVisible();
    const trigger = screen.getByRole("button", {
      name: /Match all unmatched \(1\)/,
    });
    expect(trigger).toBeEnabled();
    await userEvent.click(trigger);

    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText(/may add artists and albums to the catalog/),
    ).toBeVisible();
    expect(postCalls(fetch, "/match-all")).toHaveLength(0);

    const confirm = within(dialog).getByRole("button", {
      name: "Match all (1)",
    });
    await userEvent.click(confirm);
    await waitFor(() => expect(confirm).toBeDisabled());

    await waitFor(() => expect(postCalls(fetch, "/match-all")).toHaveLength(1));
    const call = postCalls(fetch, "/match-all")[0];
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({ reviewed: true });

    resolveMatch!(Response.json({ ...scan, phase: "queued" }, { status: 202 }));
    expect(
      await screen.findByText(/Matching all unmatched files started/),
    ).toBeVisible();
  });

  it("finalizes matched files only after consent", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return scanList([scan]);
      }
      if (path === "/api/v1/library/music/scans/1" && !options?.method) {
        return scanPage(scan);
      }
      if (
        path === "/api/v1/library/music/scans/1/finalize" &&
        options?.method === "POST"
      ) {
        return Response.json({ ...scan, phase: "organizing" }, { status: 202 });
      }
      return undefined;
    });

    mount();

    expect(
      await screen.findByRole("table", { name: "Scanned music files" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: /Finalize matched \(1\)/ }),
    );

    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText(/register the 1 matched file in this scan/),
    ).toBeVisible();
    expect(
      within(dialog).getByText(/never overwritten, and registered files/),
    ).toBeVisible();
    expect(postCalls(fetch, "/finalize")).toHaveLength(0);

    await userEvent.click(
      within(dialog).getByRole("button", {
        name: "Finalize matched files (1)",
      }),
    );
    await waitFor(() => expect(postCalls(fetch, "/finalize")).toHaveLength(1));
    const call = postCalls(fetch, "/finalize")[0];
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({ reviewed: true });
    expect(
      await screen.findByText(/Finalizing matched files started/),
    ).toBeVisible();
  });

  it("reports a failed scan honestly instead of completed", async () => {
    const failed = {
      ...scan,
      state: "failed",
      phase: "failed",
      progress_total: 5,
      progress_done: 2,
      error: "The scan worker stopped: permission denied.",
      message: "",
    };
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return scanList([failed]);
      }
      if (path === "/api/v1/library/music/scans/1" && !options?.method) {
        return scanPage(failed);
      }
      return undefined;
    });

    mount();

    expect(await screen.findByText(/permission denied/)).toBeVisible();
    expect(screen.getAllByText("Needs attention").length).toBeGreaterThan(0);
    expect(screen.getByText("Scan error")).toBeVisible();
    expect(screen.queryByText("Completed")).toBeNull();
    expect(
      screen.queryByRole("button", { name: /Match all unmatched/ }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: /Finalize matched/ }),
    ).toBeNull();
  });

  it("warns when match-all skipped AcoustID fingerprinting", async () => {
    const warned = {
      ...scan,
      phase: "ready",
      unresolved_count: 1,
      skipped_count: 1,
      message:
        "AcoustID fingerprinting was skipped because no application key is configured.",
    };
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return scanList([warned]);
      }
      if (path === "/api/v1/library/music/scans/1" && !options?.method) {
        return scanPage(warned, [matched, unresolved]);
      }
      return undefined;
    });

    mount();

    expect(
      await screen.findByText("AcoustID fingerprinting was skipped"),
    ).toBeVisible();
    expect(
      screen.getAllByText(/skipped because no application key/).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText("1 warning")).toBeVisible();
    expect(screen.queryByText("Completed")).toBeNull();
    expect(
      screen.getByRole("button", { name: /Match all unmatched/ }),
    ).toBeEnabled();
  });

  it("prompts for an AcoustID key, saves it once and never echoes it", async () => {
    let status = acoustidUnconfigured;
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/acoustid" && !options?.method) {
        return Response.json(status);
      }
      if (
        path === "/api/v1/library/music/acoustid" &&
        options?.method === "PUT"
      ) {
        status = acoustidReady;
        return Response.json(status);
      }
      if (
        path === "/api/v1/library/music/acoustid" &&
        options?.method === "DELETE"
      ) {
        status = acoustidUnconfigured;
        return Response.json(status);
      }
      if (path === "/api/v1/library/music/scans" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      return undefined;
    });

    mount();

    expect(await screen.findByText(/AcoustID is not configured/)).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Configure AcoustID" }),
    );
    const input = await screen.findByLabelText("AcoustID application API key");
    expect(input).toHaveValue("");
    expect(
      screen.getByRole("link", { name: "acoustid.org/new-application" }),
    ).toHaveAttribute("href", "https://acoustid.org/new-application");
    await userEvent.type(input, "acoustid-fixture-key");
    await userEvent.click(screen.getByRole("button", { name: "Save key" }));

    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/music/acoustid" && options?.method === "PUT",
        ),
      ).toBe(true),
    );
    const put = fetch.mock.calls.find(
      ([url, options]) =>
        new URL(url, "http://local").pathname ===
          "/api/v1/library/music/acoustid" && options?.method === "PUT",
    );
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({
      api_key: "acoustid-fixture-key",
    });
    expect(await screen.findByText(/API key configured/)).toBeVisible();
    expect(screen.getByLabelText("AcoustID application API key")).toHaveValue(
      "",
    );
    expect(document.body.textContent).not.toContain("acoustid-fixture-key");

    await userEvent.click(screen.getByRole("button", { name: "Remove key" }));
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/music/acoustid" && options?.method === "DELETE",
        ),
      ).toBe(true),
    );
    expect(await screen.findByText(/AcoustID is not configured/)).toBeVisible();
    expect(document.body.textContent).not.toContain("acoustid-fixture-key");
  });

  it("keeps fingerprint matching disabled until the key and fpcalc are ready", async () => {
    let status = acoustidUnconfigured;
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/acoustid" && !options?.method) {
        return Response.json(status);
      }
      if (
        path === "/api/v1/library/music/acoustid" &&
        options?.method === "PUT"
      ) {
        status = acoustidReady;
        return Response.json(status);
      }
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
        path === "/api/v1/library/music/scans/1/fingerprint" &&
        options?.method === "POST"
      ) {
        return Response.json({
          selected: 1,
          fingerprinted: 1,
          provider_calls: 1,
          matched: 1,
          artists_added: 1,
          albums_added: 1,
          unresolved: 0,
          remaining: 0,
          deferred: 0,
          warnings: [],
        });
      }
      return undefined;
    });

    mount();

    expect(
      await screen.findByRole("table", { name: "Scanned music files" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByLabelText("Select Music/Unknown/02 - Other.mp3"),
    );
    expect(
      screen.getByRole("button", { name: /Match by audio fingerprint/ }),
    ).toBeDisabled();

    await userEvent.click(
      screen.getByRole("button", { name: "Configure AcoustID" }),
    );
    await userEvent.type(
      await screen.findByLabelText("AcoustID application API key"),
      "acoustid-fixture-key",
    );
    await userEvent.click(screen.getByRole("button", { name: "Save key" }));
    await userEvent.click(screen.getByLabelText("Close"));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: /Match by audio fingerprint/ }),
      ).toBeEnabled(),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /Match by audio fingerprint/ }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Fingerprint selected (1)" }),
    );

    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/music/scans/1/fingerprint" &&
            options?.method === "POST",
        ),
      ).toBe(true),
    );
    const call = fetch.mock.calls.find(
      ([url, options]) =>
        new URL(url, "http://local").pathname ===
          "/api/v1/library/music/scans/1/fingerprint" &&
        options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      file_ids: [12],
      reviewed: true,
    });
    expect(
      await screen.findByText(/Fingerprinted 1 of 1 selected files/),
    ).toBeVisible();
    expect(screen.getByText(/Matched 1 of 1 selected files/)).toBeVisible();
    expect(screen.getByText(/Added 1 artist and 1 album/)).toBeVisible();
    expect(screen.queryByText(/No selected files matched/)).toBeNull();
  });

  it("reports fingerprint results without overstating matches", async () => {
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/acoustid" && !options?.method) {
        return Response.json(acoustidReady);
      }
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
        path === "/api/v1/library/music/scans/1/fingerprint" &&
        options?.method === "POST"
      ) {
        return Response.json({
          selected: 2,
          fingerprinted: 1,
          provider_calls: 1,
          matched: 0,
          artists_added: 0,
          albums_added: 0,
          unresolved: 2,
          remaining: 2,
          deferred: 1,
          warnings: [
            "One file could not be fingerprinted and was left unresolved.",
          ],
        });
      }
      return undefined;
    });

    mount();

    await screen.findByRole("table", { name: "Scanned music files" });
    await userEvent.click(
      screen.getByLabelText("Select Music/Unknown/02 - Other.mp3"),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /Match by audio fingerprint/ }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Fingerprint selected (1)" }),
    );

    expect(
      await screen.findByText(/Fingerprinted 1 of 2 selected files/),
    ).toBeVisible();
    expect(
      screen.getByText(/No selected files matched an AcoustID recording/),
    ).toBeVisible();
    expect(screen.getByText(/No catalog entries were added/)).toBeVisible();
    expect(screen.queryByText(/Matched 1 of/)).toBeNull();
    expect(
      screen.getByText(/One file could not be fingerprinted/),
    ).toBeVisible();
    expect(
      screen.getByText(/deferred; run fingerprint matching again/),
    ).toBeVisible();
  });

  it("shows actionable guidance when AcoustID rejects the key", async () => {
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/acoustid" && !options?.method) {
        return Response.json(acoustidReady);
      }
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
        path === "/api/v1/library/music/scans/1/fingerprint" &&
        options?.method === "POST"
      ) {
        return Response.json(
          {
            error: {
              message: "AcoustID rejected the application API key.",
            },
          },
          { status: 403 },
        );
      }
      return undefined;
    });

    mount();

    await screen.findByRole("table", { name: "Scanned music files" });
    await userEvent.click(
      screen.getByLabelText("Select Music/Unknown/02 - Other.mp3"),
    );
    await userEvent.click(
      screen.getByRole("button", { name: /Match by audio fingerprint/ }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Fingerprint selected (1)" }),
    );

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(
      "AcoustID rejected the application API key.",
    );
    expect(alert).toHaveTextContent(/acoustid\.org\/new-application/);
  });
});
