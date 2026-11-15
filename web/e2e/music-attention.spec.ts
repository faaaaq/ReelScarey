import { expect, test } from "@playwright/test";
import { ServerFixture, login } from "./helpers/server";

/**
 * Music download attention flow against the REAL server:
 * a seeded failed acquisition job must produce an accurate, actionable banner
 * that clears after the owner acknowledges the job, and stays cleared across
 * a reload because the acknowledgment is persisted. Retry is not exercised in
 * the browser because it would start a real yt-dlp download; it is covered by
 * the Go and Vitest suites.
 */
test.describe.serial("Music download attention", () => {
  let server: ServerFixture;

  test.beforeAll(async () => {
    server = await ServerFixture.create("music-attention");
    await server.start();
    await server.setupAdmin();
    await server.withDatabase((db) => {
      db.exec(
        `INSERT INTO music_acquisition_jobs(id,source_url,source_kind,title,root,format,state,track_count,completed_count,error,created_at,updated_at)
         VALUES(5,'','top_track','Generated album','Music','source','failed',2,0,'No tracks completed.',unixepoch(),unixepoch())`,
      );
      const track = db.prepare(
        `INSERT INTO music_acquisition_tracks(job_id,position,title,artist,album,album_artist,track_number,disc_number,duration_ms,cover_url,source_url,search_query,state,error,created_at,updated_at)
         VALUES(5,?,?,'Artist','Album','Artist',?,1,0,'','','','failed','The download did not finish. Use Retry failed tracks in the download queue.',unixepoch(),unixepoch())`,
      );
      track.run(1, "Song One", 1);
      track.run(2, "Song Two", 2);
    });
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("the failed job produces an accurate, well-formed warning", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/music"));
    const banner = page.getByText("Music downloads need attention");
    await expect(banner).toBeVisible();
    const region = banner.locator("xpath=ancestor::*[@role='status']");
    await expect(region).toContainText(
      "1 job has failed or incomplete tracks.",
    );
    // The link and its sentence share one paragraph: no orphaned period line.
    await expect(region).toContainText(
      "Review the download queue to retry failed tracks or clear resolved jobs.",
    );
  });

  test("the queue link leads to an actionable row with a Clear action", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/music"));
    await page
      .getByRole("link", { name: "Review the download queue" })
      .click();
    await expect(page).toHaveURL(/\/music\/discover$/);
    const table = page.getByRole("table", { name: "Recent music downloads" });
    const row = table.getByRole("row").filter({ hasText: "Generated album" });
    await expect(row).toContainText("failed");
    await expect(row).toContainText("0/2");
    await expect(row).toContainText("No tracks completed.");
    await expect(
      row.getByRole("button", { name: "Retry failed tracks" }),
    ).toBeVisible();
    await row.getByRole("button", { name: "Clear" }).click();
    await expect(row).toContainText("acknowledged");
  });

  test("the warning stays cleared after reload because the job is acknowledged", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/music"));
    await expect(
      page.getByText("Music downloads need attention"),
    ).toHaveCount(0);
    await page.reload();
    await expect(
      page.getByText("Music downloads need attention"),
    ).toHaveCount(0);
  });

  // Criterion 9 at the browser level: the acknowledgment lives in the
  // database, so a real process stop/start (not just a reload) must keep the
  // warning cleared while the acknowledged history stays visible.
  test("the acknowledgment survives a full server restart", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/music"));
    await expect(
      page.getByText("Music downloads need attention"),
    ).toHaveCount(0);
    await server.stop();
    await server.start();
    await page.goto(server.url("/"));
    // The session cookie is still valid after restart; sign in again only if
    // the browser was sent back to the auth screen.
    const username = page.getByLabel("Username");
    if (await username.isVisible({ timeout: 5000 }).catch(() => false)) {
      await username.fill(server.session!.username);
      await page.getByLabel("Password").fill(server.session!.password);
      await page.getByRole("button", { name: "Sign in" }).click();
      await page
        .getByRole("navigation")
        .first()
        .getByRole("link", { name: "Dashboard" })
        .waitFor({ timeout: 20000 });
    }
    await page.goto(server.url("/music"));
    await expect(
      page.getByText("Music downloads need attention"),
    ).toHaveCount(0);
    // The acknowledged job's history is intact on the queue page.
    await page.goto(server.url("/music/discover"));
    const table = page.getByRole("table", { name: "Recent music downloads" });
    const row = table.getByRole("row").filter({ hasText: "Generated album" });
    await expect(row).toContainText("acknowledged");
  });
});
