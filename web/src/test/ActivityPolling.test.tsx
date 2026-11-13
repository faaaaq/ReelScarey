import { render, screen, waitFor, act } from "@testing-library/react";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { useLiveResource } from "../useLiveResource";
import { useResource } from "../useResource";

const job = {
  id: "b".repeat(32),
  movie_id: 1,
  movie_title: "Test Movie",
  title: "Test.Movie.2026.1080p",
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
    if (url.includes("/downloads?page="))
      return Response.json({
        items: [job],
        page: 1,
        total: 1,
        limit: 24,
      });
    if (url.endsWith("/settings"))
      return Response.json({ library_name: "My library", revision: 1 });
    if (url.endsWith("/system"))
      return Response.json({
        paths: { config: "/config", media: "/media", downloads: "/downloads" },
        version: "test",
        schema_version: 3,
      });
    return Response.json(
      { error: { message: "Unexpected test request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

function visibility(state: "visible" | "hidden") {
  Object.defineProperty(document, "visibilityState", {
    value: state,
    configurable: true,
  });
}

afterEach(() => {
  visibility("visible");
  vi.useRealTimers();
});

describe("activity polling", () => {
  it("pauses the 5s poll while the page is hidden and refreshes when visible", async () => {
    const fetch = setup();
    mount("/activity");
    await screen.findByText("Test Movie");
    const initialCalls = fetch.mock.calls.filter((c) =>
      String(c[0]).includes("/downloads?page="),
    ).length;
    expect(initialCalls).toBeGreaterThan(0);

    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    visibility("hidden");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(11000);
    });
    const hiddenCalls = fetch.mock.calls.filter((c) =>
      String(c[0]).includes("/downloads?page="),
    ).length;
    // Before the visibility gate this polled three more times while hidden.
    expect(hiddenCalls).toBe(initialCalls);

    visibility("visible");
    document.dispatchEvent(new Event("visibilitychange"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    const visibleCalls = fetch.mock.calls.filter((c) =>
      String(c[0]).includes("/downloads?page="),
    ).length;
    expect(visibleCalls).toBeGreaterThan(hiddenCalls);
  });
});

function Probe({ path, interval = 0 }: { path: string; interval?: number }) {
  if (interval) useLiveResource(path, interval);
  else useResource(path);
  return null;
}

describe("read timeouts", () => {
  it("uses the shared 30s api() default, not a 15s cap", async () => {
    setup();
    const timeout = vi.spyOn(AbortSignal, "timeout");
    render(
      <Theme>
        <MemoryRouter>
          <>
            <Probe path="/system" />
            <Probe path="/settings" interval={5000} />
          </>
        </MemoryRouter>
      </Theme>,
    );
    await waitFor(() => expect(timeout).toHaveBeenCalledWith(30000));
    expect(timeout.mock.calls.map((c) => c[0])).toEqual([30000, 30000]);
  });
});
