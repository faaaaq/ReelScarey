import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { ImportExistingFile } from "../pages/details/ImportExistingFile";

const roots = {
  roots: ["Movies"],
  dir: "",
  items: [{ name: "Movies", path: "Movies", dir: true }],
};
const folder = {
  roots: ["Movies"],
  dir: "Movies/Test Movie (2026)",
  items: [
    {
      name: "Test Movie (2026)",
      path: "Movies/Test Movie (2026)",
      dir: true,
    },
  ],
};
const files = {
  roots: ["Movies"],
  dir: "Movies/Test Movie (2026)",
  items: [
    {
      name: "Test.Movie.2026.mp4",
      path: "Movies/Test Movie (2026)/Test.Movie.2026.mp4",
      dir: false,
      size_bytes: 1500000,
    },
  ],
};

function mount(ui: React.ReactElement) {
  return render(<Theme>{ui}</Theme>);
}

describe("direct existing-file import dialog", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  beforeEach(() => {
    vi.unstubAllGlobals();
  });

  it("browses configured roots, requires the reviewed confirmation and posts the selection", async () => {
    const user = userEvent.setup();
    const posts: { url: string; body: unknown }[] = [];
    const fetch = vi.fn(async (url: string, options?: RequestInit) => {
      if (url.includes("/library/imports/direct")) {
        if (options?.method === "POST") {
          posts.push({ url, body: JSON.parse(String(options.body)) });
          return Response.json({ id: "c".repeat(32) }, { status: 202 });
        }
        const target = new URL(url, "http://x").searchParams.get("dir") ?? "";
        if (target === "") return Response.json(roots);
        if (target === "Movies") return Response.json(folder);
        return Response.json(files);
      }
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
      return Response.json(
        { error: { message: "Unexpected fixture request" } },
        { status: 404 },
      );
    });
    vi.stubGlobal("fetch", fetch);
    let queued = 0;
    mount(
      <ImportExistingFile
        open
        onOpenChange={() => {}}
        kind="movie"
        mediaID={7}
        title="Test Movie"
        onQueued={() => {
          queued++;
        }}
      />,
    );
    // The configured root is offered first.
    await screen.findByRole("listbox", { name: "Library files" });
    await user.click(await screen.findByRole("button", { name: "Movies/" }));
    await user.click(
      await screen.findByRole("button", { name: "Test Movie (2026)/" }),
    );
    const radio = await screen.findByRole("radio");
    await user.click(radio);
    // Submit stays disabled without the reviewed confirmation.
    const submit = screen.getByRole("button", {
      name: /Verify & add in place/,
    });
    expect(submit).toBeDisabled();
    await user.click(screen.getByLabelText(/I confirm this file belongs to/i));
    await waitFor(() => expect(submit).toBeEnabled());
    await user.click(submit);
    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0].body).toMatchObject({
      kind: "movie",
      media_id: 7,
      relative: "Movies/Test Movie (2026)/Test.Movie.2026.mp4",
      root: "Movies",
      reviewed: true,
    });
    expect(queued).toBe(1);
  });

  it("narrows series imports to one season and skips registered episodes", async () => {
    const user = userEvent.setup();
    const posts: { body: unknown }[] = [];
    const fetch = vi.fn(async (url: string, options?: RequestInit) => {
      if (url.includes("/library/imports/direct")) {
        if (options?.method === "POST") {
          posts.push({ body: JSON.parse(String(options.body)) });
          return Response.json({ id: "d".repeat(32) }, { status: 202 });
        }
        const target = new URL(url, "http://x").searchParams.get("dir") ?? "";
        if (target === "") return Response.json(roots);
        return Response.json({
          roots: ["TV Shows"],
          dir: "TV Shows/Test Show (2026)/Season 1",
          items: [
            {
              name: "Test Show - S01E01.mp4",
              path: "TV Shows/Test Show (2026)/Season 1/Test Show - S01E01.mp4",
              dir: false,
              size_bytes: 900,
            },
          ],
        });
      }
      if (url.includes("/library/series/9/seasons/1"))
        return Response.json({
          season: { number: 1 },
          episodes: [
            { id: 101, number: 1, title: "Pilot", file_id: 55 },
            { id: 102, number: 2, title: "Second", file_id: null },
          ],
        });
      return Response.json(
        { error: { message: "Unexpected fixture request" } },
        { status: 404 },
      );
    });
    vi.stubGlobal("fetch", fetch);
    mount(
      <ImportExistingFile
        open
        onOpenChange={() => {}}
        kind="series"
        mediaID={9}
        title="Test Show"
        seasons={[{ number: 1, title: "Season 1", loaded_at: "2026-01-01" }]}
        onQueued={() => {}}
      />,
    );
    // Navigate into the folder that holds the episode file.
    await user.click(await screen.findByRole("button", { name: "Movies/" }));
    // The registered episode is not selectable; only the missing one defaults on.
    const episodes = await screen.findByRole("listbox", { name: "Episodes" });
    const registered = await within(episodes).findByLabelText(/E01/);
    expect(registered).toBeDisabled();
    const missing = (await within(episodes).findByLabelText(
      /E02/,
    )) as HTMLInputElement;
    expect((missing as HTMLInputElement).checked).toBe(true);
    await user.click(await screen.findByRole("radio"));
    await user.click(screen.getByLabelText(/I confirm this file belongs to/i));
    const submit = screen.getByRole("button", {
      name: /Verify & add in place/,
    });
    await waitFor(() => expect(submit).toBeEnabled());
    await user.click(submit);
    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0].body).toMatchObject({
      kind: "series",
      media_id: 9,
      episode_ids: [102],
      reviewed: true,
    });
  });
});
