import { render, screen } from "@testing-library/react";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { it, expect, vi } from "vitest";
import { Announces } from "../pages/Announces";

it("lists announce sources and captured releases", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (String(url).endsWith("/announce-sources")) {
        return Response.json({
          items: [
            {
              id: "a".repeat(32),
              tracker: "iptorrents",
              name: "Acceptance IPT",
              enabled: true,
              revision: 1,
              last_event_at: 0,
              server: "irc.iptorrents.com",
              port: 6697,
              tls: true,
              channel: "#ipt.announce",
              nick: "acceptbot",
              account: "",
              has_password: true,
              require_account: false,
            },
          ],
          definitions: [
            {
              id: "iptorrents",
              name: "IPTorrents",
              server: "irc.iptorrents.com",
              port: 6697,
              tls: true,
              channel: "#ipt.announce",
              require_account: false,
            },
          ],
        });
      }
      if (String(url).includes("/announces")) {
        return Response.json({
          items: [
            {
              id: 1,
              source_id: "a".repeat(32),
              tracker: "iptorrents",
              release_name: "The Movie 2010 DVDRip XviD-GROUP",
              category: "Movies/XviD",
              size_bytes: 8192650117,
              freeleech: "FREELEECH",
              info_url: "https://tracker/details.php?id=1",
              received_at: 1,
            },
          ],
          page: 1,
          total: 1,
          limit: 50,
        });
      }
      return Response.json({});
    }),
  );
  render(
    <Theme>
      <MemoryRouter>
        <Announces />
      </MemoryRouter>
    </Theme>,
  );
  expect(await screen.findByText("Acceptance IPT")).toBeInTheDocument();
  expect(
    await screen.findByText("The Movie 2010 DVDRip XviD-GROUP"),
  ).toBeInTheDocument();
  expect(screen.getByText("Open on tracker")).toBeInTheDocument();
  expect(screen.getByText("Enabled")).toBeInTheDocument();
});
