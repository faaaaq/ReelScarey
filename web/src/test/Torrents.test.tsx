import { render, screen } from "@testing-library/react";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Torrents } from "../pages/Torrents";

const torrent = {
  hash: "0123456789abcdef0123456789abcdef01234567",
  name: "Fixture.Release.1080p",
  state: "downloading",
  save_path: "/downloads/builtin/ReelScarey/manual",
  progress: 0.25,
  amount_left: 75,
  total_size: 100,
  completed: 25,
  downloaded: 25,
  uploaded: 0,
  dlspeed: 2048,
  upspeed: 0,
  eta: 90,
  ratio: 0,
  num_seeds: 4,
  num_leechs: 1,
  added_on: 1,
  completion_on: 0,
  tags: "reelscarey-abcdef",
  category: "",
  content_path: "",
};

function mount(payload: unknown) {
  const fetch = vi.fn(async (url: string) => {
    if (url.endsWith("/torrents")) return Response.json(payload);
    return Response.json(
      { error: { message: "Unexpected request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter>
        <Torrents />
      </MemoryRouter>
    </Theme>,
  );
  return fetch;
}

describe("Torrents", () => {
  it("lists built-in client torrents with transfer rates and controls", async () => {
    mount({
      available: true,
      state: "running",
      message: "",
      transfer: {
        dl_info_speed: 2048,
        up_info_speed: 0,
        dl_info_data: 4096,
        up_info_data: 0,
        connection_status: "connected",
      },
      items: [torrent],
    });
    expect(await screen.findByText("Fixture.Release.1080p")).toBeVisible();
    expect(screen.getByText(/ReelScarey/)).toBeVisible();
    expect(screen.getByRole("button", { name: "Pause" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Remove" })).toBeVisible();
    expect(screen.getByLabelText("Transfer rates")).toHaveTextContent(
      "2.0 KiB/s",
    );
  });

  it("explains when the built-in client is disabled", async () => {
    mount({
      available: false,
      state: "disabled",
      message: "The built-in qBittorrent client is not enabled.",
      transfer: {},
      items: [],
    });
    expect(
      await screen.findByText("The built-in client is not enabled"),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Open download clients" }),
    ).toHaveAttribute("href", "/download-clients");
  });
});
