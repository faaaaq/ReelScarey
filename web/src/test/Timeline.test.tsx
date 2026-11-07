import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Timeline } from "../pages/Timeline";
describe("event timeline", () => {
  it("loads older chronological records using the returned cursor", async () => {
    const fetch = vi.fn(async (url: string) =>
      url.includes("before=")
        ? Response.json({
            items: [
              {
                id: 99,
                occurred_at: 100,
                kind: "request",
                object_id: "1",
                title: "Older request",
                state: "historical state: approved",
              },
            ],
            next: null,
          })
        : Response.json({
            items: [
              {
                id: 3,
                occurred_at: 200,
                kind: "download",
                object_id: "abcd",
                title: "Recent download",
                state: "completed",
              },
            ],
            next: "200:3",
          }),
    );
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <MemoryRouter>
          <Timeline />
        </MemoryRouter>
      </Theme>,
    );
    expect(await screen.findByText("Recent download")).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Load older events" }),
    );
    expect(await screen.findByText("Older request")).toBeVisible();
    expect(fetch.mock.calls.some(([url]) => url.includes("before=200:3"))).toBe(
      true,
    );
    expect(
      screen.queryByRole("button", { name: "Load older events" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Recent download" }),
    ).toHaveAttribute("href", "/activity?job=abcd");
    const calls = fetch.mock.calls.length;
    await userEvent.click(
      screen.getByRole("button", { name: "Event type: All events" }),
    );
    await userEvent.click(
      await screen.findByRole("menuitemradio", { name: "Request" }),
    );
    expect(await screen.findByText(/1 of 2 loaded events/)).toBeVisible();
    expect(screen.queryByText("Recent download")).not.toBeInTheDocument();
    expect(screen.getByText("Older request")).toBeVisible();
    expect(fetch.mock.calls.length).toBe(calls);
  });
});
