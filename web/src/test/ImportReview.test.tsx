import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  ImportReviewFacts,
  ImportReviewSummary,
} from "../components/ImportReview";

describe("shared import review", () => {
  it("uses consistent counts and exposes identity, file plan and evidence", () => {
    render(
      <>
        <ImportReviewSummary ready={4} needsDecision={2} skipped={1} />
        <ImportReviewFacts
          identity="Owned movie — S01E01, S01E02"
          destination="/media/owned.mkv"
          operation="Verify and register in place; do not rename, move or retag."
          evidence={[
            "Trusted TMDb folder ID agreed with the saved catalog identity.",
            "The download target conflicts with the folder ID; review both identities.",
          ]}
        />
      </>,
    );
    const counts = screen.getByRole("group", {
      name: "Import review counts",
    });
    expect(counts).toHaveTextContent("Ready4");
    expect(counts).toHaveTextContent("Needs decision2");
    expect(counts).toHaveTextContent("Skipped1");
    expect(screen.getByText("Proposed identity")).toBeVisible();
    expect(screen.getByText("/media/owned.mkv")).toBeVisible();
    expect(screen.getByText(/do not rename, move or retag/)).toBeVisible();
    expect(screen.getByText(/review both identities/)).toBeVisible();
  });

  it("does not invent evidence when none is available", () => {
    render(
      <ImportReviewFacts
        identity=""
        destination=""
        operation="No file operation until review."
        evidence={[]}
      />,
    );
    expect(screen.getByText("Not confirmed")).toBeVisible();
    expect(screen.getByText("Not planned")).toBeVisible();
    expect(screen.getByText(/No match evidence is available/)).toBeVisible();
  });
});
