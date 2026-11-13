import { expect, test } from "@playwright/test";
import { ServerFixture, login } from "./helpers/server";

/**
 * Scenario B: returning user. Setup was already completed (performed via the
 * real setup API in the fixture). The app must open at the sign-in screen
 * (never the setup flow), navigation between sections must work and a reload
 * must keep the session.
 */
test.describe.serial("Scenario B: returning user", () => {
  let server: ServerFixture;

  test.beforeAll(async () => {
    server = await ServerFixture.create("returning");
    await server.start();
    await server.setupAdmin("e2eadmin", "e2e-only-password-123");
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("a completed installation opens at sign-in, not the setup flow", async ({
    page,
  }) => {
    await page.goto(server.url("/"));
    await expect(
      page.getByRole("heading", { name: "Sign in" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Create administrator" }),
    ).toHaveCount(0);
    await expect(page.getByLabel("Setup code")).toHaveCount(0);
  });

  test("signing in lands on the dashboard", async ({ page }) => {
    await login(page, server);
    await expect(
      page.getByRole("heading", { name: "My library" }),
    ).toBeVisible();
  });

  test("navigation between sections works", async ({ page }) => {
    await login(page, server);
    const nav = page.getByRole("navigation").first();
    await nav.getByRole("link", { name: "Movies", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Movies", exact: true }),
    ).toBeVisible();
    expect(new URL(page.url()).pathname).toBe("/movies");

    await nav.getByRole("link", { name: "Wanted", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Wanted" }),
    ).toBeVisible();
    expect(new URL(page.url()).pathname).toBe("/wanted");

    await nav.getByRole("link", { name: "Dashboard", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "My library" }),
    ).toBeVisible();
  });

  test("reloading keeps the session", async ({ page }) => {
    await login(page, server);
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "My library" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Sign in" }),
    ).toHaveCount(0);
  });
});
