import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { ServerFixture, login } from "./helpers/server";

const OUT = path.resolve("..", "audit", "ux-review-2026-11-19");
const APP_VERSION = (
  JSON.parse(readFileSync(path.resolve("package.json"), "utf8")) as {
    version: string;
  }
).version;
mkdirSync(path.join(OUT, "screenshots"), { recursive: true });
mkdirSync(path.join(OUT, "logs"), { recursive: true });
const shot = (name: string) => path.join(OUT, "screenshots", `${name}.png`);
const text = (name: string, value: string) =>
  writeFileSync(path.join(OUT, "logs", `${name}.txt`), value);

async function capture(page: import("@playwright/test").Page, name: string) {
  await page.screenshot({ path: shot(name), fullPage: true });
  text(
    name,
    `URL: ${page.url()}\n\n${await page.locator("body").innerText()}\n\nCLICKABLES\n${await page
      .getByRole("button")
      .evaluateAll((nodes) =>
        nodes.map((n) => ({
          text: n.textContent,
          aria: n.getAttribute("aria-label"),
          title: n.getAttribute("title"),
        })),
      )
      .then((value) => JSON.stringify(value, null, 2))}\n\nLINKS\n${await page
      .getByRole("link")
      .evaluateAll((nodes) =>
        nodes.map((n) => ({
          text: n.textContent,
          aria: n.getAttribute("aria-label"),
          href: n.getAttribute("href"),
          title: n.getAttribute("title"),
        })),
      )
      .then((value) => JSON.stringify(value, null, 2))}`,
  );
}

test.describe.serial("UX review first-owner journey", () => {
  let server: ServerFixture;
  const movieGood = "Movies/The Last Reel (2024)/The Last Reel.surviving.mp4";
  const movieBad = "Movies/The Last Reel (2024)/The Last Reel.bad-copy.mp4";
  const episode = "TV Shows/North Harbor/Season 01/North Harbor - S01E01.mp4";
  const episode2 = "TV Shows/North Harbor/Season 01/North Harbor - S01E02.mp4";

  test.beforeAll(async () => {
    server = await ServerFixture.create("ux-review");
    await server.start();
    await server.setupAdmin("auditowner", "disposable-audit-password");
    await server.seedMovies([
      {
        kind: "movie",
        tmdb_id: 880001,
        title: "The Last Reel",
        year: 2024,
        status: "available",
      },
      {
        kind: "series",
        tmdb_id: 880002,
        title: "North Harbor",
        year: 2023,
        status: "missing",
      },
      {
        kind: "series",
        tmdb_id: 880003,
        title: "Paper Moon Anime",
        year: 2022,
        status: "missing",
      },
    ]);
    for (const relative of [movieGood, movieBad, episode, episode2]) {
      const target = path.join(server.mediaDir, relative);
      mkdirSync(path.dirname(target), { recursive: true });
      execFileSync("ffmpeg", [
        "-hide_banner",
        "-loglevel",
        "error",
        "-f",
        "lavfi",
        "-i",
        "color=c=0x27343b:s=640x360:d=4",
        "-f",
        "lavfi",
        "-i",
        "sine=frequency=330:duration=4",
        "-c:v",
        "libx264",
        "-pix_fmt",
        "yuv420p",
        "-c:a",
        "aac",
        "-shortest",
        "-y",
        target,
      ]);
    }
    const audio = path.join(
      server.mediaDir,
      "Music",
      "Aurora Veil",
      "Night Bus",
      "01 Platform Lights.mp3",
    );
    mkdirSync(path.dirname(audio), { recursive: true });
    execFileSync("ffmpeg", [
      "-hide_banner",
      "-loglevel",
      "error",
      "-f",
      "lavfi",
      "-i",
      "sine=frequency=440:duration=8",
      "-b:a",
      "128k",
      "-y",
      audio,
    ]);
    await server.withDatabase((db) => {
      db.exec("UPDATE media_items SET category='anime' WHERE tmdb_id=880003");
      db.exec(
        "INSERT INTO series_details(media_id) SELECT id FROM media_items WHERE kind='series'",
      );
      db.exec(
        "INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) SELECT 1,id,880002,1,'Season 1','','2023-01-01',1,1,'2026-01-01T00:00:00Z' FROM media_items WHERE tmdb_id=880002",
      );
      db.exec(
        "INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,880021,1,'The Arrival','','2023-01-01',1)",
      );
      db.exec(
        "INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(2,1,880022,2,'The Departure','','2023-01-08',1)",
      );
      const insertVideo = db.prepare(
        "INSERT INTO media_files(id,movie_id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(?,?,?,?,?,?,?,unixepoch(),'existing')",
      );
      for (const [id, tmdb, relative] of [
        [1, 880001, movieGood],
        [2, 880001, movieBad],
        [3, 880002, episode],
      ] as const) {
        const bytes = readFileSync(path.join(server.mediaDir, relative));
        insertVideo.run(
          id,
          tmdb === 880002 ? null : 1,
          tmdb === 880002 ? 2 : null,
          relative,
          bytes.length,
          createHash("sha256").update(bytes).digest("hex"),
          "{}",
        );
      }
      db.exec(
        "INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,3)",
      );
      const names = [
        "Aurora Veil",
        "Blue Static",
        "Cobalt Route",
        "Daylight Arcade",
        "Echo Parade",
        "Fiction Club",
        "Golden Hour",
        "High Pines",
        "Ivory Coastlines",
        "June Avenue",
        "Kite Hotel",
        "Low Seasons",
        "Midnight Cinema",
        "Northern Lights",
        "Open Windows",
        "Paper Planes",
        "Quiet Company",
        "Red Letters",
        "Silver Lines",
        "Tiny Islands",
        "Union Street",
        "Velvet Antenna",
        "Western Skies",
        "X-Ray Hearts",
        "Young Fiction",
        "Zero Hour",
      ];
      const artist = db.prepare(
        "INSERT INTO music_artists(id,musicbrainz_id,name,created_at,updated_at) VALUES(?,?,?,unixepoch(),unixepoch())",
      );
      names.forEach((name, index) =>
        artist.run(index + 1, `audit-artist-${index + 1}`, name),
      );
      db.exec(
        "INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,year,created_at,updated_at) VALUES(1,1,'audit-night-bus','Night Bus',2024,unixepoch(),unixepoch())",
      );
      db.exec(
        "INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,year,created_at,updated_at) VALUES(2,2,'audit-missing','Missing Pressing',2021,unixepoch(),unixepoch())",
      );
      db.exec(
        "INSERT INTO music_tracks(id,album_id,disc,number,title,duration_ms,created_at,updated_at) VALUES(1,1,1,1,'Platform Lights',8000,unixepoch(),unixepoch())",
      );
      db.exec(
        "INSERT INTO music_tracks(id,album_id,disc,number,title,duration_ms,created_at,updated_at) VALUES(2,2,1,1,'Absent Melody',180000,unixepoch(),unixepoch())",
      );
      const good = readFileSync(audio);
      db.prepare(
        "INSERT INTO music_files(id,artist_id,album_id,track_id,relative_path,size_bytes,sha256,format,duration_ms,imported_at) VALUES(1,1,1,1,?,?,?, 'mp3',8000,unixepoch())",
      ).run(
        "Music/Aurora Veil/Night Bus/01 Platform Lights.mp3",
        good.length,
        createHash("sha256").update(good).digest("hex"),
      );
      db.exec(
        "INSERT INTO music_files(id,artist_id,album_id,track_id,relative_path,size_bytes,sha256,format,duration_ms,imported_at) VALUES(2,2,2,2,'Music/Blue Static/Missing Pressing/01 Absent Melody.mp3',123456,'','mp3',180000,unixepoch())",
      );
      db.exec(
        "INSERT INTO music_acquisition_jobs(id,source_url,source_kind,title,root,format,state,track_count,completed_count,error,created_at,updated_at) VALUES(5,'','top_track','Night Bus Singles','Music','source','failed',2,0,'No tracks completed.',unixepoch(),unixepoch())",
      );
      const jobTrack = db.prepare(
        "INSERT INTO music_acquisition_tracks(job_id,position,title,artist,album,album_artist,track_number,disc_number,duration_ms,cover_url,source_url,search_query,state,error,created_at,updated_at) VALUES(5,?,?,'Aurora Veil','Night Bus Singles','Aurora Veil',?,1,0,'','','','failed','The download did not finish. Use Retry failed tracks in the download queue.',unixepoch(),unixepoch())",
      );
      jobTrack.run(1, "Station Lights", 1);
      jobTrack.run(2, "Last Train", 2);
    });
  }, 180_000);

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("start at Dashboard and record the initial action hunt", async ({
    page,
  }) => {
    const journey: string[] = [];
    await page.setViewportSize({ width: 1440, height: 1000 });
    await login(page, server);
    journey.push(
      `Land: ${page.url()} — Dashboard. Expected existing-season import under a library Add/Import action; bad-copy removal on movie details; album playback diagnosis on album details.`,
    );
    await expect(
      page.getByRole("heading", { name: "My library" }),
    ).toBeVisible();
    await expect(page.getByText(`Connected · ${APP_VERSION}`)).toBeVisible();
    await expect(
      page.getByText("Music downloads need attention"),
    ).toBeVisible();
    await expect(page.getByText("Night Bus Singles")).toBeVisible();
    await expect(page.getByText("0/2 tracks completed")).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Review download queue" }),
    ).toHaveAttribute("href", "/music/discover");
    await capture(page, "fixed-dashboard-1440");
    text("first-owner-journey", journey.join("\n"));
  });

  test("capture first destinations without using feature documentation", async ({
    page,
  }) => {
    const journey: string[] = [];
    await page.setViewportSize({ width: 1440, height: 1000 });
    await login(page, server);
    await page
      .getByRole("navigation")
      .first()
      .getByRole("link", { name: "Movies", exact: true })
      .click();
    journey.push(
      "First click for bad copy: Movies. Expected title > files/versions > delete one copy.",
    );
    await capture(page, "02-movies-first-click-1440");
    await page
      .getByRole("link", { name: "Open The Last Reel details" })
      .click();
    journey.push(
      `Movie details URL: ${page.url()}. Looking for one-copy deletion, metadata edit, Search versus Manual Search.`,
    );
    await capture(page, "03-movie-details-first-click-1440");
    await page
      .getByRole("navigation")
      .first()
      .getByRole("link", { name: "Music", exact: true })
      .click();
    journey.push(
      "First click for silent album: Music. Expected album > tracks > playback error with cause and repair path.",
    );
    await capture(page, "04-music-first-click-1440");
    await page.goto(server.url("/music/albums/2"));
    await expect(page.getByText("Absent Melody").first()).toBeVisible({
      timeout: 20000,
    });
    await capture(page, "05-broken-album-first-click-1440");
    journey.push(
      "Existing season import was not obvious on Dashboard; next expected destination is Series or a library Import existing action.",
    );
    await page
      .getByRole("navigation")
      .first()
      .getByRole("link", { name: "TV Shows", exact: true })
      .click();
    await capture(page, "06-series-import-hunt-1440");
    text("first-owner-journey", journey.join("\n"));
  });

  test("recheck Music A-Z geometry and artist selection hit targets", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await login(page, server);
    await page.goto(server.url("/music"));
    await expect(
      page.getByText("26 matching artists · all shown"),
    ).toBeVisible();
    const geometry = await page.evaluate(() => {
      const rail = document.querySelector(".lib-az");
      const first = Array.from(document.querySelectorAll("li.lib-card")).find(
        (node) => node.textContent?.includes("Aurora Veil"),
      );
      const rect = (node: Element | null) => {
        if (!node) return null;
        const box = node.getBoundingClientRect();
        const style = getComputedStyle(node);
        return {
          x: box.x,
          y: box.y,
          width: box.width,
          height: box.height,
          position: style.position,
          display: style.display,
        };
      };
      const buttons = Array.from(
        document.querySelectorAll(".lib-az button"),
      ).map((node) => ({
        name: node.getAttribute("aria-label"),
        ...rect(node),
      }));
      return {
        viewport: { width: innerWidth, height: innerHeight },
        rail: rect(rail),
        firstArtist: rect(first ?? null),
        buttons,
        documentWidth: document.documentElement.scrollWidth,
      };
    });
    text("music-az-geometry-1440", JSON.stringify(geometry, null, 2));
    expect(geometry.rail?.position).toBe("fixed");
    expect(geometry.rail?.width).toBeLessThan(60);
    for (const button of geometry.buttons) {
      expect(button.width).toBeGreaterThanOrEqual(24);
      expect(button.height).toBeGreaterThanOrEqual(24);
    }
    await expect(
      page.getByRole("button", { name: "Reload library list" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Update artist & album artwork" }),
    ).toBeVisible();
    await expect(page.locator(".mus-tile-anchor")).toHaveCount(26);
    await expect(page.locator(".mus-tile-anchor").first()).toHaveText("A");
    await expect(page.locator(".mus-record-sleeve").first()).toBeVisible();
    expect(geometry.firstArtist?.y).toBeLessThan(400);
    expect(geometry.firstArtist?.height).toBeLessThan(300);

    await page.getByRole("button", { name: "Select", exact: true }).click();
    const card = page.locator("li.lib-card").filter({ hasText: "Aurora Veil" });
    const selected = page.getByRole("checkbox", {
      name: "Select Aurora Veil",
      exact: true,
    });
    const url = page.url();
    const outcomes: string[] = [];
    await page
      .getByRole("link", { name: "Open Aurora Veil", exact: true })
      .click();
    outcomes.push(
      `artwork: checked=${await selected.isChecked()} url=${page.url()}`,
    );
    await card.getByRole("link", { name: "Aurora Veil", exact: true }).click();
    outcomes.push(
      `title: checked=${await selected.isChecked()} url=${page.url()}`,
    );
    await card.locator(".lib-card-meta").click();
    outcomes.push(
      `metadata: checked=${await selected.isChecked()} url=${page.url()}`,
    );
    await selected.click();
    outcomes.push(
      `checkbox: checked=${await selected.isChecked()} url=${page.url()}`,
    );
    await card.locator(".lib-card-info").click({ position: { x: 8, y: 8 } });
    outcomes.push(
      `row background: checked=${await selected.isChecked()} url=${page.url()}`,
    );
    expect(page.url()).toBe(url);
    await expect(selected).toBeChecked();
    await expect(
      page.getByRole("button", { name: "Delete selected (1)", exact: true }),
    ).toBeInViewport();
    text("music-artist-selection", outcomes.join("\n"));
    await page
      .getByRole("button", { name: "Delete selected (1)", exact: true })
      .click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Cancel", exact: true })
      .click();
    await expect(
      page.getByText("26 matching artists · all shown"),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Delete selected (1)", exact: true }),
    ).toBeFocused();
    outcomes.push(
      `cancel: dialog=closed artist=preserved activeElement=${await page.evaluate(() => document.activeElement?.textContent)}`,
    );
    text("music-artist-selection", outcomes.join("\n"));
    await capture(page, "07-music-selected-1440");
  });

  test("exercise Music album and track selection without navigation", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/music"));
    await page.getByRole("button", { name: "Select", exact: true }).click();
    await page.getByRole("button", { name: "Albums", exact: true }).click();
    const album = page.locator("li.lib-card").filter({ hasText: "Night Bus" });
    const albumCheck = page.getByRole("checkbox", {
      name: "Select Night Bus",
      exact: true,
    });
    await page
      .getByRole("link", { name: "Open Night Bus", exact: true })
      .click();
    await expect(albumCheck).toBeChecked();
    await album.getByRole("link", { name: "Night Bus", exact: true }).click();
    await expect(albumCheck).not.toBeChecked();
    await album.locator(".lib-card-meta").click();
    await expect(albumCheck).toBeChecked();
    await albumCheck.click();
    await expect(albumCheck).not.toBeChecked();
    await album.locator(".lib-card-info").click({ position: { x: 8, y: 8 } });
    await expect(albumCheck).toBeChecked();
    await expect(
      page.getByRole("button", { name: "Delete selected (1)", exact: true }),
    ).toBeInViewport();
    await page
      .getByRole("button", { name: "Delete selected (1)", exact: true })
      .click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "Cancel", exact: true })
      .click();

    await page.getByRole("button", { name: "Tracks", exact: true }).click();
    const trackRow = page
      .getByRole("table", { name: "Music tracks" })
      .getByRole("row")
      .filter({ hasText: "Platform Lights" });
    const trackCheck = page.getByRole("checkbox", {
      name: "Select Platform Lights",
      exact: true,
    });
    await trackRow
      .getByRole("cell")
      .filter({ hasText: "Platform Lights" })
      .click();
    await expect(trackCheck).toBeChecked();
    await trackRow
      .getByRole("link", { name: "Night Bus", exact: true })
      .click();
    await expect(trackCheck).not.toBeChecked();
    await trackRow.getByRole("cell").filter({ hasText: "Aurora Veil" }).click();
    await expect(trackCheck).toBeChecked();
    await trackCheck.click();
    await expect(trackCheck).not.toBeChecked();
    await trackRow.click({ position: { x: 5, y: 5 } });
    await expect(trackCheck).toBeChecked();
    await expect(
      page.getByRole("button", { name: "Delete selected (1)", exact: true }),
    ).toBeInViewport();
    text(
      "music-album-track-selection",
      "PASS: album artwork/title/metadata/checkbox/background and track title/album/artist/checkbox/background each toggled once; counts remained 1; no navigation; delete stayed visible; album cancel preserved data.",
    );
    await capture(page, "08-music-track-selected-1440");
  });

  test("exercise all shared library selection surfaces", async ({ page }) => {
    const outcomes: string[] = [];
    await login(page, server);
    for (const [route, title] of [
      ["/movies", "The Last Reel"],
      ["/series", "North Harbor"],
      ["/anime", "Paper Moon Anime"],
    ] as const) {
      await page.goto(server.url(route));
      await page.getByRole("button", { name: "Select", exact: true }).click();
      const card = page.locator("li.lib-card").filter({ hasText: title });
      const checkbox = page.getByRole("checkbox", {
        name: `Select ${title}`,
        exact: true,
      });
      const url = page.url();
      await page
        .getByRole("link", { name: `Open ${title} details`, exact: true })
        .click();
      const artwork = await checkbox.isChecked();
      await card.getByRole("link", { name: title, exact: true }).click();
      const titleResult = !(await checkbox.isChecked());
      await card.locator(".lib-card-meta").click();
      const metadata = await checkbox.isChecked();
      await checkbox.click();
      const box = !(await checkbox.isChecked());
      await card.locator(".lib-card-info").click({ position: { x: 8, y: 8 } });
      const background = await checkbox.isChecked();
      await expect(
        page.getByRole("button", { name: "Delete selected (1)", exact: true }),
      ).toBeInViewport();
      const removeVisible = true;
      outcomes.push(
        `${route}: artwork=${artwork} title=${titleResult} metadata=${metadata} checkbox=${box} background=${background} deleteVisible=${removeVisible} stayed=${page.url() === url}`,
      );
      expect(page.url()).toBe(url);
      expect(
        artwork &&
          titleResult &&
          metadata &&
          box &&
          background &&
          removeVisible,
      ).toBeTruthy();
      await page
        .getByRole("button", { name: "Delete selected (1)", exact: true })
        .click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Cancel", exact: true })
        .click();
      await expect(checkbox).toBeChecked();
    }
    text("shared-library-selection", outcomes.join("\n"));
  });

  test("predict automatic search and release review before activation", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/movies/1"));
    text(
      "search-label-prediction",
      "Run automatic search should run the saved policy and may queue in download mode; Find releases should open release review without queueing. Both labels now identify the effect before activation.",
    );
    await page.keyboard.press("Control+k");
    await page.getByRole("combobox").fill("Organization");
    await page
      .getByRole("option", {
        name: /Organization — reorganize registered files/,
      })
      .click();
    await expect(page).toHaveURL(/\/organization$/);
    await page.keyboard.press("Control+k");
    const globalSearch = page.getByRole("combobox");
    await globalSearch.fill("Release search");
    await expect(
      page.getByRole("option", { name: "Activity: Release search" }),
    ).toHaveAttribute("aria-selected", "true");
    await globalSearch.press("Enter");
    await expect(page).toHaveURL(/\/releases$/);
    await page.goto(server.url("/movies/1"));
    await page
      .getByRole("button", { name: "Run automatic search", exact: true })
      .click();
    await page.waitForTimeout(750);
    await capture(page, "09-movie-search-unconfigured-1440");
    await page
      .getByRole("link", { name: "Find releases", exact: true })
      .click();
    await expect(page).toHaveURL(/\/releases/);
    await capture(page, "10-movie-manual-search-1440");
  });

  test("verify mobile Actions, both themes and dialog focus recovery", async ({
    page,
  }) => {
    await login(page, server);
    for (const width of [320, 390, 768]) {
      await page.setViewportSize({ width, height: 844 });
      await page.goto(server.url("/movies/1"));
      const actions = page.getByRole("button", {
        name: "Actions",
        exact: true,
      });
      await expect(actions).toBeVisible();
      await expect(actions).toBeInViewport();
      await actions.click();
      const remove = page.getByRole("button", {
        name: "Delete movie",
        exact: true,
      });
      await expect(remove).toBeVisible();
      const geometry = await page.evaluate(() => {
        const button = Array.from(document.querySelectorAll("button")).find(
          (node) => node.textContent?.includes("Delete movie"),
        );
        const box = button?.getBoundingClientRect();
        return {
          viewportWidth: innerWidth,
          documentWidth: document.documentElement.scrollWidth,
          deleteRect: box
            ? { x: box.x, right: box.right, width: box.width }
            : null,
          fullyVisible: !!box && box.x >= 0 && box.right <= innerWidth,
        };
      });
      expect(geometry.documentWidth).toBe(width);
      expect(geometry.fullyVisible).toBe(true);
      text(`fixed-movie-actions-${width}`, JSON.stringify(geometry, null, 2));
      await capture(page, `fixed-movie-actions-${width}`);
      if (width === 390) {
        await remove.click();
        const mobileDialog = page.getByRole("dialog");
        await mobileDialog
          .getByRole("button", { name: "Cancel", exact: true })
          .click();
        await expect(remove).toBeFocused();
      }
    }
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(server.url("/movies/1"));
    await page.getByRole("button", { name: "Switch to dark theme" }).click();
    await capture(page, "fixed-movie-details-dark-1440");
    const remove = page.getByRole("button", {
      name: "Delete movie",
      exact: true,
    });
    await remove.click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await capture(page, "fixed-movie-delete-dialog-dark-1440");
    await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(remove).toBeFocused();
    text(
      "fixed-movie-delete-focus",
      `After Cancel, focused element: ${JSON.stringify(await page.evaluate(() => ({ tag: document.activeElement?.tagName, text: document.activeElement?.textContent, aria: document.activeElement?.getAttribute("aria-label") })))}`,
    );
  });

  test("diagnose a missing album file and verify surviving music playback", async ({
    page,
  }) => {
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(String(error)));
    await login(page, server);
    await page.goto(server.url("/music/albums/2"));
    await expect(page.getByText("Absent Melody").first()).toBeVisible({
      timeout: 20000,
    });
    await capture(page, "15-broken-album-loaded-1440");
    await page
      .getByRole("button", { name: "Play Absent Melody", exact: true })
      .click();
    await expect(
      page.getByText(
        /The registered file is missing or unreadable on the server/,
      ),
    ).toBeVisible({ timeout: 20000 });
    await expect(
      page.getByRole("link", { name: "Open Music Import" }),
    ).toHaveAttribute("href", "/music/import");
    await expect(
      page.getByRole("button", { name: "Retry playback" }),
    ).toBeEnabled();
    await capture(page, "16-broken-album-playback-error-1440");
    text(
      "broken-album-diagnosis",
      `Missing on-disk registration produces: ${await page.locator("body").innerText()}\nPage errors: ${errors.join("; ")}`,
    );
    await page.goto(server.url("/music/albums/1"));
    await page
      .getByRole("button", { name: "Play Platform Lights", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          page
            .locator("audio")
            .evaluate((node) => (node as HTMLAudioElement).readyState),
        { timeout: 20000 },
      )
      .toBeGreaterThanOrEqual(1);
    expect(
      await page
        .locator("audio")
        .evaluate((node) => (node as HTMLAudioElement).error?.code ?? 0),
    ).toBe(0);
    text(
      "surviving-music-playback",
      "PASS: Platform Lights decoded from the real served bytes (readyState >= 1, media error 0) after the unrelated missing file failed.",
    );
  });

  test("start an existing-season import from Dashboard", async ({ page }) => {
    await login(page, server);
    await page
      .getByRole("link", { name: "Library Import", exact: true })
      .click();
    await expect(page).toHaveURL(/\/import$/);
    await page.getByLabel("Scan media type").selectOption("series");
    await capture(page, "22-existing-season-import-start-1440");
    await page
      .getByLabel("Automatically match and register clear results")
      .uncheck();
    const scan = page.getByRole("button", {
      name: /Scan configured folder|Scan and match library/,
    });
    if (await scan.isDisabled()) {
      text(
        "existing-season-import-outcome",
        `BLOCKED: scan button disabled. ${await page.locator("main").innerText()}`,
      );
      return;
    }
    await scan.click();
    await expect(page.getByText("Files in this scan")).toBeVisible({
      timeout: 30000,
    });
    await expect(
      page.getByRole("button", { name: "Refresh status" }),
    ).toBeVisible();
    await page.waitForTimeout(1500);
    await page.getByRole("button", { name: "Refresh status" }).click();
    await capture(page, "23-existing-season-scan-results-1440");
    await page
      .getByRole("button", { name: "Review match", exact: true })
      .click();
    const matchDialog = page.getByRole("dialog", {
      name: "Review existing file",
    });
    await expect(
      matchDialog.getByText("Already in your catalog"),
    ).toBeVisible();
    await matchDialog
      .getByRole("button", { name: "Choose title", exact: true })
      .click();
    await matchDialog.getByLabel(/E02.*The Departure/).check();
    await matchDialog.getByLabel(/I checked that this file belongs/).check();
    await capture(page, "24-existing-season-match-review-1440");
    await matchDialog
      .getByRole("button", { name: "Verify & add in place", exact: true })
      .click();
    await expect(matchDialog).toHaveCount(0, { timeout: 30000 });
    await expect(
      page.getByRole("link", { name: "Open library details" }),
    ).toBeVisible({ timeout: 30000 });
    await page.getByRole("link", { name: "Open library details" }).click();
    await page
      .getByRole("region", { name: "Season 1" })
      .getByRole("button", { name: "Season 1", exact: true })
      .click();
    await expect(page.getByText("The Departure").first()).toBeVisible();
    await capture(page, "25-existing-season-registered-1440");
    text(
      "existing-season-import-outcome",
      `PASS: Dashboard > Library Import > TV Shows > manual scan > Review match > North Harbor > E02 > verify. Registration persisted on the series page; source remained in place at ${path.join(server.mediaDir, episode2)}. The filename hint did not preselect an episode.\n\n${await page.locator("main").innerText()}`,
    );
  });

  test("capture Duplicate Review, then permanently delete only the bad movie copy", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/settings"));
    await expect(
      page.getByText(
        "Inspect probable duplicates, dismiss reviewed groups, or permanently delete selected files after reviewing every path.",
      ),
    ).toBeVisible({ timeout: 20000 });
    await capture(page, "20-settings-duplicate-claim-1440");
    await page.goto(server.url("/duplicates"));
    await expect(
      page.getByRole("heading", { name: "Duplicate Review" }),
    ).toBeVisible({ timeout: 20000 });
    await expect(
      page
        .getByRole("button", { name: /Dismiss|Delete selected files/ })
        .first(),
    ).toBeVisible({ timeout: 20000 });
    await capture(page, "21-duplicate-review-destination-1440");
    const badPath = path.join(server.mediaDir, movieBad);
    const goodPath = path.join(server.mediaDir, movieGood);
    await page.goto(server.url("/movies/1"));
    const row = page
      .getByRole("table")
      .getByRole("row")
      .filter({ hasText: "bad-copy.mp4" });
    await row.getByRole("button", { name: "Delete file", exact: true }).click();
    let dialog = page.getByRole("dialog");
    await capture(page, "17-bad-copy-delete-initial-1440");
    await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
    expect(readFileSync(badPath).length).toBeGreaterThan(0);
    await row.getByRole("button", { name: "Delete file", exact: true }).click();
    dialog = page.getByRole("dialog");
    await expect(dialog.getByText(/will be permanently deleted/)).toBeVisible();
    await dialog
      .getByRole("button", { name: "Review deletion", exact: true })
      .click();
    await expect(
      dialog.getByText("1 file paths in this review."),
    ).toBeVisible();
    await capture(page, "18-bad-copy-delete-review-1440");
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(dialog).toHaveCount(0, { timeout: 30000 });
    await page.reload();
    await expect(
      page
        .getByRole("table", { name: "Registered files" })
        .getByRole("row")
        .filter({ hasText: "surviving.mp4" }),
    ).toBeVisible();
    await expect(page.getByText("bad-copy.mp4")).toHaveCount(0);
    const { existsSync } = await import("node:fs");
    expect(existsSync(badPath)).toBe(false);
    expect(existsSync(goodPath)).toBe(true);
    expect(existsSync(path.join(server.mediaDir, ".reelscarey-trash"))).toBe(
      false,
    );
    text(
      "bad-copy-deletion-outcome",
      `PASS: Cancel preserved bad copy. Permanent review contained one path. After confirmation and reload: badExists=${existsSync(badPath)} survivingExists=${existsSync(goodPath)} trashExists=${existsSync(path.join(server.mediaDir, ".reelscarey-trash"))}. The movie UI exposes no video player, so surviving-copy playback is not supported on this surface; only its preserved registration and bytes were verified.`,
    );
    await capture(page, "19-movie-surviving-copy-1440");
  });
});
