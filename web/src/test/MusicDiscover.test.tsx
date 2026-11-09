import { render, screen } from "@testing-library/react";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicDiscover } from "../pages/MusicDiscover";

describe("music discover", () => {
  it("keeps playlist monitoring paused and explains the port model", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        const path = new URL(url, "http://local").pathname;
        if (path === "/api/v1/library/music/charts") {
          return Response.json({
            source: "Deezer charts",
            items: [],
            message: "",
          });
        }
        if (path === "/api/v1/library/music/monitor") {
          return Response.json({
            enabled: false,
            interval_hours: 24,
            next_run: 0,
            last_run: 0,
            last_error: "",
            revision: 1,
            updated_at: 1,
          });
        }
        if (path === "/api/v1/library/music/monitor/sources") {
          return Response.json({ items: [] });
        }
        if (path === "/api/v1/library/music/acquisitions") {
          return Response.json({
            items: [],
            page: 1,
            total: 0,
            limit: 20,
          });
        }
        return Response.json({});
      }),
    );

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/discover"]}>
          <MusicDiscover />
        </MemoryRouter>
      </Theme>,
    );

    expect(
      await screen.findByRole("heading", { name: "Music discover" }),
    ).toBeVisible();
    expect(screen.getByText("Playlist monitor")).toBeVisible();
    expect(screen.getByText("Opt-in schedule")).toBeVisible();
    expect(
      (
        screen.getByRole("checkbox", {
          name: "Enable playlist monitor",
        }) as HTMLInputElement
      ).checked,
    ).toBe(false);
  });
});
