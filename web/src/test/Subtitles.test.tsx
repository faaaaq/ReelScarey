import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import App from "../App";
import type {
  SubtitlePreferences,
  SubtitleFile,
  SubtitleProvider,
} from "../pages/Subtitles";

const initial: SubtitlePreferences = {
  enabled: false,
  tracks: [
    { language: "en", variant: "full" },
    { language: "en", variant: "signs" },
    { language: "en", variant: "forced" },
  ],
  hearing_impaired: "any",
  match_policy: "hash_or_release",
  interval_minutes: 360,
  only_monitored: true,
  revision: 1,
};
const provider: SubtitleProvider = {
  configured: false,
  username: "",
  user_agent: "ReelScarey v0.7.0",
  state: "not_connected",
  message: "Connect OpenSubtitles",
  cooldown_until: 0,
  remaining: null,
};
const file: SubtitleFile = {
  id: 1,
  media_id: 2,
  kind: "movie",
  title: "Generated fixture movie",
  path: "Movies/Fixture/Fixture.mkv",
  episodes: "",
  tracks: [],
  job: {
    state: "needs_review",
    mode: "review",
    requested: false,
    message: "Review timing before downloading.",
    next_run: 0,
    updated_at: 1,
  },
  candidates: [
    {
      id: "opaque-candidate",
      file_id: 42,
      language: "en",
      variant: "forced",
      release: "Other cut",
      file_name: "fixture.en.forced.ass",
      format: "ass",
      hearing_impaired: false,
      hash_match: false,
      trusted: false,
      automatic: false,
      can_download: true,
      score: 0,
      reasons: ["The title matches; timing needs review."],
    },
  ],
};
function jobFile(
  id: number,
  title: string,
  state: string,
  tracks: SubtitleFile["tracks"] = [],
): SubtitleFile {
  return {
    id,
    media_id: id + 10,
    kind: "movie",
    title,
    path: `Movies/${title.replaceAll(" ", "")}/${title}.mkv`,
    episodes: "",
    tracks,
    job: {
      state,
      mode: "automatic",
      requested: false,
      message: `Job ${state}.`,
      next_run: 0,
      updated_at: 1,
    },
  };
}
function setup(extra?: (url: string, o?: RequestInit) => Response | undefined) {
  let current = initial;
  const fetch = vi.fn(async (url: string, o?: RequestInit) => {
    const custom = extra?.(url, o);
    if (custom) return custom;
    if (url.endsWith("/auth/session"))
      return Response.json({
        authenticated: true,
        setup_required: false,
        session: {
          username: "test",
          csrf_token: "test-csrf",
          expires_at: 9999999999,
        },
      });
    if (url.endsWith("/subtitles/preferences")) {
      if (o?.method === "PUT")
        current = { ...JSON.parse(String(o.body)), revision: 2 };
      return Response.json({
        preferences: current,
        languages: [
          { code: "en", name: "English" },
          { code: "es", name: "Spanish" },
          { code: "ja", name: "Japanese" },
        ],
      });
    }
    if (url.endsWith("/providers/opensubtitles"))
      return Response.json(provider);
    if (url.includes("/subtitles/files?page="))
      return Response.json({ items: [], page: 1, total: 0, limit: 24 });
    return Response.json(
      { error: { message: "Unexpected fixture request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
function mount() {
  // The first-visit overlay has its own test; these tests cover the page.
  window.localStorage.setItem(
    "reelscarey-notice:work-in-progress:subtitles",
    "hidden",
  );
  render(
    <Theme>
      <MemoryRouter initialEntries={["/subtitles"]}>
        <App />
      </MemoryRouter>
    </Theme>,
  );
}
describe("subtitles", () => {
  it("switches subtitle providers without reusing hidden credentials", async () => {
    const fetch = setup();
    mount();
    await screen.findByRole("button", { name: "Save & test OpenSubtitles" });
    await userEvent.selectOptions(screen.getByLabelText("Provider"), "subdl");
    expect(
      screen.queryByLabelText("OpenSubtitles password"),
    ).not.toBeInTheDocument();
    await userEvent.type(
      screen.getByLabelText("Application API key"),
      "subdl-fixture-key",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save & test SubDL" }),
    );
    await screen.findByText("SubDL connection verified and saved.");
    const sent = fetch.mock.calls.find(([, o]) => o?.method === "PUT")!;
    expect(JSON.parse(String(sent[1]?.body))).toMatchObject({
      provider: "subdl",
      api_key: "subdl-fixture-key",
    });
    expect(JSON.parse(String(sent[1]?.body))).not.toHaveProperty("password");
  });
  it("saves original subtitle format explicitly while retaining SRT as the default", async () => {
    const fetch = setup();
    mount();
    await screen.findByRole("button", { name: "Save languages & tracks" });
    expect(
      screen.getByRole("combobox", { name: /Download format/ }),
    ).toHaveValue("srt");
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: /Download format/ }),
      "original",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save languages & tracks" }),
    );
    await screen.findByText("Subtitle preferences saved.");
    const call = fetch.mock.calls.find(([, o]) => o?.method === "PUT");
    expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({
      download_format: "original",
      revision: 1,
      enabled: false,
    });
  });
  it("starts with separate English full, signs and forced tracks and saves without Torznab", async () => {
    const fetch = setup();
    mount();
    await screen.findByRole("button", { name: "Save matching rules" });
    for (const name of [
      "English Full subtitles",
      "English Signs / songs",
      "English Forced",
    ])
      expect(screen.getByLabelText(name)).toBeChecked();
    expect(
      screen.getByLabelText("Automatically download matching subtitles"),
    ).not.toBeChecked();
    expect(
      await screen.findByText("Subtitles follow your imported media."),
    ).toBeVisible();
    await userEvent.click(
      screen.getByLabelText("Automatically download matching subtitles"),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save matching rules" }),
    );
    await screen.findByText("Subtitle preferences saved.");
    const call = fetch.mock.calls.find(([, o]) => o?.method === "PUT");
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      ...initial,
      enabled: true,
    });
    expect(new Headers(call?.[1]?.headers).get("X-CSRF-Token")).toBe(
      "test-csrf",
    );
  });
  it("keeps draft language choices when a revision save fails", async () => {
    setup((url, o) =>
      url.endsWith("/subtitles/preferences") && o?.method === "PUT"
        ? Response.json(
            { error: { message: "Preferences changed; reload." } },
            { status: 409 },
          )
        : undefined,
    );
    mount();
    await screen.findByRole("button", { name: "Save languages & tracks" });
    await userEvent.click(screen.getByLabelText("English Forced"));
    await userEvent.click(
      screen.getByRole("button", { name: "Save languages & tracks" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Preferences changed; reload.",
    );
    expect(screen.getByLabelText("English Forced")).not.toBeChecked();
    expect(screen.getByLabelText("English Signs / songs")).toBeChecked();
  });
  it("never fills saved secrets and sends only supplied replacements", async () => {
    const fetch = setup((url) =>
      url.endsWith("/providers/opensubtitles")
        ? Response.json({
            ...provider,
            configured: true,
            username: "saved-account",
            state: "ready",
          })
        : undefined,
    );
    mount();
    await screen.findByDisplayValue("saved-account");
    expect(screen.getByLabelText("OpenSubtitles password")).toHaveValue("");
    expect(screen.getByLabelText("Application API key")).toHaveValue("");
    await userEvent.click(
      screen.getByRole("button", { name: "Save & test OpenSubtitles" }),
    );
    await screen.findByText("OpenSubtitles connection verified and saved.");
    const call = fetch.mock.calls.find(([, o]) => o?.method === "PUT");
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      provider: "opensubtitles",
      username: "saved-account",
      user_agent: "ReelScarey v0.7.0",
    });
  });
  it("requires timing review, sends an opaque choice and resets approval after an error", async () => {
    const fetch = setup((url, o) => {
      if (url.includes("/subtitles/files?page="))
        return Response.json({ items: [file], page: 1, total: 1, limit: 24 });
      if (url.endsWith("/subtitles/files/1")) return Response.json(file);
      if (
        url.endsWith("/subtitles/candidates/opaque-candidate/download") &&
        o?.method === "POST"
      )
        return Response.json(
          { error: { message: "Selection expired. Search again." } },
          { status: 409 },
        );
      return undefined;
    });
    mount();
    await screen.findByText("Generated fixture movie");
    await userEvent.click(
      screen.getByRole("button", { name: "Review matches" }),
    );
    const button = await screen.findByRole("button", {
      name: "Download subtitle",
    });
    expect(screen.getByText(/Output: ASS/)).toBeVisible();
    expect(button).toBeDisabled();
    await userEvent.click(
      screen.getByLabelText(
        "I reviewed the language, track type and timing match",
      ),
    );
    await userEvent.click(button);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Selection expired",
    );
    expect(button).toBeDisabled();
    const call = fetch.mock.calls.find(([url]) => url.includes("/candidates/"));
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({ reviewed: true });
    expect(new Headers(call?.[1]?.headers).get("X-CSRF-Token")).toBe(
      "test-csrf",
    );
  });
  it("queues a search for an imported file without inventing provider results", async () => {
    const fetch = setup((url, o) => {
      if (url.includes("/subtitles/files?page="))
        return Response.json({ items: [file], page: 1, total: 1, limit: 24 });
      if (url.endsWith("/subtitles/files/1/search") && o?.method === "POST")
        return Response.json({ requested: true }, { status: 202 });
      if (url.endsWith("/subtitles/files/1"))
        return Response.json({ ...file, candidates: [] });
      return undefined;
    });
    mount();
    await screen.findByText("Generated fixture movie");
    await userEvent.click(
      screen.getByRole("button", {
        name: "Search subtitles for Generated fixture movie",
      }),
    );
    expect(await screen.findByText(/No saved matches/)).toBeVisible();
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, o]) => url.endsWith("/files/1/search") && o?.body === "{}",
        ),
      ).toBe(true),
    );
    expect(
      screen.queryByRole("button", { name: "Download subtitle" }),
    ).not.toBeInTheDocument();
  });
  it("orders the provider, language, matching and media sections", async () => {
    setup();
    mount();
    await screen.findByRole("button", { name: "Save & test OpenSubtitles" });
    const headings = [
      "Provider",
      "Languages & tracks",
      "Automatic matching",
      "Media files",
    ].map((name) => screen.getByRole("heading", { level: 2, name }));
    for (let index = 1; index < headings.length; index += 1)
      expect(
        headings[index - 1].compareDocumentPosition(headings[index]) &
          Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy();
    expect(screen.getByText("OpenSubtitles not connected")).toBeVisible();
  });
  it("labels activity states and reports a provider cooldown", async () => {
    const files = [
      jobFile(1, "Queued movie", "queued", [
        {
          language: "en",
          variant: "full",
          source: "downloaded",
          path: "Movies/Queued/Queued.en.srt",
          state: "available",
          message: "",
          updated_at: 1,
        },
      ]),
      jobFile(2, "Downloading movie", "running"),
      jobFile(3, "Review movie", "needs_review"),
      jobFile(4, "Failed movie", "blocked"),
      jobFile(5, "Done movie", "completed"),
    ];
    setup((url) =>
      url.endsWith("/providers/opensubtitles")
        ? Response.json({
            ...provider,
            provider: "opensubtitles",
            configured: true,
            username: "saved-account",
            state: "provider_quota",
            message: "Daily download limit reached.",
            cooldown_until: Math.floor(Date.now() / 1000) + 3600,
            remaining: 0,
          })
        : url.includes("/subtitles/files?page=")
          ? Response.json({
              items: files,
              page: 1,
              total: files.length,
              limit: 24,
            })
          : undefined,
    );
    mount();
    const table = await screen.findByRole("table", {
      name: "Subtitle media files",
    });
    for (const label of [
      "Queued",
      "Downloading",
      "Awaiting review",
      "Failed",
      "Done",
    ])
      expect(within(table).getByText(label)).toBeVisible();
    expect(within(table).getByText("English · Full subtitles")).toBeVisible();
    expect(screen.getByText("Provider cooldown")).toBeVisible();
    expect(screen.getByText(/Retry after/)).toBeVisible();
  });
});
