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
      db.exec(
        "INSERT INTO music_acquisition_jobs(id,source_url,source_kind,title,root,format,state,track_count,completed_count,error,created_at,updated_at) VALUES(5,'','album','Review Candidate Album','Music','source','failed',1,0,'No tracks completed.',unixepoch(),unixepoch())",
      );
      const evidence = JSON.stringify([
        {
          candidate: {
            id: "abcdefghijk",
            title: "Candidate One",
            channel: "Source Test Artist",
            duration_seconds: 181,
          },
          score: 179,
          eligible: true,
          reasons: ["Track title matches on token boundaries."],
          duration_difference_seconds: 1,
        },
        {
          candidate: {
            id: "zyxwvutsrqp",
            title: "Candidate Two",
            channel: "Source Test Artist",
            duration_seconds: 181,
          },
          score: 179,
          eligible: true,
          reasons: ["Track title matches on token boundaries."],
          duration_difference_seconds: 1,
        },
      ]);
      db.prepare(
        "INSERT INTO music_acquisition_tracks(id,job_id,position,title,artist,album,track_number,disc_number,duration_ms,source_url,search_query,state,error,candidate_evidence_json,created_at,updated_at) VALUES(6,5,1,'Missing Song','Source Test Artist','Review Candidate Album',1,1,180000,'','Source Test Artist Missing Song','failed','Multiple candidates have equally strong evidence. Choose a source explicitly; ReelScarey will not use search order to decide.',?,unixepoch(),unixepoch())",
      ).run(evidence);
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

  test("an eligible reviewed candidate resolves the ambiguous job", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/music/discover"));
    const table = page.getByRole("table", { name: "Recent music downloads" });
    const row = table
      .getByRole("row")
      .filter({ hasText: "Review Candidate Album" });
    await row.getByRole("button", { name: "Review" }).click();
    const dialog = page.getByRole("dialog", { name: "Review Candidate Album" });
    await expect(
      dialog.getByText(/Review candidates \(2 eligible of 2\)/),
    ).toBeVisible();
    const responsePromise = page.waitForResponse(
      (response) =>
        response
          .url()
          .includes("/api/v1/library/music/acquisitions/5/tracks/6/retry") &&
        response.request().method() === "POST",
    );
    await dialog
      .getByRole("button", {
        name: "Download candidate Candidate Two for Missing Song",
      })
      .click();
    const response = await responsePromise;
    expect(response.status()).toBe(200);
    await server.withDatabase((db) => {
      const source = db
        .prepare("SELECT source_url FROM music_acquisition_tracks WHERE id=6")
        .get() as { source_url: string };
      expect(source.source_url).toBe("zyxwvutsrqp");
    });
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
