import { lazy, Suspense } from "react";
import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { PageLoadBoundary } from "../components/PageLoadBoundary";

it("offers recovery when a page chunk from an older deployment cannot load", async () => {
  const log = vi.spyOn(console, "error").mockImplementation(() => {});
  const Page = lazy(() =>
    Promise.reject(new Error("Old chunk no longer exists")),
  );
  render(
    <PageLoadBoundary>
      <Suspense fallback={<p>Loading page</p>}>
        <Page />
      </Suspense>
    </PageLoadBoundary>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "This page could not be loaded",
  );
  expect(
    screen.getByRole("button", { name: "Reload application" }),
  ).toBeVisible();
  log.mockRestore();
});
