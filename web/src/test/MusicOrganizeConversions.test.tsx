import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicOrganize } from "../pages/MusicOrganize";

const file = {
  id: 1,
  artist_id: 1,
  album_id: 2,
  track_id: 3,
  artist_name: "Artist",
  album_title: "Album",
  track_title: "Song",
  disc: 1,
  number: 1,
  relative_path: "Music/Artist/Album/Song.flac",
  format: "flac",
  duration_ms: 1000,
  size_bytes: 7,
};

function conversion(
  id: number,
  state: string,
  error = "",
): Record<string, unknown> {
  return {
    id,
    kind: "library",
    file_id: 1,
    profile: "opus-balanced",
    state,
    priority: 8,
    progress: state === "completed" ? 100 : 0,
    attempts: state === "failed" ? 1 : 0,
    error,
    created_at: 1,
    updated_at: 2,
    completed_at: null,
    source_path: "Music/Artist/Album/Song.flac",
    output_path: "",
  };
}

function mockFetch(
  handler: (path: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const parsed = new URL(url, "http://local");
    const path = parsed.pathname + parsed.search;
    return handler(path, options) ?? Response.json({});
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

function renderPage() {
  return render(
    <Theme>
      <MemoryRouter initialEntries={["/music/organize"]}>
        <MusicOrganize />
      </MemoryRouter>
    </Theme>,
  );
}

/** Base mocks for the resources the organize page loads besides conversions. */
function background(
  handler: (path: string, options?: RequestInit) => Response | undefined,
) {
  return mockFetch((path, options) => {
    if (path === "/api/v1/library/music/files?page=1" && !options?.method) {
      return Response.json({ items: [file], page: 1, total: 1, limit: 100 });
    }
    if (path === "/api/v1/library/music/edits" && !options?.method) {
      return Response.json({ items: [], page: 1, total: 0, limit: 20 });
    }
    if (path === "/api/v1/library/music/profiles") {
      return Response.json([
        {
          id: 2,
          name: "Opus balanced",
          key: "opus-balanced",
          codec: "opus",
          bitrate_kbps: 128,
          vbr: true,
          require_lossless: false,
          keep_original: false,
        },
      ]);
    }
    return handler(path, options);
  });
}

describe("music organize conversions", () => {
  it("pages through the conversion queue", async () => {
    const fetch = background((path) => {
      if (path === "/api/v1/library/music/conversions?page=2") {
        return Response.json({
          items: [conversion(8, "queued", "second page job")],
          page: 2,
          total: 2,
          limit: 1,
        });
      }
      if (path.startsWith("/api/v1/library/music/conversions?")) {
        return Response.json({
          items: [
            conversion(7, "failed", "Conversion output failed verification."),
          ],
          page: 1,
          total: 2,
          limit: 1,
        });
      }
      return undefined;
    });

    renderPage();

    const table = await screen.findByRole("table", {
      name: "Music conversion queue",
    });
    expect(
      within(table).getByText("Conversion output failed verification."),
    ).toBeVisible();
    expect(screen.getByText(/Page 1 of 2 · 2 queued jobs/)).toBeVisible();

    await userEvent.click(
      screen.getByRole("button", { name: "Next conversions page" }),
    );
    expect(await screen.findByText("second page job")).toBeVisible();
    expect(
      screen.queryByText("Conversion output failed verification."),
    ).toBeNull();
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url]) =>
            new URL(url as string, "http://local").searchParams.get("page") ===
            "2",
        ),
      ).toBe(true),
    );

    await userEvent.click(
      screen.getByRole("button", { name: "Previous conversions page" }),
    );
    expect(
      await screen.findByText("Conversion output failed verification."),
    ).toBeVisible();
  });

  it("narrows the queue by state and labels the filter as page-scoped", async () => {
    background((path) => {
      if (path.startsWith("/api/v1/library/music/conversions?")) {
        return Response.json({
          items: [
            conversion(7, "failed", "Conversion output failed verification."),
            conversion(8, "queued"),
            conversion(9, "completed"),
          ],
          page: 1,
          total: 3,
          limit: 100,
        });
      }
      return undefined;
    });

    renderPage();

    const table = await screen.findByRole("table", {
      name: "Music conversion queue",
    });
    expect(within(table).getAllByRole("row")).toHaveLength(4); // head + 3 jobs

    await userEvent.selectOptions(
      screen.getByLabelText("Filter conversions by state"),
      "failed",
    );
    expect(
      within(table).getByText("Conversion output failed verification."),
    ).toBeVisible();
    expect(within(table).queryByText("queued")).toBeNull();
    expect(within(table).queryByText("completed")).toBeNull();
    expect(
      screen.getByText(/state filter applies to the 3 jobs on this page only/),
    ).toBeVisible();

    // No processing jobs are on this page, so the filter empties it.
    await userEvent.selectOptions(
      screen.getByLabelText("Filter conversions by state"),
      "processing",
    );
    expect(
      await screen.findByText(/No processing jobs on this page/),
    ).toBeVisible();
    expect(
      screen.queryByRole("table", { name: "Music conversion queue" }),
    ).toBeNull();
  });

  it("retries a failed conversion and refreshes the queue", async () => {
    let retried = false;
    background((path) => {
      if (path === "/api/v1/library/music/conversions/7/retry") {
        retried = true;
        return Response.json({});
      }
      if (path.startsWith("/api/v1/library/music/conversions?")) {
        return Response.json({
          items: [
            retried
              ? conversion(7, "processing")
              : conversion(
                  7,
                  "failed",
                  "Conversion output failed verification.",
                ),
          ],
          page: 1,
          total: 1,
          limit: 100,
        });
      }
      return undefined;
    });

    renderPage();

    const table = await screen.findByRole("table", {
      name: "Music conversion queue",
    });
    const failedRow = within(table)
      .getByText("Conversion output failed verification.")
      .closest("tr");
    expect(failedRow).not.toBeNull();
    await userEvent.click(
      within(failedRow as HTMLElement).getByRole("button", {
        name: "Retry",
      }),
    );
    expect(retried).toBe(true);
    expect(await within(table).findByText("processing")).toBeVisible();
  });

  it("surfaces a retry failure through the shared error message", async () => {
    background((path) => {
      if (path === "/api/v1/library/music/conversions/7/retry") {
        return Response.json(
          {
            error: {
              code: "invalid_music",
              message: "This conversion is still processing.",
            },
          },
          { status: 409 },
        );
      }
      if (path.startsWith("/api/v1/library/music/conversions?")) {
        return Response.json({
          items: [
            conversion(7, "failed", "Conversion output failed verification."),
          ],
          page: 1,
          total: 1,
          limit: 100,
        });
      }
      return undefined;
    });

    renderPage();

    const table = await screen.findByRole("table", {
      name: "Music conversion queue",
    });
    await userEvent.click(within(table).getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByText(/This conversion is still processing\./),
    ).toBeVisible();
  });
});
