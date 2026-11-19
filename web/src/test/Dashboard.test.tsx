import { render, screen, within } from "@testing-library/react";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Dashboard } from "../pages/Dashboard";

const wantedItem = {
  kind: "movie",
  media_id: 11,
  episode_id: null,
  title: "Saved missing movie",
  year: 2026,
  episode_title: "",
  season: null,
  episode: null,
  date: "2026-11-01",
  monitored: true,
  monitoring_reason: "",
  available: false,
  status: "missing",
  download_id: "",
  download_state: "",
  protocol: "",
  poster_url: "",
};
const upcomingMovie = {
  ...wantedItem,
  media_id: 21,
  title: "Upcoming movie",
  date: "2026-11-09",
  status: "upcoming",
  release_kind: "digital",
  theatrical_date: "2026-10-01",
  digital_date: "2026-11-09",
};
const upcomingEpisode = {
  ...wantedItem,
  kind: "series",
  media_id: 22,
  episode_id: 7,
  title: "Upcoming show",
  episode_title: "Next episode",
  season: 2,
  episode: 3,
  date: "2026-11-12",
  status: "upcoming",
};
const laterMovie = {
  ...upcomingMovie,
  media_id: 23,
  title: "Later movie",
  date: "2026-11-25",
};

function setup(
  downloads: { items: unknown[]; total: number } = {
    items: [
      {
        id: "job-1",
        movie_title: "Saved movie",
        title: "Saved movie release",
        state: "downloading",
        progress: 0.5,
        downloaded_bytes: 1024,
        total_bytes: 2048,
      },
    ],
    total: 1,
  },
  events: unknown[] = [
    {
      id: 1,
      occurred_at: 1762531200,
      kind: "title",
      object_id: "30",
      title: "New Addition",
      state: "added",
    },
  ],
  musicDownloads: unknown = {
    items: [],
    page: 1,
    total: 0,
    limit: 20,
    active: 0,
    issues: 0,
  },
) {
  const fetch = vi.fn(async (url: string) => {
    if (url.endsWith("/dashboard"))
      return Response.json({
        library_name: "Test library",
        counts: { movies: 12, series: 3, anime: 1 },
      });
    if (url.endsWith("/system"))
      return Response.json({
        version: "test",
        database: "connected",
        paths: { media: "/test/media" },
        capabilities: { automation: true },
      });
    if (url.endsWith("/wanted"))
      return Response.json({
        items: [wantedItem],
        counts: { missing: 7, downloading: 2, attention: 1 },
        total: 1,
        page: 1,
        limit: 50,
      });
    if (url.endsWith("/calendar?status=upcoming"))
      return Response.json({
        items: [upcomingMovie, upcomingEpisode, laterMovie],
        counts: { upcoming: 3 },
        as_of_date: "2026-11-07",
        total: 3,
        page: 1,
        limit: 50,
      });
    if (url.endsWith("/downloads?page=1"))
      return Response.json({ ...downloads, page: 1, limit: 24 });
    if (url.endsWith("/library/music/acquisitions?page=1"))
      return musicDownloads instanceof Response
        ? musicDownloads
        : Response.json(musicDownloads as object);
    if (url.endsWith("/events"))
      return Response.json({ items: events, next: null });
    return Response.json(
      { error: { message: "Unexpected test request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

function mount() {
  render(
    <Theme>
      <MemoryRouter>
        <Dashboard />
      </MemoryRouter>
    </Theme>,
  );
}

describe("dashboard overview", () => {
  it("summarizes queue, wanted, the next seven days, system health and library activity", async () => {
    setup();
    mount();
    await screen.findByRole("heading", { name: "Test library" });
    expect(
      await screen.findByRole("link", { name: /1 active download/ }),
    ).toHaveAttribute("href", "/activity");
    expect(
      await screen.findByRole("link", { name: /7 missing/ }),
    ).toHaveAttribute("href", "/wanted");
    expect(
      await screen.findByRole("link", { name: /2 this week/ }),
    ).toHaveAttribute("href", "/calendar");
    expect(
      await screen.findByRole("link", { name: /Connected · test/ }),
    ).toHaveAttribute("href", "/system");

    expect(
      screen.getByRole("progressbar", { name: /Saved movie progress/ }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "Saved movie" })).toHaveAttribute(
      "href",
      "/activity?job=job-1",
    );

    const upcoming = screen.getByRole("table", {
      name: "Upcoming this week",
    });
    expect(
      within(upcoming).getByRole("link", { name: "Upcoming movie" }),
    ).toBeVisible();
    expect(within(upcoming).getByText(/Movie · Digital/)).toBeVisible();
    expect(
      within(upcoming).getByRole("link", { name: "Upcoming show" }),
    ).toBeVisible();
    expect(
      within(upcoming).queryByRole("link", { name: "Later movie" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(/1 more saved date is later this month/),
    ).toBeVisible();

    expect(screen.getByRole("link", { name: "12 movies" })).toHaveAttribute(
      "href",
      "/movies",
    );
    expect(screen.getByRole("link", { name: "3 TV shows" })).toHaveAttribute(
      "href",
      "/series",
    );
    expect(screen.getByRole("link", { name: "1 anime" })).toHaveAttribute(
      "href",
      "/anime",
    );
    expect(screen.getByText("Recent additions")).toBeVisible();
    expect(screen.getByText("New Addition")).toBeVisible();
  });

  it("shows recent history when the active queue is empty", async () => {
    setup({ items: [], total: 0 }, [
      {
        id: 2,
        occurred_at: 1762531200,
        kind: "download",
        object_id: "job-2",
        title: "Finished Movie",
        state: "completed",
      },
    ]);
    mount();
    await screen.findByRole("heading", { name: "Test library" });
    expect(await screen.findByText("No active downloads.")).toBeVisible();
    expect(screen.getByText("Recent history")).toBeVisible();
    expect(screen.getByText("Finished Movie")).toBeVisible();
    expect(screen.getByRole("link", { name: "View history" })).toHaveAttribute(
      "href",
      "/timeline",
    );
  });

  it("leads with blocked downloads and links straight to the recovery queue", async () => {
    const blocked = {
      id: "job-blocked",
      movie_title: "Stuck download",
      title: "Stuck.Movie.2026.1080p",
      state: "blocked",
      progress: 0.9,
      downloaded_bytes: 1024,
      total_bytes: 2048,
      error_message: "Waiting for mapping review",
    };
    setup({
      items: [
        {
          id: "job-ok",
          movie_title: "Healthy download",
          title: "Healthy.Movie.2026.1080p",
          state: "downloading",
          progress: 0.2,
          downloaded_bytes: 100,
          total_bytes: 2048,
        },
        blocked,
      ],
      total: 2,
    });
    mount();
    await screen.findByRole("heading", { name: "Test library" });
    expect(await screen.findByText(/1 download needs attention/)).toBeVisible();
    expect(
      screen.getByRole("link", { name: /Review the blocked queue/ }),
    ).toHaveAttribute("href", "/activity?state=attention");
    // The blocked job leads the visible queue even though it was listed second.
    const queue = screen.getByRole("list", { name: "Active downloads" });
    const first = within(queue).getAllByRole("listitem")[0];
    expect(within(first).getByText("Stuck download")).toBeVisible();
  });

  it("shows bounded failed music acquisition details and a queue recovery link", async () => {
    const failed = {
      id: 5,
      source_url: "",
      source_kind: "album",
      title: "Failed album",
      root: "Music",
      format: "source",
      state: "failed",
      track_count: 4,
      completed_count: 1,
      error: "No approved audio source remained.",
      created_at: 1,
      updated_at: 1,
      acknowledged_at: 0,
    };
    setup({ items: [], total: 0 }, [], {
      items: [
        failed,
        {
          ...failed,
          id: 4,
          title: "Incomplete album",
          state: "completed",
          track_count: 3,
          completed_count: 2,
          error: "One track could not be acquired.",
        },
        { ...failed, id: 3, title: "Another failed album" },
        { ...failed, id: 2, title: "Oldest failed album" },
      ],
      page: 1,
      total: 4,
      limit: 20,
      active: 0,
      issues: 4,
    });
    mount();

    const title = await screen.findByText("Music downloads need attention");
    const alert = title.closest('[role="status"]') as HTMLElement;
    expect(alert).not.toBeNull();
    const rows = within(alert).getAllByRole("listitem");
    expect(rows).toHaveLength(3);
    expect(within(rows[0]).getByText("Failed album")).toBeVisible();
    expect(within(rows[0]).getByText("1/4 tracks completed")).toBeVisible();
    expect(
      within(rows[0]).getByText("No approved audio source remained."),
    ).toBeVisible();
    expect(within(rows[1]).getByText("Incomplete album")).toBeVisible();
    expect(within(rows[1]).getByText("2/3 tracks completed")).toBeVisible();
    expect(
      within(rows[1]).getByText("One track could not be acquired."),
    ).toBeVisible();
    expect(
      within(alert).getByRole("link", { name: "Review download queue" }),
    ).toHaveAttribute("href", "/music/discover");
    expect(within(alert).getAllByRole("listitem")).toHaveLength(3);
    expect(
      within(alert).queryByText("Oldest failed album"),
    ).not.toBeInTheDocument();
    expect(
      within(alert).getByText(/Showing 3 of 4 affected jobs/),
    ).toBeVisible();
    // Music attention does not replace the existing video queue panel.
    expect(await screen.findByText("No active downloads.")).toBeVisible();
  });

  it("does not show music attention for a clean acquisition queue", async () => {
    setup({ items: [], total: 0 }, [], {
      items: [],
      page: 1,
      total: 0,
      limit: 20,
      active: 0,
      issues: 0,
    });
    mount();
    await screen.findByRole("heading", { name: "Test library" });
    expect(await screen.findByText("No active downloads.")).toBeVisible();
    expect(
      screen.queryByText("Music downloads need attention"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("list", {
        name: "Failed or incomplete music downloads",
      }),
    ).not.toBeInTheDocument();
  });

  it("reports a music acquisition API error without hiding video status", async () => {
    setup(
      { items: [], total: 0 },
      [],
      Response.json(
        { error: { message: "Music acquisitions are offline." } },
        { status: 503 },
      ),
    );
    mount();

    expect(
      await screen.findByText("Music acquisitions are offline."),
    ).toBeVisible();
    expect(screen.getByText("Music download status unavailable")).toBeVisible();
    expect(
      screen.queryByText("Music downloads need attention"),
    ).not.toBeInTheDocument();
    expect(await screen.findByText("No active downloads.")).toBeVisible();
  });
});
