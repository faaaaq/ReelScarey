import { expect, test, type Page } from "@playwright/test";
import os from "node:os";
import path from "node:path";
import { ServerFixture, login } from "./helpers/server";

const SHA =
  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
const SOURCE_URL = "https://www.youtube.com/playlist?list=PLxtradays";
const HARNESS_SEED_DIR = path.join(os.tmpdir(), "reelscarey-e2e-harness", "youtube-shows");

/**
 * YouTube Experience 2.0 journeys against the REAL server with a seeded
 * database (the youtube-subtitles.spec.ts pattern). No fake yt-dlp exists and
 * none is stubbed: with yt-dlp unconfigured the inspect path answers an
 * honest 503 (journey 13), while the DB-backed reads (overview, episode
 * table, jobs feed) work without it. The batch-queue journey 9 stubs only the
 * browser's queue POST via Playwright routing to assert the exact reviewed
 * payload and the 409 revision handling; the queue endpoint itself is covered
 * by the Go suite.
 */
test.describe.serial("YouTube shows journeys", () => {
  let server: ServerFixture;

  async function dismissFirstVisitNotice(page: Page) {
    try {
      await page
        .getByRole("dialog")
        .getByRole("button", { name: "Don't show again" })
        .click({ timeout: 5000 });
    } catch {
      // The first-visit notice was already dismissed in this context.
    }
  }

  test.beforeAll(async () => {
    // The host may have a real yt-dlp on PATH (which would make the e2e
    // server reach the network); pin REELSCAREY_YTDLP_PATH to the inert Node
    // executable instead. It satisfies NewInspector (so the yt-dlp-gated
    // DB reads render) but every download attempt fails fast with no network.
    // Journey 13 uses its own server with a nonexistent path for the honest
    // 503.
    process.env.REELSCAREY_YTDLP_PATH = process.execPath;
    server = await ServerFixture.create("youtube-shows");
    await server.start();
    await server.setupAdmin();
    await server.withDatabase((db) => {
      db.exec(`
        INSERT INTO media_items(id,kind,tmdb_id,title,year,status,monitored) VALUES(1,'series',101,'XTRA XG',NULL,'missing',1);
        INSERT INTO media_items(id,kind,tmdb_id,title,year,status,monitored) VALUES(2,'series',102,'XG DAYS',NULL,'missing',1);
        
        INSERT INTO series_details(media_id) VALUES(1);
        INSERT INTO series_details(media_id) VALUES(2);
        INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored) VALUES(1,1,1,1,'Season 1','','',30,1);
        INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored) VALUES(2,2,2,1,'Season 1','','',6,1);
        INSERT INTO youtube_series_sources(id,media_id,source_url,title,mapping,season_number,start_episode,quality,subtitles_json,auto_import,enabled,interval_hours,initialized,revision,requested,next_run,last_checked,state,message,target_snapshot,created_at)
          VALUES('src-xtra',1,'${SOURCE_URL}','XTRA XG','episode',1,1,'best','{"languages":["en"],"automatic":true,"quality":"best"}',1,1,6,1,1,0,0,0,'idle','','{"Title":"XTRA XG"}',100);
        INSERT INTO youtube_series_sources(id,media_id,source_url,title,mapping,season_number,start_episode,quality,subtitles_json,auto_import,enabled,interval_hours,initialized,revision,requested,next_run,last_checked,state,message,target_snapshot,created_at)
          VALUES('src-days',2,'${SOURCE_URL}','XG DAYS','episode',1,1,'best','{"languages":["en"],"automatic":true,"quality":"best"}',0,1,6,1,1,0,0,1700000000,'attention','The last check could not load the playlist.','{"Title":"XG DAYS"}',100);
      `);
      // Show 1: 30 bound, fileless episodes so the batch cap can be exercised.
      const episode = db.prepare(
        `INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored)
         VALUES(?,1,?,?,'XTRA XG episode','','',1)`,
      );
      const entry = db.prepare(
        `INSERT INTO youtube_series_entries(source_id,video_id,position,title,channel,state,episode_id,job_id,note,discovered_at)
         VALUES('src-xtra',?,?,'XTRA XG','XTRA XG','new',?,NULL,'',?)`,
      );
      for (let i = 1; i <= 30; i++) {
        episode.run(i, 1000 + i, i);
        entry.run(`vidX${String(i).padStart(2, "0")}`, i - 1, i, i);
      }
      // Show 2: six episodes in mixed derived states.
      const daysEpisode = db.prepare(
        `INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored)
         VALUES(?,2,?,?,'XG DAYS episode','','',1)`,
      );
      const daysEntry = db.prepare(
        `INSERT INTO youtube_series_entries(source_id,video_id,position,title,channel,state,episode_id,job_id,note,discovered_at)
         VALUES('src-days',?,?,'XG DAYS','XG','new',?,NULL,'',?)`,
      );
      for (let i = 1; i <= 6; i++) {
        daysEpisode.run(40 + i, 2000 + i, i);
        daysEntry.run(`vidD${i}`, i - 1, 40 + i, i);
      }
      // A "downloaded" episode cannot be seeded in e2e: securestore.New
      // validates every release_candidates/downloads payload as sealed data
      // at startup, so a plaintext fixture fails closed. The downloaded state
      // is covered by the Go suite instead.
      // One review per job (youtube_jobs.review_id is UNIQUE NOT NULL).
      const review = db.prepare(
        `INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,media_id,episode_id,target_snapshot,expires_at)
         VALUES(?,?,?,'XG','episode',2,?,'episode:2',9999999999)`,
      );
      const job = (
        id: string,
        video: string,
        title: string,
        episodeID: number,
        state: string,
        message: string,
        file: string,
      ) => {
        review.run(`rev-${id}`, video, title, episodeID);
        db.prepare(
          `INSERT INTO youtube_jobs(id,review_id,video_id,video_title,channel,target_kind,media_id,episode_id,target_snapshot,state,message,source_path,source_size,source_mtime,source_sha256,probe_json,quality,target_label,progress,downloaded_bytes,total_bytes,auto_import,series_source_id,created_at,updated_at)
           VALUES(?,?,?,?, 'XG','episode',2,?,'episode:2',?,?,?,12,0,'${SHA}','{}','best','XG DAYS',0.5,6,12,0,'src-days',unixepoch(),unixepoch())`,
        ).run(id, `rev-${id}`, video, title, episodeID, state, message, file);
      };
      // Forward slashes keep the seeded source paths inside the harness dir
      // without shell-specific escaping.
      const jobFile = (id: string, video: string) =>
        `${HARNESS_SEED_DIR.replace(/\\/g, "/")}/${id}/${video}.mp4`;
      job("job-days-1", "vidD1", "XG DAYS #01", 41, "awaiting_review",
        "Download verified. Review the file and target before import.",
        jobFile("job-days-1", "vidD1"));
      job("job-days-2", "vidD2", "XG DAYS #02", 42, "failed",
        "yt-dlp exited with an error.", jobFile("job-days-2", "vidD2"));
      job("job-days-3", "vidD3", "XG DAYS #03", 43, "queued",
        "", jobFile("job-days-3", "vidD3"));
      job("job-days-6", "vidD6", "XG DAYS #06", 46, "awaiting_review",
        "Download verified, but required English subtitles are unavailable.",
        jobFile("job-days-6", "vidD6"));
      // Journey 4 evidence: S01E01 checked with an English track.
      db.exec(
        `INSERT INTO youtube_subtitle_sources(job_id,language,source_path,size_bytes,source_mtime,sha256)
         VALUES('job-days-1','en','job-days-1/vidD1.en.vtt',20,0,'${SHA}')`,
      );
    });
  });

  test.afterAll(async () => {
    await server?.dispose();
    delete process.env.REELSCAREY_YTDLP_PATH;
  });

  // Journeys 1/2/3: two distinct sources share one URL; the overview renders
  // both identities with honest, distinct counts.
  test("the shows overview lists two distinct sources sharing one URL", async ({
    page,
  }) => {
    const response = await server.api("/youtube/sources", "GET");
    expect(response.status).toBe(200);
    const body = (await response.json()) as {
      items: {
        id: string;
        media_id: number;
        source_url: string;
        show_title: string;
        enabled: boolean;
        counts: Record<string, number>;
      }[];
    };
    expect(body.items).toHaveLength(2);
    const xtra = body.items.find((item) => item.id === "src-xtra");
    const days = body.items.find((item) => item.id === "src-days");
    expect(xtra?.media_id).toBe(1);
    expect(days?.media_id).toBe(2);
    expect(xtra?.source_url).toBe(SOURCE_URL);
    expect(days?.source_url).toBe(SOURCE_URL);
    expect(xtra?.counts).toMatchObject({ entries: 30, missing: 30 });
    expect(days?.counts).toMatchObject({
      entries: 6,
      // The queued job may already have been claimed and honestly failed by
      // the real download worker (no yt-dlp in e2e), so queued is time-sensitive.
      awaiting_review: 2,
      downloaded: 0,
      missing: 6,
    });

    await login(page, server);
    await page.goto(server.url("/youtube/shows"));
    const xtraCard = page
      .locator(".yt-overview-card")
      .filter({ hasText: "XTRA XG" });
    const daysCard = page
      .locator(".yt-overview-card")
      .filter({ hasText: "XG DAYS" });
    // Source-state badges and messages are time-dependent (the live poller
    // checks the enabled sources while e2e runs); the durable identity
    // markers are the mapping, meta and count content.
    await expect(xtraCard).toContainText("Episode number");
    await expect(xtraCard).toContainText("auto-import exact matches");
    await expect(daysCard).toContainText("review imports");
    await expect(xtraCard).toContainText("Entries: 30");
    await expect(xtraCard).toContainText("Missing: 30");
    await expect(daysCard).toContainText("Review needed: 2");
    await expect(daysCard).toContainText("Missing: 6");
    await expect(daysCard).toContainText("review imports");
    // One URL, two identities: both cards render the shared playlist URL.
    await expect(page.getByText(SOURCE_URL)).toHaveCount(2);
  });

  // Journey 4: per-episode table with derived states and subtitle evidence,
  // plus the filter chips.
  test("the series YouTube tab derives episode states and subtitle evidence", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/series/2"));
    await page.getByRole("tab", { name: "YouTube" }).click();
    const table = page.getByRole("table", { name: "YouTube episodes" });
    const row = (code: string) => table.getByRole("row").filter({ hasText: code });
    await expect(row("S01E01")).toContainText("Review needed");
    await expect(row("S01E01")).toContainText("English subs");
    await expect(row("S01E02")).toContainText("Failed");
    await expect(row("S01E02")).toContainText("English subs not checked");
    await expect(row("S01E03")).toContainText(/Queued|Failed/);
    await expect(row("S01E04")).toContainText("Missing");
    await expect(row("S01E05")).toContainText("Missing");
    await expect(row("S01E05")).toContainText("English subs not checked");
    await expect(row("S01E06")).toContainText("Review needed");
    await expect(row("S01E06")).toContainText("English subs missing");

    // Filter chips fence what is shown.
    const chips = page.getByRole("group", { name: "Filter episodes by state" });
    await chips.getByRole("button", { name: "Failed" }).click();
    await expect(table.getByRole("row").filter({ hasText: "S01E02" })).toHaveCount(1);
    await expect(table.getByRole("row").filter({ hasText: "S01E01" })).toHaveCount(0);
    await chips
      .getByRole("button", { name: "Missing English subtitles" })
      .click();
    await expect(table.getByRole("row").filter({ hasText: "S01E06" })).toHaveCount(1);
    await expect(table.getByRole("row").filter({ hasText: "S01E02" })).toHaveCount(0);
    await chips.getByRole("button", { name: "All" }).click();
  });

  // Journey 9: the batch control refuses more than 25 selections and posts
  // the reviewed payload; a 409 renders the honest revision-conflict message.
  test("batch selection caps at 25 and posts reviewed selections", async ({
    page,
  }) => {
    await login(page, server);
    type QueuePayload = {
      revision: number;
      reviewed: boolean;
      selections: { video_id: string; episode_id: number }[];
    };
    let payload: QueuePayload | null = null;
    await page.route(
      "**/api/v1/youtube/sources/src-xtra/episodes/queue",
      async (route) => {
        payload = route.request().postDataJSON() as QueuePayload;
        const accepted = (payload?.selections ?? []).map((selection) => ({
          ...selection,
          state: "queued",
        }));
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ items: accepted }),
        });
      },
    );
    await page.goto(server.url("/series/1"));
    await page.getByRole("tab", { name: "YouTube" }).click();
    const table = page.getByRole("table", { name: "YouTube episodes" });
    const boxes = table.getByRole("checkbox");
    await expect(boxes).toHaveCount(30);
    for (let i = 0; i < 30; i++) {
      await boxes.nth(i).check();
    }
    const queueButton = page.getByRole("button", {
      name: "Queue selected (first 25 of 30)",
    });
    await expect(queueButton).toBeVisible();
    await expect(
      page.getByText(
        "At most 25 episodes can be queued at once; deselect the rest.",
      ),
    ).toBeVisible();
    await queueButton.click();
    expect(payload).not.toBeNull();
    expect(payload!.reviewed).toBe(true);
    expect(payload!.revision).toBe(1);
    expect(payload!.selections).toHaveLength(25);
    expect(payload!.selections[0]).toEqual({
      video_id: "vidX01",
      episode_id: 1,
    });
    // Success clears the selection (the parent reload remounts the table, so
    // the transient notice/results list rendering is covered by Vitest).
    await expect(
      page.getByRole("button", { name: "Queue selected (0)" }),
    ).toBeDisabled();

    // A 409 revision conflict must not be silently retried; the honest
    // "This show changed" message rendering is covered by Vitest (the parent
    // reload remounts the table, so the message is transient in the browser).
    await page.unroute("**/api/v1/youtube/sources/src-xtra/episodes/queue");
    let conflicts = 0;
    await page.route(
      "**/api/v1/youtube/sources/src-xtra/episodes/queue",
      async (route) => {
        conflicts++;
        await route.fulfill({
          status: 409,
          contentType: "application/json",
          body: JSON.stringify({
            error: { code: "youtube_changed", message: "This show changed." },
          }),
        });
      },
    );
    await boxes.nth(0).check();
    await boxes.nth(1).check();
    await page.getByRole("button", { name: "Queue selected (2)" }).click();
    await expect.poll(async () => conflicts, { timeout: 5000 }).toBe(1);
    await page.waitForTimeout(1000);
    expect(conflicts).toBe(1); // no automatic retry loop
    await page.unroute("**/api/v1/youtube/sources/src-xtra/episodes/queue");
  });

  // Journey 13: with yt-dlp unconfigured, the inspect step says so instead of
  // pretending to work.
  test("inspect honestly reports yt-dlp as unavailable", async ({ page }) => {
    // A dedicated server with no yt-dlp executable at all: the inspector is
    // nil, so the inspect path must answer the honest 503.
    const unavailable = await ServerFixture.create("youtube-shows-unavailable");
    try {
      process.env.REELSCAREY_YTDLP_PATH = path.join(
        HARNESS_SEED_DIR,
        "no-yt-dlp-here.exe",
      );
      await unavailable.start();
      await unavailable.setupAdmin();
      await login(page, unavailable);
      await page.goto(unavailable.url("/youtube"));
      await dismissFirstVisitNotice(page);
      await page.getByLabel("YouTube URL").fill(SOURCE_URL);
      const [response] = await Promise.all([
        page.waitForResponse(
          (r) =>
            r.url().includes("/api/v1/youtube/inspect") &&
            r.request().method() === "POST",
        ),
        page.getByRole("button", { name: "Load" }).click(),
      ]);
      // The backend honestly refuses: 503 youtube_unavailable, not a fake
      // success and not a crash.
      expect(response.status()).toBe(503);
      const errorBody = (await response.json()) as { error: { code: string } };
      expect(errorBody.error.code).toBe("youtube_unavailable");
      // The actionable backend copy is surfaced verbatim in the UI.
      await expect(page.getByRole("alert")).toContainText(
        "Install yt-dlp on the ReelScarey server",
      );
    } finally {
      await unavailable.dispose();
      process.env.REELSCAREY_YTDLP_PATH = process.execPath;
    }
  });

  // Journey 12: keyboard reachability, aria-pressed chips, dialog focus and a
  // usable 375px layout without page-level horizontal scroll.
  test("keyboard, dialog focus and narrow-viewport behavior", async ({
    page,
  }) => {
    await login(page, server);
    await page.goto(server.url("/series/2"));
    await page.getByRole("tab", { name: "YouTube" }).click();
    const chips = page.getByRole("group", { name: "Filter episodes by state" });
    const failedChip = chips.getByRole("button", { name: "Failed" });
    await failedChip.focus();
    expect(
      await failedChip.evaluate(
        (el) =>
          el === document.activeElement &&
          (el as HTMLElement).offsetParent !== null,
      ),
    ).toBe(true);
    // Keyboard activation updates the pressed state and filters the table.
    await page.keyboard.press("Enter");
    await expect(failedChip).toHaveAttribute("aria-pressed", "true");
    const table = page.getByRole("table", { name: "YouTube episodes" });
    await expect(table.getByRole("row").filter({ hasText: "S01E02" })).toHaveCount(1);
    // The chips behave like a radio group: keyboard-activating "All" clears
    // the "Failed" pressed state.
    const allChip = chips.getByRole("button", { name: "All" });
    await allChip.focus();
    await page.keyboard.press("Enter");
    await expect(failedChip).toHaveAttribute("aria-pressed", "false");
    await expect(allChip).toHaveAttribute("aria-pressed", "true");

    // A Tab walk from the chips reaches the first selectable episode checkbox
    // (S01E02 failed; S01E01 awaits import review so its checkbox is
    // intentionally disabled) without focus ever being dropped, and each stop
    // is visible.
    await chips.getByRole("button", { name: "All" }).focus();
    let focusedLabel = "";
    for (let i = 0; i < 20 && !focusedLabel; i++) {
      await page.keyboard.press("Tab");
      focusedLabel = await page.evaluate(() => {
        const el = document.activeElement as HTMLElement | null;
        if (!el || el === document.body || (el as HTMLElement).offsetParent === null)
          return "dropped";
        const label = el.getAttribute("aria-label") ?? "";
        return label.startsWith("Select S01E02") ? label : "";
      });
    }
    expect(focusedLabel).toContain("Select S01E02");

    // Destructive action: the Remove confirm dialog receives focus and can be
    // dismissed without any network call.
    await page
      .getByRole("button", { name: "Remove", exact: true })
      .click();
    const dialog = page.getByRole("dialog", {
      name: "Remove this YouTube source?",
    });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeVisible();
    expect(
      await dialog.evaluate((el) => el.contains(document.activeElement)),
    ).toBe(true);
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);

    // 375px viewport: both YouTube surfaces stay usable with no page-level
    // horizontal scrollbar (tables scroll inside their own wrapper).
    await page.setViewportSize({ width: 375, height: 667 });
    for (const pathname of ["/youtube/downloads", "/youtube/shows"]) {
      await page.goto(server.url(pathname));
      const overflow = await page.evaluate(() => ({
        scroll: document.documentElement.scrollWidth,
        client: document.documentElement.clientWidth,
      }));
      expect(
        overflow.scroll,
        `${pathname} must not scroll horizontally at 375px`,
      ).toBeLessThanOrEqual(overflow.client + 1);
      await expect(page.locator("body")).toBeVisible();
    }
    await page.setViewportSize({ width: 1280, height: 720 });
  });

  // Journey 10, last so the restart does not disturb the tests above: seeded
  // jobs, sources and their states survive a real process stop/start.
  test("jobs and sources persist across a server restart", async ({ page }) => {
    const assertDownloads = async () => {
      const panel = page.getByLabel("YouTube downloads", { exact: true });
      await expect(panel.getByText("XG DAYS #01")).toBeVisible();
      await expect(panel.getByText("XG DAYS #02")).toBeVisible();
      await expect(panel.getByText("XG DAYS #03")).toBeVisible();
      await expect(
        panel.locator("li.yt-job").filter({ hasText: "XG DAYS #02" }),
      ).toContainText("Failed");
      await expect(
        panel.locator("li.yt-job").filter({ hasText: "XG DAYS #03" }),
      ).toContainText(/Queued|Failed/);
    };
    await login(page, server);
    await page.goto(server.url("/youtube/downloads"));
    await dismissFirstVisitNotice(page);
    await assertDownloads();
    await server.stop();
    await server.start();
    await page.goto(server.url("/"));
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
    await page.goto(server.url("/youtube/downloads"));
    await assertDownloads();
    // The overview still lists both sources with the same identities.
    await page.goto(server.url("/youtube/shows"));
    await expect(
      page.locator(".yt-overview-card").filter({ hasText: "XTRA XG" }),
    ).toContainText("Entries: 30");
    await expect(
      page.locator(".yt-overview-card").filter({ hasText: "XG DAYS" }),
    ).toContainText("Review needed: 2");
  });
});
