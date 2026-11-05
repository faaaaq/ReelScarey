import { Badge, Button, Spinner } from "@radix-ui/themes";
import { AlertCircle, ArrowRight, type LucideIcon } from "lucide-react";
import { Link } from "react-router-dom";
import type { ReactNode } from "react";

export function PageHeading({
  title,
  description,
  eyebrow,
  children,
}: {
  title: string;
  description?: string;
  eyebrow?: string;
  children?: ReactNode;
}) {
  return (
    <header className="page-heading flex flex-wrap items-end justify-between gap-4">
      <div className="page-heading-copy">
        {eyebrow && <p className="page-eyebrow">{eyebrow}</p>}
        <h1>{title}</h1>
        {description && <p className="page-description">{description}</p>}
      </div>
      {children && <div className="page-actions">{children}</div>}
    </header>
  );
}

export function RequestState({
  loading,
  error,
  retry,
}: {
  loading: boolean;
  error: string;
  retry: () => void;
}) {
  if (loading)
    return (
      <div className="request-state" role="status">
        <Spinner /> Loading your library…
      </div>
    );
  if (error)
    return (
      <div className="error-box" role="alert">
        <AlertCircle size={20} />
        <div>
          <strong>Could not load this page</strong>
          <p>{error}</p>
          <Button variant="soft" onClick={retry}>
            Try again
          </Button>
        </div>
      </div>
    );
  return null;
}

export function FuturePage({
  icon: Icon,
  title,
  description,
  phase,
  children,
}: {
  icon: LucideIcon;
  title: string;
  description: string;
  phase: string;
  children?: ReactNode;
}) {
  return (
    <section className="empty-state panel">
      <span className="empty-icon">
        <Icon size={32} strokeWidth={1.4} aria-hidden="true" />
      </span>
      <Badge color="gray" variant="soft">
        {phase}
      </Badge>
      <h2>{title}</h2>
      <p>{description}</p>
      {children}
      <Button asChild variant="soft">
        <Link to="/settings">
          View library settings <ArrowRight size={16} />
        </Link>
      </Button>
    </section>
  );
}
