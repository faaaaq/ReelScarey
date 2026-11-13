import { expect, test } from "@playwright/test";
import { ServerFixture } from "./helpers/server";

/**
 * Scenario A: fresh user completes first-run setup.
 * A brand-new server shows the "Create administrator" form, validates input,
 * accepts the real setup token, lands in the app and keeps the session
 * across a reload.
 */
test.describe.serial("Scenario A: fresh first-run setup", () => {
  let server: ServerFixture;

  test.beforeAll(async () => {
    server = await ServerFixture.create("setup");
    await server.start();
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("a fresh installation shows the first-run setup form", async ({
    page,
  }) => {
    await page.goto(server.url("/"));
    await expect(
      page.getByRole("heading", { name: "Create administrator" }),
    ).toBeVisible();
    await expect(page.getByLabel("Setup code")).toBeVisible();
    await expect(
      page.getByText(/setup-token/i).first(),
    ).toBeVisible();
  });

  test("a too-short setup code is rejected with validation feedback", async ({
    page,
  }) => {
    await page.goto(server.url("/"));
    await page.getByLabel("Setup code").fill("short");
    await page.getByLabel("Username").fill("e2eadmin");
    await page.getByLabel("Password").fill("e2e-only-password-123");
    await page
      .getByRole("button", { name: "Create administrator" })
      .click();
    await expect(page.getByRole("alert")).toContainText("43 characters");
    // The form never submitted; the setup is still incomplete.
    await expect(
      page.getByRole("heading", { name: "Create administrator" }),
    ).toBeVisible();
    await expect(
      await fetch(server.url("/api/v1/auth/session")).then((r) => r.json()),
    ).toMatchObject({ setup_required: true });
  });

  test("a weak password is blocked by input validation", async ({ page }) => {
    await page.goto(server.url("/"));
    await page.getByLabel("Setup code").fill("x".repeat(43));
    await page.getByLabel("Username").fill("e2eadmin");
    await page.getByLabel("Password").fill("only-10-ch");
    await page
      .getByRole("button", { name: "Create administrator" })
      .click();
    // Native constraint validation blocks the submission.
    const message = await page
      .getByLabel("Password")
      .evaluate((el: HTMLInputElement) => el.validationMessage);
    expect(message).toMatch(/12/);
    await expect(
      page.getByRole("heading", { name: "Create administrator" }),
    ).toBeVisible();
    await expect(
      await fetch(server.url("/api/v1/auth/session")).then((r) => r.json()),
    ).toMatchObject({ setup_required: true });
  });

  test("the administrator is created, lands in the app and keeps the session across a reload", async ({
    page,
  }) => {
    await page.goto(server.url("/"));
    await page.getByLabel("Setup code").fill(server.setupToken);
    await page.getByLabel("Username").fill("e2eadmin");
    await page.getByLabel("Password").fill("e2e-only-password-123");
    await page
      .getByRole("button", { name: "Create administrator" })
      .click();
    // Setup completes into the application shell (setup guide page).
    await expect(
      page.getByRole("heading", { name: "Set up your library" }),
    ).toBeVisible({ timeout: 30000 });
    await expect(
      page.getByRole("heading", { name: "Create administrator" }),
    ).toHaveCount(0);
    // Same browser context: the session must survive a reload.
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Set up your library" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Create administrator" }),
    ).toHaveCount(0);
  });
});
