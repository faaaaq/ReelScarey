import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8787",
      "/healthz": "http://127.0.0.1:8787",
      "/readyz": "http://127.0.0.1:8787",
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
    restoreMocks: true,
    // Playwright E2E specs live in web/e2e and run under @playwright/test,
    // never under vitest.
    exclude: ["**/node_modules/**", "e2e/**"],
  },
});
