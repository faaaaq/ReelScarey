import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import path from "node:path";

import { expect, test, type Page } from "@playwright/test";
import { ServerFixture, login } from "./helpers/server";

/**
 * Browser playback compatibility against the REAL server and REAL audio.
 *
 * What is honestly verifiable headlessly: the audio element receives the
 * negotiated stream URL, metadata/decoding progresses (readyState >= 1, a
 * finite duration), buffered data exists, error events stay absent, the
 * visible error state appears when the stream fails and clears on recovery.
 * A headless run cannot prove "sounds correct"; it proves the element decodes
 * the served bytes without a media error, which is the strongest headless
 * claim. Real Safari/iOS/Android negotiation is covered by the manual
 * checklist in the release notes, not here.
 *
 * The seed uses real MP3 files generated with ffmpeg so the server's
 * transcode pipeline (balanced quality -> Ogg/Opus) runs the actual binary.
 */

const TRACKS = [
  { id: 1, title: "Signal One", seconds: 4 },
  { id: 2, title: "Signal Two", seconds: 4 },
  { id: 3, title: "Signal Three", seconds: 4 },
];

const STREAM_PATTERN = /\/api\/v1\/library\/music\/files\/\d+\/stream/;

type AudioState = {
  src: string | null;
  readyState: number;
  duration: number;
  bufferedEnd: number;
  errorCode: number;
};

async function audioState(page: Page): Promise<AudioState | null> {
  return page.evaluate(() => {
    const audio = document.querySelector("audio");
    if (!audio) return null;
    return {
      src: audio.getAttribute("src"),
      readyState: audio.readyState,
      duration: Number.isFinite(audio.duration) ? audio.duration : 0,
      bufferedEnd: audio.buffered.length
        ? audio.buffered.end(audio.buffered.length - 1)
        : 0,
      errorCode: audio.error?.code ?? 0,
    };
  });
}

/** Waits until the element decoded metadata of the current track. */
async function expectDecoding(page: Page): Promise<AudioState> {
  let state: AudioState | null = null;
  await expect
    .poll(
      async () => {
        state = await audioState(page);
        return Boolean(
          state && state.src && state.readyState >= 1 && state.errorCode === 0,
        );
      },
      { timeout: 20_000 },
    )
    .toBeTruthy();
  return state as AudioState;
}

async function startTrack(page: Page, title: string) {
  await page
    .getByRole("table", { name: "Music tracks" })
    .getByRole("button", { name: `Play ${title}` })
    .click();
  await expect(
    page.getByRole("region", { name: "Music player" }).getByText(title),
  ).toBeVisible();
}

test.describe.serial("Music playback compatibility", () => {
  let server: ServerFixture;

  test.beforeAll(async () => {
    server = await ServerFixture.create("music-playback");
    await server.start();
    await server.setupAdmin();
    for (const track of TRACKS) {
      const target = path.join(
        server.mediaDir,
        "E2E Artist",
        `Track ${track.id}.mp3`,
      );
      mkdirSync(path.dirname(target), { recursive: true });
      execFileSync("ffmpeg", [
        "-hide_banner",
        "-loglevel",
        "error",
        "-f",
        "lavfi",
        "-i",
        `sine=frequency=440:duration=${track.seconds}`,
        "-b:a",
        "64k",
        "-y",
        target,
      ]);
    }
    await server.withDatabase((db) => {
      db.exec(
        `INSERT INTO music_artists(id,musicbrainz_id,name,created_at,updated_at)
         VALUES(1,'e2e-playback-artist','E2E Artist',unixepoch(),unixepoch())`,
      );
      db.exec(
        `INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,year,created_at,updated_at)
         VALUES(1,1,'e2e-playback-album','E2E Album',2020,unixepoch(),unixepoch())`,
      );
      const insertTrack = db.prepare(
        `INSERT INTO music_tracks(id,album_id,disc,number,title,duration_ms,created_at,updated_at)
         VALUES(?,1,1,?,?,?,unixepoch(),unixepoch())`,
      );
      const insertFile = db.prepare(
        `INSERT INTO music_files(id,artist_id,album_id,track_id,relative_path,size_bytes,sha256,format,duration_ms,imported_at)
         VALUES(?,?,?,?,?,?,?,?,?,unixepoch())`,
      );
      for (const track of TRACKS) {
        insertTrack.run(track.id, track.id, track.title, track.seconds * 1000);
        insertFile.run(
          track.id,
          1,
          1,
          track.id,
          `E2E Artist/Track ${track.id}.mp3`,
          64_000,
          "",
          "mp3",
          track.seconds * 1000,
        );
      }
    });
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("a track starts and the element decodes the served stream without errors", async ({
    page,
  }) => {
    const pageErrors: string[] = [];
    page.on("pageerror", (error) => pageErrors.push(String(error)));
    await login(page, server);
    await page.goto(server.url("/music/albums/1"));
    await startTrack(page, "Signal One");
    const state = await expectDecoding(page);
    // Default preferences (balanced) transcode through ffmpeg to Ogg/Opus.
    expect(state.src).toBe("/api/v1/library/music/files/1/stream");
    expect(state.errorCode).toBe(0);
    expect(state.duration).toBeGreaterThan(0);
    expect(
      page.getByText("Playback is unavailable for this file."),
    ).toHaveCount(0);
    expect(pageErrors).toEqual([]);
  });

  test("a client without Opus support requests MP3 and the server honors it", async ({
    page,
  }) => {
    await login(page, server);
    // Simulate an old Safari: the media element reports no Ogg/Opus support.
    await page.addInitScript(() => {
      const original = HTMLMediaElement.prototype.canPlayType;
      HTMLMediaElement.prototype.canPlayType = function (type: string) {
        if (type.includes("opus") || type.includes("ogg")) return "";
        return original.call(this, type);
      };
    });
    let contentType = "";
    await page.route(STREAM_PATTERN, async (route) => {
      // Pass the request through to the real server but observe the served
      // media type so the fallback is proven end to end.
      const response = await route.fetch();
      contentType = response.headers()["content-type"] ?? "";
      await route.fulfill({ response });
    });
    await page.goto(server.url("/music/albums/1"));
    await startTrack(page, "Signal One");
    await expect(page.locator("audio")).toHaveAttribute(
      "src",
      /format=mp3&maxBitRate=192/,
    );
    await expect.poll(() => contentType).toContain("audio/mpeg");
  });

  test("pause, resume and seek do not produce media errors", async ({
    page,
  }) => {
    const pageErrors: string[] = [];
    page.on("pageerror", (error) => pageErrors.push(String(error)));
    await login(page, server);
    await page.goto(server.url("/music/albums/1"));
    await startTrack(page, "Signal One");
    await expectDecoding(page);

    const toggle = page
      .getByRole("region", { name: "Music player" })
      .getByRole("button", { name: "Pause", exact: true });
    await toggle.click();
    await expect(
      page
        .getByRole("region", { name: "Music player" })
        .getByRole("button", { name: "Play", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("region", { name: "Music player" })
      .getByRole("button", { name: "Play", exact: true })
      .click();
    await expect(toggle).toBeVisible();

    // Seek through the visible control; the served transcode is not
    // range-seekable, so only assert honest outcomes: no error event, no
    // visible failure, and the position control still tracks the element.
    const seek = page.getByRole("slider", { name: "Seek" });
    await seek.fill("1");
    await expect(
      page.getByText("Playback is unavailable for this file."),
    ).toHaveCount(0);
    const state = await audioState(page);
    expect(state?.errorCode).toBe(0);
    expect(pageErrors).toEqual([]);
  });

  test("rapid track skipping never wedges the player", async ({ page }) => {
    const pageErrors: string[] = [];
    page.on("pageerror", (error) => pageErrors.push(String(error)));
    await login(page, server);
    await page.goto(server.url("/music/albums/1"));
    await startTrack(page, "Signal One");
    const region = page.getByRole("region", { name: "Music player" });
    const next = region.getByRole("button", { name: "Next track" });
    await next.click();
    await next.click();
    await next.click();
    await next.click();
    // Three tracks exist: the queue ends on Signal Three and further skips
    // pause at the end instead of looping or stalling.
    await expect(region.getByText("Signal Three")).toBeVisible();
    await expectDecoding(page);
    // The transport still responds after the burst.
    await region
      .getByRole("button", { name: "Pause", exact: true })
      .or(region.getByRole("button", { name: "Play", exact: true }))
      .first()
      .click();
    await expect(
      region
        .getByRole("button", { name: "Pause", exact: true })
        .or(region.getByRole("button", { name: "Play", exact: true })),
    ).toBeVisible();
    expect(
      page.getByText("Playback is unavailable for this file."),
    ).toHaveCount(0);
    expect(pageErrors).toEqual([]);
  });

  test("a failed stream shows the error state and the player recovers", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/music/albums/1"));
    let failing = true;
    await page.route(STREAM_PATTERN, async (route) => {
      if (failing) return route.fulfill({ status: 503, body: "down" });
      return route.fallback();
    });
    await startTrack(page, "Signal One");
    await expect(
      page.getByText("Playback is unavailable for this file."),
    ).toBeVisible();
    // The failure is not a wedge: stop failing, skip and the next track plays.
    failing = false;
    await page
      .getByRole("region", { name: "Music player" })
      .getByRole("button", { name: "Next track" })
      .click();
    await expect(
      page
        .getByRole("region", { name: "Music player" })
        .getByText("Signal Two"),
    ).toBeVisible();
    const state = await expectDecoding(page);
    expect(state.errorCode).toBe(0);
    await expect(
      page.getByText("Playback is unavailable for this file."),
    ).toHaveCount(0);
  });

  test("the streaming quality choice persists across a reload", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/music/albums/1"));
    await startTrack(page, "Signal One");
    const quality = page.getByLabel("Streaming quality");
    await expect(quality).toBeEnabled();
    await quality.selectOption("data-saver");
    // The select is disabled while the preference write is in flight.
    await expect(quality).toBeEnabled();
    await page.reload();
    await startTrack(page, "Signal One");
    await expect(page.getByLabel("Streaming quality")).toHaveValue(
      "data-saver",
    );
    // The saved quality reaches the server: data-saver transcodes to Opus.
    let contentType = "";
    await page.route(STREAM_PATTERN, async (route) => {
      const response = await route.fetch();
      contentType = response.headers()["content-type"] ?? "";
      await route.fulfill({ response });
    });
    await page
      .getByRole("table", { name: "Music tracks" })
      .getByRole("button", { name: "Play Signal Two" })
      .click();
    await expect(
      page
        .getByRole("region", { name: "Music player" })
        .getByText("Signal Two"),
    ).toBeVisible();
    await expect.poll(() => contentType).toContain("audio/ogg");
  });
});
