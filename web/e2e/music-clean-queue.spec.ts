import { expect, test } from "@playwright/test";
import { ServerFixture, login } from "./helpers/server";

/**
 * Clean-queue honesty in the REAL browser: a fresh server with no acquisition
 * history renders neither the attention warning nor the running banner on
 * /music — the page must not imply work that does not exist.
 */
test.describe.serial("Music clean queue", () => {
  let server: ServerFixture;

  test.beforeAll(async () => {
    server = await ServerFixture.create("music-clean-queue");
    await server.start();
    await server.setupAdmin();
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("/music shows no attention warning and no running banner", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/music"));
    await expect(
      page.getByRole("heading", { name: "Music" }),
    ).toBeVisible();
    await expect(
      page.getByText("Music downloads need attention"),
    ).toHaveCount(0);
    await expect(page.getByText("Music downloads running")).toHaveCount(0);
  });

  test("/music/discover shows an empty download queue without warnings", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/music/discover"));
    // An empty queue is stated honestly: no table is rendered at all.
    await expect(page.getByText("No link or chart downloads yet.")).toBeVisible();
    await expect(
      page.getByRole("table", { name: "Recent music downloads" }),
    ).toHaveCount(0);
    await expect(
      page.getByText("Music downloads need attention"),
    ).toHaveCount(0);
  });
});
