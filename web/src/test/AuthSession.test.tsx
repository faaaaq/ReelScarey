import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { AuthGate, useAuth } from "../auth";
import { api, messageFor, type Session } from "../api";

const adminSession: Session = {
  username: "testadmin",
  csrf_token: "test-csrf",
  expires_at: 9999999999,
  role: "admin",
};

// A page with in-progress work: local input state plus a save action that
// talks to the API. This mirrors any form or review draft in the app.
function EditingChild() {
  const { signOut } = useAuth();
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  return (
    <div>
      <input
        aria-label="draft"
        value={value}
        onChange={(e) => setValue(e.target.value)}
      />
      <button
        type="button"
        onClick={() =>
          api("/library", { method: "POST", body: "{}" }).catch((e) =>
            setError(messageFor(e)),
          )
        }
      >
        save draft
      </button>
      <button type="button" onClick={() => void signOut()}>
        sign out
      </button>
      {error && <p role="alert">{error}</p>}
    </div>
  );
}

function mount() {
  return render(
    <Theme>
      <MemoryRouter>
        <AuthGate>
          <EditingChild />
        </AuthGate>
      </MemoryRouter>
    </Theme>,
  );
}

function stubFetch(mock: ReturnType<typeof vi.fn>) {
  vi.stubGlobal("fetch", mock);
  return mock;
}

function json(body: unknown, status = 200) {
  return Response.json(body, { status });
}

describe("AuthGate session expiry", () => {
  it("keeps in-progress work mounted and re-authenticates through an overlay when a 401 arrives mid-session", async () => {
    const user = userEvent.setup();
    let authenticated = true;
    const fetchMock = vi.fn(async (url: string) => {
      if (url.endsWith("/auth/session"))
        return json({
          setup_required: false,
          authenticated: authenticated,
          session: authenticated ? adminSession : undefined,
        });
      if (url.endsWith("/auth/login")) {
        authenticated = true;
        return json(adminSession);
      }
      if (url.endsWith("/auth/logout")) {
        authenticated = false;
        return new Response(null, { status: 204 });
      }
      // The save endpoint fails with 401: the server-side session expired
      // even though the page still believes it is signed in.
      return json(
        { error: { message: "Session expired.", code: "unauthorized" } },
        401,
      );
    });
    stubFetch(fetchMock);

    mount();
    const draft = await screen.findByLabelText("draft");
    await user.type(draft, "half-written review");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "save draft" }));

    // The 401 must surface the sign-in form as an overlay...
    const dialog = await screen.findByRole("dialog", {
      name: "Session expired",
    });
    expect(
      within(dialog).getByRole("heading", { name: "Sign in" }),
    ).toBeInTheDocument();
    // ...and the API error must still be reported honestly to the page.
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Session expired.",
    );
    // The app must stay mounted: the unsaved draft survives.
    expect(screen.getByLabelText("draft")).toHaveValue("half-written review");

    await user.type(within(dialog).getByLabelText("Username"), "testadmin");
    await user.type(within(dialog).getByLabelText("Password"), "password");
    await user.click(within(dialog).getByRole("button", { name: "Sign in" }));

    // After re-authentication the overlay is gone and the draft is intact.
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(screen.getByLabelText("draft")).toHaveValue("half-written review");
  });

  it("shows the full sign-in page without mounting the app when no session was ever established", async () => {
    stubFetch(
      vi.fn(async (url: string) => {
        if (url.endsWith("/auth/session"))
          return json({ setup_required: false, authenticated: false });
        return json({});
      }),
    );
    mount();
    expect(
      await screen.findByRole("heading", { name: "Sign in" }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("draft")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("replaces the app with the sign-in page after an explicit sign-out", async () => {
    const user = userEvent.setup();
    let authenticated = true;
    stubFetch(
      vi.fn(async (url: string) => {
        if (url.endsWith("/auth/session"))
          return json({
            setup_required: false,
            authenticated: authenticated,
            session: authenticated ? adminSession : undefined,
          });
        if (url.endsWith("/auth/logout")) {
          authenticated = false;
          return new Response(null, { status: 204 });
        }
        return json({});
      }),
    );
    mount();
    await screen.findByLabelText("draft");
    await user.click(screen.getByRole("button", { name: "sign out" }));
    expect(
      await screen.findByRole("heading", { name: "Sign in" }),
    ).toBeInTheDocument();
    // An explicit sign-out is a deliberate choice: the app is replaced and
    // the overlay/dialog form is not used.
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("draft")).not.toBeInTheDocument();
  });
});
