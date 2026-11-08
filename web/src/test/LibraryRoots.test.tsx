import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { LibraryRootsEditor } from "../components/LibraryRootsEditor";
import { ExistingLibrary } from "../pages/ExistingLibrary";
import { setCSRFToken } from "../api";

const roots = {
  revision: 5,
  media_root: "/media",
  libraries: [
    {
      key: "movies",
      name: "Movies",
      kind: "movie",
      configured: true,
      roots: [
        { path: "Movies", available: true, default: false },
        { path: "Movies 4K", available: true, default: false },
      ],
    },
    {
      key: "tv",
      name: "TV Shows",
      kind: "series",
      configured: false,
      roots: [{ path: "TV Shows", available: true, default: true }],
    },
    {
      key: "anime",
      name: "Anime",
      kind: "series",
      configured: false,
      roots: [{ path: "Anime", available: false, default: true }],
    },
    {
      key: "music",
      name: "Music",
      kind: "music",
      configured: false,
      roots: [{ path: "music", available: true, default: true }],
    },
  ],
};

function mockFetch(handler: (url: string, options?: RequestInit) => Response) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) =>
    handler(url, options),
  );
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("library roots editor", () => {
  it("adds and removes scan roots and saves them with CSRF", async () => {
    setCSRFToken("fixture-csrf");
    let current = roots;
    const fetch = mockFetch((url, options) => {
      if (url.endsWith("/library/roots") && options?.method === "PUT") {
        const body = JSON.parse(String(options.body));
        current = {
          ...current,
          revision: current.revision + 1,
          libraries: current.libraries.map((library) =>
            library.key === "movies"
              ? {
                  ...library,
                  configured: true,
                  roots: body.libraries
                    .find((item: { key: string }) => item.key === "movies")
                    .roots.map((path: string) => ({
                      path,
                      available: true,
                      default: false,
                    })),
                }
              : library,
          ),
        };
        return Response.json(current);
      }
      return Response.json(current);
    });
    render(
      <Theme>
        <LibraryRootsEditor />
      </Theme>,
    );
    expect(await screen.findByText("Library roots")).toBeVisible();
    expect(await screen.findByText("Anime")).toBeVisible();
    expect(screen.getByText(/This folder is unavailable/)).toBeVisible();
    await userEvent.click(
      screen.getAllByRole("button", { name: "Remove root" })[0],
    );
    await userEvent.click(
      screen.getAllByRole("button", { name: "Add scan root" })[0],
    );
    const input = screen.getByLabelText("Movies scan root 2");
    await userEvent.type(input, "Movies 3D");
    await userEvent.click(
      screen.getByRole("button", { name: "Save library roots" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            url.endsWith("/library/roots") && options?.method === "PUT",
        ),
      ).toBe(true),
    );
    const call = fetch.mock.calls.find(
      ([url, options]) =>
        url.endsWith("/library/roots") && options?.method === "PUT",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      revision: 5,
      libraries: [
        { key: "movies", roots: ["Movies 4K", "Movies 3D"] },
        { key: "tv", roots: ["TV Shows"] },
        { key: "anime", roots: ["Anime"] },
        { key: "music", roots: ["music"] },
      ],
    });
    expect(call?.[1]?.headers).toMatchObject({
      "X-CSRF-Token": "fixture-csrf",
    });
  });
});

describe("existing library root selection", () => {
  it("scans all configured roots or one chosen root", async () => {
    const fetch = mockFetch((url, options) => {
      if (url.endsWith("/library/imports") && options?.method === "POST") {
        return Response.json(
          {
            id: "scan-root",
            kind: "movie",
            roots: ["Movies 4K"],
            state: "queued",
          },
          { status: 202 },
        );
      }
      return Response.json({
        items: [],
        media_root: "/media",
        movie_root: "/media/Movies",
        series_root: "/media/TV Shows",
        collections: [
          {
            key: "movies",
            name: "Movies",
            content: "movie",
            movie_root: "/media/Movies",
            series_root: "",
            roots: roots.libraries[0].roots,
          },
          {
            key: "tv",
            name: "TV Shows",
            content: "series",
            movie_root: "",
            series_root: "/media/TV Shows",
            roots: roots.libraries[1].roots,
          },
          {
            key: "anime",
            name: "Anime",
            content: "series",
            movie_root: "",
            series_root: "/media/Anime",
            roots: roots.libraries[2].roots,
          },
        ],
      });
    });
    render(
      <Theme>
        <MemoryRouter initialEntries={["/import"]}>
          <ExistingLibrary />
        </MemoryRouter>
      </Theme>,
    );
    const selector = await screen.findByRole("combobox", {
      name: "Scan folder",
    });
    expect(selector).toHaveValue("");
    expect(
      screen.getByRole("option", { name: "All configured roots (2)" }),
    ).toBeInTheDocument();
    await userEvent.selectOptions(selector, "Movies 4K");
    await userEvent.click(
      screen.getByRole("button", { name: "Scan and match library" }),
    );
    await waitFor(() => {
      const post = fetch.mock.calls.find(
        ([, options]) => options?.method === "POST",
      );
      expect(JSON.parse(String(post?.[1]?.body))).toEqual({
        kind: "movie",
        automatic_matching: true,
        root: "Movies 4K",
      });
    });
  });
});
