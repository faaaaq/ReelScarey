import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { describe, expect, it, vi } from "vitest";
import { LibraryDeletion } from "../components/LibraryDeletion";

function mount() {
  const done = vi.fn();
  const fetch = vi.fn(async (url: string, _options?: RequestInit) => {
    if (url.endsWith("/library/deletion"))
      return Response.json({
        id: "delete-review",
        state: "review",
        removal: { paths: ["Movies/Fixture.mkv"] },
      });
    if (url.endsWith("/apply")) return Response.json({ state: "queued" });
    return Response.json({ id: "delete-review", state: "completed" });
  });
  vi.stubGlobal("fetch", fetch);
  render(
    <Theme>
      <LibraryDeletion
        selection="video_files"
        ids={[42]}
        label="Delete file"
        onDone={done}
      />
    </Theme>,
  );
  return { done, fetch };
}

describe("library file deletion", () => {
  it("keeps the file policy fixed while a deletion review is in flight", async () => {
    const { fetch } = mount();
    let finishReview!: (response: Response) => void;
    fetch.mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          finishReview = resolve;
        }),
    );
    await userEvent.click(screen.getByRole("button", { name: "Delete file" }));
    await userEvent.click(
      screen.getByRole("button", { name: "Review deletion" }),
    );
    const keep = screen.getByRole("checkbox", { name: "Keep files on disk" });
    expect(keep).toBeDisabled();
    await userEvent.click(keep);
    expect(keep).not.toBeChecked();
    finishReview(
      Response.json({
        id: "delete-review",
        state: "review",
        removal: { paths: ["Movies/Fixture.mkv"] },
      }),
    );
    await screen.findByRole("button", { name: "Delete" });
    expect(screen.getByText(/will be permanently deleted/)).toBeVisible();
  });
  it("reviews permanent deletion and waits for confirmation before applying", async () => {
    const { done, fetch } = mount();
    await userEvent.click(screen.getByRole("button", { name: "Delete file" }));
    expect(screen.getByText(/will be permanently deleted/)).toBeVisible();
    expect(screen.queryByText(/reelscarey-trash/)).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", { name: "Review deletion" }),
    );
    const request = fetch.mock.calls.find(([url]) =>
      url.endsWith("/library/deletion"),
    );
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({
      selection: "video_files",
      ids: [42],
      mode: "delete",
    });
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/apply"))).toBe(
      false,
    );
    await userEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(done).toHaveBeenCalledTimes(1));
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/apply"))).toBe(true);
  });

  it("retains the explicit option to remove registrations while keeping files", async () => {
    const { fetch } = mount();
    await userEvent.click(screen.getByRole("button", { name: "Delete file" }));
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Keep files on disk" }),
    );
    expect(screen.getByText(/files will remain on disk/)).toBeVisible();
    await userEvent.click(
      screen.getByRole("button", { name: "Review deletion" }),
    );
    const request = fetch.mock.calls.find(([url]) =>
      url.endsWith("/library/deletion"),
    );
    expect(JSON.parse(String(request?.[1]?.body)).mode).toBe("keep");
  });

  it("restores focus to the invoking control after Cancel and Escape", async () => {
    mount();
    const trigger = screen.getByRole("button", { name: "Delete file" });
    await userEvent.click(trigger);
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(trigger).toHaveFocus());

    await userEvent.click(trigger);
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(trigger).toHaveFocus());
  });

  it("explains that a timed-out review has not applied deletion", async () => {
    const { fetch } = mount();
    fetch.mockRejectedValueOnce(
      new DOMException("Review deadline", "TimeoutError"),
    );
    await userEvent.click(screen.getByRole("button", { name: "Delete file" }));
    await userEvent.click(
      screen.getByRole("button", { name: "Review deletion" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "No files were deleted.",
    );
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/apply"))).toBe(
      false,
    );
  });
});
