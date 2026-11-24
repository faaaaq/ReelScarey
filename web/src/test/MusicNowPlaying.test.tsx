import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MusicNowPlaying } from "../components/MusicNowPlaying";
import { MusicPlayerProvider, useMusicPlayer } from "../musicPlayer";

const TRACKS = [
  {
    fileID: 7,
    title: "Airbag",
    artist: "Radiohead",
    album: "OK Computer",
    albumID: 2,
    durationMS: 180_000,
    formatLabel: "FLAC · 16-bit",
  },
  {
    fileID: 8,
    title: "Paranoid Android",
    artist: "Radiohead",
    album: "OK Computer",
    albumID: 2,
    durationMS: 240_000,
  },
  {
    fileID: 9,
    title: "Exit Music",
    artist: "Radiohead",
    album: "OK Computer",
    albumID: 2,
    durationMS: 360_000,
  },
];

const LARGE_TRACKS = Array.from({ length: 240 }, (_, index) => ({
  fileID: 1_000 + index,
  title: `Track ${String(index + 1).padStart(3, "0")}`,
  artist: "Queue Artist",
  album: "Queue Album",
}));

let player: ReturnType<typeof useMusicPlayer>;

function FixtureControls() {
  player = useMusicPlayer();
  return (
    <>
      <button type="button" onClick={() => player.enqueue(TRACKS[0])}>
        Enqueue Airbag
      </button>
      <button type="button" onClick={() => player.play(TRACKS[0], TRACKS)}>
        Start fixture queue
      </button>
      <button
        type="button"
        onClick={() => player.play(LARGE_TRACKS[0], LARGE_TRACKS)}
      >
        Start large queue
      </button>
      <button type="button" onClick={() => player.playShuffled(LARGE_TRACKS)}>
        Start shuffled large queue
      </button>
    </>
  );
}

function renderPane(onClose?: () => void) {
  return render(
    <MemoryRouter>
      <MusicPlayerProvider>
        <FixtureControls />
        <MusicNowPlaying onClose={onClose} />
      </MusicPlayerProvider>
    </MemoryRouter>,
  );
}

function queueRows() {
  return Array.from(
    document.querySelectorAll<HTMLLIElement>(".music-now-playing-row"),
  );
}

function playNames() {
  return queueRows().map((row) =>
    within(row)
      .getByRole("button", { name: /^Play / })
      .getAttribute("aria-label"),
  );
}

describe("MusicNowPlaying", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  });

  it("shows the honest empty state, closes, and renders an enqueued track", async () => {
    const onClose = vi.fn();
    renderPane(onClose);

    expect(
      screen.getByRole("complementary", {
        name: "Now playing and queue",
      }),
    ).toBeVisible();
    expect(screen.getByText("Your queue is empty")).toBeVisible();
    expect(
      screen.getByText(/The browser queue is saved on this device\./),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Clear queue" })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: "Close queue" }));
    expect(onClose).toHaveBeenCalledTimes(1);

    await userEvent.click(
      screen.getByRole("button", { name: "Enqueue Airbag" }),
    );
    expect(
      await screen.findByRole("button", { name: "Play Airbag" }),
    ).toBeVisible();
    expect(screen.getByText("No track selected")).toBeVisible();
    expect(screen.getByRole("button", { name: "Clear queue" })).toBeEnabled();
    expect(document.querySelectorAll("audio")).toHaveLength(1);
  });

  it("highlights and jumps to tracks, moves and removes rows, and clears", async () => {
    renderPane();
    await userEvent.click(
      screen.getByRole("button", { name: "Start fixture queue" }),
    );

    expect(
      await screen.findByRole("heading", { name: "Airbag" }),
    ).toBeVisible();
    expect(await screen.findByText("Playing")).toBeVisible();
    expect(screen.getByText("FLAC · 16-bit")).toBeVisible();
    expect(screen.getByRole("link", { name: "OK Computer" })).toHaveAttribute(
      "href",
      "/music/albums/2",
    );
    expect(
      document.querySelector<HTMLImageElement>(
        ".music-now-playing-cover-current",
      ),
    ).toHaveAttribute("src", "/api/v1/library/music/albums/2/cover");
    expect(screen.getByText("2 tracks up next · 10:00")).toBeVisible();
    expect(queueRows()[0]).toHaveClass("is-current");
    expect(queueRows()[0]).toHaveAttribute("aria-current", "true");
    expect(within(queueRows()[0]).getByText("Current track:")).toHaveClass(
      "music-now-playing-sr-only",
    );
    expect(
      within(queueRows()[0]).getByRole("button", { name: "Move Airbag up" }),
    ).toBeDisabled();
    expect(
      within(queueRows()[2]).getByRole("button", {
        name: "Move Exit Music down",
      }),
    ).toBeDisabled();

    await userEvent.click(
      screen.getByRole("button", { name: "Play Paranoid Android" }),
    );
    expect(
      await screen.findByRole("heading", { name: "Paranoid Android" }),
    ).toBeVisible();
    expect(queueRows()[1]).toHaveAttribute("aria-current", "true");
    expect(queueRows()[0]).not.toHaveAttribute("aria-current");
    expect(screen.getByText("1 track up next · 6:00")).toBeVisible();

    await userEvent.click(
      screen.getByRole("button", { name: "Move Exit Music up" }),
    );
    expect(playNames()).toEqual([
      "Play Airbag",
      "Play Exit Music",
      "Play Paranoid Android",
    ]);
    expect(queueRows()[2]).toHaveAttribute("aria-current", "true");

    await userEvent.click(
      screen.getByRole("button", { name: "Remove Exit Music" }),
    );
    expect(playNames()).toEqual(["Play Airbag", "Play Paranoid Android"]);
    expect(queueRows()[1]).toHaveAttribute("aria-current", "true");

    await userEvent.click(screen.getByRole("button", { name: "Clear queue" }));
    expect(await screen.findByText("Your queue is empty")).toBeVisible();
    expect(queueRows()).toHaveLength(0);
    expect(screen.getByRole("button", { name: "Clear queue" })).toBeDisabled();
  });

  it("bounds a large queue and slides the current track through the end", async () => {
    renderPane();
    await userEvent.click(
      screen.getByRole("button", { name: "Start large queue" }),
    );

    expect(
      await screen.findByRole("heading", { name: "Track 001" }),
    ).toBeVisible();
    expect(player.queue).toHaveLength(240);
    expect(screen.getByText("240 tracks")).toBeVisible();
    expect(queueRows()).toHaveLength(100);
    expect(screen.getByText("Showing positions 1–100 of 240")).toHaveAttribute(
      "role",
      "status",
    );
    expect(queueRows()[0]).toHaveTextContent("Track 001");
    expect(queueRows()[99]).toHaveTextContent("Track 100");
    expect(
      screen.queryByRole("button", { name: "Play Track 101" }),
    ).not.toBeInTheDocument();

    await act(async () => {
      player.playAt(149);
      await Promise.resolve();
    });
    expect(
      await screen.findByRole("heading", { name: "Track 150" }),
    ).toBeVisible();
    expect(queueRows()).toHaveLength(100);
    expect(screen.getByText("Showing positions 125–224 of 240")).toBeVisible();
    expect(queueRows()[25]).toHaveAttribute("aria-current", "true");
    expect(queueRows()[25]).toHaveTextContent("Track 150");
    expect(within(queueRows()[25]).getByText("150")).toHaveAttribute(
      "aria-label",
      "Queue position 150",
    );

    await act(async () => {
      player.playAt(239);
      await Promise.resolve();
    });
    expect(
      await screen.findByRole("heading", { name: "Track 240" }),
    ).toBeVisible();
    expect(queueRows()).toHaveLength(26);
    expect(screen.getByText("Showing positions 215–240 of 240")).toBeVisible();
    expect(queueRows()[25]).toHaveAttribute("aria-current", "true");
    expect(queueRows()[25]).toHaveTextContent("Track 240");
    expect(
      within(queueRows()[25]).getByRole("button", {
        name: "Move Track 240 down",
      }),
    ).toBeDisabled();
    expect(screen.getByText("0 tracks up next")).toBeVisible();
    expect(document.querySelectorAll("audio")).toHaveLength(1);
  });

  it("shows the exact bounded upcoming order after playShuffled", async () => {
    const random = vi.spyOn(Math, "random").mockReturnValue(0);
    renderPane();
    await userEvent.click(
      screen.getByRole("button", { name: "Start shuffled large queue" }),
    );

    const expected = [...LARGE_TRACKS.slice(1), LARGE_TRACKS[0]];
    expect(player.queue.map((track) => track.fileID)).toEqual(
      expected.map((track) => track.fileID),
    );
    expect(player.current?.fileID).toBe(expected[0].fileID);
    expect(
      await screen.findByRole("heading", { name: expected[0].title }),
    ).toBeVisible();
    expect(queueRows()).toHaveLength(100);
    expect(playNames()).toEqual(
      expected.slice(0, 100).map((track) => `Play ${track.title}`),
    );
    expect(
      screen.getByText("239 tracks up next · duration unknown"),
    ).toBeVisible();
    expect(screen.getByText("Showing positions 1–100 of 240")).toBeVisible();
    expect(document.querySelectorAll("audio")).toHaveLength(1);
    random.mockRestore();
  });

  it("uses absolute queue indexes for Play, Move, and Remove", async () => {
    renderPane();
    await userEvent.click(
      screen.getByRole("button", { name: "Start large queue" }),
    );
    await act(async () => {
      player.playAt(149);
      await Promise.resolve();
    });

    await userEvent.click(
      screen.getByRole("button", { name: "Play Track 200" }),
    );
    expect(player.index).toBe(199);
    expect(player.current?.fileID).toBe(LARGE_TRACKS[199].fileID);
    expect(
      await screen.findByRole("heading", { name: "Track 200" }),
    ).toBeVisible();

    await userEvent.click(
      screen.getByRole("button", { name: "Move Track 210 up" }),
    );
    expect(player.queue[208].fileID).toBe(LARGE_TRACKS[209].fileID);
    expect(player.queue[209].fileID).toBe(LARGE_TRACKS[208].fileID);
    expect(player.current?.fileID).toBe(LARGE_TRACKS[199].fileID);

    await userEvent.click(
      screen.getByRole("button", { name: "Remove Track 220" }),
    );
    expect(player.queue).toHaveLength(239);
    expect(
      player.queue.some((track) => track.fileID === LARGE_TRACKS[219].fileID),
    ).toBe(false);
    expect(player.index).toBe(199);
    expect(player.current?.fileID).toBe(LARGE_TRACKS[199].fileID);
    expect(screen.getByText("239 tracks")).toBeVisible();
    expect(document.querySelectorAll("audio")).toHaveLength(1);
  });
});
