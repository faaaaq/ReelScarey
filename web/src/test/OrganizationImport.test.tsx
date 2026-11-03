import { render, screen, waitFor, within } from "@testing-library/react";
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

describe("Collection-aware existing-library discovery", () => {
  it("preselects the Anime series folder from the Anime import link", async () => {
    setup(true, "/import?collection=anime");
    expect(
      await screen.findByRole("combobox", { name: "Scan collection" }),
    ).toHaveValue("anime");
    expect(
      screen.getByRole("combobox", { name: "Scan media type" }),
    ).toHaveValue("series");
    expect(screen.getByText("/media/Anime")).toBeVisible();
  });

  it("shows Anime roots from the existing response and scans the selected canonical format", async () => {
    const fetch = setup();
    await screen.findByRole("combobox", { name: "Scan collection" });
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Scan collection" }),
      "anime",
    );
    expect(screen.getByRole("option", { name: "Movies" })).toBeDisabled();
    expect(screen.getByText("/media/Anime")).toBeVisible();
    expect(screen.queryByText("/media/Legacy Movies")).not.toBeInTheDocument();
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Scan media type" }),
      "series",
    );
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "POST"),
    ).toBe(false);
    await userEvent.click(
      screen.getByRole("button", { name: "Scan and match library" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(([, options]) => options?.method === "POST"),
      ).toBe(true),
    );
    const call = fetch.mock.calls.find(
      ([, options]) => options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      kind: "series",
      library_key: "anime",
      automatic_matching: true,
    });
    expect(
      fetch.mock.calls.every(([url]) => url === "/api/v1/library/imports"),
    ).toBe(true);
    expect(
      screen.getByRole("link", { name: "Organize library" }),
    ).toHaveAttribute("href", "/organization");
  });

  it("limits a custom collection to its supported media type and displays only its root", async () => {
    const fetch = setup();
    await screen.findByRole("combobox", { name: "Scan collection" });
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Scan media type" }),
      "series",
    );
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Scan collection" }),
      "family",
    );
    await waitFor(() =>
      expect(
        screen.getByRole("combobox", { name: "Scan media type" }),
      ).toHaveValue("movie"),
    );
    expect(screen.getByRole("option", { name: "TV shows" })).toBeDisabled();
    expect(screen.getByText("/media/Family")).toBeVisible();
    expect(screen.queryByText("/media/Legacy TV")).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Scan and match library" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(([, options]) => options?.method === "POST"),
      ).toBe(true),
    );
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(
            ([, options]) => options?.method === "POST",
          )?.[1]?.body,
        ),
      ),
    ).toEqual({
      kind: "movie",
      library_key: "family",
      automatic_matching: true,
    });
  });

  it("omits library_key after returning to the default folders", async () => {
    const fetch = setup();
    await screen.findByRole("combobox", { name: "Scan collection" });
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Scan collection" }),
      "tv",
    );
    await waitFor(() =>
      expect(
        screen.getByRole("combobox", { name: "Scan media type" }),
      ).toHaveValue("series"),
    );
    expect(
      within(
        screen.getByRole("combobox", { name: "Scan media type" }),
      ).getByRole("option", { name: "Movies", selected: false }),
    ).toBeDisabled();
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Scan collection" }),
      "",
    );
    expect(screen.getByText("/media/Legacy Movies")).toBeVisible();
    expect(screen.getByText("/media/Legacy TV")).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Scan and match library" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(([, options]) => options?.method === "POST"),
      ).toBe(true),
    );
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(
            ([, options]) => options?.method === "POST",
          )?.[1]?.body,
        ),
      ),
    ).toEqual({ kind: "series", automatic_matching: true });
  });

  it("keeps legacy responses usable without collections or another configuration fetch", async () => {
    const fetch = setup(false);
    await screen.findByRole("button", { name: "Scan and match library" });
    expect(
      screen.queryByRole("combobox", { name: "Scan collection" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("/media/Legacy Movies")).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Scan and match library" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(([, options]) => options?.method === "POST"),
      ).toBe(true),
    );
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(
            ([, options]) => options?.method === "POST",
          )?.[1]?.body,
        ),
      ),
    ).toEqual({ kind: "movie", automatic_matching: true });
    expect(
      fetch.mock.calls.every(([url]) => url === "/api/v1/library/imports"),
    ).toBe(true);
  });
});
