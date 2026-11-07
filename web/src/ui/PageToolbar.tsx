import {
  forwardRef,
  type ButtonHTMLAttributes,
  type ComponentType,
  type ReactNode,
} from "react";
import { Link } from "react-router-dom";

type IconType = ComponentType<{
  size?: number;
  className?: string;
  "aria-hidden"?: boolean | "true";
}>;

/** Sticky page toolbar beneath the header, as in Sonarr/Radarr. */
export function PageToolbar({
  children,
  label = "Page actions",
}: {
  children: ReactNode;
  label?: string;
}) {
  return (
    <div className="rs-toolbar" role="toolbar" aria-label={label}>
      {children}
    </div>
  );
}

export function ToolbarSection({
  children,
  align = "left",
}: {
  children: ReactNode;
  align?: "left" | "right";
}) {
  return (
    <div
      className={`rs-toolbar-section ${align === "right" ? "is-right" : ""}`}
    >
      {children}
    </div>
  );
}

export function ToolbarSeparator() {
  return <span className="rs-toolbar-separator" aria-hidden="true" />;
}

type ToolbarButtonProps = {
  icon: IconType;
  label: string;
  /** Spins the icon while an operation is running. */
  spinning?: boolean;
  active?: boolean;
  danger?: boolean;
  /** Renders a router link instead of a button. */
  to?: string;
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children">;

/** Icon-over-label action. Forwards refs so it can be a Radix trigger. */
export const ToolbarButton = forwardRef<HTMLButtonElement, ToolbarButtonProps>(
  function ToolbarButton(
    { icon: Icon, label, spinning, active, danger, to, className, ...rest },
    ref,
  ) {
    const classes = [
      "rs-toolbar-button",
      active ? "is-active" : "",
      danger ? "is-danger" : "",
      className ?? "",
    ]
      .filter(Boolean)
      .join(" ");
    const content = (
      <>
        <Icon
          size={21}
          aria-hidden="true"
          className={spinning ? "rs-spin" : undefined}
        />
        <span>{label}</span>
      </>
    );
    if (to && !rest.disabled)
      return (
        <Link className={classes} to={to} title={rest.title}>
          {content}
        </Link>
      );
    return (
      <button
        ref={ref}
        type="button"
        className={classes}
        aria-pressed={active === undefined ? undefined : active}
        {...rest}
      >
        {content}
      </button>
    );
  },
);
