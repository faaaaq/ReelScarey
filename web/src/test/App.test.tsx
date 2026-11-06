import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import App from "../App";

const settings = {
  library_name: "My library",
  movie_folder: "Movies",
  series_folder: "TV Shows",
  revision: 1,
  updated_at: "2026-10-22T00:00:00Z",
};
const system = {
  name: "ReelScarey",
  version: "test",
  schema_version: 1,
  database: "connected",
  paths: { config: "/config", media: "/media", downloads: "/downloads" },
};

function mount(path = "/") {
  render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </Theme>,
  );
}
function setupFetch(save?: (body: unknown) => Response) {
  const mock = vi.fn(async (url: string, options?: RequestInit) => {
    if (url.endsWith("/auth/session"))
      return Response.json({
        setup_required: false,
        authenticated: true,
        session: {
          username: "testadmin",
          csrf_token: "test-csrf",
          expires_at: 9999999999,
        },
      });
    if (url.endsWith("/providers/tmdb"))
      return Response.json({ configured: false, updated_at: "" });
    if (options?.method === "PUT")
      return (
        save?.(JSON.parse(String(options.body))) ??
        Response.json({
          ...settings,
          ...JSON.parse(String(options.body)),
          revision: 2,
        })
      );
    if (url.endsWith("/settings")) return Response.json(settings);
    if (
      url.endsWith("/organization") &&
      (!options?.method || options.method === "GET")
    )
      return Response.json({
        revision: 1,
        main_root: "/media",
        libraries: [
          {
            key: "movies",
            name: "Movies",
            folder: "Movies",
            content: "movie",
            preset: "emby",
            movie_template: "",
            series_template: "",
            episode_template: "",
          },
          {
            key: "tv",
            name: "TV Shows",
            folder: "TV Shows",
            content: "series",
            preset: "emby",
            movie_template: "",
            series_template: "",
            episode_template: "",
          },
          {
            key: "anime",
            name: "Anime",
            folder: "Anime",
            content: "mixed",
            preset: "emby",
            movie_template: "",
            series_template: "",
            episode_template: "",
          },
        ],
      });
    if (url.endsWith("/system")) return Response.json(system);
    if (url.includes("/library"))
      return Response.json({ items: [], limit: 24, total: 0, page: 1 });
    return Response.json({
      library_name: "My library",
      counts: { movies: 0, series: 0 },
      settings_revision: 1,
    });
  });
  vi.stubGlobal("fetch", mock);
  return mock;
}

describe("foundation UI", () => {
  it("loads real API state and navigates to an honest movie empty state", async () => {
    const fetch = setupFetch();
    mount();
    expect(
      await screen.findByRole("heading", { name: "My library" }),
    ).toBeVisible();
    expect(await screen.findByText(/^Connected ·/)).toBeVisible();
    await userEvent.click(screen.getByRole("link", { name: /^Movies$/ }));
    expect(await screen.findByText("No movies in the library")).toBeVisible();
    expect(screen.getByRole("link", { name: /Add movies/i })).toBeVisible();
    expect(
      fetch.mock.calls.some(
        ([url]) => url === "/api/v1/library?kind=movie&page=1",
      ),
    ).toBe(true);
  });

  it("organizes settings into focused sections", async () => {
    setupFetch();
    mount("/settings");
    const general = await screen.findByRole("button", { name: "General" });
    expect(general).toHaveAttribute("aria-current", "page");
    await userEvent.click(screen.getByRole("button", { name: "System" }));
    expect(
      await screen.findByRole("heading", { name: "System" }),
    ).toBeVisible();
    expect(screen.getByText("Server & storage")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Save preferences" }),
    ).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Indexers" }));
    expect(
      screen.getByRole("link", { name: /Trackers and feeds/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /IRC announce feeds/ }),
    ).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Media Management" }),
    );
    expect(await screen.findByText("Collections & folders")).toBeVisible();
    expect(screen.getByLabelText("Collection name 3")).toHaveValue("Anime");
    expect(
      screen.getByRole("button", { name: "Add collection" }),
    ).toBeVisible();
  });

  it("saves a changed preference with the current revision and reports success", async () => {
    const fetch = setupFetch();
    mount("/settings");
    const name = await screen.findByLabelText("Library name");
    expect(
      screen.getByRole("button", { name: "Save preferences" }),
    ).toBeDisabled();
    await userEvent.clear(name);
    await userEvent.type(name, "Friday cinema");
    await userEvent.click(
      screen.getByRole("button", { name: "Save preferences" }),
    );
    expect(await screen.findByText("Preferences saved.")).toBeVisible();
    const saveCall = fetch.mock.calls.find(
      ([, options]) => options?.method === "PUT",
    );
    expect(JSON.parse(String(saveCall?.[1]?.body))).toEqual({
      library_name: "Friday cinema",
      movie_folder: "Movies",
      series_folder: "TV Shows",
      revision: 1,
    });
    expect(
      screen.getByRole("button", { name: "Save preferences" }),
    ).toBeDisabled();
  });

  it("preserves unsaved edits on a revision conflict and allows reload", async () => {
    setupFetch(() =>
      Response.json(
        { error: { message: "Settings changed; reload and try again." } },
        { status: 409 },
      ),
    );
    mount("/settings");
    const name = await screen.findByLabelText("Library name");
    await userEvent.clear(name);
    await userEvent.type(name, "My edits");
    await userEvent.click(
      screen.getByRole("button", { name: "Save preferences" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Settings changed",
    );
    expect(name).toHaveValue("My edits");
    expect(screen.queryByText("Preferences saved.")).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Reload saved settings" }),
    );
    await waitFor(() =>
      expect(screen.getByLabelText("Library name")).toHaveValue("My library"),
    );
  });

  it("shows an actionable error instead of fabricated status when offline", async () => {
    const fetch = vi.fn().mockRejectedValue(new TypeError("offline"));
    vi.stubGlobal("fetch", fetch);
    mount();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Check that the server is running",
    );
    expect(screen.queryByText("System online")).not.toBeInTheDocument();
    const before = fetch.mock.calls.length;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() =>
      expect(fetch.mock.calls.length).toBeGreaterThan(before),
    );
  });
});
