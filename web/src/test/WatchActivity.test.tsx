import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { describe, expect, it, vi } from "vitest";
import { WatchActivity } from "../pages/WatchActivity";

describe("Emby watch activity", () => {
  it("saves a private connection and displays Emby's latest playback", async () => {
    let configured = false;
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/emby/activity"))
        return Response.json({
          items: [
            {
              user: "Alex",
              title: "Pilot",
              series: "Example",
              kind: "Episode",
              season: 1,
              episode: 2,
              last_played: "2026-01-01T00:00:00Z",
              play_count: 1,
              played: false,
              progress: 25,
            },
          ],
          truncated: false,
          checked_at: "2026-01-01T00:00:00Z",
        });
      if (init?.method === "PUT") {
        configured = true;
        return Response.json({ saved: true });
      }
      return Response.json({
        url: configured ? "http://emby:8096" : "",
        configured,
      });
    });
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <WatchActivity />
      </Theme>,
    );
    await screen.findByText("Emby connection");
    await userEvent.type(
      screen.getByLabelText("Server URL"),
      "http://emby:8096",
    );
    await userEvent.type(screen.getByLabelText("API key"), "private-key");
    await userEvent.click(
      screen.getByRole("button", { name: "Save connection" }),
    );
    const refresh = await screen.findByRole("button", {
      name: "Refresh activity",
    });
    await vi.waitFor(() => expect(refresh).toBeEnabled());
    expect(screen.getByLabelText("API key")).toHaveValue("");
    await userEvent.click(refresh);
    expect(await screen.findByText("Example — Pilot")).toBeVisible();
    expect(screen.getByText("In progress (25%)")).toBeVisible();
    expect(
      screen.getByText(/snapshot, not a complete play-by-play history/),
    ).toBeVisible();
  });
});
