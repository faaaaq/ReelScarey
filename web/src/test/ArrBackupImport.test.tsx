import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Settings } from "../pages/Settings";

function mount() {
  const calls: { url: string; method: string; body: unknown }[] = [];
  const fetch = vi.fn(async (url: string, init: RequestInit = {}) => {
    calls.push({ url, method: init.method ?? "GET", body: init.body });
    if (url.endsWith("/library/import/backup/preview"))
      return Response.json({
        source: "radarr",
        items: [
          {
            kind: "movie",
            tmdb_id: 11,
            title: "Unreleased film",
            year: 2026,
            monitored: true,
            in_library: false,
          },
        ],
      });
    if (url.endsWith("/library/import/backup/apply"))
      return Response.json({ added: 1, skipped: 0, errors: [] });
    return Response.json({ error: { message: "unexpected" } }, { status: 404 });
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter initialEntries={["/settings/backup-import"]}>
        <Routes>
          <Route path="/settings/:section" element={<Settings />} />
        </Routes>
      </MemoryRouter>
    </Theme>,
  );
  return calls;
}

describe("Sonarr and Radarr backup import", () => {
  it("reviews and adds a backup title", async () => {
    const calls = mount();
    const input = screen.getByLabelText("Sonarr or Radarr backup");
    const file = new File([new Uint8Array([1, 2, 3])], "radarr_backup.zip", {
      type: "application/zip",
    });
    await userEvent.upload(input, file);
    await userEvent.click(
      screen.getByRole("button", { name: "Review backup" }),
    );
    expect(await screen.findByText("Unreleased film")).toBeVisible();
    expect(screen.getByText("Will add")).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Add 1 selected" }),
    );
    expect(await screen.findAllByText(/Added 1 title/)).not.toHaveLength(0);
    expect(
      calls.some(
        (call) =>
          call.url.endsWith("/library/import/backup/preview") &&
          call.method === "POST" &&
          call.body instanceof File,
      ),
    ).toBe(true);
    await waitFor(() =>
      expect(
        calls.some(
          (call) =>
            call.url.endsWith("/library/import/backup/apply") &&
            call.method === "POST",
        ),
      ).toBe(true),
    );
  });
});
