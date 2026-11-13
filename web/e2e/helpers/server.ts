import { spawn, execSync, type ChildProcess } from "node:child_process";
import { mkdirSync, readFileSync, rmSync } from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";
import { DatabaseSync } from "node:sqlite";

/**
 * Manages one real ReelScarey server process per spec file:
 * a fresh disposable config/data directory under the OS temp dir, a loopback
 * listener on a free port, readiness polling on /readyz and tree teardown.
 *
 * The launch configuration mirrors scripts/smoke.mjs: the app is pointed at
 * its directories with REELSCAREY_* environment variables only.
 */

const HARNESS_DIR = path.join(os.tmpdir(), "reelscarey-e2e-harness");
const REPO_ROOT = path.resolve(fileURLToPath(new URL("../../..", import.meta.url)));
const BIN_PATH = path.join(
  HARNESS_DIR,
  process.platform === "win32" ? "reelscarey-e2e.exe" : "reelscarey-e2e",
);

export type SeedMovie = {
  kind: "movie" | "series";
  tmdb_id: number;
  title: string;
  year?: number | null;
  monitored?: number;
  status?: "missing" | "downloading" | "available";
};

export class ServerFixture {
  readonly label: string;
  readonly dir: string;
  readonly configDir: string;
  readonly mediaDir: string;
  readonly downloadsDir: string;
  readonly dbPath: string;
  port = 0;
  private proc?: ChildProcess;
  private logs = "";
  session?: { cookie: string; csrf: string; username: string; password: string };

  private constructor(label: string) {
    this.label = label;
    this.dir = path.join(
      HARNESS_DIR,
      `run-${label}-${Date.now()}-${Math.floor(Math.random() * 1e6)}`,
    );
    this.configDir = path.join(this.dir, "config");
    this.mediaDir = path.join(this.dir, "media");
    this.downloadsDir = path.join(this.dir, "downloads");
    this.dbPath = path.join(this.configDir, "reelscarey.db");
    for (const dir of [this.configDir, this.mediaDir, this.downloadsDir]) {
      mkdirSync(dir, { recursive: true });
    }
  }

  static async create(label: string): Promise<ServerFixture> {
    if (!rmExists(BIN_PATH)) {
      throw new Error(
        `The e2e server binary is missing at ${BIN_PATH}. globalSetup must build it before specs run.`,
      );
    }
    return new ServerFixture(label);
  }

  url(pathname = "/"): string {
    return `http://127.0.0.1:${this.port}${pathname}`;
  }

  get setupTokenPath(): string {
    return path.join(this.configDir, "setup-token");
  }

  get setupToken(): string {
    return readFileSync(this.setupTokenPath, "utf8").trim();
  }

  async freePort(): Promise<void> {
    const listener = net.createServer();
    await new Promise<void>((resolve, reject) => {
      listener.once("error", reject);
      listener.listen(0, "127.0.0.1", () => resolve());
    });
    this.port = (listener.address() as net.AddressInfo).port;
    await new Promise<void>((resolve) => listener.close(() => resolve()));
  }

  async start(): Promise<void> {
    if (this.proc) throw new Error("server already running");
    await this.freePort();
    this.logs = "";
    this.proc = spawn(
      BIN_PATH,
      [],
      {
        env: {
          ...process.env,
          REELSCAREY_LISTEN_ADDR: `127.0.0.1:${this.port}`,
          REELSCAREY_CONFIG_DIR: this.configDir,
          REELSCAREY_MEDIA_DIR: this.mediaDir,
          REELSCAREY_DOWNLOADS_DIR: this.downloadsDir,
        },
        windowsHide: true,
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    this.proc.stdout?.on("data", (chunk) => {
      this.logs += chunk;
    });
    this.proc.stderr?.on("data", (chunk) => {
      this.logs += chunk;
    });
    for (let attempt = 0; attempt < 400; attempt++) {
      if (this.proc.exitCode !== null) {
        throw new Error(`server exited during startup: ${this.logs}`);
      }
      try {
        const response = await fetch(this.url("/readyz"), {
          signal: AbortSignal.timeout(1000),
        });
        if (response.ok) return;
      } catch {
        // not ready yet
      }
      await delay(100);
    }
    throw new Error(`server did not become ready: ${this.logs}`);
  }

  async stop(): Promise<void> {
    const proc = this.proc;
    if (!proc || proc.exitCode !== null) return;
    if (process.platform === "win32") {
      try {
        execSync(`taskkill /pid ${proc.pid} /T /F`, { stdio: "ignore" });
      } catch {
        // already gone
      }
    } else {
      proc.kill("SIGKILL");
    }
    await new Promise<void>((resolve) => {
      proc.once("exit", () => resolve());
      setTimeout(resolve, 10000);
    });
    this.proc = undefined;
  }

  /** Performs first-run setup through the real API and stores the session. */
  async setupAdmin(
    username = "e2eadmin",
    password = "e2e-only-password-123",
  ): Promise<void> {
    const response = await fetch(this.url("/api/v1/auth/setup"), {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Origin: `http://127.0.0.1:${this.port}`,
      },
      body: JSON.stringify({
        username,
        password,
        setup_token: this.setupToken,
        import_mode: "hardlink",
      }),
    });
    if (response.status !== 201) {
      throw new Error(
        `setup failed (${response.status}): ${await response.text()}`,
      );
    }
    const cookie = response.headers.get("set-cookie")?.split(";")[0];
    const body = (await response.json()) as { csrf_token: string };
    if (!cookie) throw new Error("setup response carried no session cookie");
    this.session = { cookie, csrf: body.csrf_token, username, password };
  }

  /** Authenticated API call for seeding/review state through the real API. */
  async api(
    apiPath: string,
    method: "GET" | "POST" | "PUT" | "DELETE",
    body?: unknown,
  ): Promise<Response> {
    if (!this.session) throw new Error("setupAdmin must run before api()");
    return fetch(this.url(`/api/v1${apiPath}`), {
      method,
      headers: {
        Cookie: this.session.cookie,
        "X-CSRF-Token": this.session.csrf,
        Origin: `http://127.0.0.1:${this.port}`,
        ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
      },
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    });
  }

  /**
   * Runs a function against the SQLite database with the server STOPPED.
   * This is the documented seeding path used by scripts/*-seed.py; the app
   * never sees a concurrently modified database.
   */
  async withDatabase(fn: (db: DatabaseSync) => void): Promise<void> {
    if (this.proc) await this.stop();
    const db = new DatabaseSync(this.dbPath);
    try {
      fn(db);
    } finally {
      db.close();
    }
    await this.start();
  }

  async seedMovies(items: SeedMovie[]): Promise<void> {
    await this.withDatabase((db) => {
      const insert = db.prepare(
        `INSERT INTO media_items(kind,tmdb_id,title,year,monitored,status)
         VALUES(?,?,?,?,?,?)`,
      );
      for (const item of items) {
        insert.run(
          item.kind,
          item.tmdb_id,
          item.title,
          item.year ?? null,
          item.monitored ?? 1,
          item.status ?? "missing",
        );
      }
    });
  }

  async dispose(): Promise<void> {
    await this.stop();
    rmSync(this.dir, { recursive: true, force: true });
  }
}

function rmExists(p: string): boolean {
  try {
    readFileSync(p);
    return true;
  } catch {
    return false;
  }
}

/** Logs in through the real sign-in form and waits for the dashboard. */
export async function login(
  page: import("@playwright/test").Page,
  server: ServerFixture,
): Promise<void> {
  await page.goto(server.url("/"));
  await page.getByLabel("Username").fill(server.session!.username);
  await page.getByLabel("Password").fill(server.session!.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  // Signed-in shell: the main navigation replaces the auth screen. (The
  // dashboard heading shows the configured library name, which tests may
  // have changed, so do not key on it.)
  await page
    .getByRole("navigation")
    .first()
    .getByRole("link", { name: "Dashboard" })
    .waitFor({ timeout: 20000 });
}
