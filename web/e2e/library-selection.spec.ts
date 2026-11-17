import { expect, test } from "@playwright/test";
import { ServerFixture, login } from "./helpers/server";

test.describe.serial("Library selection in the built application", () => {
  let server: ServerFixture;

  test.beforeAll(async () => {
    server = await ServerFixture.create("selection");
    await server.start();
    await server.setupAdmin();
    await server.seedMovies([
      ...Array.from({ length: 500 }, (_, index) => ({
        kind: "movie" as const,
        tmdb_id: 980000 + index,
        title: `Selection Movie ${String(index).padStart(4, "0")}`,
      })),
      { kind: "series", tmdb_id: 990001, title: "Selection Show" },
      { kind: "series", tmdb_id: 990002, title: "Selection Anime" },
    ]);
    await server.withDatabase((db) => {
      db.exec("UPDATE media_items SET category='anime' WHERE tmdb_id=990002");
    });
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  for (const [route, title] of [
    ["/movies", "Selection Movie 0000"],
    ["/series", "Selection Show"],
    ["/anime", "Selection Anime"],
  ]) {
    test(`${route}: covers select without navigation and deletion is visible`, async ({
      page,
    }) => {
      await login(page, server);
      await page.goto(server.url(route));
      const cover = page.getByRole("link", { name: `Open ${title} details` });
      await expect(cover).toBeVisible();
      await page.getByRole("button", { name: "Select", exact: true }).click();
      await cover.click();
      await expect(page).toHaveURL(server.url(route));
      const selected = page.getByRole("checkbox", {
        name: `Select ${title}`,
        exact: true,
      });
      await expect(selected).toBeChecked();
      const remove = page.getByRole("button", {
        name: "Delete selected (1)",
        exact: true,
      });
      await expect(remove).toBeVisible();
      await expect(remove).toBeInViewport();
      await remove.click();
      await expect(
        page.getByRole("dialog", { name: "Delete selected (1)" }),
      ).toBeVisible();
      await page.getByRole("button", { name: "Cancel", exact: true }).click();
      await cover.click();
      await expect(selected).not.toBeChecked();
      await expect(page).toHaveURL(server.url(route));
    });
  }
});
