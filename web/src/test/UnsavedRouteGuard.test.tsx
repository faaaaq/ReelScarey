import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import App from "../App";

function setup() {
  const fetch = vi.fn(async (url: string) => {
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
    if (url.includes("/downloads?page="))
      return Response.json({ items: [], page: 1, total: 0, limit: 24 });
    return Response.json(
      { error: { message: "Unexpected test request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

function mount(path: string) {
  render(
    <Theme>
      <RouterProvider
        router={createMemoryRouter([{ path: "*", element: <App /> }], {
          initialEntries: [path],
        })}
      />
    </Theme>,
  );
}

describe("unsaved-changes in-app navigation guard", () => {
  it("prompts on in-app navigation, keeps the draft on cancel, and navigates on confirm", async () => {
    setup();
    mount("/settings/general");
    const name = await screen.findByLabelText("Library name");
    await userEvent.type(name, "X");
    expect(name).toHaveValue("My libraryX");

    await userEvent.click(screen.getByRole("link", { name: "Activity" }));
    await screen.findByText("Leave with unsaved changes?");

    // Cancel: the user stays on the form with the draft intact.
    await userEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    await waitFor(() =>
      expect(
        screen.queryByText("Leave with unsaved changes?"),
      ).not.toBeInTheDocument(),
    );
    const kept = screen.getByLabelText("Library name") as HTMLInputElement;
    expect(kept.value).toBe("My libraryX");

    // Confirm: navigation proceeds.
    await userEvent.click(screen.getByRole("link", { name: "Activity" }));
    await screen.findByText("Leave with unsaved changes?");
    await userEvent.click(
      screen.getByRole("button", { name: "Leave without saving" }),
    );
    await screen.findByText(
      "Active downloads only. Completed items appear in History.",
    );
    expect(screen.queryByLabelText("Library name")).not.toBeInTheDocument();
  });

  it("navigates without prompting when there are no unsaved changes", async () => {
    setup();
    mount("/settings/general");
    await screen.findByLabelText("Library name");
    await userEvent.click(screen.getByRole("link", { name: "Activity" }));
    await screen.findByText(
      "Active downloads only. Completed items appear in History.",
    );
    expect(
      screen.queryByText("Leave with unsaved changes?"),
    ).not.toBeInTheDocument();
  });
});
