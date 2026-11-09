import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import App from "../App";
import { ReleaseBlacklist } from "../pages/Blacklist";
import { Duplicates } from "../pages/Duplicates";
import { SecuritySettings } from "../pages/SecuritySettings";

function renderPage(node: ReactNode) {
  return render(
    <Theme>
      <MemoryRouter>{node}</MemoryRouter>
    </Theme>,
  );
}

describe("release blacklist", () => {
  it("lists blocked releases and removes one through review", async () => {
    const user = userEvent.setup();
    let items = [
      {
        id: 7,
        protocol: "usenet" as const,
        release_key: "release-key",
        indexer: "Indexer A",
        title: "Example.Release.1080p",
        reason: "Import failed: no video file",
        created_at: 1700000000,
        expires_at: 0,
      },
      {
        id: 8,
        protocol: "torrent" as const,
        release_key: "other-key",
        indexer: "",
        title: "Another.Release.720p",
        reason: "Client reported a terminal failure",
        created_at: 1700000100,
        expires_at: 1700000200,
      },
    ];
    const fetch = vi.fn(async (url: string, options?: RequestInit) => {
      if (
        url.endsWith("/releases/blacklist") &&
        (!options?.method || options.method === "GET")
      )
        return Response.json({ items });
      if (
        url.endsWith("/releases/blacklist/7") &&
        options?.method === "DELETE"
      ) {
        items = items.filter((entry) => entry.id !== 7);
        return Response.json({ removed: true });
      }
      return Response.json(
        { error: { message: "Unexpected test request" } },
        { status: 404 },
      );
    });
    vi.stubGlobal("fetch", fetch);

    renderPage(<ReleaseBlacklist />);

    expect(await screen.findByText("Example.Release.1080p")).toBeVisible();
    expect(screen.getByText("Permanent")).toBeVisible();
    expect(screen.getByText(/\(expired\)/)).toBeVisible();
    expect(screen.getByText("Indexer A")).toBeVisible();

    await user.click(screen.getAllByRole("button", { name: "Remove" })[0]);
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText("Remove this release from the blacklist?"),
    ).toBeVisible();
    expect(
      within(dialog).getByText(/does not start a new download/),
    ).toBeVisible();
    await user.click(
      within(dialog).getByRole("button", { name: "Remove entry" }),
    );

    expect(
      await screen.findByText(/Removed “Example.Release.1080p”/),
    ).toBeVisible();
    await waitFor(() =>
      expect(screen.queryByText("Example.Release.1080p")).toBeNull(),
    );
    expect(
      fetch.mock.calls.some(
        ([url, options]) =>
          String(url).endsWith("/releases/blacklist/7") &&
          options?.method === "DELETE",
      ),
    ).toBe(true);
  });

  it("explains automatic blocking when the list is empty", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ items: [] })),
    );
    renderPage(<ReleaseBlacklist />);
    expect(await screen.findByText("No blocked releases")).toBeVisible();
    expect(
      screen.getByText(/blocked automatically after terminal failures/),
    ).toBeVisible();
  });
});

describe("duplicate review", () => {
  it("shows probable duplicates and the first failed check without actions", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          groups: [
            {
              kind: "movie",
              media_id: 1,
              title: "Alpha",
              file_count: 2,
              status: "probable_duplicate",
              reason: "identical size, SHA-256 and resolution",
              files: [
                {
                  id: 1,
                  relative_path: "Movies/Alpha/a.mkv",
                  size_bytes: 1024,
                  sha256: "same-sha",
                  resolution: 1080,
                },
                {
                  id: 2,
                  relative_path: "Movies/Alpha/b.mkv",
                  size_bytes: 1024,
                  sha256: "same-sha",
                  resolution: 1080,
                },
              ],
            },
            {
              kind: "episode",
              media_id: 2,
              episode_id: 9,
              title: "A Show",
              episode_title: "Pilot",
              season: 1,
              episode: 1,
              file_count: 2,
              status: "needs_attention",
              reason: "SHA-256 differs",
              files: [
                {
                  id: 3,
                  relative_path: "TV/A Show/S01E01/a.mkv",
                  size_bytes: 2048,
                  sha256: "one",
                  resolution: 0,
                },
                {
                  id: 4,
                  relative_path: "TV/A Show/S01E01/b.mkv",
                  size_bytes: 2048,
                  sha256: "two",
                  resolution: 0,
                },
              ],
            },
          ],
          total: 2,
          limit: 100,
          truncated: false,
        }),
      ),
    );

    renderPage(<Duplicates />);

    expect(
      await screen.findByRole("heading", { name: "Duplicate review" }),
    ).toBeVisible();
    expect(screen.getByText("Probable duplicate")).toBeVisible();
    expect(screen.getByText("Needs attention")).toBeVisible();
    expect(screen.getByText(/SHA-256 differs/)).toBeVisible();
    expect(screen.getByText(/S01E01 · Pilot/)).toBeVisible();
    expect(screen.getByText("Movies/Alpha/a.mkv")).toBeVisible();
    expect(screen.getAllByText("Unknown").length).toBeGreaterThan(0);
    expect(
      screen.queryByRole("button", { name: /delete|move|remove/i }),
    ).toBeNull();
  });
});

describe("two-factor authentication settings", () => {
  it("provisions, confirms once and shows recovery codes without persisting them", async () => {
    const user = userEvent.setup();
    let configured = false;
    let enabled = false;
    const fetch = vi.fn(async (url: string, options?: RequestInit) => {
      if (
        url.endsWith("/auth/totp") &&
        (!options?.method || options.method === "GET")
      )
        return Response.json({
          configured,
          enabled,
          recovery_codes_remaining: enabled ? 10 : 0,
        });
      if (url.endsWith("/auth/totp/provision") && options?.method === "POST") {
        configured = true;
        return Response.json({
          secret: "JBSWY3DPEHPK3PXP",
          otpauth_uri:
            "otpauth://totp/ReelScarey:admin?secret=JBSWY3DPEHPK3PXP",
        });
      }
      if (url.endsWith("/auth/totp/confirm") && options?.method === "POST") {
        expect(JSON.parse(String(options.body))).toEqual({ code: "123456" });
        enabled = true;
        return Response.json({
          enabled: true,
          recovery_codes: ["AAAA-BBBB", "CCCC-DDDD"],
        });
      }
      return Response.json(
        { error: { message: "Unexpected test request" } },
        { status: 404 },
      );
    });
    vi.stubGlobal("fetch", fetch);

    renderPage(<SecuritySettings />);

    expect(
      await screen.findByText(/Two-factor authentication is/),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Start setup" }));
    expect(await screen.findByText("JBSWY3DPEHPK3PXP")).toBeVisible();
    await user.type(screen.getByLabelText("Authenticator code"), "123456");
    await user.click(
      screen.getByRole("button", { name: "Confirm and enable" }),
    );

    expect(
      await screen.findByRole("heading", { name: "Save your recovery codes" }),
    ).toBeVisible();
    expect(screen.getByText(/AAAA-BBBB/)).toBeVisible();
    expect(screen.getByText(/shown only once/)).toBeVisible();
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);

    await user.click(
      screen.getByRole("button", { name: "I saved these codes" }),
    );
    expect(
      await screen.findByText("10 unused recovery codes remain."),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Disable two-factor authentication" }),
    ).toBeVisible();
  });
});

describe("two-factor sign-in", () => {
  it("asks for an authenticator code and reports an invalid one", async () => {
    const user = userEvent.setup();
    const fetch = vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith("/auth/session"))
        return Response.json({
          setup_required: false,
          authenticated: false,
          totp_enabled: true,
        });
      if (url.endsWith("/auth/login") && options?.method === "POST") {
        const body = JSON.parse(String(options.body));
        if (body.totp_code !== "123456")
          return Response.json(
            { error: { message: "The authenticator code is incorrect." } },
            { status: 401 },
          );
        return Response.json({
          username: "testadmin",
          csrf_token: "test-csrf",
          expires_at: 9999999999,
        });
      }
      return Response.json({});
    });
    vi.stubGlobal("fetch", fetch);

    render(
      <Theme>
        <MemoryRouter>
          <App />
        </MemoryRouter>
      </Theme>,
    );

    expect(
      await screen.findByLabelText("Authenticator or recovery code"),
    ).toBeVisible();
    await user.type(screen.getByLabelText("Username"), "testadmin");
    await user.type(screen.getByLabelText("Password"), "test-password");
    await user.type(
      screen.getByLabelText("Authenticator or recovery code"),
      "000000",
    );
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The authenticator code is incorrect.",
    );

    const password = screen.getByLabelText("Password");
    const code = screen.getByLabelText("Authenticator or recovery code");
    await user.clear(password);
    await user.type(password, "test-password");
    await user.clear(code);
    await user.type(code, "123456");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => {
      const calls = fetch.mock.calls.filter(
        ([url, options]) =>
          String(url).endsWith("/auth/login") && options?.method === "POST",
      );
      const call = calls.at(-1);
      expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({
        username: "testadmin",
        password: "test-password",
        totp_code: "123456",
      });
    });
  }, 20000);
});
