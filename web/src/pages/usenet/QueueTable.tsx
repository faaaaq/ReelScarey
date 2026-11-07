import { ProgressBar, Label, Table, formatBytes, formatSpeed } from "../../ui";
import { statusTone, type UsenetItem } from "./model";

const labelKind = {
  default: "default",
  primary: "primary",
  info: "info",
  success: "success",
  warning: "warning",
  danger: "danger",
  disabled: "disabled",
} as const;

type Tone = keyof typeof labelKind;

export function UsenetQueueTable({
  items,
  selected,
  onToggle,
  onToggleAll,
  emptyText,
}: {
  items: UsenetItem[];
  selected: Set<string>;
  onToggle: (id: string) => void;
  onToggleAll: (checked: boolean) => void;
  emptyText: string;
}) {
  const allSelected =
    items.length > 0 && items.every((i) => selected.has(i.id));
  return (
    <Table label="Usenet queue" className="use-table">
      <thead>
        <tr>
          <th scope="col" className="use-check">
            <input
              type="checkbox"
              aria-label="Select all queued jobs"
              checked={allSelected}
              onChange={(e) => onToggleAll(e.target.checked)}
            />
          </th>
          <th scope="col">Name</th>
          <th scope="col">Status</th>
          <th scope="col">Progress</th>
          <th scope="col" className="use-number">
            Size
          </th>
          <th scope="col" className="use-number">
            Remaining
          </th>
          <th scope="col" className="use-number">
            Speed
          </th>
          <th scope="col">ETA</th>
        </tr>
      </thead>
      <tbody>
        {items.length === 0 && (
          <tr>
            <td colSpan={8} className="use-empty">
              {emptyText}
            </td>
          </tr>
        )}
        {items.map((item) => (
          <tr
            key={item.id}
            className={selected.has(item.id) ? "is-selected" : undefined}
          >
            <td className="use-check">
              <input
                type="checkbox"
                aria-label={`Select ${item.name}`}
                checked={selected.has(item.id)}
                onChange={() => onToggle(item.id)}
              />
            </td>
            <td>
              <div className="use-name" title={item.name}>
                {item.name}
              </div>
              {item.category && (
                <span className="use-sub">{item.category}</span>
              )}
            </td>
            <td>
              <Label kind={labelKind[statusTone(item) as Tone]}>
                {item.status}
              </Label>
            </td>
            <td className="use-progress">
              <ProgressBar
                value={item.progress}
                size="small"
                label={`${Math.round(item.progress * 100)}%`}
              />
            </td>
            <td className="use-number">
              {item.size ? formatBytes(item.size) : "—"}
            </td>
            <td className="use-number">
              {item.remaining ? formatBytes(item.remaining) : "0 B"}
            </td>
            <td className="use-number">
              {item.speed > 0 ? formatSpeed(item.speed, "0 B/s") : "—"}
            </td>
            <td>{item.eta || "—"}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
