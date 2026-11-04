import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import {
  MovieVersions,
  type MovieVersionSettings,
} from "../pages/MovieVersions";
import { Releases } from "../pages/Releases";

const initial: MovieVersionSettings = {
  enabled: false,
  hd_profile_id: 1,
  uhd_profile_id: 2,
  revision: 4,
  hd_files: 1,
  uhd_files: 0,
  hd_download: "",
  uhd_download: "",
};
const profiles = [
  { id: 1, name: "HD balanced", cutoff: 1080 },
  { id: 2, name: "Ultra HD", cutoff: 2160 },
  { id: 3, name: "4K HDR only", cutoff: 2160 },
];
function mount(component: React.ReactNode, path = "/movies/1") {
  render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>{component}</MemoryRouter>
    </Theme>,
  );
}

describe("Movie versions", () => {
  it("saves opt-in with independent profiles and the reviewed revision", async () => {
    let saved = { ...initial };
    const fetch = vi.fn(async (url: string, options?: RequestInit) => {
      if (url.endsWith("/quality-profiles"))
        return Response.json({ items: profiles });
      if (options?.method === "PUT") {
        saved = { ...saved, ...JSON.parse(String(options.body)), revision: 5 };
      }
      return Response.json(saved);
    });
    vi.stubGlobal("fetch", fetch);
    mount(<MovieVersions id={1} />);
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("checkbox", { name: "Keep HD and 4K copies" }),
    );
    expect(screen.getByText("1 on disk")).toBeInTheDocument();
    expect(screen.getByText("Missing")).toBeInTheDocument();
    await user.selectOptions(screen.getByLabelText("4K quality profile"), "3");
    await user.click(
      screen.getByRole("button", { name: "Save movie versions" }),
    );
    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/library/movies/1/versions",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({
            enabled: true,
            hd_profile_id: 1,
            uhd_profile_id: 3,
            revision: 4,
          }),
        }),
      ),
    );
    expect(
      await screen.findByRole("link", { name: "Find 4K releases" }),
    ).toHaveAttribute("href", "/releases?movie=1&movie_version=uhd");
    expect(
      screen.getByRole("link", { name: "Download another version" }),
    ).toHaveAttribute("href", "/releases?movie=1&movie_version=additional");
  });
  it("keeps unsaved selections visible on a stale-settings response", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, options?: RequestInit) => {
        if (url.endsWith("/quality-profiles"))
          return Response.json({ items: profiles });
        if (options?.method === "PUT")
          return Response.json(
            {
              error: {
                message:
                  "Movie version settings changed. Reload before saving.",
              },
            },
            { status: 409 },
          );
        return Response.json(initial);
      }),
    );
    mount(<MovieVersions id={1} />);
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("checkbox", { name: "Keep HD and 4K copies" }),
    );
    await user.click(
      screen.getByRole("button", { name: "Save movie versions" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Reload before saving",
    );
    expect(screen.getByRole("checkbox")).toBeChecked();
  });
  it.each(["additional", "uhd"])(
    "queues the explicit %s purpose without selecting a replacement file",
    async (version) => {
      const fetch = vi.fn(async (url: string, options?: RequestInit) => {
        if (url.endsWith("/versions"))
          return Response.json({ ...initial, enabled: true });
        if (url.includes("/organization/files"))
          return Response.json({ items: [{ id: 9, path: "Movie.1080p.mkv" }] });
        if (url.endsWith("/library/movies/1"))
          return Response.json({ id: 1, title: "Movie", year: 2020 });
        if (url.includes("/releases?"))
          return Response.json({
            items: [
              {
                id: "a".repeat(32),
                title: "Movie.2020.2160p.HDR",
                size_bytes: 1024,
                seeders: 3,
                peers: 0,
                published_at: "",
              },
            ],
          });
        if (url.endsWith("/downloads") && options?.method === "POST")
          return Response.json({ item: { id: "job" } });
        return Response.json({ configured: true });
      });
      vi.stubGlobal("fetch", fetch);
      mount(<Releases />, `/releases?movie=1&movie_version=${version}`);
      const user = userEvent.setup();
      await user.click(
        await screen.findByRole("button", { name: "Search releases" }),
      );
      await user.click(await screen.findByRole("button", { name: "Download" }));
      await waitFor(() =>
        expect(fetch).toHaveBeenCalledWith(
          "/api/v1/downloads",
          expect.objectContaining({
            method: "POST",
            body: JSON.stringify({
              release_id: "a".repeat(32),
              movie_version: version,
            }),
          }),
        ),
      );
    },
  );
});
