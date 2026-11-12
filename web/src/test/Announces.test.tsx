import userEvent from "@testing-library/user-event";
import { render, screen, waitFor, within } from "@testing-library/react";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { it, expect, vi } from "vitest";
import { setCSRFToken } from "../api";
import { Announces } from "../pages/Announces";

const source = {
  id: "a".repeat(32),
  tracker: "iptorrents",
  name: "Acceptance IPT",
  enabled: true,
  auto_grab: false,
  revision: 1,
  last_event_at: 0,
  server: "irc.iptorrents.com",
  port: 6697,
  tls: true,
  channel: "#ipt.announce",
  nick: "acceptbot",
  account: "",
  has_password: true,
  has_key: true,
  require_account: false,
  skip_tls_verify: false,
};

const definitions = [
  {
    id: "iptorrents",
    name: "IPTorrents",
    server: "irc.iptorrents.com",
    port: 6697,
    tls: true,
    channel: "#ipt.announce",
    require_account: false,
  },
  {
    id: "beyondhd",
    name: "BeyondHD",
    server: "irc.beyondhd.org",
    port: 6697,
    tls: true,
    channel: "#bhd.announce",
    require_account: false,
  },
];

const captureEvent = {
  id: 1,
  source_id: "a".repeat(32),
  tracker: "iptorrents",
  release_name: "The Movie 2010 DVDRip XviD-GROUP",
  category: "Movies/XviD",
  size_bytes: 8192650117,
  freeleech: "FREELEECH",
  info_url: "https://tracker/details.php?id=1",
  received_at: 1,
  outcome: "capture_only",
  message: "",
  attempts: 0,
  processed_at: 1,
  download_id: "",
};

// stub installs a fetch double and returns it so tests can inspect mutations.
function stub(items: unknown[], events: unknown[]) {
  const fetch = vi.fn(async (url: string, o?: RequestInit) => {
    if (o?.method) return Response.json({});
    if (String(url).endsWith("/announce-sources"))
      return Response.json({ items, definitions });
    if (String(url).includes("/announces"))
      return Response.json({
        items: events,
        page: 1,
        total: events.length,
        limit: 50,
      });
    return Response.json({});
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

function mount() {
  setCSRFToken("announce-csrf");
  render(
    <Theme>
      <MemoryRouter>
        <Announces />
      </MemoryRouter>
    </Theme>,
  );
}

it("lists announce sources and captured releases", async () => {
  stub([source], [captureEvent]);
  mount();
  expect(await screen.findByText("Acceptance IPT")).toBeInTheDocument();
  expect(
    await screen.findByText("The Movie 2010 DVDRip XviD-GROUP"),
  ).toBeInTheDocument();
  expect(screen.getByText("Open on tracker")).toBeInTheDocument();
  expect(screen.getByText("Enabled")).toBeInTheDocument();
  // Capture history stays readable and is labelled as capture-only.
  expect(screen.getByText("Capture only")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Add Source" }));
  const dialog = await screen.findByRole("dialog");
  expect(within(dialog).getByText("Add an announce source")).toBeVisible();
  expect(
    within(dialog).getByRole("button", { name: "Add source" }),
  ).toBeVisible();
  expect(
    within(dialog).getByLabelText("Skip TLS certificate verification"),
  ).not.toBeChecked();
});

it("defaults automatic grabbing off and requires a saved passkey", async () => {
  stub([], []);
  mount();
  await userEvent.click(screen.getByRole("button", { name: "Add Source" }));
  const dialog = await screen.findByRole("dialog");
  const autoGrab = within(dialog).getByLabelText("Automatic grabbing");
  expect(autoGrab).not.toBeChecked();
  // No passkey typed yet for a new source, so opt-in is blocked.
  expect(autoGrab).toBeDisabled();
  expect(
    within(dialog).getByText(
      "Save a tracker passkey before enabling automatic grabbing.",
    ),
  ).toBeVisible();
  await userEvent.type(
    within(dialog).getByLabelText("Tracker key"),
    "fixture-passkey",
  );
  await waitFor(() => expect(autoGrab).toBeEnabled());
});

it("limits automatic grabbing to IPTorrents", async () => {
  stub([], []);
  mount();
  await userEvent.click(screen.getByRole("button", { name: "Add Source" }));
  const dialog = await screen.findByRole("dialog");
  await userEvent.selectOptions(
    within(dialog).getByLabelText("Tracker"),
    "beyondhd",
  );
  expect(within(dialog).getByLabelText("Automatic grabbing")).toBeDisabled();
  expect(
    within(dialog).getByText(
      "Automatic grabbing is currently supported only for IPTorrents.",
    ),
  ).toBeVisible();
});

it("saves the automatic grabbing opt-in for a new IPTorrents source", async () => {
  const fetch = stub([], []);
  mount();
  await userEvent.click(screen.getByRole("button", { name: "Add Source" }));
  const dialog = await screen.findByRole("dialog");
  await userEvent.type(
    within(dialog).getByLabelText("Source name"),
    "Fixture IPT",
  );
  await userEvent.type(
    within(dialog).getByLabelText("IRC nick"),
    "fixture_bot",
  );
  await userEvent.type(
    within(dialog).getByLabelText("Tracker key"),
    "fixture-passkey",
  );
  const autoGrab = within(dialog).getByLabelText("Automatic grabbing");
  await waitFor(() => expect(autoGrab).toBeEnabled());
  await userEvent.click(autoGrab);
  await userEvent.click(
    within(dialog).getByRole("button", { name: "Add source" }),
  );
  const sent = fetch.mock.calls.find(([, o]) => o?.method === "POST");
  expect(JSON.parse(String(sent?.[1]?.body))).toMatchObject({
    tracker: "iptorrents",
    auto_grab: true,
    skip_tls_verify: false,
  });
  expect(sent?.[1]?.headers).toMatchObject({
    "X-CSRF-Token": "announce-csrf",
  });
});

it("preserves a saved automatic grabbing opt-in when pausing a source", async () => {
  const fetch = stub([{ ...source, auto_grab: true }], []);
  mount();
  expect(await screen.findByText("Acceptance IPT")).toBeInTheDocument();
  expect(screen.getByText("Auto-grab on")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Pause" }));
  const put = fetch.mock.calls.find(([, o]) => o?.method === "PUT");
  const body = JSON.parse(String(put?.[1]?.body));
  expect(body).toMatchObject({ enabled: false, revision: 1 });
  // Optional fields are omitted so the saved opt-in, passkey and TLS choice
  // survive a pause/resume toggle.
  expect(body).not.toHaveProperty("auto_grab");
  expect(body).not.toHaveProperty("skip_tls_verify");
  expect(body).not.toHaveProperty("key");
});

it("shows per-event grab outcomes, attempts, message and download", async () => {
  const queued = {
    ...captureEvent,
    id: 2,
    release_name: "Another Release 2021 1080p-GRP",
    freeleech: "",
    outcome: "queued",
    message: "Matched a monitored missing movie",
    attempts: 2,
    download_id: "abc123",
  };
  stub([source], [captureEvent, queued]);
  mount();
  expect(
    await screen.findByText("Another Release 2021 1080p-GRP"),
  ).toBeInTheDocument();
  expect(screen.getByText("Capture only")).toBeInTheDocument();
  expect(screen.getByText("queued")).toBeInTheDocument();
  expect(screen.getByText("2 attempts")).toBeInTheDocument();
  expect(
    screen.getByText("Matched a monitored missing movie"),
  ).toBeInTheDocument();
  expect(screen.getByText("Download abc123")).toBeInTheDocument();
});
