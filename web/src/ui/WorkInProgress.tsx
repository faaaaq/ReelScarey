import { useState, type ReactNode } from "react";
import { Button } from "@radix-ui/themes";
import { Modal } from "./Modal";
import { hideNotice, useHiddenNotices } from "./notices";

/**
 * First-visit notice for areas that are still being built. "Continue" closes
 * the dialog for this visit; "Don't show again" stores the per-page choice in
 * localStorage, matching the shared notices behaviour.
 */
export function WorkInProgress({
  noticeKey,
  children,
}: {
  noticeKey: string;
  children: ReactNode;
}) {
  const hidden = useHiddenNotices([noticeKey]);
  const [closed, setClosed] = useState(false);
  if (closed || hidden.includes(noticeKey)) return null;
  return (
    <Modal
      open
      onOpenChange={(open) => !open && setClosed(true)}
      title="Work in progress"
      description="This area is still being built and may change."
      footer={
        <>
          <Button autoFocus variant="soft" onClick={() => setClosed(true)}>
            Continue
          </Button>
          <Button
            variant="soft"
            color="gray"
            onClick={() => {
              hideNotice(noticeKey, true);
              setClosed(true);
            }}
          >
            Don't show again
          </Button>
        </>
      }
    >
      {children}
    </Modal>
  );
}
