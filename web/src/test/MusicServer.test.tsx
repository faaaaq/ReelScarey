import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicServer } from "../pages/MusicServer";

function mockFetch(handler: (path: string, options?: RequestInit) => Response) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      const path = new URL(url, "http://local").pathname;
      return handler(path, options);
    }),
  );
}

describe("music server", () => {
  it("shows the opt-in state and reveals a generated token once", async () => {
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/server" && !options?.method) {
        return Response.json({
          enabled: false,
          configured: false,
          token_hint: "",
          revision: 1,
          updated_at: 1,
        });
      }
      if (
        path === "/api/v1/library/music/server/token" &&
        options?.method === "POST"
      ) {
        return Response.json(
          {
            token: "generated-test-token",
            server: {
              enabled: false,
              configured: true,
              token_hint: "••••oken",
              revision: 1,
              updated_at: 1,
            },
          },
          { status: 201 },
        );
      }
      return Response.json({});
    });

    render(
      <Theme>
        <MemoryRouter initialEntries={["/music/server"]}>
          <MusicServer />
        </MemoryRouter>
      </Theme>,
    );

    expect(
      await screen.findByRole("heading", { name: "Music server" }),
    ).toBeVisible();
    expect(screen.getByText("Disabled")).toBeVisible();
    expect(screen.getByText("Opt-in and same port")).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Generate token" }),
    );
    expect(
      await screen.findByDisplayValue("generated-test-token"),
    ).toBeVisible();
  });
});
