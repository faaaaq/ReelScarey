import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { describe, expect, it, vi } from "vitest";
import { AnimeDubSettings } from "../pages/Library";

function mockFetch(
  handler: (path: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const path = new URL(url, "http://local").pathname;
    return handler(path, options) ?? Response.json({});
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("anime english dub settings", () => {
  it("tests the rule and enables it for every anime show", async () => {
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/anime/preferences" && !options?.method) {
        return Response.json({
          dub_rule: "Eng Dub",
          revision: 1,
          default_rule: "Eng Dub",
        });
      }
      if (
        path === "/api/v1/library/anime/dub-test" &&
        options?.method === "POST"
      ) {
        return Response.json({ matches: true });
      }
      if (
        path === "/api/v1/library/anime/english-dub" &&
        options?.method === "POST"
      ) {
        return Response.json({ shows: 2 });
      }
      return undefined;
    });

    render(
      <Theme>
        <AnimeDubSettings />
      </Theme>,
    );

    await waitFor(() =>
      expect(screen.getByLabelText("English-dub rule")).toHaveValue("Eng Dub"),
    );
    await userEvent.type(
      screen.getByLabelText("Test release title"),
      "Show Eng Dub 01",
    );
    await userEvent.click(screen.getByRole("button", { name: "Test" }));
    expect(await screen.findByText("Matches")).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Enable for all anime" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            new URL(url, "http://local").pathname ===
              "/api/v1/library/anime/english-dub" && options?.method === "POST",
        ),
      ).toBe(true),
    );
    expect(await screen.findByText(/2 Anime shows updated/)).toBeVisible();
  });
});
