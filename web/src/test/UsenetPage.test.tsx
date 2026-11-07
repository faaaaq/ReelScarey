import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Usenet } from "../pages/Usenet";

const status = {
  present: true,
  installed: true,
  enabled: true,
  configured: true,
  state: "running",
  revision: 3,
  active_jobs: 0,
  name: "Built-in SABnzbd",
  host: "news.example.com",
  username: "fixture",
  port: 563,
  tls: true,
  connections: 8,
};

const queue = {
  available: true,
  state: "Downloading",
  paused: false,
  speed: 2 * 1024 * 1024,
  remaining: 750 * 1024 * 1024,
  eta: "0:10:00",
  limit_bytes: 0,
  limit_percent: 100,
  items: [
    {
      id: "SABnzbd_nzo_aaa111",
      name: "reelscarey-movie",
      size: 1000 * 1024 * 1024,
      remaining: 250 * 1024 * 1024,
      progress: 0.75,
      status: "Downloading",
      speed: 2 * 1024 * 1024,
      eta: "0:05:00",
      category: "reelscarey",
      priority: "Normal",
    },
    {
      id: "SABnzbd_nzo_bbb222",
      name: "reelscarey-paused",
      size: 500 * 1024 * 1024,
      remaining: 500 * 1024 * 1024,
      progress: 0,
      status: "Paused",
      speed: 0,
      eta: "0:00:00",
      category: "reelscarey",
      priority: "Low",
    },
  ],
};

function overview(overrides: Record<string, unknown> = {}) {
  return {
    status,
    queue,
    ...overrides,
  };
}

function renderPage() {
  return render(
    <Theme>
      <MemoryRouter>
        <Usenet />
      </MemoryRouter>
    </Theme>,
  );
}

function mockApi(data: ReturnType<typeof overview>) {
  const requests = vi.fn(
    async (url: string, options?: RequestInit): Promise<Response> => {
      if (url.includes("/usenet/news") && options?.method === "PUT") {
        return Response.json(status);
      }
      if (
        url.includes("/usenet/queue/actions") ||
        url.includes("/usenet/speed-limits")
      ) {
        return new Response(null, { status: 204 });
      }
      if (url.endsWith("/usenet/queue") && options?.method === "POST") {
        return Response.json(
          { added: true, id: "SABnzbd_nzo_new123" },
          { status: 202 },
        );
      }
      if (url.endsWith("/usenet") && options?.method === "PUT") {
        return Response.json(status);
      }
      return Response.json(data);
    },
  );
  vi.stubGlobal("fetch", requests);
  return requests;
}

describe("Usenet page", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("shows the status strip and a dense queue with honest labels", async () => {
    mockApi(overview());
    renderPage();
    expect(await screen.findByText("reelscarey-movie")).toBeInTheDocument();
    expect((await screen.findAllByText("Running")).length).toBeGreaterThan(0);
    expect(screen.getByText("SABnzbd installed")).toBeInTheDocument();
    expect(screen.getAllByText("2.0 MiB/s").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Downloading").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Paused").length).toBeGreaterThan(0);
    expect(screen.getByText("reelscarey-paused")).toBeInTheDocument();
    expect(screen.queryByText("Add a news server")).not.toBeInTheDocument();
  });

  it("explains a missing news server instead of showing an empty working queue", async () => {
    mockApi(
      overview({
        status: {
          ...status,
          present: false,
          enabled: false,
          configured: false,
          state: "stopped",
          host: "",
          username: "",
          port: 563,
          connections: 0,
        },
        queue: {
          ...queue,
          available: false,
          state: "disabled",
          items: [],
          message:
            "The built-in Usenet client is not configured. Save a news server first.",
        },
      }),
    );
    renderPage();
    expect(await screen.findByText("Add a news server")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Open news server settings" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add NZB" })).toBeDisabled();
    expect(screen.getAllByRole("button", { name: "Pause" })[0]).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Turn on client" }),
    ).toBeDisabled();
  });

  it("explains a deployment without the SABnzbd binary", async () => {
    mockApi(
      overview({
        status: {
          ...status,
          installed: false,
          enabled: false,
          configured: false,
          state: "stopped",
        },
        queue: { ...queue, available: false, state: "disabled", items: [] },
      }),
    );
    renderPage();
    expect(
      await screen.findByText("The bundled Usenet client is not installed"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Open news server settings" }),
    ).not.toBeInTheDocument();
  });

  it("requires an explicit review before deleting and forwards delete_files", async () => {
    const requests = mockApi(overview());
    renderPage();
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("checkbox", { name: "Select reelscarey-movie" }),
    );
    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await user.click(
      screen.getByRole("checkbox", {
        name: "Also delete the job's downloaded files in SABnzbd",
      }),
    );
    const confirm = screen.getByRole("button", { name: "Delete jobs" });
    expect(confirm).toBeDisabled();
    expect(
      requests.mock.calls.some(([url]) =>
        String(url).includes("/usenet/queue/actions"),
      ),
    ).toBe(false);
    await user.click(
      screen.getByRole("checkbox", { name: "I reviewed these jobs" }),
    );
    await user.click(confirm);
    await waitFor(() =>
      expect(
        requests.mock.calls.some(([url]) =>
          String(url).includes("/usenet/queue/actions"),
        ),
      ).toBe(true),
    );
    const sent = requests.mock.calls.find(([url]) =>
      String(url).includes("/usenet/queue/actions"),
    )!;
    expect(JSON.parse(sent[1]!.body as string)).toEqual({
      ids: ["SABnzbd_nzo_aaa111"],
      action: "delete",
      delete_files: true,
      reviewed: true,
    });
  });

  it("saves the news server payload without exposing the saved password", async () => {
    const requests = mockApi(overview());
    renderPage();
    const user = userEvent.setup();
    await screen.findByText("News server");
    const connections = screen.getByLabelText("Connections");
    await user.clear(connections);
    await user.type(connections, "12");
    const password = screen.getByLabelText("Password");
    await user.type(password, "new-nntp-secret");
    await user.click(screen.getByRole("button", { name: "Save news server" }));
    await waitFor(() =>
      expect(
        requests.mock.calls.some(
          ([url, options]) =>
            String(url).endsWith("/usenet/news") && options?.method === "PUT",
        ),
      ).toBe(true),
    );
    const sent = requests.mock.calls.find(
      ([url, options]) =>
        String(url).endsWith("/usenet/news") && options?.method === "PUT",
    )!;
    expect(JSON.parse(sent[1]!.body as string)).toEqual({
      host: "news.example.com",
      port: 563,
      tls: true,
      username: "fixture",
      connections: 12,
      password: "new-nntp-secret",
    });
    expect(screen.queryByText("new-nntp-secret")).not.toBeInTheDocument();
  });

  it("uploads only NZB files with the dedicated media type", async () => {
    const requests = mockApi(overview());
    renderPage();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Add NZB" }));
    const file = new File(["<nzb/>"], "release.nzb", {
      type: "application/x-nzb",
    });
    await user.upload(screen.getByLabelText("NZB files"), file);
    await user.click(screen.getByRole("button", { name: "Add to queue" }));
    await waitFor(() =>
      expect(
        requests.mock.calls.some(
          ([url, options]) =>
            String(url).endsWith("/usenet/queue") && options?.method === "POST",
        ),
      ).toBe(true),
    );
    const sent = requests.mock.calls.find(
      ([url, options]) =>
        String(url).endsWith("/usenet/queue") && options?.method === "POST",
    )!;
    expect((sent[1]!.headers as Record<string, string>)["Content-Type"]).toBe(
      "application/x-nzb",
    );
  });

  it("sets an absolute speed limit from the toolbar", async () => {
    const requests = mockApi(overview());
    renderPage();
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("button", { name: "Speed limit" }),
    );
    fireEvent.change(
      screen.getByLabelText("Limit in KiB/s (0 removes the limit)"),
      { target: { value: "2048" } },
    );
    await user.click(screen.getByRole("button", { name: "Save limit" }));
    await waitFor(() =>
      expect(
        requests.mock.calls.some(([url]) =>
          String(url).includes("/usenet/speed-limits"),
        ),
      ).toBe(true),
    );
    const sent = requests.mock.calls.find(([url]) =>
      String(url).includes("/usenet/speed-limits"),
    )!;
    expect(JSON.parse(sent[1]!.body as string)).toEqual({
      download_limit: 2048 * 1024,
    });
  });
});
