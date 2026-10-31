import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import {
  AnimeOrder,
  type AnimeIdentity,
  type AnimeOrderSettings,
} from "../pages/AnimeOrder";

function identity(id = 1): AnimeIdentity {
  return {
    provider: "anilist",
    id,
    title: id === 1 ? "Anime show" : "Anime side story",
    romaji: "Anime Romaji",
    english: "Anime English",
    native: "アニメ",
    format: id === 1 ? "TV" : "OVA",
    year: 2020,
    episodes: id === 1 ? 12 : 1,
    url: `https://anilist.co/anime/${id}`,
  };
}
function fixture(): AnimeOrderSettings {
  return {
    media_id: 11,
    revision: 3,
    category_revision: 2,
    numbering_revision: 7,
    identities: [identity()],
    entries: [],
    episodes: [
      {
        episode_id: 101,
        season: 0,
        number: 1,
        title: "Special",
        absolute_number: null,
      },
      {
        episode_id: 102,
        season: 1,
        number: 1,
        title: "Opening",
        absolute_number: 1,
      },
      {
        episode_id: 103,
        season: 2,
        number: 1,
        title: "Return",
        absolute_number: 13,
      },
    ],
  };
}
function setup(
  extra?: (
    url: string,
    options: RequestInit | undefined,
    state: AnimeOrderSettings,
  ) => Response | Promise<Response> | undefined,
  initial = fixture(),
) {
  let state = structuredClone(initial),
    review = state;
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const custom = await extra?.(url, options, state);
    if (custom) return custom;
    if (url.startsWith("/api/v1/metadata/anime?"))
      return Response.json({ items: [identity(2)] });
    if (url.endsWith("/preview")) {
      const draft = JSON.parse(String(options?.body));
      review = {
        ...state,
        identities: draft.provider_ids.map((id: number) => identity(id)),
        entries: draft.entries,
      };
      return Response.json({
        review_id: "review-token",
        settings: review,
        expires_at: Math.floor(Date.now() / 1000) + 600,
      });
    }
    if (options?.method === "PUT") {
      state = {
        ...review,
        revision: state.revision + 1,
        numbering_revision: state.numbering_revision + 1,
      };
      return Response.json(state);
    }
    if (url.endsWith("/sources"))
      return Response.json({
        items: [
          {
            id: "group-one",
            name: "Alternate order",
            type: 2,
            episode_count: 2,
            group_count: 1,
          },
        ],
      });
    if (url.endsWith("/propose")) {
      const body = JSON.parse(String(options?.body));
      return Response.json({
        source_name: "Alternate order",
        groups: [
          {
            id: "subgroup-one",
            name: "Chosen subgroup",
            order: 0,
            episode_count: 2,
          },
        ],
        entries: body.subgroup_id
          ? [
              {
                episode_id: 102,
                provider_id: body.provider_id,
                release_title: body.release_title,
                label: "SP5",
              },
            ]
          : [],
        unmatched: body.subgroup_id
          ? [{ tmdb_id: 999, season: 9, number: 1, title: "Unloaded episode" }]
          : [],
      });
    }
    if (url.endsWith("/releases/anime-pack"))
      return Response.json({
        items: [
          {
            id: "release-one",
            movie_id: 11,
            title: "Owned Anime pack",
            size_bytes: 1000000,
            seeders: 4,
            peers: 1,
            published_at: "",
            protocol: "torrent",
            indexer: "Fixture",
          },
        ],
        warnings: [],
      });
    if (url.endsWith("/downloads"))
      return Response.json({ id: "job-one" }, { status: 201 });
    return Response.json(state);
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter>
        <AnimeOrder id={11} />
      </MemoryRouter>
    </Theme>,
  );
  return fetch;
}
async function open() {
  await userEvent.click(
    screen.getByRole("button", { name: "Edit Anime identity & mapping" }),
  );
  await screen.findByRole("heading", { name: "Linked AniList entries" });
}
async function mapSpecial(label = "SP1", title = "Anime special") {
  await userEvent.type(
    screen.getByRole("textbox", { name: "Release title for S00E01 · Special" }),
    title,
  );
  await userEvent.type(
    screen.getByRole("textbox", { name: "Release label for S00E01 · Special" }),
    label,
  );
}

describe("Reviewed Anime identity, order and pack workflow", () => {
  it("scopes Kitsu searches and review payloads separately from AniList IDs", async () => {
    const initial = fixture();
    initial.identities = [];
    const kitsu = {
      ...identity(2),
      provider: "kitsu",
      url: "https://kitsu.io/anime/2",
    };
    const fetch = setup(
      (url) =>
        url.includes("/metadata/anime?")
          ? Response.json({ items: [kitsu] })
          : undefined,
      initial,
    );
    await open();
    await userEvent.selectOptions(
      screen.getByLabelText("Anime provider"),
      "kitsu",
    );
    await userEvent.type(
      screen.getByLabelText("Search Kitsu titles"),
      "Owned story",
    );
    await userEvent.click(screen.getByRole("button", { name: "Search Kitsu" }));
    await userEvent.click(
      await screen.findByRole("button", { name: "Link Kitsu 2" }),
    );
    expect(screen.getByLabelText("Anime provider")).toBeDisabled();
    expect(
      fetch.mock.calls.some(([url]) => url.includes("&provider=kitsu")),
    ).toBe(true);
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    await waitFor(() =>
      expect(fetch.mock.calls.some(([url]) => url.endsWith("/preview"))).toBe(
        true,
      ),
    );
    const sent = fetch.mock.calls.find(([url]) => url.endsWith("/preview"))!;
    expect(JSON.parse(String(sent[1]?.body))).toMatchObject({
      provider: "kitsu",
      provider_ids: [2],
    });
  });
  it("reviews refreshed AniList facts without applying or changing canonical mappings", async () => {
    const fetch = setup((url, _options, state) =>
      url.endsWith("/refresh")
        ? Response.json({
            review_id: "refresh-review",
            settings: {
              ...state,
              identities: [{ ...identity(), title: "Fresh AniList facts" }],
            },
            expires_at: Math.floor(Date.now() / 1000) + 600,
          })
        : undefined,
    );
    await open();
    await userEvent.click(
      screen.getByRole("button", { name: "Review refreshed AniList details" }),
    );
    const review = await screen.findByRole("region", {
      name: "Review Anime identity and episode changes",
    });
    expect(within(review).getByText(/Fresh AniList facts/)).toBeVisible();
    expect(fetch.mock.calls.some(([, o]) => o?.method === "PUT")).toBe(false);
    expect(
      screen.getByRole("button", { name: "Save reviewed Anime mapping" }),
    ).toBeDisabled();
  });
  it("offers identity-only links for Anime films without TV group requests", async () => {
    const initial = fixture();
    initial.numbering_revision = 0;
    initial.episodes = [];
    const fetch = setup(undefined, initial);
    await open();
    expect(
      screen.getByText(/This Anime film supports identity links only/),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Load TMDb episode orders" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("region", { name: "Manual Anime pack search" }),
    ).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Unlink 1" }));
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    await screen.findByRole("region", {
      name: "Review Anime identity and episode changes",
    });
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([url]) => url.endsWith("/preview"))?.[1]?.body,
        ),
      ),
    ).toMatchObject({ numbering_revision: 0, provider_ids: [], entries: [] });
    expect(
      fetch.mock.calls.some(
        ([url]) => url.endsWith("/sources") || url.endsWith("/propose"),
      ),
    ).toBe(false);
  });

  it("explains unloaded series episodes while keeping explicit order lookup available", async () => {
    const initial = fixture();
    initial.episodes = [];
    const fetch = setup(undefined, initial);
    await open();
    expect(screen.getByText(/No canonical episodes are loaded/)).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Load TMDb episode orders" }),
    );
    await screen.findByRole("combobox", { name: "TMDb episode order" });
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/sources"))).toBe(
      true,
    );
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
  });

  it("retains saved links while limiting a review to eight new provider entries", async () => {
    const fetch = setup((url) =>
      url.startsWith("/api/v1/metadata/anime?")
        ? Response.json({
            items: Array.from({ length: 9 }, (_, index) => identity(index + 2)),
          })
        : undefined,
    );
    await open();
    await userEvent.type(
      screen.getByRole("textbox", { name: "Search AniList titles" }),
      "Explicit entries",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Search AniList" }),
    );
    await screen.findByRole("button", { name: "Link AniList 2" });
    for (let id = 2; id <= 9; id++)
      await userEvent.click(
        screen.getByRole("button", { name: `Link AniList ${id}` }),
      );
    expect(
      screen.getByRole("button", { name: "Link AniList 10" }),
    ).toBeDisabled();
    expect(
      screen.getByText(/Add at most eight new links per review/),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    await screen.findByRole("region", {
      name: "Review Anime identity and episode changes",
    });
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([url]) => url.endsWith("/preview"))?.[1]?.body,
        ),
      ).provider_ids,
    ).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9]);
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
  });

  it("refuses an expired review without attempting a write", async () => {
    const fetch = setup((url, options, state) => {
      if (!url.endsWith("/preview")) return undefined;
      const body = JSON.parse(String(options?.body));
      return Response.json({
        review_id: "expired-token",
        expires_at: 1,
        settings: { ...state, entries: body.entries },
      });
    });
    await open();
    await mapSpecial();
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    await screen.findByRole("region", {
      name: "Review Anime identity and episode changes",
    });
    await userEvent.click(
      screen.getByRole("checkbox", {
        name: "I reviewed these Anime identities and episode mappings",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed Anime mapping" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("review expired");
    expect(
      screen.getByRole("button", { name: "Reload saved Anime mapping" }),
    ).toBeVisible();
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
  });

  it("loads identities lazily and previews only selected provider IDs and explicit special mappings", async () => {
    const fetch = setup();
    expect(fetch).not.toHaveBeenCalled();
    await open();
    expect(
      screen.getByRole("link", { name: "View on AniList" }),
    ).toHaveAttribute("href", "https://anilist.co/anime/1");
    await userEvent.type(
      screen.getByRole("textbox", { name: "Search AniList titles" }),
      "Side story",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Search AniList" }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Link AniList 2" }),
    );
    await mapSpecial("OVA1");
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Provider for S00E01 · Special" }),
      "2",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    const review = await screen.findByRole("region", {
      name: "Review Anime identity and episode changes",
    });
    expect(review).toHaveTextContent("S00E01 · Special");
    expect(review).toHaveTextContent("OVA1");
    const body = JSON.parse(
      String(
        fetch.mock.calls.find(([url]) => url.endsWith("/preview"))?.[1]?.body,
      ),
    );
    expect(body).toEqual({
      revision: 3,
      category_revision: 2,
      numbering_revision: 7,
      provider_ids: [1, 2],
      entries: [
        {
          episode_id: 101,
          provider_id: 2,
          release_title: "Anime special",
          label: "OVA1",
        },
      ],
    });
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
    await userEvent.click(
      screen.getByRole("button", { name: "Cancel review" }),
    );
    expect(
      screen.queryByRole("region", {
        name: "Review Anime identity and episode changes",
      }),
    ).not.toBeInTheDocument();
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
  });

  it("requires confirmation of the normalized server review and applies only its token", async () => {
    const fetch = setup();
    await open();
    await mapSpecial();
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    const review = await screen.findByRole("region", {
      name: "Review Anime identity and episode changes",
    });
    expect(
      screen.getByRole("button", { name: "Save reviewed Anime mapping" }),
    ).toBeDisabled();
    await userEvent.click(
      within(review).getByRole("checkbox", {
        name: "I reviewed these Anime identities and episode mappings",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed Anime mapping" }),
    );
    await screen.findByText(/Anime identity and episode mapping saved/);
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([, options]) => options?.method === "PUT")?.[1]
            ?.body,
        ),
      ),
    ).toEqual({ review_id: "review-token" });
    await waitFor(() =>
      expect(
        screen.getByRole("textbox", {
          name: "Release label for S00E01 · Special",
        }),
      ).toHaveValue("SP1"),
    );
    expect(
      fetch.mock.calls.filter(
        ([url, options]) => url.endsWith("/anime-order") && !options?.method,
      ),
    ).toHaveLength(2);
  });

  it("retains edits across episode pages without conflating numbered episodes and specials", async () => {
    const initial = fixture();
    for (let n = 2; n <= 51; n++)
      initial.episodes.push({
        episode_id: 102 + n,
        season: 2,
        number: n,
        title: `Additional ${n}`,
        absolute_number: null,
      });
    const fetch = setup(undefined, initial);
    await open();
    await mapSpecial("SP1", "Anime release");
    await userEvent.click(screen.getByRole("button", { name: /Next/ }));
    await userEvent.type(
      screen.getByRole("textbox", {
        name: "Release title for S02E51 · Additional 51",
      }),
      "Anime release",
    );
    await userEvent.type(
      screen.getByRole("textbox", {
        name: "Release label for S02E51 · Additional 51",
      }),
      "1",
    );
    await userEvent.click(screen.getByRole("button", { name: /Previous/ }));
    expect(
      screen.getByRole("textbox", {
        name: "Release label for S00E01 · Special",
      }),
    ).toHaveValue("SP1");
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    await screen.findByRole("region", {
      name: "Review Anime identity and episode changes",
    });
    const body = JSON.parse(
      String(
        fetch.mock.calls.find(([url]) => url.endsWith("/preview"))?.[1]?.body,
      ),
    );
    expect(body.entries).toHaveLength(2);
    expect(body.entries.map((entry: { label: string }) => entry.label)).toEqual(
      ["SP1", "1"],
    );
  });

  it("shows unmatched source episodes and merges proposals only after an explicit action", async () => {
    const initial = fixture();
    initial.entries = [
      {
        episode_id: 103,
        provider_id: 0,
        release_title: "Retained release",
        label: "13",
      },
    ];
    const fetch = setup(undefined, initial);
    await open();
    await userEvent.click(
      screen.getByRole("button", { name: "Load TMDb episode orders" }),
    );
    await userEvent.selectOptions(
      await screen.findByRole("combobox", { name: "TMDb episode order" }),
      "group-one",
    );
    await userEvent.type(
      screen.getByRole("textbox", { name: "Proposed release title" }),
      "Anime release",
    );
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Proposed label type" }),
      "SP",
    );
    await userEvent.clear(
      screen.getByRole("textbox", { name: "First proposed release number" }),
    );
    await userEvent.type(
      screen.getByRole("textbox", { name: "First proposed release number" }),
      "5",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Load order subgroups" }),
    );
    await userEvent.selectOptions(
      await screen.findByRole("combobox", { name: "TMDb order subgroup" }),
      "subgroup-one",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review proposed labels" }),
    );
    const proposal = await screen.findByRole("region", {
      name: "Proposed episode mappings",
    });
    expect(proposal).toHaveTextContent("Unloaded episode");
    expect(
      within(proposal).getByRole("button", {
        name: "Merge proposed mappings into draft",
      }),
    ).toBeDisabled();
    expect(
      screen.getByRole("textbox", {
        name: "Release label for S01E01 · Opening",
      }),
    ).toHaveValue("");
    await userEvent.click(
      within(proposal).getByRole("checkbox", {
        name: "I understand unmatched episodes are excluded",
      }),
    );
    await userEvent.click(
      within(proposal).getByRole("button", {
        name: "Merge proposed mappings into draft",
      }),
    );
    expect(
      screen.getByRole("textbox", {
        name: "Release label for S01E01 · Opening",
      }),
    ).toHaveValue("SP5");
    expect(
      screen.getByRole("textbox", {
        name: "Release label for S02E01 · Return",
      }),
    ).toHaveValue("13");
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    await screen.findByRole("region", {
      name: "Review Anime identity and episode changes",
    });
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([url]) => url.endsWith("/preview"))?.[1]?.body,
        ),
      ).entries,
    ).toHaveLength(2);
  });

  it("rejects invalid and colliding typed labels before a provider-resolving review", async () => {
    const fetch = setup();
    await open();
    await mapSpecial("OVA0", "Anime release");
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("label such as");
    const special = screen.getByRole("textbox", {
      name: "Release label for S00E01 · Special",
    });
    await userEvent.clear(special);
    await userEvent.type(special, "SP01");
    await userEvent.type(
      screen.getByRole("textbox", {
        name: "Release title for S01E01 · Opening",
      }),
      "Anime release",
    );
    await userEvent.type(
      screen.getByRole("textbox", {
        name: "Release label for S01E01 · Opening",
      }),
      "sp1",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "only one canonical episode",
    );
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/preview"))).toBe(
      false,
    );
  });

  it("locks stale or uncertain saved reviews until the settings are reloaded", async () => {
    const fetch = setup((_url, options) =>
      options?.method === "PUT"
        ? Response.json(
            { error: { message: "Episode metadata changed." } },
            { status: 409 },
          )
        : undefined,
    );
    await open();
    await mapSpecial();
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    await screen.findByRole("region", {
      name: "Review Anime identity and episode changes",
    });
    await userEvent.click(
      screen.getByRole("checkbox", {
        name: "I reviewed these Anime identities and episode mappings",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed Anime mapping" }),
    );
    await screen.findByRole("alert");
    expect(
      screen.getByRole("textbox", {
        name: "Release label for S00E01 · Special",
      }),
    ).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "Save reviewed Anime mapping" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Reload saved Anime mapping" }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("textbox", {
          name: "Release label for S00E01 · Special",
        }),
      ).toBeEnabled(),
    );
    expect(
      screen.getByRole("textbox", {
        name: "Release label for S00E01 · Special",
      }),
    ).toHaveValue("");
    expect(
      fetch.mock.calls.filter(([, options]) => options?.method === "PUT"),
    ).toHaveLength(1);
  });

  it("disables editing and cancellation while saving the reviewed token", async () => {
    let resolveSave: ((response: Response) => void) | undefined;
    const pending = new Promise<Response>((resolve) => {
      resolveSave = resolve;
    });
    setup((_url, options) => (options?.method === "PUT" ? pending : undefined));
    await open();
    await mapSpecial();
    await userEvent.click(
      screen.getByRole("button", { name: "Review Anime mapping changes" }),
    );
    await screen.findByRole("region", {
      name: "Review Anime identity and episode changes",
    });
    await userEvent.click(
      screen.getByRole("checkbox", {
        name: "I reviewed these Anime identities and episode mappings",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed Anime mapping" }),
    );
    expect(screen.getByRole("button", { name: "Close editor" })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Cancel review" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("textbox", {
        name: "Release label for S00E01 · Special",
      }),
    ).toBeDisabled();
    await act(async () => {
      resolveSave!(Response.json(fixture()));
    });
    await screen.findByText(/Anime identity and episode mapping saved/);
  });

  it("reviews exact saved pack targets and requires a second release confirmation before queueing", async () => {
    const initial = fixture();
    initial.entries = [
      {
        episode_id: 101,
        provider_id: 1,
        release_title: "Anime release",
        label: "SP1",
      },
      {
        episode_id: 102,
        provider_id: 1,
        release_title: "Anime release",
        label: "1",
      },
    ];
    const fetch = setup(undefined, initial);
    await open();
    const pack = screen.getByRole("region", {
      name: "Manual Anime pack search",
    });
    await userEvent.click(
      within(pack).getByRole("checkbox", {
        name: "Select mapped episodes on this page",
      }),
    );
    await userEvent.type(
      within(pack).getByRole("textbox", { name: "Anime pack search query" }),
      "Owned Anime pack",
    );
    await userEvent.click(
      within(pack).getByRole("button", { name: "Review pack targets" }),
    );
    const review = within(pack).getByRole("region", {
      name: "Review Anime pack targets",
    });
    expect(review).toHaveTextContent("S00E01 · Special");
    expect(review).toHaveTextContent("S01E01 · Opening");
    expect(
      fetch.mock.calls.some(([url]) => url.endsWith("/releases/anime-pack")),
    ).toBe(false);
    await userEvent.click(
      within(review).getByRole("button", {
        name: "Search reviewed pack targets",
      }),
    );
    await userEvent.click(
      await within(pack).findByRole("radio", {
        name: "Choose release Owned Anime pack",
      }),
    );
    expect(
      within(pack).getByRole("button", { name: "Download selected release" }),
    ).toBeDisabled();
    await userEvent.click(
      within(pack).getByRole("checkbox", {
        name: "I reviewed the selected pack release and episode targets",
      }),
    );
    await userEvent.click(
      within(pack).getByRole("button", { name: "Download selected release" }),
    );
    await within(pack).findByRole("link", {
      name: "Open Activity to track and review episode files.",
    });
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([url]) =>
            url.endsWith("/releases/anime-pack"),
          )?.[1]?.body,
        ),
      ),
    ).toEqual({
      series_id: 11,
      episode_ids: [101, 102],
      query: "Owned Anime pack",
      order_revision: 3,
    });
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([url]) => url.endsWith("/downloads"))?.[1]
            ?.body,
        ),
      ),
    ).toEqual({ release_id: "release-one" });
  });
});
