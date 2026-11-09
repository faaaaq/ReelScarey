import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Torrents } from "../pages/Torrents";

const jobHash = "0123456789abcdef0123456789abcdef01234567";
const manualHash = "1111111111111111111111111111111111111111";
const otherHash = "2222222222222222222222222222222222222222";

function torrent(overrides: Record<string, unknown> = {}) {
  return {
    hash: jobHash,
    name: "Fixture.Release.1080p",
    state: "downloading",
    save_path: "/downloads/builtin/ReelScarey/jobs",
    progress: 0.25,
    amount_left: 75,
    total_size: 100,
    completed: 25,
    downloaded: 25,
    uploaded: 0,
    dlspeed: 2048,
    upspeed: 0,
    eta: 90,
    ratio: 0,
    num_seeds: 4,
    num_leechs: 1,
    added_on: 3,
    completion_on: 0,
    tags: "reelscarey-abcdef",
    category: "",
    content_path: "",
    priority: 1,
    num_complete: 10,
    num_incomplete: 2,
    dl_limit: 0,
    up_limit: 0,
    seq_dl: false,
    f_l_piece_prio: false,
    force_start: false,
    super_seeding: false,
    auto_tmm: false,
    time_active: 60,
    seeding_time: 0,
    last_activity: 3,
    availability: 1.5,
    ratio_limit: -2,
    seeding_time_limit: -2,
    downloaded_session: 25,
    uploaded_session: 0,
    tracker: "https://tracker.example",
    origin: "job",
    job_id: "abcdef",
    ...overrides,
  };
}

const jobTorrent = torrent();
const manualTorrent = torrent({
  hash: manualHash,
  name: "Ubuntu.ISO",
  state: "pausedUP",
  progress: 1,
  amount_left: 0,
  dlspeed: 0,
  eta: 8640000,
  added_on: 2,
  tags: "reelscarey-manual",
  tracker: "",
  origin: "manual",
  job_id: undefined,
});
const otherTorrent = torrent({
  hash: otherHash,
  name: "Debian.Netinst",
  state: "stalledUP",
  progress: 1,
  amount_left: 0,
  dlspeed: 0,
  upspeed: 0,
  eta: 8640000,
  added_on: 1,
  tags: "linux",
  category: "isos",
  tracker: "udp://tracker.debian.example:6969",
  origin: "external",
  job_id: undefined,
});

const policyFixture = {
  enabled: false,
  action: "pause",
  max_ratio: 2,
  max_seeding_minutes: 0,
  include_reelscarey: false,
  revision: 1,
  updated_at: 1700000000,
};

const publicPauseFixture = {
  enabled: false,
  revision: 1,
  updated_at: 1700000000,
};

function overview(overrides: Record<string, unknown> = {}) {
  return {
    available: true,
    state: "running",
    message: "",
    transfer: {
      dl_info_speed: 2048,
      up_info_speed: 0,
      dl_info_data: 4096,
      up_info_data: 0,
      connection_status: "connected",
      dht_nodes: 120,
      dl_rate_limit: 0,
      up_rate_limit: 0,
    },
    server: {
      alt_speed_enabled: false,
      queueing_enabled: true,
      alt_dl_limit: 0,
      alt_up_limit: 0,
      max_active_downloads: 3,
      max_active_uploads: 3,
      max_active_torrents: 5,
    },
    queue: {
      queueing_enabled: true,
      max_active_downloads: 3,
      max_active_uploads: 3,
      max_active_torrents: 5,
      revision: 1,
    },
    items: [jobTorrent, manualTorrent, otherTorrent],
    ...overrides,
  };
}

function detailFor(t: ReturnType<typeof torrent>, files?: unknown[]) {
  return {
    torrent: t,
    files: files ?? [
      {
        index: 0,
        name: "Folder/movie.mkv",
        size: 90,
        progress: 0.2,
        priority: 1,
        availability: 1,
        is_seed: false,
      },
      {
        index: 1,
        name: "Folder/info.nfo",
        size: 10,
        progress: 1,
        priority: 1,
        availability: 1,
        is_seed: false,
      },
    ],
    properties: {
      save_path: t.save_path,
      creation_date: 1700000000,
      piece_size: 16384,
      comment: "Generated fixture",
      total_wasted: 0,
      total_uploaded: 0,
      total_downloaded: 25,
      up_limit: -1,
      dl_limit: -1,
      time_elapsed: 60,
      seeding_time: 0,
      nb_connections: 3,
      nb_connections_limit: 100,
      share_ratio: 0,
      addition_date: 1700000100,
      completion_date: -1,
      created_by: "fixture-maker",
      dl_speed_avg: 1024,
      dl_speed: 2048,
      up_speed_avg: 0,
      up_speed: 0,
      eta: 90,
      last_seen: 0,
      peers: 1,
      peers_total: 2,
      seeds: 4,
      seeds_total: 10,
      pieces_have: 2,
      pieces_num: 8,
      reannounce: 1200,
      total_size: 100,
    },
    trackers: [
      {
        url: "https://tracker.example",
        status: 2,
        tier: 0,
        num_peers: 3,
        num_seeds: 4,
        num_leeches: 1,
        num_downloaded: 9,
        msg: "",
      },
    ],
    peers: [],
    webseeds: [],
    pieces: [2, 2, 1, 0, 0, 0, 0, 0],
  };
}

type Call = { url: string; method: string; body: unknown };

function mount(payload: unknown, options: { failMagnet?: string } = {}) {
  const calls: Call[] = [];
  let policy = { ...policyFixture };
  let pause = { ...publicPauseFixture };
  const fetch = vi.fn(async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? "GET";
    let body: unknown = init.body;
    if (typeof body === "string") body = JSON.parse(body);
    calls.push({ url, method, body });
    if (method === "GET" && url === "/api/v1/torrents")
      return Response.json(payload);
    if (method === "GET" && url === "/api/v1/torrents/share-policy")
      return Response.json(policy);
    if (method === "PUT" && url === "/api/v1/torrents/share-policy") {
      policy = {
        ...(body as typeof policyFixture),
        revision: policy.revision + 1,
        updated_at: 1700000100,
      };
      return Response.json(policy);
    }
    if (method === "POST" && url === "/api/v1/torrents/share-policy/run")
      return Response.json({ checked: 3, acted: 2 });
    if (method === "POST" && url === "/api/v1/torrents/queue-limits")
      return Response.json(body);
    if (method === "GET" && url === "/api/v1/torrents/public-pause")
      return Response.json(pause);
    if (method === "PUT" && url === "/api/v1/torrents/public-pause") {
      pause = {
        ...(body as typeof publicPauseFixture),
        revision: pause.revision + 1,
        updated_at: 1700000100,
      };
      return Response.json(pause);
    }
    if (method === "GET" && url.startsWith("/api/v1/torrents/")) {
      const hash = url.slice("/api/v1/torrents/".length).split("?")[0];
      const t = [jobTorrent, manualTorrent, otherTorrent].find(
        (i) => i.hash === hash,
      );
      if (t) return Response.json(detailFor(t));
    }
    if (method === "POST" && url === "/api/v1/torrents") {
      const magnet = (body as { magnet?: string }).magnet;
      if (options.failMagnet && magnet === options.failMagnet)
        return Response.json(
          {
            error: {
              code: "invalid_magnet",
              message: "Enter a valid magnet link.",
            },
          },
          { status: 400 },
        );
      return Response.json({ added: true }, { status: 202 });
    }
    if (method === "POST") return new Response(null, { status: 204 });
    return Response.json(
      { error: { message: "Unexpected request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter>
        <Torrents />
      </MemoryRouter>
    </Theme>,
  );
  return { fetch, calls };
}

function posts(calls: Call[], url: string) {
  return calls.filter((c) => c.method === "POST" && c.url === url);
}

function puts(calls: Call[], url: string) {
  return calls.filter((c) => c.method === "PUT" && c.url === url);
}

function row(name: string) {
  const cell = screen.getByText(name);
  const tr = cell.closest("tr");
  if (!tr) throw new Error(`row for ${name} not found`);
  return tr;
}

function filterButton(section: string, label: RegExp) {
  return within(
    screen.getByRole("list", { name: `${section} filters` }),
  ).getByRole("button", { name: label });
}

beforeEach(() => {
  window.localStorage.clear();
});

describe("Torrents", () => {
  it("lists built-in client torrents with transfer rates and controls", async () => {
    mount(overview({ items: [jobTorrent] }));
    expect(await screen.findByText("Fixture.Release.1080p")).toBeVisible();
    expect(screen.getByRole("grid", { name: "Torrents" })).toBeVisible();
    expect(filterButton("Source", /ReelScarey downloads/)).toHaveTextContent(
      "1",
    );
    expect(screen.getByRole("button", { name: "Pause" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Delete" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Pause" })).toBeDisabled();
    expect(screen.getByLabelText("Transfer rates")).toHaveTextContent(
      "2.0 KiB/s",
    );
    expect(screen.getByLabelText("Transfer rates")).toHaveTextContent(
      "Connected",
    );
    expect(screen.getByLabelText("Transfer rates")).toHaveTextContent(
      "DHT: 120 nodes",
    );
  });

  it("explains when the built-in client is disabled", async () => {
    mount({
      available: false,
      state: "disabled",
      message: "The built-in qBittorrent client is not enabled.",
      transfer: {},
      server: null,
      items: [],
    });
    expect(
      await screen.findByText("The built-in client is not enabled"),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "Open download clients" }),
    ).toHaveAttribute("href", "/download-clients");
  });

  it("counts and filters by status, source, category, tag and tracker", async () => {
    const user = userEvent.setup();
    mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    expect(filterButton("Status", /^All/)).toHaveTextContent("3");
    expect(filterButton("Status", /^Downloading/)).toHaveTextContent("1");
    expect(filterButton("Status", /^Seeding/)).toHaveTextContent("1");
    expect(filterButton("Status", /^Completed/)).toHaveTextContent("2");
    expect(filterButton("Status", /^Paused/)).toHaveTextContent("1");
    expect(filterButton("Status", /^Active/)).toHaveTextContent("1");
    expect(filterButton("Status", /^Stalled Uploading/)).toHaveTextContent("1");
    expect(filterButton("Source", /^Manual/)).toHaveTextContent("1");
    expect(filterButton("Source", /^Other/)).toHaveTextContent("1");
    expect(filterButton("Categories", /^isos/)).toHaveTextContent("1");
    expect(filterButton("Categories", /^Uncategorized/)).toHaveTextContent("2");
    // Internal reelscarey-* tags are grouped under Source, not listed as tags.
    const tags = screen.getByRole("list", { name: "Tags filters" });
    expect(within(tags).queryByText(/reelscarey/)).toBeNull();
    expect(filterButton("Tags", /^linux/)).toHaveTextContent("1");
    expect(filterButton("Tags", /^Untagged/)).toHaveTextContent("2");
    expect(filterButton("Trackers", /^Trackerless/)).toHaveTextContent("1");
    expect(
      filterButton("Trackers", /^tracker\.debian\.example:6969/),
    ).toHaveTextContent("1");

    await user.click(filterButton("Status", /^Paused/));
    expect(filterButton("Status", /^Paused/)).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByText("Ubuntu.ISO")).toBeVisible();
    expect(screen.queryByText("Fixture.Release.1080p")).toBeNull();
    expect(screen.queryByText("Debian.Netinst")).toBeNull();

    await user.click(filterButton("Status", /^All/));
    await user.click(filterButton("Source", /^ReelScarey downloads/));
    expect(screen.getByText("Fixture.Release.1080p")).toBeVisible();
    expect(screen.queryByText("Ubuntu.ISO")).toBeNull();

    await user.click(filterButton("Source", /^All/));
    await user.click(filterButton("Tags", /^linux/));
    expect(screen.getByText("Debian.Netinst")).toBeVisible();
    expect(screen.queryByText("Ubuntu.ISO")).toBeNull();
  });

  it("filters the list by name text", async () => {
    const user = userEvent.setup();
    mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    await user.type(screen.getByLabelText("Filter torrent list"), "ubuntu iso");
    expect(screen.getByText("Ubuntu.ISO")).toBeVisible();
    expect(screen.queryByText("Fixture.Release.1080p")).toBeNull();
    expect(screen.queryByText("Debian.Netinst")).toBeNull();
    expect(screen.getByLabelText("Transfer rates")).toHaveTextContent(
      "1 of 3 torrents",
    );
  });

  it("pauses a multi-selection through the bulk action endpoint", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    fireEvent.click(row("Fixture.Release.1080p"));
    fireEvent.click(row("Debian.Netinst"), { ctrlKey: true });
    expect(row("Fixture.Release.1080p")).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(row("Debian.Netinst")).toHaveAttribute("aria-selected", "true");
    expect(row("Ubuntu.ISO")).toHaveAttribute("aria-selected", "false");

    await user.click(screen.getByRole("button", { name: "Pause" }));
    await waitFor(() =>
      expect(posts(calls, "/api/v1/torrents/actions")).toHaveLength(1),
    );
    expect(posts(calls, "/api/v1/torrents/actions")[0].body).toEqual({
      hashes: [jobHash, otherHash],
      action: "pause",
    });
  });

  it("supports shift ranges, Ctrl+A and the Delete key", async () => {
    mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    fireEvent.click(row("Fixture.Release.1080p"));
    fireEvent.click(row("Debian.Netinst"), { shiftKey: true });
    expect(row("Ubuntu.ISO")).toHaveAttribute("aria-selected", "true");

    const grid = screen.getByRole("grid", { name: "Torrents" });
    fireEvent.click(row("Ubuntu.ISO"));
    fireEvent.keyDown(grid, { key: "ArrowDown" });
    expect(row("Debian.Netinst")).toHaveAttribute("aria-selected", "true");
    expect(row("Ubuntu.ISO")).toHaveAttribute("aria-selected", "false");

    fireEvent.keyDown(grid, { key: "a", ctrlKey: true });
    for (const name of [
      "Fixture.Release.1080p",
      "Ubuntu.ISO",
      "Debian.Netinst",
    ])
      expect(row(name)).toHaveAttribute("aria-selected", "true");

    fireEvent.keyDown(grid, { key: "Delete" });
    expect(
      await screen.findByRole("dialog", { name: "Delete 3 torrents" }),
    ).toBeVisible();
  });

  it("requires the delete review dialog and warns about ReelScarey downloads", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    fireEvent.click(row("Fixture.Release.1080p"));
    await user.click(screen.getByRole("button", { name: "Delete" }));

    const dialog = await screen.findByRole("dialog", {
      name: "Delete torrent",
    });
    expect(
      within(dialog).getByText(
        /can break a pending import or a seeding source/,
      ),
    ).toBeVisible();
    expect(posts(calls, "/api/v1/torrents/actions")).toHaveLength(0);

    await user.click(
      within(dialog).getByLabelText("Also delete files on disk"),
    );
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(posts(calls, "/api/v1/torrents/actions")).toHaveLength(1),
    );
    expect(posts(calls, "/api/v1/torrents/actions")[0].body).toEqual({
      hashes: [jobHash],
      action: "remove",
      delete_files: true,
      reviewed: true,
    });
  });

  it("deletes a manual torrent without the job warning and keeps files by default", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Ubuntu.ISO");
    fireEvent.click(row("Ubuntu.ISO"));
    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Delete torrent",
    });
    expect(within(dialog).queryByText(/Added by ReelScarey/)).toBeNull();
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(posts(calls, "/api/v1/torrents/actions")).toHaveLength(1),
    );
    expect(posts(calls, "/api/v1/torrents/actions")[0].body).toEqual({
      hashes: [manualHash],
      action: "remove",
      delete_files: false,
      reviewed: true,
    });
  });

  it("disables queue buttons when queueing is off in the client", async () => {
    const view = overview();
    mount({
      ...view,
      server: { ...view.server, queueing_enabled: false },
    });
    await screen.findByText("Fixture.Release.1080p");
    fireEvent.click(row("Fixture.Release.1080p"));
    for (const name of [
      "Move to top of queue",
      "Move up in queue",
      "Move down in queue",
      "Move to bottom of queue",
    ]) {
      const button = screen.getByRole("button", { name });
      expect(button).toBeDisabled();
      expect(button).toHaveAttribute(
        "title",
        "Queueing is disabled in qBittorrent",
      );
    }
    expect(screen.getByRole("button", { name: "Recheck" })).toBeEnabled();
  });

  it("queues the selection when queueing is enabled", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    fireEvent.click(row("Fixture.Release.1080p"));
    await user.click(
      screen.getByRole("button", { name: "Move to top of queue" }),
    );
    await waitFor(() =>
      expect(posts(calls, "/api/v1/torrents/actions")[0]?.body).toEqual({
        hashes: [jobHash],
        action: "queue_top",
      }),
    );
  });

  it("loads properties for one selected torrent and protects job file choices", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    expect(
      screen.getByText("Select one torrent to see its properties."),
    ).toBeVisible();
    fireEvent.click(row("Fixture.Release.1080p"));

    expect(await screen.findByText("fixture-maker")).toBeVisible();
    expect(
      calls.some(
        (c) => c.url === `/api/v1/torrents/${jobHash}?section=general`,
      ),
    ).toBe(true);
    expect(screen.getByRole("tab", { name: /^General/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByText("Transfer")).toBeVisible();
    expect(screen.getByText("Information")).toBeVisible();
    expect(screen.getByText("8 × 16.0 KiB (have 2)")).toBeVisible();
    expect(screen.getByRole("img", { name: /Pieces: 2 of 8/ })).toBeVisible();

    await user.click(screen.getByRole("tab", { name: /^Trackers/ }));
    expect(await screen.findByText("Working")).toBeVisible();
    expect(
      screen.getByText(/shortened to the host to hide private passkeys/),
    ).toBeVisible();

    await user.click(screen.getByRole("tab", { name: /^Content/ }));
    expect(await screen.findByText("movie.mkv")).toBeVisible();
    expect(
      screen.getByText("ReelScarey chooses files for its own downloads."),
    ).toBeVisible();
    expect(screen.getByLabelText("Priority for movie.mkv")).toBeDisabled();
    expect(screen.getByLabelText("Priority for Folder")).toBeDisabled();

    // A second selection hides the per-torrent panel again.
    fireEvent.click(row("Ubuntu.ISO"), { ctrlKey: true });
    expect(
      screen.getByText("Select one torrent to see its properties."),
    ).toBeVisible();
  });

  it("changes file priorities for manual torrents, including whole folders", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Ubuntu.ISO");
    fireEvent.click(row("Ubuntu.ISO"));
    await user.click(screen.getByRole("tab", { name: /^Content/ }));
    const file = await screen.findByLabelText("Priority for movie.mkv");
    expect(file).toBeEnabled();
    expect(
      screen.queryByText("ReelScarey chooses files for its own downloads."),
    ).toBeNull();

    await user.selectOptions(file, "0");
    await waitFor(() =>
      expect(posts(calls, `/api/v1/torrents/${manualHash}/files`)).toHaveLength(
        1,
      ),
    );
    expect(
      posts(calls, `/api/v1/torrents/${manualHash}/files`)[0].body,
    ).toEqual({
      indexes: [0],
      priority: 0,
    });

    await waitFor(() =>
      expect(screen.getByLabelText("Priority for Folder")).toBeEnabled(),
    );
    await user.selectOptions(screen.getByLabelText("Priority for Folder"), "7");
    await waitFor(() =>
      expect(posts(calls, `/api/v1/torrents/${manualHash}/files`)).toHaveLength(
        2,
      ),
    );
    const folderBody = posts(calls, `/api/v1/torrents/${manualHash}/files`)[1]
      .body as { indexes: number[]; priority: number };
    expect(folderBody.priority).toBe(7);
    expect([...folderBody.indexes].sort()).toEqual([0, 1]);
  });

  it("toggles the alternative speed limits", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    const toggle = screen.getByRole("button", {
      name: "Alternative speed limits",
    });
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    await user.click(toggle);
    await waitFor(() =>
      expect(posts(calls, "/api/v1/torrents/speed-limits")).toHaveLength(1),
    );
    expect(posts(calls, "/api/v1/torrents/speed-limits")[0].body).toEqual({
      alt_enabled: true,
    });
  });

  it("sets persistent torrent queue limits", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    await user.click(
      screen.getByRole("button", { name: "Set torrent queue limits" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Torrent queue limits",
    });
    await user.click(within(dialog).getByRole("checkbox"));
    const downloads = within(dialog).getByLabelText("Maximum active downloads");
    const uploads = within(dialog).getByLabelText("Maximum active uploads");
    const totals = within(dialog).getByLabelText(
      "Maximum active torrents overall",
    );
    await user.clear(downloads);
    await user.type(downloads, "7");
    await user.clear(uploads);
    await user.type(uploads, "5");
    await user.clear(totals);
    await user.type(totals, "9");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(posts(calls, "/api/v1/torrents/queue-limits")[0]?.body).toEqual({
        queueing_enabled: false,
        max_active_downloads: 7,
        max_active_uploads: 5,
        max_active_torrents: 9,
        revision: 1,
      }),
    );
  });

  it("sets the global download limit in bytes per second", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    await user.click(
      screen.getByRole("button", { name: /set global download limit/ }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Global Download Speed Limit",
    });
    await user.type(within(dialog).getByLabelText("Limit (KiB/s)"), "512");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(posts(calls, "/api/v1/torrents/speed-limits")[0]?.body).toEqual({
        download_limit: 512 * 1024,
      }),
    );
  });

  it("adds magnet links one at a time and reports failed lines", async () => {
    const user = userEvent.setup();
    const bad = "magnet:?xt=urn:btih:bad";
    const good = "magnet:?xt=urn:btih:3333333333333333333333333333333333333333";
    const { calls } = mount(overview(), { failMagnet: bad });
    await screen.findByText("Fixture.Release.1080p");
    await user.click(screen.getByRole("button", { name: "Add Torrent Link" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Add Torrent Links",
    });
    const area = within(dialog).getByLabelText(/One magnet link per line/);
    await user.type(area, `${good}{enter}${bad}`);
    await user.click(within(dialog).getByRole("button", { name: "Download" }));

    expect(
      await within(dialog).findByText(/1 of the links could not be added/),
    ).toBeVisible();
    expect(within(dialog).getByText(/Enter a valid magnet link/)).toBeVisible();
    expect(area).toHaveValue(bad);
    expect(posts(calls, "/api/v1/torrents").map((c) => c.body)).toEqual([
      { magnet: good },
      { magnet: bad },
    ]);
  });

  it("refuses torrent URLs in the link dialog", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    await user.click(screen.getByRole("button", { name: "Add Torrent Link" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Add Torrent Links",
    });
    await user.type(
      within(dialog).getByLabelText(/One magnet link per line/),
      "https://example.invalid/file.torrent",
    );
    await user.click(within(dialog).getByRole("button", { name: "Download" }));
    expect(
      await within(dialog).findByText(/Only magnet links are accepted/),
    ).toBeVisible();
    expect(posts(calls, "/api/v1/torrents")).toHaveLength(0);
  });

  it("uploads torrent files and rejects files over 10 MiB", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    const small = new File(["d4:infoe"], "small.torrent", {
      type: "application/x-bittorrent",
    });
    const large = new File(
      [new Uint8Array(10 * 1024 * 1024 + 1)],
      "large.torrent",
      {
        type: "application/x-bittorrent",
      },
    );
    await user.upload(screen.getByLabelText("Torrent files"), [small, large]);
    expect(
      await screen.findByText(/large\.torrent: larger than 10 MiB/),
    ).toBeVisible();
    const uploads = calls.filter(
      (c) => c.method === "POST" && c.url === "/api/v1/torrents",
    );
    expect(uploads).toHaveLength(1);
    expect(uploads[0].body).toBeInstanceOf(ArrayBuffer);
  });

  it("persists the sort column", async () => {
    const user = userEvent.setup();
    mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    await user.click(screen.getByRole("button", { name: "Name" }));
    expect(
      JSON.parse(window.localStorage.getItem("reelscarey.torrents.sort")!),
    ).toEqual({
      key: "name",
      direction: "asc",
    });
    const names = screen
      .getAllByRole("row")
      .slice(1)
      .map((r) => within(r).getAllByRole("gridcell")[1].textContent);
    expect(names).toEqual([
      "Debian.Netinst",
      "Fixture.Release.1080p",
      "Ubuntu.ISO",
    ]);
  });

  it("saves the seeding policy and reports the manual run", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    await waitFor(() =>
      expect(screen.getByLabelText("Transfer rates")).toHaveTextContent(
        "Seeding limit: off",
      ),
    );

    await user.click(screen.getByRole("button", { name: "Seeding policy" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Seeding policy",
    });
    await user.click(
      within(dialog).getByLabelText(/Enforce an automatic seeding limit/),
    );
    await user.selectOptions(
      within(dialog).getByLabelText("When a limit is reached"),
      "remove_and_delete",
    );
    const ratio = within(dialog).getByLabelText("Maximum ratio");
    await user.clear(ratio);
    await user.type(ratio, "1.5");
    const hours = within(dialog).getByLabelText("Seeding time hours");
    await user.clear(hours);
    await user.type(hours, "2");
    await user.click(
      within(dialog).getByLabelText(/Include ReelScarey downloads/),
    );
    expect(within(dialog).getByText(/never deletes their files/)).toBeVisible();

    await user.click(within(dialog).getByRole("button", { name: "Run now" }));
    expect(
      await within(dialog).findByText(/Checked 3 torrents; acted on 2\./),
    ).toBeVisible();
    expect(posts(calls, "/api/v1/torrents/share-policy/run")).toHaveLength(1);

    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(puts(calls, "/api/v1/torrents/share-policy")).toHaveLength(1),
    );
    expect(puts(calls, "/api/v1/torrents/share-policy")[0].body).toEqual({
      ...policyFixture,
      enabled: true,
      action: "remove_and_delete",
      max_ratio: 1.5,
      max_seeding_minutes: 120,
      include_reelscarey: true,
    });
    await waitFor(() =>
      expect(screen.getByLabelText("Transfer rates")).toHaveTextContent(
        "Seeding limit: ratio 1.5 · 2 h · remove torrent and delete files",
      ),
    );
  });

  it("toggles the auto-pause option for public sources", async () => {
    const user = userEvent.setup();
    const { calls } = mount(overview());
    await screen.findByText("Fixture.Release.1080p");
    const toggle = screen.getByRole("button", {
      name: "Pause public when done",
    });
    await waitFor(() =>
      expect(toggle).toHaveAttribute("aria-pressed", "false"),
    );

    await user.click(toggle);
    await waitFor(() =>
      expect(puts(calls, "/api/v1/torrents/public-pause")).toHaveLength(1),
    );
    expect(puts(calls, "/api/v1/torrents/public-pause")[0].body).toEqual({
      enabled: true,
      revision: 1,
      updated_at: 0,
    });
    await waitFor(() => expect(toggle).toHaveAttribute("aria-pressed", "true"));
    expect(
      await screen.findByText(
        /Finished public-tracker torrents will be paused automatically/,
      ),
    ).toBeVisible();

    await user.click(toggle);
    await waitFor(() =>
      expect(puts(calls, "/api/v1/torrents/public-pause")).toHaveLength(2),
    );
    expect(puts(calls, "/api/v1/torrents/public-pause")[1].body).toEqual({
      enabled: false,
      revision: 2,
      updated_at: 0,
    });
    await waitFor(() =>
      expect(toggle).toHaveAttribute("aria-pressed", "false"),
    );
  });
});
