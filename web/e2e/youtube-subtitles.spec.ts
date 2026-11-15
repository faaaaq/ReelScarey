import { expect, test } from "@playwright/test";
import { ServerFixture, login } from "./helpers/server";

const SHA =
  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

/**
 * English-subtitle requirement in the REAL browser: a verified YouTube job
 * without English captions shows an explicit missing-English badge and a
 * subtitle-only retry action; a job with English captions shows its subtitle
 * evidence. The retry button is NOT clicked here: it would start a real
 * yt-dlp network session, which e2e must never do — the retry path is covered
 * end to end by the Go suite.
 */
test.describe.serial("YouTube English subtitles", () => {
  let server: ServerFixture;

  test.beforeAll(async () => {
    server = await ServerFixture.create("youtube-subtitles");
    await server.start();
    await server.setupAdmin();
    await server.withDatabase((db) => {
      db.exec(
        `INSERT INTO youtube_collections(id,title,revision,created_at) VALUES(1,'XG Days',1,unixepoch())`,
      );
      const review = db.prepare(
        `INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,collection_id,target_snapshot,expires_at)
         VALUES(?,?,?,?, 'custom',1,'custom:1',9999999999)`,
      );
      review.run("rev-sub", "limOfz2srCk", "XG DAYS #01", "XG");
      review.run("rev-ok", "boKAdg0BS-c", "XG TAPE", "XG");
      const options = db.prepare(
        `INSERT INTO youtube_subtitle_options(review_id,languages_json,automatic,quality) VALUES(?, '["en"]',1,'best')`,
      );
      options.run("rev-sub");
      options.run("rev-ok");
      const job = (id: string, review: string, video: string, title: string, state: string, message: string, path: string) =>
        db.prepare(
          `INSERT INTO youtube_jobs(id,review_id,video_id,video_title,channel,target_kind,collection_id,target_snapshot,state,message,source_path,source_size,source_mtime,source_sha256,probe_json,quality,target_label,progress,downloaded_bytes,total_bytes,auto_import,created_at,updated_at)
           VALUES('${id}','${review}','${video}','${title}','XG','custom',1,'custom:1','${state}','${message}','${path}',12,0,'${SHA}','{}','best','XG Days',1,12,12,0,unixepoch(),unixepoch())`,
        ).run();
      job(
        "job-sub",
        "rev-sub",
        "limOfz2srCk",
        "XG DAYS #01",
        "awaiting_review",
        "Download verified, but required English subtitles are unavailable. Use Retry subtitles to fetch them; import stays blocked until they are acquired.",
        "job-sub-aaa/limOfz2srCk.mp4",
      );
      job(
        "job-ok",
        "rev-ok",
        "boKAdg0BS-c",
        "XG TAPE",
        "awaiting_review",
        "Download verified. Review the file and target before import.",
        "job-ok-bbb/boKAdg0BS-c.mp4",
      );
      db.exec(
        `INSERT INTO youtube_subtitle_sources(job_id,language,source_path,size_bytes,source_mtime,sha256)
         VALUES('job-ok','en','job-ok-bbb/boKAdg0BS-c.en.vtt',20,0,'${SHA}')`,
      );
    });
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("missing English captions are explicit with a retry action; present captions are shown", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/youtube/downloads"));
    const missing = page
      .getByRole("listitem")
      .filter({ hasText: "XG DAYS #01" });
    await expect(missing).toContainText("English subs missing");
    await expect(missing).toContainText("required English subtitles");
    await expect(
      missing.getByRole("button", { name: "Retry subtitles" }),
    ).toBeVisible();
    // Import stays blocked: the retry offer replaces a one-click import.
    await expect(
      missing.getByRole("button", { name: "Review import" }),
    ).toBeVisible();
    const ok = page.getByRole("listitem").filter({ hasText: "XG TAPE" });
    await expect(ok).toContainText("English subs · en");
    await expect(
      ok.getByRole("button", { name: "Retry subtitles" }),
    ).toHaveCount(0);
    // The verified job state is honest: no silent completion claim.
    await expect(ok).toContainText("Review needed");
  });
});
