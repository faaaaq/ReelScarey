import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { HistoryTray } from "../components/HistoryTray";

describe("recent activity tray", () => {
  it("loads recent download status only when opened and links to the job", async () => {
    const fetch = vi.fn(async () =>
      Response.json({
        items: [
          {
            id: 1,
            object_id: "abc",
            kind: "download",
            title: "Fixture movie",
            state: "completed",
            occurred_at: 1700000000,
          },
        ],
        next: null,
      }),
    );
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <MemoryRouter>
          <HistoryTray />
        </MemoryRouter>
      </Theme>,
    );
    expect(fetch).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", { name: "Recent activity" }),
    );
    expect(await screen.findByText("Download · completed")).toBeVisible();
    expect(screen.getByRole("link", { name: "Fixture movie" })).toHaveAttribute(
      "href",
      "/activity?job=abc",
    );
  });
});
