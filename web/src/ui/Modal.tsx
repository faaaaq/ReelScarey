import type { ReactNode } from "react";
import { Dialog } from "@radix-ui/themes";
import { X } from "lucide-react";

const widths = {
  small: "440px",
  medium: "640px",
  large: "880px",
  xlarge: "1180px",
};

/**
 * Header/body/footer dialog in the Sonarr layout, built on Radix Dialog for
 * focus trapping and escape handling. Render triggers outside and control
 * `open`, or pass `trigger` (wrapped in Dialog.Trigger).
 */
export function Modal({
  open,
  onOpenChange,
  title,
  description,
  size = "medium",
  trigger,
  footer,
  children,
}: {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  size?: keyof typeof widths;
  trigger?: ReactNode;
  footer?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      {trigger && <Dialog.Trigger>{trigger}</Dialog.Trigger>}
      <Dialog.Content
        className="rs-modal"
        maxWidth={widths[size]}
        {...(description ? {} : { "aria-describedby": undefined })}
      >
        <div className="rs-modal-header">
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Close>
            <button type="button" className="rs-modal-close" aria-label="Close">
              <X size={18} aria-hidden="true" />
            </button>
          </Dialog.Close>
        </div>
        <div className="rs-modal-body">
          {description && (
            <Dialog.Description className="rs-modal-description" size="2">
              {description}
            </Dialog.Description>
          )}
          {children}
        </div>
        {footer && <div className="rs-modal-footer">{footer}</div>}
      </Dialog.Content>
    </Dialog.Root>
  );
}

/** Closes the surrounding Modal; use for Cancel buttons in footers. */
export const ModalClose = Dialog.Close;
