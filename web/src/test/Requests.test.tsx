import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import App from "../App";

describe("request accounts", () => {
  it("shows a request-only interface and submits a selected title", async () => {
    const fetch = vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith("/auth/session"))
        return Response.json({
          authenticated: true,
          setup_required: false,
          session: {
            username: "viewer",
            role: "requester",
            account_id: 1,
            csrf_token: "csrf",
            expires_at: 9999999999,
          },
        });
      if (url.includes("/requester/search/movies"))
        return Response.json({
          items: [
            {
              tmdb_id: 11,
              title: "Example title",
              year: 2020,
              in_library: false,
            },
          ],
        });
      if (url.endsWith("/requester/lists"))
        return Response.json(
          options?.method === "POST" ? { id: 1 } : { items: [] },
        );
      if (url.endsWith("/requester/discord"))
        return Response.json({ configured: options?.method === "PUT" });
      if (url.endsWith("/requester/requests"))
        return options?.method === "POST"
          ? Response.json({ id: 1 })
          : Response.json({ items: [] });
      return Response.json(
        { error: { message: "unexpected request" } },
        { status: 404 },
      );
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
      await screen.findByRole("heading", { name: "My requests" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("navigation", { name: "Administration" }),
    ).not.toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Title"), "Example");
    await userEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(await screen.findByText(/Example title/)).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Request" }));
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            url.endsWith("/requester/requests") && options?.method === "POST",
        ),
      ).toBe(true),
    );
    const call = fetch.mock.calls.find(
      ([url, options]) =>
        url.endsWith("/requester/requests") && options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      kind: "movie",
      tmdb_id: 11,
    });
    await userEvent.type(screen.getByLabelText("TMDb numeric list ID"), "123");
    await userEvent.click(screen.getByRole("button", { name: "Add list" }));
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            url.endsWith("/requester/lists") && options?.method === "POST",
        ),
      ).toBe(true),
    );
    expect(await screen.findByText(/List saved/)).toBeVisible();
    const hook =
      "https://discord.com/api/webhooks/123456789012345678/" + "a".repeat(60);
    await userEvent.type(screen.getByLabelText("Discord webhook URL"), hook);
    await userEvent.click(screen.getByRole("button", { name: "Save webhook" }));
    expect(await screen.findByText("Webhook saved.")).toBeVisible();
    expect(screen.getByLabelText("Discord webhook URL")).toHaveValue("");
  });
});
