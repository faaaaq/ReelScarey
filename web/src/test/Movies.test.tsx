import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import App from "../App";
const session = {
  username: "testadmin",
  csrf_token: "test-csrf",
  expires_at: 9999999999,
};
const movie = {
  tmdb_id: 11,
  title: "Fixture Movie",
  overview: "A synopsis used only in tests.",
  year: 2020,
  poster_url: "",
  poster_path: "",
  release_date: "2020-01-01",
  runtime_minutes: 101,
};
function mount(path = "/") {
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
    const overridden = extra?.(url, options);
    if (overridden) return overridden;
    if (url.endsWith("/auth/session"))
      return Response.json({
        setup_required: false,
        authenticated: true,
        session,
      });
    if (url.endsWith("/providers/tmdb"))
      return Response.json({ configured: true });
    if (url.includes("/metadata/movies?"))
      return Response.json({
        items: [{ ...movie, in_library: false }],
        page: 1,
        total_pages: 1,
        total_results: 1,
      });
    if (url.endsWith("/metadata/movies/11")) return Response.json(movie);
    if (url.endsWith("/setup"))
      return Response.json({ steps: [], recovery_pending: false });
    if (url.endsWith("/library/movies"))
      return Response.json({ created: true, item: movie }, { status: 201 });
    if (url.endsWith("/settings"))
      return Response.json({
        library_name: "My library",
        movie_folder: "Movies",
        series_folder: "TV",
        revision: 1,
      });
    if (url.endsWith("/system"))
      return Response.json({ paths: {}, version: "test", schema_version: 2 });
    return Response.json({
      library_name: "My library",
      counts: { movies: 0, series: 0 },
      settings_revision: 1,
    });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("authentication and movies", () => {
  it("keeps the library behind setup and creates the administrator with a one-time code", async () => {
    const fetch = setup((url) =>
      url.endsWith("/auth/session")
        ? Response.json({ authenticated: false, setup_required: true })
        : url.endsWith("/auth/setup")
          ? Response.json(session, { status: 201 })
          : undefined,
    );
    mount();
    expect(
      await screen.findByRole("heading", {
        name: "Your library starts with you.",
      }),
    ).toBeVisible();
    expect(
      screen.queryByRole("link", { name: "Movies" }),
    ).not.toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Setup code"), "x".repeat(43));
    await userEvent.type(screen.getByLabelText("Username"), "testadmin");
    await userEvent.type(
      screen.getByLabelText("Password"),
      "test-only-password",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Create administrator" }),
    );
    expect(
      await screen.findByRole("heading", { name: "Set up your library" }),
    ).toBeVisible();
    const sent = fetch.mock.calls.find(([url]) => url.endsWith("/auth/setup"));
    expect(JSON.parse(String(sent?.[1]?.body))).toEqual({
      username: "testadmin",
      password: "test-only-password",
      setup_token: "x".repeat(43),
    });
  });
  it("explains a truncated setup code before sending an authentication attempt", async () => {
    const fetch = setup((url) =>
      url.endsWith("/auth/session")
        ? Response.json({ authenticated: false, setup_required: true })
        : undefined,
    );
    mount();
    await screen.findByLabelText("Setup code");
    await userEvent.type(screen.getByLabelText("Setup code"), "x".repeat(40));
    await userEvent.type(screen.getByLabelText("Username"), "testadmin");
    await userEvent.type(
      screen.getByLabelText("Password"),
      "test-only-password",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Create administrator" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "all 43 characters",
    );
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/auth/setup"))).toBe(
      false,
    );
    expect(screen.getByLabelText("Password")).toHaveValue("test-only-password");
  });
  it("shows failed login without fetching private data", async () => {
    const fetch = setup((url) =>
      url.endsWith("/auth/session")
        ? Response.json({ authenticated: false, setup_required: false })
        : url.endsWith("/auth/login")
          ? Response.json(
              { error: { message: "The username or password is incorrect." } },
              { status: 401 },
            )
          : undefined,
    );
    mount();
    await screen.findByRole("heading", { name: "Welcome back." });
    await userEvent.type(screen.getByLabelText("Username"), "testadmin");
    await userEvent.type(screen.getByLabelText("Password"), "wrong-password");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "username or password",
    );
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/dashboard"))).toBe(
      false,
    );
  });
  it("searches, shows canonical details, and adds with a chosen monitoring preference", async () => {
    const fetch = setup();
    mount("/search");
    await screen.findByLabelText("Movie title");
    await userEvent.type(screen.getByLabelText("Movie title"), "Fixture");
    await userEvent.click(screen.getByRole("button", { name: /^Search$/ }));
    expect(
      await screen.findByRole("heading", { name: "Fixture Movie" }),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "View details" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      await within(dialog).findByText("A synopsis used only in tests."),
    ).toBeVisible();
    await userEvent.click(within(dialog).getByRole("switch"));
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Add movie" }),
    );
    expect(await within(dialog).findByText("In library")).toBeVisible();
    const sent = fetch.mock.calls.find(([url]) =>
      url.endsWith("/library/movies"),
    );
    expect(JSON.parse(String(sent?.[1]?.body))).toEqual({
      tmdb_id: 11,
      monitored: false,
    });
    expect(sent?.[1]?.headers).toMatchObject({ "X-CSRF-Token": "test-csrf" });
  });
  it("preserves search results and displays add failures without claiming success", async () => {
    setup((url) =>
      url.endsWith("/library/movies")
        ? Response.json(
            { error: { message: "TMDb is temporarily unavailable." } },
            { status: 502 },
          )
        : undefined,
    );
    mount("/search?query=Fixture&page=1");
    await userEvent.click(
      await screen.findByRole("button", { name: "Add movie" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "temporarily unavailable",
    );
    expect(screen.queryByText("In library")).not.toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Fixture Movie" }),
    ).toBeVisible();
  });
  it("saves a write-only token and clears the input after the connection is verified", async () => {
    let configured = false;
    const fetch = setup((url, options) => {
      if (url.endsWith("/providers/tmdb")) {
        if (options?.method === "PUT") {
          configured = true;
          return Response.json({ configured: true, tested: true });
        }
        return Response.json({ configured });
      }
      return undefined;
    });
    mount("/settings");
    const field = await screen.findByLabelText("API Read Access Token");
    await userEvent.type(field, "test-only-provider-token-value");
    await userEvent.click(
      within(screen.getByRole("region", { name: "TMDb settings" })).getByRole(
        "button",
        { name: "Save & test connection" },
      ),
    );
    expect(
      await screen.findByText("TMDb connection verified and saved."),
    ).toBeVisible();
    expect(field).toHaveValue("");
    const sent = fetch.mock.calls.find(
      ([url, options]) =>
        url.endsWith("/providers/tmdb") && options?.method === "PUT",
    );
    expect(JSON.parse(String(sent?.[1]?.body))).toEqual({
      token: "test-only-provider-token-value",
    });
  });
  it("returns to sign-in when a session expires", async () => {
    setup((url) =>
      url.endsWith("/settings")
        ? Response.json(
            { error: { message: "Your session has expired." } },
            { status: 401 },
          )
        : undefined,
    );
    mount("/settings");
    expect(
      await screen.findByRole("heading", { name: "Welcome back." }),
    ).toBeVisible();
    expect(screen.queryByLabelText("Library name")).not.toBeInTheDocument();
  });
  it("signs out with CSRF protection and clears the library from view", async () => {
    const fetch = setup((url) =>
      url.endsWith("/auth/logout")
        ? Response.json({ signed_out: true })
        : undefined,
    );
    mount();
    await screen.findByRole("heading", { name: "My library" });
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Welcome back." }),
      ).toBeVisible(),
    );
    const sent = fetch.mock.calls.find(([url]) => url.endsWith("/auth/logout"));
    expect(sent?.[1]?.headers).toMatchObject({ "X-CSRF-Token": "test-csrf" });
  });
});
