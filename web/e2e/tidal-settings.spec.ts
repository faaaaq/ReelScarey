import { expect, test } from "@playwright/test";
import { ServerFixture, login } from "./helpers/server";

/** Real-server TIDAL/source UX with no account, token or external download. */
test.describe.serial("TIDAL status and acquisition source UX", () => {
  let server: ServerFixture;

  test.beforeAll(async () => {
    server = await ServerFixture.create("tidal-settings");
    await server.start();
    await server.setupAdmin();
    await server.withDatabase((db) => {
      db.exec(
        "INSERT INTO music_artists(id,musicbrainz_id,name,created_at,updated_at) VALUES(1,'tidal-source-artist','Source Test Artist',unixepoch(),unixepoch())",
      );
      db.exec(
        "INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,year,tracks_loaded,created_at,updated_at) VALUES(1,1,'tidal-source-album','Source Test Album',2024,1,unixepoch(),unixepoch())",
      );
      db.exec(
        "INSERT INTO music_tracks(id,album_id,disc,number,title,duration_ms,created_at,updated_at) VALUES(1,1,1,1,'Missing Song',180000,unixepoch(),unixepoch())",
      );
    });
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("provider status is explicit and the default source persists", async ({
    page,
  }) => {
    const status = await server.api("/library/music/tidal/status", "GET");
    expect(status.status).toBe(200);
    const body = await status.json();
    expect(body).toMatchObject({
      available: false,
      logged_in: false,
      token_present: false,
      verification_state: "unavailable",
    });
    expect(body.detail).toContain("not installed");

    await login(page, server);
    await page.goto(server.url("/music/settings"));
    await expect(page.getByText("TIDAL provider not installed")).toBeVisible();
    const source = page.getByLabel("Default download source");
    await expect(source).toHaveValue("");
    await expect(source.locator("option[value=tidal]")).toBeDisabled();
    await source.selectOption("youtube");
    await page.getByRole("button", { name: "Save TIDAL settings" }).click();
    await expect(page.getByText("Music settings saved.")).toBeVisible();
    await page.reload();
    await expect(page.getByLabel("Default download source")).toHaveValue(
      "youtube",
    );

    const preferences = await server.api("/library/music/preferences", "GET");
    expect(await preferences.json()).toMatchObject({
      download_source: "youtube",
    });
  });

  test("album source choices are visible, honest and mobile-sized", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await login(page, server);
    await page.goto(server.url("/music/albums/1"));
    await page.getByRole("button", { name: "Download album" }).click();
    const dialog = page.getByRole("dialog", { name: "Download album" });
    await expect(dialog).toBeVisible();
    const source = dialog.getByLabel("Album audio source");
    await expect(source).toHaveValue("youtube");
    await expect(source.locator("option[value=tidal]")).toBeDisabled();
    await expect(
      dialog.getByText(/Only YouTube will be searched/),
    ).toBeVisible();
    await source.selectOption("");
    await expect(dialog.getByText(/Automatic tries TIDAL first/)).toBeVisible();
    await expect(dialog.getByLabel("Album download profile")).toBeVisible();
    await expect(dialog.getByLabel("Album destination root")).toHaveValue(
      "Music",
    );

    const geometry = await dialog.evaluate((node) => {
      const rect = node.getBoundingClientRect();
      return {
        left: rect.left,
        right: rect.right,
        viewport: window.innerWidth,
      };
    });
    expect(geometry.left).toBeGreaterThanOrEqual(0);
    expect(geometry.right).toBeLessThanOrEqual(geometry.viewport);
    await dialog.getByRole("button", { name: "Close" }).last().click();
    await expect(dialog).toHaveCount(0);
  });

  test("status details remain readable at desktop width", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await login(page, server);
    await page.goto(server.url("/music/settings"));
    await expect(page.getByText("TIDAL provider not installed")).toBeVisible();
    await expect(page.getByLabel("Default download source")).toHaveValue(
      "youtube",
    );
    await expect(
      page.getByText(/Music acquisition uses the default source/),
    ).toBeVisible();
  });
});
