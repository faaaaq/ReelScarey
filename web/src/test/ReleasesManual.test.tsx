import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { it, expect, vi } from "vitest";
import { Releases } from "../pages/Releases";

function renderReleases(entry: string) {
  return render(
    <Theme>
      <MemoryRouter initialEntries={[entry]}>
        <Releases />
      </MemoryRouter>
    </Theme>,
  );
}

function stubFetch(handlers: (url: string, o?: RequestInit) => Response) {
  return vi.fn(async (url: string, o?: RequestInit) => handlers(url, o));
}

it("attaches an uploaded torrent to the movie and opens Activity", async () => {
  const requests = stubFetch((url, o) => {
    if (url.includes("/manual-torrent")) {
      expect(o?.method).toBe("POST");
      expect(o?.body).toBeInstanceOf(File);
      return Response.json({ item: {}, created: true });
    }
    if (url.endsWith("/versions")) return Response.json({ enabled: false });
    if (url.includes("/organization/files"))
      return Response.json({ items: [] });
    if (url.includes("/library/movies/7"))
      return Response.json({
        id: 7,
        title: "Fixture Movie",
        year: 2020,
        kind: "movie",
        tmdb_id: 1,
      });
    return Response.json({ configured: true, items: [] });
  });
  vi.stubGlobal("fetch", requests);
  renderReleases("/releases?movie=7");
  await userEvent.click(
    await screen.findByRole("button", { name: "Attach torrent" }),
  );
  const input = await screen.findByLabelText("Torrent file");
  await userEvent.upload(
    input,
    new File([new Uint8Array([1, 2, 3])], "release.torrent", {
      type: "application/x-bittorrent",
    }),
  );
  await userEvent.click(
    screen.getByRole("button", { name: "Attach and queue" }),
  );
  await waitFor(() =>
    expect(
      requests.mock.calls.some(([url]) =>
        String(url).includes("/manual-torrent"),
      ),
    ).toBe(true),
  );
});

it("offers season-pack attachment with explicit episode numbers for series", async () => {
  const requests = stubFetch((url, o) => {
    if (url.includes("/manual-torrent")) {
      expect(url).toContain("season=1");
      expect(url).toContain("episodes=2");
      expect(o?.method).toBe("POST");
      return Response.json({ item: {}, created: true });
    }
    if (url.includes("/library/series/9"))
      return Response.json({
        id: 9,
        title: "Fixture Show",
        kind: "series",
        tmdb_id: 2,
      });
    return Response.json({ configured: true, items: [] });
  });
  vi.stubGlobal("fetch", requests);
  renderReleases("/releases?series=9&season=1");
  await userEvent.click(
    await screen.findByRole("button", { name: "Attach torrent" }),
  );
  const input = await screen.findByLabelText("Torrent file");
  await userEvent.upload(
    input,
    new File([new Uint8Array([9])], "pack.torrent", {
      type: "application/x-bittorrent",
    }),
  );
  await userEvent.type(screen.getByLabelText(/Episode numbers/), "2");
  await userEvent.click(
    screen.getByRole("button", { name: "Attach and queue" }),
  );
  await waitFor(() =>
    expect(
      requests.mock.calls.some(([url]) =>
        String(url).includes("/manual-torrent"),
      ),
    ).toBe(true),
  );
});

it("locks a replacement search to the file it was started from", async () => {
  const requests = stubFetch((url) => {
    if (url.includes("/organization/files"))
      return Response.json({
        items: [
          { id: 41, path: "tvshows/Show/Season 38/Show - S38E02 - Two.mkv" },
          { id: 42, path: "tvshows/Show/Season 34/Show - S34E13 - Old.mkv" },
        ],
      });
    if (url.includes("/library/series/9/seasons/38"))
      return Response.json({ season: {}, episodes: [], revision: 1 });
    if (url.includes("/library/series/9"))
      return Response.json({
        id: 9,
        kind: "series",
        tmdb_id: 456,
        title: "Show",
        year: 1989,
      });
    return Response.json({ configured: true, items: [] });
  });
  vi.stubGlobal("fetch", requests);
  renderReleases("/releases?series=9&season=38&episode=2&purpose=41");
  expect(await screen.findByText("Replacing")).toBeVisible();
  expect(screen.queryByLabelText("Download purpose")).not.toBeInTheDocument();
  expect(
    screen.getByText("tvshows/Show/Season 38/Show - S38E02 - Two.mkv"),
  ).toBeVisible();
});

it("offers only the current season's files when choosing a replacement", async () => {
  const requests = stubFetch((url) => {
    if (url.includes("/organization/files"))
      return Response.json({
        items: [
          { id: 41, path: "tvshows/Show/Season 38/Show - S38E02 - Two.mkv" },
          { id: 42, path: "tvshows/Show/Season 34/Show - S34E13 - Old.mkv" },
        ],
      });
    if (url.includes("/library/series/9/seasons/38"))
      return Response.json({ season: {}, episodes: [], revision: 1 });
    if (url.includes("/library/series/9"))
      return Response.json({
        id: 9,
        kind: "series",
        tmdb_id: 456,
        title: "Show",
        year: 1989,
      });
    return Response.json({ configured: true, items: [] });
  });
  vi.stubGlobal("fetch", requests);
  renderReleases("/releases?series=9&season=38&episode=2");
  const select = await screen.findByLabelText("Download purpose");
  await waitFor(() =>
    expect(select).toHaveTextContent("Season 38/Show - S38E02 - Two.mkv"),
  );
  expect(select).not.toHaveTextContent("S34E13");
});
