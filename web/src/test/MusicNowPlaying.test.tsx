import { render, screen, within } from "@testing-library/react";
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

function FixtureControls() {
  const player = useMusicPlayer();
  return (
    <>
      <button type="button" onClick={() => player.enqueue(TRACKS[0])}>
        Enqueue Airbag
      </button>
      <button type="button" onClick={() => player.play(TRACKS[0], TRACKS)}>
        Start fixture queue
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
});
