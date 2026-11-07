import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
} from "react";
import { ContextMenu } from "@radix-ui/themes";
import { ChevronDown, ChevronUp } from "lucide-react";
import type { Column, Sort } from "./columns";
import { ContextMenuEntries, type MenuEntry } from "./menu";
import type { Torrent } from "./model";
import type { Modifiers } from "./selection";

export const rowHeight = 26;
/** Below this many rows everything renders; above it rows are windowed. */
const windowThreshold = 250;
const overscan = 12;

type RowProps = {
  torrent: Torrent;
  columns: Column[];
  index: number;
  selected: boolean;
  focused: boolean;
  onClick: (hash: string, mods: Modifiers) => void;
  onDoubleClick: (hash: string) => void;
  onContext: (hash: string) => void;
};

const Row = memo(function Row({
  torrent,
  columns,
  index,
  selected,
  focused,
  onClick,
  onDoubleClick,
  onContext,
}: RowProps) {
  return (
    <tr
      id={`tor-row-${torrent.hash}`}
      className={`tor-row ${index % 2 ? "is-odd" : ""} ${selected ? "is-selected" : ""} ${focused ? "is-focused" : ""}`}
      role="row"
      aria-selected={selected}
      aria-rowindex={index + 2}
      onClick={(e: MouseEvent) =>
        onClick(torrent.hash, {
          ctrl: e.ctrlKey || e.metaKey,
          shift: e.shiftKey,
        })
      }
      onDoubleClick={() => onDoubleClick(torrent.hash)}
      onContextMenu={() => onContext(torrent.hash)}
    >
      {columns.map((c) => (
        <td
          key={c.key}
          role="gridcell"
          className={c.numeric ? "is-numeric" : undefined}
        >
          {c.render(torrent)}
        </td>
      ))}
    </tr>
  );
});

export function TorrentTable({
  rows,
  columns,
  sort,
  onSort,
  selected,
  focus,
  onRowClick,
  onRowDoubleClick,
  onRowContext,
  onMove,
  onToggleFocus,
  onSelectAll,
  onClear,
  onDelete,
  menuEntries,
  emptyText,
}: {
  rows: Torrent[];
  columns: Column[];
  sort: Sort;
  onSort: (sort: Sort) => void;
  selected: ReadonlySet<string>;
  focus: string | null;
  onRowClick: (hash: string, mods: Modifiers) => void;
  onRowDoubleClick: (hash: string) => void;
  onRowContext: (hash: string) => void;
  onMove: (delta: number | "start" | "end", mods: Modifiers) => void;
  onToggleFocus: () => void;
  onSelectAll: () => void;
  onClear: () => void;
  onDelete: () => void;
  menuEntries: MenuEntry[];
  emptyText: string;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [viewport, setViewport] = useState({ top: 0, height: 0 });

  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const update = () =>
      setViewport({ top: el.scrollTop, height: el.clientHeight });
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (el) setViewport({ top: el.scrollTop, height: el.clientHeight });
  }, []);

  const windowed = rows.length > windowThreshold && viewport.height > 0;
  let start = 0;
  let end = rows.length;
  if (windowed) {
    start = Math.max(0, Math.floor(viewport.top / rowHeight) - overscan);
    end = Math.min(
      rows.length,
      Math.ceil((viewport.top + viewport.height) / rowHeight) + overscan,
    );
  }

  const focusIndex = focus ? rows.findIndex((t) => t.hash === focus) : -1;
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || focusIndex < 0) return;
    // Row offsets include the sticky header, which covers the first row slot.
    const top = rowHeight + focusIndex * rowHeight;
    if (top - rowHeight < el.scrollTop) el.scrollTop = top - rowHeight;
    else if (top + rowHeight > el.scrollTop + el.clientHeight)
      el.scrollTop = top + rowHeight - el.clientHeight;
  }, [focusIndex]);

  function keyDown(event: KeyboardEvent) {
    const mods = {
      ctrl: event.ctrlKey || event.metaKey,
      shift: event.shiftKey,
    };
    const page = Math.max(
      1,
      Math.floor((scrollRef.current?.clientHeight ?? 0) / rowHeight) - 1,
    );
    let handled = true;
    switch (event.key) {
      case "ArrowDown":
        onMove(1, mods);
        break;
      case "ArrowUp":
        onMove(-1, mods);
        break;
      case "PageDown":
        onMove(page, mods);
        break;
      case "PageUp":
        onMove(-page, mods);
        break;
      case "Home":
        onMove("start", mods);
        break;
      case "End":
        onMove("end", mods);
        break;
      case " ":
        if (mods.ctrl) onToggleFocus();
        else handled = false;
        break;
      case "Escape":
        onClear();
        break;
      case "Delete":
        onDelete();
        break;
      case "a":
      case "A":
        if (mods.ctrl) onSelectAll();
        else handled = false;
        break;
      default:
        handled = false;
    }
    if (handled) {
      event.preventDefault();
      event.stopPropagation();
    }
  }

  const totalWidth = columns.reduce((sum, c) => sum + c.width, 0);

  return (
    <ContextMenu.Root>
      <ContextMenu.Trigger>
        <div className="tor-table-scroll" ref={scrollRef} onScroll={onScroll}>
          <table
            className="tor-table"
            role="grid"
            aria-label="Torrents"
            aria-multiselectable="true"
            aria-rowcount={rows.length + 1}
            aria-activedescendant={
              focusIndex >= 0 && focusIndex >= start && focusIndex < end
                ? `tor-row-${focus}`
                : undefined
            }
            tabIndex={0}
            onKeyDown={keyDown}
            style={{ minWidth: totalWidth }}
          >
            <colgroup>
              {columns.map((c) => (
                <col
                  key={c.key}
                  style={c.key === "name" ? undefined : { width: c.width }}
                />
              ))}
            </colgroup>
            <thead>
              <tr role="row" aria-rowindex={1}>
                {columns.map((c) => {
                  const active = sort.key === c.key;
                  return (
                    <th
                      key={c.key}
                      scope="col"
                      role="columnheader"
                      className={c.numeric ? "is-numeric" : undefined}
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
                        className="tor-sort"
                        title={c.title ?? c.label}
                        tabIndex={-1}
                        onClick={() =>
                          onSort({
                            key: c.key,
                            direction:
                              active && sort.direction === "asc"
                                ? "desc"
                                : "asc",
                          })
                        }
                      >
                        <span className="tor-sort-label">{c.label}</span>
                        {active &&
                          (sort.direction === "asc" ? (
                            <ChevronUp size={12} aria-hidden="true" />
                          ) : (
                            <ChevronDown size={12} aria-hidden="true" />
                          ))}
                      </button>
                    </th>
                  );
                })}
              </tr>
            </thead>
            <tbody>
              {windowed && start > 0 && (
                <tr aria-hidden="true" style={{ height: start * rowHeight }} />
              )}
              {rows.slice(start, end).map((t, i) => (
                <Row
                  key={t.hash}
                  torrent={t}
                  columns={columns}
                  index={start + i}
                  selected={selected.has(t.hash)}
                  focused={focus === t.hash}
                  onClick={onRowClick}
                  onDoubleClick={onRowDoubleClick}
                  onContext={onRowContext}
                />
              ))}
              {windowed && end < rows.length && (
                <tr
                  aria-hidden="true"
                  style={{ height: (rows.length - end) * rowHeight }}
                />
              )}
            </tbody>
          </table>
          {rows.length === 0 && <p className="tor-table-empty">{emptyText}</p>}
        </div>
      </ContextMenu.Trigger>
      <ContextMenu.Content size="1" className="tor-menu">
        <ContextMenuEntries entries={menuEntries} />
      </ContextMenu.Content>
    </ContextMenu.Root>
  );
}
