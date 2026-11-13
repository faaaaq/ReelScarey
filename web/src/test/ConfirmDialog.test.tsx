import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import "../styles/app.css";

it("keeps the dialog open and shows the server error when confirm fails", async () => {
  const onConfirm = vi.fn().mockRejectedValue(new Error("boom"));
  render(
    <ConfirmDialog
      trigger={<button type="button">Remove item</button>}
      title="Remove item"
      confirmLabel="Remove"
      onConfirm={onConfirm}
    />,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Remove item" }));
  await user.click(await screen.findByRole("button", { name: "Remove" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "ReelScarey could not be reached",
  );
  // RS-0007: a failed destructive action must not look completed.
  expect(screen.getByRole("button", { name: "Remove" })).toBeInTheDocument();
  expect(onConfirm).toHaveBeenCalledTimes(1);
});

it("shows the backend's own message when the API fails with one", async () => {
  const { APIError } = await import("../api");
  const onConfirm = vi
    .fn()
    .mockRejectedValue(new APIError("This show changed. Reload and try again.", 409, "revision_conflict"));
  render(
    <ConfirmDialog
      trigger={<button type="button">Delete</button>}
      title="Delete"
      onConfirm={onConfirm}
    />,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Delete" }));
  await user.click(await screen.findByRole("button", { name: "Confirm" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "This show changed. Reload and try again.",
  );
  expect(screen.getByRole("button", { name: "Confirm" })).toBeInTheDocument();
});

it("closes only after a successful confirm", async () => {
  let settle: (() => void) | undefined;
  const onConfirm = vi.fn().mockImplementation(
    () => new Promise<void>((resolve) => { settle = resolve; }),
  );
  render(
    <ConfirmDialog
      trigger={<button type="button">Run it</button>}
      title="Run it"
      onConfirm={onConfirm}
    />,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Run it" }));
  const confirm = await screen.findByRole("button", { name: "Confirm" });
  await user.click(confirm);
  // While the callback is pending the dialog stays open with busy state.
  expect(screen.getByRole("button", { name: /Working/ })).toBeDisabled();
  settle?.();
  await screen.findByRole("button", { name: "Run it" });
  expect(screen.queryByRole("button", { name: "Confirm" })).toBeNull();
});
