import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import App from "../App";
import type { DiscoveredFile, LibraryScan } from "../pages/ExistingLibrary";

const movie = {
  id: 1,
  kind: "movie",
  tmdb_id: 11,
  title: "Owned fixture",
  year: 2026,
  status: "missing",
  monitored: false,
  overview: "Canonical fixture",
  poster_url: "",
  poster_path: "",
  release_date: "",
  runtime_minutes: 96,
};
const scan: LibraryScan = {
  id: "a".repeat(32),
  kind: "movie",
  media_root: "/media",
  folder: "Movies",
  state: "completed",
  message: "Discovery finished. Review files before adding them.",
  created_at: 1,
  updated_at: 1,
  discovered: 1,
  registered: 0,
  queued: 0,
  needs_match: 1,
  needs_attention: 0,
  examined: 1,
  skipped: 0,
};
const file: DiscoveredFile = {
  id: "b".repeat(32),
  scan_id: scan.id,
  path: "Movies/Owned.Fixture.2026.mp4",
  full_path: "/media/Movies/Owned.Fixture.2026.mp4",
  hint: "Owned fixture",
  size_bytes: 1000000,
  mtime: 1,
  state: "review",
  media_id: null,
  media_file_id: null,
  message: "",
};
function mount() {
  render(
    <Theme>
      <MemoryRouter initialEntries={["/import"]}>
        <App />
      </MemoryRouter>
    </Theme>,
  );
}
function setup(
  extra?: (url: string, options?: RequestInit) => Response | undefined,
  kind: "movie" | "series" = "movie",
) {
  let state = "review";
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const custom = extra?.(url, options);
    if (custom) return custom;
    if (url.endsWith("/auth/session"))
      return Response.json({
        authenticated: true,
        setup_required: false,
        session: {
          username: "fixture",
          csrf_token: "fixture-csrf",
          expires_at: 9999999999,
        },
      });
    if (url.endsWith("/library/imports"))
      return Response.json({
        items: [{ ...scan, kind }],
        media_root: "/media",
        movie_root: "/media/Movies",
        series_root: "/media/TV",
      });
    if (url.includes(`/library/imports/${scan.id}?`))
      return Response.json({
        scan: { ...scan, kind },
        items: [{ ...file, state }],
        page: 1,
        total: 1,
        limit: 50,
      });
    if (url.includes("/library/imports/catalog?"))
      return Response.json({ items: [{ ...movie, kind }] });
    if (url.endsWith("/providers/tmdb"))
      return Response.json({ configured: true });
    if (url.endsWith("/register") && options?.method === "POST") {
      state = "queued";
      return Response.json({ queued: true }, { status: 202 });
    }
    return Response.json(
      { error: { message: "Unexpected fixture request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("existing library discovery and review", () => {
  it("reads status without starting a scan and submits only the configured media type", async () => {
    let started = false;
    const fetch = setup((url, options) => {
      if (url.endsWith("/library/imports") && options?.method === "POST") {
        started = true;
        return Response.json(scan, { status: 202 });
      }
      if (url.endsWith("/library/imports"))
        return Response.json({
          items: started ? [scan] : [],
          media_root: "/media",
          movie_root: "/media/Movies",
          series_root: "/media/TV",
        });
    });
    mount();
    await screen.findByText(
      "No discovery scans yet. Choose movies or TV shows to begin.",
    );
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "POST"),
    ).toBe(false);
    await userEvent.click(
      screen.getByRole("button", { name: "Scan and match library" }),
    );
    await screen.findByRole("button", { name: "Review match" });
    const call = fetch.mock.calls.find(
      ([, options]) => options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      kind: "movie",
      automatic_matching: true,
    });
    expect(call?.[1]?.headers).toMatchObject({
      "X-CSRF-Token": "fixture-csrf",
    });
  });
  it("requires explicit movie review and never submits a client-supplied file path", async () => {
    const fetch = setup();
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "Review match" }),
    );
    const dialog = screen.getByRole("dialog");
    const submit = within(dialog).getByRole("button", {
      name: "Verify & add in place",
    });
    expect(submit).toBeDisabled();
    await userEvent.click(
      await within(dialog).findByRole("button", { name: "Choose title" }),
    );
    expect(submit).toBeDisabled();
    await userEvent.click(
      within(dialog).getByRole("checkbox", {
        name: "I checked that this file belongs to this movie.",
      }),
    );
    await userEvent.click(submit);
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    const call = fetch.mock.calls.find(([url]) => url.endsWith("/register"));
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      media_id: 1,
      episode_ids: [],
      reviewed: true,
    });
    expect(call?.[1]?.headers).toMatchObject({
      "X-CSRF-Token": "fixture-csrf",
    });
  });
  it("requires individual TV episode choices and sends the reviewed group", async () => {
    const fetch = setup((url) => {
      if (url.endsWith("/library/series/1"))
        return Response.json({
          ...movie,
          kind: "series",
          revision: 1,
          seasons: [
            { id: 2, number: 1, title: "Season 1", loaded_at: "saved" },
          ],
        });
      if (url.endsWith("/seasons/1"))
        return Response.json({
          season: { id: 2, number: 1, loaded_at: "saved" },
          revision: 1,
          episodes: [
            { id: 9, number: 1, title: "Pilot", available: false },
            { id: 10, number: 2, title: "Return", available: false },
          ],
        });
    }, "series");
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "Review match" }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Choose title" }),
    );
    const pilot = await screen.findByRole("checkbox", { name: "E01 · Pilot" });
    expect(pilot).not.toBeChecked();
    await userEvent.click(pilot);
    await userEvent.click(
      screen.getByRole("checkbox", { name: "E02 · Return" }),
    );
    await userEvent.click(
      screen.getByRole("checkbox", {
        name: "I checked that this file belongs to this show and selected episodes.",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Verify & add in place" }),
    );
    await waitFor(() =>
      expect(fetch.mock.calls.some(([url]) => url.endsWith("/register"))).toBe(
        true,
      ),
    );
    const call = fetch.mock.calls.find(([url]) => url.endsWith("/register"));
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      media_id: 1,
      episode_ids: [9, 10],
      reviewed: true,
    });
  });
  it("adds a canonical TMDb title with monitoring paused and still waits for registration review", async () => {
    const fetch = setup((url, options) => {
      if (url.includes("/library/imports/catalog?"))
        return Response.json({ items: [] });
      if (url.includes("/metadata/movies?"))
        return Response.json({
          items: [{ ...movie, id: undefined, in_library: false }],
          page: 1,
          total_pages: 1,
          total_results: 1,
        });
      if (url.endsWith("/library/movies") && options?.method === "POST")
        return Response.json({ item: movie, created: true }, { status: 201 });
    });
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "Review match" }),
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Search TMDb" })).toBeEnabled(),
    );
    await userEvent.click(screen.getByRole("button", { name: "Search TMDb" }));
    await userEvent.click(
      await screen.findByRole("button", { name: "Add & choose" }),
    );
    await screen.findByRole("heading", {
      name: "Selected: Owned fixture (2026)",
    });
    const call = fetch.mock.calls.find(
      ([url, options]) =>
        url.endsWith("/library/movies") && options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      tmdb_id: 11,
      monitored: false,
    });
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/register"))).toBe(
      false,
    );
  });
  it("keeps the reviewed match visible when the server refuses a changed or conflicting file", async () => {
    setup((url, options) =>
      url.endsWith("/register") && options?.method === "POST"
        ? Response.json(
            {
              error: {
                message: "The file changed since discovery. Start a new scan.",
              },
            },
            { status: 409 },
          )
        : undefined,
    );
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "Review match" }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Choose title" }),
    );
    await userEvent.click(
      screen.getByRole("checkbox", {
        name: "I checked that this file belongs to this movie.",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Verify & add in place" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Start a new scan.",
    );
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Verify & add in place" }),
    ).toBeEnabled();
  });
});

describe("automatic existing-library matching", () => {
  it("allows opting out before starting discovery", async () => {
    const fetch = setup((url, options) => {
      if (url.endsWith("/library/imports") && options?.method === "POST")
        return Response.json(scan, { status: 202 });
    });
    mount();
    const checkbox = await screen.findByRole("checkbox", {
      name: "Automatically match and register clear results",
    });
    expect(checkbox).toBeChecked();
    await userEvent.click(checkbox);
    await userEvent.click(
      screen.getByRole("button", { name: "Scan configured folder" }),
    );
    await waitFor(() =>
      expect(fetch.mock.calls.some(([, o]) => o?.method === "POST")).toBe(true),
    );
    const request = fetch.mock.calls.find(
      ([url, o]) => url.endsWith("/library/imports") && o?.method === "POST",
    );
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      kind: "movie",
      automatic_matching: false,
    });
  });
  it("starts matching an existing scan, displays progress and stops it", async () => {
    let state = "";
    const fetch = setup((url, options) => {
      if (url.endsWith("/matches/automatic")) {
        state =
          JSON.parse(String(options?.body)).action === "start"
            ? "running"
            : "cancelled";
        return Response.json({}, { status: 202 });
      }
      if (url.includes(`/library/imports/${scan.id}?`))
        return Response.json({
          scan: {
            ...scan,
            automatic_matching: state
              ? {
                  state,
                  phase: "resolve",
                  message: "Matching fixture titles",
                  resolved: 1,
                  queued: 0,
                  review: 0,
                }
              : undefined,
          },
          items: [file],
          page: 1,
          total: 1,
          limit: 50,
        });
    });
    mount();
    await userEvent.click(
      await screen.findByRole("button", {
        name: "Match clear files automatically",
      }),
    );
    expect(
      await screen.findByText("Automatic matching: running"),
    ).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Stop automatic matching" }),
    );
    expect(
      await screen.findByText("Automatic matching: cancelled"),
    ).toBeInTheDocument();
    const calls = fetch.mock.calls.filter(([url]) =>
      url.endsWith("/matches/automatic"),
    );
    expect(calls.map(([, o]) => JSON.parse(String(o?.body)))).toEqual([
      { action: "start" },
      { action: "stop" },
    ]);
    expect(calls[0][1]?.headers).toMatchObject({
      "X-CSRF-Token": "fixture-csrf",
    });
  });
});
