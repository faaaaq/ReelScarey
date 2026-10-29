import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { it, expect, vi } from "vitest";
import { Storage } from "../pages/Storage";
it("browses only selected mounted folders, supports navigation and performs no writes", async () => {
  const requests = vi.fn(async (url: string, o?: RequestInit) => {
    expect(o?.method ?? "GET").toBe("GET");
    if (url.includes("/migration")) return Response.json({ state: "none" });
    const path = new URL(url, "http://localhost").searchParams.get("path");
    return Response.json({
      roots: [
        { id: "media", path: "/media" },
        { id: "archive", path: "/archive" },
      ],
      root: "media",
      path: path ?? "",
      page: 1,
      total: path ? 1 : 2,
      limit: 100,
      items: path
        ? [
            {
              name: "owned.mkv",
              path: "Series/owned.mkv",
              kind: "file",
              size_bytes: 2048,
              modified_at: 1,
            },
          ]
        : [
            {
              name: "Series",
              path: "Series",
              kind: "directory",
              size_bytes: 0,
              modified_at: 1,
            },
            {
              name: "link",
              path: "link",
              kind: "blocked",
              size_bytes: 0,
              modified_at: 1,
            },
          ],
    });
  });
  vi.stubGlobal("fetch", requests);
  render(
    <Theme>
      <MemoryRouter>
        <Storage />
      </MemoryRouter>
    </Theme>,
  );
  await screen.findByRole("button", { name: "Open folder Series" });
  expect(screen.getByText("Link or special file")).toBeInTheDocument();
  await userEvent.click(
    screen.getByRole("button", { name: "Open folder Series" }),
  );
  await screen.findByText("owned.mkv");
  expect(screen.getByText("2.0 KiB")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Root" }));
  await screen.findByRole("button", { name: "Open folder Series" });
  await userEvent.selectOptions(
    screen.getByRole("combobox", { name: /Mounted folder/ }),
    "archive",
  );
  expect(requests).toHaveBeenCalledWith(
    expect.stringContaining("root=archive"),
    expect.anything(),
  );
});
it("shows saved migration evidence and offline activation guidance without live copy controls", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      Response.json(
        url.includes("/migration")
          ? {
              state: "completed",
              source: "/media",
              destination: "/migration-target/library",
              digest: "a".repeat(64),
              files: 3,
              directories: 2,
              bytes: 4096,
              backup: "/backups/storage-safety",
            }
          : {
              roots: [{ id: "media", path: "/media" }],
              root: "media",
              path: "",
              items: [],
              page: 1,
              total: 0,
              limit: 100,
            },
      ),
    ),
  );
  render(
    <Theme>
      <MemoryRouter>
        <Storage />
      </MemoryRouter>
    </Theme>,
  );
  await screen.findByText("completed");
  expect(screen.getByText("/backups/storage-safety")).toBeInTheDocument();
  expect(screen.getByText("media-root-activate DIGEST")).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /copy|activate|migrate/i }),
  ).not.toBeInTheDocument();
});
