import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { describe, expect, it, vi } from "vitest";
import { YouTube } from "../pages/YouTube";

function mount() {
  const fetch = vi.fn(async (url: string, _options?: RequestInit) => {
    if (url.endsWith("/youtube/collections"))
      return Response.json({
        items: [{ id: 2, title: "XG Days", revision: 1 }],
      });
    if (url.endsWith("/youtube/jobs")) return Response.json({ items: [] });
    if (url.endsWith("/youtube/files")) return Response.json({ items: [] });
    if (url.endsWith("/youtube/inspect"))
      return Response.json({
        source: {
          kind: "playlist",
          id: "PLabcdefghijk",
          url: "https://www.youtube.com/playlist?list=PLabcdefghijk",
        },
        title: "XG Days",
        channel: "XG",
        truncated: false,
        entries: [
          {
            kind: "video",
            id: "limOfz2srCk",
            title: "XG DAYS #01–30",
            channel: "XG",
            url: "https://www.youtube.com/watch?v=limOfz2srCk",
            published: "",
          },
        ],
      });
    if (url.endsWith("/youtube/reviews"))
      return Response.json({
        id: "review123",
        video: { id: "limOfz2srCk", title: "XG DAYS #01–30", channel: "XG" },
        target_label: "XG Days",
        expires_at: 2000000000,
        subtitles: { languages: ["en"], automatic: false },
      });
    if (url.endsWith("/youtube/reviews/review123/queue"))
      return Response.json({ id: "job123", state: "queued" });
    return Response.json(
      { error: { message: "Unexpected request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <YouTube />
    </Theme>,
  );
  return fetch;
}

describe("YouTube source review", () => {
  it("requires an explicit video and target review before queuing", async () => {
    const fetch = mount();
    await userEvent.type(
      screen.getByLabelText("Video, playlist or channel URL"),
      "https://www.youtube.com/playlist?list=PLabcdefghijk",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Inspect source" }),
    );
    const results = await screen.findByRole("list", { name: "Source results" });
    expect(within(results).getByText(/XG DAYS #01/)).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Queue download" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      within(results).getByRole("button", { name: "Select video" }),
    );
    await userEvent.selectOptions(
      await screen.findByLabelText("Custom collection"),
      "2",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review selection" }),
    );
    const review = await screen.findByLabelText("Review YouTube download");
    expect(review).toHaveTextContent("XG DAYS #01–30");
    expect(review).toHaveTextContent("Destination: XG Days");
    const preview = fetch.mock.calls.find(([url]) =>
      url.endsWith("/youtube/reviews"),
    );
    expect(JSON.parse(String(preview?.[1]?.body))).toEqual({
      video_id: "limOfz2srCk",
      target: { kind: "custom", collection_id: 2 },
      subtitles: { languages: ["en"], automatic: false },
    });
    await userEvent.click(
      within(review).getByRole("button", { name: "Queue download" }),
    );
    expect(await screen.findByText(/Download queued/)).toBeVisible();
  });

  it("clears a review when its target changes", async () => {
    mount();
    await userEvent.type(
      screen.getByLabelText("Video, playlist or channel URL"),
      "https://youtu.be/limOfz2srCk",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Inspect source" }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Select video" }),
    );
    await userEvent.selectOptions(
      await screen.findByLabelText("Custom collection"),
      "2",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review selection" }),
    );
    await screen.findByLabelText("Review YouTube download");
    await userEvent.selectOptions(
      screen.getByLabelText("Destination type"),
      "movie",
    );
    expect(
      screen.queryByLabelText("Review YouTube download"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Queue download" }),
    ).not.toBeInTheDocument();
  });

  it("shows subtitle evidence before approving import", async () => {
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
    const fetch = vi.fn(async (url: string, _options?: RequestInit) => {
      if (url.endsWith("/youtube/collections"))
        return Response.json({ items: [] });
      if (url.endsWith("/youtube/files")) return Response.json({ items: [] });
      if (url.endsWith("/youtube/jobs"))
        return Response.json({
          items: [
            {
              id: "job123",
              video: { id: "limOfz2srCk", title: "XG DAYS", channel: "XG" },
              state: "awaiting_review",
              message: "Ready",
            },
          ],
        });
      if (url.endsWith("/youtube/jobs/job123/import-review"))
        return Response.json(evidence);
      if (url.endsWith("/youtube/jobs/job123/import"))
        return Response.json({ status: "queued" });
      return Response.json(
        { error: { message: "Unexpected request" } },
        { status: 404 },
      );
    });
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <YouTube />
      </Theme>,
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Review and import" }),
    );
    const review = await screen.findByLabelText("Import review");
    expect(review).toHaveTextContent("YouTube/video.en.vtt");
    expect(review).toHaveTextContent("subtitle-hash");
    await userEvent.click(
      within(review).getByRole("button", { name: "Approve import" }),
    );
    const request = fetch.mock.calls.find(([url]) =>
      url.endsWith("/youtube/jobs/job123/import"),
    );
    expect(JSON.parse(String(request?.[1]?.body))).toEqual(evidence);
  });
});
