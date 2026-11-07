import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Library } from "../pages/Library";
import { Dashboard } from "../pages/Dashboard";

const item = {
  id: 11,
  kind: "movie",
  tmdb_id: 1,
  title: "Saved title",
  year: 2024,
  poster_url: "",
  status: "missing",
  monitored: true,
};
function Location() {
  const location = useLocation(),
    navigate = useNavigate();
  return (
    <>
      <output aria-label="Current address">
        {location.pathname + location.search}
      </output>
      <button onClick={() => navigate(-1)}>Browser back</button>
    </>
  );
}
function mount(path = "/movies", kind: "movie" | "series" = "movie") {
  render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        <Library kind={kind} />
        <Location />
      </MemoryRouter>
    </Theme>,
  );
}
function mock(response?: (url: string) => Response | undefined) {
  const fetch = vi.fn(
    async (url: string) =>
      response?.(url) ??
      Response.json({
        items: [item],
        total: 1,
        collection_total: 1,
        page: 1,
        limit: 1,
      }),
  );
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("catalog browsing", () => {
  it("requests every title on one page without pagination or per-page controls", async () => {
    const fetch = mock();
    mount("/movies?view=table&query=Saved");
    await screen.findByRole("table", { name: "Movie library" });
    const request = new URL(fetch.mock.calls.at(-1)![0], "http://local");
    expect(request.searchParams.get("page")).toBe("all");
    expect(request.searchParams.get("query")).toBe("Saved");
    expect(
      screen.queryByRole("combobox", { name: "Library items per page" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Previous|Next/ }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/all shown/)).toBeVisible();
  });
  it("selects every shown title even past the old 200 cap", async () => {
    const items = Array.from({ length: 250 }, (_, index) => ({
      ...item,
      id: index + 1,
      title: `Saved title ${index + 1}`,
    }));
    mock(() =>
      Response.json({
        items,
        total: items.length,
        collection_total: items.length,
        page: 1,
        limit: items.length,
      }),
    );
    mount("/movies?view=table");
    await screen.findByRole("link", { name: "Saved title 1" });
    await userEvent.click(screen.getByRole("button", { name: "Select" }));
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Select all shown" }),
    );
    expect(screen.getByText("250 selected")).toBeVisible();
    expect(screen.getByText(/covers all 250 movies/)).toBeVisible();
    expect(
      screen.queryByText(/covers up to 200|Select all shown covers up to/),
    ).not.toBeInTheDocument();
  });
  it("retains bookmarks and sends literal filters to the server while resetting pages", async () => {
    const fetch = mock();
    mount(
      "/movies?view=table&page=2&status=missing&monitored=monitored&sort=year_desc&query=Saved",
    );
    expect(
      await screen.findByRole("table", { name: "Movie library" }),
    ).toBeVisible();
    expect(screen.getByLabelText("Search saved titles")).toHaveValue("Saved");
    await userEvent.clear(screen.getByLabelText("Search saved titles"));
    await userEvent.type(
      screen.getByLabelText("Search saved titles"),
      "100%_ film",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Apply title filter" }),
    );
    await waitFor(() =>
      expect(screen.getByLabelText("Current address")).not.toHaveTextContent(
        "page=2",
      ),
    );
    const calls = fetch.mock.calls.map(([url]) => new URL(url, "http://local"));
    const last = calls.at(-1)!;
    expect(last.searchParams.get("query")).toBe("100%_ film");
    expect(last.searchParams.get("sort")).toBe("year_desc");
    expect(last.searchParams.get("monitored")).toBe("monitored");
    expect(last.searchParams.has("view")).toBe(false);
    expect(last.searchParams.get("page")).toBe("all");
    await userEvent.click(screen.getByRole("button", { name: "Browser back" }));
    await waitFor(() =>
      expect(screen.getByLabelText("Search saved titles")).toHaveValue("Saved"),
    );
  });
  it("switches poster and table layouts without fetching again or losing the sort", async () => {
    const fetch = mock();
    mount("/movies?sort=added");
    await screen.findByRole("link", { name: "Open Saved title details" });
    const count = fetch.mock.calls.length;
    await userEvent.click(screen.getByRole("button", { name: "Table" }));
    expect(screen.getByRole("table")).toBeVisible();
    expect(fetch).toHaveBeenCalledTimes(count);
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      "sort=added",
    );
    expect(screen.getByRole("link", { name: "Find releases" })).toHaveAttribute(
      "href",
      "/releases?movie=11",
    );
    await userEvent.click(screen.getByRole("button", { name: "Overview" }));
    expect(screen.getByRole("list", { name: "Movies overview" })).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Posters" }));
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByRole("list", { name: "Movies posters" })).toBeVisible();
    expect(fetch).toHaveBeenCalledTimes(count);
  });
  it("sort and monitoring changes preserve the other filters and reset pagination", async () => {
    const fetch = mock();
    mount("/movies?page=2&query=Saved&status=available&view=table");
    await screen.findByRole("table");
    await userEvent.click(screen.getByRole("button", { name: "Sort library" }));
    await userEvent.click(
      await screen.findByRole("menuitemradio", { name: "Title Z–A" }),
    );
    await userEvent.selectOptions(
      screen.getByLabelText("Library monitoring"),
      "paused",
    );
    await waitFor(() =>
      expect(fetch.mock.calls.at(-1)?.[0]).toContain("monitored=paused"),
    );
    const q = new URL(fetch.mock.calls.at(-1)![0], "http://local").searchParams;
    expect(q.get("status")).toBe("available");
    expect(q.get("query")).toBe("Saved");
    expect(q.get("sort")).toBe("title_desc");
    expect(q.get("page")).toBe("all");
    expect(screen.getByRole("table")).toBeVisible();
  });
  it("shows active filters and clears them while keeping sort and view", async () => {
    const fetch = mock();
    mount("/movies?status=missing&sort=year_desc&view=table");
    await screen.findByRole("table");
    expect(
      screen.getByRole("region", { name: "Library filters" }),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Filter (1)" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Clear filters" }),
    );
    await waitFor(() =>
      expect(screen.getByLabelText("Current address")).toHaveTextContent(
        /^\/movies\?sort=year_desc&view=table$/,
      ),
    );
    const q = new URL(fetch.mock.calls.at(-1)![0], "http://local").searchParams;
    expect(q.has("status")).toBe(false);
    expect(q.get("sort")).toBe("year_desc");
    await userEvent.click(screen.getByRole("button", { name: "Filter" }));
    expect(
      screen.queryByRole("region", { name: "Library filters" }),
    ).not.toBeInTheDocument();
  });
  it("groups titles by first letter, ignoring The, with an A–Z jump bar", async () => {
    const items = [
      { ...item, id: 1, title: "The Alpha" },
      { ...item, id: 2, title: "Beta" },
      { ...item, id: 3, title: "42" },
      { ...item, id: 4, title: "the zeta" },
    ];
    mock(() =>
      Response.json({
        items,
        total: items.length,
        collection_total: items.length,
        page: 1,
        limit: items.length,
      }),
    );
    const scroll = vi.spyOn(Element.prototype, "scrollIntoView");
    mount("/movies");
    await screen.findByRole("link", { name: "Open The Alpha details" });
    const jump = screen.getByRole("navigation", { name: "Jump to letter" });
    for (const letter of ["A", "B", "Z", "#"])
      expect(
        within(jump).getByRole("button", {
          name: new RegExp(
            letter === "#" ? "other titles" : `Jump to ${letter}`,
          ),
        }),
      ).toBeVisible();
    for (const letter of ["A", "B", "Z", "#"])
      expect(
        screen.getByRole("heading", { name: letter === "#" ? "#" : letter }),
      ).toBeVisible();
    await userEvent.click(
      within(jump).getByRole("button", { name: "Jump to Z" }),
    );
    expect(scroll).toHaveBeenCalledWith({ block: "start" });
  });
  it("falls back to pages with a clear note when the server ignores page=all", async () => {
    const items = Array.from({ length: 60 }, (_, index) => ({
      ...item,
      id: index + 1,
      title: `Saved title ${index + 1}`,
    }));
    const fetch = mock((url) => {
      const page = new URL(url, "http://local").searchParams.get("page");
      if (page === "all")
        return Response.json({
          items: items.slice(0, 24),
          total: 60,
          collection_total: 60,
          page: 1,
          limit: 24,
        });
      const n = Number(page);
      return Response.json({
        items: items.slice((n - 1) * 24, n * 24),
        total: 60,
        collection_total: 60,
        page: n,
        limit: 24,
      });
    });
    mount("/movies?page=2&query=Saved&view=table");
    expect(await screen.findByText("Showing one page at a time")).toBeVisible();
    expect(
      screen.getByText(/more than the server returns at once/),
    ).toBeVisible();
    await screen.findByRole("table");
    expect(screen.getByText("Page 2 of 3")).toBeVisible();
    expect(fetch.mock.calls[0][0]).toContain("page=all");
    expect(
      screen.queryByRole("combobox", { name: "Library items per page" }),
    ).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /Previous/ }));
    expect(await screen.findByText("Page 1 of 3")).toBeVisible();
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      "query=Saved",
    );
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      "view=table",
    );
  });
  it("falls back to pages when page=all exceeds the server cap", async () => {
    const items = Array.from({ length: 60 }, (_, index) => ({
      ...item,
      id: index + 1,
      title: `Saved title ${index + 1}`,
    }));
    mock((url) => {
      const page = new URL(url, "http://local").searchParams.get("page");
      if (page === "all")
        return Response.json(
          {
            error: {
              message:
                "This library view has more than 5000 items. Narrow the filters or choose a page.",
            },
          },
          { status: 400 },
        );
      const n = Number(page);
      return Response.json({
        items: items.slice((n - 1) * 24, n * 24),
        total: 60,
        collection_total: 60,
        page: n,
        limit: 24,
      });
    });
    mount("/movies?page=2&view=table");
    expect(await screen.findByText("Showing one page at a time")).toBeVisible();
    expect(
      screen.getByText(/more movies than the server returns in one response/),
    ).toBeVisible();
    await screen.findByRole("table");
    expect(screen.getByText("Page 2 of 3")).toBeVisible();
  });
  it("recovers an empty fallback page without discarding filters", async () => {
    mock((url) => {
      const page = new URL(url, "http://local").searchParams.get("page");
      if (page === "all")
        return Response.json({
          items: [{ ...item, id: 1, title: "Saved title 1" }],
          total: 60,
          collection_total: 60,
          page: 1,
          limit: 24,
        });
      return Response.json({
        items: page === "1" ? [{ ...item, id: 1, title: "Saved title 1" }] : [],
        total: 60,
        collection_total: 24,
        page: Number(page),
        limit: 24,
      });
    });
    mount("/movies?page=9&query=Saved&view=table");
    await screen.findByRole("heading", { name: "No movies on this page" });
    await userEvent.click(
      screen.getByRole("button", { name: "Go to first page" }),
    );
    await screen.findByRole("table");
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      "query=Saved",
    );
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      "view=table",
    );
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      "page=1",
    );
  });
  it("describes partial show availability and uses the exact show details link", async () => {
    mock(() =>
      Response.json({
        items: [
          { ...item, kind: "series", status: "available", monitored: false },
        ],
        page: 1,
        limit: 24,
        total: 1,
        collection_total: 1,
      }),
    );
    mount("/series?view=table", "series");
    await screen.findByRole("table", { name: "TV library" });
    expect(
      screen.getByText("Episode files registered", {
        selector: ".rs-label",
      }),
    ).toBeVisible();
    expect(screen.getByText(/not full episode coverage/)).toBeVisible();
    expect(screen.getByRole("link", { name: "Episodes" })).toHaveAttribute(
      "href",
      "/series/11",
    );
    expect(
      within(screen.getByRole("table")).getByText("Paused", {
        selector: ".lib-monitor-text",
      }),
    ).toBeInTheDocument();
  });
  it("lets invalid bookmarks recover through clear filters", async () => {
    mock((url) =>
      url.includes("sort=bad")
        ? Response.json(
            { error: { message: "Choose valid library filters." } },
            { status: 400 },
          )
        : undefined,
    );
    mount("/movies?sort=bad");
    await screen.findByRole("alert");
    await userEvent.click(
      screen.getByRole("button", { name: "Clear library filters" }),
    );
    await screen.findByRole("link", { name: "Open Saved title details" });
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      /^\/movies$/,
    );
  });
  it("keeps panel errors separate and links live catalog counts to working views", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        if (url.endsWith("/dashboard"))
          return Response.json({
            library_name: "Test library",
            counts: { movies: 12, series: 3 },
          });
        if (url.endsWith("/system"))
          return Response.json({
            version: "test",
            database: "connected",
            paths: { media: "/test/media" },
          });
        if (url.endsWith("/wanted"))
          return Response.json({
            items: [
              {
                ...item,
                media_id: 11,
                episode_id: null,
                date: "2026-10-01",
              },
            ],
            counts: { missing: 7, downloading: 2, attention: 1 },
          });
        if (url.endsWith("/downloads?page=1"))
          return Response.json({ items: [], page: 1, total: 0, limit: 24 });
        if (url.endsWith("/events"))
          return Response.json({ items: [], next: null });
        return Response.json(
          { error: { message: "Calendar temporarily unavailable." } },
          { status: 503 },
        );
      }),
    );
    render(
      <Theme>
        <MemoryRouter>
          <Dashboard />
        </MemoryRouter>
      </Theme>,
    );
    await screen.findByRole("heading", { name: "Test library" });
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Calendar temporarily unavailable",
    );
    expect(screen.getByRole("link", { name: /7 Missing/ })).toHaveAttribute(
      "href",
      "/wanted",
    );
    expect(
      screen.getByRole("link", { name: /1 Needs attention/ }),
    ).toHaveAttribute("href", "/wanted?status=attention");
    expect(screen.getByRole("link", { name: /Saved title/ })).toHaveAttribute(
      "href",
      "/movies/11",
    );
    expect(
      screen.getByRole("link", { name: /Connected · test/ }),
    ).toHaveAttribute("href", "/system");
    expect(screen.getByText("No active downloads.")).toBeVisible();
  });
});
