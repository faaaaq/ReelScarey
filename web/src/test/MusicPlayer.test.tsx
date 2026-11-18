import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  MusicPlayerBar,
  MusicPlayerProvider,
  useMusicPlayer,
} from "../musicPlayer";

const QUEUE_TRACKS = [
  {
    fileID: 7,
    title: "Airbag",
    artist: "Radiohead",
    album: "OK Computer",
    albumID: 2,
  },
  {
    fileID: 8,
    title: "Paranoid Android",
    artist: "Radiohead",
    album: "OK Computer",
    albumID: 2,
  },
];

function Harness() {
  const player = useMusicPlayer();
  return (
    <button
      type="button"
      onClick={() => player.play(QUEUE_TRACKS[0], QUEUE_TRACKS)}
    >
      Start
    </button>
  );
}

describe("music player", () => {
  beforeEach(() => {
    window.localStorage.clear();
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

  it("treats a 'maybe' codec report as support and keeps the default stream", async () => {
    // canPlayType returns "", "maybe" or "probably". Only the empty string
    // means the browser reported no support, so "maybe" must keep the
    // quality-driven default URL rather than forcing the MP3 fallback.
    const maybe = vi
      .spyOn(HTMLMediaElement.prototype, "canPlayType")
      .mockReturnValue("maybe");
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
    expect(document.querySelector("audio")?.getAttribute("src")).toBe(
      "/api/v1/library/music/files/7/stream",
    );
    maybe.mockRestore();
  });

  it("reloads an unbuffered live seek with a media-time offset", async () => {
    render(
      <MusicPlayerProvider>
        <MemoryRouter>
          <Harness />
          <MusicPlayerBar />
        </MemoryRouter>
      </MusicPlayerProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "Start" }));
    const audio = document.querySelector("audio") as HTMLAudioElement;
    Object.defineProperty(audio, "duration", {
      configurable: true,
      value: 120,
    });
    Object.defineProperty(audio, "seekable", {
      configurable: true,
      value: { length: 0 },
    });
    fireEvent(audio, new Event("durationchange"));
    fireEvent.change(screen.getByRole("slider", { name: "Seek" }), {
      target: { value: "42" },
    });
    expect(audio.getAttribute("src")).toContain("timeOffset=42");
    expect(audio.getAttribute("src")).toContain("format=mp3");
    expect(screen.getByRole("slider", { name: "Seek" })).toHaveValue("42");
  });

  it("uses native ranges when the requested position is seekable", async () => {
    render(
      <MusicPlayerProvider>
        <MemoryRouter>
          <Harness />
          <MusicPlayerBar />
        </MemoryRouter>
      </MusicPlayerProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "Start" }));
    const audio = document.querySelector("audio") as HTMLAudioElement;
    Object.defineProperty(audio, "duration", {
      configurable: true,
      value: 120,
    });
    Object.defineProperty(audio, "seekable", {
      configurable: true,
      value: { length: 1, start: () => 0, end: () => 120 },
    });
    fireEvent(audio, new Event("durationchange"));
    fireEvent.change(screen.getByRole("slider", { name: "Seek" }), {
      target: { value: "42" },
    });
    expect(audio.currentTime).toBe(42);
    expect(audio.getAttribute("src")).not.toContain("timeOffset");
  });

  it("restores queue, position and volume on reload and clears on session expiry", async () => {
    const renderPlayer = () =>
      render(
        <MusicPlayerProvider>
          <MemoryRouter>
            <Harness />
            <MusicPlayerBar />
          </MemoryRouter>
        </MusicPlayerProvider>,
      );
    renderPlayer();
    await userEvent.click(screen.getByRole("button", { name: "Start" }));
    const audio = document.querySelector("audio") as HTMLAudioElement;
    Object.defineProperty(audio, "duration", {
      configurable: true,
      value: 120,
    });
    Object.defineProperty(audio, "seekable", {
      configurable: true,
      value: { length: 0 },
    });
    fireEvent(audio, new Event("durationchange"));
    fireEvent.change(screen.getByRole("slider", { name: "Seek" }), {
      target: { value: "42" },
    });
    fireEvent.change(screen.getByRole("slider", { name: "Volume" }), {
      target: { value: "0.35" },
    });
    await waitFor(() => {
      const saved = JSON.parse(
        window.localStorage.getItem("reelscarey:music-player:v1") ?? "null",
      );
      expect(saved?.position).toBe(42);
      expect(saved?.volume).toBe(0.35);
      expect(saved?.queue).toHaveLength(2);
    });

    cleanup();
    renderPlayer();
    expect(await screen.findByText("Airbag")).toBeVisible();
    expect(screen.getByRole("slider", { name: "Seek" })).toHaveValue("42");
    expect(screen.getByRole("slider", { name: "Volume" })).toHaveValue("0.35");
    const restoredAudio = document.querySelector("audio") as HTMLAudioElement;
    expect(restoredAudio.getAttribute("src")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Play" }));
    expect(restoredAudio.getAttribute("src")).toContain("timeOffset=42");

    window.dispatchEvent(new Event("reelscarey:unauthorized"));
    await waitFor(() =>
      expect(
        screen.queryByRole("region", { name: "Music player" }),
      ).not.toBeInTheDocument(),
    );
    expect(
      window.localStorage.getItem("reelscarey:music-player:v1"),
    ).toBeNull();
  });

  it("ignores a stale play() rejection when tracks are skipped rapidly", async () => {
    // First track's play() is held pending; skipping starts a second play()
    // that resolves. When the abandoned promise then rejects with the usual
    // AbortError, the player must not flip back to a paused state.
    let rejectFirst: ((reason?: unknown) => void) | undefined;
    const playSpy = vi
      .spyOn(HTMLMediaElement.prototype, "play")
      .mockImplementationOnce(
        () =>
          new Promise<void>((_resolve, reject) => {
            rejectFirst = reject;
          }),
      )
      .mockResolvedValue(undefined);
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
    await userEvent.click(screen.getByRole("button", { name: "Next track" }));
    expect(await screen.findByText("Paranoid Android")).toBeVisible();
    expect(screen.getByRole("button", { name: "Pause" })).toBeVisible();
    rejectFirst?.(
      new DOMException(
        "The play() request was interrupted by a new load request",
        "AbortError",
      ),
    );
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(screen.getByRole("button", { name: "Pause" })).toBeVisible();
    playSpy.mockRestore();
  });

  it("surfaces a stream failure with a visible error and stays usable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, status: 500 }),
    );
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
    const audio = document.querySelector("audio");
    expect(audio).toBeTruthy();
    await act(async () => {
      audio?.dispatchEvent(new Event("error"));
    });
    expect(
      await screen.findByText("Playback is unavailable for this file."),
    ).toBeVisible();
    // The player bar and transport remain interactive after the failure.
    expect(screen.getByRole("button", { name: "Next track" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Play" })).toBeEnabled();
  });
});
