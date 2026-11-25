import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicWanted } from "../pages/MusicWanted";

type FetchHandler = (url: URL, options?: RequestInit) => Response | undefined;

function mockFetch(handler: FetchHandler = () => undefined) {
  const fetch = vi.fn(async (input: string, options?: RequestInit) => {
    const url = new URL(input, "http://local");
    const handled = handler(url, options);
    if (handled) return handled;
    switch (url.pathname) {
      case "/api/v1/library/music/wanted":
        return Response.json({ items: [], page: 1, total: 0, limit: 48 });
      case "/api/v1/library/music/preferences":
        return Response.json({
          allow_lossless: true,
          min_bitrate: 0,
          preferred_formats: ["FLAC", "ALAC", "MP3"],
          revision: 1,
        });
      case "/api/v1/library/music/schedule":
        return Response.json({
          enabled: false,
          interval_hours: 24,
          mode: "search",
          next_run: 0,
          last_run: 0,
          last_error: "",
          revision: 1,
        });
      case "/api/v1/library/music/downloads":
        return Response.json({ items: [], page: 1, total: 0, limit: 24 });
      case "/api/v1/library/music/upgrades":
        return Response.json({ items: [] });
      case "/api/v1/library/music/conversions":
        return Response.json({ items: [], page: 1, total: 0, limit: 24 });
      case "/api/v1/library/music/upgrade-jobs":
        if (!options?.method || options.method === "GET") {
          return Response.json({ items: [], page: 1, total: 0, limit: 24 });
        }
        return Response.json({}, { status: 202 });
      default:
        return Response.json({});
    }
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

function renderWanted() {
  return render(
    <Theme>
      <MemoryRouter initialEntries={["/music/wanted"]}>
        <MusicWanted />
      </MemoryRouter>
    </Theme>,
  );
}

function postBodies(
  fetch: ReturnType<typeof mockFetch>,
  path: string,
): Record<string, unknown>[] {
  return fetch.mock.calls
    .filter(([input, options]) => {
      const url = new URL(input, "http://local");
      return url.pathname === path && options?.method === "POST";
    })
    .map(([, options]) => JSON.parse(String(options?.body)));
}

function upgradeJob(overrides: Record<string, unknown> = {}) {
  return {
    id: 11,
    scope: "album",
    scope_id: 20,
    scope_name: "Radiohead - Kid A",
    target_quality: "lossless",
    max_bitrate_kbps: 0,
    rate_per_hour: 200,
    state: "running",
    total_count: 10,
    processed_count: 5,
    upgraded_count: 2,
    skipped_count: 2,
    failed_count: 1,
    current_track: "Everything in Its Right Place",
    error: "",
    created_at: 1700000000,
    updated_at: 1700000300,
    ...overrides,
  };
}

describe("music source upgrades", () => {
  it("requires library confirmation and creates an uncapped library job", async () => {
    const user = userEvent.setup();
    const fetch = mockFetch();
    renderWanted();

    expect(await screen.findByText("No source upgrades queued")).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Queue source upgrade" }),
    );
    expect(
      screen.getByText(
        "Confirm that you want to queue a source upgrade for the entire library.",
      ),
    ).toBeVisible();
    expect(
      postBodies(fetch, "/api/v1/library/music/upgrade-jobs"),
    ).toHaveLength(0);

    await user.click(
      screen.getByLabelText(
        /I confirm this source upgrade for the entire saved music library/,
      ),
    );
    await user.click(
      screen.getByRole("button", { name: "Queue source upgrade" }),
    );
    expect(
      await screen.findByText("Source upgrade queued for the entire library."),
    ).toBeVisible();
    expect(postBodies(fetch, "/api/v1/library/music/upgrade-jobs")).toEqual([
      {
        scope: "library",
        scope_id: 0,
        target_quality: "lossless",
        max_bitrate_kbps: 0,
      },
    ]);
  });

  it("keeps an artist selection across catalog pages and searches", async () => {
    const user = userEvent.setup();
    const fetch = mockFetch((url) => {
      if (url.pathname === "/api/v1/library/music/artists") {
        if (url.searchParams.get("query") === "Nobody") {
          return Response.json({ items: [], page: 1, total: 0, limit: 12 });
        }
        if (url.searchParams.get("page") === "2") {
          return Response.json({
            items: [
              { id: 8, name: "Portishead", type: "Group", country: "GB" },
            ],
            page: 2,
            total: 2,
            limit: 1,
          });
        }
        return Response.json({
          items: [{ id: 7, name: "Radiohead", type: "Group", country: "GB" }],
          page: 1,
          total: 2,
          limit: 1,
        });
      }
      return undefined;
    });
    renderWanted();

    await user.selectOptions(screen.getByLabelText("Upgrade scope"), "artist");
    await user.click(
      screen.getByRole("button", { name: "Queue source upgrade" }),
    );
    expect(
      screen.getByText(
        "Select a saved artist before queueing the source upgrade.",
      ),
    ).toBeVisible();
    await user.click(
      await screen.findByRole("button", { name: "Select artist Radiohead" }),
    );
    await user.selectOptions(screen.getByLabelText("Target quality"), "high");
    await user.clear(screen.getByLabelText("Maximum bitrate (kbps)"));
    await user.type(screen.getByLabelText("Maximum bitrate (kbps)"), "320");

    await user.click(
      screen.getByRole("button", { name: "Next saved artist catalog page" }),
    );
    expect(
      await screen.findByRole("button", { name: "Select artist Portishead" }),
    ).toBeVisible();
    expect(screen.getByText("Radiohead")).toBeVisible();

    await user.type(
      screen.getByRole("textbox", { name: "Search saved artists" }),
      "Nobody",
    );
    await user.click(screen.getByRole("button", { name: "Search catalog" }));
    expect(
      await screen.findByText(
        "No saved artists match this search. Your current selection is unchanged.",
      ),
    ).toBeVisible();
    expect(screen.getByText("Radiohead")).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Queue source upgrade" }),
    );

    await waitFor(() =>
      expect(
        postBodies(fetch, "/api/v1/library/music/upgrade-jobs"),
      ).toContainEqual({
        scope: "artist",
        scope_id: 7,
        target_quality: "high",
        max_bitrate_kbps: 320,
      }),
    );
  });

  it("creates album lossless and track hi-res jobs with saved catalog IDs", async () => {
    const user = userEvent.setup();
    const fetch = mockFetch((url) => {
      if (url.pathname === "/api/v1/library/music/albums") {
        return Response.json({
          items: [
            {
              id: 20,
              title: "Kid A",
              artist_name: "Radiohead",
              year: 2000,
            },
          ],
          page: 1,
          total: 1,
          limit: 12,
        });
      }
      if (url.pathname === "/api/v1/library/music/tracks") {
        return Response.json({
          items: [
            {
              id: 30,
              title: "Idioteque",
              artist_name: "Radiohead",
              album_title: "Kid A",
              disc: 1,
              number: 8,
            },
          ],
          page: 1,
          total: 1,
          limit: 12,
        });
      }
      return undefined;
    });
    renderWanted();

    await user.selectOptions(screen.getByLabelText("Upgrade scope"), "album");
    await user.click(
      await screen.findByRole("button", {
        name: "Select album Radiohead - Kid A",
      }),
    );
    await user.click(
      screen.getByRole("button", { name: "Queue source upgrade" }),
    );
    await waitFor(() =>
      expect(
        postBodies(fetch, "/api/v1/library/music/upgrade-jobs"),
      ).toHaveLength(1),
    );

    await user.selectOptions(screen.getByLabelText("Upgrade scope"), "track");
    await user.selectOptions(screen.getByLabelText("Target quality"), "hires");
    await user.clear(screen.getByLabelText("Maximum bitrate (kbps)"));
    await user.type(screen.getByLabelText("Maximum bitrate (kbps)"), "1411");
    await user.click(
      await screen.findByRole("button", {
        name: "Select track Radiohead - Idioteque",
      }),
    );
    await user.click(
      screen.getByRole("button", { name: "Queue source upgrade" }),
    );

    await waitFor(() =>
      expect(postBodies(fetch, "/api/v1/library/music/upgrade-jobs")).toEqual([
        {
          scope: "album",
          scope_id: 20,
          target_quality: "lossless",
          max_bitrate_kbps: 0,
        },
        {
          scope: "track",
          scope_id: 30,
          target_quality: "hires",
          max_bitrate_kbps: 1411,
        },
      ]),
    );
    expect(
      screen.getByRole("option", { name: "Hi-Res (lossless, above 48 kHz)" }),
    ).toBeVisible();
  });

  it("shows queue progress and details and sends cancel and retry actions", async () => {
    const user = userEvent.setup();
    const running = upgradeJob();
    const failed = upgradeJob({
      id: 12,
      scope: "track",
      scope_id: 30,
      scope_name: "Radiohead - Idioteque",
      target_quality: "hires",
      max_bitrate_kbps: 1411,
      state: "failed",
      total_count: 1,
      processed_count: 1,
      upgraded_count: 0,
      skipped_count: 0,
      failed_count: 1,
      current_track: "Idioteque",
      error: "No candidate passed byte probing",
    });
    const fetch = mockFetch((url) => {
      if (url.pathname === "/api/v1/library/music/upgrade-jobs") {
        return Response.json({
          items: [running, failed],
          page: 1,
          total: 2,
          limit: 24,
        });
      }
      if (url.pathname.endsWith("/12/retry")) {
        return Response.json(
          { error: { message: "Retry service unavailable" } },
          { status: 503 },
        );
      }
      return undefined;
    });
    renderWanted();

    const table = await screen.findByRole("table", {
      name: "Source upgrade jobs",
    });
    expect(within(table).getByText("5 of 10 processed")).toBeVisible();
    expect(within(table).getByText("Upgraded: 2")).toBeVisible();
    expect(within(table).getByText("Skipped: 2")).toBeVisible();
    expect(within(table).getAllByText("Failed: 1")).toHaveLength(2);
    expect(
      within(table).getByText("Everything in Its Right Place"),
    ).toBeVisible();
    expect(
      within(table).getByText("No candidate passed byte probing"),
    ).toBeVisible();
    expect(
      within(table).getAllByText(new Date(1700000300 * 1000).toLocaleString()),
    ).toHaveLength(2);
    expect(
      screen.getByRole("progressbar", {
        name: "Source upgrade job 11 progress",
      }),
    ).toHaveAttribute("aria-valuenow", "50");

    await user.click(
      screen.getByRole("button", { name: "Cancel source upgrade job 11" }),
    );
    await waitFor(() =>
      expect(
        postBodies(fetch, "/api/v1/library/music/upgrade-jobs/11/cancel"),
      ).toEqual([{}]),
    );
    await user.click(
      screen.getByRole("button", { name: "Retry source upgrade job 12" }),
    );
    expect(await screen.findByText("Retry service unavailable")).toBeVisible();
    expect(
      postBodies(fetch, "/api/v1/library/music/upgrade-jobs/12/retry"),
    ).toEqual([{}]);
  });

  it("shows queue load and submission errors", async () => {
    const user = userEvent.setup();
    mockFetch((url, options) => {
      if (
        url.pathname === "/api/v1/library/music/upgrade-jobs" &&
        (!options?.method || options.method === "GET")
      ) {
        return Response.json(
          { error: { message: "Upgrade queue unavailable" } },
          { status: 503 },
        );
      }
      if (
        url.pathname === "/api/v1/library/music/upgrade-jobs" &&
        options?.method === "POST"
      ) {
        return Response.json(
          { error: { message: "Upgrade target rejected" } },
          { status: 400 },
        );
      }
      return undefined;
    });
    renderWanted();

    expect(await screen.findByText("Upgrade queue unavailable")).toBeVisible();
    await user.click(
      screen.getByLabelText(
        /I confirm this source upgrade for the entire saved music library/,
      ),
    );
    await user.click(
      screen.getByRole("button", { name: "Queue source upgrade" }),
    );
    expect(await screen.findByText("Upgrade target rejected")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Queue source upgrade" }),
    ).toBeEnabled();
  });

  it("renders the empty state and reloads the queue from the toolbar", async () => {
    const user = userEvent.setup();
    const fetch = mockFetch();
    renderWanted();

    expect(await screen.findByText("No source upgrades queued")).toBeVisible();
    const initialCalls = fetch.mock.calls.filter(
      ([input, options]) =>
        new URL(input, "http://local").pathname ===
          "/api/v1/library/music/upgrade-jobs" &&
        (!options?.method || options.method === "GET"),
    ).length;
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => {
      const calls = fetch.mock.calls.filter(
        ([input, options]) =>
          new URL(input, "http://local").pathname ===
            "/api/v1/library/music/upgrade-jobs" &&
          (!options?.method || options.method === "GET"),
      ).length;
      expect(calls).toBeGreaterThan(initialCalls);
    });
  });

  it("bounds queue paging and requests the selected page", async () => {
    const user = userEvent.setup();
    mockFetch((url, options) => {
      if (
        url.pathname === "/api/v1/library/music/upgrade-jobs" &&
        (!options?.method || options.method === "GET")
      ) {
        const page = Number(url.searchParams.get("page") || 1);
        return Response.json({
          items: [
            upgradeJob(
              page === 2
                ? {
                    id: 12,
                    scope_name: "Portishead - Dummy",
                    state: "cancelled",
                    processed_count: 10,
                  }
                : {},
            ),
          ],
          page,
          total: 2,
          limit: 1,
        });
      }
      return undefined;
    });
    renderWanted();

    expect(await screen.findByText("Page 1 of 2")).toBeVisible();
    expect(
      screen.getByRole("button", {
        name: "Previous source upgrade queue page",
      }),
    ).toBeDisabled();
    await user.click(
      screen.getByRole("button", { name: "Next source upgrade queue page" }),
    );
    expect(await screen.findByText("Page 2 of 2")).toBeVisible();
    expect(screen.getByText("Portishead - Dummy")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Next source upgrade queue page" }),
    ).toBeDisabled();
    await user.click(
      screen.getByRole("button", {
        name: "Previous source upgrade queue page",
      }),
    );
    expect(await screen.findByText("Page 1 of 2")).toBeVisible();
  });
});
