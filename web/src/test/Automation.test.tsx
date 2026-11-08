import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import App from "../App";
import type { Profile, Schedule } from "../pages/Automation";

const profile: Profile = {
  id: 1,
  name: "HD balanced",
  qualities: [720, 1080],
  cutoff: 1080,
  language: "any",
  min_seeders: 1,
  max_size_gib: 40,
  required_terms: "",
  rejected_terms: "",
  revision: 1,
};
const initial: Schedule = {
  enabled: false,
  mode: "search",
  interval_minutes: 360,
  batch_size: 10,
  default_profile_id: 1,
  revision: 1,
  next_run: 0,
  requested: false,
  state: "paused",
  message: "Paused",
  indexer_connected: false,
  client_connected: false,
};
const rss = {
  enabled: true,
  interval_minutes: 15,
  revision: 1,
  next_run: 0,
  requested: false,
  last_checked: 0,
  checked: 0,
  queued: 0,
  state: "scheduled",
  message: "Waiting for the next release check.",
  indexer_connected: false,
  client_connected: false,
  last_run: null,
  results: [],
};
function mount() {
  render(
    <Theme>
      <MemoryRouter initialEntries={["/automation"]}>
        <App />
      </MemoryRouter>
    </Theme>,
  );
}
function setup(
  extra?: (url: string, options?: RequestInit) => Response | undefined,
) {
  let current = initial;
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const custom = extra?.(url, options);
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
    if (url.endsWith("/automation")) {
      if (options?.method === "PUT") {
        current = {
          ...current,
          ...JSON.parse(String(options.body)),
          revision: 2,
        };
      }
      return Response.json(current);
    }
    if (url.endsWith("/automation/rss")) {
      if (options?.method === "PUT") {
        return Response.json({
          ...rss,
          ...JSON.parse(String(options.body)),
          revision: 2,
        });
      }
      return Response.json(rss);
    }
    if (url.endsWith("/quality-profiles"))
      return Response.json({ items: [profile] });
    if (url.includes("/automation/items?page="))
      return Response.json({
        items: [
          {
            media_id: 1,
            title: "Fixture Movie",
            kind: "movie",
            profile_id: null,
            revision: 0,
            monitored: true,
          },
        ],
        page: 1,
        total: 1,
        limit: 24,
      });
    if (url.endsWith("/automation/runs")) return Response.json({ items: [] });
    return Response.json(
      { error: { message: "Unexpected test request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("automation", () => {
  it("saves ordered source and codec preferences with Usenet retention limits", async () => {
    let sent: unknown;
    setup((url, o) => {
      if (url.endsWith("/quality-profiles/1") && o?.method === "PUT") {
        sent = JSON.parse(String(o.body));
        return Response.json({ ...profile, revision: 2 });
      }
      return undefined;
    });
    mount();
    await screen.findByText("Searches are waiting for an indexer.");
    await userEvent.type(
      screen.getByLabelText("Preferred sources, in order"),
      "remux,webdl",
    );
    await userEvent.type(
      screen.getByLabelText("Preferred codecs, in order"),
      "h265,h264",
    );
    await userEvent.type(
      screen.getByLabelText("Preferred release terms"),
      "proper",
    );
    await userEvent.clear(
      screen.getByLabelText("Usenet retention (days; 0 disables)"),
    );
    await userEvent.type(
      screen.getByLabelText("Usenet retention (days; 0 disables)"),
      "3000",
    );
    await userEvent.click(screen.getByRole("button", { name: "Save profile" }));
    expect(sent).toMatchObject({
      scoring: {
        sources: ["remux", "webdl"],
        codecs: ["h265", "h264"],
        preferred_terms: "proper",
        max_usenet_age_days: 3000,
        min_usenet_age_minutes: 0,
      },
    });
  });
  it("lets Torznab wait while saving an opt-in download schedule", async () => {
    const fetch = setup();
    mount();
    expect(
      await screen.findByText("Searches are waiting for an indexer."),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Search now" })).toBeDisabled();
    expect(
      screen.getByLabelText("Enable scheduled searches"),
    ).not.toBeChecked();
    await userEvent.click(screen.getByLabelText("Enable scheduled searches"));
    await userEvent.selectOptions(
      screen.getByLabelText("When a release matches"),
      "download",
    );
    expect(
      screen.getByText(/single-episode TV download imports automatically/),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Save automation" }),
    );
    expect(
      await screen.findByText("Automation preferences saved."),
    ).toBeVisible();
    const call = fetch.mock.calls.find(([, o]) => o?.method === "PUT");
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      enabled: true,
      mode: "download",
      interval_minutes: 360,
      batch_size: 10,
      default_profile_id: 1,
      revision: 1,
    });
    expect(new Headers(call?.[1]?.headers).get("X-CSRF-Token")).toBe(
      "test-csrf",
    );
  });
  it("keeps profile choices after a stale save and sends explicit language and cutoff rules", async () => {
    let sent: Profile | undefined;
    setup((url, o) => {
      if (url.endsWith("/quality-profiles/1") && o?.method === "PUT") {
        sent = JSON.parse(String(o.body));
        return Response.json(
          { error: { message: "Profile changed. Refresh before saving." } },
          { status: 409 },
        );
      }
      return undefined;
    });
    mount();
    await screen.findByText("Searches are waiting for an indexer.");
    await userEvent.selectOptions(
      screen.getByLabelText("Release language"),
      "japanese",
    );
    await userEvent.type(screen.getByLabelText("Excluded terms"), "av1");
    await userEvent.click(screen.getByRole("button", { name: "Save profile" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Profile changed",
    );
    expect(screen.getByLabelText("Release language")).toHaveValue("japanese");
    expect(sent).toMatchObject({
      id: 1,
      revision: 1,
      language: "japanese",
      cutoff: 1080,
      rejected_terms: "av1",
    });
  });
  it("explains rejected releases and does not silently enable downloads for one-off searches", async () => {
    const fetch = setup((url, o) => {
      if (url.endsWith("/automation"))
        return Response.json({ ...initial, indexer_connected: true });
      if (url.endsWith("/automation/run"))
        return Response.json({ requested: true }, { status: 202 });
      if (url.endsWith("/automation/runs"))
        return Response.json({
          items: [
            {
              id: "a".repeat(32),
              started_at: 100,
              state: "completed",
              message: "Search complete",
              mode: "search",
              checked: 1,
              queued: 0,
            },
          ],
        });
      if (url.includes("/automation/runs/"))
        return Response.json({
          items: [
            {
              id: 1,
              target: "Fixture Movie",
              outcome: "no_match",
              message: "No acceptable release.",
              decisions: [
                {
                  release_id: "one",
                  title: "Other.Movie.2020.1080p",
                  quality: 1080,
                  language: "unknown",
                  score: 1,
                  eligible: false,
                  reasons: ["Title does not match."],
                },
              ],
            },
          ],
        });
      if (o?.method === "PUT") throw new Error("Unexpected schedule mutation");
      return undefined;
    });
    mount();
    expect(await screen.findByText("No acceptable release.")).toBeVisible();
    await userEvent.click(screen.getByText("Why these releases?"));
    expect(screen.getByText("Title does not match.")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Search now" }));
    expect(await screen.findByText(/Search requested/)).toBeVisible();
    expect(fetch.mock.calls.some(([, o]) => o?.method === "PUT")).toBe(false);
  });
  it("shows release detection enabled every 15 minutes by default", async () => {
    setup();
    mount();
    await screen.findByText("Searches are waiting for an indexer.");
    expect(screen.getByText("Release detection")).toBeVisible();
    expect(
      await screen.findByLabelText("Check indexer feeds automatically"),
    ).toBeChecked();
    expect(await screen.findByLabelText("Check every (minutes)")).toHaveValue(
      15,
    );
    expect(
      await screen.findByRole("button", { name: "Check now" }),
    ).toBeDisabled();
  });
  it("keeps per-title profiles out of Automation and points to Bulk profiles", async () => {
    setup();
    mount();
    await screen.findByText("Searches are waiting for an indexer.");
    expect(screen.queryByText("Library preferences")).not.toBeInTheDocument();
    expect(screen.queryByText("Fixture Movie")).not.toBeInTheDocument();
    const main = within(screen.getByRole("main"));
    expect(main.getByText(/Bulk profiles/)).toBeVisible();
    expect(main.getByRole("link", { name: "Movies" })).toHaveAttribute(
      "href",
      "/movies",
    );
    expect(main.getByRole("link", { name: "TV Shows" })).toHaveAttribute(
      "href",
      "/series",
    );
    expect(main.getByRole("link", { name: "Anime" })).toHaveAttribute(
      "href",
      "/anime",
    );
  });
});
