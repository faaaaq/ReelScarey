import {
  render,
  screen,
  waitFor,
  within,
  fireEvent,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
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
  it("shows calendar coverage and links undated items without carrying its month", async () => {
    setup();
    mount(
      "/calendar?month=2026-10&kind=series&query=Saved&monitored=all&page=2",
    );
    await screen.findByRole("heading", { name: "October 2026" });
    expect(screen.getByText(/2 seasons need/)).toBeVisible();
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
    expect(within(available).getByText(/The Arrival/)).toBeVisible();
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
    await userEvent.click(screen.getByRole("link", { name: "Reset filters" }));
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
