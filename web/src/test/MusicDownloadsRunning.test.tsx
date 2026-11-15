import { render, screen } from "@testing-library/react";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MusicLibrary } from "../pages/MusicLibrary";

const runningJob = {
  id: 9,
  source_url: "",
  source_kind: "top_track",
  title: "In-flight album",
  root: "Music",
  format: "source",
  state: "running",
  track_count: 3,
  completed_count: 1,
  error: "",
  created_at: 1,
  updated_at: 1,
  acknowledged_at: 0,
};

function commonFetch(handler: (url: string) => unknown) {
  return vi.fn(async (url: string) => {
    const body = handler(url);
    if (body !== undefined) return Response.json(body as object);
    return Response.json(
      { error: { message: "Unexpected request" } },
      { status: 404 },
    );
  });
}

function renderWith(fetch: ReturnType<typeof commonFetch>) {
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter>
        <MusicLibrary />
      </MemoryRouter>
    </Theme>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Music downloads running banner", () => {
  it("announces active downloads as an info status with a queue link", async () => {
    renderWith(
      commonFetch((url) => {
        if (url.includes("/library/music/acquisitions?"))
          return {
            items: [runningJob],
            page: 1,
            total: 1,
            limit: 20,
            active: 1,
            issues: 0,
          };
        if (url.includes("/library/music/artists"))
          return { items: [], page: 1, total: 0, limit: 100 };
        if (url.includes("/library/music/albums"))
          return { items: [], page: 1, total: 0, limit: 100 };
        if (url.includes("/library/music/tracks"))
          return { items: [], page: 1, total: 0, limit: 100 };
        return undefined;
      }),
    );
    // Info alerts are announced as status regions like the warning banner.
    const banner = await screen.findByText("Music downloads running");
    expect(banner.closest('[role="status"]')).not.toBeNull();
    expect(banner.closest('[role="status"]')).toHaveTextContent(
      "1 job is being processed automatically.",
    );
    expect(banner.closest('[role="status"]')).toHaveTextContent(
      "View the download queue",
    );
    // No warning while nothing needs attention.
    expect(
      screen.queryByText("Music downloads need attention"),
    ).not.toBeInTheDocument();
  });

  it("does not render the running banner for an idle queue", async () => {
    renderWith(
      commonFetch((url) => {
        if (url.includes("/library/music/acquisitions?"))
          return {
            items: [],
            page: 1,
            total: 0,
            limit: 20,
            active: 0,
            issues: 0,
          };
        if (url.includes("/library/music/artists"))
          return { items: [], page: 1, total: 0, limit: 100 };
        if (url.includes("/library/music/albums"))
          return { items: [], page: 1, total: 0, limit: 100 };
        if (url.includes("/library/music/tracks"))
          return { items: [], page: 1, total: 0, limit: 100 };
        return undefined;
      }),
    );
    await screen.findByRole("heading", { name: "Music" });
    expect(
      screen.queryByText("Music downloads running"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("Music downloads need attention"),
    ).not.toBeInTheDocument();
  });
});
