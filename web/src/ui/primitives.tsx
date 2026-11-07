import { useId, type ReactNode } from "react";
import { Spinner } from "@radix-ui/themes";
import {
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  Info,
  Plus,
  X,
  type LucideIcon,
} from "lucide-react";
import { Link } from "react-router-dom";
import { hideNotice, useHiddenNotices } from "./notices";

function cx(...values: (string | false | null | undefined)[]) {
  return values.filter(Boolean).join(" ");
}

/** Padded page content area below the toolbar. */
export function PageBody({
  children,
  flush,
  className,
}: {
  children: ReactNode;
  flush?: boolean;
  className?: string;
}) {
  return (
    <div className={cx("rs-page-body", flush && "is-flush", className)}>
      {children}
    </div>
  );
}

/** Sonarr-style titled group with an underlined legend. */
export function FieldSet({
  legend,
  description,
  actions,
  children,
  className,
  headingLevel = 2,
}: {
  legend: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  className?: string;
  headingLevel?: 2 | 3;
}) {
  const Heading = headingLevel === 2 ? "h2" : "h3";
  return (
    <section className={cx("rs-fieldset", className)}>
      <div className="rs-legend">
        <Heading>{legend}</Heading>
        {actions && <div className="rs-legend-actions">{actions}</div>}
      </div>
      {description && <p className="rs-fieldset-description">{description}</p>}
      {children}
    </section>
  );
}

/**
 * Label-left, control-right form row. Pass `htmlFor` matching the control's
 * id, or omit it for grouped controls (checkbox lists), which use a group.
 */
export function FormGroup({
  label,
  htmlFor,
  help,
  error,
  warning,
  children,
}: {
  label: ReactNode;
  htmlFor?: string;
  help?: ReactNode;
  error?: ReactNode;
  warning?: boolean;
  children: ReactNode;
}) {
  const id = useId();
  if (!htmlFor)
    return (
      <div className="rs-form-group" role="group" aria-labelledby={id}>
        <div className="rs-form-label" id={id}>
          {label}
        </div>
        <div className="rs-form-control">
          {children}
          {help && (
            <p className={cx("rs-form-help", warning && "is-warning")}>
              {help}
            </p>
          )}
          {error && <p className="rs-form-error">{error}</p>}
        </div>
      </div>
    );
  return (
    <div className="rs-form-group">
      <label className="rs-form-label" htmlFor={htmlFor}>
        {label}
      </label>
      <div className="rs-form-control">
        {children}
        {help && (
          <p className={cx("rs-form-help", warning && "is-warning")}>{help}</p>
        )}
        {error && <p className="rs-form-error">{error}</p>}
      </div>
    </div>
  );
}

/** Checkbox with an inline label and optional description. */
export function Check({
  label,
  description,
  checked,
  onChange,
  disabled,
  id,
}: {
  label: ReactNode;
  description?: ReactNode;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  id?: string;
}) {
  return (
    <label className="rs-check">
      <input
        id={id}
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span>
        {label}
        {description && (
          <span className="rs-form-help" style={{ display: "block" }}>
            {description}
          </span>
        )}
      </span>
    </label>
  );
}

export function FormActions({ children }: { children: ReactNode }) {
  return <div className="rs-form-actions">{children}</div>;
}

export function Panel({
  title,
  actions,
  children,
  flush,
  className,
  headingLevel = 2,
  ...rest
}: {
  title?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  flush?: boolean;
  className?: string;
  headingLevel?: 2 | 3;
  "aria-label"?: string;
}) {
  const Heading = headingLevel === 2 ? "h2" : "h3";
  return (
    <section
      className={cx("rs-panel", flush && "is-flush", className)}
      {...rest}
    >
      {(title || actions) && (
        <div className="rs-panel-header">
          {title && <Heading>{title}</Heading>}
          {actions && <div className="rs-row">{actions}</div>}
        </div>
      )}
      {children}
    </section>
  );
}

export function CardGrid({ children }: { children: ReactNode }) {
  return <div className="rs-card-grid">{children}</div>;
}

/** Tile used for indexers, clients, profiles and similar collections. */
export function Card({
  title,
  children,
  labels,
  onClick,
  to,
  ariaLabel,
}: {
  title: ReactNode;
  children?: ReactNode;
  labels?: ReactNode;
  onClick?: () => void;
  to?: string;
  ariaLabel?: string;
}) {
  const body = (
    <>
      <span className="rs-card-title">{title}</span>
      {children && <div className="rs-card-body">{children}</div>}
      {labels && <div className="rs-card-labels">{labels}</div>}
    </>
  );
  if (to)
    return (
      <Link className="rs-card is-interactive" to={to} aria-label={ariaLabel}>
        {body}
      </Link>
    );
  if (onClick)
    return (
      <button
        type="button"
        className="rs-card is-interactive"
        onClick={onClick}
        aria-label={ariaLabel}
      >
        {body}
      </button>
    );
  return <article className="rs-card">{body}</article>;
}

export function AddCard({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className="rs-add-card"
      onClick={onClick}
      aria-label={label}
      title={label}
    >
      <Plus size={36} strokeWidth={1.5} aria-hidden="true" />
    </button>
  );
}

export type Kind =
  | "default"
  | "primary"
  | "info"
  | "success"
  | "warning"
  | "danger"
  | "purple"
  | "disabled";

/** Small rectangular status tag. */
export function Label({
  kind = "default",
  outline,
  large,
  title,
  children,
}: {
  kind?: Kind;
  outline?: boolean;
  large?: boolean;
  title?: string;
  children: ReactNode;
}) {
  return (
    <span
      className={cx(
        "rs-label",
        kind !== "default" && `is-${kind}`,
        outline && "is-outline",
        large && "is-large",
      )}
      title={title}
    >
      {children}
    </span>
  );
}

const alertIcons = {
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  danger: AlertCircle,
};

/**
 * Inline message. Danger alerts announce as `alert`; others as `status`
 * unless a role is given explicitly. Pass `dismissKey` to let the owner
 * dismiss the notice for this session or hide it permanently.
 */
export function Alert({
  kind = "info",
  title,
  children,
  role,
  dismissKey,
}: {
  kind?: "info" | "success" | "warning" | "danger";
  title?: ReactNode;
  children?: ReactNode;
  role?: "alert" | "status" | "note";
  dismissKey?: string;
}) {
  const Icon = alertIcons[kind];
  const hidden = useHiddenNotices(dismissKey ? [dismissKey] : []);
  if (dismissKey && hidden.length) return null;
  return (
    <div
      className={cx("rs-alert", kind !== "info" && `is-${kind}`)}
      role={role ?? (kind === "danger" ? "alert" : "status")}
    >
      <Icon size={18} aria-hidden="true" />
      <div className="rs-alert-content">
        {title && <span className="rs-alert-title">{title}</span>}
        {children}
      </div>
      {dismissKey && (
        <div className="rs-alert-dismiss">
          <button
            type="button"
            className="rs-link-button rs-small"
            onClick={() => hideNotice(dismissKey, true)}
          >
            Don't show again
          </button>
          <button
            type="button"
            className="rs-alert-close"
            aria-label="Dismiss"
            title="Dismiss for now"
            onClick={() => hideNotice(dismissKey, false)}
          >
            <X size={16} aria-hidden="true" />
          </button>
        </div>
      )}
    </div>
  );
}
export function ProgressBar({
  value,
  kind = "primary",
  size = "medium",
  text,
  label,
}: {
  /** Fraction from 0 to 1. */
  value: number;
  kind?: "primary" | "success" | "warning" | "danger" | "purple" | "disabled";
  size?: "small" | "medium" | "large";
  text?: string;
  label: string;
}) {
  const percent = Math.max(0, Math.min(100, (value || 0) * 100));
  return (
    <div
      className={cx(
        "rs-progress",
        kind !== "primary" && `is-${kind}`,
        size !== "medium" && `is-${size}`,
      )}
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(percent * 10) / 10}
    >
      <div className="rs-progress-fill" style={{ width: `${percent}%` }} />
      {text && size !== "small" && (
        <span className="rs-progress-text">{text}</span>
      )}
    </div>
  );
}

export function EmptyState({
  icon: Icon,
  title,
  children,
  actions,
}: {
  icon?: LucideIcon;
  title: string;
  children?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <section className="rs-empty">
      {Icon && <Icon size={44} strokeWidth={1.2} aria-hidden="true" />}
      <h2>{title}</h2>
      {children && <div>{children}</div>}
      {actions && <div className="rs-empty-actions">{actions}</div>}
    </section>
  );
}

export function LoadingIndicator({ label = "Loading…" }: { label?: string }) {
  return (
    <div className="rs-loading" role="status">
      <Spinner size="3" /> {label}
    </div>
  );
}

export function DescriptionList({ children }: { children: ReactNode }) {
  return <dl className="rs-dl">{children}</dl>;
}

export function DescriptionItem({
  title,
  children,
}: {
  title: ReactNode;
  children: ReactNode;
}) {
  return (
    <>
      <dt>{title}</dt>
      <dd>{children}</dd>
    </>
  );
}
