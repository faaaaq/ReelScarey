import { expect, test, type Page } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { ServerFixture, login } from "./helpers/server";

const OUT = path.resolve("..", "audit", "ux-review-2026-11-19");
const ADMIN = {
  username: "requestauditadmin",
  password: "request-audit-admin-123",
};
const REQUESTER = {
  username: "requestauditviewer",
  password: "request-audit-viewer-123",
};
const SEEDED_TITLE = "Offline Request Review Fixture";
const SEEDED_TMDB_ID = 969901;

type Account = {
  id: number;
  username: string;
  disabled: boolean;
  request_account_id: number;
};
type RequestItem = {
  id: number;
  account_id: number;
  username: string;
  kind: "movie" | "series";
  tmdb_id: number;
  title: string;
  state: string;
};
type SearchResults = {
  items: Array<{
    tmdb_id: number;
    title: string;
    year: number | null;
    in_library: boolean;
  }>;
};

function evidence(name: string, body: string) {
  writeFileSync(path.join(OUT, "logs", `${name}.txt`), body);
}

async function capture(page: Page, name: string) {
  await page.screenshot({
    path: path.join(OUT, "screenshots", `${name}.png`),
    fullPage: true,
  });
  evidence(
    name,
    `URL: ${page.url()}\n\n${await page.locator("body").innerText()}\n`,
  );
}

async function signInRequester(page: Page, server: ServerFixture) {
  await page.goto(server.url("/"));
  await expect(
    page.getByRole("heading", { name: "Sign in", exact: true }),
  ).toBeVisible();
  await page.getByLabel("Username", { exact: true }).fill(REQUESTER.username);
  await page.getByLabel("Password", { exact: true }).fill(REQUESTER.password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "My requests", exact: true }),
  ).toBeVisible({
    timeout: 20_000,
  });
}

async function signOut(page: Page) {
  const requesterButton = page.getByRole("button", {
    name: "Sign out",
    exact: true,
  });
  if (await requesterButton.count()) {
    await requesterButton.click();
  } else {
    await page.getByRole("button", { name: /^Account:/ }).click();
    await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  }
  await expect(
    page.getByRole("heading", { name: "Sign in", exact: true }),
  ).toBeVisible();
}

test.use({ trace: "off", screenshot: "off", video: "off" });
test.setTimeout(180_000);

test.describe("requester request and administrator review loop", () => {
  let server: ServerFixture;

  test.beforeAll(async () => {
    mkdirSync(path.join(OUT, "screenshots"), { recursive: true });
    mkdirSync(path.join(OUT, "logs"), { recursive: true });
    server = await ServerFixture.create("ux-request-review");
    await server.start();
    await server.setupAdmin(ADMIN.username, ADMIN.password);
  });

  test.afterAll(async () => {
    await server?.dispose();
  });

  test("requester sees a browser-reviewed outcome after sign-in and reload", async ({
    page,
  }) => {
    const flow: string[] = [];
    const record = (message: string) => {
      flow.push(message);
      evidence("request-flow", flow.join("\n") + "\n");
    };
    let requestTitle = SEEDED_TITLE;
    let requestID = 0;
    let providerMode = "";

    try {
      await page.setViewportSize({ width: 1440, height: 1000 });
      await login(page, server);
      await page.goto(server.url("/accounts"));
      await expect(
        page.getByRole("heading", { name: "Accounts", exact: true }),
      ).toBeVisible();

      const accountResponsePromise = page.waitForResponse(
        (response) =>
          response.url().endsWith("/api/v1/accounts") &&
          response.request().method() === "POST",
      );
      await page
        .getByLabel("Username", { exact: true })
        .fill(REQUESTER.username);
      await page
        .getByLabel("Password", { exact: true })
        .fill(REQUESTER.password);
      await page.getByRole("checkbox", { name: /^Requests/ }).check();
      await page
        .getByRole("button", { name: "Create account", exact: true })
        .click();
      const accountResponse = await accountResponsePromise;
      expect(accountResponse.status()).toBe(201);
      const account = (await accountResponse.json()) as Account;
      expect(account.username).toBe(REQUESTER.username);
      expect(account.disabled).toBe(false);
      expect(account.request_account_id).toBeGreaterThan(0);
      await expect(
        page
          .getByRole("table", { name: "Accounts" })
          .getByRole("row")
          .filter({ hasText: REQUESTER.username }),
      ).toContainText("Active");
      await capture(page, "request-01-admin-account-created");
      record(
        `Created central account id=${account.id} with Requests profile id=${account.request_account_id} through the administrator Accounts UI (HTTP ${accountResponse.status()}).`,
      );

      await signOut(page);
      await signInRequester(page, server);
      const searchResponsePromise = page.waitForResponse(
        (response) =>
          response.url().includes("/api/v1/requester/search/movies?") &&
          response.request().method() === "GET",
        { timeout: 40_000 },
      );
      await page.getByLabel("Title", { exact: true }).fill("The Matrix");
      await page.getByRole("button", { name: "Search", exact: true }).click();
      const searchResponse = await searchResponsePromise;
      const searchBody = await searchResponse.text();
      evidence(
        "request-provider-search-response",
        `HTTP ${searchResponse.status()}\n${searchBody}\n`,
      );

      if (searchResponse.ok()) {
        const results = JSON.parse(searchBody) as SearchResults;
        const index = results.items.findIndex((item) => !item.in_library);
        expect(
          index,
          "TMDb returned a title that can be requested",
        ).toBeGreaterThanOrEqual(0);
        const selected = results.items[index];
        requestTitle = selected.title;
        providerMode = `live TMDb result tmdb_id=${selected.tmdb_id}`;
        await capture(page, "request-02-requester-search-results");
        const createResponsePromise = page.waitForResponse(
          (response) =>
            response.url().endsWith("/api/v1/requester/requests") &&
            response.request().method() === "POST",
        );
        await page
          .getByRole("table", { name: "Search results" })
          .getByRole("row")
          .nth(index)
          .getByRole("button", { name: "Request", exact: true })
          .click();
        const createResponse = await createResponsePromise;
        expect(createResponse.status()).toBe(201);
        const created = (await createResponse.json()) as RequestItem;
        requestID = created.id;
        requestTitle = created.title;
        record(
          `Submitted request id=${requestID} through the requester UI using ${providerMode}.`,
        );
      } else {
        const providerError = JSON.parse(searchBody) as {
          error?: { code?: string; message?: string };
        };
        expect(
          providerError.error?.code,
          `only a TMDb availability/configuration failure permits seeding: ${searchBody}`,
        ).toMatch(/^tmdb_/);
        providerMode = `seeded after ${providerError.error?.code}`;
        await expect(
          page.getByText(
            "Title search is unavailable until an administrator connects TMDb. Contact the administrator.",
          ),
        ).toBeVisible();
        await expect(
          page.getByText(/Connect TMDb in Settings before searching/),
        ).toHaveCount(0);
        await capture(page, "request-02-requester-tmdb-unavailable");
        record(
          `Requester search returned HTTP ${searchResponse.status()} (${providerError.error?.code}); using the permitted offline pending-request seed.`,
        );

        await server.withDatabase((db) => {
          const now = Math.floor(Date.now() / 1000);
          db.prepare(
            `INSERT INTO media_requests(account_id,kind,tmdb_id,title,state,created_at,updated_at)
             VALUES(?,?,?,?,'pending',?,?)`,
          ).run(
            account.request_account_id,
            "movie",
            SEEDED_TMDB_ID,
            SEEDED_TITLE,
            now,
            now,
          );
          requestID = Number(
            db
              .prepare(
                "SELECT id FROM media_requests WHERE account_id=? AND kind='movie' AND tmdb_id=?",
              )
              .get(account.request_account_id, SEEDED_TMDB_ID)?.id,
          );
        });
        expect(requestID).toBeGreaterThan(0);
        // withDatabase deliberately restarts the fixture on a new free port.
        await page.goto(server.url("/"));
        await expect(
          page.getByRole("heading", { name: "My requests", exact: true }),
        ).toBeVisible({
          timeout: 20_000,
        });
        record(
          `Seeded pending request id=${requestID} with ServerFixture.withDatabase while the real server was stopped; restarted the same server and retained the requester session.`,
        );
      }

      const requesterRow = page
        .getByRole("table", { name: "My submitted requests" })
        .getByRole("row")
        .filter({ hasText: requestTitle });
      await expect(requesterRow).toContainText("Waiting for review");
      await expect(requesterRow).toContainText(`#${requestID}`);
      await capture(page, "request-03-requester-pending");
      record(
        `Requester sees request id=${requestID} (${requestTitle}) as Waiting for review.`,
      );

      await signOut(page);
      await login(page, server);
      await page
        .getByRole("navigation")
        .first()
        .getByRole("link", { name: "Requests", exact: true })
        .click();
      const adminRow = page
        .getByRole("table", { name: "Requested titles" })
        .getByRole("row")
        .filter({ hasText: requestTitle });
      await expect(adminRow).toContainText(REQUESTER.username);
      await expect(adminRow).toContainText("pending");
      await capture(page, "request-04-admin-pending-review");

      await adminRow
        .getByRole("button", { name: "Decline", exact: true })
        .click();
      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible();
      await expect(dialog).toContainText(requestTitle);
      await expect(dialog).toContainText(
        "The requester can see this status on their Requests page",
      );
      await expect(dialog).not.toContainText("requester is notified");
      await capture(page, "request-05-admin-decline-confirmation");
      const decisionResponsePromise = page.waitForResponse(
        (response) =>
          response.url().endsWith(`/api/v1/requests/${requestID}/decline`) &&
          response.request().method() === "POST",
      );
      await dialog
        .getByRole("button", { name: "Decline request", exact: true })
        .click();
      const decisionResponse = await decisionResponsePromise;
      expect(decisionResponse.status()).toBe(200);
      const decisionBody = await decisionResponse.text();
      evidence(
        "request-admin-decision-response",
        `HTTP ${decisionResponse.status()}\n${decisionBody}\n`,
      );
      await expect(dialog).toHaveCount(0);
      await expect(adminRow).toContainText("declined");
      await capture(page, "request-06-admin-declined");
      record(
        `Administrator declined request id=${requestID} in the browser (HTTP ${decisionResponse.status()}). Decline was chosen so the review does not require TMDb during approval.`,
      );

      await signOut(page);
      await signInRequester(page, server);
      const signedInRow = page
        .getByRole("table", { name: "My submitted requests" })
        .getByRole("row")
        .filter({ hasText: requestTitle });
      await expect(signedInRow).toContainText("Declined");
      await expect(signedInRow).toContainText(`#${requestID}`);
      await expect(signedInRow).not.toContainText("Waiting for review");
      await capture(page, "request-07-requester-declined-after-signin");
      record(
        "Requester saw Declined after a fresh sign-in following the administrator decision.",
      );

      await page.reload();
      const reloadedRow = page
        .getByRole("table", { name: "My submitted requests" })
        .getByRole("row")
        .filter({ hasText: requestTitle });
      await expect(reloadedRow).toContainText("Declined");
      await expect(reloadedRow).toContainText(`#${requestID}`);
      await expect(reloadedRow).not.toContainText("Waiting for review");
      await capture(page, "request-08-requester-declined-after-reload");
      record(
        `PASS: request id=${requestID} remained Declined after reload. Mode: ${providerMode}.`,
      );
      evidence(
        "request-result",
        `PASS\nrequest_id=${requestID}\ntitle=${requestTitle}\naccount_id=${account.request_account_id}\ncentral_account_id=${account.id}\nprovider_mode=${providerMode}\nadmin_action=decline\nrequester_after_signin=Declined\nrequester_after_reload=Declined\n`,
      );
    } catch (error) {
      record(`FAIL: ${error instanceof Error ? error.stack : String(error)}`);
      if (!page.isClosed()) {
        await capture(page, "request-failure").catch(() => undefined);
      }
      throw error;
    }
  });
});
