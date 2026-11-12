import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import App from "../App";
import { Releases } from "../pages/Releases";
import type { TVSeries } from "../api";

const show: TVSeries = {
  id: 7,
  kind: "series",
  tmdb_id: 42,
  title: "Test TV series",
  year: 2025,
  monitored: true,
  status: "missing",
  overview: "A television synopsis.",
  poster_url: "",
  poster_path: "",
  release_date: "2025-01-02",
  runtime_minutes: null,
  series_status: "Returning Series",
  revision: 1,
  updated_at: "",
  seasons: [
    {
      id: 8,
      tmdb_id: 44,
      number: 1,
      title: "Season 1",
      overview: "Season synopsis.",
      air_date: "2025-01-02",
      episode_count: 2,
      monitored: true,
      loaded_at: "2026-10-22",
      known_episodes: 2,
      selected_episodes: 2,
      available_episodes: 1,
    },
  ],
};

const episodes = [
  {
    id: 9,
    number: 1,
    title: "Pilot",
    available: true,
    file_path: "TV/Test TV series/Season 01/Pilot.mkv",
  },
  {
    id: 10,
    number: 2,
    title: "Second",
    available: false,
    air_date: "2025-01-09",
    monitored: true,
    effective_monitored: true,
  },
];

const release = {
  id: "r1",
  movie_id: 7,
  title: "Test.TV.Series.S01E02.1080p",
  protocol: "torrent",
  size_bytes: 1400000000,
  seeders: 6,
  peers: 2,
  published_at: "2026-10-01T12:00:00Z",
};

function seasonPayload() {
  return {
    season: show.seasons[0],
    revision: show.revision,
    series_monitored: true,
    episodes,
  };
}

function mount(path: string) {
  render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </Theme>,
  );
}

function setup(
  extra: (url: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const custom = extra(url, options);
    if (custom) return custom;
    if (url.endsWith("/auth/session"))
      return Response.json({
        authenticated: true,
        setup_required: false,
        session: {
          username: "testadmin",
          csrf_token: "test-csrf",
          expires_at: 9999999999,
        },
      });
    if (url.endsWith("/providers/tmdb"))
      return Response.json({ configured: true });
    if (url.endsWith("/franchise"))
      return Response.json({ source: "Wikidata", items: [], truncated: false });
    if (url.includes("/library/items/7?"))
      return Response.json({
        item: show,
        information: { cast: [], crew: [], genres: [], rating: 0, tagline: "" },
        metadata_updated_at: Math.floor(Date.now() / 1000),
        media_root: "/media",
        library_root: "/media/TV",
        total_size_bytes: 0,
        file_count: 0,
        history_count: 0,
        page: 1,
        files: [],
        history: [],
      });
    if (url.endsWith("/youtube-sources")) return Response.json({ items: [] });
    return Response.json(
      { error: { message: "Unexpected test request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("Series release search and automatic searches", () => {
  it("opens the release search modal from an episode row, searches and queues a download without leaving the page", async () => {
    const downloads: { url: string; body: unknown }[] = [];
    const fetch = setup((url, options) => {
      if (url.endsWith("/library/series/7")) return Response.json(show);
      if (url.endsWith("/seasons/1")) return Response.json(seasonPayload());
      if (url.endsWith("/indexers/status"))
        return Response.json({ configured: true });
      if (url.includes("/integrations/"))
        return Response.json({ configured: true });
      if (url.endsWith("/events")) return Response.json({ items: [] });
      if (url.endsWith("/download-clients"))
        return Response.json({
          items: [{ builtin: true, enabled: true, kind: "qbittorrent" }],
        });
      if (url.includes("/releases?"))
        return Response.json({ items: [release], warnings: [] });
      if (url.endsWith("/downloads") && options?.method === "POST") {
        downloads.push({ url, body: JSON.parse(String(options.body)) });
        return Response.json({ item: { id: "job-1" } }, { status: 201 });
      }
      return undefined;
    });
    mount("/series/7");
    await screen.findByRole("heading", { name: "Test TV series" });
    await userEvent.click(screen.getByRole("button", { name: "Season 1" }));
    const rows = await screen.findByRole("table", {
      name: "Season 1 episodes",
    });
    await userEvent.click(
      within(rows).getByRole("button", {
        name: "Search releases for S01E02 Second",
      }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Releases for Test TV series",
    });
    // The modal searches immediately with the suggested query.
    await within(dialog).findByText("Test.TV.Series.S01E02.1080p");
    expect(
      fetch.mock.calls.some(
        ([url]) =>
          String(url) ===
          "/api/v1/library/series/7/seasons/1/releases?episode=2&query=Test%20TV%20series%20S01E02",
      ),
    ).toBe(true);
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Download" }),
    );
    await waitFor(() => expect(downloads).toHaveLength(1));
    expect(downloads[0].url).toBe("/api/v1/downloads");
    expect(downloads[0].body).toEqual({ release_id: "r1" });
    expect(
      fetch.mock.calls.find(([url]) => String(url).endsWith("/downloads"))?.[1]
        ?.headers,
    ).toMatchObject({ "X-CSRF-Token": "test-csrf" });
    // Success stays inside the modal with an Activity link; no navigation.
    // Success alerts use role="status" (the Alert default outside danger), so
    // the outcome is matched by its title text.
    expect(await within(dialog).findByText("Download queued")).toBeVisible();
    expect(
      within(dialog).getByRole("link", { name: "Open Activity" }),
    ).toHaveAttribute("href", "/activity");
    expect(
      fetch.mock.calls.some(
        ([url, options]) =>
          String(url).endsWith("/downloads") &&
          options?.method === "POST" &&
          // Only the release download was submitted; the page never moved.
          JSON.parse(String(options.body)).release_id === "r1",
      ),
    ).toBe(true);
  });

  it("posts a season-scoped automatic search from the season header", async () => {
    const posts: { url: string; body: unknown }[] = [];
    let runsCalls = 0;
    const now = Math.floor(Date.now() / 1000);
    setup((url, options) => {
      if (url.endsWith("/library/series/7")) return Response.json(show);
      if (url.endsWith("/seasons/1")) return Response.json(seasonPayload());
      if (url.endsWith("/automation/runs")) {
        runsCalls += 1;
        return Response.json({
          items:
            runsCalls === 1
              ? []
              : [
                  {
                    id: "run-9",
                    started_at: now,
                    finished_at: now,
                    state: "completed",
                    mode: "search",
                    checked: 2,
                    queued: 1,
                    matched: 0,
                    message: "",
                  },
                ],
        });
      }
      if (
        url.endsWith("/automation/search-item") &&
        options?.method === "POST"
      ) {
        posts.push({ url, body: JSON.parse(String(options.body)) });
        return Response.json({ requested: true }, { status: 202 });
      }
      return undefined;
    });
    mount("/series/7");
    await screen.findByRole("heading", { name: "Test TV series" });
    await userEvent.click(
      screen.getByRole("button", { name: "Automatic search for Season 1" }),
    );
    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0].url).toBe("/api/v1/automation/search-item");
    expect(posts[0].body).toEqual({ media_id: 7, season: 1 });
    // The result alert uses role="status", so match its title text.
    expect(await screen.findByText("Download queued")).toBeVisible();
  });

  it("keeps the /releases page searching and queueing for a season", async () => {
    const fetch = vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith("/library/series/7")) return Response.json(show);
      if (url.endsWith("/seasons/1")) return Response.json(seasonPayload());
      if (url.endsWith("/indexers/status"))
        return Response.json({ configured: true });
      if (url.includes("/integrations/"))
        return Response.json({ configured: true });
      if (url.endsWith("/events")) return Response.json({ items: [] });
      if (url.endsWith("/download-clients"))
        return Response.json({ items: [] });
      if (url.includes("/organization/files"))
        return Response.json({ items: [] });
      if (url.includes("/releases?"))
        return Response.json({ items: [release], warnings: [] });
      if (url.endsWith("/downloads") && options?.method === "POST")
        return Response.json({ item: { id: "job-2" } }, { status: 201 });
      return Response.json({ configured: true, items: [] });
    });
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <MemoryRouter initialEntries={["/releases?series=7&season=1"]}>
          <Releases />
        </MemoryRouter>
      </Theme>,
    );
    expect(
      await screen.findByRole("heading", {
        name: "Releases for Test TV series",
      }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Search releases" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Search releases" }),
    );
    await screen.findByText("Test.TV.Series.S01E02.1080p");
    expect(
      fetch.mock.calls.some(
        ([url]) =>
          String(url) ===
          "/api/v1/library/series/7/seasons/1/releases?query=Test%20TV%20series%20S01",
      ),
    ).toBe(true);
    await userEvent.click(screen.getByRole("button", { name: "Download" }));
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            String(url).endsWith("/downloads") &&
            options?.method === "POST" &&
            JSON.parse(String(options.body)).release_id === "r1",
        ),
      ).toBe(true),
    );
  });
});
