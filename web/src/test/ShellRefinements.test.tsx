import type { ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { Theme } from "@radix-ui/themes";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { Library } from "../pages/Library";

vi.mock("../auth", () => ({
  AuthGate: ({ children }: { children: ReactNode }) => children,
  useAuth: () => ({
    session: { role: "admin", username: "owner" },
    signOut: vi.fn(),
  }),
}));
vi.mock("../components/HistoryTray", () => ({ HistoryTray: () => null }));
vi.mock("../pages/Settings", () => ({
  Settings: () => <h1>Primary connections</h1>,
}));
vi.mock("../pages/MusicDiscover", () => ({
  MusicDiscover: () => <h1>Music discover</h1>,
}));

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
  const location = useLocation();
  return (
    <output aria-label="Current address">
      {location.pathname + location.search}
    </output>
  );
}

function mountLibrary(path: string) {
  return render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        <Library kind="movie" />
        <Location />
      </MemoryRouter>
    </Theme>,
  );
}

function lastURL(fetch: ReturnType<typeof vi.fn>) {
  return String(fetch.mock.calls.at(-1)![0]);
}

afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("shell and library workflow refinements", () => {
  it("restores remembered library filters when returning to the bare path", async () => {
    const fetch = vi.fn(async () =>
      Response.json({
        items: [item],
        total: 1,
        collection_total: 1,
        page: 1,
        limit: 1,
      }),
    );
    vi.stubGlobal("fetch", fetch);
    const first = mountLibrary("/movies?query=Saved&sort=year&view=table");
    await screen.findByRole("table", { name: "Movie library" });
    first.unmount();

    const fetch2 = vi.fn(async () =>
      Response.json({
        items: [item],
        total: 1,
        collection_total: 1,
        page: 1,
        limit: 1,
      }),
    );
    vi.stubGlobal("fetch", fetch2);
    mountLibrary("/movies");
    await screen.findByRole("table", { name: "Movie library" });
    await waitFor(() =>
      expect(
        new URL(lastURL(fetch2), "http://local").searchParams.get("query"),
      ).toBe("Saved"),
    );
    await waitFor(() =>
      expect(screen.getByLabelText("Current address")).toHaveTextContent(
        "query=Saved",
      ),
    );
  });

  it("deep links to a cleared library keep working over remembered filters", async () => {
    localStorage.setItem(
      "reelscarey.library.movie.params",
      "query=Saved&sort=year",
    );
    const fetch = vi.fn(async () =>
      Response.json({
        items: [],
        total: 0,
        collection_total: 0,
        page: 1,
        limit: 1,
      }),
    );
    vi.stubGlobal("fetch", fetch);
    mountLibrary("/movies?query=Other");
    await waitFor(() =>
      expect(
        new URL(lastURL(fetch), "http://local").searchParams.get("query"),
      ).toBe("Other"),
    );
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      "query=Other",
    );
  });

  it("carries the library browsing state into release review", async () => {
    const fetch = vi.fn(async () =>
      Response.json({
        items: [item],
        total: 1,
        collection_total: 1,
        page: 1,
        limit: 1,
      }),
    );
    vi.stubGlobal("fetch", fetch);
    mountLibrary("/movies?sort=added&view=table");
    await screen.findByRole("table", { name: "Movie library" });
    await userEvent.click(screen.getByRole("link", { name: "Find releases" }));
    expect(screen.getByLabelText("Current address")).toHaveTextContent(
      "/releases?movie=11&return=%2Fmovies%3Fsort%3Dadded",
    );
  });
});

describe("navigation groups the settings and library tasks", () => {
  it("marks the primary connections child as the current page", async () => {
    render(
      <Theme>
        <MemoryRouter initialEntries={["/settings/clients"]}>
          <App />
        </MemoryRouter>
      </Theme>,
    );
    await screen.findByRole("heading", { name: "Primary connections" });
    const child = await screen.findByRole("link", {
      name: "Primary connections",
    });
    await waitFor(() => expect(child).toHaveAttribute("aria-current", "page"));
  });

  it("shows the music discover and server children under Music", async () => {
    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/discover"]}>
          <App />
        </MemoryRouter>
      </Theme>,
    );
    await screen.findByRole("heading", { name: "Music discover" });
    const discover = await screen.findByRole("link", { name: "Discover" });
    await waitFor(() =>
      expect(discover).toHaveAttribute("aria-current", "page"),
    );
    expect(screen.getByRole("link", { name: "Server" })).toBeInTheDocument();
  });
});

describe("activity dialog focus", () => {
  it("returns focus to the row control that opened the job dialog", async () => {
    const job = {
      id: "b".repeat(32),
      movie_id: 1,
      movie_title: "Test Movie",
      title: "Test.Movie.2026.1080p",
      info_hash: "a".repeat(40),
      state: "downloading",
      progress: 0.5,
      total_bytes: 2000000000,
      downloaded_bytes: 1000000000,
      client_state: "downloading",
      selected_file: null,
      choices: [],
      attempts: 0,
      error_code: "",
      error_message: "",
      created_at: 1,
      updated_at: 1,
    };
    const fetch = vi.fn(async (url: string) => {
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
      if (url.includes("/downloads"))
        return Response.json({
          items: [job],
          page: 1,
          total: 1,
          limit: 24,
        });
      if (url.endsWith("/settings"))
        return Response.json({ library_name: "My library", revision: 1 });
      if (url.endsWith("/system"))
        return Response.json({
          paths: {
            config: "/config",
            media: "/media",
            downloads: "/downloads",
          },
          version: "test",
          schema_version: 3,
        });
      return Response.json(
        { error: { message: "Unexpected test request" } },
        { status: 404 },
      );
    });
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <MemoryRouter initialEntries={["/activity"]}>
          <App />
        </MemoryRouter>
      </Theme>,
    );
    const details = await screen.findByRole("button", {
      name: "Details for Test Movie",
    });
    await userEvent.click(details);
    await screen.findByRole("dialog");
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(details).toHaveFocus());
  });
});
