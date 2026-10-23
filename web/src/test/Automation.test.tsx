import { render, screen, waitFor, within } from "@testing-library/react";
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
  it("lets Torznab wait while saving an opt-in download schedule", async () => {
    const fetch = setup();
    mount();
    expect(await screen.findByText("Torznab can wait.")).toBeVisible();
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
      screen.getByText(/TV imports still need episode-to-file review/),
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
    await screen.findByText("Torznab can wait.");
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
  it("saves an item's profile and movie monitoring with its revision", async () => {
    let body: unknown;
    setup((url, o) => {
      if (url.endsWith("/automation/items/1") && o?.method === "PUT") {
        body = JSON.parse(String(o.body));
        return Response.json({ saved: true });
      }
      return undefined;
    });
    mount();
    const save = await screen.findByRole("button", {
      name: "Save Fixture Movie",
    });
    const form = save.closest("form")!;
    await userEvent.selectOptions(within(form).getByRole("combobox"), "1");
    await userEvent.click(within(form).getByLabelText("Monitor"));
    await userEvent.click(save);
    await waitFor(() =>
      expect(body).toEqual({ profile_id: 1, revision: 0, monitored: false }),
    );
  });
});
