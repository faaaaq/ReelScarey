import { render, screen, waitFor } from "@testing-library/react";
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
        total: 40,
        collection_total: 60,
        page: Number(
          new URL(url, "http://local").searchParams.get("page") ?? 1,
        ),
        limit: 24,
      }),
  );
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("catalog browsing", () => {
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
    await userEvent.click(screen.getByRole("button", { name: /^Filter$/ }));
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
    expect(last.searchParams.get("page")).toBe("1");
    await userEvent.click(screen.getByRole("button", { name: "Browser back" }));
    await waitFor(() =>
      expect(screen.getByLabelText("Search saved titles")).toHaveValue("Saved"),
    );
  });
  it("switches poster and table layouts without fetching again or losing the current page", async () => {
    const fetch = mock();
    mount("/movies?page=2&sort=added");
    await screen.findByRole("link", { name: "Open Saved title details" });
    const count = fetch.mock.calls.length;
    await userEvent.click(screen.getByRole("button", { name: "Table view" }));
    expect(screen.getByRole("table")).toBeVisible();
    expect(fetch).toHaveBeenCalledTimes(count);
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      "page=2",
    );
    expect(screen.getByRole("link", { name: "Find releases" })).toHaveAttribute(
      "href",
      "/releases?movie=11",
    );
    await userEvent.click(screen.getByRole("button", { name: "Poster view" }));
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(count);
  });
  it("sort and monitoring changes preserve the other filters and reset pagination", async () => {
    const fetch = mock();
    mount("/movies?page=2&query=Saved&status=available&view=table");
    await screen.findByRole("table");
    await userEvent.selectOptions(
      screen.getByLabelText("Sort library"),
      "title_desc",
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
    expect(q.get("page")).toBe("1");
    expect(screen.getByRole("table")).toBeVisible();
  });
  it("recovers empty later pages without discarding filters", async () => {
    mock((url) =>
      new URL(url, "http://local").searchParams.get("page") === "9"
        ? Response.json({
            items: [],
            total: 1,
            collection_total: 9,
            page: 9,
            limit: 24,
          })
        : undefined,
    );
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
        selector: ".catalog-state",
      }),
    ).toBeVisible();
    expect(screen.getByText(/does not mean every episode/)).toBeVisible();
    expect(screen.getByRole("link", { name: "Episodes" })).toHaveAttribute(
      "href",
      "/series/11",
    );
    expect(screen.getByText("Paused", { selector: "td" })).toBeVisible();
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
  it("keeps overview errors separate and links live catalog counts to working views", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) =>
        url.endsWith("/dashboard")
          ? Response.json({
              library_name: "Test library",
              counts: { movies: 12, series: 3 },
            })
          : url.endsWith("/system")
            ? Response.json({
                version: "test",
                paths: { media: "/test/media" },
              })
            : url.endsWith("/wanted")
              ? Response.json({
                  items: [
                    {
                      ...item,
                      media_id: 11,
                      episode_id: null,
                      date: "2026-10-01",
                    },
                  ],
                  counts: { missing: 7, downloading: 2, attention: 1 },
                })
              : Response.json(
                  { error: { message: "Calendar temporarily unavailable." } },
                  { status: 503 },
                ),
      ),
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
  });
});
