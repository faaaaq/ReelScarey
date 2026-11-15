import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Downloads } from "../pages/youtube/Downloads";

function job(overrides: Record<string, unknown>) {
  return {
    id: "job-1",
    video: { id: "vid1", title: "Video one", channel: "XG" },
    target_label: "XG Days",
    state: "queued",
    message: "",
    quality: "best",
    progress: 0,
    downloaded_bytes: 0,
    total_bytes: 0,
    speed_bps: 0,
    eta_seconds: 0,
    created_at: 1,
    english_subtitles: true,
    ...overrides,
  };
}

function mount(
  response: unknown,
  extraRoutes: (url: string) => Response | undefined = () => undefined,
) {
  const fetch = vi.fn(async (url: string, _options?: RequestInit) => {
    const extra = extraRoutes(url);
    if (extra) return extra;
    if (url.endsWith("/youtube/jobs")) return Response.json(response);
    return Response.json(
      { error: { message: "Unexpected request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter>
        <Downloads />
      </MemoryRouter>
    </Theme>,
  );
  return fetch;
}

describe("YouTube downloads page", () => {
  it("filters jobs with chip buttons and shows server counts when present", async () => {
    mount({
      items: [
        job({
          id: "a",
          state: "queued",
          video: { id: "v1", title: "Queued one", channel: "XG" },
        }),
        job({
          id: "b",
          state: "awaiting_review",
          message: "Ready",
          video: { id: "v2", title: "Review one", channel: "XG" },
        }),
        job({
          id: "c",
          state: "failed",
          message: "Download failed",
          video: { id: "v3", title: "Failed one", channel: "XG" },
        }),
        job({
          id: "d",
          state: "completed",
          message: "Imported.",
          video: { id: "v4", title: "Done one", channel: "XG" },
        }),
      ],
      counts: {
        queued: 1,
        downloading: 0,
        awaiting_review: 1,
        failed: 1,
        completed: 1,
      },
    });
    expect(await screen.findByText("Queued one")).toBeVisible();
    const group = screen.getByRole("group", {
      name: "Filter downloads by state",
    });
    const review = within(group).getByRole("button", {
      name: "Review needed (1)",
    });
    expect(review).toHaveAttribute("aria-pressed", "false");
    await userEvent.click(review);
    expect(review).toHaveAttribute("aria-pressed", "true");
    expect(screen.queryByText("Queued one")).not.toBeInTheDocument();
    expect(screen.getByText("Ready")).toBeVisible();
    const failed = within(group).getByRole("button", { name: "Failed (1)" });
    await userEvent.click(failed);
    expect(screen.getByText("Download failed")).toBeVisible();
    expect(screen.queryByText("Ready")).not.toBeInTheDocument();
  });

  it("computes chip counts client-side and captions truncation without server counts", async () => {
    const items = Array.from({ length: 200 }, (_, index) =>
      job({ id: `job-${index}`, state: "queued" }),
    );
    mount({ items });
    expect(
      await screen.findByRole("button", { name: "All (200)" }),
    ).toBeVisible();
    expect(screen.getByText("Showing the latest 200 downloads.")).toBeVisible();
  });

  it("groups jobs by their per-show source and by target", async () => {
    mount({
      items: [
        job({
          id: "s1",
          target_label: "S01E01 · Pilot",
          series_source_id: "src1",
          target: { kind: "episode", media_id: 77 },
        }),
        job({
          id: "s2",
          target_label: "S01E02 · Second",
          series_source_id: "src1",
          target: { kind: "episode", media_id: 77 },
        }),
        job({ id: "c1", target_label: "XG Days" }),
      ],
    });
    const headings = await screen.findAllByRole("heading", { level: 3 });
    expect(headings.map((h) => h.textContent?.trim())).toEqual([
      "S01E01 · Pilot Open in library",
      "XG Days",
    ]);
    const showLink = screen.getByRole("link", { name: "Open in library" });
    expect(showLink).toHaveAttribute("href", "/series/77");
    expect(screen.getAllByText("Video one")).toHaveLength(3);
  });

  it("reviews verified imports with the full evidence body", async () => {
    const evidence = {
      job_id: "job123",
      size_bytes: 12,
      sha256: "video-hash",
      probe_json: "{}",
      destination: "YouTube/video.mp4",
      target_label: "XG Days",
      subtitles: [
        {
          language: "en",
          size_bytes: 20,
          sha256: "subtitle-hash",
          destination: "YouTube/video.en.vtt",
        },
      ],
    };
    const fetch = mount(
      {
        items: [
          job({ id: "job123", state: "awaiting_review", message: "Ready" }),
        ],
      },
      (url) => {
        if (url.endsWith("/youtube/jobs/job123/import-review"))
          return Response.json(evidence);
        if (url.endsWith("/youtube/jobs/job123/import"))
          return Response.json({ status: "queued" });
        return undefined;
      },
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Review import" }),
    );
    const review = await screen.findByLabelText("Import review");
    expect(review).toHaveTextContent("YouTube/video.en.vtt");
    expect(review).toHaveTextContent("subtitle-hash");
    await userEvent.click(
      within(review).getByRole("button", { name: "Import" }),
    );
    const request = fetch.mock.calls.find(([url]) =>
      url.endsWith("/youtube/jobs/job123/import"),
    );
    expect(JSON.parse(String(request?.[1]?.body))).toEqual(evidence);
    expect(await screen.findByText("Import requested.")).toBeVisible();
  });

  it("retries subtitles without redownloading the verified video", async () => {
    const fetch = mount(
      {
        items: [
          job({
            id: "job-sub",
            state: "awaiting_review",
            english_subtitles: false,
            message:
              "Download verified, but required English subtitles are unavailable.",
          }),
        ],
      },
      (url) => {
        if (url.endsWith("/youtube/jobs/job-sub/subtitles"))
          return Response.json({ status: "retrying" });
        return undefined;
      },
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Retry subtitles" }),
    );
    expect(
      await screen.findByText(
        "Subtitle retry started. The video is not downloaded again.",
      ),
    ).toBeVisible();
    const retry = fetch.mock.calls.find(([url]) =>
      url.endsWith("/youtube/jobs/job-sub/subtitles"),
    );
    expect(retry).toBeDefined();
  });
});
