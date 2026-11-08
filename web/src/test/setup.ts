import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterEach, vi } from "vitest";
import { setCSRFToken } from "../api";

// The full suite runs many jsdom environments in parallel. The default 1s
// async query timeout is too short under that load; keep a generous bound so
// an otherwise passing page is not reported as a flake.
configure({ asyncUtilTimeout: 5000 });

// jsdom lacks a few browser APIs that Radix menus, popovers and selects use.
if (!("ResizeObserver" in globalThis)) {
  class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  Object.assign(globalThis, { ResizeObserver });
}
if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.releasePointerCapture = () => {};
  Element.prototype.setPointerCapture = () => {};
}
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  setCSRFToken("");
  window.localStorage.clear();
  window.sessionStorage.clear();
});
