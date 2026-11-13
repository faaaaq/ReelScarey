import { Button } from "@radix-ui/themes";
import { useBlocker } from "react-router-dom";
import { Modal, ModalClose } from "./ui";

/**
 * Blocks in-app route changes while a form has unsaved edits, complementing
 * the beforeunload guard for browser exits. The draft stays mounted either
 * way: confirming resumes the navigation, cancelling keeps the page with the
 * draft intact.
 *
 * Scoped to the two documented draft screens (Settings General, Subtitle
 * preferences); do not attach it globally.
 */
export function UnsavedRouteGuard({ dirty }: { dirty: boolean }) {
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      dirty &&
      (currentLocation.pathname !== nextLocation.pathname ||
        currentLocation.search !== nextLocation.search),
  );
  return (
    <Modal
      open={blocker.state === "blocked"}
      onOpenChange={(open) => {
        if (!open && blocker.state === "blocked") blocker.reset();
      }}
      title="Leave with unsaved changes?"
      description="Your edits have not been saved. Leaving this page now discards them."
      size="small"
      footer={
        <>
          <ModalClose>
            <Button variant="soft" color="gray">
              Keep editing
            </Button>
          </ModalClose>
          <Button color="red" onClick={() => blocker.proceed?.()}>
            Leave without saving
          </Button>
        </>
      }
    />
  );
}
