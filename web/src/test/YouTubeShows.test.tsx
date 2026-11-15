import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { ShowsOverview } from "../pages/youtube/ShowsOverview";

const sources = {
  items: [
    {
      id: "src1",
      media_id: 77,
      show_title: "X or Go",
      source_url: "https://www.youtube.com/playlist?list=PL1",
      mapping: "order",
      season_number: 1,
      quality: "best",
      auto_import: true,
      enabled: true,
      initialized: true,
      revision: 4,
      state: "ok",
      message: "",
      next_run: 1800000000,
      last_checked: 1700000000,
      counts: {
        entries: 12,
        queued: 1,
        downloading: 1,
        awaiting_review: 2,
        completed: 3,
        downloaded: 4,
        missing: 5,
      },
      show: { id: 77, title: "X or Go" },
    },
    {
      id: "src2",
      media_id: 88,
      show_title: "Other show",
      source_url: "https://www.youtube.com/playlist?list=PL2",
      mapping: "episode",
      season_number: 0,
      quality: "1080",
      auto_import: false,
      enabled: false,
      initialized: false,
      revision: 9,
      state: "ok",
      message: "Waiting for the first check.",
      next_run: 0,
      last_checked: 0,
      counts: {
        entries: 0,
        queued: 0,
        downloading: 0,
        awaiting_review: 0,
        completed: 0,
        downloaded: 0,
        missing: 0,
      },
      show: { id: 88, title: "Other show" },
    },
  ],
};

function mount(response: unknown, routes: Record<string, unknown> = {}) {
  const fetch = vi.fn(async (url: string, _options?: RequestInit) => {
    const extra = routes[url.replace("/api/v1", "")];
    if (extra !== undefined) return Response.json(extra);
    if (url.endsWith("/youtube/sources")) return Response.json(response);
    return Response.json(
      { error: { message: "Unexpected request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter>
        <ShowsOverview />
      </MemoryRouter>
    </Theme>,
  );
  return fetch;
}

describe("YouTube shows overview", () => {
  it("renders one card per source with counts, mapping and show link", async () => {
    mount(sources);
    const showLink = await screen.findByRole("link", { name: "X or Go" });
    expect(showLink).toHaveAttribute("href", "/series/77");
    const card = showLink.closest("li");
    expect(card).not.toBeNull();
    expect(within(card as HTMLElement).getByText("Active")).toBeVisible();
    expect(
      within(card as HTMLElement).getByText((_, element) =>
        (element?.textContent ?? "").startsWith("Playlist order · Season 1"),
      ),
    ).toBeVisible();
    expect(
      within(card as HTMLElement).getByText("Review needed: 2"),
    ).toBeVisible();
    expect(within(card as HTMLElement).getByText("Missing: 5")).toBeVisible();
    const paused = await screen.findByText("Other show");
    const pausedCard = paused.closest("li");
    expect(within(pausedCard as HTMLElement).getByText("Paused")).toBeVisible();
    expect(
      within(pausedCard as HTMLElement).getByText(
        "Waiting for the first check.",
      ),
    ).toBeVisible();
  });

  it("pauses and checks a source through the per-show APIs", async () => {
    const fetch = mount(sources, {
      "/youtube/sources": sources,
      "/library/series/77/youtube-sources": { id: "src1" },
      "/library/series/77/youtube-sources/src1": { requested: true },
    });
    await screen.findByRole("link", { name: "X or Go" });
    const card = screen.getAllByRole("listitem")[0];
    await userEvent.click(within(card).getByRole("button", { name: "Pause" }));
    expect(await screen.findByText("Source paused.")).toBeVisible();
    const pause = fetch.mock.calls.find(
      ([url]) => url === "/api/v1/library/series/77/youtube-sources",
    );
    expect(pause).toBeDefined();
    expect(JSON.parse(String(pause?.[1]?.body))).toMatchObject({
      enabled: false,
      revision: 4,
      reviewed: true,
      mapping: "order",
      source_url: "https://www.youtube.com/playlist?list=PL1",
    });
    await userEvent.click(
      within(card).getByRole("button", { name: "Check now" }),
    );
    expect(await screen.findByText("Check requested.")).toBeVisible();
    const check = fetch.mock.calls.find(
      ([url]) => url === "/api/v1/library/series/77/youtube-sources/src1",
    );
    expect(JSON.parse(String(check?.[1]?.body))).toEqual({ revision: 4 });
  });

  it("explains how to add the first source", async () => {
    mount({ items: [] });
    expect(await screen.findByText(/No YouTube sources yet/)).toBeVisible();
  });
});
