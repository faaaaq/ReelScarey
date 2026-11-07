import type { ComponentProps } from "react";
import { DropdownMenu } from "@radix-ui/themes";
import { ToolbarButton } from "../../ui";

type Icon = ComponentProps<typeof ToolbarButton>["icon"];

/** Toolbar button that opens a single-choice menu (Sonarr's View/Filter menus). */
export function ToolbarMenu({
  icon,
  label,
  menuLabel,
  value,
  options,
  onChange,
  disabled,
}: {
  icon: Icon;
  label: string;
  /** Accessible name of the menu trigger, e.g. "Media type". */
  menuLabel: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const current = options.find((o) => o.value === value);
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger disabled={disabled}>
        <ToolbarButton
          icon={icon}
          label={label}
          aria-label={`${menuLabel}: ${current?.label ?? value}`}
          title={`${menuLabel}: ${current?.label ?? value}`}
          className={
            current && current.value !== options[0]?.value
              ? "is-active"
              : undefined
          }
        />
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end">
        <DropdownMenu.Label>{menuLabel}</DropdownMenu.Label>
        <DropdownMenu.RadioGroup value={value} onValueChange={onChange}>
          {options.map((option) => (
            <DropdownMenu.RadioItem key={option.value} value={option.value}>
              {option.label}
            </DropdownMenu.RadioItem>
          ))}
        </DropdownMenu.RadioGroup>
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  );
}
