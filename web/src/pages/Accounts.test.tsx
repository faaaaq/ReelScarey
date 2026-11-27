import userEvent from "@testing-library/user-event";
import { render, screen, waitFor, within } from "@testing-library/react";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { setCSRFToken } from "../api";
import { Accounts } from "./Accounts";

const fixtures = [
  {
    id: 1,
    username: "alice",
    disabled: false,
    created_at: 1_700_000_000,
    requests: true,
    music_player: true,
    subsonic: false,
    password_set: true,
    request_account_id: 11,
  },
  {
    id: 2,
    username: "migrated",
    disabled: false,
    created_at: 1_700_000_100,
    requests: false,
    music_player: false,
    subsonic: true,
    password_set: false,
    music_server_user_id: 7,
  },
];

function stub(items = fixtures) {
  const fetchMock = vi.fn(
    async (input: string | URL | Request, options?: RequestInit) => {
      if (options?.method) return Response.json({});
      if (String(input).endsWith("/api/v1/accounts"))
        return Response.json({ items });
      return Response.json({});
    },
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function mount() {
  setCSRFToken("accounts-csrf");
  render(
    <Theme>
      <MemoryRouter>
        <Accounts />
      </MemoryRouter>
    </Theme>,
  );
}

function sent(
  fetchMock: ReturnType<typeof stub>,
  method: string,
  path: string,
) {
  return fetchMock.mock.calls.find(
    ([input, options]) =>
      String(input).endsWith(path) && options?.method === method,
  );
}

describe("Accounts", () => {
  it("lists access and explains migrated Subsonic-only passwords", async () => {
    stub();
    mount();

    expect(
      await screen.findByRole("heading", { name: "Accounts", level: 1 }),
    ).toBeInTheDocument();
    expect(await screen.findByText("alice")).toBeInTheDocument();
    expect(
      screen.getByText(
        /one username and password for both browser and Subsonic/i,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/migrated with Subsonic-only credentials/i),
    ).toBeInTheDocument();

    const migratedRow = screen.getByRole("row", { name: /migrated/ });
    expect(within(migratedRow).getByText("Password required")).toBeVisible();
    expect(
      within(migratedRow).getByRole("button", {
        name: "Set password for migrated",
      }),
    ).toBeVisible();
    expect(
      within(migratedRow).queryByRole("button", { name: /Discord/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /delete account/i }),
    ).not.toBeInTheDocument();
  });

  it("creates an account with independent access choices", async () => {
    const fetchMock = stub([]);
    mount();
    expect(await screen.findByText("No accounts")).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText("Username"), " new-user ");
    await userEvent.type(screen.getByLabelText("Password"), "fixture-password");
    await userEvent.click(screen.getByRole("checkbox", { name: /^Requests/ }));
    await userEvent.click(screen.getByRole("checkbox", { name: /^Subsonic/ }));
    await userEvent.click(
      screen.getByRole("button", { name: "Create account" }),
    );

    await waitFor(() =>
      expect(sent(fetchMock, "POST", "/api/v1/accounts")).toBeDefined(),
    );
    const request = sent(fetchMock, "POST", "/api/v1/accounts");
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      username: "new-user",
      password: "fixture-password",
      requests: true,
      music_player: false,
      subsonic: true,
    });
    expect(request?.[1]?.headers).toMatchObject({
      "X-CSRF-Token": "accounts-csrf",
    });
  });

  it("edits account status and access", async () => {
    const fetchMock = stub();
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "Edit access for alice" }),
    );
    const dialog = await screen.findByRole("dialog");

    expect(
      within(dialog).getByRole("checkbox", { name: /^Requests/ }),
    ).toBeChecked();
    expect(
      within(dialog).getByRole("checkbox", { name: /^Music player/ }),
    ).toBeChecked();
    expect(
      within(dialog).getByRole("checkbox", { name: /^Subsonic/ }),
    ).not.toBeChecked();

    await userEvent.click(
      within(dialog).getByRole("checkbox", { name: /^Disable account/ }),
    );
    await userEvent.click(
      within(dialog).getByRole("checkbox", { name: /^Music player/ }),
    );
    await userEvent.click(
      within(dialog).getByRole("checkbox", { name: /^Subsonic/ }),
    );
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Save changes" }),
    );

    await waitFor(() =>
      expect(sent(fetchMock, "PUT", "/api/v1/accounts/1")).toBeDefined(),
    );
    const request = sent(fetchMock, "PUT", "/api/v1/accounts/1");
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      disabled: true,
      requests: true,
      music_player: false,
      subsonic: true,
    });
  });

  it("sets a migrated account password for browser and Subsonic sign-in", async () => {
    const fetchMock = stub();
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "Set password for migrated" }),
    );
    const dialog = await screen.findByRole("dialog");

    expect(
      within(dialog).getByText(/needs an administrator-set password/i),
    ).toBeVisible();
    expect(within(dialog).getByText(/applies everywhere/i)).toBeVisible();
    await userEvent.type(
      within(dialog).getByLabelText("New password"),
      "replacement-password",
    );
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Set password" }),
    );

    await waitFor(() =>
      expect(
        sent(fetchMock, "POST", "/api/v1/accounts/2/password"),
      ).toBeDefined(),
    );
    const request = sent(fetchMock, "POST", "/api/v1/accounts/2/password");
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      password: "replacement-password",
    });
  });

  it("links Discord only for accounts with a request profile", async () => {
    const fetchMock = stub();
    mount();
    expect(
      await screen.findByRole("button", { name: "Link Discord for alice" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: /Discord for migrated/ }),
    ).not.toBeInTheDocument();

    await userEvent.click(
      screen.getByRole("button", { name: "Link Discord for alice" }),
    );
    const dialog = await screen.findByRole("dialog");
    await userEvent.type(
      within(dialog).getByLabelText("Discord user ID"),
      "123456789012345678",
    );
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Save Discord link" }),
    );

    await waitFor(() =>
      expect(
        sent(fetchMock, "PUT", "/api/v1/accounts/1/discord"),
      ).toBeDefined(),
    );
    const request = sent(fetchMock, "PUT", "/api/v1/accounts/1/discord");
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      discord_user_id: "123456789012345678",
    });
  });
});
