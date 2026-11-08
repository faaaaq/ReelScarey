import {
  act,
  render,
  screen,
  waitFor,
  within,
  fireEvent,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, it, expect, vi } from "vitest";
import App from "../App";
import type { PlanningData, PlanningEntry } from "../pages/Planning";

const movie: PlanningEntry = {
  kind: "movie",
  media_id: 1,
  episode_id: null,
  title: "Saved Movie",
  year: 2026,
  episode_title: "",
  season: null,
  episode: null,
  date: "2026-10-24",
  monitored: true,
  monitoring_reason: "",
  available: false,
  status: "missing",
  download_id: "",
  download_state: "",
  protocol: "",
  poster_url: "",
  release_kind: "theatrical",
  theatrical_date: "2026-10-24",
  digital_date: "2026-11-10",
  physical_date: "2026-12-01",
};
const episode: PlanningEntry = {
  ...movie,
  kind: "series",
  media_id: 2,
  episode_id: 10,
  title: "Saved Show",
  episode_title: "The Arrival",
  season: 1,
  episode: 3,
  date: "2026-10-25",
  status: "upcoming",
};
const attention: PlanningEntry = {
  ...episode,
  episode_id: 11,
  episode: 4,
  episode_title: "Review needed",
  status: "attention",
  download_id: "a".repeat(32),
  download_state: "blocked",
  protocol: "usenet",
};
const undated: PlanningEntry = {
  ...movie,
  media_id: 3,
  title: "No Date Movie",
  date: "",
  status: "undated",
  monitored: false,
  monitoring_reason: "Movie monitoring is paused.",
};
function data(calendar = false, month = "2026-10"): PlanningData {
  return {
    items: calendar
      ? [movie, { ...episode, available: true, status: "available" }]
      : [movie, episode, attention, undated],
    counts: {
      missing: 1,
      upcoming: 1,
      undated: 1,
      downloading: 0,
      attention: 1,
      available: calendar ? 1 : 0,
    },
    coverage: { unloaded_seasons: 2, undated: 1 },
    total: calendar ? 2 : 4,
    page: 1,
    limit: 50,
    as_of_date: "2026-10-24",
    month: calendar ? month : "",
    kind: "all",
    status: calendar ? "all" : "all",
    query: "",
    monitored_only: false,
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
  extra?: (url: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    if (url.endsWith("/auth/session"))
      return Response.json({
        authenticated: true,
        setup_required: false,
        session: {
          username: "fixture",
          csrf_token: "fixture-csrf",
          expires_at: 9999999999,
        },
      });
    const custom = extra?.(url, options);
    if (custom) return custom;
    if (url.endsWith("/automation"))
      return Response.json({ mode: "download", state: "idle", message: "" });
    if (url.includes("/wanted?")) return Response.json(data());
    if (url.includes("/calendar?")) {
      const month =
        new URL(url, "http://fixture").searchParams.get("month") ?? "2026-10";
      return Response.json({
        ...data(true, month),
        ...(month !== "2026-10" ? { items: [], total: 0 } : {}),
      });
    }
    return Response.json(
      { error: { message: "Unexpected test request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
afterEach(() => vi.restoreAllMocks());

describe("wanted and release planning", () => {
  it("links missing targets to explicit searches and tracked targets to Activity", async () => {
    const fetch = setup();
    mount("/wanted?status=all&monitored=all");
    const film = (
      await screen.findByRole("link", { name: "Saved Movie" })
    ).closest("tr")!;
    expect(within(film).getByText("Missing")).toBeVisible();
    expect(
      within(film).getByRole("link", { name: "Find releases" }),
    ).toHaveAttribute("href", "/releases?movie=1");
    const tv = screen.getByText("S01E03").closest("tr")!;
    expect(within(tv).getByText("The Arrival")).toBeVisible();
    expect(
      within(tv).getByRole("link", { name: "Find releases" }),
    ).toHaveAttribute("href", "/releases?series=2&season=1&episode=3");
    const review = screen.getByText("S01E04").closest("tr")!;
    expect(within(review).getByText("Needs attention")).toBeVisible();
    expect(
      within(review).getByRole("link", { name: "View activity" }),
    ).toHaveAttribute("href", `/activity?job=${"a".repeat(32)}`);
    expect(
      within(review).queryByRole("link", { name: "Find releases" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("Movie monitoring is paused.")).toBeVisible();
    expect(screen.getByText("Date unavailable")).toBeVisible();
    expect(
      fetch.mock.calls.every(([, o]) => !o?.method || o.method === "GET"),
    ).toBe(true);
  });
  it("preserves bookmarked filters, escapes a literal search and resets pagination", async () => {
    const fetch = setup();
    mount("/wanted?kind=series&status=all&monitored=all&page=3");
    await screen.findByText("Date unavailable");
    expect(
      screen.getByRole("button", { name: "Media type: TV episodes" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Monitoring filter: Include paused" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Media type: TV episodes" }),
    );
    await userEvent.click(
      await screen.findByRole("menuitemradio", { name: "Movies" }),
    );
    await screen.findByRole("button", { name: "Media type: Movies" });
    await userEvent.type(
      screen.getByLabelText("Find in saved catalog"),
      "100%_Movie",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Filter titles" }),
    );
    await waitFor(() => {
      const url = fetch.mock.calls
        .map((c) => c[0])
        .filter((u) => u.includes("/wanted?"))
        .at(-1)!;
      const params = new URL(url, "http://fixture").searchParams;
      expect(params.get("query")).toBe("100%_Movie");
      expect(params.get("kind")).toBe("movie");
      expect(params.get("monitored")).toBe("all");
      expect(params.get("status")).toBe("all");
      expect(params.has("page")).toBe(false);
    });
  });
  it("hides a coverage note for good only when asked, and can show it again", async () => {
    setup();
    mount("/calendar?month=2026-10&kind=series&monitored=all");
    await screen.findByRole("heading", { name: "October 2026" });
    const note = screen
      .getByText(/2 seasons' episodes are not loaded/)
      .closest(".rs-alert") as HTMLElement;
    await userEvent.click(
      within(note).getByRole("button", { name: "Don't show again" }),
    );
    expect(
      screen.queryByText(/2 seasons' episodes are not loaded/),
    ).not.toBeInTheDocument();
    expect(
      window.localStorage.getItem(
        "reelscarey-notice:coverage-unloaded-seasons",
      ),
    ).toBe("hidden");
    expect(screen.getByText(/1 coverage note is hidden/)).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Show again" }));
    expect(
      screen.getByText(/2 seasons' episodes are not loaded/),
    ).toBeVisible();
    expect(
      window.localStorage.getItem(
        "reelscarey-notice:coverage-unloaded-seasons",
      ),
    ).toBeNull();
  });
  it("shows calendar coverage and links undated items without carrying its month", async () => {
    setup();
    mount(
      "/calendar?month=2026-10&kind=series&query=Saved&monitored=all&page=2",
    );
    await screen.findByRole("heading", { name: "October 2026" });
    expect(
      screen.getByText(/2 seasons' episodes are not loaded/),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Metadata refresh schedule" }),
    ).toHaveAttribute("href", "/metadata-refresh");
    expect(screen.getByText(/theatrical releases/)).toBeVisible();
    const link = screen.getByRole("link", { name: "Review undated items" });
    const params = new URL(String(link.getAttribute("href")), "http://fixture")
      .searchParams;
    expect(params.get("status")).toBe("undated");
    expect(params.get("query")).toBe("Saved");
    expect(params.get("kind")).toBe("series");
    expect(params.get("monitored")).toBe("all");
    expect(params.has("month")).toBe(false);
    expect(params.has("page")).toBe(false);
    const day = screen.getByRole("cell", { name: "Oct 25, 2026" });
    await userEvent.click(
      within(day).getByRole("button", { name: /Saved Show.*S01E03/ }),
    );
    const available = await screen.findByRole("dialog", {
      name: "Saved Show (2026)",
    });
    expect(within(available).getByText("In library")).toBeVisible();
    expect(
      within(available).getAllByText(/The Arrival/).length,
    ).toBeGreaterThan(0);
    expect(
      within(available).getByRole("button", { name: "Automatic search" }),
    ).toBeVisible();
    expect(
      within(available).getByRole("link", { name: "Manual search" }),
    ).toBeVisible();
    expect(
      within(available).queryByRole("link", { name: "Find releases" }),
    ).not.toBeInTheDocument();
    expect(
      within(available).getByRole("link", { name: "View show" }),
    ).toHaveAttribute("href", "/series/2");
  });
  it("navigates months and retains media and monitoring filters", async () => {
    const fetch = setup();
    mount("/calendar?month=2026-10&kind=movie&monitored=all");
    await screen.findByRole("heading", { name: "October 2026" });
    await userEvent.click(screen.getByRole("button", { name: "Next month" }));
    await screen.findByRole("heading", { name: "November 2026" });
    expect(screen.getByLabelText("Calendar month")).toHaveValue("2026-11");
    fireEvent.change(screen.getByLabelText("Calendar month"), {
      target: { value: "2027-02" },
    });
    await screen.findByRole("heading", { name: "February 2027" });
    const url = fetch.mock.calls
      .map((c) => c[0])
      .filter((u) => u.includes("/calendar?"))
      .at(-1)!;
    const params = new URL(url, "http://fixture").searchParams;
    expect(params.get("kind")).toBe("movie");
    expect(params.get("monitored")).toBe("all");
    expect(params.get("month")).toBe("2027-02");
    await userEvent.click(screen.getByRole("button", { name: "Today" }));
    await screen.findByRole("heading", { name: "October 2026" });
  });
  it("switches to an agenda list that keeps release and activity links", async () => {
    localStorage.removeItem("reelscarey.calendar.view");
    setup();
    mount("/calendar?month=2026-10");
    await screen.findByRole("heading", { name: "October 2026" });
    expect(screen.getByRole("table", { name: "October 2026" })).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Agenda" }));
    const agenda = await screen.findByRole("table", {
      name: "Calendar agenda",
    });
    const film = within(agenda)
      .getByRole("link", { name: "Saved Movie" })
      .closest("tr")!;
    expect(
      within(film).getByRole("link", { name: "Find releases" }),
    ).toHaveAttribute("href", "/releases?movie=1");
    expect(localStorage.getItem("reelscarey.calendar.view")).toBe("agenda");
    localStorage.removeItem("reelscarey.calendar.view");
  });
  it("labels movie release kinds and shows every saved date in the entry dialog", async () => {
    const fetch = setup();
    mount("/calendar?month=2026-10");
    await screen.findByRole("heading", { name: "October 2026" });
    const day = screen.getByRole("cell", { name: "Oct 24, 2026" });
    expect(within(day).getByText("Theatrical")).toBeVisible();
    await userEvent.click(
      within(day).getByRole("button", { name: /Saved Movie/ }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Saved Movie (2026)",
    });
    expect(within(dialog).getAllByText("Theatrical").length).toBeGreaterThan(0);
    expect(within(dialog).getByText("Nov 10, 2026")).toBeVisible();
    expect(within(dialog).getByText("Dec 1, 2026")).toBeVisible();
    await userEvent.click(
      within(dialog).getAllByRole("button", { name: "Close" })[0],
    );
    await userEvent.click(screen.getByRole("button", { name: "Agenda" }));
    const agenda = await screen.findByRole("table", {
      name: "Calendar agenda",
    });
    expect(within(agenda).getAllByText("Theatrical").length).toBeGreaterThan(0);

    await userEvent.click(
      screen.getByRole("button", { name: "Releases: All releases" }),
    );
    await userEvent.click(
      await screen.findByRole("menuitemradio", { name: "Digital" }),
    );
    await screen.findByRole("button", { name: "Releases: Digital" });
    await waitFor(() => {
      const url = fetch.mock.calls
        .map((c) => c[0])
        .filter((u) => u.includes("/calendar?"))
        .at(-1)!;
      expect(new URL(url, "http://fixture").searchParams.get("release")).toBe(
        "digital",
      );
    });
    expect(localStorage.getItem("reelscarey.calendar.filters")).toContain(
      "release=digital",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Releases: Digital" }),
    );
    await userEvent.click(
      await screen.findByRole("menuitemradio", { name: "All releases" }),
    );
    await waitFor(() => {
      const url = fetch.mock.calls
        .map((c) => c[0])
        .filter((u) => u.includes("/calendar?"))
        .at(-1)!;
      expect(new URL(url, "http://fixture").searchParams.has("release")).toBe(
        false,
      );
    });
  });

  it("shows a dash when a movie has no saved release dates", async () => {
    setup((url) =>
      url.includes("/calendar?")
        ? Response.json({
            ...data(true),
            items: [
              {
                ...movie,
                release_kind: undefined,
                theatrical_date: undefined,
                digital_date: undefined,
                physical_date: undefined,
              },
            ],
            total: 1,
          })
        : undefined,
    );
    mount("/calendar?month=2026-10");
    const day = await screen.findByRole("cell", { name: "Oct 24, 2026" });
    await userEvent.click(
      within(day).getByRole("button", { name: /Saved Movie/ }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Saved Movie (2026)",
    });
    const label = within(dialog).getByText("Release dates");
    expect(label.closest("dl")).toHaveTextContent("—");
  });

  it("refreshes the open calendar and keeps the last data on a transient error", async () => {
    localStorage.removeItem("reelscarey.calendar.view");
    const intervals: { ms: number; run: () => void }[] = [];
    vi.spyOn(window, "setInterval").mockImplementation(((
      handler: TimerHandler,
      timeout?: number,
    ) => {
      intervals.push({
        ms: Number(timeout),
        run: () => {
          if (typeof handler === "function") handler();
        },
      });
      return intervals.length;
    }) as typeof window.setInterval);
    let fail = false;
    let extra = false;
    setup((url) => {
      if (!url.includes("/calendar?")) return undefined;
      if (fail)
        return Response.json(
          { error: { message: "Temporary outage" } },
          { status: 503 },
        );
      const items = [
        movie,
        { ...episode, available: true, status: "available" },
        ...(extra
          ? [
              {
                ...movie,
                media_id: 9,
                title: "Newly downloaded",
                date: "2026-10-26",
                status: "available",
              },
            ]
          : []),
      ];
      return Response.json({ ...data(true), items, total: items.length });
    });
    mount("/calendar?month=2026-10");
    await screen.findByRole("heading", { name: "October 2026" });
    expect(screen.getByText("Updated just now")).toBeVisible();
    expect(screen.queryByText("Newly downloaded")).not.toBeInTheDocument();
    const poll = intervals.find((interval) => interval.ms === 25000)!;
    expect(poll).toBeDefined();
    extra = true;
    await act(async () => poll.run());
    expect(await screen.findByText("Newly downloaded")).toBeVisible();
    fail = true;
    await act(async () => poll.run());
    expect(await screen.findByText(/Could not refresh/)).toBeVisible();
    expect(screen.getByText("Newly downloaded")).toBeVisible();
  });

  it("keeps count shortcuts scoped to the current title filters", async () => {
    const fetch = setup();
    mount("/wanted?query=Saved&kind=series&monitored=all&page=2");
    await screen.findByText("Date unavailable");
    await userEvent.click(
      screen.getByRole("button", { name: "Needs attention: 1" }),
    );
    await waitFor(() => {
      const url = fetch.mock.calls
        .map((c) => c[0])
        .filter((u) => u.includes("/wanted?"))
        .at(-1)!;
      const p = new URL(url, "http://fixture").searchParams;
      expect(p.get("status")).toBe("attention");
      expect(p.get("query")).toBe("Saved");
      expect(p.has("page")).toBe(false);
    });
  });
  it("lets invalid bookmarks be reset and does not queue work", async () => {
    const fetch = setup((url) =>
      url.includes("status=bad")
        ? Response.json(
            { error: { message: "Choose valid planning filters." } },
            { status: 400 },
          )
        : undefined,
    );
    mount("/wanted?status=bad");
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Choose valid planning filters.",
    );
    await userEvent.click(screen.getByRole("button", { name: "Reset" }));
    await screen.findByText("Date unavailable");
    expect(fetch.mock.calls.some(([, o]) => o?.method === "POST")).toBe(false);
  });
  it("recovers an out-of-range page after catalog changes", async () => {
    setup((url) =>
      url.includes("page=8")
        ? Response.json({ ...data(), items: [], total: 1, page: 8 })
        : undefined,
    );
    mount("/wanted?page=8");
    await screen.findByRole("heading", { name: "This page has no items." });
    await userEvent.click(screen.getByRole("button", { name: "First page" }));
    await screen.findByText("Date unavailable");
  });
  it("disables navigation beyond the supported calendar years", async () => {
    setup();
    mount("/calendar?month=0001-01");
    await screen.findByRole("heading", { name: "January 1" });
    expect(
      screen.getByRole("button", { name: "Previous month" }),
    ).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Calendar month"), {
      target: { value: "9999-12" },
    });
    await screen.findByRole("heading", { name: "December 9999" });
    expect(screen.getByRole("button", { name: "Next month" })).toBeDisabled();
  });
});

function stubIntervals() {
  const intervals: { ms: number; run: () => void }[] = [];
  vi.spyOn(window, "setInterval").mockImplementation(((
    handler: TimerHandler,
    timeout?: number,
  ) => {
    intervals.push({
      ms: Number(timeout),
      run: () => {
        if (typeof handler === "function") handler();
      },
    });
    return intervals.length;
  }) as typeof window.setInterval);
  return intervals;
}

it("states the saved download mode and reports what the run did", async () => {
  const intervals = stubIntervals();
  const posts: unknown[] = [];
  let phase: "idle" | "completed" = "idle";
  setup((url, options) => {
    if (url.endsWith("/automation/search-bulk") && options?.method === "POST") {
      posts.push(JSON.parse(String(options.body)));
      phase = "completed";
      return Response.json({ requested: true }, { status: 202 });
    }
    if (url.endsWith("/automation/runs"))
      return Response.json(
        phase === "idle"
          ? []
          : [
              {
                id: "run-1",
                started_at: Math.floor(Date.now() / 1000),
                finished_at: Math.floor(Date.now() / 1000) + 1,
                state: "completed",
                message: "Search complete.",
                mode: "download",
                checked: 4,
                queued: 2,
              },
            ],
      );
    return undefined;
  });
  mount("/calendar?month=2026-10");
  await screen.findByRole("heading", { name: "October 2026" });
  await userEvent.click(screen.getByRole("button", { name: "Search movies" }));
  const dialog = await screen.findByRole("dialog", {
    name: "Search every missing monitored movie?",
  });
  expect(
    await within(dialog).findByText(/saved automation mode is Download/),
  ).toBeVisible();
  await userEvent.click(
    within(dialog).getByRole("button", { name: "Start search" }),
  );
  const poll = intervals.find((interval) => interval.ms === 2500)!;
  await act(async () => poll.run());
  expect(
    await screen.findByText(/Searched 4 items and queued 2 downloads/),
  ).toBeVisible();
  expect(posts).toEqual([{ scope: "movie" }]);
});

it("shows live indeterminate progress while a bulk search runs", async () => {
  const intervals = stubIntervals();
  let phase: "idle" | "running" | "completed" = "idle";
  setup((url, options) => {
    if (url.endsWith("/automation/search-bulk") && options?.method === "POST") {
      phase = "running";
      return Response.json({ requested: true }, { status: 202 });
    }
    if (url.endsWith("/automation/runs"))
      return Response.json(
        phase === "idle"
          ? []
          : [
              {
                id: "run-1",
                started_at: Math.floor(Date.now() / 1000),
                finished_at:
                  phase === "completed" ? Math.floor(Date.now() / 1000) + 1 : 0,
                state: phase === "completed" ? "completed" : "running",
                message:
                  phase === "completed" ? "Search complete." : "Searching.",
                mode: "download",
                checked: 2,
                queued: 1,
              },
            ],
      );
    return undefined;
  });
  mount("/calendar?month=2026-10");
  await screen.findByRole("heading", { name: "October 2026" });
  await userEvent.click(screen.getByRole("button", { name: "Search movies" }));
  const dialog = await screen.findByRole("dialog", {
    name: "Search every missing monitored movie?",
  });
  await userEvent.click(
    within(dialog).getByRole("button", { name: "Start search" }),
  );
  const bar = await screen.findByRole("progressbar", {
    name: "Calendar bulk search",
  });
  expect(bar).toHaveAttribute("aria-busy", "true");
  const poll = intervals.find((interval) => interval.ms === 2500)!;
  await act(async () => poll.run());
  expect(
    await screen.findByText(/Searching… 2 checked, 1 queued/),
  ).toBeVisible();
  phase = "completed";
  await act(async () => poll.run());
  await waitFor(() =>
    expect(
      screen.queryByRole("progressbar", { name: "Calendar bulk search" }),
    ).not.toBeInTheDocument(),
  );
  expect(
    screen.getByText(/Searched 2 items and queued 1 download/),
  ).toBeVisible();
});

it("waits visibly when an earlier search still owns the scheduler", async () => {
  const intervals = stubIntervals();
  setup((url) => {
    if (url.endsWith("/automation/runs"))
      return Response.json([
        {
          id: "run-0",
          started_at: Math.floor(Date.now() / 1000) - 60,
          finished_at: 0,
          state: "running",
          message: "Searching.",
          mode: "search",
          checked: 3,
          queued: 0,
        },
      ]);
    if (url.endsWith("/automation/search-bulk"))
      return Response.json({ requested: true }, { status: 202 });
    return undefined;
  });
  mount("/calendar?month=2026-10");
  await screen.findByRole("heading", { name: "October 2026" });
  await userEvent.click(screen.getByRole("button", { name: "Search movies" }));
  const dialog = await screen.findByRole("dialog", {
    name: "Search every missing monitored movie?",
  });
  await userEvent.click(
    within(dialog).getByRole("button", { name: "Start search" }),
  );
  const poll = intervals.find((interval) => interval.ms === 2500)!;
  await act(async () => poll.run());
  expect(
    await screen.findByText(/Waiting for the current search to finish/),
  ).toBeVisible();
});

it("states search-only mode and explains a run that checked nothing", async () => {
  const intervals = stubIntervals();
  let phase: "idle" | "completed" = "idle";
  setup((url, options) => {
    if (url.endsWith("/automation") && !options?.method)
      return Response.json({ mode: "search", state: "idle", message: "" });
    if (url.endsWith("/automation/search-bulk") && options?.method === "POST") {
      phase = "completed";
      return Response.json({ requested: true }, { status: 202 });
    }
    if (url.endsWith("/automation/runs"))
      return Response.json(
        phase === "idle"
          ? []
          : [
              {
                id: "run-2",
                started_at: Math.floor(Date.now() / 1000),
                finished_at: Math.floor(Date.now() / 1000) + 1,
                state: "completed",
                message: "Waiting for an enabled indexer.",
                mode: "search",
                checked: 0,
                queued: 0,
              },
            ],
      );
    return undefined;
  });
  mount("/calendar?month=2026-10");
  await screen.findByRole("heading", { name: "October 2026" });
  await userEvent.click(screen.getByRole("button", { name: "Search movies" }));
  const dialog = await screen.findByRole("dialog", {
    name: "Search every missing monitored movie?",
  });
  expect(
    await within(dialog).findByText(/saved automation mode is Search-only/),
  ).toBeVisible();
  await userEvent.click(
    within(dialog).getByRole("button", { name: "Start search" }),
  );
  const poll = intervals.find((interval) => interval.ms === 2500)!;
  await act(async () => poll.run());
  const message = await screen.findByText(/No items were checked/);
  expect(message).toHaveTextContent(/Waiting for an enabled indexer/);
  expect(message).toHaveTextContent(
    /waiting for an indexer or download client/,
  );
});

it("remembers the last calendar filters until Reset", async () => {
  localStorage.removeItem("reelscarey.calendar.filters");
  setup();
  const { unmount } = render(
    <Theme>
      <MemoryRouter
        initialEntries={["/calendar?month=2026-10&kind=series&monitored=all"]}
      >
        <App />
      </MemoryRouter>
    </Theme>,
  );
  await screen.findByRole("heading", { name: "October 2026" });
  unmount();
  setup();
  render(
    <Theme>
      <MemoryRouter initialEntries={["/calendar"]}>
        <App />
      </MemoryRouter>
    </Theme>,
  );
  await screen.findByRole("heading", { name: "October 2026" });
  expect(screen.getByLabelText("Calendar month")).toHaveValue("2026-10");
  await userEvent.click(screen.getByRole("button", { name: "Reset" }));
  await waitFor(() =>
    expect(
      window.localStorage.getItem("reelscarey.calendar.filters") || "",
    ).toBe(""),
  );
});
