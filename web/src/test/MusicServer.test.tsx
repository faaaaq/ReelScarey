import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { MusicServer } from "../pages/MusicServer";
import type { MusicServerUser } from "../api";

function mockFetch(handler: (path: string, options?: RequestInit) => Response) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const path = new URL(url, "http://local").pathname;
    return handler(path, options);
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
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

function server(users: number, enabled = true) {
  return Response.json({
    enabled,
    configured: enabled,
    token_hint: "",
    users,
    revision: 1,
    updated_at: 1,
  });
}

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
  it("links identity management to Accounts and edits only protocol roles", async () => {
    let saved: Record<string, unknown> | undefined;
    const fetch = mockFetch((path, options) => {
      if (path === "/api/v1/library/music/server" && !options?.method)
        return server(1);
      if (path === "/api/v1/library/music/server/users" && !options?.method)
        return Response.json({ users: [alex] });
      if (
        path === "/api/v1/library/music/server/users/1" &&
        options?.method === "PUT"
      ) {
        saved = JSON.parse(String(options.body));
        return Response.json({ ...alex, upload_role: true });
      }
      return Response.json({});
    });

    renderPage();

    expect(
      await screen.findByRole("heading", { name: "Music server" }),
    ).toBeVisible();
    expect(await screen.findByText("alex")).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Manage accounts" }),
    ).toHaveAttribute("href", "/accounts");
    expect(
      screen.queryByRole("button", { name: /Add account/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Reset password/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^Remove$/i }),
    ).not.toBeInTheDocument();

    await userEvent.click(
      screen.getByRole("button", { name: "Edit permissions" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Account identity, password, and access");
    expect(screen.queryByLabelText("Username")).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/New password/)).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("checkbox", { name: /^Upload/ }));
    await userEvent.click(
      screen.getByRole("button", { name: "Save permissions" }),
    );

    expect(await screen.findByText(/Saved changes to/)).toBeVisible();
    expect(saved).toEqual({
      username: "alex",
      email: "alex@example.com",
      admin_role: false,
      settings_role: false,
      download_role: true,
      upload_role: true,
      playlist_role: true,
      cover_art_role: true,
      share_role: false,
      scrobbling_enabled: true,
    });
    expect(saved).not.toHaveProperty("password");
    expect(
      fetch.mock.calls.some(([, options]) => options?.method === "POST"),
    ).toBe(false);
  });

  it("generates a legacy shared token once", async () => {
    mockFetch((path, options) => {
      if (path === "/api/v1/library/music/server" && !options?.method)
        return server(0, false);
      if (path === "/api/v1/library/music/server/users" && !options?.method)
        return Response.json({ users: [] });
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
    expect(await screen.findByText("No accounts yet")).toBeVisible();
    expect(
      screen.getByText(/Grant Subsonic access from Accounts/),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: /Generate token/ }),
    );
    expect(
      await screen.findByDisplayValue("generated-test-token"),
    ).toBeVisible();
  });
});
