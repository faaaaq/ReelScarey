import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { setCSRFToken } from "../api";
import { MetadataRefresh } from "../pages/MetadataRefresh";
import { ShowSubtitlePreferences } from "../pages/ShowSubtitlePreferences";
import { SubtitleEdits } from "../pages/SubtitleEdits";
import { YouTubeBulk } from "../pages/YouTubeBulk";
import { YouTubeSubscriptions } from "../pages/YouTubeSubscriptions";
import { BulkMatching } from "../pages/BulkMatching";

function mount(ui: ReactNode) {
  setCSRFToken("seven-csrf");
  render(
    <Theme>
      <MemoryRouter>{ui}</MemoryRouter>
    </Theme>,
  );
}
const schedule = {
  enabled: false,
  interval_hours: 24,
  batch_size: 5,
  only_monitored: true,
  revision: 1,
  requested: false,
  next_run: 0,
  last_run: 0,
  state: "paused",
  message: "Metadata refresh is paused.",
  cursor: 0,
};
const prefs = {
  enabled: false,
  tracks: [{ language: "en", variant: "full" }],
  hearing_impaired: "any",
  match_policy: "hash",
  interval_minutes: 360,
  only_monitored: true,
  revision: 3,
};
const track = {
  language: "en" as const,
  variant: "full" as const,
  path: "Movies/Fixture/Fixture.en.srt",
  source: "existing",
};
const file = {
  id: "a".repeat(32),
  scan_id: "b".repeat(32),
  path: "Movies/Fixture.2020.mkv",
  full_path: "/media/Movies/Fixture.2020.mkv",
  hint: "Fixture",
  size_bytes: 100,
  mtime: 1,
  state: "review",
  media_id: null,
  media_file_id: null,
  message: "Review needed",
};

describe("remaining feature controls", () => {
  it("keeps metadata schedules paused until saved and requests manual refresh explicitly", async () => {
    const fetch = vi.fn(async (_url: string, o?: RequestInit) => {
      if (o?.method === "PUT")
        return Response.json({
          ...schedule,
          ...JSON.parse(String(o.body)),
          revision: 2,
        });
      if (o?.method === "POST")
        return Response.json({ queued: true }, { status: 202 });
      return Response.json(schedule);
    });
    vi.stubGlobal("fetch", fetch);
    mount(<MetadataRefresh />);
    const enabled = await screen.findByLabelText(
      "Enable scheduled metadata refresh",
    );
    expect(enabled).not.toBeChecked();
    expect(fetch.mock.calls.every(([, o]) => !o?.method)).toBe(true);
    await userEvent.click(enabled);
    await userEvent.click(
      screen.getByRole("button", { name: "Save refresh schedule" }),
    );
    const sent = fetch.mock.calls.find(([, o]) => o?.method === "PUT");
    expect(JSON.parse(String(sent?.[1]?.body))).toMatchObject({
      enabled: true,
      interval_hours: 24,
      revision: 1,
    });
    expect(sent?.[1]?.headers).toMatchObject({ "X-CSRF-Token": "seven-csrf" });
    await userEvent.click(screen.getByRole("button", { name: "Refresh now" }));
    expect(fetch.mock.calls.some(([, o]) => o?.method === "POST")).toBe(true);
  });
  it("loads show overrides on demand and saves explicit language tracks with both revisions", async () => {
    const response = {
      inherited: true,
      preferences: prefs,
      revision: 0,
      global_revision: 3,
      languages: [
        { code: "en", name: "English" },
        { code: "ja", name: "Japanese" },
      ],
    };
    const fetch = vi.fn(async () => Response.json(response));
    vi.stubGlobal("fetch", fetch);
    mount(<ShowSubtitlePreferences id={4} />);
    expect(fetch).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", {
        name: "Edit subtitle preferences",
      }),
    );
    await screen.findByText("Using global subtitle preferences");
    await userEvent.selectOptions(
      screen.getByLabelText("Subtitle language"),
      "ja",
    );
    await userEvent.click(screen.getByRole("button", { name: "Add track" }));
    await userEvent.click(
      screen.getByRole("button", { name: "Save show subtitle preferences" }),
    );
    const sent = fetch.mock.calls.find(
      (call) => (call as unknown as [string, RequestInit])[1]?.method === "PUT",
    ) as unknown as [string, RequestInit];
    expect(JSON.parse(String(sent[1].body))).toMatchObject({
      revision: 0,
      global_revision: 3,
      tracks: [
        { language: "en", variant: "full" },
        { language: "ja", variant: "full" },
      ],
    });
  });
  it("requires review and consent before changing a saved subtitle", async () => {
    const review = {
      id: "edit",
      media_file_id: 1,
      ...track,
      action: "offset",
      offset_ms: 1000,
      old_sha256: "a".repeat(64),
      new_sha256: "b".repeat(64),
      destination: track.path,
      previous_path: "history",
      size_bytes: 100,
      cues: 2,
      state: "review",
      phase: "planned",
      message: "Review",
      expires_at: 9999999999,
      created_at: 1,
    };
    const fetch = vi.fn(async (url: string, o?: RequestInit) =>
      url.endsWith("/apply")
        ? Response.json({ queued: true }, { status: 202 })
        : o?.method === "POST"
          ? Response.json(review)
          : Response.json({ items: [] }),
    );
    vi.stubGlobal("fetch", fetch);
    mount(<SubtitleEdits fileID={1} tracks={[track]} reload={vi.fn()} />);
    expect(fetch).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", { name: "Edit or replace a saved subtitle" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review subtitle change" }),
    );
    const apply = await screen.findByRole("button", {
      name: "Apply reviewed subtitle change",
    });
    expect(apply).toBeDisabled();
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/apply"))).toBe(
      false,
    );
    await userEvent.click(
      screen.getByRole("checkbox", { name: /I reviewed this subtitle change/ }),
    );
    await userEvent.click(apply);
    expect(
      await screen.findByText("Reviewed subtitle change queued."),
    ).toBeVisible();
  });
  it("queues the entire explicitly selected and reviewed YouTube batch", async () => {
    const reviews = [
      {
        id: "one",
        video: { id: "aaaaaaaaaaa", title: "Video one" },
        target_label: "Fixtures",
        expires_at: 9999999999,
      },
      {
        id: "two",
        video: { id: "bbbbbbbbbbb", title: "Video two" },
        target_label: "Fixtures",
        expires_at: 9999999999,
      },
    ];
    const fetch = vi.fn(async (url: string) =>
      Response.json(
        url.endsWith("/review") ? { items: reviews } : { items: [] },
      ),
    );
    vi.stubGlobal("fetch", fetch);
    mount(
      <YouTubeBulk
        url="https://www.youtube.com/playlist?list=PLabcdefghijk"
        entries={[
          { id: "aaaaaaaaaaa", title: "Video one" },
          { id: "bbbbbbbbbbb", title: "Video two" },
        ]}
        collectionID={2}
        subtitles={{ languages: [], automatic: false }}
        reload={vi.fn()}
      />,
    );
    await userEvent.click(screen.getByRole("checkbox", { name: "Video one" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Video two" }));
    await userEvent.click(
      screen.getByRole("button", { name: "Review selected videos (2)" }),
    );
    const queue = await screen.findByRole("button", {
      name: "Queue reviewed videos",
    });
    expect(queue).toBeDisabled();
    await userEvent.click(
      screen.getByRole("checkbox", {
        name: "I reviewed every selected video and destination.",
      }),
    );
    await userEvent.click(queue);
    const sent = fetch.mock.calls.find((call) =>
      (call as unknown as [string, RequestInit])[0].endsWith("/queue"),
    ) as unknown as [string, RequestInit];
    expect(JSON.parse(String(sent[1].body))).toEqual({
      review_ids: ["one", "two"],
      reviewed: true,
    });
  });
  it("creates channel subscriptions paused in discovery mode", async () => {
    const fetch = vi.fn(async (url: string, o?: RequestInit) => {
      if (url.endsWith("/collections"))
        return Response.json({ items: [{ id: 2, title: "Fixtures" }] });
      if (o?.method === "POST") return Response.json({ id: "new" });
      return Response.json({ items: [] });
    });
    vi.stubGlobal("fetch", fetch);
    mount(<YouTubeSubscriptions />);
    await screen.findByRole("option", { name: "Fixtures" });
    await userEvent.type(screen.getByLabelText("Name"), "Fixture channel");
    await userEvent.type(
      screen.getByLabelText("Public channel or playlist URL"),
      "https://www.youtube.com/@fixture",
    );
    await userEvent.selectOptions(
      screen.getByLabelText("Custom collection"),
      "2",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Add paused subscription" }),
    );
    const sent = fetch.mock.calls.find(([, o]) => o?.method === "POST");
    expect(JSON.parse(String(sent?.[1]?.body))).toMatchObject({
      enabled: false,
      mode: "review",
      collection_id: 2,
      interval_hours: 24,
    });
  });
  it("does not preselect filename matches and requires batch registration consent", async () => {
    const candidate = {
      media_id: 7,
      tmdb_id: 77,
      title: "Fixture",
      year: 2020,
      source: "catalog",
      score: 120,
      reasons: [],
      episode_ids: [],
    };
    const review = {
      review_id: "review",
      expires_at: 9999999999,
      items: [
        {
          choice: { file_id: file.id, media_id: 7, episode_ids: [] },
          file,
          title: "Fixture",
          year: 2020,
          episodes: [],
        },
      ],
    };
    const fetch = vi.fn(async (url: string) =>
      Response.json(
        url.endsWith("/suggest")
          ? {
              items: [
                { file, candidates: [candidate], message: "Review identity" },
              ],
            }
          : url.endsWith("/review")
            ? review
            : { queued: true },
      ),
    );
    vi.stubGlobal("fetch", fetch);
    mount(
      <BulkMatching
        scanID={file.scan_id}
        kind="movie"
        files={[file]}
        reload={vi.fn()}
        match={vi.fn()}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Match multiple files" }),
    );
    await userEvent.click(screen.getByRole("checkbox", { name: file.path }));
    await userEvent.click(
      screen.getByRole("button", { name: "Find batch suggestions" }),
    );
    const select = await screen.findByLabelText("Canonical title");
    expect(select).toHaveValue("");
    await userEvent.selectOptions(select, "0");
    await userEvent.click(
      screen.getByRole("button", { name: "Review chosen file matches" }),
    );
    const apply = await screen.findByRole("button", {
      name: "Register reviewed batch",
    });
    expect(apply).toBeDisabled();
    await userEvent.click(
      screen.getByRole("checkbox", {
        name: "I reviewed every file, canonical title and episode in this batch.",
      }),
    );
    await userEvent.click(apply);
    expect(
      await screen.findByText(/The reviewed batch is queued/),
    ).toBeVisible();
  });
});
