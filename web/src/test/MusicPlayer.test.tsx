import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  MusicPlayerBar,
  MusicPlayerProvider,
  useMusicPlayer,
} from "../musicPlayer";

function Harness() {
  const player = useMusicPlayer();
  return (
    <button
      type="button"
      onClick={() =>
        player.play(
          {
            fileID: 7,
            title: "Airbag",
            artist: "Radiohead",
            album: "OK Computer",
            albumID: 2,
          },
          [
            {
              fileID: 7,
              title: "Airbag",
              artist: "Radiohead",
              album: "OK Computer",
              albumID: 2,
            },
          ],
        )
      }
    >
      Start
    </button>
  );
}

describe("music player", () => {
  beforeEach(() => {
    vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  });

  it("plays a registered file and exposes seek and close controls", async () => {
    render(
      <MusicPlayerProvider>
        <MemoryRouter>
          <Harness />
          <MusicPlayerBar />
        </MemoryRouter>
      </MusicPlayerProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "Start" }));
    expect(await screen.findByText("Airbag")).toBeVisible();
    expect(screen.getByRole("region", { name: "Music player" })).toBeVisible();
    expect(screen.getByRole("slider", { name: "Seek" })).toBeVisible();
    expect(screen.getByRole("slider", { name: "Volume" })).toBeVisible();
    const audio = document.querySelector("audio");
    // jsdom's canPlayType reports no codec support, so the player negotiates
    // the compatible MP3 fallback instead of the server's Ogg/Opus default.
    expect(audio?.getAttribute("src")).toBe(
      "/api/v1/library/music/files/7/stream?format=mp3&maxBitRate=192",
    );
    await userEvent.click(screen.getByRole("button", { name: "Close player" }));
    await waitFor(() =>
      expect(
        screen.queryByRole("region", { name: "Music player" }),
      ).not.toBeInTheDocument(),
    );
  });

  it("negotiates the stream format by browser capability", async () => {
    // jsdom reports no codec support, so the player must request the MP3
    // fallback instead of the server's Ogg/Opus default. A browser that
    // reports Ogg/Opus support keeps the quality-driven default URL.
    const renderAndPlay = async () => {
      render(
        <MusicPlayerProvider>
          <MemoryRouter>
            <Harness />
            <MusicPlayerBar />
          </MemoryRouter>
        </MusicPlayerProvider>,
      );
      await userEvent.click(screen.getByRole("button", { name: "Start" }));
      return document.querySelector("audio");
    };

    const noOpus = vi
      .spyOn(HTMLMediaElement.prototype, "canPlayType")
      .mockReturnValue("");
    const audio = await renderAndPlay();
    expect(audio?.getAttribute("src")).toContain("format=mp3&maxBitRate=192");
    noOpus.mockRestore();
    cleanup();

    const withOpus = vi
      .spyOn(HTMLMediaElement.prototype, "canPlayType")
      .mockReturnValue("probably");
    const audio2 = await renderAndPlay();
    expect(audio2?.getAttribute("src")).toBe(
      "/api/v1/library/music/files/7/stream",
    );
    withOpus.mockRestore();
  });
});
