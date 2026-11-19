import { chromium } from "@playwright/test";
import path from "node:path";
import { mkdirSync } from "node:fs";
import { pathToFileURL } from "node:url";

const out = path.resolve("..", "audit", "ux-review-2026-11-19", "mockups");
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage();
const url = (file, query) =>
  `${pathToFileURL(path.join(out, file)).href}?${query}`;
for (const scene of ["music", "dashboard", "movie"]) {
  for (const variant of ["restrained", "bold"]) {
    await page.setViewportSize(
      scene === "movie"
        ? { width: 390, height: 844 }
        : { width: 1440, height: 1000 },
    );
    await page.goto(url("mockup.html", `scene=${scene}&variant=${variant}`));
    await page.screenshot({
      path: path.join(out, `${scene}-${variant}.png`),
      fullPage: true,
    });
  }
}
for (const scene of ["music", "dashboard", "movie"]) {
  await page.setViewportSize(
    scene === "movie"
      ? { width: 1100, height: 1000 }
      : { width: 1800, height: 1100 },
  );
  await page.goto(url("compare.html", `scene=${scene}&mode=baseline`));
  await page.screenshot({
    path: path.join(out, `comparison-${scene}.png`),
    fullPage: true,
  });
}
for (const scene of ["music", "dashboard", "movie"]) {
  await page.setViewportSize(
    scene === "movie"
      ? { width: 1100, height: 1000 }
      : { width: 1800, height: 1100 },
  );
  await page.goto(url("compare.html", `scene=${scene}&mode=variants`));
  await page.screenshot({
    path: path.join(out, `variants-${scene}.png`),
    fullPage: true,
  });
}
await browser.close();
console.log("Rendered 6 mockups and 6 comparison boards.");
