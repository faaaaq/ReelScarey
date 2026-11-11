import { render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { it, expect, vi } from "vitest";
import { DownloadClients } from "../pages/DownloadClients";
const renderPage = () =>
  render(
    <Theme>
      <MemoryRouter>
        <DownloadClients />
      </MemoryRouter>
    </Theme>,
  );
it("offers Transmission only as a torrent default", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({
        items: [
          {
            id: "t",
            name: "Owned Transmission",
            kind: "transmission",
            enabled: true,
            revision: 1,
            url: "http://transmission:9091/transmission/rpc",
            username: "fixture",
            download_path: "/completed",
            local_folder: "",
            active_jobs: 0,
          },
        ],
        defaults: { torrent: "t", usenet: "", revision: 1 },
      }),
    ),
  );
  renderPage();
  const torrent = await screen.findByLabelText("Torrent default");
  expect(
    within(torrent).getByRole("option", { name: /Owned Transmission/ }),
  ).toBeInTheDocument();
  expect(
    within(screen.getByLabelText("Usenet default")).queryByRole("option", {
      name: /Owned Transmission/,
    }),
  ).not.toBeInTheDocument();
});
it("creates a tested paused client and sends its explicit folder mapping", async () => {
  const requests = vi.fn(async (_url: string, o?: RequestInit) =>
    Response.json(
      o?.method
        ? {}
        : { items: [], defaults: { torrent: "", usenet: "", revision: 1 } },
    ),
  );
  vi.stubGlobal("fetch", requests);
  renderPage();
  await screen.findByText("No named clients saved.");
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Add download client" }));
  await user.type(screen.getByLabelText("Client name"), "Owned NZBGet");
  await user.selectOptions(screen.getByLabelText("Client type"), "nzbget");
  await user.type(
    screen.getByLabelText("Server address"),
    "http://nzbget:6789",
  );
  await user.type(screen.getByLabelText("Username"), "fixture");
  await user.type(screen.getByLabelText("Password"), "fixture-password");
  await user.type(
    screen.getByLabelText("Client's completed download folder"),
    "/completed",
  );
  await user.type(
    screen.getByLabelText("Local subfolder inside /downloads"),
    "server-a",
  );
  await user.click(
    screen.getByRole("button", { name: "Test and save client" }),
  );
  await waitFor(() =>
    expect(requests.mock.calls.some(([, o]) => o?.method === "POST")).toBe(
      true,
    ),
  );
  const sent = requests.mock.calls.find(([, o]) => o?.method === "POST")!;
  expect(JSON.parse(sent[1]!.body as string)).toMatchObject({
    kind: "nzbget",
    enabled: false,
    revision: 0,
    local_folder: "server-a",
    password: "fixture-password",
  });
});
it("retains blank secrets, restricts protocol defaults and protects bound clients", async () => {
  const client = {
    id: "a".repeat(32),
    name: "Owned SAB",
    kind: "sabnzbd",
    enabled: true,
    revision: 7,
    url: "http://sab:8080",
    download_path: "/completed",
    local_folder: "sab",
    active_jobs: 2,
  };
  const requests = vi.fn(async (_url: string, o?: RequestInit) =>
    Response.json(
      o?.method
        ? {}
        : {
            items: [client],
            defaults: { torrent: "", usenet: client.id, revision: 4 },
          },
    ),
  );
  vi.stubGlobal("fetch", requests);
  renderPage();
  await screen.findByText("Owned SAB");
  expect(
    within(screen.getByLabelText("Torrent default")).queryByRole("option", {
      name: /Owned SAB/,
    }),
  ).not.toBeInTheDocument();
  expect(
    within(screen.getByLabelText("Usenet default")).getByRole("option", {
      name: /Owned SAB/,
    }),
  ).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Edit Owned SAB" }));
  const card = await screen.findByRole("dialog");
  expect(
    within(card).getByRole("button", { name: "Remove client" }),
  ).toBeDisabled();
  expect(within(card).getByRole("checkbox")).toBeDisabled();
  await userEvent.click(
    within(card).getByRole("button", { name: "Test and save client" }),
  );
  await waitFor(() =>
    expect(requests.mock.calls.some(([, o]) => o?.method === "PUT")).toBe(true),
  );
  const sent = requests.mock.calls.find(
    ([url, o]) => url.endsWith(client.id) && o?.method === "PUT",
  )!;
  expect(JSON.parse(sent[1]!.body as string)).toMatchObject({
    revision: 7,
    enabled: true,
  });
  expect(JSON.parse(sent[1]!.body as string)).not.toHaveProperty("api_key");
  await userEvent.selectOptions(screen.getByLabelText("Usenet default"), "");
  await userEvent.click(screen.getByRole("button", { name: "Save defaults" }));
  await waitFor(() =>
    expect(
      requests.mock.calls.some(
        ([url, o]) =>
          url.endsWith("/defaults") &&
          JSON.parse(o!.body as string).revision === 4,
      ),
    ).toBe(true),
  );
});
it("supervises the built-in client and disables it with explicit review", async () => {
  const builtin = {
    present: true,
    enabled: true,
    state: "running",
    revision: 4,
    peer_port: 6881,
    active_jobs: 2,
    name: "Built-in qBittorrent",
  };
  const requests = vi.fn(async (url: string, o?: RequestInit) => {
    if (url.endsWith("/download-clients/builtin")) {
      if (o?.method === "PUT") {
        expect(JSON.parse(o.body as string)).toMatchObject({
          revision: 4,
          enabled: false,
        });
        return Response.json({ ...builtin, enabled: false, state: "stopped" });
      }
      return Response.json(builtin);
    }
    return Response.json({
      items: [],
      defaults: { torrent: "", usenet: "", revision: 1 },
    });
  });
  vi.stubGlobal("fetch", requests);
  renderPage();
  expect(
    await screen.findByRole("heading", { name: "Built-in qBittorrent" }),
  ).toBeInTheDocument();
  expect(await screen.findByText("Running")).toBeInTheDocument();
  expect(screen.getByText(/Included and on by default/)).toBeInTheDocument();
  expect(screen.getByText(/2 active jobs use this client/)).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Turn off" }));
  const dialog = await screen.findByRole("dialog", {
    name: "Turn off the built-in qBittorrent client?",
  });
  expect(
    within(dialog).getByText(
      /torrent default is cleared and its downloads stay/,
    ),
  ).toBeVisible();
  await userEvent.click(
    within(dialog).getByRole("button", { name: "Confirm" }),
  );
  await waitFor(() =>
    expect(
      requests.mock.calls.some(
        ([url, o]) =>
          url.endsWith("/download-clients/builtin") && o?.method === "PUT",
      ),
    ).toBe(true),
  );
});
it("uploads a Mullvad config and connects with the parsed values", async () => {
  const config = `[Interface]
PrivateKey = priv-key
Address = 10.1.2.3/32,fc00::2/128
DNS = 10.64.0.1

[Peer]
PublicKey = peer-key
AllowedIPs = 0.0.0.0/0,::/0
Endpoint = 1.2.3.4:51820
`;
  const requests = vi.fn(async (url: string, o?: RequestInit) => {
    if (url.endsWith("/vpn/mullvad")) {
      if (o?.method === "PUT") {
        expect(JSON.parse(o.body as string)).toMatchObject({
          enabled: true,
          address: "10.1.2.3/32,fc00::2/128",
          peer_key: "peer-key",
          endpoint: "1.2.3.4:51820",
          allowed_ips: "0.0.0.0/0,::/0",
          private_key: "priv-key",
          kill_switch: true,
        });
        return Response.json({
          present: true,
          enabled: true,
          kill_switch: true,
          state: "up",
          revision: 2,
        });
      }
      return Response.json({
        present: false,
        enabled: false,
        kill_switch: true,
        state: "stopped",
        revision: 0,
      });
    }
    if (url.endsWith("/download-clients/builtin"))
      return Response.json({ present: false, state: "stopped", revision: 0 });
    return Response.json({
      items: [],
      defaults: { torrent: "", usenet: "", revision: 1 },
    });
  });
  vi.stubGlobal("fetch", requests);
  renderPage();
  const input = await screen.findByLabelText(/Upload Mullvad config/);
  const file = new File([config], "mullvad.conf", { type: "text/plain" });
  Object.defineProperty(file, "text", { value: async () => config });
  await userEvent.upload(input, file);
  await waitFor(() =>
    expect(
      requests.mock.calls.some(
        ([url, o]) => url.endsWith("/vpn/mullvad") && o?.method === "PUT",
      ),
    ).toBe(true),
  );
});

it("toggles the VPN kill switch without reconnecting the tunnel", async () => {
  const requests = vi.fn(async (url: string, o?: RequestInit) => {
    if (url.endsWith("/vpn/mullvad")) {
      if (o?.method === "PUT") {
        expect(JSON.parse(o.body as string)).toEqual({
          revision: 5,
          kill_switch: false,
        });
        return Response.json({
          present: true,
          enabled: true,
          kill_switch: false,
          state: "up",
          revision: 6,
        });
      }
      return Response.json({
        present: true,
        enabled: true,
        kill_switch: true,
        state: "up",
        revision: 5,
      });
    }
    if (url.endsWith("/download-clients/builtin"))
      return Response.json({
        present: true,
        enabled: true,
        state: "running",
        revision: 1,
      });
    return Response.json({
      items: [],
      defaults: { torrent: "", usenet: "", revision: 1 },
    });
  });
  vi.stubGlobal("fetch", requests);
  renderPage();
  const toggle = await screen.findByLabelText(/Kill switch/);
  expect(toggle).toBeChecked();
  await userEvent.click(toggle);
  await waitFor(() =>
    expect(
      requests.mock.calls.some(
        ([url, o]) => url.endsWith("/vpn/mullvad") && o?.method === "PUT",
      ),
    ).toBe(true),
  );
});

it("sets up the built-in client from scratch without routing jobs", async () => {
  const requests = vi.fn(async (url: string, o?: RequestInit) => {
    if (url.endsWith("/download-clients/builtin")) {
      if (o?.method === "PUT") {
        expect(JSON.parse(o.body as string)).toMatchObject({
          revision: 0,
          enabled: true,
        });
        return Response.json({
          present: true,
          enabled: true,
          state: "running",
          revision: 1,
          peer_port: 6881,
          active_jobs: 0,
          name: "Built-in qBittorrent",
        });
      }
      return Response.json({
        present: false,
        enabled: false,
        state: "stopped",
        revision: 0,
        peer_port: 6881,
        active_jobs: 0,
        name: "Built-in qBittorrent",
      });
    }
    return Response.json({
      items: [],
      defaults: { torrent: "", usenet: "", revision: 1 },
    });
  });
  vi.stubGlobal("fetch", requests);
  renderPage();
  expect(
    await screen.findByText(
      /The application supervises a private qBittorrent daemon/,
    ),
  ).toBeInTheDocument();
  await userEvent.click(
    screen.getByRole("button", { name: "Set up built-in client" }),
  );
  const dialog = await screen.findByRole("dialog", {
    name: "Set up the built-in qBittorrent client?",
  });
  await userEvent.click(
    within(dialog).getByRole("button", { name: "Confirm" }),
  );
  await waitFor(() =>
    expect(
      requests.mock.calls.some(
        ([url, o]) =>
          url.endsWith("/download-clients/builtin") && o?.method === "PUT",
      ),
    ).toBe(true),
  );
});
