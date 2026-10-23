import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import App from "../App";
import type { DownloadJob } from "../api";

const movie = {
  id: 1,
  kind: "movie",
  tmdb_id: 123,
  title: "Test Movie",
  year: 2026,
  monitored: true,
  status: "missing",
  overview: "A test-only movie.",
  poster_url: "",
  poster_path: "",
  release_date: "2026-01-01",
  runtime_minutes: 96,
};
const release = {
  id: "a".repeat(32),
  movie_id: 1,
  title: "Test.Movie.2026.1080p",
  size_bytes: 2000000000,
  seeders: 4,
  peers: 6,
  published_at: "2026-10-22T00:00:00Z",
};
const job: DownloadJob = {
  id: "b".repeat(32),
  movie_id: 1,
  movie_title: "Test Movie",
  title: release.title,
  info_hash: "a".repeat(40),
  state: "downloading",
  progress: 0.5,
  total_bytes: 2000000000,
  downloaded_bytes: 1000000000,
  client_state: "downloading",
  selected_file: null,
  choices: [],
  attempts: 0,
  error_code: "",
  error_message: "",
  created_at: 1,
  updated_at: 1,
};
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
  extra?: (url: string, options?: RequestInit) => Response | undefined,
) {
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const custom = extra?.(url, options);
    if (custom) return custom;
    if (url.endsWith("/auth/session"))
      return Response.json({
        authenticated: true,
        setup_required: false,
        session: {
          username: "testadmin",
          csrf_token: "test-csrf",
          expires_at: 9999999999,
        },
      });
    if (url.includes("/integrations/"))
      return Response.json({
        configured: true,
        url: "https://service.invalid",
        username: "testadmin",
        download_path: "/downloads",
      });
    if (url.endsWith("/library/movies/1")) return Response.json(movie);
    if (url.includes("/releases?")) return Response.json({ items: [release] });
    if (url.includes("/downloads?page="))
      return Response.json({ items: [job], page: 1, total: 1, limit: 24 });
    if (url.endsWith("/settings"))
      return Response.json({
        library_name: "My library",
        movie_folder: "Movies",
        series_folder: "TV",
        revision: 1,
      });
    if (url.endsWith("/system"))
      return Response.json({
        paths: { config: "/config", media: "/media", downloads: "/downloads" },
        version: "test",
        schema_version: 3,
      });
    if (url.endsWith("/providers/tmdb"))
      return Response.json({ configured: false });
    return Response.json(
      { error: { message: "Unexpected test request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("release downloads and imports", () => {
  it("searches canonical movie releases and queues only the selected opaque ID", async () => {
    const fetch = setup((url, options) =>
      url.endsWith("/downloads") && options?.method === "POST"
        ? Response.json({ created: true, item: job }, { status: 202 })
        : undefined,
    );
    mount("/releases?movie=1");
    await screen.findByRole("heading", { name: "Releases for Test Movie" });
    await userEvent.click(
      screen.getByRole("button", { name: "Search releases" }),
    );
    expect(
      await screen.findByRole("heading", { name: release.title }),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Download" }));
    expect(
      await screen.findByRole("heading", { name: "Activity" }),
    ).toBeVisible();
    await screen.findByText("50% · 953.7 MiB of 1.9 GiB · downloading");
    const sent = fetch.mock.calls.find(
      ([url, options]) =>
        url.endsWith("/downloads") && options?.method === "POST",
    );
    expect(JSON.parse(String(sent?.[1]?.body))).toEqual({
      release_id: release.id,
    });
    expect(sent?.[1]?.headers).toMatchObject({ "X-CSRF-Token": "test-csrf" });
    expect(
      fetch.mock.calls.some(([url]) =>
        url.includes("query=Test%20Movie%202026"),
      ),
    ).toBe(true);
  });
  it("explains missing indexer configuration without inventing releases", async () => {
    const fetch = setup((url) =>
      url.endsWith("/integrations/torznab")
        ? Response.json({ configured: false, url: "" })
        : undefined,
    );
    mount("/releases?movie=1");
    expect(
      await screen.findByRole("link", { name: "Connect indexer" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Download" }),
    ).not.toBeInTheDocument();
    expect(fetch.mock.calls.some(([url]) => url.includes("/releases?"))).toBe(
      false,
    );
  });
  it("keeps failed submissions visible without pretending they downloaded", async () => {
    setup((url, options) =>
      url.endsWith("/downloads") && options?.method === "POST"
        ? Response.json(
            { error: { message: "The client is not configured." } },
            { status: 409 },
          )
        : undefined,
    );
    mount("/releases?movie=1");
    await screen.findByRole("heading", { name: "Releases for Test Movie" });
    await userEvent.click(
      screen.getByRole("button", { name: "Search releases" }),
    );
    await screen.findByRole("button", { name: "Download" });
    await userEvent.click(screen.getByRole("button", { name: "Download" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The client is not configured",
    );
    expect(screen.getByRole("heading", { name: release.title })).toBeVisible();
  });
  it("requires a file choice before resuming an ambiguous import", async () => {
    let current = {
      ...job,
      state: "blocked",
      error_code: "choose_file",
      error_message: "Choose the correct file.",
      choices: [
        { index: 3, name: "feature.mkv", size_bytes: 5000 },
        { index: 7, name: "second.mkv", size_bytes: 4000 },
      ],
    };
    const fetch = setup((url) => {
      if (url.endsWith(`/downloads/${job.id}/file`)) {
        current = {
          ...current,
          state: "completed",
          error_code: "",
          error_message: "",
          choices: [],
        };
        return Response.json(current);
      }
      if (url.includes("/downloads?page="))
        return Response.json({
          items: [current],
          page: 1,
          total: 1,
          limit: 24,
        });
    });
    mount("/activity");
    await screen.findByLabelText("Movie file to import");
    expect(
      screen.getByRole("button", { name: "Import selected file" }),
    ).toBeDisabled();
    await userEvent.selectOptions(
      screen.getByLabelText("Movie file to import"),
      "7",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Import selected file" }),
    );
    expect(await screen.findByText("In library")).toBeVisible();
    const sent = fetch.mock.calls.find(([url]) =>
      url.endsWith(`/downloads/${job.id}/file`),
    );
    expect(JSON.parse(String(sent?.[1]?.body))).toEqual({ index: 7 });
  });
  it("saves qBittorrent credentials once and omits them on later edits", async () => {
    let configured = false;
    const fetch = setup((url, options) => {
      if (url.endsWith("/integrations/qbittorrent")) {
        if (options?.method === "PUT") {
          configured = true;
          return Response.json({ configured: true, tested: true });
        }
        return Response.json({
          configured,
          url: configured ? "https://qbt.invalid" : "",
          username: configured ? "testadmin" : "",
          download_path: configured ? "/downloads" : "",
        });
      }
    });
    mount("/settings");
    const region = await screen.findByRole("region", {
      name: "qBittorrent settings",
    });
    const form = within(region);
    await form.findByLabelText("Web UI URL");
    await userEvent.type(
      form.getByLabelText("Web UI URL"),
      "https://qbt.invalid",
    );
    await userEvent.type(form.getByLabelText("Web UI username"), "testadmin");
    const password = form.getByLabelText("Web UI password");
    await userEvent.type(password, "test-only-qbt-password");
    await userEvent.type(
      form.getByLabelText("qBittorrent downloads folder"),
      "/downloads",
    );
    await userEvent.click(
      form.getByRole("button", { name: "Save & test connection" }),
    );
    expect(
      await form.findByText("qBittorrent connection verified and saved."),
    ).toBeVisible();
    await waitFor(() => expect(password).toHaveValue(""));
    await form.findByRole("button", { name: "Test saved connection" });
    await userEvent.click(
      form.getByRole("button", { name: "Save & test connection" }),
    );
    const puts = fetch.mock.calls.filter(
      ([url, options]) =>
        url.endsWith("/integrations/qbittorrent") && options?.method === "PUT",
    );
    expect(JSON.parse(String(puts[0]?.[1]?.body))).toMatchObject({
      password: "test-only-qbt-password",
      download_path: "/downloads",
    });
    expect(JSON.parse(String(puts[1]?.[1]?.body))).not.toHaveProperty(
      "password",
    );
  });
});
