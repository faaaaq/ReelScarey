import { expect, test } from "@playwright/test";
import { ServerFixture, login } from "./helpers/server";

/**
 * Scenario F: dialog-gated destructive action on synthetic data.
 *
 * Language profiles are safe synthetic targets: the fixture creates them via
 * the real API, the UI deletes them through the shared ConfirmDialog. The
 * final phase stops the server and verifies the RS-0007 contract: a failed
 * destructive confirmation keeps the dialog open with an honest error and
 * never pretends the action completed.
 */
test.describe.serial("Scenario F: destructive confirm dialog", () => {
  let server: ServerFixture;
  const PROFILE = "E2E Profile";

  async function createProfile(name: string) {
    const response = await server.api("/subtitles/language-profiles", "POST", {
      name,
      tracks: [
        { language: "en", variant: "full" },
        { language: "es", variant: "forced" },
      ],
      audio_languages: ["en"],
      revision: 0,
    });
    if (response.status !== 201 && response.status !== 200) {
      throw new Error(
        `profile creation failed (${response.status}): ${await response.text()}`,
      );
    }
  }

  test.beforeAll(async () => {
    server = await ServerFixture.create("destructive");
    await server.start();
    await server.setupAdmin("e2eadmin", "e2e-only-password-123");
    await createProfile(PROFILE);
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("cancel leaves the profile intact", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/language-profiles"));
    const row = page.getByRole("row", { name: new RegExp(PROFILE) });
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("dialog");
    await expect(
      dialog.getByText(`Remove "${PROFILE}"?`).first(),
    ).toBeVisible();
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog).toHaveCount(0);
    await expect(row).toBeVisible();
  });

  test("confirming removes the profile", async ({ page }) => {
    await login(page, server);
    await page.goto(server.url("/language-profiles"));
    const row = page.getByRole("row", { name: new RegExp(PROFILE) });
    await row.getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "Remove profile" }).click();
    await expect(dialog).toHaveCount(0);
    await expect(
      page.getByText(`Removed “${PROFILE}”.`).first(),
    ).toBeVisible();
    await expect(row).toHaveCount(0);
    const listed = await server
      .api("/subtitles/language-profiles", "GET")
      .then((r) => r.json());
    expect(listed.items).toHaveLength(0);
  });

  test("with the backend stopped, a failed confirm keeps the dialog open with an honest error", async ({
    page,
  }) => {
    await createProfile("E2E Offline Profile");
    await login(page, server);
    await page.goto(server.url("/language-profiles"));
    const row = page.getByRole("row", { name: /E2E Offline Profile/ });
    await expect(row).toBeVisible();

    // Kill the real backend, then attempt the destructive action.
    await server.stop();
    await row.getByRole("button", { name: "Remove" }).click();
    const dialog = page.getByRole("dialog");
    await expect(
      dialog.getByText('Remove "E2E Offline Profile"?').first(),
    ).toBeVisible();
    await dialog.getByRole("button", { name: "Remove profile" }).click();
    // RS-0007: the dialog must stay open and show the failure; it must never
    // look like the removal succeeded.
    const alert = dialog.getByRole("alert");
    await expect(alert).toBeVisible();
    await expect(alert).not.toBeEmpty();
    await expect(dialog).toBeVisible();
    // The dialog is still open, i.e. the action was not silently completed.
    await expect(
      dialog.getByRole("button", { name: "Remove profile" }),
    ).toBeVisible();
  });
});
