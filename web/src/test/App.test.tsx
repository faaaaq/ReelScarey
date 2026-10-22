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
    if (url.endsWith("/system")) return Response.json(system);
    if (url.includes("/library"))
      return Response.json({ items: [], limit: 100 });
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
    expect(await screen.findByText("Connected")).toBeVisible();
    await userEvent.click(screen.getByRole("link", { name: /^Movies$/ }));
    expect(
      await screen.findByText("Your first feature is still to come."),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: /add movie/i }),
    ).not.toBeInTheDocument();
    expect(
      fetch.mock.calls.some(([url]) => url === "/api/v1/library?kind=movie"),
    ).toBe(true);
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
