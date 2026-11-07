import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Library } from "../pages/Library";

const titles = [
  {
    id: 11,
    title: "Saved title",
    year: 2024,
    monitored: true,
    kind: "movie",
    poster_url: "",
    status: "missing",
  },
  {
    id: 12,
    title: "Second saved title",
    year: 2020,
    monitored: false,
    kind: "movie",
    poster_url: "",
    status: "missing",
  },
];
type Extra = (
  url: string,
  options?: RequestInit,
) => Response | Promise<Response> | undefined;
function setup(extra?: Extra) {
  let saved = titles.map((v) => ({ ...v }));
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const custom = await extra?.(url, options);
    if (custom) return custom;
    if (url.endsWith("/monitoring/preview")) {
      const body = JSON.parse(String(options?.body));
      return Response.json({
        kind: body.kind,
        items: body.ids.map((id: number) => {
          const item = saved.find((v) => v.id === id)!;
          return {
            id,
            title: item.title,
            year: item.year,
            monitored: item.monitored,
            series_revision: body.kind === "series" ? 4 : 0,
            automation_revision: 0,
          };
        }),
      });
    }
    if (options?.method === "PUT") {
      const body = JSON.parse(String(options.body));
      let changed = 0;
      for (const expectation of body.items) {
        const item = saved.find((v) => v.id === expectation.id)!;
        if (item.monitored !== body.monitored) changed++;
        item.monitored = body.monitored;
      }
      return Response.json({
        total: body.items.length,
        changed,
        unchanged: body.items.length - changed,
        monitored: body.monitored,
      });
    }
    return Response.json({
      items: saved,
      page: Number(new URL(url, "http://local").searchParams.get("page") ?? 1),
      limit: 24,
      total: 26,
      collection_total: 26,
    });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
function mount(kind: "movie" | "series" = "movie") {
  render(
    <Theme>
      <MemoryRouter
        initialEntries={[kind === "series" ? "/series" : "/movies"]}
      >
        <Library kind={kind} />
      </MemoryRouter>
    </Theme>,
  );
}
async function enterSelectMode() {
  await userEvent.click(await screen.findByRole("button", { name: "Select" }));
}
async function selectOne() {
  await enterSelectMode();
  await screen.findByRole("checkbox", { name: "Select Saved title" });
  await userEvent.click(
    screen.getByRole("checkbox", { name: "Select Saved title" }),
  );
}
async function review() {
  await userEvent.click(
    screen.getByRole("button", { name: "Review monitoring" }),
  );
  return await screen.findByRole("dialog");
}
describe("reviewed bulk monitoring", () => {
  it("previews only selected IDs and cancels without a write", async () => {
    const fetch = setup();
    mount();
    await selectOne();
    const dialog = await review();
    expect(await within(dialog).findByText("Saved title")).toBeVisible();
    expect(
      within(dialog).queryByText("Second saved title"),
    ).not.toBeInTheDocument();
    expect(
      JSON.parse(
        String(
          fetch.mock.calls.find(([url]) =>
            url.endsWith("/monitoring/preview"),
          )?.[1]?.body,
        ),
      ),
    ).toEqual({ kind: "movie", ids: [11] });
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Cancel" }),
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(fetch.mock.calls.some(([, o]) => o?.method === "PUT")).toBe(false);
    expect(
      screen.getByRole("checkbox", { name: "Select Saved title" }),
    ).toBeChecked();
  });
  it("reviews current state and applies exact expectations, then refreshes the visible catalog", async () => {
    const fetch = setup();
    mount();
    await screen.findByRole("link", { name: "Open Saved title details" });
    expect(
      screen.queryByRole("checkbox", { name: "Select Saved title" }),
    ).not.toBeInTheDocument();
    await enterSelectMode();
    expect(screen.getByText("0 selected")).toBeVisible();
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Select all shown" }),
    );
    const dialog = await review();
    await within(dialog).findByRole("button", { name: "Pause 1 title" });
    expect(
      within(dialog).getByText(/1 title will change; 1 already paused/),
    ).toBeVisible();
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Pause 1 title" }),
    );
    expect(
      await screen.findByText("Paused 1 title. 1 unchanged."),
    ).toBeVisible();
    await waitFor(() =>
      expect(
        screen.getByRole("checkbox", { name: "Select Saved title" }),
      ).not.toBeChecked(),
    );
    const body = JSON.parse(
      String(fetch.mock.calls.find(([, o]) => o?.method === "PUT")?.[1]?.body),
    );
    expect(body).toEqual({
      kind: "movie",
      monitored: false,
      items: [
        { id: 11, monitored: true, series_revision: 0, automation_revision: 0 },
        {
          id: 12,
          monitored: false,
          series_revision: 0,
          automation_revision: 0,
        },
      ],
    });
    expect(
      screen.queryByRole("region", { name: "Selected titles" }),
    ).not.toBeInTheDocument();
    const libraryCalls = fetch.mock.calls
      .map(([url]) => String(url))
      .filter((url) => url.includes("/library?"));
    expect(libraryCalls.length).toBeGreaterThanOrEqual(2);
    expect(libraryCalls.at(-1)).toContain("page=1");
  });
  it("preserves selection between layouts and clears it on filter or page changes", async () => {
    setup();
    mount();
    await selectOne();
    expect(
      screen.getByRole("checkbox", { name: "Select all shown" }),
    ).toBePartiallyChecked();
    expect(screen.getByText("1 selected")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Table" }));
    expect(
      screen.getByRole("checkbox", { name: "Select Saved title" }),
    ).toBeChecked();
    await userEvent.click(screen.getByRole("button", { name: "Filter" }));
    await userEvent.selectOptions(
      screen.getByLabelText("Library monitoring"),
      "paused",
    );
    await waitFor(() =>
      expect(
        screen.getByRole("checkbox", { name: "Select Saved title" }),
      ).not.toBeChecked(),
    );
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Select Saved title" }),
    );
    await userEvent.click(screen.getByRole("button", { name: /Next/ }));
    await waitFor(() =>
      expect(
        screen.queryByRole("region", { name: "Selected titles" }),
      ).not.toBeInTheDocument(),
    );
  });
  it("explains show-level resume and preserves the review's series revision", async () => {
    const fetch = setup();
    mount("series");
    await selectOne();
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Select Second saved title" }),
    );
    await userEvent.selectOptions(
      screen.getByLabelText("Monitoring action"),
      "resume",
    );
    const dialog = await review();
    await within(dialog).findByRole("button", { name: "Resume 1 title" });
    expect(
      within(dialog).getByText(/Season and episode choices are retained/),
    ).toBeVisible();
    expect(
      within(dialog).getByText(/enabled automatic searches/),
    ).toBeVisible();
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Resume 1 title" }),
    );
    await screen.findByText("Resumed 1 title. 1 unchanged.");
    const body = JSON.parse(
      String(fetch.mock.calls.find(([, o]) => o?.method === "PUT")?.[1]?.body),
    );
    expect(body.kind).toBe("series");
    expect(body.items[0].series_revision).toBe(4);
  });
  it("requires another review after a conflict and never silently retries the write", async () => {
    let reviews = 0,
      writes = 0;
    const fetch = setup((url, o) => {
      if (url.endsWith("/monitoring/preview")) {
        reviews++;
        return Response.json({
          kind: "movie",
          items: [
            {
              ...titles[0],
              series_revision: 0,
              automation_revision: reviews - 1,
            },
          ],
        });
      }
      if (o?.method === "PUT" && ++writes === 1)
        return Response.json(
          {
            error: {
              message: "Selected titles changed. No titles were changed.",
            },
          },
          { status: 409 },
        );
    });
    mount();
    await selectOne();
    let dialog = await review();
    await userEvent.click(
      await within(dialog).findByRole("button", { name: "Pause 1 title" }),
    );
    await within(dialog).findByRole("alert");
    expect(writes).toBe(1);
    expect(
      within(dialog).queryByRole("button", { name: "Pause 1 title" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Review again" }),
    );
    await userEvent.click(
      await within(dialog).findByRole("button", { name: "Pause 1 title" }),
    );
    await screen.findByText("Paused 1 title. 0 unchanged.");
    expect(reviews).toBe(2);
    expect(writes).toBe(2);
    const puts = fetch.mock.calls.filter(([, o]) => o?.method === "PUT");
    expect(
      JSON.parse(String(puts[1][1]?.body)).items[0].automation_revision,
    ).toBe(1);
  });
  it("finishes an unchanged selection without sending a mutation", async () => {
    const fetch = setup();
    mount();
    await enterSelectMode();
    await screen.findByRole("checkbox", { name: "Select Second saved title" });
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Select Second saved title" }),
    );
    const dialog = await review();
    await userEvent.click(
      await within(dialog).findByRole("button", { name: "Done" }),
    );
    expect(await screen.findByText(/No changes were needed/)).toBeVisible();
    expect(fetch.mock.calls.some(([, o]) => o?.method === "PUT")).toBe(false);
  });
  it("locks confirmation and cancel while saving and sends one write", async () => {
    let finish!: (value: Response) => void;
    const fetch = setup((_, o) =>
      o?.method === "PUT"
        ? new Promise<Response>((resolve) => {
            finish = resolve;
          })
        : undefined,
    );
    mount();
    await selectOne();
    const dialog = await review();
    await userEvent.click(
      await within(dialog).findByRole("button", { name: "Pause 1 title" }),
    );
    expect(
      within(dialog).getByRole("button", { name: "Saving…" }),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("button", { name: "Cancel" }),
    ).toBeDisabled();
    await userEvent.keyboard("{Escape}");
    expect(screen.getByRole("dialog")).toBeVisible();
    finish(
      Response.json({ total: 1, changed: 1, unchanged: 0, monitored: false }),
    );
    await screen.findByText("Paused 1 title. 0 unchanged.");
    expect(
      fetch.mock.calls.filter(([, o]) => o?.method === "PUT"),
    ).toHaveLength(1);
  });
  it("shows preview failures without an apply action and allows cancellation", async () => {
    const fetch = setup((url) =>
      url.endsWith("/monitoring/preview")
        ? Response.json(
            { error: { message: "A selected title no longer exists." } },
            { status: 404 },
          )
        : undefined,
    );
    mount();
    await selectOne();
    const dialog = await review();
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "no longer exists",
    );
    expect(
      within(dialog).queryByRole("button", { name: "Pause 1 title" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Cancel" }),
    );
    expect(fetch.mock.calls.some(([, o]) => o?.method === "PUT")).toBe(false);
  });
});
