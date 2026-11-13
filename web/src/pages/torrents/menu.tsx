import { ContextMenu, DropdownMenu } from "@radix-ui/themes";

export type MenuEntry =
  | {
      kind: "item";
      label: string;
      onSelect: () => void;
      disabled?: boolean;
      danger?: boolean;
      shortcut?: string;
    }
  | {
      kind: "check";
      label: string;
      checked: boolean;
      onSelect: () => void;
      disabled?: boolean;
    }
  | { kind: "separator" }
  | { kind: "sub"; label: string; entries: MenuEntry[]; disabled?: boolean };

/** Stable React key for a menu entry (labels, with positional separators). */
function entryKey(entry: MenuEntry, index: number) {
  return entry.kind === "separator" ? `separator-${index}` : entry.label;
}

export function ContextMenuEntries({ entries }: { entries: MenuEntry[] }) {
  return (
    <>
      {entries.map((entry, index) => {
        switch (entry.kind) {
          case "separator":
            return <ContextMenu.Separator key={entryKey(entry, index)} />;
          case "item":
            return (
              <ContextMenu.Item
                key={entryKey(entry, index)}
                disabled={entry.disabled}
                color={entry.danger ? "red" : undefined}
                shortcut={entry.shortcut}
                onSelect={entry.onSelect}
              >
                {entry.label}
              </ContextMenu.Item>
            );
          case "check":
            return (
              <ContextMenu.CheckboxItem
                key={entryKey(entry, index)}
                checked={entry.checked}
                disabled={entry.disabled}
                onSelect={entry.onSelect}
              >
                {entry.label}
              </ContextMenu.CheckboxItem>
            );
          case "sub":
            return (
              <ContextMenu.Sub key={entryKey(entry, index)}>
                <ContextMenu.SubTrigger disabled={entry.disabled}>
                  {entry.label}
                </ContextMenu.SubTrigger>
                <ContextMenu.SubContent>
                  <ContextMenuEntries entries={entry.entries} />
                </ContextMenu.SubContent>
              </ContextMenu.Sub>
            );
        }
      })}
    </>
  );
}

export function DropdownMenuEntries({ entries }: { entries: MenuEntry[] }) {
  return (
    <>
      {entries.map((entry, index) => {
        switch (entry.kind) {
          case "separator":
            return <DropdownMenu.Separator key={entryKey(entry, index)} />;
          case "item":
            return (
              <DropdownMenu.Item
                key={entryKey(entry, index)}
                disabled={entry.disabled}
                color={entry.danger ? "red" : undefined}
                shortcut={entry.shortcut}
                onSelect={entry.onSelect}
              >
                {entry.label}
              </DropdownMenu.Item>
            );
          case "check":
            return (
              <DropdownMenu.CheckboxItem
                key={entryKey(entry, index)}
                checked={entry.checked}
                disabled={entry.disabled}
                onSelect={entry.onSelect}
              >
                {entry.label}
              </DropdownMenu.CheckboxItem>
            );
          case "sub":
            return (
              <DropdownMenu.Sub key={entryKey(entry, index)}>
                <DropdownMenu.SubTrigger disabled={entry.disabled}>
                  {entry.label}
                </DropdownMenu.SubTrigger>
                <DropdownMenu.SubContent>
                  <DropdownMenuEntries entries={entry.entries} />
                </DropdownMenu.SubContent>
              </DropdownMenu.Sub>
            );
        }
      })}
    </>
  );
}
