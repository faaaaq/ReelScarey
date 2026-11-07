import { Button } from "@radix-ui/themes";
import { AlertCircle } from "lucide-react";
import type { ReactNode } from "react";
import { LoadingIndicator } from "./ui";

/**
 * Page title row. `eyebrow` is accepted for compatibility but intentionally
 * not rendered: decorative page labels are not part of the design.
 */
export function PageHeading({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  eyebrow?: string;
  children?: ReactNode;
}) {
  return (
    <header className="page-heading">
      <div className="page-heading-copy">
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
  if (loading) return <LoadingIndicator label="Loading…" />;
  if (error)
    return (
      <div className="rs-alert is-danger" role="alert">
        <AlertCircle size={18} aria-hidden="true" />
        <div className="rs-alert-content">
          <span className="rs-alert-title">Could not load this page</span>
          <p>{error}</p>
          <div>
            <Button size="1" variant="soft" color="gray" onClick={retry}>
              Try again
            </Button>
          </div>
        </div>
      </div>
    );
  return null;
}
