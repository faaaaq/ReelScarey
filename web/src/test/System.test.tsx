import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import App from "../App";

const report = {
  checked_at: 1792713600,
  checks: [
    {
      id: "database",
      title: "Database",
      state: "ready",
      message: "Integrity verified.",
    },
  ],
  resources: {
    heap_bytes: 1048576,
    go_memory_bytes: 2097152,
    goroutines: 5,
    logical_cpus: 2,
  },
};
function setup(path: string, recovery = false, fail = false) {
  let current = recovery;
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    if (url.endsWith("/auth/session"))
      return Response.json({
        authenticated: true,
        setup_required: false,
        session: {
          username: "test",
          csrf_token: "test-csrf",
          expires_at: 9999999999,
        },
      });
    if (url.endsWith("/setup"))
      return Response.json({
        recovery_pending: false,
        steps: [
          {
            id: "torznab",
            title: "Release search (can wait)",
            message: "Connect when ready.",
            state: "optional",
            href: "/settings#torznab",
            optional: true,
          },
        ],
      });
    if (url.endsWith("/system/recovery/resume")) {
      current = false;
      return Response.json({ recovery_pending: false });
    }
    if (url.endsWith("/system/checks")) {
      if (options?.method === "POST" && fail)
        return Response.json(
          { error: { message: "Checks are busy. Try again." } },
          { status: 409 },
        );
      return Response.json({
        recovery_pending: current,
        report: options?.method === "POST" ? report : null,
      });
    }
    if (url.endsWith("/system"))
      return Response.json({
        version: "9.9.9-test",
        database: "connected",
        schema_version: 42,
        uptime_seconds: 3700,
        paths: { config: "/config", media: "/media", downloads: "/downloads" },
      });
    throw new Error(`Unexpected request ${url}`);
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </Theme>,
  );
  return fetch;
}
describe("installation and recovery", () => {
  it("allows deferred Torznab and links to its real settings", async () => {
    setup("/setup");
    expect(await screen.findByText("Optional")).toBeVisible();
    expect(screen.getByRole("link", { name: "Open" })).toHaveAttribute(
      "href",
      "/settings#torznab",
    );
  });
  it("runs storage probes only after a deliberate click", async () => {
    const fetch = setup("/system");
    await screen.findByText("No checks run this session");
    expect(await screen.findByText("9.9.9-test")).toBeVisible();
    expect(fetch.mock.calls.some(([, o]) => o?.method === "POST")).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "Run checks" }));
    expect(await screen.findByText("Passed")).toBeVisible();
    expect(screen.getByText("Go runtime memory")).toBeVisible();
    expect(
      fetch.mock.calls.find(([, o]) => o?.method === "POST")?.[1]?.headers,
    ).toMatchObject({ "X-CSRF-Token": "test-csrf" });
  });
  it("requires an explicit recovery review before resuming workers", async () => {
    const fetch = setup("/system", true);
    const resume = await screen.findByRole("button", {
      name: "Resume workers",
    });
    expect(resume).toBeDisabled();
    await userEvent.click(screen.getByRole("checkbox"));
    await userEvent.click(resume);
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Recovery hold released",
    );
    const sent = fetch.mock.calls.find(([url]) =>
      url.endsWith("/system/recovery/resume"),
    );
    expect(JSON.parse(String(sent?.[1]?.body))).toEqual({ reviewed: true });
    expect(
      screen.queryByRole("region", { name: "Recovery review" }),
    ).not.toBeInTheDocument();
  });
  it("shows failed checks without claiming success and permits retry", async () => {
    setup("/system", false, true);
    await screen.findByText("No checks run this session");
    await userEvent.click(screen.getByRole("button", { name: "Run checks" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Checks are busy",
    );
    expect(screen.getByRole("button", { name: "Run checks" })).toBeEnabled();
    expect(screen.queryByText("Passed")).not.toBeInTheDocument();
  });
});
