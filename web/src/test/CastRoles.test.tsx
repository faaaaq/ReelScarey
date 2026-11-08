import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { CreditsSection } from "../pages/details/DetailParts";
import type { ItemDetailsState } from "../pages/details/useItemDetails";

function details(): ItemDetailsState {
  return {
    data: {
      item: {
        id: 1,
        kind: "movie",
        tmdb_id: 11,
        title: "Current film",
        year: 2026,
        monitored: true,
        status: "missing",
        overview: "",
        poster_url: "",
        poster_path: "",
        release_date: "2026-01-01",
        runtime_minutes: null,
      },
      information: null,
      metadata_updated_at: 0,
      files: [],
      history: [],
    },
    info: {
      tagline: "",
      genres: [],
      rating: 0,
      votes: 0,
      imdb_url: "",
      tmdb_url: "",
      trailer_url: "",
      cast: [{ id: 9, name: "Fixture Actor", role: "Lead", image_url: "" }],
      crew: [],
    },
    busy: false,
    updated: 0,
    refresh: vi.fn(async () => undefined),
  } as unknown as ItemDetailsState;
}

describe("cast roles", () => {
  it("opens a person's recent roles and quick-adds an Anime movie there", async () => {
    const calls: { url: string; method: string; body: unknown }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit = {}) => {
        const method = init.method ?? "GET";
        let body: unknown = init.body;
        if (typeof body === "string") body = JSON.parse(body);
        calls.push({ url, method, body });
        if (method === "GET" && url.includes("/metadata/people/9/credits"))
          return Response.json({
            items: [
              {
                kind: "movie",
                tmdb_id: 12,
                title: "Anime Role",
                year: 2026,
                date: "2026-01-02",
                character: "Voice",
                poster_url: "",
                category: "anime",
                in_library: false,
              },
            ],
          });
        if (method === "POST" && url.endsWith("/library/movies"))
          return Response.json(
            { created: true, item: { id: 2 } },
            { status: 201 },
          );
        return Response.json(
          { error: { message: "unexpected" } },
          { status: 404 },
        );
      }),
    );
    render(
      <Theme>
        <MemoryRouter>
          <CreditsSection details={details()} />
        </MemoryRouter>
      </Theme>,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Other roles for Fixture Actor" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText("Anime Role")).toBeVisible();
    expect(
      calls.some(
        (call) =>
          call.method === "GET" &&
          call.url.includes("exclude_kind=movie") &&
          call.url.includes("exclude_tmdb_id=11"),
      ),
    ).toBe(true);
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Add monitored" }),
    );
    await within(dialog).findByText("In library");
    expect(
      calls.find(
        (call) =>
          call.method === "POST" && call.url.endsWith("/library/movies"),
      )?.body,
    ).toEqual({ tmdb_id: 12, monitored: true, category: "anime" });
  });
});
