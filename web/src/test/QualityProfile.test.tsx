import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { describe, expect, it, vi } from "vitest";
import type { CatalogProfile, CatalogProfileContext } from "../api";
import { QualityProfile } from "../pages/details/QualityProfile";

const context: CatalogProfileContext = {
  default_profile: { id: 1, name: "HD balanced", revision: 4 },
  profiles: [
    { id: 1, name: "HD balanced", revision: 4 },
    { id: 2, name: "Ultra HD", revision: 1 },
    { id: 3, name: "1080p/2160p any-seeders", revision: 1 },
  ],
};
const assignment: CatalogProfile = {
  profile_id: 3,
  effective_profile: { id: 3, name: "1080p/2160p any-seeders", revision: 1 },
  revision: 3,
};

function setup(
  response: () => Response,
  onSaved?: () => void,
): ReturnType<typeof vi.fn> {
  const fetch = vi.fn(async () => response());
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <QualityProfile
        id={771}
        assignment={assignment}
        context={context}
        onSaved={onSaved}
      />
    </Theme>,
  );
  return fetch;
}

describe("per-title quality profile editor", () => {
  it("shows the current assignment and writes an explicit profile with the reviewed revision", async () => {
    const fetch = setup(() => Response.json({ saved: true }));
    expect(
      screen.getByText("1080p/2160p any-seeders", { exact: false }),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Change quality profile" }),
    );
    await userEvent.selectOptions(
      screen.getByLabelText("Target quality profile"),
      "1",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review change" }),
    );
    expect(
      screen.getByText(/Assigned: 1080p\/2160p any-seeders → HD balanced/),
    ).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed change" }),
    );
    const sent = fetch.mock.calls[0];
    expect(sent?.[0]).toBe("/api/v1/automation/items/771");
    expect(sent?.[1]).toMatchObject({ method: "PUT" });
    expect(JSON.parse(String(sent?.[1]?.body))).toEqual({
      profile_id: 1,
      revision: 3,
    });
    expect(await screen.findByText("Assigned to HD balanced.")).toBeVisible();
  });

  it("clears back to the schedule default by sending a null profile", async () => {
    const fetch = setup(() => Response.json({ saved: true }));
    await userEvent.click(
      screen.getByRole("button", { name: "Change quality profile" }),
    );
    await userEvent.selectOptions(
      screen.getByLabelText("Target quality profile"),
      "default",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review change" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed change" }),
    );
    expect(JSON.parse(String(fetch.mock.calls[0]?.[1]?.body))).toEqual({
      profile_id: null,
      revision: 3,
    });
    expect(
      await screen.findByText(
        "Now follows the schedule default (HD balanced).",
      ),
    ).toBeVisible();
  });

  it("keeps a stale editor from overwriting a newer assignment", async () => {
    setup(() =>
      Response.json(
        {
          error: {
            message:
              "This item's automation preferences changed. Refresh before saving.",
          },
        },
        { status: 409 },
      ),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Change quality profile" }),
    );
    await userEvent.selectOptions(
      screen.getByLabelText("Target quality profile"),
      "2",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review change" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save reviewed change" }),
    );
    expect(
      await screen.findByText(/Reload the saved assignment/),
    ).toBeVisible();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Reload saved assignment" }),
      ).toBeVisible(),
    );
  });
});
