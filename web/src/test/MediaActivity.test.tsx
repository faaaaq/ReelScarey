import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { Theme } from "@radix-ui/themes";
import { describe, expect, it, vi } from "vitest";
import { MediaActivity } from "../pages/details/MediaActivity";

describe("per-item Activity", () => {
  it("shows candidate decisions and real events with recovery links", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          items: [
            {
              id: 4,
              occurred_at: 10,
              kind: "download",
              object_id: "job1",
              title: "Owned release",
              state: "verification failed",
            },
          ],
          candidates: [
            {
              id: "candidate1",
              title: "Owned release",
              scope: "movie",
              published_at: "2026-01-01T00:00:00Z",
              state: "selected for download",
              download_id: "job1",
            },
          ],
        }),
      ),
    );
    render(
      <Theme>
        <MemoryRouter>
          <MediaActivity mediaID={1} />
        </MemoryRouter>
      </Theme>,
    );
    expect(
      await screen.findByRole("table", { name: "Candidate decisions" }),
    ).toBeVisible();
    expect(screen.getByText("selected for download")).toBeVisible();
    expect(screen.getByRole("link", { name: "Open download" })).toHaveAttribute(
      "href",
      "/activity?job=job1",
    );
    expect(screen.getByText("verification failed")).toBeVisible();
    expect(screen.getByRole("link", { name: "Owned release" })).toHaveAttribute(
      "href",
      "/activity?job=job1",
    );
  });

  it("does not fabricate a successful search or refresh when no events exist", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ items: [], candidates: [] })),
    );
    render(
      <Theme>
        <MemoryRouter>
          <MediaActivity mediaID={1} />
        </MemoryRouter>
      </Theme>,
    );
    expect(
      await screen.findByText(/No activity events are recorded/),
    ).toBeVisible();
    expect(
      screen.queryByText(/search succeeded|refresh succeeded/i),
    ).not.toBeInTheDocument();
  });
});
