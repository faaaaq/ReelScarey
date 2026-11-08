import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicWanted } from "../pages/MusicWanted";

function mockFetch(
  handler: (path: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const path = new URL(url, "http://local").pathname;
    return handler(path, options) ?? Response.json({});
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("music wanted", () => {
  it("lists wanted albums and saves the schedule", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/wanted") {
        return Response.json({
          items: [
            {
              id: 2,
              artist_id: 1,
              artist_name: "Radiohead",
              title: "Kid A",
              year: 2000,
              status: "missing",
              track_count: 10,
            },
          ],
          page: 1,
          total: 1,
          limit: 48,
        });
      }
      if (path === "/api/v1/library/music/preferences" && !options?.method) {
        return Response.json({
          allow_lossless: true,
          min_bitrate: 0,
          preferred_formats: ["FLAC", "ALAC", "MP3"],
          revision: 1,
        });
      }
      if (path === "/api/v1/library/music/schedule" && !options?.method) {
        return Response.json({
          enabled: false,
          interval_hours: 24,
          mode: "search",
          next_run: 0,
          last_run: 0,
          last_error: "",
          revision: 1,
        });
      }
      if (
        path === "/api/v1/library/music/schedule" &&
        options?.method === "PUT"
      ) {
        return Response.json({
          enabled: true,
          interval_hours: 24,
          mode: "search",
          next_run: 123,
          last_run: 0,
          last_error: "",
          revision: 2,
        });
      }
      if (path === "/api/v1/library/music/downloads") {
        return Response.json({ items: [], page: 1, total: 0, limit: 24 });
      }
      if (path === "/api/v1/library/music/upgrades") {
        return Response.json({ items: [] });
      }
      return undefined;
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/wanted"]}>
          <MusicWanted />
        </MemoryRouter>
      </Theme>,
    );

    expect(
      await screen.findByRole("table", { name: "Wanted music albums" }),
    ).toBeVisible();
    expect(screen.getByText("Kid A")).toBeVisible();
    await userEvent.click(screen.getByLabelText("Enabled"));
    await userEvent.click(screen.getAllByRole("button", { name: "Save" })[1]);
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/music/schedule" && options?.method === "PUT",
        ),
      ).toBe(true),
    );
  });
});
