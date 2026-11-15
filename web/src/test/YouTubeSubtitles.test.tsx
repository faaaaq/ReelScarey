import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Downloads as YouTube } from "../pages/youtube/Downloads";

const jobs = [
  {
    id: "job-sub",
    video: { id: "limOfz2srCk", title: "XG DAYS #01", channel: "XG" },
    target_label: "XG Days",
    state: "awaiting_review",
    message:
      "Download verified, but required English subtitles are unavailable. Use Retry subtitles to fetch them; import stays blocked until they are acquired.",
    quality: "best",
    progress: 1,
    downloaded_bytes: 12,
    total_bytes: 12,
    speed_bps: 0,
    eta_seconds: 0,
    created_at: 1,
    updated_at: 1,
    subtitle_languages: [],
    english_subtitles: false,
  },
  {
    id: "job-ok",
    video: { id: "boKAdg0BS-c", title: "XG TAPE", channel: "XG" },
    target_label: "XG Days",
    state: "completed",
    message: "Imported.",
    quality: "best",
    progress: 1,
    downloaded_bytes: 12,
    total_bytes: 12,
    speed_bps: 0,
    eta_seconds: 0,
    created_at: 2,
    updated_at: 2,
    subtitle_languages: ["en"],
    english_subtitles: true,
  },
];

function mount() {
  const fetch = vi.fn(async (url: string, _options?: RequestInit) => {
    if (url.endsWith("/youtube/collections"))
      return Response.json({ items: [] });
    if (url.endsWith("/youtube/jobs")) return Response.json({ items: jobs });
    if (url.endsWith("/youtube/files")) return Response.json({ items: [] });
    if (url.endsWith("/youtube/jobs/job-sub/subtitles"))
      return Response.json({ status: "retrying" });
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
  return fetch;
}

describe("YouTube English subtitle requirement", () => {
  it(
    "marks the missing-English job and offers a subtitle-only retry",
    { timeout: 20000 },
    async () => {
      const fetch = mount();
      try {
        await screen.findByText("XG DAYS #01", {}, { timeout: 2000 });
      } catch {
        // eslint-disable-next-line no-console
        console.log(document.body.innerHTML.slice(0, 3000));
        throw new Error("jobs never rendered");
      }
      const missing = (await screen.findByText("XG DAYS #01")).closest("li");
      expect(missing).not.toBeNull();
      expect(
        within(missing as HTMLElement).getByText("English subs missing"),
      ).toBeVisible();
      const ok = (await screen.findByText("XG TAPE")).closest("li");
      expect(
        within(ok as HTMLElement).getByText(/English subs · en/),
      ).toBeVisible();
      expect(
        within(ok as HTMLElement).queryByRole("button", {
          name: "Retry subtitles",
        }),
      ).not.toBeInTheDocument();
      await userEvent.click(
        within(missing as HTMLElement).getByRole("button", {
          name: "Retry subtitles",
        }),
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
    },
  );
});
