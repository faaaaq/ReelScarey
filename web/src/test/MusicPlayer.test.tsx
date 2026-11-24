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
  useMusicQueue,
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
    expect(audio?.getAttribute("src")).toMatch(
      /^\/api\/v1\/library\/music\/files\/7\/stream\?compat=mp3&stream_id=[a-f0-9]{32}$/,
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
    expect(audio?.getAttribute("src")).toContain("compat=mp3");
    noOpus.mockRestore();
    cleanup();

    const withOpus = vi
      .spyOn(HTMLMediaElement.prototype, "canPlayType")
      .mockReturnValue("probably");
    const audio2 = await renderAndPlay();
    expect(audio2?.getAttribute("src")).toMatch(
      /^\/api\/v1\/library\/music\/files\/7\/stream\?stream_id=[a-f0-9]{32}$/,
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
    expect(document.querySelector("audio")?.getAttribute("src")).toMatch(
      /^\/api\/v1\/library\/music\/files\/7\/stream\?stream_id=[a-f0-9]{32}$/,
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
    expect(audio.getAttribute("src")).toContain("compat=mp3");
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
      const progress = JSON.parse(
        window.localStorage.getItem("reelscarey:music-player-progress:v1") ??
          "null",
      );
      expect(progress?.position).toBe(42);
      expect(progress?.volume).toBe(0.35);
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
    expect(
      window.localStorage.getItem("reelscarey:music-player-progress:v1"),
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

  it("shuffle preserves current/history and Next, ended and Previous follow the displayed order", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0);
    renderQueue();
    await start([...QUEUE_TRACKS, THIRD, FOURTH]);
    await act(async () => {
      player.playAt(1);
      await Promise.resolve();
    });
    act(() => player.toggleShuffle());
    expect(player.queue.map((track) => track.fileID)).toEqual([7, 8, 10, 9]);
    expect(player.index).toBe(1);
    expect(player.current?.fileID).toBe(8);

    await act(async () => {
      player.next();
      await Promise.resolve();
    });
    expect(player.index).toBe(2);
    expect(player.current?.fileID).toBe(10);
    const audio = document.querySelector("audio") as HTMLAudioElement;
    await ended(audio);
    expect(player.index).toBe(3);
    expect(player.current?.fileID).toBe(9);
    audio.currentTime = 0;
    await act(async () => {
      player.previous();
      await Promise.resolve();
    });
    expect(player.index).toBe(2);
    expect(player.current?.fileID).toBe(10);
    random.mockRestore();
  });

  it("disabling shuffle leaves the installed visible order unchanged", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0);
    renderQueue();
    await start([...QUEUE_TRACKS, THIRD, FOURTH]);
    act(() => player.toggleShuffle());
    const displayed = player.queue.map((track) => track.fileID);
    expect(displayed).toEqual([7, 9, 10, 8]);
    act(() => player.toggleShuffle());
    expect(player.shuffle).toBe(false);
    expect(player.queue.map((track) => track.fileID)).toEqual(displayed);
    await act(async () => {
      player.next();
      await Promise.resolve();
    });
    expect(player.current?.fileID).toBe(9);
    random.mockRestore();
  });

  it("playShuffled installs a unique shuffled permutation and ordinary play stays coherent", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0);
    const tracks = [...QUEUE_TRACKS, THIRD, FOURTH];
    renderQueue();
    await act(async () => {
      player.playShuffled([
        ...tracks,
        QUEUE_TRACKS[0],
        { ...THIRD, fileID: 0 },
        THIRD,
      ]);
      await Promise.resolve();
    });
    expect(player.queue.map((track) => track.fileID)).toEqual([8, 9, 10, 7]);
    expect(new Set(player.queue.map((track) => track.fileID)).size).toBe(4);
    expect([...player.queue.map((track) => track.fileID)].sort()).toEqual(
      tracks.map((track) => track.fileID).sort(),
    );
    expect(player.index).toBe(0);
    expect(player.current?.fileID).toBe(8);
    expect(player.shuffle).toBe(true);

    await act(async () => {
      player.play(THIRD, tracks);
      await Promise.resolve();
    });
    // While shuffle is active, ordinary play puts the selection first and
    // shuffles the remaining unique tracks into a coherent visible queue.
    expect(player.index).toBe(0);
    expect(player.current?.fileID).toBe(9);
    expect(player.queue[0].fileID).toBe(9);
    expect(new Set(player.queue.map((track) => track.fileID)).size).toBe(4);
    expect([...player.queue.map((track) => track.fileID)].sort()).toEqual(
      tracks.map((track) => track.fileID).sort(),
    );
    random.mockRestore();
  });

  it("shuffle with no selection shuffles the entire queue", () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0);
    renderQueue();
    act(() => {
      player.enqueue(QUEUE_TRACKS[0]);
      player.enqueue(QUEUE_TRACKS[1]);
      player.enqueue(THIRD);
    });
    act(() => player.toggleShuffle());
    expect(player.queue.map((track) => track.fileID)).toEqual([8, 9, 7]);
    expect(player.index).toBe(-1);
    expect(player.current).toBeNull();
    random.mockRestore();
  });

  it("keeps a large live queue but persists and reopens a current-first bounded window", async () => {
    const tracks = Array.from({ length: 620 }, (_, index) => ({
      fileID: index + 1,
      title: `Track ${index + 1}`,
      artist: "Artist",
      album: "Album",
    }));
    renderQueue();
    await act(async () => {
      player.play(tracks[550], tracks);
      await Promise.resolve();
    });
    expect(player.queue).toHaveLength(620);
    expect(player.index).toBe(550);
    expect(player.current?.fileID).toBe(551);
    await waitFor(() => {
      const saved = JSON.parse(
        window.localStorage.getItem("reelscarey:music-player:v1") ?? "null",
      );
      expect(saved?.queue).toHaveLength(70);
      expect(saved?.index).toBe(0);
      expect(saved?.queue[0].fileID).toBe(551);
      expect(saved?.queue.at(-1).fileID).toBe(620);
    });
    expect(player.queue).toHaveLength(620);

    cleanup();
    renderQueue();
    expect(player.queue).toHaveLength(70);
    expect(player.index).toBe(0);
    expect(player.current?.fileID).toBe(551);
  });

  it("flushes an immediate track change and matching progress on pagehide", async () => {
    renderQueue();
    await start();
    const audio = document.querySelector("audio") as HTMLAudioElement;
    act(() => {
      // Hide before React commits the selection or either delayed writer runs.
      player.playAt(1);
      audio.currentTime = 27;
      audio.dispatchEvent(new Event("timeupdate"));
      window.dispatchEvent(new Event("pagehide"));
    });
    const saved = JSON.parse(
      window.localStorage.getItem("reelscarey:music-player:v1") ?? "null",
    );
    const progress = JSON.parse(
      window.localStorage.getItem("reelscarey:music-player-progress:v1") ??
        "null",
    );
    expect(saved?.index).toBe(1);
    expect(saved?.queue[saved.index].fileID).toBe(8);
    expect(saved?.position).toBe(27);
    expect(progress).toMatchObject({ fileID: 8, position: 27 });

    cleanup();
    renderQueue();
    expect(player.current?.fileID).toBe(8);
    expect(player.index).toBe(1);
    expect(player.position).toBe(27);
  });

  it("ignores mismatched track progress while restoring global preferences", () => {
    window.localStorage.setItem(
      "reelscarey:music-player:v1",
      JSON.stringify({
        queue: QUEUE_TRACKS,
        index: 0,
        position: 11,
        duration: 111,
        volume: 0.65,
        muted: false,
      }),
    );
    window.localStorage.setItem(
      "reelscarey:music-player-progress:v1",
      JSON.stringify({
        fileID: 8,
        position: 91,
        duration: 910,
        volume: 0.2,
        muted: true,
      }),
    );
    renderQueue();
    expect(player.current?.fileID).toBe(7);
    expect(player.position).toBe(11);
    expect(player.duration).toBe(111);
    expect(player.volume).toBe(0.2);
    expect(player.muted).toBe(true);
  });

  it("preserves a legacy oversized current before clamping and merges progress preferences", () => {
    const tracks = Array.from({ length: 650 }, (_, index) => ({
      fileID: index + 1,
      title: `Track ${index + 1}`,
      artist: "Artist",
      album: "Album",
    }));
    window.localStorage.setItem(
      "reelscarey:music-player:v1",
      JSON.stringify({ queue: tracks, index: 575, position: 4 }),
    );
    window.localStorage.setItem(
      "reelscarey:music-player-progress:v1",
      JSON.stringify({
        fileID: 576,
        position: 19,
        duration: 120,
        volume: 0.2,
        muted: true,
      }),
    );
    renderQueue();
    expect(player.queue).toHaveLength(75);
    expect(player.index).toBe(0);
    expect(player.current?.fileID).toBe(576);
    expect(player.position).toBe(19);
    expect(player.duration).toBe(120);
    expect(player.volume).toBe(0.2);
    expect(player.muted).toBe(true);
  });

  it("does not rerender queue-only consumers for progress and bounds large storage", async () => {
    let queuePlayer!: ReturnType<typeof useMusicQueue>;
    let queueRenders = 0;
    function QueueOnly() {
      queuePlayer = useMusicQueue();
      queueRenders += 1;
      return null;
    }
    function Progress() {
      const fullPlayer = useMusicPlayer();
      return <output data-testid="progress">{fullPlayer.position}</output>;
    }
    render(
      <MusicPlayerProvider>
        <QueueOnly />
        <Progress />
      </MusicPlayerProvider>,
    );
    const tracks = Array.from({ length: 1200 }, (_, index) => ({
      fileID: index + 1,
      title: `Track ${index + 1}`,
      artist: "Artist",
      album: "Album",
    }));
    await act(async () => {
      queuePlayer.play(tracks[0], tracks);
      await Promise.resolve();
    });
    const rendersAfterPlay = queueRenders;
    const audio = document.querySelector("audio") as HTMLAudioElement;
    audio.currentTime = 17;
    fireEvent(audio, new Event("timeupdate"));
    expect(screen.getByTestId("progress")).toHaveTextContent("17");
    expect(queueRenders).toBe(rendersAfterPlay);
    act(() => queuePlayer.changeVolume(0.25));
    expect(queueRenders).toBe(rendersAfterPlay);
    fireEvent(window, new Event("pagehide"));
    const progress = JSON.parse(
      window.localStorage.getItem("reelscarey:music-player-progress:v1") ??
        "null",
    );
    expect(progress).toEqual({
      fileID: 1,
      position: 17,
      duration: 0,
      volume: 0.25,
      muted: false,
    });
    expect(progress.queue).toBeUndefined();
    await waitFor(() => {
      const saved = JSON.parse(
        window.localStorage.getItem("reelscarey:music-player:v1") ?? "null",
      );
      expect(saved?.queue).toHaveLength(500);
      expect(saved?.index).toBe(0);
      expect(saved?.queue.at(-1).fileID).toBe(500);
    });
    expect(queuePlayer.queue).toHaveLength(1200);
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
    window.localStorage.removeItem("reelscarey:music-player-progress:v1");
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
