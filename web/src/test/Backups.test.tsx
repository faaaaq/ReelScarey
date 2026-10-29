import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";
import App from "../App";

const archive = {
  id: "a".repeat(32),
  kind: "manual",
  created_at: 1792713600,
  size_bytes: 10240,
  sha256: "b".repeat(64),
  manifest: {
    created_at: "2026-10-23T00:00:00Z",
    application_version: "0.21.0",
    schema_version: 31,
  },
};
function setup(fail = false) {
  const state = {
    schedule: {
      enabled: false,
      interval_days: 7,
      keep_count: 10,
      keep_days: 30,
      revision: 1,
      next_run: 0,
      last_attempt: 0,
      last_success: 0,
      last_error: "",
      running: false,
    },
    archives: [archive],
    directory: "/backups",
    max_upload_bytes: 512 * 1024 * 1024,
    recovery_pending: false,
    restore_message: "",
    restoring: false,
  };
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    if (url.endsWith("/auth/session"))
      return Response.json({
        authenticated: true,
        setup_required: false,
        session: {
          username: "admin",
          role: "admin",
          csrf_token: "fixture-csrf",
          expires_at: 9999999999,
        },
      });
    if (url.endsWith("/schedule")) {
      Object.assign(state.schedule, JSON.parse(String(options?.body)), {
        revision: 2,
      });
      return Response.json(state);
    }
    if (url.endsWith("/upload")) {
      if (fail)
        return Response.json(
          { error: { message: "This archive failed verification." } },
          { status: 400 },
        );
      state.archives.push({ ...archive, id: "c".repeat(32), kind: "upload" });
      return Response.json(state.archives[1], { status: 201 });
    }
    if (url.endsWith("/preview"))
      return Response.json({
        archive,
        movies: 3,
        series: 2,
        registered_files: 8,
        administrator: "restored-admin",
      });
    if (url.endsWith("/restore"))
      return Response.json({ restarting: true }, { status: 202 });
    if (url.endsWith("/backups")) {
      if (options?.method === "POST") {
        state.schedule.running = true;
        return Response.json({ started: true }, { status: 202 });
      }
      return Response.json(state);
    }
    if (options?.method === "DELETE") {
      state.archives = [];
      return Response.json({ deleted: true });
    }
    throw new Error(`Unexpected ${url}`);
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <MemoryRouter initialEntries={["/backups"]}>
        <App />
      </MemoryRouter>
    </Theme>,
  );
  return fetch;
}
describe("backup management", () => {
  it("starts paused, explains scope and saves the retention schedule with CSRF", async () => {
    const fetch = setup();
    const checkbox = await screen.findByRole("checkbox", {
      name: "Enable scheduled backups",
    });
    expect(checkbox).not.toBeChecked();
    expect(screen.getByRole("link", { name: "Download" })).toHaveAttribute(
      "href",
      `/api/v1/system/backups/${archive.id}/download`,
    );
    expect(fetch.mock.calls.every(([, o]) => !o?.method)).toBe(true);
    await userEvent.click(checkbox);
    await userEvent.clear(
      screen.getByRole("spinbutton", { name: "Keep scheduled copies" }),
    );
    await userEvent.type(
      screen.getByRole("spinbutton", { name: "Keep scheduled copies" }),
      "4",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save schedule" }),
    );
    expect(await screen.findByText("Backup schedule saved.")).toBeVisible();
    const sent = fetch.mock.calls.find(([url]) => url.endsWith("/schedule"));
    expect(JSON.parse(String(sent?.[1]?.body))).toMatchObject({
      enabled: true,
      keep_count: 4,
      revision: 1,
    });
    expect(sent?.[1]?.headers).toMatchObject({
      "X-CSRF-Token": "fixture-csrf",
    });
  });
  it("starts a manual backup and disables conflicting operations while it runs", async () => {
    const fetch = setup();
    await screen.findByRole("checkbox");
    await userEvent.click(screen.getByRole("button", { name: "Back up now" }));
    expect(
      await screen.findByText("Creating and verifying a backup…"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Restore" })).toBeDisabled();
    expect(
      fetch.mock.calls.filter(
        ([url, o]) => url.endsWith("/backups") && o?.method === "POST",
      ),
    ).toHaveLength(1);
  });
  it("uploads without restoring and requires both review and typed confirmation", async () => {
    const fetch = setup();
    await screen.findByRole("checkbox");
    await userEvent.upload(
      screen.getByLabelText("Upload backup ZIP"),
      new File(["PK-fixture"], "backup.zip", { type: "application/zip" }),
    );
    expect(
      await screen.findByText(
        "Backup uploaded and verified. Review it before restoring.",
      ),
    ).toBeVisible();
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/restore"))).toBe(
      false,
    );
    await userEvent.click(
      screen.getAllByRole("button", { name: "Restore" })[0],
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Restore backup",
    });
    expect(within(dialog).getByText("restored-admin")).toBeVisible();
    const restore = within(dialog).getByRole("button", {
      name: "Restore and restart",
    });
    expect(restore).toBeDisabled();
    await userEvent.click(within(dialog).getByRole("checkbox"));
    expect(restore).toBeDisabled();
    await userEvent.type(
      within(dialog).getByRole("textbox", { name: "Type RESTORE to continue" }),
      "RESTORE",
    );
    await userEvent.click(restore);
    expect(await screen.findByText("Restoring your backup")).toBeVisible();
    const sent = fetch.mock.calls.find(([url]) => url.endsWith("/restore"));
    expect(JSON.parse(String(sent?.[1]?.body))).toEqual({
      sha256: archive.sha256,
      confirmation: "RESTORE",
    });
  });
  it("shows failed verification without offering a restore of the upload", async () => {
    const fetch = setup(true);
    await screen.findByRole("checkbox");
    await userEvent.upload(
      screen.getByLabelText("Upload backup ZIP"),
      new File(["bad"], "backup.zip", { type: "application/zip" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "failed verification",
    );
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/restore"))).toBe(
      false,
    );
    expect(screen.queryByText("Uploaded")).not.toBeInTheDocument();
  });
  it("deletes only after a separate confirmation", async () => {
    const fetch = setup();
    await screen.findByRole("checkbox");
    await userEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(fetch.mock.calls.some(([, o]) => o?.method === "DELETE")).toBe(
      false,
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Delete backup?",
    });
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Delete backup" }),
    );
    expect(await screen.findByText(/No backups yet/)).toBeVisible();
  });
});
