import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, it, expect, vi } from "vitest";
import App from "../App";

beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  Element.prototype.scrollIntoView = vi.fn();
});

function mount(path: string) {
  render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </Theme>,
  );
}
function setup(
  extra: (url: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    if (url.endsWith("/auth/session"))
      return Response.json({
        authenticated: true,
        setup_required: false,
        session: {
          username: "fixture",
          csrf_token: "fixture-csrf",
          expires_at: 9999999999,
        },
      });
    return (
      extra(url, options) ??
      Response.json(
        { error: { message: "Unexpected fixture request" } },
        { status: 404 },
      )
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("Usenet setup and release routing", () => {
  it("saves a Newznab source with a private key and no tracker credentials", async () => {
    const fetch = setup((url) =>
      url.endsWith("/indexers") ? Response.json({ items: [] }) : undefined,
    );
    mount("/indexers");
    await screen.findByRole("heading", { name: "Indexers" });
    await userEvent.click(screen.getByRole("button", { name: "Add Indexer" }));
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Indexer type" }),
      "newznab",
    );
    await userEvent.type(
      screen.getByLabelText("Indexer URL"),
      "https://newznab.invalid/api",
    );
    await userEvent.type(
      screen.getByLabelText("Indexer API key"),
      "fixture-private-key",
    );
    expect(screen.queryByLabelText("Session cookie")).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Save & test connection" }),
    );
    await screen.findByText("Indexer verified and saved.");
    const call = fetch.mock.calls.find(([, o]) => o?.method === "POST");
    expect(JSON.parse(String(call?.[1]?.body))).toMatchObject({
      kind: "newznab",
      url: "https://newznab.invalid/api",
      api_key: "fixture-private-key",
    });
    expect(JSON.parse(String(call?.[1]?.body))).not.toHaveProperty("cookie");
    expect(call?.[1]?.headers).toMatchObject({
      "X-CSRF-Token": "fixture-csrf",
    });
  });
  it("saves SABnzbd's API key and completed path, then preserves the saved key on edit", async () => {
    let saved = false;
    const fetch = setup((url, o) => {
      if (url.endsWith("/integrations/sabnzbd")) {
        if (o?.method === "PUT") {
          saved = true;
          return Response.json({ configured: true, tested: true });
        }
        return Response.json({
          configured: saved,
          url: saved ? "https://sab.invalid" : "",
          download_path: saved ? "/completed" : "",
        });
      }
      if (url.endsWith("/settings"))
        return Response.json({
          library_name: "Fixture",
          movie_folder: "Movies",
          series_folder: "TV",
          revision: 1,
        });
      if (url.endsWith("/system"))
        return Response.json({
          paths: {
            config: "/config",
            media: "/media",
            downloads: "/downloads",
          },
          version: "test",
          schema_version: 10,
        });
      if (url.includes("/integrations/") || url.endsWith("/providers/tmdb"))
        return Response.json({ configured: false, url: "" });
    });
    mount("/settings#sabnzbd");
    const region = await screen.findByRole("region", {
      name: "SABnzbd settings",
    });
    const form = within(region);
    await form.findByLabelText("Web UI URL");
    expect(form.queryByLabelText("Web UI username")).not.toBeInTheDocument();
    expect(form.getByText(/NZB-only key/)).toBeVisible();
    await userEvent.type(
      form.getByLabelText("Web UI URL"),
      "https://sab.invalid",
    );
    const secret = form.getByLabelText("API key");
    await userEvent.type(secret, "fixture-sab-key");
    await userEvent.type(
      form.getByLabelText("SABnzbd completed downloads folder"),
      "/completed",
    );
    await userEvent.click(
      form.getByRole("button", { name: "Save & test connection" }),
    );
    await form.findByText("SABnzbd connection verified and saved.");
    await waitFor(() => expect(secret).toHaveValue(""));
    await form.findByRole("button", { name: "Test saved connection" });
    await userEvent.click(
      form.getByRole("button", { name: "Save & test connection" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.filter(
          ([url, o]) =>
            url.endsWith("/integrations/sabnzbd") && o?.method === "PUT",
        ),
      ).toHaveLength(2),
    );
    const puts = fetch.mock.calls.filter(
      ([url, o]) =>
        url.endsWith("/integrations/sabnzbd") && o?.method === "PUT",
    );
    expect(JSON.parse(String(puts[0]?.[1]?.body))).toEqual({
      url: "https://sab.invalid",
      api_key: "fixture-sab-key",
      download_path: "/completed",
    });
    expect(JSON.parse(String(puts[1]?.[1]?.body))).not.toHaveProperty(
      "api_key",
    );
  });
  it("allows NZBs with SABnzbd alone while keeping torrent download disabled", async () => {
    const fetch = setup((url) => {
      if (url.endsWith("/library/movies/1"))
        return Response.json({ id: 1, title: "Owned Movie", year: 2026 });
      if (
        url.endsWith("/integrations/sabnzbd") ||
        url.endsWith("/indexers/status")
      )
        return Response.json({ configured: true });
      if (url.endsWith("/integrations/qbittorrent"))
        return Response.json({ configured: false });
      if (url.includes("/releases?"))
        return Response.json({
          items: [
            {
              id: "a".repeat(32),
              title: "Owned Usenet release",
              protocol: "usenet",
              size_bytes: 100,
              indexer: "Owned Newznab",
              seeders: null,
              peers: null,
            },
            {
              id: "b".repeat(32),
              title: "Owned torrent release",
              protocol: "torrent",
              size_bytes: 100,
              seeders: 4,
              peers: 5,
            },
          ],
        });
      if (url.endsWith("/downloads"))
        return Response.json({ created: true }, { status: 202 });
      if (url.includes("/downloads?page="))
        return Response.json({ items: [], page: 1, limit: 24, total: 0 });
    });
    mount("/releases?movie=1");
    await screen.findByRole("button", { name: "Search releases" });
    await userEvent.click(
      screen.getByRole("button", { name: "Search releases" }),
    );
    const nzb = (await screen.findByText("Owned Usenet release")).closest(
      "tr",
    )!;
    const torrent = screen.getByText("Owned torrent release").closest("tr")!;
    expect(within(nzb).getByRole("button", { name: "Download" })).toBeEnabled();
    expect(within(nzb).queryByText(/seeders/)).not.toBeInTheDocument();
    expect(
      within(torrent).getByRole("button", { name: "Download" }),
    ).toBeDisabled();
    expect(
      within(torrent).getByRole("link", { name: "Set up a download client" }),
    ).toHaveAttribute("href", "/download-clients");
    await userEvent.click(
      within(nzb).getByRole("button", { name: "Download" }),
    );
    await screen.findByRole("heading", { name: "Activity" });
    const post = fetch.mock.calls.find(
      ([url, o]) => url.endsWith("/downloads") && o?.method === "POST",
    );
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({
      release_id: "a".repeat(32),
    });
  });
  it("makes torrent downloads ready with the included built-in client alone", async () => {
    const fetch = setup((url) => {
      if (url.endsWith("/library/movies/1"))
        return Response.json({ id: 1, title: "Owned Movie", year: 2026 });
      if (url.endsWith("/indexers/status"))
        return Response.json({ configured: true });
      if (
        url.endsWith("/integrations/qbittorrent") ||
        url.endsWith("/integrations/sabnzbd")
      )
        return Response.json({ configured: false });
      if (url.endsWith("/download-clients"))
        return Response.json({
          items: [
            {
              id: "9".repeat(32),
              name: "Built-in qBittorrent",
              kind: "qbittorrent",
              enabled: true,
              builtin: true,
            },
          ],
          defaults: { torrent: "", usenet: "", revision: 1 },
        });
      if (url.includes("/releases?"))
        return Response.json({
          items: [
            {
              id: "b".repeat(32),
              title: "Included client release",
              protocol: "torrent",
              size_bytes: 100,
              seeders: 4,
              peers: 5,
            },
          ],
        });
      if (url.endsWith("/downloads"))
        return Response.json({ created: true }, { status: 202 });
    });
    mount("/releases?movie=1");
    await userEvent.click(
      await screen.findByRole("button", { name: "Search releases" }),
    );
    const row = (await screen.findByText("Included client release")).closest(
      "tr",
    )!;
    expect(within(row).getByRole("button", { name: "Download" })).toBeEnabled();
    expect(
      within(row).queryByRole("link", { name: "Set up a download client" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      within(row).getByRole("button", { name: "Download" }),
    );
    await waitFor(() =>
      expect(fetch.mock.calls.some(([, o]) => o?.method === "POST")).toBe(true),
    );
  });
});
