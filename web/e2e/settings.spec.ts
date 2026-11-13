import { expect, test } from "@playwright/test";
import { ServerFixture, login } from "./helpers/server";

/**
 * Scenario D: settings. The general settings page must populate saved
 * values, persist edits with visible success feedback, survive a reload and
 * refuse invalid input without saving.
 */
test.describe.serial("Scenario D: settings", () => {
  let server: ServerFixture;

  test.beforeAll(async () => {
    server = await ServerFixture.create("settings");
    await server.start();
    await server.setupAdmin("e2eadmin", "e2e-only-password-123");
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("saved values populate the general settings form", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/settings/general"));
    await expect(page.getByLabel("Library name")).toHaveValue("My library");
    await expect(page.getByLabel("Movie folder")).toHaveValue("Movies");
    await expect(page.getByLabel("TV folder")).toHaveValue("TV Shows");
  });

  test("editing a value saves it with success feedback", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/settings/general"));
    await page.getByLabel("Library name").fill("E2E Library");
    await page.getByRole("button", { name: "Save Changes" }).click();
    await expect(page.getByText("Preferences saved.")).toBeVisible();
  });

  test("the saved value survives a reload", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/settings/general"));
    await expect(page.getByLabel("Library name")).toHaveValue("E2E Library");
  });

  test("invalid input shows an error and is not saved", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/settings/general"));
    await page.getByLabel("Movie folder").fill("NUL");
    await page.getByRole("button", { name: "Save Changes" }).click();
    await expect(
      page.getByText(/Movie folder must start with a letter or number/),
    ).toBeVisible();
    await expect(page.getByText("Preferences saved.")).toHaveCount(0);
    // Reload: the invalid value was never persisted.
    await page.reload();
    await expect(page.getByLabel("Movie folder")).toHaveValue("Movies");
  });
});
