import { useState, type ReactNode } from "react";
import { Button } from "@radix-ui/themes";
import { Modal, ModalClose } from "./Modal";

/**
 * A review-style confirm dialog built on the Modal component. Use it instead
 * of window.confirm for destructive or review-required actions: the dialog is
 * stylable, accessible and consistent with the rest of the UI.
 *
 * The trigger opens the dialog; the confirm button fires onConfirm (async),
 * and the dialog stays open with a spinner until the callback settles.
 */
export function ConfirmDialog({
  trigger,
  title,
  description,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  color = "red",
  onConfirm,
}: {
  trigger: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  color?: "red" | "orange" | "blue";
  onConfirm: () => void | Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  async function handleConfirm() {
    setBusy(true);
    try {
      await onConfirm();
    } finally {
      setBusy(false);
      setOpen(false);
    }
  }
  return (
    <Modal
      open={open}
      onOpenChange={(next) => {
        if (!busy) setOpen(next);
      }}
      title={title}
      description={description}
      size="small"
      trigger={trigger}
      footer={
        <>
          <ModalClose>
            <Button variant="soft" color="gray" disabled={busy}>
              {cancelLabel}
            </Button>
          </ModalClose>
          <Button
            color={color}
            disabled={busy}
            onClick={() => void handleConfirm()}
          >
            {busy ? "Working…" : confirmLabel}
          </Button>
        </>
      }
    />
  );
}
