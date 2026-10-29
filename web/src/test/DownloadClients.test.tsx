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
  await screen.findByText(
    "No named clients saved. Existing Settings connections continue to work.",
  );
  const user = userEvent.setup();
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
  const card = screen.getByText("Owned SAB").closest("details")!;
  card.open = true;
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
