import { execSync } from "node:child_process";
import { existsSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";

/**
 * Builds the real application once for the whole run:
 *  1. the frontend bundle (web/dist) if it is missing, and
 *  2. the Go server binary, which embeds web/dist via go:embed.
 *
 * The binary is written into the harness temp dir; spec fixtures launch it
 * from there. No repository files are modified.
 */
const repoRoot = path.resolve(process.cwd(), "..");
const harnessDir = path.join(os.tmpdir(), "reelscarey-e2e-harness");
const binName = process.platform === "win32" ? "reelscarey-e2e.exe" : "reelscarey-e2e";
const binPath = path.join(harnessDir, binName);
const manifestPath = path.join(harnessDir, "manifest.json");

function sh(command: string, cwd: string) {
  execSync(command, { cwd, stdio: "inherit", shell: true });
}

export default function globalSetup() {
  if (!existsSync(path.join(repoRoot, "web", "dist", "index.html"))) {
    console.log("[e2e] web/dist is missing; building the frontend");
    sh("npm run build", path.join(repoRoot, "web"));
  }
  console.log(`[e2e] building server binary: ${binPath}`);
  sh(`go build -o "${binPath}" ./cmd/reelscarey`, repoRoot);
  if (!existsSync(binPath)) {
    throw new Error(`go build did not produce ${binPath}`);
  }
  writeFileSync(
    manifestPath,
    JSON.stringify({ binPath, builtAt: new Date().toISOString() }),
  );
  console.log("[e2e] global setup complete");
}
