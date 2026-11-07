import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
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
  // The first-visit overlay has its own test; these tests cover the page.
  window.localStorage.setItem(
    "reelscarey-notice:work-in-progress:youtube",
    "hidden",
  );
  render(
    <Theme>
      <MemoryRouter>
        <YouTube />
      </MemoryRouter>
    </Theme>,
  );
  return fetch;
}

describe("YouTube source review", () => {
  it("requires an explicit video and target review before queuing", async () => {
    const fetch = mount();
    await userEvent.type(
      screen.getByLabelText("YouTube URL"),
      "https://www.youtube.com/playlist?list=PLabcdefghijk",
    );
    await userEvent.click(screen.getByRole("button", { name: "Load" }));
    const results = await screen.findByRole("list", { name: "Source results" });
    expect(within(results).getByText(/XG DAYS #01/)).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Download" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      within(results).getByRole("button", { name: /XG DAYS #01/ }),
    );
    await userEvent.selectOptions(
      await screen.findByLabelText("Collection"),
      "2",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Check destination" }),
    );
    const review = await screen.findByLabelText("Review YouTube download");
    expect(review).toHaveTextContent("XG Days");
    const preview = fetch.mock.calls.find(([url]) =>
      url.endsWith("/youtube/reviews"),
    );
    expect(JSON.parse(String(preview?.[1]?.body))).toEqual({
      video_id: "limOfz2srCk",
      target: { kind: "custom", collection_id: 2 },
      subtitles: { languages: ["en"], automatic: false, quality: "best" },
    });
    await userEvent.click(
      within(review).getByRole("button", { name: "Download" }),
    );
    expect(await screen.findByText("Download queued.")).toBeVisible();
  });

  it("clears a review when its target changes", async () => {
    mount();
    await userEvent.type(
      screen.getByLabelText("YouTube URL"),
      "https://youtu.be/limOfz2srCk",
    );
    await userEvent.click(screen.getByRole("button", { name: "Load" }));
    const results = await screen.findByRole("list", { name: "Source results" });
    await userEvent.click(
      within(results).getByRole("button", { name: /XG DAYS #01/ }),
    );
    await userEvent.selectOptions(
      await screen.findByLabelText("Collection"),
      "2",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Check destination" }),
    );
    await screen.findByLabelText("Review YouTube download");
    await userEvent.selectOptions(
      screen.getByLabelText("Destination"),
      "movie",
    );
    expect(
      screen.queryByLabelText("Review YouTube download"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Download" }),
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
    window.localStorage.setItem(
      "reelscarey-notice:work-in-progress:youtube",
      "hidden",
    );
    render(
      <Theme>
        <MemoryRouter>
          <YouTube />
        </MemoryRouter>
      </Theme>,
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
  });

  it("matches a playlist to a saved show and queues selected episodes", async () => {
    const fetch = vi.fn(async (url: string, _options?: RequestInit) => {
      if (url.endsWith("/youtube/collections"))
        return Response.json({ items: [] });
      if (url.endsWith("/youtube/files")) return Response.json({ items: [] });
      if (url.endsWith("/youtube/jobs")) return Response.json({ items: [] });
      if (url.endsWith("/youtube/playlist/preview"))
        return Response.json({
          source_url:
            "https://www.youtube.com/playlist?list=PLohGMC3h97JT6mdbfBiriHz4aVx6XdZUF",
          title: "X or GO Full Episodes",
          channel: "Vaff angool",
          truncated: false,
          needs_show: false,
          show: {
            id: 1908,
            title: "X or Go",
            year: 2024,
            tmdb_id: 1,
            score: 1,
          },
          candidates: [],
          season_number: 1,
          seasons: [{ number: 1, episode_count: 5, loaded: true }],
          entries: [
            {
              video_id: "gA3TyIxnMKY",
              title: "[X or GO] Ep.1 (Full Ver.) (ENG/JPN)",
              channel: "Vaff angool",
              duration_seconds: 2876,
              thumbnail: "",
              season_number: 1,
              episode_number: 1,
              episode_id: 14100,
              episode_title: "Episode 1",
              status: "missing",
              note: "",
              selected_default: true,
              duplicate: false,
            },
            {
              video_id: "ogaqMkYZGBc",
              title: "[X or GO] Ep.2 (Full Ver.) (ENG/JPN)",
              channel: "Vaff angool",
              duration_seconds: 3077,
              thumbnail: "",
              season_number: 1,
              episode_number: 2,
              episode_id: 14101,
              episode_title: "Episode 2",
              status: "missing",
              note: "",
              selected_default: true,
              duplicate: false,
            },
          ],
        });
      if (url.endsWith("/youtube/playlist/queue"))
        return Response.json(
          {
            items: [
              {
                video_id: "gA3TyIxnMKY",
                episode_id: 14100,
                job_id: "job1",
                state: "queued",
                message: "Download queued.",
              },
              {
                video_id: "ogaqMkYZGBc",
                episode_id: 14101,
                job_id: "job2",
                state: "queued",
                message: "Download queued.",
              },
            ],
          },
          { status: 201 },
        );
      return Response.json(
        { error: { message: "Unexpected request" } },
        { status: 404 },
      );
    });
    vi.stubGlobal("fetch", fetch);
    window.localStorage.setItem(
      "reelscarey-notice:work-in-progress:youtube",
      "hidden",
    );
    render(
      <Theme>
        <MemoryRouter>
          <YouTube />
        </MemoryRouter>
      </Theme>,
    );
    await userEvent.click(
      screen.getByRole("tab", { name: "Playlist to series" }),
    );
    await userEvent.type(
      screen.getByLabelText("Playlist URL"),
      "https://www.youtube.com/playlist?list=PLohGMC3h97JT6mdbfBiriHz4aVx6XdZUF",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Load playlist" }),
    );
    expect(await screen.findByText("X or Go")).toBeVisible();
    expect(
      screen.getByRole("list", { name: "Playlist episodes" }),
    ).toHaveTextContent("S01E01");
    expect(screen.getByText("2 selected")).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Download 2 selected" }),
    );
    expect(await screen.findByText("2 of 2 queued.")).toBeVisible();
    const request = fetch.mock.calls.find(([url]) =>
      url.endsWith("/youtube/playlist/queue"),
    );
    expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({
      show_id: 1908,
      season_number: 1,
      entries: ["gA3TyIxnMKY", "ogaqMkYZGBc"],
      replace_existing: false,
      keep_source: false,
    });
  });
});
