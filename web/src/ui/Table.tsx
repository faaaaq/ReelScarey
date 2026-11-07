import type { ReactNode } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";

/** Bordered, horizontally scrollable table container. */
export function Table({
  label,
  children,
  className,
}: {
  label: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className="rs-table-wrap">
      <table
        className={["rs-table", className].filter(Boolean).join(" ")}
        aria-label={label}
      >
        {children}
      </table>
    </div>
  );
}

export type SortState<K extends string> = {
  key: K;
  direction: "asc" | "desc";
};

/** Column header that toggles sort direction and reports aria-sort. */
export function SortHeader<K extends string>({
  column,
  label,
  sort,
  onSort,
  className,
}: {
  column: K;
  label: string;
  sort: SortState<K>;
  onSort: (next: SortState<K>) => void;
  className?: string;
}) {
  const active = sort.key === column;
  return (
    <th
      scope="col"
      className={className}
      aria-sort={
        active
          ? sort.direction === "asc"
            ? "ascending"
            : "descending"
          : undefined
      }
    >
      <button
        type="button"
        className="rs-sort-button"
        onClick={() =>
          onSort({
            key: column,
            direction: active && sort.direction === "asc" ? "desc" : "asc",
          })
        }
      >
        {label}
        {active &&
          (sort.direction === "asc" ? (
            <ChevronUp size={14} aria-hidden="true" />
          ) : (
            <ChevronDown size={14} aria-hidden="true" />
          ))}
      </button>
    </th>
  );
}
