import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import {
  MediaWorkBanner,
  activePlanDetail,
} from "../components/MediaWorkBanner";
import { setCSRFToken } from "../api";

function mount() {
  render(
    <Theme>
      <MemoryRouter>
        <MediaWorkBanner />
      </MemoryRouter>
    </Theme>,
  );
}

describe("media work banner", () => {
  it("names the running job and stops it through the reviewed cancel route", async () => {
    const user = userEvent.setup();
    const calls: string[] = [];
    let running = true;
    const fetch = vi.fn(async (url: string) => {
      if (url.endsWith("/organization/active"))
        return Response.json({
          active: running,
          plan: running
            ? {
                id: "cleanup-1",
                state: "running",
                kind: "track_cleanup",
                processed: 2,
                total: 5,
                changed: 2,
                skipped: 1,
                dropped: 3,
                stoppable: true,
              }
            : undefined,
          title_queue: { paused: false, pending: 0 },
        });
      if (url.endsWith("/organization/plans/cleanup-1/cancel")) {
        calls.push(url);
        running = false;
        return Response.json({});
      }
      return undefined as unknown as Response;
    });
    vi.stubGlobal("fetch", fetch);
    setCSRFToken("test-csrf");
    mount();
    expect(
      await screen.findByText(
        "Removing media tracks · 2 of 5 videos · 3 tracks removed · 1 unresolved skip",
      ),
    ).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Stop Removing media tracks" }),
    );
    await waitFor(() =>
      expect(calls).toEqual(["/api/v1/organization/plans/cleanup-1/cancel"]),
    );
  });

  it("pauses and stops the automatic title-update batch", async () => {
    const user = userEvent.setup();
    const calls: { url: string; method: string; body: string }[] = [];
    let queue = { paused: false, pending: 4 };
    const fetch = vi.fn(async (url: string, o?: RequestInit) => {
      if (url.endsWith("/organization/active"))
        return Response.json({ active: false, title_queue: queue });
      if (url.endsWith("/organization/title-queue") && o?.method === "PUT") {
        calls.push({ url, method: "PUT", body: String(o.body) });
        queue = {
          paused: JSON.parse(String(o.body)).paused,
          pending: queue.pending,
        };
        return Response.json(queue);
      }
      if (url.endsWith("/organization/title-queue/discard")) {
        calls.push({ url, method: "POST", body: String(o?.body ?? "") });
        queue = { paused: true, pending: 0 };
        return Response.json(queue);
      }
      return undefined as unknown as Response;
    });
    vi.stubGlobal("fetch", fetch);
    setCSRFToken("test-csrf");
    mount();
    expect(await screen.findByText("4 title updates waiting")).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Pause title updates" }),
    );
    await waitFor(() =>
      expect(calls[0]).toEqual({
        url: "/api/v1/organization/title-queue",
        method: "PUT",
        body: JSON.stringify({ paused: true }),
      }),
    );
    // Stopping the batch requires an explicit confirmation.
    await user.click(await screen.findByRole("button", { name: "Stop batch" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText(/No media files are changed/),
    ).toBeVisible();
    await user.click(
      within(dialog).getByRole("button", { name: "Stop batch" }),
    );
    await waitFor(() =>
      expect(
        calls.some(
          (c) =>
            c.url === "/api/v1/organization/title-queue/discard" &&
            c.method === "POST",
        ),
      ).toBe(true),
    );
  });

  it("offers to resume a paused title queue even with nothing pending", async () => {
    const user = userEvent.setup();
    const calls: string[] = [];
    let paused = true;
    const fetch = vi.fn(async (url: string, o?: RequestInit) => {
      if (url.endsWith("/organization/active"))
        return Response.json({
          active: false,
          title_queue: { paused, pending: 0 },
        });
      if (url.endsWith("/organization/title-queue") && o?.method === "PUT") {
        calls.push(String(o.body));
        paused = JSON.parse(String(o.body)).paused;
        return Response.json({ paused, pending: 0 });
      }
      return undefined as unknown as Response;
    });
    vi.stubGlobal("fetch", fetch);
    setCSRFToken("test-csrf");
    mount();
    expect(
      await screen.findByText("0 title updates waiting · queue paused"),
    ).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Resume title updates" }),
    );
    await waitFor(() =>
      expect(calls).toEqual([JSON.stringify({ paused: false })]),
    );
  });
});

it("reports disabled tracks without calling them removed", () => {
  expect(
    activePlanDetail({
      id: "cleanup",
      state: "running",
      kind: "track_cleanup",
      action: "disable",
      processed: 23,
      total: 1592,
      changed: 1,
      skipped: 8,
      dropped: 0,
      disabled: 2,
    }),
  ).toContain("2 tracks hidden");
});
