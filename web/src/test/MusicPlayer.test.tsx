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

  it("explains a registered-but-missing file and offers import and retry recovery", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(async () =>
        Response.json(
          {
            error: {
              code: "music_file_missing",
              message: "The registered file cannot be opened.",
            },
          },
          { status: 404 },
        ),
      ),
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
    const audio = document.querySelector("audio");
    await act(async () => {
      audio?.dispatchEvent(new Event("error"));
    });
    expect(
      await screen.findByText(/registered file is missing or unreadable/),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Open Music Import" }),
    ).toHaveAttribute("href", "/music/import");
    await userEvent.click(
      screen.getByRole("button", { name: "Retry playback" }),
    );
    expect(
      screen.queryByText(/registered file is missing or unreadable/),
    ).not.toBeInTheDocument();
  });

  it("keeps an unknown registration distinct from missing file bytes", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockImplementation(async () =>
          Response.json(
            { error: { code: "not_found", message: "No such registration." } },
            { status: 404 },
          ),
        ),
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
    const audio = document.querySelector("audio");
    await act(async () => {
      audio?.dispatchEvent(new Event("error"));
    });
    expect(
      await screen.findByText(
        "This file is no longer registered in the music library.",
      ),
    ).toBeVisible();
    expect(
      screen.queryByRole("link", { name: "Open Music Import" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Retry playback" }),
    ).toBeEnabled();
  });
});

describe("music player queue workspace", () => {
  const THIRD = {
    fileID: 9,
    title: "Exit Music",
    artist: "Radiohead",
    album: "OK Computer",
  };
  const FOURTH = {
    fileID: 10,
    title: "Let Down",
    artist: "Radiohead",
    album: "OK Computer",
  };
  let player!: ReturnType<typeof useMusicPlayer>;

  function QueueHarness() {
    player = useMusicPlayer();
    return null;
  }

  function renderQueue() {
    render(
      <MusicPlayerProvider>
        <QueueHarness />
      </MusicPlayerProvider>,
    );
  }

  async function start(
    queue: ReturnType<typeof useMusicPlayer>["queue"] = QUEUE_TRACKS,
  ) {
    await act(async () => {
      player.play(queue[0], queue);
      await Promise.resolve();
    });
  }

  async function ended(audio: HTMLAudioElement) {
    await act(async () => {
      audio.dispatchEvent(new Event("ended"));
      await Promise.resolve();
    });
  }

  beforeEach(() => {
    window.localStorage.clear();
    vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  });

  it("enqueues without autoplay, deduplicates and inserts play-next after current", async () => {
    const playSpy = vi.spyOn(HTMLMediaElement.prototype, "play");
    renderQueue();
    act(() => player.enqueue(THIRD));
    expect(player.queue.map((track) => track.fileID)).toEqual([9]);
    expect(player.current).toBeNull();
    expect(player.index).toBe(-1);
    expect(playSpy).not.toHaveBeenCalled();

    await start();
    const callsAfterStart = playSpy.mock.calls.length;
    act(() => {
      player.enqueue(THIRD);
      player.enqueue(FOURTH, true);
      player.enqueue(THIRD, true);
    });
    // The functional queue update also keeps synchronous enqueue calls safe.
    expect(player.queue.map((track) => track.fileID)).toEqual([7, 10, 8, 9]);
    act(() => player.enqueue(FOURTH, true));
    expect(player.queue.map((track) => track.fileID)).toEqual([7, 10, 8, 9]);
    expect(player.current?.fileID).toBe(7);
    expect(player.index).toBe(0);
    expect(playSpy).toHaveBeenCalledTimes(callsAfterStart);
  });

  it("preserves batch play-next order when the queue has no current track", () => {
    renderQueue();
    // Workspace batches enqueue in reverse so repeated insertion yields the
    // original order, including when there is no current track yet.
    act(() => {
      player.enqueue(FOURTH, true);
      player.enqueue(THIRD, true);
    });
    expect(player.queue.map((track) => track.fileID)).toEqual([9, 10]);
    expect(player.current).toBeNull();
    expect(player.index).toBe(-1);
  });

  it("plays only valid queue indexes", async () => {
    renderQueue();
    await start([...QUEUE_TRACKS, THIRD]);
    await act(async () => {
      player.playAt(2);
      await Promise.resolve();
    });
    expect(player.current?.fileID).toBe(9);
    expect(player.index).toBe(2);
    const source = document.querySelector("audio")?.getAttribute("src");
    act(() => {
      player.playAt(-1);
      player.playAt(3);
      player.playAt(1.5);
    });
    expect(player.current?.fileID).toBe(9);
    expect(player.index).toBe(2);
    expect(document.querySelector("audio")?.getAttribute("src")).toBe(source);
  });

  it("keeps the current index correct through moves and removals", async () => {
    renderQueue();
    await start([...QUEUE_TRACKS, THIRD, FOURTH]);
    await act(async () => {
      player.playAt(1);
      await Promise.resolve();
    });
    act(() => player.moveQueueItem(0, 3));
    expect(player.queue.map((track) => track.fileID)).toEqual([8, 9, 10, 7]);
    expect(player.index).toBe(0);
    expect(player.current?.fileID).toBe(8);

    act(() => player.moveQueueItem(0, 2));
    expect(player.queue.map((track) => track.fileID)).toEqual([9, 10, 8, 7]);
    expect(player.index).toBe(2);
    act(() => {
      player.moveQueueItem(-1, 1);
      player.moveQueueItem(0, 9);
    });
    expect(player.queue.map((track) => track.fileID)).toEqual([9, 10, 8, 7]);

    act(() => player.removeAt(0));
    expect(player.queue.map((track) => track.fileID)).toEqual([10, 8, 7]);
    expect(player.index).toBe(1);
    expect(player.current?.fileID).toBe(8);
    await act(async () => {
      player.removeAt(1);
      await Promise.resolve();
    });
    expect(player.queue.map((track) => track.fileID)).toEqual([10, 7]);
    expect(player.index).toBe(1);
    expect(player.current?.fileID).toBe(7);
    await act(async () => {
      player.removeAt(1);
      await Promise.resolve();
    });
    expect(player.current?.fileID).toBe(10);
    expect(player.index).toBe(0);
  });

  it("clears the queue with the same teardown semantics as stop", async () => {
    renderQueue();
    await start();
    act(() => player.clearQueue());
    expect(player.queue).toEqual([]);
    expect(player.current).toBeNull();
    expect(player.index).toBe(-1);
    expect(player.playing).toBe(false);
    expect(document.querySelector("audio")?.getAttribute("src")).toBeNull();
    expect(
      window.localStorage.getItem("reelscarey:music-player:v1"),
    ).toBeNull();
  });

  it("keeps repeat-off end behavior and wraps with repeat-all", async () => {
    renderQueue();
    await start();
    await act(async () => {
      player.playAt(1);
      await Promise.resolve();
    });
    const audio = document.querySelector("audio") as HTMLAudioElement;
    await ended(audio);
    expect(player.repeat).toBe("off");
    expect(player.current?.fileID).toBe(8);
    expect(player.index).toBe(1);
    expect(player.playing).toBe(false);
    expect(player.position).toBe(0);

    act(() => player.cycleRepeat());
    expect(player.repeat).toBe("all");
    await ended(audio);
    expect(player.current?.fileID).toBe(7);
    expect(player.index).toBe(0);
  });

  it("repeats one only for ended and lets manual Next skip", async () => {
    renderQueue();
    await start();
    act(() => {
      player.cycleRepeat();
    });
    act(() => player.cycleRepeat());
    expect(player.repeat).toBe("one");
    const audio = document.querySelector("audio") as HTMLAudioElement;
    await ended(audio);
    expect(player.current?.fileID).toBe(7);
    expect(player.index).toBe(0);
    await act(async () => {
      player.next();
      await Promise.resolve();
    });
    expect(player.current?.fileID).toBe(8);
    expect(player.index).toBe(1);
  });

  it("shuffle selects a valid different queue entry", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0);
    renderQueue();
    await start([...QUEUE_TRACKS, THIRD]);
    act(() => player.toggleShuffle());
    await act(async () => {
      player.next();
      await Promise.resolve();
    });
    expect(player.index).toBeGreaterThanOrEqual(0);
    expect(player.index).toBeLessThan(3);
    expect(player.index).not.toBe(0);
    expect(player.index).toBe(1);
    expect(player.current?.fileID).toBe(8);
    random.mockRestore();
  });

  it("persists shuffle and repeat while restoring older payloads with defaults", async () => {
    renderQueue();
    await start();
    act(() => {
      player.toggleShuffle();
      player.cycleRepeat();
    });
    await waitFor(() => {
      const saved = JSON.parse(
        window.localStorage.getItem("reelscarey:music-player:v1") ?? "null",
      );
      expect(saved?.shuffle).toBe(true);
      expect(saved?.repeat).toBe("all");
    });

    cleanup();
    renderQueue();
    expect(player.shuffle).toBe(true);
    expect(player.repeat).toBe("all");

    cleanup();
    window.localStorage.setItem(
      "reelscarey:music-player:v1",
      JSON.stringify({
        queue: QUEUE_TRACKS,
        index: 0,
        position: 12,
        duration: 120,
        volume: 0.4,
        muted: false,
      }),
    );
    renderQueue();
    expect(player.current?.fileID).toBe(7);
    expect(player.shuffle).toBe(false);
    expect(player.repeat).toBe("off");
    expect(player.position).toBe(12);
  });

  it("offers accessible shuffle, repeat and full-player controls in the bar", async () => {
    render(
      <MusicPlayerProvider>
        <MemoryRouter>
          <Harness />
          <MusicPlayerBar />
        </MemoryRouter>
      </MusicPlayerProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "Start" }));
    const shuffle = screen.getByRole("button", { name: "Shuffle" });
    const repeat = screen.getByRole("button", { name: "Repeat" });
    expect(shuffle).toHaveAttribute("aria-pressed", "false");
    expect(repeat).toHaveAttribute("data-repeat", "off");
    await userEvent.click(shuffle);
    expect(shuffle).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(repeat);
    expect(repeat).toHaveAttribute("data-repeat", "all");
    expect(repeat).toHaveAttribute("aria-pressed", "true");
    expect(
      screen.getByRole("link", { name: "Open full player" }),
    ).toHaveAttribute("href", "/music/player");
  });
});
