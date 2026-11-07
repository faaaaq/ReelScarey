import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, it, expect, vi } from "vitest";
import App from "../App";
import type { Indexer } from "../pages/Indexers";

beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

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
    if (url.endsWith("/franchise"))
      return Response.json({ source: "Wikidata", items: [], truncated: false });
    if (url.endsWith("/library/movies/1/versions"))
      return Response.json({
        enabled: false,
        hd_profile_id: 1,
        uhd_profile_id: 2,
        revision: 0,
        hd_files: 1,
        uhd_files: 0,
        hd_download: "",
        uhd_download: "",
      });
    if (url.endsWith("/quality-profiles")) return Response.json({ items: [] });
    return (
      extra(url, options) ??
      Response.json(
        { error: { message: "Unexpected fixture request" } },
        { status: 404 },
      )
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
const source: Indexer = {
  id: "a".repeat(32),
  name: "Private tracker",
  kind: "iptorrents",
  url: "https://tracker.invalid",
  enabled: true,
  revision: 1,
  user_agent: "Fixture browser",
  freeleech_only: false,
  has_cookie: true,
  has_feed: false,
  has_api_key: false,
  legacy: false,
  last_test_at: 1,
  last_search_at: 0,
  last_error: "",
  last_result_count: 0,
};
const local = {
  item: {
    id: 1,
    kind: "movie",
    title: "Fixture movie",
    tmdb_id: 11,
    year: 2026,
    monitored: true,
    status: "available",
    overview: "Disposable synopsis",
    poster_url: "",
    runtime_minutes: 96,
  },
  information: {
    tagline: "Saved details",
    genres: ["Drama"],
    rating: 8.2,
    votes: 10,
    imdb_url: "https://www.imdb.com/title/tt1234567/",
    tmdb_url: "https://www.themoviedb.org/movie/11",
    trailer_url: "https://www.youtube.com/watch?v=abcdefghijk",
    cast: [{ id: 3, name: "Fixture actor", role: "Lead", image_url: "" }],
    crew: [
      { id: 4, name: "Fixture director", role: "Director", image_url: "" },
      { id: 5, name: "Fixture writer", role: "Writer", image_url: "" },
    ],
  },
  metadata_updated_at: Math.floor(Date.now() / 1000),
  media_root: "/media",
  library_root: "/media/Movies",
  total_size_bytes: 1073741824,
  file_count: 1,
  history_count: 1,
  page: 1,
  files: [
    {
      id: 2,
      path: "/media/Movies/Fixture/Fixture.mkv",
      directory: "/media/Movies/Fixture",
      size_bytes: 1073741824,
      imported_at: 1,
      download_id: "b".repeat(32),
      release: "Fixture.Movie.2026.1080p",
      episodes: "",
      probe: {
        codec: "h264",
        width: 1920,
        height: 1080,
        duration_seconds: 5760,
      },
    },
  ],
  history: [
    {
      id: "b".repeat(32),
      title: "Fixture.Movie.2026.1080p",
      indexer: "Private tracker",
      state: "completed",
      progress: 1,
      size_bytes: 1073741824,
      created_at: 1,
      updated_at: 2,
      error: "",
    },
  ],
};
describe("indexers and library details", () => {
  it("inspects and displays embedded audio language tags", async () => {
    let inspected = false;
    const fetch = setup((url, options) => {
      if (
        url.endsWith("/library/files/2/inspect") &&
        options?.method === "POST"
      ) {
        inspected = true;
        return Response.json({
          audio_streams: [{ language: "jpn", codec: "aac", title: "" }],
        });
      }
      if (url.includes("/library/items/1?"))
        return Response.json({
          ...local,
          files: local.files.map((file) => ({
            ...file,
            probe: {
              ...file.probe,
              ...(inspected
                ? {
                    embedded_title: "Wrong fixture title",
                    audio_streams: [
                      { language: "jpn", codec: "aac", title: "" },
                    ],
                  }
                : {}),
            },
          })),
        });
    });
    mount("/movies/1");
    await screen.findByText("Not inspected");
    await userEvent.click(
      screen.getByRole("button", { name: "Inspect embedded tags" }),
    );
    await screen.findByText("Audio: jpn · aac");
    expect(screen.getByText("Title: Wrong fixture title")).toBeVisible();
    expect(screen.getByText(/differs from the saved title/i)).toBeVisible();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/library/files/2/inspect",
      expect.objectContaining({ method: "POST", body: "{}" }),
    );
  });
  it("adds a public NYAA source without credentials and uses CSRF", async () => {
    let items: Indexer[] = [];
    const fetch = setup((url, options) => {
      if (url.endsWith("/indexers") && options?.method === "POST") {
        const body = JSON.parse(String(options.body));
        items = [
          { ...source, ...body, kind: "nyaa", name: "NYAA", has_cookie: false },
        ];
        return Response.json(items[0], { status: 201 });
      }
      if (url.endsWith("/indexers")) return Response.json({ items });
    });
    mount("/indexers");
    await screen.findByText("Add your first indexer");
    await userEvent.click(screen.getByRole("button", { name: "Add Indexer" }));
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Indexer type" }),
      "nyaa",
    );
    expect(screen.queryByLabelText("Session cookie")).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Save & test connection" }),
    );
    await screen.findByText("Indexer verified and saved.");
    const call = fetch.mock.calls.find(
      ([, options]) => options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({
      kind: "nyaa",
      url: "https://nyaa.si",
      enabled: true,
    });
    expect(JSON.parse(String(call?.[1]?.body))).not.toHaveProperty("cookie");
    expect(call?.[1]?.headers).toMatchObject({
      "X-CSRF-Token": "fixture-csrf",
    });
  });
  it("preserves a saved private cookie on edit and can disable an offline source", async () => {
    let current = { ...source, last_error: "The tracker is offline." };
    const fetch = setup((url, options) => {
      if (url.endsWith("/enabled")) {
        current = { ...current, enabled: false, revision: 2 };
        return Response.json(current);
      }
      if (url.endsWith(`/indexers/${source.id}`) && options?.method === "PUT") {
        current = { ...current, revision: 3 };
        return Response.json(current);
      }
      if (url.endsWith("/indexers")) return Response.json({ items: [current] });
    });
    mount("/indexers");
    await screen.findByText("Needs attention");
    await userEvent.click(
      screen.getByRole("switch", { name: "Enable Private tracker" }),
    );
    await screen.findByText("Disabled");
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/test"))).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.getByLabelText("Session cookie")).toHaveValue("");
    await userEvent.click(
      screen.getByRole("button", { name: "Save & test connection" }),
    );
    await screen.findByText("Indexer verified and saved.");
    const call = fetch.mock.calls.find(
      ([url, options]) => url.endsWith(source.id) && options?.method === "PUT",
    );
    const body = JSON.parse(String(call?.[1]?.body));
    expect(body.revision).toBe(2);
    expect(body).not.toHaveProperty("cookie");
  });
  it("shows cast, credits, trailer, storage and original release history", async () => {
    setup((url) =>
      url.includes("/library/items/1?") ? Response.json(local) : undefined,
    );
    mount("/movies/1");
    await screen.findByRole("heading", { name: "Fixture movie" });
    expect(screen.getByText("Fixture actor")).toBeVisible();
    expect(screen.getByText("Fixture director")).toBeVisible();
    expect(screen.getByText("Fixture writer")).toBeVisible();
    expect(screen.getByRole("link", { name: /IMDb/ })).toHaveAttribute(
      "href",
      local.information.imdb_url,
    );
    expect(screen.getByRole("link", { name: /Trailer/ })).toHaveAttribute(
      "rel",
      "noopener noreferrer",
    );
    expect(screen.getByText(local.files[0].path)).toBeVisible();
    expect(screen.getByRole("link", { name: "View download" })).toHaveAttribute(
      "href",
      `/activity?job=${"b".repeat(32)}`,
    );
    const history = within(
      screen.getByRole("table", { name: "Download history" }),
    );
    expect(
      history.getByRole("cell", { name: "Private tracker" }),
    ).toBeVisible();
    expect(history.getByRole("link", { name: "Details" })).toHaveAttribute(
      "href",
      `/activity?job=${"b".repeat(32)}`,
    );
  });
  it("keeps cached credits and imported files when metadata refresh fails", async () => {
    const fetch = setup((url, options) =>
      url.endsWith("/metadata") && options?.method === "POST"
        ? Response.json(
            { error: { message: "TMDb unavailable" } },
            { status: 503 },
          )
        : url.includes("/library/items/1?")
          ? Response.json({ ...local, metadata_updated_at: 1 })
          : undefined,
    );
    mount("/movies/1");
    await screen.findByRole("alert");
    expect(screen.getByText("Fixture actor")).toBeVisible();
    expect(screen.getByText(local.files[0].path)).toBeVisible();
    await waitFor(() =>
      expect(
        fetch.mock.calls.filter(([url]) => url.endsWith("/metadata")),
      ).toHaveLength(1),
    );
    expect(
      within(screen.getByRole("alert")).getByRole("button", {
        name: "Retry metadata",
      }),
    ).toBeEnabled();
  });
});
