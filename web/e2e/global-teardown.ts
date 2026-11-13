import { rmSync } from "node:fs";
import os from "node:os";
import path from "node:path";

/**
 * Removes the entire harness temp workspace. The binary is rebuilt on the
 * next run, and per-spec fixture directories are already removed by their
 * own afterAll hooks; this is the catch-all for leftovers from crashed runs.
 */
export default function globalTeardown() {
  const harnessDir = path.join(os.tmpdir(), "reelscarey-e2e-harness");
  rmSync(harnessDir, { recursive: true, force: true });
}
