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

export function ContextMenuEntries({ entries }: { entries: MenuEntry[] }) {
  return (
    <>
      {entries.map((entry, index) => {
        switch (entry.kind) {
          case "separator":
            return <ContextMenu.Separator key={index} />;
          case "item":
            return (
              <ContextMenu.Item
                key={index}
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
                key={index}
                checked={entry.checked}
                disabled={entry.disabled}
                onSelect={entry.onSelect}
              >
                {entry.label}
              </ContextMenu.CheckboxItem>
            );
          case "sub":
            return (
              <ContextMenu.Sub key={index}>
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
            return <DropdownMenu.Separator key={index} />;
          case "item":
            return (
              <DropdownMenu.Item
                key={index}
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
                key={index}
                checked={entry.checked}
                disabled={entry.disabled}
                onSelect={entry.onSelect}
              >
                {entry.label}
              </DropdownMenu.CheckboxItem>
            );
          case "sub":
            return (
              <DropdownMenu.Sub key={index}>
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
