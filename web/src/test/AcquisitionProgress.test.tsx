import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Theme } from "@radix-ui/themes";
import { AcquisitionProgress } from "../musicDownload";
import type { MusicAcquisitionJob } from "../api";

const failedJob = {
  id: 77,
  source_kind: "top_track",
  title: "Song",
  root: "Music",
  format: "original",
  source_url: "",
  state: "failed",
  track_count: 1,
  completed_count: 0,
  error: "No tracks completed.",
  created_at: 1,
  updated_at: 1,
  acknowledged_at: 0,
  tracks: [
    {
      id: 55,
      job_id: 77,
      position: 1,
      title: "Song",
      artist: "Artist",
      album: "Album",
      duration_ms: 200000,
      state: "failed",
      error:
        "Multiple candidates have equally strong evidence. Choose a source explicitly; ReelScarey will not use search order to decide.",
      candidate_evidence: [
        {
          candidate: {
            id: "aaaaaaaaaaa",
            title: "Artist - Song (Official Audio)",
            channel: "Artist - Topic",
            duration_seconds: 200,
          },
          score: 160,
          eligible: true,
          reasons: ["Track title matches on token boundaries."],
        },
        {
          candidate: {
            id: "bbbbbbbbbbb",
            title: "Other - Song",
            channel: "Other",
            duration_seconds: 200,
          },
          score: 40,
          eligible: false,
          reasons: [
            "Channel conflicts with the requested artist; review instead of assuming identity.",
          ],
        },
      ],
    },
  ],
} as unknown as MusicAcquisitionJob;

describe("AcquisitionProgress candidate review", () => {
  it("shows the specific error and retries the exact reviewed candidate", async () => {
    const onRetry = vi.fn().mockResolvedValue(undefined);
    render(
      <Theme>
        <AcquisitionProgress job={failedJob} onRetry={onRetry} />
      </Theme>,
    );
    expect(screen.getByText("No tracks completed.")).toBeVisible();
    expect(screen.getByText(/Multiple candidates have equally strong/)).toBeVisible();
    expect(screen.getByText(/Review candidates/)).toBeVisible();
    // The ineligible candidate stays visible for diagnosis but cannot be chosen.
    expect(
      screen.queryByRole("button", { name: /Download candidate Other - Song/ }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", {
        name: /Download candidate Artist - Song \(Official Audio\)/,
      }),
    );
    expect(onRetry).toHaveBeenCalledWith(55, "aaaaaaaaaaa");
  });
});
