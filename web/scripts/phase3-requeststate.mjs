// Phase 3 (RS-0036): thread the backend error code into every RequestState
// that reads from a useResource/useLiveResource result, so permission and
// capability failures render with their own frame. Idempotent.
import { readFileSync, writeFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const dir = new URL("../src/pages/", import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1");
for (const name of readdirSync(dir)) {
  if (!name.endsWith(".tsx") && !name.endsWith(".ts")) continue;
  const path = join(dir, name);
  const original = readFileSync(path, "utf8");
  let out = original.replace(
    /<RequestState(\s*\n?\s*loading=\{(\w+)\.loading\}\s*\n?\s*error=\{\2\.error\})(?!\s*\n?\s*code=)/g,
    "<RequestState$1\n        code={$2.code}",
  );
  if (out !== original) {
    writeFileSync(path, out, "utf8");
    console.log("updated", name);
  }
}
