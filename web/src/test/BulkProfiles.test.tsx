import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Library } from "../pages/Library";

function setup(
  extra?: (
    url: string,
    o: RequestInit | undefined,
    state: ReturnType<typeof stateForTest>,
  ) => Response | Promise<Response> | undefined,
) {
  const state = stateForTest();
  const fetch = vi.fn(async (url: string, o?: RequestInit) => {
    const custom = await extra?.(url, o, state);
    if (custom) return custom;
    if (url.endsWith("/profiles/preview"))
      return Response.json(reviewForTest(JSON.parse(String(o?.body)), state));
    if (url.endsWith("/profiles") && o?.method === "PUT") {
      const body = JSON.parse(String(o.body));
      let changed = 0;
      for (const item of body.items) {
        const row = state.items.find((v) => v.id === item.id)!;
        const next = body.target.use_default ? null : body.target.profile_id;
        if (row.profile !== next) {
          changed++;
          row.profile = next;
          row.revision++;
        }
      }
      const profile = state.profiles.find(
        (p) => p.id === (body.target.use_default ? 1 : body.target.profile_id),
      )!;
      return Response.json({
        total: body.items.length,
        changed,
        unchanged: body.items.length - changed,
        profile,
        use_default: body.target.use_default,
      });
    }
    return Response.json({
      items: state.items.map((row) => ({
        ...row,
        kind: "movie",
        monitored: row.id === 11,
        poster_url: "",
        status: "missing",
        quality_profile: {
          profile_id: row.profile,
          effective_profile: state.profiles.find(
            (p) => p.id === (row.profile ?? 1),
          ),
        },
      })),
      profile_context: {
        default_profile: state.profiles[0],
        profiles: state.profiles,
      },
      page: 1,
      limit: 24,
      total: 2,
      collection_total: 2,
    });
  });
  vi.stubGlobal("fetch", fetch);
  return { fetch, state };
}
function stateForTest() {
  return {
    profiles: [
      { id: 1, name: "HD balanced", revision: 1 },
      { id: 2, name: "Ultra HD", revision: 1 },
    ],
    items: [
      {
        id: 11,
        title: "First title",
        year: 2024,
        profile: null as number | null,
        revision: 0,
      },
      {
        id: 12,
        title: "Second title",
        year: 2020,
        profile: 2 as number | null,
        revision: 1,
      },
    ],
    targetSize: 100,
  };
}
function reviewForTest(
  body: {
    kind: string;
    ids: number[];
    target: { use_default: boolean; profile_id: number | null };
  },
  state: ReturnType<typeof stateForTest>,
) {
  const target = state.profiles.find(
    (p) => p.id === (body.target.use_default ? 1 : body.target.profile_id),
  )!;
  return {
    target: body.target,
    schedule_revision: 4,
    default_profile: state.profiles[0],
    profiles: state.profiles,
    target_profile: {
      ...target,
      qualities: target.id === 1 ? [720, 1080] : [1080, 2160],
      cutoff: target.id === 1 ? 1080 : 2160,
      language: "any",
      min_seeders: 1,
      max_size_gib: target.id === 1 ? 40 : state.targetSize,
      required_terms: "",
      rejected_terms: "CAM",
    },
    items: body.ids.map((id) => {
      const row = state.items.find((v) => v.id === id)!;
      return {
        ...row,
        profile_id: row.profile,
        effective_profile: state.profiles.find(
          (p) => p.id === (row.profile ?? 1),
        ),
      };
    }),
  };
}
function mount(path = "/movies") {
  render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        <Library kind="movie" />
      </MemoryRouter>
    </Theme>,
  );
}
async function select(name = "First title") {
  await userEvent.click(
    await screen.findByRole("checkbox", { name: `Select ${name}` }),
  );
}
async function open() {
  await userEvent.click(
    screen.getByRole("button", { name: "Assign quality profile" }),
  );
  return await screen.findByRole("dialog");
}
describe("reviewed quality profile assignment", () => {
  it("shows effective/default labels and cancels an exact selection without writing", async () => {
    const { fetch } = setup();
    mount();
    expect(await screen.findByText("HD balanced · default")).toBeVisible();
    expect(screen.getByText("Ultra HD · assigned")).toBeVisible();
    await select();
    const dialog = await open();
    await within(dialog).findByRole("button", { name: "Done" });
    expect(within(dialog).getByText("First title")).toBeVisible();
    expect(within(dialog).queryByText("Second title")).not.toBeInTheDocument();
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Cancel" }),
    );
    expect(fetch.mock.calls.some(([, o]) => o?.method === "PUT")).toBe(false);
  });
  it("shows target rules, applies exact reviewed revisions and refreshes assignment labels", async () => {
    const { fetch } = setup();
    mount();
    await userEvent.click(
      await screen.findByRole("checkbox", {
        name: "Select all titles on this page",
      }),
    );
    const dialog = await open();
    await userEvent.selectOptions(
      within(dialog).getByLabelText("Target quality profile"),
      "2",
    );
    await within(dialog).findByRole("button", { name: "Apply to 1 title" });
    expect(
      within(dialog).getByText(/1 assignment will change; 1 unchanged/),
    ).toBeVisible();
    expect(
      within(dialog).getByRole("region", { name: "Target profile rules" }),
    ).toHaveTextContent("2160p");
    expect(
      within(dialog).getByText(/keep this explicit profile/),
    ).toBeVisible();
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Apply to 1 title" }),
    );
    await screen.findByText("1 title assigned to (Ultra HD). 1 unchanged.");
    const body = JSON.parse(
      String(fetch.mock.calls.find(([, o]) => o?.method === "PUT")?.[1]?.body),
    );
    expect(body).toEqual({
      kind: "movie",
      target: { use_default: false, profile_id: 2 },
      target_id: 2,
      target_revision: 1,
      schedule_revision: 4,
      items: [
        { id: 11, revision: 0, effective_id: 1, effective_revision: 1 },
        { id: 12, revision: 1, effective_id: 2, effective_revision: 1 },
      ],
    });
    await waitFor(() =>
      expect(screen.getAllByText("Ultra HD · assigned")).toHaveLength(2),
    );
    expect(
      screen.getAllByText("Paused", { selector: ".catalog-card-meta span" }),
    ).toHaveLength(1);
  });
  it("treats pinning the current default profile as a real assignment change", async () => {
    const { fetch } = setup();
    mount();
    await select();
    const dialog = await open();
    await within(dialog).findByRole("button", { name: "Done" });
    await userEvent.selectOptions(
      within(dialog).getByLabelText("Target quality profile"),
      "1",
    );
    await userEvent.click(
      await within(dialog).findByRole("button", { name: "Apply to 1 title" }),
    );
    await screen.findByText("1 title assigned to (HD balanced). 0 unchanged.");
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([, o]) => o?.method === "PUT")?.[1]?.body,
        ),
      ).target.use_default,
    ).toBe(false);
  });
  it("returns an explicit title to inheritance and finishes an unchanged review without a second write", async () => {
    const { fetch } = setup();
    mount();
    await select("Second title");
    let dialog = await open();
    await userEvent.click(
      await within(dialog).findByRole("button", { name: "Apply to 1 title" }),
    );
    await screen.findByText(
      "1 title now follows the schedule default (HD balanced). 0 unchanged.",
    );
    await select("Second title");
    dialog = await open();
    await userEvent.click(
      await within(dialog).findByRole("button", { name: "Done" }),
    );
    await screen.findByText(/No changes were needed/);
    const writes = fetch.mock.calls.filter(([, o]) => o?.method === "PUT");
    expect(writes).toHaveLength(1);
    expect(JSON.parse(String(writes[0][1]?.body)).target).toEqual({
      use_default: true,
      profile_id: null,
    });
  });
  it("requires fresh rules and a new confirmation after an apply conflict", async () => {
    let writes = 0;
    const { fetch } = setup((_, o, state) => {
      if (o?.method === "PUT" && ++writes === 1) {
        state.profiles[1].revision = 2;
        state.targetSize = 99;
        return Response.json(
          {
            error: {
              message: "Profile rules changed. No titles were changed.",
            },
          },
          { status: 409 },
        );
      }
    });
    mount();
    await select();
    const dialog = await open();
    await userEvent.selectOptions(
      within(dialog).getByLabelText("Target quality profile"),
      "2",
    );
    await userEvent.click(
      await within(dialog).findByRole("button", { name: "Apply to 1 title" }),
    );
    await within(dialog).findByRole("alert");
    expect(writes).toBe(1);
    expect(
      within(dialog).queryByRole("button", { name: "Apply to 1 title" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Review again" }),
    );
    await within(dialog).findByRole("button", { name: "Apply to 1 title" });
    expect(
      within(dialog).getByRole("region", { name: "Target profile rules" }),
    ).toHaveTextContent("99 GiB");
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Apply to 1 title" }),
    );
    await screen.findByText("1 title assigned to (Ultra HD). 0 unchanged.");
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.filter(([, o]) => o?.method === "PUT")[1][1]?.body,
        ),
      ).target_revision,
    ).toBe(2);
  });
  it("locks the target and confirmation while saving", async () => {
    let finish!: (value: Response) => void;
    const { fetch } = setup((_, o) =>
      o?.method === "PUT"
        ? new Promise<Response>((resolve) => {
            finish = resolve;
          })
        : undefined,
    );
    mount();
    await select();
    const dialog = await open();
    await userEvent.selectOptions(
      within(dialog).getByLabelText("Target quality profile"),
      "2",
    );
    await userEvent.click(
      await within(dialog).findByRole("button", { name: "Apply to 1 title" }),
    );
    expect(
      within(dialog).getByLabelText("Target quality profile"),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("button", { name: "Saving…" }),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("button", { name: "Cancel" }),
    ).toBeDisabled();
    await userEvent.keyboard("{Escape}");
    expect(screen.getByRole("dialog")).toBeVisible();
    finish(
      Response.json({
        total: 1,
        changed: 1,
        unchanged: 0,
        profile: { id: 2, name: "Ultra HD", revision: 1 },
        use_default: false,
      }),
    );
    await screen.findByText("1 title assigned to (Ultra HD). 0 unchanged.");
    expect(
      fetch.mock.calls.filter(([, o]) => o?.method === "PUT"),
    ).toHaveLength(1);
  });
  it("keeps bookmarked profile filters, preserves selection across views and clears it on filter changes", async () => {
    const { fetch } = setup();
    mount("/movies?profile=default&view=table&sort=year_desc");
    expect(await screen.findByLabelText("Library quality profile")).toHaveValue(
      "default",
    );
    await select();
    await userEvent.click(screen.getByRole("button", { name: "Poster view" }));
    expect(
      screen.getByRole("checkbox", { name: "Select First title" }),
    ).toBeChecked();
    await userEvent.selectOptions(
      screen.getByLabelText("Library quality profile"),
      "2",
    );
    await waitFor(() =>
      expect(
        screen.getByRole("checkbox", { name: "Select First title" }),
      ).not.toBeChecked(),
    );
    expect(screen.getByLabelText("Library quality profile")).toHaveValue("2");
    const q = new URL(fetch.mock.calls.at(-1)![0], "http://local").searchParams;
    expect(q.get("profile")).toBe("2");
    expect(q.get("sort")).toBe("year_desc");
    expect(q.get("page")).toBe("1");
  });
  it("ignores a late preview for the previous target", async () => {
    let finishDefault!: (r: Response) => void;
    let previous!: ReturnType<typeof reviewForTest>;
    setup((url, o, state) => {
      if (url.endsWith("/profiles/preview")) {
        const body = JSON.parse(String(o?.body));
        if (body.target.use_default) {
          previous = reviewForTest(body, state);
          return new Promise<Response>((resolve) => {
            finishDefault = resolve;
          });
        }
      }
    });
    mount();
    await select();
    const dialog = await open();
    await userEvent.selectOptions(
      within(dialog).getByLabelText("Target quality profile"),
      "2",
    );
    await within(dialog).findByRole("button", { name: "Apply to 1 title" });
    finishDefault(Response.json(previous));
    await waitFor(() =>
      expect(
        within(dialog).getByRole("region", { name: "Target profile rules" }),
      ).toHaveTextContent("Ultra HD"),
    );
    expect(within(dialog).getByLabelText("Target quality profile")).toHaveValue(
      "2",
    );
  });
});
