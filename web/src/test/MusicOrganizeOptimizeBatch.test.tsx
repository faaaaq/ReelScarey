import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import {
  chunkFileIds,
  mergePreviews,
  MusicOrganize,
} from "../pages/MusicOrganize";

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

const profiles = [
  {
    id: 1,
    name: "Original",
    key: "original",
    codec: "original",
    bitrate_kbps: 0,
    vbr: false,
    require_lossless: false,
    keep_original: false,
  },
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
];

describe("chunkFileIds", () => {
  it("splits ids into at most 500-sized batches in order", () => {
    const ids = Array.from({ length: 510 }, (_, index) => index + 1);
    expect(chunkFileIds(ids)).toEqual([ids.slice(0, 500), ids.slice(500)]);
  });

  it("keeps an exact multiple in one batch and handles empty input", () => {
    const ids = Array.from({ length: 500 }, (_, index) => index + 1);
    expect(chunkFileIds(ids)).toHaveLength(1);
    expect(chunkFileIds([])).toEqual([]);
  });

  it("honors a custom size", () => {
    expect(chunkFileIds([1, 2, 3, 4, 5], 2)).toEqual([[1, 2], [3, 4], [5]]);
  });
});

describe("mergePreviews", () => {
  it("concatenates items and sums the totals of chunked dry runs", () => {
    const merged = mergePreviews([
      {
        profile: "opus-balanced",
        items: [
          {
            file_id: 1,
            eligible: true,
            reason: "",
            current_bytes: 100,
            estimated_bytes: 10,
            saved_bytes: 90,
          },
        ],
        eligible_count: 1,
        current_bytes: 100,
        estimated_bytes: 10,
        saved_bytes: 90,
      },
      {
        profile: "opus-balanced",
        items: [
          {
            file_id: 2,
            eligible: false,
            reason: "already converted",
            current_bytes: 50,
            estimated_bytes: 50,
            saved_bytes: 0,
          },
        ],
        eligible_count: 0,
        current_bytes: 50,
        estimated_bytes: 50,
        saved_bytes: 0,
      },
    ]);
    expect(merged.items.map((item) => item.file_id)).toEqual([1, 2]);
    expect(merged.eligible_count).toBe(1);
    expect(merged.current_bytes).toBe(150);
    expect(merged.estimated_bytes).toBe(60);
    expect(merged.saved_bytes).toBe(90);
    expect(merged.profile).toBe("opus-balanced");
  });
});

function mockFetch(
  handler: (
    path: string,
    options?: RequestInit,
  ) => Response | Promise<Response> | undefined,
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

function eligibleItems(count: number) {
  return Array.from({ length: count }, (_, index) => ({
    file_id: index + 1,
    eligible: true,
    reason: "",
    current_bytes: 1000,
    estimated_bytes: 100,
    saved_bytes: 900,
  }));
}

/** Base mocks; the optimize preview/apply handlers are provided per test. */
function organizeFetch(
  handlers: Record<
    string,
    (options?: RequestInit) => Response | Promise<Response> | undefined
  >,
) {
  return mockFetch((path, options) => {
    if (path === "/api/v1/library/music/files?page=1" && !options?.method) {
      return Response.json({ items: [file], page: 1, total: 1, limit: 100 });
    }
    if (path === "/api/v1/library/music/edits" && !options?.method) {
      return Response.json({ items: [], page: 1, total: 0, limit: 20 });
    }
    if (path === "/api/v1/library/music/conversions?page=1") {
      return Response.json({ items: [], page: 1, total: 0, limit: 20 });
    }
    if (path === "/api/v1/library/music/profiles") {
      return Response.json(profiles);
    }
    const hit = handlers[path];
    return hit ? hit(options) : undefined;
  });
}

async function runPreview() {
  await screen.findByRole("table", { name: "Registered music files" });
  await userEvent.selectOptions(
    screen.getByLabelText("Optimize target profile"),
    "opus-balanced",
  );
  await userEvent.click(
    screen.getByRole("button", { name: "Preview savings" }),
  );
  await screen.findByRole("table", { name: "Optimization preview" });
}

describe("music optimize batching", () => {
  it("applies eligible files in bounded batches with visible progress", async () => {
    const applies: number[][] = [];
    let resolveFirstApply!: (response: Response) => void;
    const firstApply = new Promise<Response>((resolve) => {
      resolveFirstApply = resolve;
    });
    organizeFetch({
      "/api/v1/library/music/optimize/preview": () =>
        Response.json({
          profile: "opus-balanced",
          items: eligibleItems(501),
          eligible_count: 501,
          current_bytes: 501000,
          estimated_bytes: 50100,
          saved_bytes: 450900,
        }),
      "/api/v1/library/music/optimize/apply": (options) => {
        const body = JSON.parse(String(options?.body));
        applies.push(body.file_ids);
        if (applies.length === 1) return firstApply;
        return Response.json({ queued: body.file_ids.length }, { status: 202 });
      },
    });

    renderPage();
    await runPreview();
    await userEvent.click(
      screen.getByRole("button", { name: "Convert 501 files" }),
    );

    // The progress line is visible while the first batch is in flight.
    expect(await screen.findByText("Optimizing batch 1 of 2…")).toBeVisible();
    resolveFirstApply(Response.json({ queued: 500 }, { status: 202 }));

    expect(
      await screen.findByText(
        /Queued 501 files for conversion across 2 batches/,
      ),
    ).toBeVisible();
    expect(applies).toHaveLength(2);
    expect(applies[0]).toHaveLength(500);
    expect(applies[1]).toHaveLength(1);
    expect(applies[0][0]).toBe(1);
    expect(applies[1][0]).toBe(501);
  });

  it("stops on a failed batch, reports honestly, and queues the rest on retry", async () => {
    const applies: number[][] = [];
    organizeFetch({
      "/api/v1/library/music/optimize/preview": () =>
        Response.json({
          profile: "opus-balanced",
          items: eligibleItems(600),
          eligible_count: 600,
          current_bytes: 600000,
          estimated_bytes: 60000,
          saved_bytes: 540000,
        }),
      "/api/v1/library/music/optimize/apply": (options) => {
        const body = JSON.parse(String(options?.body));
        applies.push(body.file_ids);
        if (applies.length === 2) {
          return Response.json(
            {
              error: {
                code: "invalid_music",
                message: "batch two failed",
              },
            },
            { status: 409 },
          );
        }
        return Response.json({ queued: body.file_ids.length }, { status: 202 });
      },
    });

    renderPage();
    await runPreview();
    await userEvent.click(
      screen.getByRole("button", { name: "Convert 600 files" }),
    );

    expect(
      await screen.findByText(
        /Queued 500 of 600 eligible files before a batch failed: batch two failed/,
      ),
    ).toBeVisible();
    expect(applies).toHaveLength(2);
    expect(applies[0]).toHaveLength(500);
    expect(applies[1]).toHaveLength(100);

    // The retry click queues only the files that are not queued yet.
    await userEvent.click(
      screen.getByRole("button", { name: "Convert 600 files" }),
    );
    expect(
      await screen.findByText(
        /Queued 600 files for conversion across 2 batches/,
      ),
    ).toBeVisible();
    expect(applies).toHaveLength(3);
    expect(applies[2]).toHaveLength(100);
    expect(applies[2][0]).toBe(501);
  });

  // Rendering 600 file rows and 600 preview rows is heavy for jsdom, so this
  // test carries its own generous timeouts for loaded CI runs.
  it("splits a large selection into bounded preview requests and merges them", async () => {
    const previews: number[][] = [];
    const applies: number[][] = [];
    const items = eligibleItems(600);
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/files?page=1" && !options?.method) {
        // One oversized mock page so a single "Select all" covers the library.
        return Response.json({
          items: Array.from({ length: 600 }, (_, index) => ({
            ...file,
            id: index + 1,
          })),
          page: 1,
          total: 600,
          limit: 600,
        });
      }
      if (path === "/api/v1/library/music/edits" && !options?.method) {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (path === "/api/v1/library/music/conversions?page=1") {
        return Response.json({ items: [], page: 1, total: 0, limit: 20 });
      }
      if (path === "/api/v1/library/music/profiles") {
        return Response.json(profiles);
      }
      if (
        path === "/api/v1/library/music/optimize/preview" &&
        options?.method === "POST"
      ) {
        const body = JSON.parse(String(options.body));
        previews.push(body.file_ids);
        // Like the backend, answer only the requested files per chunk.
        const requested = new Set(body.file_ids as number[]);
        return Response.json({
          profile: "opus-balanced",
          items: items.filter((item) => requested.has(item.file_id)),
        });
      }
      if (
        path === "/api/v1/library/music/optimize/apply" &&
        options?.method === "POST"
      ) {
        const body = JSON.parse(String(options.body));
        applies.push(body.file_ids);
        return Response.json({ queued: body.file_ids.length }, { status: 202 });
      }
      return undefined;
    });

    renderPage();

    await waitFor(
      () =>
        expect(
          screen.getByRole("table", { name: "Registered music files" }),
        ).toBeVisible(),
      { timeout: 15000 },
    );
    await userEvent.selectOptions(
      screen.getByLabelText("Optimize target profile"),
      "opus-balanced",
    );
    await userEvent.click(screen.getByRole("button", { name: "Select all" }));
    await userEvent.click(
      screen.getByRole("button", { name: "Preview savings" }),
    );

    await waitFor(
      () =>
        expect(
          screen.getByRole("table", { name: "Optimization preview" }),
        ).toBeVisible(),
      { timeout: 15000 },
    );
    expect(previews).toHaveLength(2);
    expect(previews[0]).toHaveLength(500);
    expect(previews[1]).toHaveLength(100);

    await userEvent.click(
      screen.getByRole("button", { name: "Convert 600 files" }),
    );
    await waitFor(
      () =>
        expect(
          screen.getByText(/Queued 600 files for conversion across 2 batches/),
        ).toBeVisible(),
      { timeout: 15000 },
    );
    expect(applies.map((ids) => ids.length)).toEqual([500, 100]);
  }, 30000);
});
