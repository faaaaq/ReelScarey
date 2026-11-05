import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { Organization } from "../pages/Organization";

function fixtures() {
  const libraries = [
    {
      key: "movies",
      name: "Movies",
      folder: "Movies",
      content: "movie",
      preset: "legacy",
      movie_template: "",
      series_template: "",
      episode_template: "",
    },
    {
      key: "tv",
      name: "TV Shows",
      folder: "TV Shows",
      content: "series",
      preset: "legacy",
      movie_template: "",
      series_template: "",
      episode_template: "",
    },
    {
      key: "anime",
      name: "Anime",
      folder: "Anime",
      content: "mixed",
      preset: "plex",
      movie_template: "",
      series_template: "",
      episode_template: "",
    },
  ];
  const files = [
    {
      id: 41,
      media_id: 1,
      title: "First feature",
      kind: "movie",
      category: "standard",
      path: "Old/First feature.mkv",
      size_bytes: 1000000,
    },
    {
      id: 42,
      media_id: 2,
      title: "Second feature",
      kind: "movie",
      category: "standard",
      path: "Old/Second feature.mp4",
      size_bytes: 2000000,
    },
  ];
  const config = { revision: 9, main_root: "/media", libraries };
  const plan = (ids: number[], id = "plan-1") => ({
    id,
    state: "review",
    phase: "",
    message: "Review these exact file changes.",
    created_at: 1,
    updated_at: 1,
    files: files
      .filter((file) => ids.includes(file.id))
      .map((file) => ({
        media_file_id: file.id,
        title: file.title,
        source: file.path,
        destination: `Movies/${file.title}/${file.title}.mkv`,
        size_bytes: file.size_bytes,
        unchanged: false,
        companions: [
          {
            source: `Old/${file.title}.eng.forced.srt`,
            destination: `Movies/${file.title}/${file.title}.eng.forced.srt`,
            size_bytes: 500,
          },
        ],
      })),
  });
  return { config, files, plan };
}
function setup(
  extra?: (
    url: string,
    options: RequestInit | undefined,
    fixture: ReturnType<typeof fixtures>,
  ) => Response | Promise<Response> | undefined,
) {
  const fixture = fixtures();
  let previews = 0;
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const result = await extra?.(url, options, fixture);
    if (result) return result;
    if (url.endsWith("/organization") && options?.method === "PUT")
      return Response.json({
        ...fixture.config,
        ...JSON.parse(String(options.body)),
        revision: 10,
      });
    if (url.endsWith("/organization")) return Response.json(fixture.config);
    if (url.includes("/organization/files?"))
      return Response.json({
        items: fixture.files,
        page: 1,
        total: 2,
        limit: 50,
      });
    if (url.endsWith("/organization/plans") && options?.method === "POST")
      return Response.json(
        fixture.plan(
          JSON.parse(String(options.body)).file_ids,
          `plan-${++previews}`,
        ),
      );
    if (url.endsWith("/apply"))
      return Response.json({ ...fixture.plan([41]), state: "queued" });
    if (url.endsWith("/organization/plans"))
      return Response.json({ items: [] });
    return Response.json(
      { error: { message: "Unexpected test request" } },
      { status: 404 },
    );
  });
  vi.stubGlobal("fetch", fetch);
  return { fetch, fixture };
}
function mount(path = "/organization") {
  render(
    <Theme>
      <MemoryRouter initialEntries={[path]}>
        <Organization />
      </MemoryRouter>
    </Theme>,
  );
}
async function selectFirstAndReview() {
  await screen.findByRole("checkbox", {
    name: "Select file 41: First feature",
  });
  await userEvent.click(
    screen.getByRole("checkbox", { name: "Select file 41: First feature" }),
  );
  await userEvent.click(
    screen.getByRole("button", { name: "Review 1 selected file" }),
  );
  return screen.findByRole("region", { name: "Organization review" });
}

describe("Reviewed library organization", () => {
  it("includes a reviewed title update with a single file move", async () => {
    const { fetch } = setup((url, options, fixture) => {
      if (url.endsWith("/organization/plans") && options?.method === "POST")
        return Response.json({
          ...fixture.plan([41]),
          update_embedded_title: true,
          embedded_title: "First feature",
        });
    });
    mount();
    await userEvent.click(
      await screen.findByRole("checkbox", {
        name: "Select file 41: First feature",
      }),
    );
    await userEvent.click(
      screen.getByRole("checkbox", {
        name: "Also update embedded titles after organization",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review 1 selected file" }),
    );
    const review = await screen.findByRole("region", {
      name: "Organization review",
    });
    expect(
      within(review).getByText(/After the file move, update embedded titles/),
    ).toBeVisible();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/organization/plans",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          file_ids: [41],
          target_library: "",
          update_embedded_title: true,
        }),
      }),
    );
  });
  it("reviews the catalog-derived embedded title before queueing", async () => {
    const { fetch } = setup((url, options, fixture) => {
      if (
        url.endsWith("/organization/embedded-title") &&
        options?.method === "POST"
      )
        return Response.json({
          ...fixture.plan([]),
          phase: "planned",
          replacement: {
            title: "First feature",
            tag_title: "First feature",
            metadata_only: true,
            previous_title: "Wrong title",
            media_file_id: 41,
            original: { source: "Old/First feature.mkv", size_bytes: 1000000 },
            incoming: { source: "Old/First feature.mkv", size_bytes: 1000000 },
            backup: "",
            episodes: [],
          },
        });
    });
    mount();
    await userEvent.click(
      await screen.findByRole("checkbox", {
        name: "Select file 41: First feature",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review embedded title update" }),
    );
    const review = await screen.findByRole("region", {
      name: "Organization review",
    });
    expect(within(review).getByText("New embedded title:")).toBeVisible();
    expect(within(review).getByText("Previous embedded title:")).toBeVisible();
    expect(within(review).getByText("Wrong title")).toBeVisible();
    expect(
      within(review).getByText(/no duplicate video copy is kept/i),
    ).toBeVisible();
    expect(
      within(review).getByRole("button", { name: "Apply reviewed changes" }),
    ).toBeDisabled();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/organization/embedded-title",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ media_file_id: 41 }),
      }),
    );
  });
  it("can cancel a queued job before file work starts", async () => {
    const { fetch } = setup((url, options, fixture) => {
      if (url.endsWith("/apply") && options?.method === "POST")
        return Response.json({
          ...fixture.plan([41]),
          state: "queued",
          phase: "planned",
        });
      if (url.endsWith("/cancel") && options?.method === "POST")
        return Response.json({
          ...fixture.plan([41]),
          phase: "planned",
          message: "Queue cancelled before file changes.",
        });
    });
    mount();
    const review = await selectFirstAndReview();
    await userEvent.click(
      within(review).getByRole("checkbox", {
        name: "I reviewed all paths and subtitle changes",
      }),
    );
    await userEvent.click(
      within(review).getByRole("button", { name: "Apply reviewed changes" }),
    );
    await userEvent.click(
      await within(review).findByRole("button", { name: "Cancel queued job" }),
    );
    expect(
      await within(review).findByText("Queue cancelled before file changes."),
    ).toBeVisible();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/organization/plans/plan-1/cancel",
      expect.objectContaining({ method: "POST" }),
    );
  });
  it("stops a batch without starting the remaining file checks", async () => {
    const { fetch } = setup((url, options) => {
      if (url.endsWith("/files/41/health"))
        return new Promise<Response>((_, reject) => {
          options?.signal?.addEventListener(
            "abort",
            () => reject(new DOMException("Stopped", "AbortError")),
            { once: true },
          );
        });
    });
    mount();
    await userEvent.click(
      await screen.findByRole("checkbox", {
        name: "Select all files on this page",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Check selected files (2)" }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "Stop checking" }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Check selected files (2)" }),
      ).toBeEnabled(),
    );
    expect(
      fetch.mock.calls.some(([url]) => url.endsWith("/files/42/health")),
    ).toBe(false);
  });
  it("checks the selected files sequentially and reports their separate results", async () => {
    const { fetch } = setup((url, options) => {
      if (options?.method === "POST" && url.endsWith("/files/41/health"))
        return Response.json({ status: "verified" });
      if (options?.method === "POST" && url.endsWith("/files/42/health"))
        return Response.json({ status: "missing" });
    });
    mount();
    await userEvent.click(
      await screen.findByRole("checkbox", {
        name: "Select all files on this page",
      }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Check selected files (2)" }),
    );
    expect(
      await screen.findByText("File matches its saved checksum."),
    ).toBeVisible();
    expect(
      await screen.findByText("File is missing from its registered path."),
    ).toBeVisible();
    const checks = fetch.mock.calls.filter(([url]) => url.endsWith("/health"));
    expect(checks.map(([url]) => url)).toEqual([
      "/api/v1/organization/files/41/health",
      "/api/v1/organization/files/42/health",
    ]);
  });
  it("reviews catalog removal without applying it or silently deleting files", async () => {
    const { fetch } = setup((url, _options, fixture) => {
      if (url.endsWith("/organization/removal"))
        return Response.json({
          ...fixture.plan([], "removal"),
          removal: {
            media_id: 1,
            title: "First feature",
            mode: "keep",
            paths: ["Old/First feature.mkv"],
            affected_records: [{ table: "media_items", ids: [1] }],
          },
        });
    });
    mount("/organization?media_id=1");
    await userEvent.click(
      await screen.findByRole("button", { name: "Review catalog removal" }),
    );
    const review = await screen.findByRole("region", {
      name: "Organization review",
    });
    expect(
      within(review).getByText(/Keep all library files in place/),
    ).toBeVisible();
    expect(
      within(review).getByRole("button", { name: "Apply reviewed changes" }),
    ).toBeDisabled();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/organization/removal",
      expect.objectContaining({
        body: JSON.stringify({ media_id: 1, mode: "keep" }),
      }),
    );
    expect(
      fetch.mock.calls.some(([url]) => url.endsWith("/removal/apply")),
    ).toBe(false);
  });
  it("loads an acquired replacement only after an explicit review action", async () => {
    const { fetch } = setup((url, _options, fixture) => {
      if (url.endsWith("/organization/acquisition"))
        return Response.json(fixture.plan([41], "acquired"));
    });
    mount("/organization?acquisition_kind=download&acquisition_job=job-1");
    await userEvent.click(
      await screen.findByRole("button", {
        name: "Review acquired replacement",
      }),
    );
    const review = await screen.findByRole("region", {
      name: "Organization review",
    });
    expect(
      within(review).getByRole("button", { name: "Apply reviewed changes" }),
    ).toBeDisabled();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/organization/acquisition",
      expect.objectContaining({
        body: JSON.stringify({ kind: "download", job_id: "job-1" }),
      }),
    );
  });
  it("requires review before undoing a completed file move", async () => {
    const { fetch } = setup((url, options, fixture) => {
      if (url.endsWith("/organization/plans") && !options?.method)
        return Response.json({
          items: [
            { ...fixture.plan([41], "completed-move"), state: "completed" },
          ],
        });
      if (url.endsWith("/completed-move/rollback"))
        return Response.json({
          ...fixture.plan([41], "inverse"),
          restores_plan: "completed-move",
        });
    });
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "Review undo move" }),
    );
    const review = await screen.findByRole("region", {
      name: "Organization review",
    });
    expect(
      within(review).getByText(/Current collection and naming preferences/),
    ).toBeVisible();
    expect(
      within(review).getByRole("button", { name: "Apply reviewed changes" }),
    ).toBeDisabled();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/organization/plans/completed-move/rollback",
      expect.objectContaining({ method: "POST" }),
    );
    expect(
      fetch.mock.calls.some(([url]) => url.endsWith("/inverse/apply")),
    ).toBe(false);
  });
  it("finds saved versions for a selected file and reviews a restore", async () => {
    const savedReplacement = {
      title: "First feature",
      media_file_id: 41,
      original: { source: "Old/First feature.mkv", size_bytes: 1000000 },
      incoming: { source: "Incoming/new.mkv", size_bytes: 2000000 },
      backup: ".reelscarey-versions/example",
      episodes: [],
    };
    const { fetch } = setup((url, options, fixture) => {
      if (url.includes("/organization/replacements?media_file_id=41"))
        return Response.json({
          items: [
            {
              ...fixture.plan([], "old-version"),
              state: "completed",
              replacement: savedReplacement,
            },
          ],
        });
      if (url.endsWith("/old-version/rollback") && options?.method === "POST")
        return Response.json({
          ...fixture.plan([], "restore"),
          replacement: savedReplacement,
        });
    });
    mount();
    const buttons = await screen.findAllByRole("button", {
      name: "Show saved versions",
    });
    await userEvent.click(buttons[0]);
    await userEvent.click(
      await screen.findByRole("button", {
        name: "Review restore previous version",
      }),
    );
    const review = await screen.findByRole("region", {
      name: "Organization review",
    });
    expect(within(review).getByText(/Incoming\/new.mkv/)).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/organization/plans/old-version/rollback",
      expect.objectContaining({ method: "POST" }),
    );
  });
  it("requires a selected registered file and an explicit review for replacement", async () => {
    const { fetch } = setup((url, options, fixture) => {
      if (
        url.endsWith("/organization/replacements") &&
        options?.method === "POST"
      ) {
        return Response.json({
          ...fixture.plan([]),
          replacement: {
            title: "First feature",
            media_file_id: 41,
            original: { source: "Old/First feature.mkv", size_bytes: 1000000 },
            incoming: { source: "Incoming/new.mkv", size_bytes: 2000000 },
            backup: ".reelscarey-versions/example",
          },
        });
      }
    });
    mount("/organization?replacement_source=candidate-1");
    const review = await screen.findByRole("button", {
      name: "Review replacement",
    });
    expect(review).toBeDisabled();
    await userEvent.click(
      await screen.findByRole("checkbox", {
        name: "Select file 41: First feature",
      }),
    );
    await userEvent.click(review);
    const panel = await screen.findByRole("region", {
      name: "Organization review",
    });
    expect(within(panel).getByText(/Incoming\/new.mkv/)).toBeInTheDocument();
    expect(within(panel).getByText(/timing may differ/)).toBeInTheDocument();
    expect(
      within(panel).getByRole("button", { name: "Apply reviewed changes" }),
    ).toBeDisabled();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/organization/replacements",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          media_file_id: 41,
          scan_file_id: "candidate-1",
        }),
      }),
    );
  });
  it("shows all folder contents before a whole-folder move", async () => {
    const { fetch } = setup((url, options, fixture) => {
      if (url.endsWith("/organization/plans") && options?.method === "POST")
        return Response.json({
          ...fixture.plan([41]),
          folder_move: {
            source: "Movies/First",
            destination: "Anime/Movies/First",
            inventory: {
              directories: [
                { path: "Movies/First" },
                { path: "Movies/First/Extras" },
              ],
              files: [{ source: "Movies/First/poster.jpg", size_bytes: 123 }],
            },
          },
        });
    });
    mount("/organization?media_id=1");
    const button = await screen.findByRole("button", {
      name: "Review entire folder and extras",
    });
    await userEvent.selectOptions(
      screen.getByLabelText("Destination collection"),
      "anime",
    );
    await userEvent.click(button);
    const review = await screen.findByRole("region", {
      name: "Organization review",
    });
    expect(
      within(review).getByText("Movies/First/poster.jpg"),
    ).toBeInTheDocument();
    expect(within(review).getByText("Movies/First/Extras")).toBeInTheDocument();
    const call = fetch.mock.calls.find(
      ([url, options]) =>
        url.endsWith("/organization/plans") && options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      media_id: 1,
      target_library: "anime",
      whole_folder: true,
    });
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/apply"))).toBe(
      false,
    );
  });
  it("reviews every registered file for a title only after choosing a destination", async () => {
    const { fetch } = setup((url, options, fixture) => {
      if (url.endsWith("/organization/plans") && options?.method === "POST")
        return Response.json(fixture.plan([41]));
    });
    mount("/organization?media_id=1");
    const button = await screen.findByRole("button", {
      name: "Review all files for this title",
    });
    expect(button).toBeDisabled();
    await userEvent.selectOptions(
      screen.getByLabelText("Destination collection"),
      "anime",
    );
    await userEvent.click(button);
    await screen.findByRole("region", { name: "Organization review" });
    const call = fetch.mock.calls.find(
      ([url, options]) =>
        url.endsWith("/organization/plans") && options?.method === "POST",
    );
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      media_id: 1,
      target_library: "anime",
    });
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/apply"))).toBe(
      false,
    );
  });
  it("discards an obsolete preview reply after changing pages", async () => {
    let resolvePreview: ((response: Response) => void) | undefined;
    let previewSignal: AbortSignal | null | undefined;
    const pendingPreview = new Promise<Response>((resolve) => {
      resolvePreview = resolve;
    });
    const { fetch, fixture } = setup((url, options, data) => {
      if (url.includes("/organization/files?")) {
        const page = Number(
          new URL(url, "http://test").searchParams.get("page"),
        );
        return Response.json({
          items: [data.files[page - 1]],
          page,
          total: 2,
          limit: 1,
        });
      }
      if (url.endsWith("/organization/plans") && options?.method === "POST") {
        previewSignal = options.signal;
        return pendingPreview;
      }
    });
    mount();
    await screen.findByRole("checkbox", {
      name: "Select file 41: First feature",
    });
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Select file 41: First feature" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review 1 selected file" }),
    );
    await screen.findByRole("button", { name: "Checking files…" });
    await userEvent.click(screen.getByRole("button", { name: /Next/ }));
    await screen.findByRole("checkbox", {
      name: "Select file 42: Second feature",
    });
    expect(previewSignal?.aborted).toBe(true);
    await act(async () => {
      resolvePreview!(Response.json(fixture.plan([41])));
    });
    expect(
      screen.queryByRole("region", { name: "Organization review" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("checkbox", { name: "Select file 42: Second feature" }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("button", { name: "Review 0 selected files" }),
    ).toBeDisabled();
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/apply"))).toBe(
      false,
    );
  });

  it("resumes a saved failed job without selecting off-page file IDs", async () => {
    const { fetch } = setup((url, options, fixture) => {
      if (url.endsWith("/organization/plans") && !options?.method) {
        const failed = fixture.plan([41], "saved-failed");
        return Response.json({
          items: [
            {
              ...failed,
              state: "failed",
              files: failed.files.map((file) => ({
                ...file,
                media_file_id: 999,
                title: "Off-page saved file",
              })),
            },
          ],
        });
      }
    });
    mount();
    await screen.findByRole("checkbox", {
      name: "Select file 41: First feature",
    });
    await userEvent.click(await screen.findByText(/failed · 1 file ·/));
    await userEvent.click(
      screen.getByRole("button", { name: "Resume saved job" }),
    );
    await screen.findByRole("heading", { name: "Organization job" });
    expect(
      screen.queryByRole("checkbox", { name: /Select file 999/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("checkbox", { name: "Select file 41: First feature" }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "Select file 42: Second feature" }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("button", { name: "Review 0 selected files" }),
    ).toBeDisabled();
    const applied = fetch.mock.calls.find(([url]) => url.endsWith("/apply"));
    expect(applied?.[0]).toBe("/api/v1/organization/plans/saved-failed/apply");
    expect(applied?.[1]?.body).toBe("{}");
    expect(
      fetch.mock.calls.some(
        ([url, options]) =>
          url.endsWith("/organization/plans") && options?.method === "POST",
      ),
    ).toBe(false);
  });

  it("previews only the selected page file and cancels without applying or moving it", async () => {
    const { fetch } = setup();
    mount();
    await screen.findByRole("checkbox", {
      name: "Select file 41: First feature",
    });
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Select file 41: First feature" }),
    );
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Destination collection" }),
      "anime",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review 1 selected file" }),
    );
    const review = await screen.findByRole("region", {
      name: "Organization review",
    });
    expect(review).toHaveTextContent("Old/First feature.mkv");
    expect(review).toHaveTextContent("Movies/First feature/First feature.mkv");
    expect(review).toHaveTextContent("First feature.eng.forced.srt");
    expect(review).not.toHaveTextContent("Second feature");
    const preview = fetch.mock.calls.find(
      ([url, options]) =>
        url.endsWith("/organization/plans") && options?.method === "POST",
    );
    expect(JSON.parse(String(preview?.[1]?.body))).toEqual({
      file_ids: [41],
      target_library: "anime",
    });
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/apply"))).toBe(
      false,
    );
    await userEvent.click(
      within(review).getByRole("button", { name: "Cancel review" }),
    );
    expect(
      screen.queryByRole("region", { name: "Organization review" }),
    ).not.toBeInTheDocument();
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/apply"))).toBe(
      false,
    );
    expect(
      screen.getByRole("checkbox", { name: "Select file 41: First feature" }),
    ).toBeChecked();
  });

  it("requires path-and-subtitle review before applying the saved plan", async () => {
    const { fetch } = setup();
    mount();
    const review = await selectFirstAndReview();
    const apply = within(review).getByRole("button", {
      name: "Apply reviewed changes",
    });
    expect(apply).toBeDisabled();
    await userEvent.click(
      within(review).getByRole("checkbox", {
        name: "I reviewed all paths and subtitle changes",
      }),
    );
    expect(apply).toBeEnabled();
    await userEvent.click(apply);
    await within(review).findByRole("heading", { name: "Organization job" });
    const applied = fetch.mock.calls.find(([url]) => url.endsWith("/apply"));
    expect(applied?.[0]).toBe("/api/v1/organization/plans/plan-1/apply");
    expect(applied?.[1]?.method).toBe("POST");
    expect(applied?.[1]?.body).toBe("{}");
    expect(
      screen.getByRole("checkbox", { name: "Select file 41: First feature" }),
    ).not.toBeChecked();
  });

  it("invalidates an old review when the selection or destination changes", async () => {
    const { fetch } = setup();
    mount();
    let review = await selectFirstAndReview();
    await userEvent.click(
      within(review).getByRole("checkbox", {
        name: "I reviewed all paths and subtitle changes",
      }),
    );
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Select file 42: Second feature" }),
    );
    expect(
      screen.queryByRole("region", { name: "Organization review" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Review 2 selected files" }),
    );
    review = await screen.findByRole("region", { name: "Organization review" });
    expect(
      within(review).getByRole("button", { name: "Apply reviewed changes" }),
    ).toBeDisabled();
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Destination collection" }),
      "movies",
    );
    expect(
      screen.queryByRole("region", { name: "Organization review" }),
    ).not.toBeInTheDocument();
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/apply"))).toBe(
      false,
    );
  });

  it("requires a fresh plan after a stale apply response", async () => {
    const { fetch } = setup((url) =>
      url.endsWith("/apply")
        ? Response.json(
            { error: { message: "The source file changed." } },
            { status: 409 },
          )
        : undefined,
    );
    mount();
    let review = await selectFirstAndReview();
    await userEvent.click(
      within(review).getByRole("checkbox", {
        name: "I reviewed all paths and subtitle changes",
      }),
    );
    await userEvent.click(
      within(review).getByRole("button", { name: "Apply reviewed changes" }),
    );
    await screen.findByRole("alert");
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Create a fresh review before another attempt",
    );
    expect(
      within(review).getByRole("button", { name: "Apply reviewed changes" }),
    ).toBeDisabled();
    expect(
      within(review).getByRole("checkbox", {
        name: "I reviewed all paths and subtitle changes",
      }),
    ).toBeDisabled();
    await userEvent.click(
      screen.getByRole("button", { name: "Review 1 selected file" }),
    );
    review = await screen.findByRole("region", { name: "Organization review" });
    await waitFor(() =>
      expect(
        within(review).getByRole("checkbox", {
          name: "I reviewed all paths and subtitle changes",
        }),
      ).toBeEnabled(),
    );
    expect(
      within(review).getByRole("button", { name: "Apply reviewed changes" }),
    ).toBeDisabled();
    expect(
      fetch.mock.calls.filter(([url]) => url.endsWith("/apply")),
    ).toHaveLength(1);
    expect(
      fetch.mock.calls.filter(
        ([url, options]) =>
          url.endsWith("/organization/plans") && options?.method === "POST",
      ),
    ).toHaveLength(2);
  });

  it("saves custom collection rules with the revision while omitting the read-only main root", async () => {
    const { fetch, fixture } = setup();
    fixture.config.main_root = "";
    mount();
    await screen.findByRole("button", { name: "Folders & naming" });
    await userEvent.click(
      screen.getByRole("button", { name: "Folders & naming" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Add collection" }),
    );
    await userEvent.click(screen.getByText("New collection", { exact: true }));
    const name = screen.getByRole("textbox", { name: "Collection name 4" });
    await userEvent.clear(name);
    await userEvent.type(name, "Family");
    const folder = screen.getByRole("textbox", { name: "Collection folder 4" });
    await userEvent.clear(folder);
    await userEvent.type(folder, "Family");
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Naming preset 4" }),
      "custom",
    );
    const template = screen.getByRole("textbox", { name: "Movie template 4" });
    await userEvent.clear(template);
    await userEvent.type(template, "{{title} [[{{tmdb_id}]/{{title}");
    await userEvent.click(
      screen.getByRole("button", { name: "Save folders & naming" }),
    );
    await waitFor(() =>
      expect(
        fetch.mock.calls.some(
          ([url, options]) =>
            url.endsWith("/organization") && options?.method === "PUT",
        ),
      ).toBe(true),
    );
    const body = JSON.parse(
      String(
        fetch.mock.calls.find(
          ([url, options]) =>
            url.endsWith("/organization") && options?.method === "PUT",
        )?.[1]?.body,
      ),
    );
    expect(Object.keys(body).sort()).toEqual(["libraries", "revision"]);
    expect(body.revision).toBe(9);
    expect(body.main_root).toBeUndefined();
    expect(body.libraries).toHaveLength(4);
    expect(body.libraries[3]).toMatchObject({
      name: "Family",
      folder: "Family",
      content: "mixed",
      preset: "custom",
      movie_template: "{title} [{tmdb_id}]/{title}",
    });
    expect(body.libraries[3].key).toMatch(/^collection_/);
  });
});
