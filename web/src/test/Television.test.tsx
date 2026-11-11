import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import App from "../App";
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
      episode_count: 1,
      monitored: true,
      loaded_at: "",
      known_episodes: 0,
      selected_episodes: 0,
    },
  ],
};
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
    if (url.includes("/youtube-sources")) return Response.json({ items: [] });
    return Response.json(
      { error: { message: "Unexpected test request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("TV catalog and monitoring", () => {
  it("triggers focused automatic searches for the show and a single episode", async () => {
    const posts: { url: string; body: unknown }[] = [];
    setup((url, options) => {
      if (url.endsWith("/library/series/7")) return Response.json(show);
      if (url.endsWith("/seasons/1"))
        return Response.json({
          season: show.seasons[0],
          revision: show.revision,
          series_monitored: true,
          episodes: [
            {
              id: 9,
              tmdb_id: 45,
              number: 1,
              title: "Pilot",
              overview: "",
              air_date: "2025-01-09",
              runtime_minutes: null,
              monitored: true,
              effective_monitored: true,
            },
            {
              id: 10,
              tmdb_id: 46,
              number: 2,
              title: "Second",
              overview: "",
              air_date: "2025-01-16",
              runtime_minutes: null,
              monitored: true,
              effective_monitored: true,
            },
          ],
        });
      if (
        url.endsWith("/automation/search-item") &&
        options?.method === "POST"
      ) {
        posts.push({ url, body: JSON.parse(String(options.body)) });
        return Response.json({ requested: true }, { status: 202 });
      }
      if (url.includes("/automation/item-results"))
        return Response.json({
          outcome: "queued",
          message: "Download queued.",
          started_at: Math.floor(Date.now() / 1000),
        });
      return undefined;
    });
    mount("/series/7");
    await screen.findByRole("heading", { name: "Test TV series" });
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0].body).toEqual({ media_id: 7 });
    await userEvent.click(
      await screen.findByRole("button", { name: "Season 1" }),
    );
    await userEvent.click(
      await screen.findByRole("button", {
        name: "Automatic search for S01E02 Second",
      }),
    );
    await waitFor(() => expect(posts).toHaveLength(2));
    expect(posts[1].body).toEqual({ media_id: 7, episode_id: 10 });
  });

  it("applies monitoring to selected seasons after confirmation", async () => {
    const fetch = setup((url, options) => {
      if (url.endsWith("/library/series/7")) return Response.json(show);
      if (url.endsWith("/seasons/monitor") && options?.method === "PUT")
        return Response.json({ ...show, revision: 2 });
    });
    mount("/series/7");
    await userEvent.click(
      await screen.findByRole("checkbox", { name: "Select season Season 1" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Unmonitor selected seasons (1)" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Stop monitoring 1 selected seasons?",
    });
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Confirm" }),
    );
    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/library/series/7/seasons/monitor",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({
            revision: 1,
            monitored: false,
            season_ids: [8],
          }),
        }),
      ),
    );
  });
  it("confirms and applies monitoring to only selected episodes", async () => {
    const current = {
      ...structuredClone(show),
      seasons: [
        {
          ...show.seasons[0],
          loaded_at: "2026-10-22",
          known_episodes: 2,
          selected_episodes: 2,
        },
      ],
    };
    const fetch = setup((url, options) => {
      if (url.endsWith("/library/series/7")) return Response.json(current);
      if (url.endsWith("/seasons/1"))
        return Response.json({
          season: current.seasons[0],
          revision: current.revision,
          series_monitored: true,
          episodes: [
            {
              id: 9,
              tmdb_id: 45,
              number: 1,
              title: "Pilot",
              overview: "",
              air_date: "",
              runtime_minutes: null,
              monitored: true,
              effective_monitored: true,
            },
            {
              id: 10,
              tmdb_id: 46,
              number: 2,
              title: "Second",
              overview: "",
              air_date: "",
              runtime_minutes: null,
              monitored: true,
              effective_monitored: true,
            },
          ],
        });
      if (url.endsWith("/episodes/monitor") && options?.method === "PUT")
        return Response.json({ ...current, revision: 2 });
    });
    mount("/series/7");
    await screen.findByRole("heading", { name: "Test TV series" });
    await userEvent.click(screen.getByRole("button", { name: "Season 1" }));
    const episode = await screen.findByRole("checkbox", {
      name: "Select episode 2: Second",
    });
    await userEvent.click(episode);
    await userEvent.click(
      screen.getByRole("button", { name: "Unmonitor selected (1)" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Stop monitoring 1 selected episodes?",
    });
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Confirm" }),
    );
    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/library/series/7/seasons/1/episodes/monitor",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({
            revision: 1,
            monitored: false,
            episode_ids: [10],
          }),
        }),
      ),
    );
    // The season refreshes in place: the same rows stay mounted, so the page
    // keeps its scroll position, and the finished selection is cleared.
    await waitFor(() =>
      expect(
        fetch.mock.calls.filter(
          ([url, o]) =>
            String(url).endsWith("/seasons/1") &&
            (!o?.method || o.method === "GET"),
        ).length,
      ).toBeGreaterThan(1),
    );
    expect(episode.isConnected).toBe(true);
    await waitFor(() => expect(episode).not.toBeChecked());
  });
  it("searches TV separately and saves a show with the chosen monitoring preference", async () => {
    const fetch = setup((url, options) => {
      if (url.includes("/metadata/series?"))
        return Response.json({
          items: [{ ...show, in_library: false }],
          page: 1,
          total_pages: 1,
          total_results: 1,
        });
      if (url.endsWith("/metadata/series/42")) return Response.json(show);
      if (url.endsWith("/library/series") && options?.method === "POST")
        return Response.json({ item: show, created: true }, { status: 201 });
    });
    mount("/search?kind=series&query=Test");
    await screen.findByRole("heading", { name: "Test TV series" });
    expect(
      screen.getByRole("heading", { name: "Search TV shows" }),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "View details" }));
    await screen.findByText("TV show details from TMDb.");
    const dialog = screen.getByRole("dialog");
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Add show" }),
    );
    await userEvent.click(
      await screen.findByRole("menuitem", {
        name: "Add monitoring future episodes",
      }),
    );
    await within(dialog).findByText("In library");
    expect(
      within(dialog).getByRole("link", { name: "Open library" }),
    ).toHaveAttribute("href", "/series");
    const call = fetch.mock.calls.find(
      ([url, options]) =>
        url.endsWith("/library/series") && options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      tmdb_id: 42,
      monitor: "future",
    });
    expect(call?.[1]?.headers).toMatchObject({ "X-CSRF-Token": "test-csrf" });
    expect(
      fetch.mock.calls.some(([url]) => url.includes("/metadata/movies")),
    ).toBe(false);
  });
  it("offers every add-time monitoring choice for a show", async () => {
    const fetch = setup((url, options) => {
      if (url.includes("/metadata/series?"))
        return Response.json({
          items: [{ ...show, in_library: false }],
          page: 1,
          total_pages: 1,
          total_results: 1,
        });
      if (url.endsWith("/metadata/series/42")) return Response.json(show);
      if (url.endsWith("/library/series") && options?.method === "POST")
        return Response.json({ item: show, created: true }, { status: 201 });
    });
    mount("/search?kind=series&query=Test");
    await screen.findByRole("heading", { name: "Test TV series" });
    await userEvent.click(screen.getByRole("button", { name: "View details" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Add show" }),
    );
    for (const name of [
      "Add without monitoring",
      "Add monitoring missing",
      "Add monitoring current season",
      "Add monitoring future episodes",
    ]) {
      expect(await screen.findByRole("menuitem", { name })).toBeVisible();
    }
    await userEvent.click(
      screen.getByRole("menuitem", { name: "Add without monitoring" }),
    );
    await within(dialog).findByText("In library");
    const call = fetch.mock.calls.find(
      ([url, options]) =>
        url.endsWith("/library/series") && options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      tmdb_id: 42,
      monitor: "none",
    });
  });
  it("lists specials last and mounts show settings only when their tab opens", async () => {
    const specials = {
      ...show.seasons[0],
      id: 20,
      number: 0,
      title: "Specials",
      monitored: false,
    };
    const fetch = setup((url) => {
      if (url.endsWith("/library/series/7"))
        return Response.json({ ...show, seasons: [specials, show.seasons[0]] });
    });
    mount("/series/7");
    await screen.findByRole("heading", { name: "Test TV series" });
    const seasons = screen
      .getAllByRole("button", { name: /^(Season 1|Specials)$/ })
      .map((button) => button.textContent);
    expect(seasons).toEqual(["Season 1", "Specials"]);
    expect(
      screen.getByRole("switch", { name: "Monitor Specials" }),
    ).not.toBeChecked();
    expect(
      fetch.mock.calls.some(([url]) =>
        String(url).includes("/youtube-sources"),
      ),
    ).toBe(false);
    await userEvent.click(screen.getByRole("tab", { name: /^Settings/ }));
    expect(
      await screen.findByRole("button", { name: "Edit release rules" }),
    ).toBeVisible();
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(([url]) =>
          String(url).includes("/youtube-sources"),
        ),
      ).toBe(true),
    );
    await userEvent.click(screen.getByRole("tab", { name: /^Episodes/ }));
    expect(
      screen.getByRole("checkbox", { name: "Select season Season 1" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Edit release rules", hidden: true }),
    ).not.toBeVisible();
  });
  it("shows registered, missing and upcoming episodes and expands the file path", async () => {
    const season = {
      ...show.seasons[0],
      loaded_at: "2026-10-22",
      episode_count: 3,
      known_episodes: 3,
      available_episodes: 1,
    };
    const filePath = "TV/Test TV series/Season 01/Pilot.mkv";
    setup((url) => {
      if (url.endsWith("/library/series/7"))
        return Response.json({ ...show, seasons: [season] });
      if (url.endsWith("/seasons/1"))
        return Response.json({
          season,
          revision: 1,
          series_monitored: true,
          episodes: [
            {
              id: 9,
              number: 1,
              title: "Pilot",
              available: true,
              file_path: filePath,
            },
            {
              id: 10,
              number: 2,
              title: "Return",
              available: false,
              air_date: "2025-01-09",
            },
            {
              id: 11,
              number: 3,
              title: "Finale",
              available: false,
              air_date: "2099-01-16",
            },
          ],
        });
    });
    mount("/series/7");
    expect(await screen.findByText("1 / 3 on disk")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Season 1" }));
    const table = within(
      await screen.findByRole("table", { name: "Season 1 episodes" }),
    );
    expect(table.getByText("On disk")).toBeVisible();
    expect(table.getByText("Missing")).toBeVisible();
    expect(table.getByText("Upcoming")).toBeVisible();
    await userEvent.click(table.getByText("Pilot.mkv"));
    expect(table.getByText(filePath).closest("details")).toHaveAttribute(
      "open",
    );
  });

  it("loads episodes, preserves selections while paused, and keeps cached data on refresh failure", async () => {
    let current = structuredClone(show);
    let selected = true;
    let failRefresh = false;
    const fetch = setup((url, options) => {
      if (url.endsWith("/library/series/7")) return Response.json(current);
      if (url.endsWith("/seasons/1"))
        return Response.json({
          season: current.seasons[0],
          episodes: current.seasons[0].loaded_at
            ? [
                {
                  id: 9,
                  tmdb_id: 45,
                  number: 1,
                  title: "Pilot",
                  overview: "Episode synopsis.",
                  air_date: "2025-01-02",
                  runtime_minutes: 45,
                  monitored: selected,
                  effective_monitored: current.monitored && selected,
                },
              ]
            : [],
          revision: current.revision,
          series_monitored: current.monitored,
        });
      if (url.endsWith("/refresh")) {
        if (failRefresh)
          return Response.json(
            { error: { message: "TMDb is temporarily unavailable." } },
            { status: 502 },
          );
        current = {
          ...current,
          revision: current.revision + 1,
          seasons: [
            {
              ...current.seasons[0],
              loaded_at: "2026-10-22",
              known_episodes: 1,
              selected_episodes: 1,
            },
          ],
        };
        return Response.json(current);
      }
      if (url.endsWith("/monitor") && options?.method === "PUT") {
        const body = JSON.parse(String(options.body));
        expect(body.revision).toBe(current.revision);
        if (url.includes("/episodes/1/")) selected = body.monitored;
        else current = { ...current, monitored: body.monitored };
        current = {
          ...current,
          revision: current.revision + 1,
          seasons: [
            { ...current.seasons[0], selected_episodes: selected ? 1 : 0 },
          ],
        };
        return Response.json(current);
      }
    });
    mount("/series/7");
    await screen.findByRole("heading", { name: "Test TV series" });
    await userEvent.click(screen.getByRole("button", { name: "Season 1" }));
    await screen.findByRole("button", { name: "Load episodes from TMDb" });
    expect(
      fetch.mock.calls.some(
        ([url, options]) =>
          url.endsWith("/refresh") && options?.method === "POST",
      ),
    ).toBe(false);
    await userEvent.click(
      screen.getByRole("button", { name: "Load episodes from TMDb" }),
    );
    await screen.findByRole("switch", { name: "Monitor episode 1: Pilot" });
    await userEvent.click(
      screen.getByRole("switch", { name: "Monitor episode 1: Pilot" }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("switch", { name: "Monitor episode 1: Pilot" }),
      ).not.toBeChecked(),
    );
    await userEvent.click(
      screen.getByRole("switch", { name: "Monitor this show" }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("switch", { name: "Monitor episode 1: Pilot" }),
      ).toBeDisabled(),
    );
    expect(
      screen.getByRole("switch", { name: "Monitor episode 1: Pilot" }),
    ).not.toBeChecked();
    failRefresh = true;
    await userEvent.click(
      screen.getByRole("button", { name: "Refresh episodes" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "TMDb is temporarily unavailable.",
    );
    expect(
      within(
        screen.getByRole("table", { name: "Season 1 episodes" }),
      ).getByText("Pilot"),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Download" }),
    ).not.toBeInTheDocument();
  });
});
