import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MovieCollection } from "../pages/MovieCollection";

describe("movie collections", () => {
  it("loads related titles and links those already in the library", async () => {
    const fetch = vi.fn(async () =>
      Response.json({
        name: "Fixture collection",
        items: [
          {
            id: 12,
            tmdb_id: 24,
            title: "First film",
            year: 2020,
            status: "available",
          },
          { id: 0, tmdb_id: 25, title: "Second film", year: 2022, status: "" },
        ],
      }),
    );
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <MemoryRouter>
          <MovieCollection id={12} />
        </MemoryRouter>
      </Theme>,
    );
    expect(fetch).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", { name: "Load related movies" }),
    );
    expect(
      await screen.findByRole("heading", { name: "Fixture collection" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "First film" })).toHaveAttribute(
      "href",
      "/movies/12",
    );
    expect(screen.getByRole("button", { name: "Add movie" })).toBeVisible();
  });
});
