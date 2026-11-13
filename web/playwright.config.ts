import { defineConfig } from "@playwright/test";
import { mkdirSync } from "node:fs";
import os from "node:os";
import path from "node:path";

/**
 * Playwright harness for the REAL ReelScarey application.
 *
 * The Go backend binary is built once in globalSetup (it embeds the built
 * web/dist frontend) and every spec file starts its own disposable server
 * instance against a fresh config directory under the OS temp dir. All
 * fixture state stays inside REELSCAREY_E2E_HARNESS_DIR; nothing is written
 * into the repository.
 */
const harnessDir = path.join(os.tmpdir(), "reelscarey-e2e-harness");
mkdirSync(harnessDir, { recursive: true });
process.env.REELSCAREY_E2E_HARNESS_DIR = harnessDir;

export default defineConfig({
  testDir: "./e2e",
  timeout: 120_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: 1,
  globalSetup: "./e2e/global-setup.ts",
  globalTeardown: "./e2e/global-teardown.ts",
  use: {
    // Default only; each spec drives its own server instance and navigates
    // with absolute URLs from its fixture.
    baseURL: process.env.REELSCAREY_E2E_BASE_URL ?? "http://127.0.0.1:18811",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  reporter: [["list"], ["html", { open: "never" }]],
});
