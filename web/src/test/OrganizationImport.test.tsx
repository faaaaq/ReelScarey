import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { ExistingLibrary } from "../pages/ExistingLibrary";

const collections = [
  {
    key: "movies",
    name: "Movies",
    content: "movie",
    movie_root: "/media/Movies",
    series_root: "",
  },
  {
    key: "tv",
    name: "TV Shows",
    content: "series",
    movie_root: "",
    series_root: "/media/TV Shows",
  },
  {
    key: "anime",
    name: "Anime",
    content: "series",
    movie_root: "",
    series_root: "/media/Anime",
  },
  {
    key: "family",
    name: "Family",
    content: "movie",
    movie_root: "/media/Family",
    series_root: "",
  },
];
function setup(includeCollections = true, path = "/import") {
  const fetch = vi.fn(async (_url: string, options?: RequestInit) => {
    if (options?.method === "POST")
      return Response.json(
        {
          id: "scan-collection",
          kind: JSON.parse(String(options.body)).kind,
          state: "queued",
        },
        { status: 202 },
      );
    return Response.json({
      items: [],
      media_root: "/media",
      movie_root: "/media/Legacy Movies",
      series_root: "/media/Legacy TV",
      ...(includeCollections ? { collections } : {}),
    });
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        <ExistingLibrary />
      </MemoryRouter>
    </Theme>,
  );
  return fetch;
}

describe("Existing-library discovery targets", () => {
  it("preselects Anime from the Anime import link", async () => {
    setup(true, "/import?collection=anime");
    expect(
      await screen.findByRole("combobox", { name: "Scan media type" }),
    ).toHaveValue("anime");
    expect(screen.getByText("/media/Anime")).toBeVisible();
  });

  it.each([
    ["movie", { kind: "movie", automatic_matching: true }],
    ["series", { kind: "series", automatic_matching: true }],
    [
      "anime",
      { kind: "series", library_key: "anime", automatic_matching: true },
    ],
  ])("scans %s from the single selector", async (target, payload) => {
    const fetch = setup();
    const selector = await screen.findByRole("combobox", {
      name: "Scan media type",
    });
    expect(screen.getAllByRole("combobox")).toEqual([selector]);
    expect(
      screen.getAllByRole("option").map((option) => option.textContent),
    ).toEqual(["Movies", "TV Shows", "Anime"]);
    expect(screen.getByText("/media/Anime")).toBeVisible();
    await userEvent.selectOptions(selector, target);
    await userEvent.click(
      screen.getByRole("button", { name: "Scan and match library" }),
    );
    await waitFor(() => {
      const post = fetch.mock.calls.find(
        ([, options]) => options?.method === "POST",
      );
      expect(JSON.parse(String(post?.[1]?.body))).toEqual(payload);
    });
  });

  it("keeps Movies and TV usable when the server does not advertise an Anime folder", async () => {
    setup(false);
    expect(await screen.findByRole("option", { name: "Anime" })).toBeDisabled();
    expect(screen.getByRole("option", { name: "TV Shows" })).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Scan and match library" }),
    ).toBeEnabled();
  });
});
