import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { RelatedTitles } from "../pages/RelatedTitles";
describe("related titles", () => {
  it("links an explicitly chosen saved show to a movie", async () => {
    let linked = false;
    const show = { id: 2, kind: "series", title: "Fixture Show", year: 2020 };
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "POST") {
        linked = true;
        return Response.json({ saved: true });
      }
      if (url.includes("/imports/catalog"))
        return Response.json({ items: [show] });
      return Response.json({ items: linked ? [show] : [] });
    });
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <MemoryRouter>
          <RelatedTitles id={1} />
        </MemoryRouter>
      </Theme>,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Manage related titles" }),
    );
    await userEvent.selectOptions(
      await screen.findByLabelText("Title type"),
      "series",
    );
    await userEvent.type(
      screen.getByLabelText("Search saved titles"),
      "Fixture",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Search library" }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Link title" }),
    );
    expect(
      await screen.findByRole("link", { name: "Fixture Show" }),
    ).toHaveAttribute("href", "/series/2");
    const call = fetch.mock.calls.find(([, init]) => init?.method === "POST");
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({ other_id: 2 });
  });
});
