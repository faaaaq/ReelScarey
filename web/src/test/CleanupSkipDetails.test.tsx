import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { expect, it } from "vitest";
import { CleanupSkipDetails } from "../components/CleanupSkipDetails";
import { activePlanDetail } from "../components/MediaWorkBanner";
it("separates unresolved files from retained historical attempts", async () => {
  render(
    <MemoryRouter>
      <CleanupSkipDetails
        cleanup={{
          skipped: 1,
          skip_attempts: 2,
          recovered: 1,
          skip_history: [
            {
              file_id: 1,
              path: "Movies/recovered.mkv",
              message: "Old verifier failed",
              occurred_at: 1000,
              resolved_by: "successful-retry",
              resolved_at: 2000,
            },
            {
              file_id: 2,
              path: "Movies/missing.mkv",
              message: "File is missing",
              occurred_at: 1500,
            },
          ],
        }}
      />
    </MemoryRouter>,
  );
  const unresolved = screen
    .getByText("Unresolved skips (1)")
    .closest("details")!;
  await userEvent.click(within(unresolved).getByText("Unresolved skips (1)"));
  expect(within(unresolved).getByText("Movies/missing.mkv")).toBeVisible();
  expect(
    within(unresolved).queryByText("Movies/recovered.mkv"),
  ).not.toBeInTheDocument();
  const history = screen
    .getByText("Historical failed attempts (2)")
    .closest("details")!;
  await userEvent.click(
    within(history).getByText("Historical failed attempts (2)"),
  );
  expect(within(history).getByText("Movies/recovered.mkv")).toBeVisible();
  expect(
    within(history).getByRole("link", { name: "View successful retry" }),
  ).toHaveAttribute("href", "/organization?view=jobs&plan=successful-retry");
  expect(within(history).getByText("Old verifier failed")).toBeVisible();
});
it("keeps history after every skipped video has recovered", () => {
  render(
    <MemoryRouter>
      <CleanupSkipDetails
        cleanup={{
          skipped: 0,
          skip_attempts: 1,
          recovered: 1,
          skip_history: [
            {
              file_id: 1,
              path: "movie.mkv",
              message: "Old error",
              occurred_at: 0,
              resolved_by: "retry",
              resolved_at: 2000,
            },
          ],
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.queryByText(/Unresolved skips/)).not.toBeInTheDocument();
  expect(screen.getByText("Historical failed attempts (1)")).toBeVisible();
});
it("uses unresolved skips in the global summary", () => {
  const detail = activePlanDetail({
    id: "main",
    state: "running",
    kind: "track_cleanup",
    action: "disable",
    processed: 920,
    total: 1592,
    changed: 300,
    dropped: 0,
    disabled: 1000,
    skipped: 12,
    skip_attempts: 60,
    recovered: 48,
  });
  expect(detail).toContain("12 unresolved skips");
  expect(detail).toContain("48 recovered");
  expect(detail).not.toContain("60");
});
