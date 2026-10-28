import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { describe, expect, it, vi } from "vitest";
import { EmbyHistory } from "../pages/EmbyHistory";

describe("Emby playback recording", () => {
  it("starts paused, lets an administrator enable recording and labels uncertain gaps", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) =>
      url.endsWith("/history")
        ? Response.json({
            items: [
              {
                id: 1,
                user: "Alex",
                title: "Fixture",
                series: "",
                started_at: 1700000000,
                last_seen: 1700000010,
                state: "uncertain",
                paused: false,
              },
            ],
          })
        : Response.json({
            enabled: init?.method === "PUT",
            revision: init?.method === "PUT" ? 2 : 1,
            last_success: 0,
            status: "waiting",
          }),
    );
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <EmbyHistory />
      </Theme>,
    );
    expect(fetch).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", { name: "Open playback history" }),
    );
    expect(
      await screen.findByText(/End unknown after a monitoring gap/),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Start recording" }),
    );
    expect(
      await screen.findByRole("button", { name: "Pause recording" }),
    ).toBeVisible();
    const call = fetch.mock.calls.find(([, init]) => init?.method === "PUT");
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      enabled: true,
      revision: 1,
    });
  });
});
