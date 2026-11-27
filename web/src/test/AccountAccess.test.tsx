import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import App from "../App";
import { hasAccess, type Session } from "../api";

vi.mock("../pages/MusicPlayerWorkspace", () => ({
  MusicPlayerWorkspace: () => <h1>Music player</h1>,
}));
vi.mock("../components/HistoryTray", () => ({ HistoryTray: () => null }));

function session(
  role: Session["role"],
  access: string[],
  username = "limited",
): Session {
  return {
    username,
    role,
    access,
    account_id: 7,
    csrf_token: "csrf",
    expires_at: 9999999999,
  };
}

function mount(path = "/") {
  return render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </Theme>,
  );
}

function stubSession(
  current: Session,
  handler?: (url: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    if (url.endsWith("/auth/session"))
      return Response.json({
        authenticated: true,
        setup_required: false,
        session: current,
      });
    const response = handler?.(url, options);
    if (response) return response;
    return Response.json(
      { error: { message: `Unexpected limited-account request: ${url}` } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

function requestAPI(url: string, options?: RequestInit) {
  if (url.endsWith("/requester/requests"))
    return options?.method === "POST"
      ? Response.json({ id: 1 })
      : Response.json({ items: [] });
  if (url.endsWith("/requester/lists")) return Response.json({ items: [] });
  if (url.endsWith("/requester/discord"))
    return Response.json({ configured: false });
  return undefined;
}

describe("centralized account access", () => {
  it("gives administrators all access and limits other roles to their grants", () => {
    const admin = session("admin", []);
    expect(hasAccess(admin, "requests")).toBe(true);
    expect(hasAccess(admin, "music_player")).toBe(true);
    expect(hasAccess(admin, "subsonic")).toBe(true);
    expect(hasAccess(session("requester", []), "requests")).toBe(false);
    expect(hasAccess(session("requester", []), "music_player")).toBe(false);
    const legacy = {
      ...session("requester", []),
      access: undefined,
    } as unknown as Session;
    expect(hasAccess(legacy, "requests")).toBe(true);
    const user = session("user", ["subsonic"]);
    expect(hasAccess(user, "subsonic")).toBe(true);
    expect(hasAccess(user, "requests")).toBe(false);
  });

  it("honors a revoked Requests grant on a request-backed session", async () => {
    stubSession(session("requester", ["music_player"]), requestAPI);
    mount("/");

    expect(
      await screen.findByRole("heading", { name: "Music player" }),
    ).toBeVisible();
    expect(screen.queryByRole("heading", { name: "My requests" })).toBeNull();
    expect(
      screen.getByRole("navigation", { name: "Main navigation" }),
    ).not.toHaveTextContent("Requests");
  });

  it("lands a music-only user in the player without admin navigation or API calls", async () => {
    const fetch = stubSession(session("user", ["music_player"]));
    mount("/");

    expect(
      await screen.findByRole("heading", { name: "Music player" }),
    ).toBeVisible();
    const nav = screen.getByRole("navigation", { name: "Main navigation" });
    expect(nav).toHaveTextContent("Music player");
    expect(nav).not.toHaveTextContent("Requests");
    expect(
      screen.queryByRole("link", { name: "Add media" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("textbox", { name: /Search/ })).toBeNull();
    expect(fetch.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/auth/session",
    ]);
  });

  it("lets a combined user switch between Requests and Music player", async () => {
    stubSession(session("user", ["requests", "music_player"]), requestAPI);
    mount("/");

    expect(
      await screen.findByRole("heading", { name: "My requests" }),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("link", { name: "Music player" }));
    expect(
      await screen.findByRole("heading", { name: "Music player" }),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("link", { name: "Requests" }));
    expect(
      await screen.findByRole("heading", { name: "My requests" }),
    ).toBeVisible();
    expect(
      screen.getByRole("navigation", { name: "Main navigation" }),
    ).toHaveTextContent("RequestsMusic player");
  });

  it("redirects denied limited routes to the first allowed page", async () => {
    const fetch = stubSession(session("user", ["requests"]), requestAPI);
    mount("/settings");

    expect(
      await screen.findByRole("heading", { name: "My requests" }),
    ).toBeVisible();
    expect(screen.queryByRole("heading", { name: "Settings" })).toBeNull();
    expect(
      fetch.mock.calls.every(
        ([url]) => url.endsWith("/auth/session") || url.includes("/requester/"),
      ),
    ).toBe(true);
  });

  it("lets a player-only account change its shared password", async () => {
    const fetch = stubSession(
      session("user", ["music_player"]),
      (url, options) => {
        if (url.endsWith("/account/password") && options?.method === "PUT")
          return Response.json({ changed: true });
        return undefined;
      },
    );
    mount("/account/password");

    expect(
      await screen.findByRole("heading", { name: "Change password" }),
    ).toBeVisible();
    await userEvent.type(
      screen.getByLabelText("Current password"),
      "current-test-password",
    );
    await userEvent.type(
      screen.getByLabelText("New password"),
      "replacement-test-password",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Change password" }),
    );
    expect(
      fetch.mock.calls.some(
        ([url, options]) =>
          url.endsWith("/account/password") && options?.method === "PUT",
      ),
    ).toBe(true);
  });

  it("shows an honest no-access page and signs out without admin calls", async () => {
    const current = session("user", ["subsonic"]);
    const fetch = stubSession(current, (url, options) => {
      if (url.endsWith("/auth/logout") && options?.method === "POST")
        return new Response(null, { status: 204 });
      return undefined;
    });
    mount("/music/player");

    expect(
      await screen.findByRole("heading", { name: "No web pages available" }),
    ).toBeVisible();
    expect(
      screen.getByText(/This account has no Requests or Music player access/),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(
      await screen.findByRole("heading", { name: "Sign in" }),
    ).toBeVisible();
    expect(
      fetch.mock.calls.every(
        ([url]) =>
          url.endsWith("/auth/session") || url.endsWith("/auth/logout"),
      ),
    ).toBe(true);
  });
});
