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
