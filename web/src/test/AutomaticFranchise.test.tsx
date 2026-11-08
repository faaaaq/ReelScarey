import { render, screen } from "@testing-library/react";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { AutomaticFranchise } from "../pages/AutomaticFranchise";
describe("automatic franchise discovery", () => {
  it("automatically displays a related movie and TV show with library status", async () => {
    const fetch = vi.fn(async () =>
      Response.json({
        source: "Wikidata",
        items: [
          {
            kind: "series",
            tmdb_id: 24,
            title: "Fixture show",
            wikidata_id: "Q2",
            franchise_id: "Q10",
            franchise: "Fixture saga",
            library_id: 9,
            status: "available",
          },
          {
            kind: "movie",
            tmdb_id: 25,
            title: "Fixture film",
            wikidata_id: "Q3",
            franchise_id: "Q10",
            franchise: "Fixture saga",
            library_id: 0,
            status: "",
          },
        ],
        truncated: false,
      }),
    );
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <MemoryRouter>
          <AutomaticFranchise id={1} />
        </MemoryRouter>
      </Theme>,
    );
    expect(
      await screen.findByRole("link", { name: "Fixture show" }),
    ).toHaveAttribute("href", "/series/9");
    expect(screen.getByText("Fixture film")).toBeVisible();
    expect(screen.getByRole("button", { name: "Add monitored" })).toBeVisible();
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("shows a retryable provider failure rather than an empty result", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json(
          { error: { message: "Provider unavailable" } },
          { status: 502 },
        ),
      ),
    );
    render(
      <Theme>
        <MemoryRouter>
          <AutomaticFranchise id={1} />
        </MemoryRouter>
      </Theme>,
    );
    expect(await screen.findByText("Provider unavailable")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Retry discovery" }),
    ).toBeVisible();
  });
});
