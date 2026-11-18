import { expect, test } from "@playwright/test";
import { createHash } from "node:crypto";
import {
  existsSync,
  linkSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";
import { ServerFixture, login } from "./helpers/server";

test.describe.serial("Permanent deletion of owned fixture files", () => {
  let server: ServerFixture;
  const relative = "Movies/Deletion Fixture/fixture.mkv";
  const subtitle = "Movies/Deletion Fixture/fixture.en.srt";
  const payload = Buffer.from("Synthetic owned deletion fixture");

  test.beforeAll(async () => {
    server = await ServerFixture.create("permanent-delete");
    await server.start();
    await server.setupAdmin();
    await server.seedMovies([
      { kind: "movie", tmdb_id: 970001, title: "Permanent deletion fixture" },
    ]);
    mkdirSync(path.dirname(path.join(server.mediaDir, relative)), {
      recursive: true,
    });
    writeFileSync(path.join(server.mediaDir, relative), payload);
    writeFileSync(
      path.join(server.mediaDir, subtitle),
      "1\n00:00:00,000 --> 00:00:01,000\nOwned cue.\n",
    );
    writeFileSync(
      path.join(server.mediaDir, "Movies/Deletion Fixture/extra.txt"),
      "Unselected extra",
    );
    linkSync(
      path.join(server.mediaDir, relative),
      path.join(server.downloadsDir, "seed.mkv"),
    );
    await server.withDatabase((db) => {
      db.prepare(
        `INSERT INTO media_files(movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin)
        SELECT id,?,?,?,?,unixepoch(),'existing' FROM media_items WHERE tmdb_id=970001`,
      ).run(
        relative,
        payload.length,
        createHash("sha256").update(payload).digest("hex"),
        JSON.stringify({ embedded_title: "Permanent deletion fixture" }),
      );
      db.exec("DELETE FROM video_title_updates");
    });
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("review and confirmation unlink media and subtitles without retained trash", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/movies"));
    await page.getByRole("button", { name: "Select", exact: true }).click();
    await page
      .getByRole("link", { name: "Open Permanent deletion fixture details" })
      .click();
    await page.getByRole("button", { name: "Delete selected (1)" }).click();
    const dialog = page.getByRole("dialog", { name: "Delete selected (1)" });
    await expect(dialog.getByText(/will be permanently deleted/)).toBeVisible();
    await dialog.getByRole("button", { name: "Review deletion" }).click();
    await expect(
      dialog.getByText("2 file paths in this review."),
    ).toBeVisible();
    expect(existsSync(path.join(server.mediaDir, relative))).toBe(true);
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(existsSync(path.join(server.mediaDir, relative))).toBe(false);
    expect(existsSync(path.join(server.mediaDir, subtitle))).toBe(false);
    expect(existsSync(path.join(server.mediaDir, ".reelscarey-trash"))).toBe(
      false,
    );
    expect(readFileSync(path.join(server.downloadsDir, "seed.mkv"))).toEqual(
      payload,
    );
    expect(
      readFileSync(
        path.join(server.mediaDir, "Movies/Deletion Fixture/extra.txt"),
        "utf8",
      ),
    ).toBe("Unselected extra");
  });
});
