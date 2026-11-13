import { expect, test } from "@playwright/test";
import { ServerFixture, login, type SeedMovie } from "./helpers/server";

/**
 * Scenario C: media browsing against synthetic catalog rows seeded directly
 * into the disposable SQLite database (server stopped while seeding, per the
 * documented seeding path used by scripts/*-seed.py). No real media, no
 * external providers: every item has no artwork and must show the honest
 * fallback.
 */
test.describe.serial("Scenario C: library browsing", () => {
  let server: ServerFixture;

  const NAMED: SeedMovie[] = [
    { kind: "movie", tmdb_id: 900001, title: "Alpha Fixture", year: 2020 },
    { kind: "movie", tmdb_id: 900002, title: "Bravo Fixture", year: 2021 },
    { kind: "movie", tmdb_id: 900003, title: "Charlie Fixture", year: 2022 },
  ];
  // Push the catalog past the page=all cap (5000) so the paginated view with
  // real page controls is exercised.
  const BULK_COUNT = 5007;
  const TOTAL = NAMED.length + BULK_COUNT;

  test.beforeAll(async () => {
    server = await ServerFixture.create("library");
    await server.start();
    await server.setupAdmin("e2eadmin", "e2e-only-password-123");
    const bulk: SeedMovie[] = [];
    for (let i = 1; i <= BULK_COUNT; i++) {
      bulk.push({
        kind: "movie",
        tmdb_id: 910000 + i,
        title: `E2E Movie ${String(i).padStart(4, "0")}`,
        year: 1950 + (i % 70),
      });
    }
    await server.seedMovies([...NAMED, ...bulk]);
  }, 180_000);

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("the movies page renders the seeded catalog", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/movies"));
    await expect(
      page.getByRole("heading", { name: "Movies", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Alpha Fixture" }).first(),
    ).toBeVisible();
    await expect(page.getByText(/5010 matching movies/)).toBeVisible();
  });

  test("the title filter narrows the catalog", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/movies"));
    await page.getByLabel("Search saved titles").fill("Fixture");
    await page.getByRole("button", { name: "Apply title filter" }).click();
    await expect(
      page.getByRole("link", { name: "Bravo Fixture" }).first(),
    ).toBeVisible();
    await expect(page.getByText(/3 matching movies of 5010/)).toBeVisible();
    await expect(
      page.getByRole("link", { name: /E2E Movie 0001/ }),
    ).toHaveCount(0);
  });

  test("a search with no matches shows the empty state", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/movies"));
    await page.getByLabel("Search saved titles").fill("zzz-no-such-movie");
    await page.getByRole("button", { name: "Apply title filter" }).click();
    await expect(
      page.getByText("No matching movies").first(),
    ).toBeVisible();
  });

  test("sorting changes the visible order", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/movies"));

    // Default sort: Title A–Z. The named fixtures precede the bulk titles.
    await expect(
      page.getByRole("link", { name: "Alpha Fixture" }).first(),
    ).toBeVisible();

    // Title Z–A puts the highest bulk title first.
    await page.getByRole("button", { name: "Sort library" }).click();
    await page
      .getByRole("menuitemradio", { name: "Title Z–A" })
      .click();
    await expect(
      page.getByRole("link", { name: "E2E Movie 5007" }).first(),
    ).toBeVisible();

    // Year, oldest first puts the oldest bulk year first (ties break by title).
    await page.getByRole("button", { name: "Sort library" }).click();
    await page
      .getByRole("menuitemradio", { name: "Year, oldest first" })
      .click();
    // i % 70 === 0 gives year 1950; the smallest such title is E2E Movie 0070
    // and year ties break by title.
    await expect(
      page.getByRole("link", { name: "E2E Movie 0070" }).first(),
    ).toBeVisible();
  });

  test("a catalog past the page cap paginates with page controls", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/movies"));
    await expect(
      page.getByText("Showing one page at a time").first(),
    ).toBeVisible();
    await expect(page.getByText(/Page 1 of \d+/)).toBeVisible();
    await page.getByRole("button", { name: "Next" }).click();
    await expect(page.getByText(/Page 2 of \d+/)).toBeVisible();
    // Totals line: "5010 matching movies · showing 49–96"
    await expect(page.getByText(/showing 49–96/)).toBeVisible();
    await page.getByRole("button", { name: "Previous" }).click();
    await expect(page.getByText(/Page 1 of \d+/)).toBeVisible();
  });

  test("items without artwork show the fallback", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/movies"));
    const fallbacks = page.getByText("Artwork unavailable");
    await expect(fallbacks.first()).toBeVisible();
    expect(await fallbacks.count()).toBeGreaterThan(0);
  });
});
