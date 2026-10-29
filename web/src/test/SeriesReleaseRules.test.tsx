import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { describe, expect, it, vi } from "vitest";
import { SeriesReleaseRules } from "../pages/SeriesReleaseRules";

describe("series release rules", () => {
  it("loads and saves codec and release group for a single show", async () => {
    const fetch = vi.fn(async (_url: string, init?: RequestInit) =>
      Response.json(
        init?.method === "PUT"
          ? { ...JSON.parse(String(init.body)), revision: 1 }
          : { codec: "", group: "", revision: 0 },
      ),
    );
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <SeriesReleaseRules id={5} />
      </Theme>,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Edit release rules" }),
    );
    await userEvent.selectOptions(
      await screen.findByLabelText("Required codec"),
      "h265",
    );
    await userEvent.type(
      screen.getByLabelText("Required release group"),
      "MeGusta",
    );
    await userEvent.click(screen.getByRole("button", { name: "Save rules" }));
    expect(await screen.findByText("Release rules saved.")).toBeVisible();
    const call = fetch.mock.calls.find(([, init]) => init?.method === "PUT");
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      codec: "h265",
      group: "MeGusta",
      revision: 0,
    });
  });
});
