import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { describe, expect, it, vi } from "vitest";
import { AnimeSearch, type AnimeSearchPolicy } from "../pages/AnimeSearch";

function fixture(): AnimeSearchPolicy {
  return {
    media_id: 11,
    canonical_title: "Anime serial",
    category: "anime",
    revision: 0,
    category_revision: 3,
    numbering_revision: 7,
    mode: "disabled",
    search_title: "",
    alternate_titles: [],
    loaded_count: 3,
    ready_count: 0,
    missing_number_count: 2,
    include_specials: false,
    allow_packs: false,
    max_pack_episodes: 20,
    order_revision: 4,
    approved_order_revision: 0,
    mapped_count: 2,
    missing_mapping_count: 1,
  };
}
function setup(
  extra?: (
    url: string,
    options: RequestInit | undefined,
    state: AnimeSearchPolicy,
  ) => Response | Promise<Response> | undefined,
  initial = fixture(),
) {
  let state = initial;
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const custom = await extra?.(url, options, state);
    if (custom) return custom;
    if (options?.method === "PUT") {
      const body = JSON.parse(String(options.body));
      state = {
        ...state,
        ...body,
        revision: state.revision + 1,
        approved_order_revision:
          body.mode === "mapped"
            ? body.order_revision
            : state.approved_order_revision,
        ready_count:
          body.mode === "absolute" ? 1 : body.mode === "season" ? 3 : 0,
      };
    }
    return Response.json(state);
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <AnimeSearch id={11} />
    </Theme>,
  );
  return fetch;
}
async function open() {
  await userEvent.click(
    screen.getByRole("button", { name: "Edit automatic searches" }),
  );
  await screen.findByRole("combobox", {
    name: "Anime automatic search numbering",
  });
}
const mode = () =>
  screen.getByRole("combobox", { name: "Anime automatic search numbering" });
const aliases = () =>
  screen.getByRole("textbox", { name: "Anime alternate release titles" });

describe("Explicit Anime automatic search policy", () => {
  it("reviews saved mapping consent separately for specials and packs and preserves other title settings", async () => {
    const fetch = setup(undefined, {
      ...fixture(),
      search_title: "Old query",
      alternate_titles: ["Old query"],
    });
    await open();
    await userEvent.selectOptions(mode(), "mapped");
    const specials = screen.getByRole("checkbox", {
      name: "Include mapped specials",
    });
    const packs = screen.getByRole("checkbox", {
      name: "Allow mapped episode packs",
    });
    expect(specials).not.toBeChecked();
    expect(packs).not.toBeChecked();
    expect(
      screen.getByRole("spinbutton", {
        name: "Maximum episodes per mapped pack",
      }),
    ).toBeDisabled();
    expect(
      screen.queryByRole("textbox", { name: "Anime alternate release titles" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(/every mapped episode in that group must qualify/i),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    expect(
      screen.getByRole("region", {
        name: "Review Anime automatic search changes",
      }),
    ).toHaveTextContent("Mapping revision being approved4");
    await userEvent.click(specials);
    expect(
      screen.queryByRole("button", { name: "Save reviewed search settings" }),
    ).not.toBeInTheDocument();
    await userEvent.click(packs);
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    const review = screen.getByRole("region", {
      name: "Review Anime automatic search changes",
    });
    expect(review).toHaveTextContent("Mapped specialsIncluded");
    expect(review).toHaveTextContent("Allowed, up to 20 episodes");
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed search settings" }),
    );
    await screen.findByText(/Anime search settings saved/);
    const put = fetch.mock.calls.find(
      ([, options]) => options?.method === "PUT",
    );
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({
      revision: 0,
      category_revision: 3,
      numbering_revision: 7,
      mode: "mapped",
      search_title: "Old query",
      alternate_titles: ["Old query"],
      include_specials: true,
      allow_packs: true,
      max_pack_episodes: 20,
      order_revision: 4,
    });
  });

  it("requires fresh approval of changed provider mappings even when options are unchanged", async () => {
    const fetch = setup(undefined, {
      ...fixture(),
      mode: "mapped",
      approved_order_revision: 3,
    });
    await open();
    expect(
      screen.getByText(/current provider mappings have not been approved/),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Review search changes" }),
    ).toBeEnabled();
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed search settings" }),
    );
    await screen.findByText(/Anime search settings saved/);
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Review search changes" }),
      ).toBeDisabled(),
    );
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([, options]) => options?.method === "PUT")?.[1]
            ?.body,
        ),
      ).order_revision,
    ).toBe(4);
  });

  it("cannot approve mapped searching without saved loaded episode mappings", async () => {
    const fetch = setup(undefined, {
      ...fixture(),
      order_revision: 0,
      mapped_count: 0,
    });
    await open();
    await userEvent.selectOptions(mode(), "mapped");
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Save at least one loaded episode mapping",
    );
    expect(
      screen.queryByRole("button", { name: "Save reviewed search settings" }),
    ).not.toBeInTheDocument();
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
  });

  it("bounds explicit range packs to 2–100 episodes and cancels review without a write", async () => {
    const fetch = setup();
    await open();
    await userEvent.selectOptions(mode(), "mapped");
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Allow mapped episode packs" }),
    );
    const limit = screen.getByRole("spinbutton", {
      name: "Maximum episodes per mapped pack",
    });
    for (const value of ["1", "101", "2.5"]) {
      await userEvent.clear(limit);
      await userEvent.type(limit, value);
      await userEvent.click(
        screen.getByRole("button", { name: "Review search changes" }),
      );
      expect(screen.getByRole("alert")).toHaveTextContent("2–100");
    }
    await userEvent.clear(limit);
    await userEvent.type(limit, "100");
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    expect(
      screen.getByRole("region", {
        name: "Review Anime automatic search changes",
      }),
    ).toHaveTextContent("up to 100 episodes");
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
  });

  it("locks stale mapped consent and reloads the new mapping revision before another review", async () => {
    let failed = false;
    const fetch = setup((_url, options, state) => {
      if (options?.method === "PUT") {
        failed = true;
        return Response.json(
          { error: { message: "Provider mappings changed." } },
          { status: 409 },
        );
      }
      if (failed)
        return Response.json({
          ...state,
          order_revision: 5,
          numbering_revision: 8,
        });
    });
    await open();
    await userEvent.selectOptions(mode(), "mapped");
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Include mapped specials" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed search settings" }),
    );
    await screen.findByRole("alert");
    expect(
      screen.getByRole("checkbox", { name: "Include mapped specials" }),
    ).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "Save reviewed search settings" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Reload saved search policy" }),
    );
    await waitFor(() => expect(mode()).toHaveValue("disabled"));
    await userEvent.selectOptions(mode(), "mapped");
    expect(
      screen.getByRole("checkbox", { name: "Include mapped specials" }),
    ).not.toBeChecked();
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed search settings" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.filter(([, options]) => options?.method === "PUT"),
      ).toHaveLength(2),
    );
    expect(
      JSON.parse(
        String(
          fetch.mock.calls
            .filter(([, options]) => options?.method === "PUT")
            .at(-1)?.[1]?.body,
        ),
      ),
    ).toMatchObject({
      order_revision: 5,
      numbering_revision: 8,
      include_specials: false,
    });
  });
  it("loads on demand with searches off and shows saved numbering readiness without a write", async () => {
    const fetch = setup();
    expect(fetch).not.toHaveBeenCalled();
    await open();
    expect(mode()).toHaveValue("disabled");
    expect(screen.getByText("Ready under saved settings")).toBeVisible();
    expect(screen.getByText("Without an absolute number")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Review search changes" }),
    ).toBeDisabled();
    expect(fetch.mock.calls).toHaveLength(1);
    expect(fetch.mock.calls[0][0]).toBe(
      "/api/v1/library/items/11/anime-search",
    );
    expect(fetch.mock.calls[0][1]?.method).toBeUndefined();
    expect(
      screen.getByText(/Completed files still require mapping review/),
    ).toBeVisible();
  });

  it("reviews explicit numbering and alternate names, then cancels without changing policy", async () => {
    const fetch = setup();
    await open();
    await userEvent.selectOptions(mode(), "absolute");
    await userEvent.type(aliases(), "Anime Romaji\nAnime translated");
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Anime search title" }),
      "Anime Romaji",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    const review = screen.getByRole("region", {
      name: "Review Anime automatic search changes",
    });
    expect(review).toHaveTextContent("Off → Absolute episode number");
    expect(review).toHaveTextContent("Anime Romaji");
    expect(review).toHaveTextContent("Anime translated");
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      screen.getByRole("button", { name: "Edit automatic searches" }),
    ).toBeVisible();
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
  });

  it("submits only the reviewed per-show policy and exact revisions, then reloads saved settings", async () => {
    const fetch = setup();
    await open();
    await userEvent.selectOptions(mode(), "absolute");
    await userEvent.type(aliases(), "Anime Romaji\nAnime translated");
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Anime search title" }),
      "Anime Romaji",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed search settings" }),
    );
    await screen.findByText(/Anime search settings saved/);
    await waitFor(() => expect(mode()).toHaveValue("absolute"));
    expect(aliases()).toHaveValue("Anime Romaji\nAnime translated");
    const put = fetch.mock.calls.find(
      ([, options]) => options?.method === "PUT",
    );
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({
      revision: 0,
      category_revision: 3,
      numbering_revision: 7,
      mode: "absolute",
      search_title: "Anime Romaji",
      alternate_titles: ["Anime Romaji", "Anime translated"],
      include_specials: false,
      allow_packs: false,
      max_pack_episodes: 20,
    });
    expect(
      fetch.mock.calls.filter(([, options]) => !options?.method),
    ).toHaveLength(2);
    expect(
      fetch.mock.calls.every(
        ([url]) => url === "/api/v1/library/items/11/anime-search",
      ),
    ).toBe(true);
    expect(
      screen.getByRole("button", { name: "Review search changes" }),
    ).toBeDisabled();
  });

  it("uses an empty search_title for canonical searches and invalidates review after edits", async () => {
    const fetch = setup();
    await open();
    await userEvent.selectOptions(mode(), "season");
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    expect(
      screen.getByRole("button", { name: "Save reviewed search settings" }),
    ).toBeEnabled();
    await userEvent.type(aliases(), "Anime alternative");
    expect(
      screen.queryByRole("button", { name: "Save reviewed search settings" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed search settings" }),
    );
    await screen.findByText(/Anime search settings saved/);
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([, options]) => options?.method === "PUT")?.[1]
            ?.body,
        ),
      ).search_title,
    ).toBe("");
  });

  it("rejects excessive, duplicated and oversized UTF-8 aliases before review", async () => {
    const fetch = setup();
    await open();
    await userEvent.type(
      aliases(),
      Array.from({ length: 9 }, (_, n) => `Alias ${n + 1}`).join("\n"),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("at most eight");
    await userEvent.clear(aliases());
    await userEvent.type(aliases(), "Alias title\nalias.title");
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("distinct");
    await userEvent.clear(aliases());
    await userEvent.type(aliases(), "日".repeat(67));
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("at most 200 bytes");
    expect(
      screen.queryByRole("button", { name: "Save reviewed search settings" }),
    ).not.toBeInTheDocument();
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "PUT"),
    ).toBe(false);
  });

  it("requires fresh saved policy and numbering after a conflict", async () => {
    let failed = false;
    const fetch = setup((_url, options, state) => {
      if (options?.method === "PUT") {
        failed = true;
        return Response.json(
          { error: { message: "Episode numbering changed." } },
          { status: 409 },
        );
      }
      if (failed)
        return Response.json({ ...state, revision: 2, numbering_revision: 8 });
    });
    await open();
    await userEvent.selectOptions(mode(), "season");
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed search settings" }),
    );
    await screen.findByRole("alert");
    expect(mode()).toBeDisabled();
    expect(aliases()).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "Save reviewed search settings" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Reload saved search policy" }),
    );
    await waitFor(() => expect(mode()).toBeEnabled());
    expect(mode()).toHaveValue("disabled");
    await userEvent.selectOptions(mode(), "season");
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed search settings" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.filter(([, options]) => options?.method === "PUT"),
      ).toHaveLength(2),
    );
    const latest = JSON.parse(
      String(
        fetch.mock.calls
          .filter(([, options]) => options?.method === "PUT")
          .at(-1)?.[1]?.body,
      ),
    );
    expect(latest).toMatchObject({
      revision: 2,
      category_revision: 3,
      numbering_revision: 8,
    });
  });

  it("locks the reviewed form while saving", async () => {
    let resolveSave: ((response: Response) => void) | undefined;
    const pending = new Promise<Response>((resolve) => {
      resolveSave = resolve;
    });
    setup((_url, options) => (options?.method === "PUT" ? pending : undefined));
    await open();
    await userEvent.selectOptions(mode(), "season");
    await userEvent.click(
      screen.getByRole("button", { name: "Review search changes" }),
    );
    const review = screen.getByRole("region", {
      name: "Review Anime automatic search changes",
    });
    expect(within(review).getByText("Anime serial")).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed search settings" }),
    );
    expect(mode()).toBeDisabled();
    expect(aliases()).toBeDisabled();
    expect(
      screen.getByRole("combobox", { name: "Anime search title" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled();
    await act(async () => {
      resolveSave!(
        Response.json({ ...fixture(), mode: "season", revision: 1 }),
      );
    });
    await screen.findByText(/Anime search settings saved/);
  });
});
