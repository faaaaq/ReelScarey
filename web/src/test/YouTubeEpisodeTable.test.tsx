import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { EpisodeTable } from "../pages/youtube/EpisodeTable";

function episode(overrides: Record<string, unknown>) {
  return {
    episode_id: 1,
    season_number: 1,
    episode_number: 1,
    title: "Episode one",
    monitored: true,
    file_count: 0,
    state: "missing",
    note: "",
    english_subtitles: null as boolean | null,
    videos: [
      {
        video_id: "vid1",
        title: "Episode one (Full Ver.)",
        position: 1,
        state: "ready",
      },
    ],
    ...overrides,
  };
}

function episodesFor(
  items: ReturnType<typeof episode>[],
  status = 200,
  body: unknown = { items },
) {
  return vi.fn(async (url: string, options?: RequestInit) => {
    if (url.endsWith("/youtube/sources/src1/episodes")) {
      return Response.json(body, { status });
    }
    if (url.endsWith("/youtube/sources/src1/episodes/queue")) {
      if (status === 409)
        return Response.json(
          { error: { message: "The show changed." } },
          { status: 409 },
        );
      const request = JSON.parse(String(options?.body));
      return Response.json({
        items: request.selections.map(
          (selection: { video_id: string; episode_id: number }) => ({
            ...selection,
            state: selection.episode_id === 2 ? "blocked" : "queued",
            message:
              selection.episode_id === 2 ? "A download is active." : "Queued.",
          }),
        ),
      });
    }
    return Response.json(
      { error: { message: "Unexpected request" } },
      { status: 404 },
    );
  });
}

function mount(
  items: ReturnType<typeof episode>[],
  status = 200,
  body: unknown = { items },
) {
  const fetch = episodesFor(items, status, body);
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter>
        <EpisodeTable sourceID="src1" revision={7} />
      </MemoryRouter>
    </Theme>,
  );
  return fetch;
}

describe("YouTube episode table", () => {
  it("renders states, file counts and English subtitle evidence honestly", async () => {
    mount([
      episode({}),
      episode({
        episode_id: 2,
        episode_number: 0,
        state: "queued",
        english_subtitles: false,
        note: "Waiting for a check.",
      }),
      episode({
        episode_id: 3,
        episode_number: 3,
        state: "downloaded",
        file_count: 1,
        english_subtitles: true,
      }),
      episode({
        episode_id: 4,
        episode_number: 4,
        state: "missing",
        english_subtitles: null,
        videos: [],
      }),
    ]);
    const table = await screen.findByRole("table", {
      name: "YouTube episodes",
    });
    expect(within(table).getAllByText(/^unnumbered/)).toHaveLength(1);
    expect(within(table).getByText("S01E01 · Episode one")).toBeVisible();
    expect(within(table).getAllByText("Episode one (Full Ver.)")).toHaveLength(
      3,
    );
    expect(within(table).getByText("Downloaded")).toBeVisible();
    expect(within(table).getByText("English subs")).toBeVisible();
    expect(within(table).getByText("English subs missing")).toBeVisible();
    expect(within(table).getAllByText("English subs not checked")).toHaveLength(
      2,
    );
  });

  it("filters rows by state and subtitle evidence", async () => {
    mount([
      episode({}),
      episode({
        episode_id: 2,
        episode_number: 2,
        state: "failed",
        english_subtitles: false,
      }),
      episode({
        episode_id: 3,
        episode_number: 3,
        state: "downloaded",
        file_count: 1,
        english_subtitles: true,
      }),
    ]);
    const group = screen.getByRole("group", {
      name: "Filter episodes by state",
    });
    await userEvent.click(
      within(group).getByRole("button", { name: "Missing" }),
    );
    let table = screen.getByRole("table", { name: "YouTube episodes" });
    expect(within(table).getByText("S01E01 · Episode one")).toBeVisible();
    expect(within(table).queryByText("S01E02 · Episode one")).toBeNull();
    expect(within(table).queryByText("S01E03 · Episode one")).toBeNull();
    await userEvent.click(
      within(group).getByRole("button", {
        name: "Missing English subtitles",
      }),
    );
    table = screen.getByRole("table", { name: "YouTube episodes" });
    expect(within(table).queryByText("S01E01 · Episode one")).toBeNull();
    expect(within(table).getByText("S01E02 · Episode one")).toBeVisible();
  });

  it("queues a reviewed per-row download with the source revision", async () => {
    const fetch = mount([episode({})]);
    await screen.findByRole("table", { name: "YouTube episodes" });
    await userEvent.click(
      screen.getByRole("button", { name: "Download S01E01" }),
    );
    expect(await screen.findByText("Queued 1 download.")).toBeVisible();
    const queue = fetch.mock.calls.find(([url]) =>
      url.endsWith("/youtube/sources/src1/episodes/queue"),
    );
    expect(JSON.parse(String(queue?.[1]?.body))).toEqual({
      revision: 7,
      selections: [{ video_id: "vid1", episode_id: 1 }],
      reviewed: true,
    });
  });

  it("queues selected rows in one reviewed batch and reports partial results", async () => {
    const fetch = mount([
      episode({}),
      episode({ episode_id: 2, episode_number: 2, state: "missing" }),
    ]);
    const table = await screen.findByRole("table", {
      name: "YouTube episodes",
    });
    await userEvent.click(
      within(table).getByRole("checkbox", {
        name: "Select S01E01 Episode one",
      }),
    );
    await userEvent.click(
      within(table).getByRole("checkbox", {
        name: "Select S01E02 Episode one",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Queue selected (2)" }),
    );
    expect(await screen.findByText(/1 of 2 queued\./)).toBeVisible();
    const queue = fetch.mock.calls.find(([url]) =>
      url.endsWith("/youtube/sources/src1/episodes/queue"),
    );
    expect(JSON.parse(String(queue?.[1]?.body))).toMatchObject({
      revision: 7,
      reviewed: true,
      selections: [
        { video_id: "vid1", episode_id: 1 },
        { video_id: "vid1", episode_id: 2 },
      ],
    });
    expect(
      screen.getByRole("list", { name: "Queue results" }),
    ).toHaveTextContent("A download is active.");
  });

  it("caps batch queueing at 25 reviewed selections", async () => {
    const items = Array.from({ length: 30 }, (_, index) =>
      episode({ episode_id: index + 1, episode_number: index + 1 }),
    );
    mount(items);
    const table = await screen.findByRole("table", {
      name: "YouTube episodes",
    });
    const checkboxes = within(table).getAllByRole("checkbox");
    expect(checkboxes).toHaveLength(30);
    for (const checkbox of checkboxes) {
      await userEvent.click(checkbox);
    }
    expect(
      screen.getByRole("button", {
        name: "Queue selected (first 25 of 30)",
      }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "At most 25 episodes can be queued at once; deselect the rest.",
      ),
    ).toBeVisible();
  });

  it("reports a revision conflict and asks for the source review", async () => {
    const fetch = vi.fn(async (url: string) => {
      if (url.endsWith("/youtube/sources/src1/episodes"))
        return Response.json({ items: [episode({})] });
      if (url.endsWith("/youtube/sources/src1/episodes/queue"))
        return Response.json(
          { error: { message: "The show changed." } },
          { status: 409 },
        );
      return Response.json(
        { error: { message: "Unexpected request" } },
        { status: 404 },
      );
    });
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <MemoryRouter>
          <EpisodeTable sourceID="src1" revision={7} />
        </MemoryRouter>
      </Theme>,
    );
    await screen.findByRole("table", { name: "YouTube episodes" });
    await userEvent.click(
      screen.getByRole("button", { name: "Download S01E01" }),
    );
    expect(
      await screen.findByText("This show changed; review the YouTube source."),
    ).toBeVisible();
  });
});
