import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Library } from "../pages/Library";
import { Search } from "../pages/Search";
import { AnimeSettings } from "../pages/AnimeSettings";
import type { AnimeLibrarySettings } from "../api";

function locationForTest() {
  const location = useLocation();
  return (
    <output aria-label="Current address">
      {location.pathname}
      {location.search}
    </output>
  );
}
const LocationForTest = locationForTest;
function mount(child: React.ReactNode, path = "/anime") {
  render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        {child}
        <LocationForTest />
      </MemoryRouter>
    </Theme>,
  );
}
function stateForTest(): AnimeLibrarySettings {
  return {
    id: 11,
    kind: "series",
    category: "standard",
    revision: 3,
    numbering_revision: 7,
    episodes: [
      {
        episode_id: 101,
        season: 1,
        number: 1,
        title: "The opening",
        absolute_number: 12,
      },
      {
        episode_id: 102,
        season: 1,
        number: 2,
        title: "The return",
        absolute_number: null,
      },
      {
        episode_id: 201,
        season: 2,
        number: 1,
        title: "Next season",
        absolute_number: 15,
      },
    ],
  };
}
function editorFetch(
  extra?: (
    url: string,
    init?: RequestInit,
  ) => Response | Promise<Response> | undefined,
) {
  const state = stateForTest();
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    const result = await extra?.(url, init);
    if (result) return result;
    if (init?.method === "PUT")
      return Response.json({
        ...state,
        ...JSON.parse(String(init.body)),
        revision: 4,
        numbering_revision: 8,
      });
    return Response.json(state);
  });
  vi.stubGlobal("fetch", fetch);
  return { fetch, state };
}
async function openEditor() {
  await userEvent.click(
    screen.getByRole("button", { name: "Edit library section" }),
  );
  await screen.findByRole("combobox", { name: "Library section" });
}

describe("Anime library and explicit numbering", () => {
  it("links mixed anime formats correctly and reviews each selected kind separately", async () => {
    const items = [
      {
        id: 11,
        title: "Anime feature",
        kind: "movie",
        monitored: true,
        year: 2020,
        status: "missing",
        poster_url: "",
      },
      {
        id: 12,
        title: "Anime serial",
        kind: "series",
        monitored: false,
        year: 2021,
        status: "missing",
        poster_url: "",
      },
    ];
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/monitoring/preview")) {
        const body = JSON.parse(String(init?.body));
        return Response.json({
          items: items
            .filter((item) => body.ids.includes(item.id))
            .map((item) => ({
              ...item,
              series_revision: item.kind === "series" ? 2 : 0,
              automation_revision: 3,
            })),
        });
      }
      return Response.json({
        items,
        collection_total: 2,
        total: 2,
        page: 1,
        limit: 24,
      });
    });
    vi.stubGlobal("fetch", fetch);
    mount(<Library kind="anime" />, "/anime?view=table");
    await screen.findByRole("table", { name: "Anime library" });
    expect(screen.getByRole("link", { name: "Anime feature" })).toHaveAttribute(
      "href",
      "/movies/11",
    );
    expect(screen.getByRole("link", { name: "Anime serial" })).toHaveAttribute(
      "href",
      "/series/12",
    );
    expect(screen.getByRole("link", { name: "Add New" })).toHaveAttribute(
      "href",
      "/search?kind=anime&format=series",
    );
    expect(
      screen.getByRole("link", { name: "Library Import" }),
    ).toHaveAttribute("href", "/import?collection=anime");
    await userEvent.click(screen.getByRole("button", { name: "Select" }));
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Select all shown" }),
    );
    const movies = screen.getByRole("region", {
      name: "Selected anime movies",
    });
    const shows = screen.getByRole("region", { name: "Selected anime shows" });
    await userEvent.click(
      within(movies).getByRole("button", { name: "Review monitoring" }),
    );
    await screen.findByRole("dialog");
    expect(
      fetch.mock.calls.find(([url]) => url.endsWith("/monitoring/preview"))?.[1]
        ?.body,
    ).toBe(JSON.stringify({ kind: "movie", ids: [11] }));
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await userEvent.click(
      within(shows).getByRole("button", { name: "Review monitoring" }),
    );
    await screen.findByRole("dialog");
    expect(
      fetch.mock.calls
        .filter(([url]) => url.endsWith("/monitoring/preview"))
        .at(-1)?.[1]?.body,
    ).toBe(JSON.stringify({ kind: "series", ids: [12] }));
  });

  it("adds an anime film to Movies and retains Anime in search pagination", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/providers/tmdb"))
        return Response.json({ configured: true });
      if (init?.method === "POST")
        return Response.json({
          created: true,
          item: { id: 11, category: "standard" },
        });
      return Response.json({
        items: [
          {
            tmdb_id: 42,
            title: "Anime feature",
            year: 2025,
            overview: "A film",
            poster_url: "",
            in_library: false,
          },
        ],
        page:
          new URL(url, "http://test").searchParams.get("page") === "2" ? 2 : 1,
        total_pages: 2,
        total_results: 2,
      });
    });
    vi.stubGlobal("fetch", fetch);
    mount(<Search />, "/search?kind=anime&format=movie&query=Feature");
    await screen.findByRole("heading", { name: "Anime feature" });
    expect(
      screen.getByRole("heading", { name: "Search anime movies" }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Add monitored" }),
    );
    await screen.findByText("In library");
    expect(
      fetch.mock.calls.find(
        ([url, init]) =>
          url.endsWith("/library/movies") && init?.method === "POST",
      )?.[1]?.body,
    ).toBe(JSON.stringify({ tmdb_id: 42, monitored: true }));
    await userEvent.click(screen.getByRole("button", { name: /Next/ }));
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      "kind=anime&format=movie&query=Feature&page=2",
    );
  });

  it("reviews category and explicit aliases without writing, then cancels", async () => {
    const { fetch } = editorFetch();
    mount(<AnimeSettings id={11} />);
    expect(fetch).not.toHaveBeenCalled();
    await openEditor();
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Library section" }),
      "anime",
    );
    const number = screen.getByRole("textbox", {
      name: "Absolute number for S01E02 · The return",
    });
    await userEvent.type(number, "13");
    await userEvent.click(
      screen.getByRole("button", { name: "Review changes" }),
    );
    expect(
      screen.getByRole("region", { name: "Review library section changes" }),
    ).toHaveTextContent("Unknown → 13");
    expect(
      screen.getByText(/Automatic Anime searches are off until enabled/),
    ).toBeVisible();
    expect(fetch.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(
      false,
    );
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      screen.getByRole("button", { name: "Edit library section" }),
    ).toBeVisible();
    expect(fetch.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(
      false,
    );
  });

  it("saves the reviewed complete alias list with both revisions and preserves hidden seasons", async () => {
    const { fetch } = editorFetch();
    const onSaved = vi.fn();
    mount(<AnimeSettings id={11} onSaved={onSaved} />);
    await openEditor();
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Library section" }),
      "anime",
    );
    await userEvent.type(
      screen.getByRole("textbox", {
        name: "Absolute number for S01E02 · The return",
      }),
      "13",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review changes" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed changes" }),
    );
    await screen.findByText("Library section and episode numbering saved.");
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([, init]) => init?.method === "PUT")?.[1]
            ?.body,
        ),
      ),
    ).toEqual({
      category: "anime",
      revision: 3,
      numbering_revision: 7,
      episodes: [
        { episode_id: 101, absolute_number: 12 },
        { episode_id: 102, absolute_number: 13 },
        { episode_id: 201, absolute_number: 15 },
      ],
    });
    expect(onSaved).toHaveBeenCalledOnce();
  });

  it("rejects guessed duplicates and out-of-range numbers before review", async () => {
    const { fetch } = editorFetch();
    mount(<AnimeSettings id={11} />);
    await openEditor();
    const number = screen.getByRole("textbox", {
      name: "Absolute number for S01E02 · The return",
    });
    await userEvent.type(number, "12");
    await userEvent.click(
      screen.getByRole("button", { name: "Review changes" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Each absolute number must identify one episode",
    );
    await userEvent.clear(number);
    await userEvent.type(number, "100001");
    await userEvent.click(
      screen.getByRole("button", { name: "Review changes" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("1 to 100000");
    expect(
      screen.queryByRole("button", { name: "Save reviewed changes" }),
    ).not.toBeInTheDocument();
    expect(fetch.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(
      false,
    );
  });

  it("locks stale edits after a conflict and reloads saved state before another review", async () => {
    const { fetch } = editorFetch((_url, init) =>
      init?.method === "PUT"
        ? Response.json(
            { error: { message: "The numbering changed." } },
            { status: 409 },
          )
        : undefined,
    );
    mount(<AnimeSettings id={11} />);
    await openEditor();
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Library section" }),
      "anime",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review changes" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed changes" }),
    );
    await screen.findByRole("alert");
    expect(
      screen.getByRole("combobox", { name: "Library section" }),
    ).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "Save reviewed changes" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", {
        name: "Reload saved section and numbering",
      }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("combobox", { name: "Library section" }),
      ).toBeEnabled(),
    );
    expect(
      screen.getByRole("combobox", { name: "Library section" }),
    ).toHaveValue("standard");
    expect(
      fetch.mock.calls.filter(([, init]) => init?.method === "PUT"),
    ).toHaveLength(1);
  });
});
