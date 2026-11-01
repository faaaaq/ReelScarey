import type { ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { expect, it, vi } from "vitest";
import App from "../App";
vi.mock("../auth", () => ({
  AuthGate: ({ children }: { children: ReactNode }) => children,
  useAuth: () => ({
    session: { role: "admin", username: "owner" },
    signOut: vi.fn(),
  }),
}));
vi.mock("../components/HistoryTray", () => ({ HistoryTray: () => null }));
vi.mock("../pages/Storage", () => ({ Storage: () => <h1>Storage</h1> }));
vi.mock("../pages/MetadataRefresh", () => ({
  MetadataRefresh: () => <h1>Metadata refresh</h1>,
}));
vi.mock("../pages/DownloadClients", () => ({
  DownloadClients: () => <h1>Download clients</h1>,
}));
it.each([
  ["/storage", "Storage"],
  ["/metadata-refresh", "Metadata refresh"],
  ["/download-clients", "Download clients"],
])(
  "names the existing %s page for browser and assistive technology",
  async (path, title) => {
    render(
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>,
    );
    await screen.findByRole("heading", { name: title });
    await waitFor(() => expect(document.title).toBe(`${title} · ReelScarey`));
  },
);
it("returns keyboard focus when navigation closes and focuses content after a route change", async () => {
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={["/storage"]}>
      <App />
    </MemoryRouter>,
  );
  await screen.findByRole("heading", { name: "Storage" });
  const toggle = screen.getByRole("button", { name: "Open navigation" });
  await user.click(toggle);
  const link = screen.getByRole("link", { name: "Metadata refresh" });
  link.focus();
  await user.keyboard("{Escape}");
  expect(toggle).toHaveFocus();
  expect(toggle).toHaveAttribute("aria-expanded", "false");
  await user.click(toggle);
  await user.click(link);
  await screen.findByRole("heading", { name: "Metadata refresh" });
  await waitFor(() => expect(screen.getByRole("main")).toHaveFocus());
  expect(toggle).toHaveAttribute("aria-expanded", "false");
});
