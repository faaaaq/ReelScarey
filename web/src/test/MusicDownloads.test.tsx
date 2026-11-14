import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicLibrary } from "../pages/MusicLibrary";
import { MusicDiscover } from "../pages/MusicDiscover";

const failedJob = {
  id: 5,
  source_url: "",
  source_kind: "top_track",
  title: "Generated album",
  root: "Music",
  format: "source",
  state: "failed",
  track_count: 2,
  completed_count: 0,
  error: "No tracks completed.",
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

function renderWith(
  fetch: ReturnType<typeof commonFetch>,
  node: React.ReactNode,
) {
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter>{node}</MemoryRouter>
    </Theme>,
  );
}

describe("Music download attention banner", () => {
  it("shows the accurate backend issue count", async () => {
    const fetch = commonFetch((url) => {
      if (url.includes("/library/music/acquisitions?"))
        return {
          items: [failedJob],
          page: 1,
          total: 1,
          limit: 20,
          active: 0,
          issues: 1,
        };
      if (url.includes("/library/music/artists"))
        return { items: [], page: 1, total: 0, limit: 100 };
      if (url.includes("/library/music/albums"))
        return { items: [], page: 1, total: 0, limit: 100 };
      if (url.includes("/library/music/tracks"))
        return { items: [], page: 1, total: 0, limit: 100 };
      return undefined;
    });
    renderWith(fetch, <MusicLibrary />);
    // Warning alerts are announced as status regions, not role=alert.
    const banner = await screen.findByText("Music downloads need attention");
    expect(banner.closest('[role="status"]')).not.toBeNull();
    expect(banner.closest('[role="status"]')).toHaveTextContent(
      "1 job has failed or incomplete tracks.",
    );
    // No stray punctuation: the link and its sentence share one paragraph.
    expect(
      banner
        .closest('[role="status"]')
        ?.textContent?.includes("Review the download queue to retry"),
    ).toBe(true);
  });

  it("does not render the warning for a clean queue", async () => {
    const clean = commonFetch((url) => {
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
    });
    renderWith(clean, <MusicLibrary />);
    await screen.findByRole("heading", { name: "Music" });
    expect(
      screen.queryByText("Music downloads need attention"),
    ).not.toBeInTheDocument();
  });
});

describe("Music download queue actions", () => {
  it("retries failed tracks and acknowledges a resolved job", async () => {
    let jobs = [failedJob];
    const fetch = commonFetch((url) => {
      if (url.includes("/library/music/acquisitions?"))
        return {
          items: jobs,
          page: 1,
          total: jobs.length,
          limit: 20,
          active: 0,
          issues: jobs.filter((job) => job.acknowledged_at === 0).length,
        };
      if (url.includes("/library/music/acquisitions/5/retry")) {
        jobs = [{ ...failedJob, state: "running", error: "" }];
        return { ...jobs[0], tracks: [] };
      }
      if (url.includes("/library/music/acquisitions/5/acknowledge")) {
        jobs = [{ ...failedJob, acknowledged_at: 1700000000 }];
        return jobs[0];
      }
      if (url.endsWith("/library/music/charts")) return { items: [], total: 0 };
      if (url.endsWith("/library/music/monitor"))
        return {
          enabled: false,
          interval_hours: 24,
          next_run: 0,
          last_run: 0,
          last_error: "",
          revision: 1,
        };
      if (url.endsWith("/library/music/monitor/sources")) return { items: [] };
      return undefined;
    });
    renderWith(fetch, <MusicDiscover />);
    const table = await screen.findByRole("table", {
      name: "Recent music downloads",
    });
    const row = within(table).getByText("Generated album").closest("tr");
    expect(row).not.toBeNull();
    // Clear the attention state without deleting the job or its history.
    await userEvent.click(
      within(row as HTMLElement).getByRole("button", { name: "Clear" }),
    );
    await screen.findByText(/acknowledged/);
    expect(
      fetch.mock.calls.some(([url]) =>
        String(url).includes("/acquisitions/5/acknowledge"),
      ),
    ).toBe(true);
    // Retry requeues the failed tracks in the same job.
    await userEvent.click(
      within(row as HTMLElement).getByRole("button", {
        name: "Retry failed tracks",
      }),
    );
    await screen.findByText(/running/);
    expect(
      fetch.mock.calls.some(([url]) =>
        String(url).includes("/acquisitions/5/retry"),
      ),
    ).toBe(true);
  });
});
