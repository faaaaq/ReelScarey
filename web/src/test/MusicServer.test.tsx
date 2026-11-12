import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicServer } from "../pages/MusicServer";
import type { MusicServerUser } from "../api";

function mockFetch(handler: (path: string, options?: RequestInit) => Response) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      const path = new URL(url, "http://local").pathname;
      return handler(path, options);
    }),
  );
}

const alex: MusicServerUser = {
  id: 1,
  username: "alex",
  email: "alex@example.com",
  password_generated: true,
  scrobbling_enabled: true,
  admin_role: false,
  settings_role: false,
  download_role: true,
  upload_role: false,
  playlist_role: true,
  cover_art_role: true,
  share_role: false,
  created_at: 1,
  updated_at: 1,
  last_used_at: 0,
};

function renderPage() {
  return render(
    <Theme>
      <MemoryRouter initialEntries={["/music/server"]}>
        <MusicServer />
      </MemoryRouter>
    </Theme>,
  );
}

describe("music server", () => {
  it("lists accounts and reveals a generated password once", async () => {
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/server" && !options?.method) {
        return Response.json({
          enabled: true,
          configured: true,
          token_hint: "",
          users: 1,
          revision: 1,
          updated_at: 1,
        });
      }
      if (path === "/api/v1/library/music/server/users" && !options?.method) {
        return Response.json({ users: [alex] });
      }
      if (
        path === "/api/v1/library/music/server/users" &&
        options?.method === "POST"
      ) {
        return Response.json(
          {
            user: { ...alex, id: 2, username: "sam", email: "" },
            password: "generated-test-password",
          },
          { status: 201 },
        );
      }
      return Response.json({});
    });

    renderPage();

    expect(
      await screen.findByRole("heading", { name: "Music server" }),
    ).toBeVisible();
    expect(screen.getByText("Enabled")).toBeVisible();
    expect(screen.getByText("1 account")).toBeVisible();
    expect(await screen.findByText("alex")).toBeVisible();
    expect(screen.getByText("Never")).toBeVisible();

    await userEvent.type(screen.getByPlaceholderText("alex"), "sam");
    await userEvent.click(screen.getByRole("button", { name: /Add account/ }));

    expect(
      await screen.findByDisplayValue("generated-test-password"),
    ).toBeVisible();

    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    await userEvent.click(
      screen.getByRole("button", { name: "Copy password" }),
    );
    expect(writeText).toHaveBeenCalledWith("generated-test-password");
    expect(await screen.findByRole("button", { name: "Copied" })).toBeVisible();
  });

  it("resets an account password and shows the new value once", async () => {
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/server" && !options?.method) {
        return Response.json({
          enabled: true,
          configured: true,
          token_hint: "",
          users: 1,
          revision: 1,
          updated_at: 1,
        });
      }
      if (path === "/api/v1/library/music/server/users" && !options?.method) {
        return Response.json({ users: [alex] });
      }
      if (
        path === "/api/v1/library/music/server/users/1/password" &&
        options?.method === "POST"
      ) {
        return Response.json({ user: alex, password: "reset-test-password" });
      }
      return Response.json({});
    });

    renderPage();

    await screen.findByText("alex");
    await userEvent.click(
      screen.getByRole("button", { name: /Reset password/ }),
    );
    expect(
      await screen.findByDisplayValue("reset-test-password"),
    ).toBeVisible();
  });

  it("generates a legacy shared token once", async () => {
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/server" && !options?.method) {
        return Response.json({
          enabled: false,
          configured: false,
          token_hint: "",
          users: 0,
          revision: 1,
          updated_at: 1,
        });
      }
      if (path === "/api/v1/library/music/server/users" && !options?.method) {
        return Response.json({ users: [] });
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
              token_hint: "\u2022\u2022\u2022\u2022oken",
              users: 0,
              revision: 1,
              updated_at: 1,
            },
          },
          { status: 201 },
        );
      }
      return Response.json({});
    });

    renderPage();

    expect(
      await screen.findByRole("heading", { name: "Music server" }),
    ).toBeVisible();
    expect(screen.getByText("Disabled")).toBeVisible();
    expect(screen.getByText("Opt-in and same port")).toBeVisible();
    expect(await screen.findByText("No accounts yet")).toBeVisible();

    await userEvent.click(
      screen.getByRole("button", { name: /Generate token/ }),
    );
    expect(
      await screen.findByDisplayValue("generated-test-token"),
    ).toBeVisible();
  });
});
