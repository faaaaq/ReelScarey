import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DropdownMenu } from "@radix-ui/themes";
import { expect, it } from "vitest";
import { ThemeProvider } from "../theme";
import "../styles/app.css";

it("keeps page-only theme sizing off portalled menu surfaces", async () => {
  render(
    <ThemeProvider>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger>
          <button type="button">Open menu</button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Content>
          <DropdownMenu.Item>One</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Root>
    </ThemeProvider>,
  );
  expect(document.querySelector(".radix-themes.rs-app-theme")).not.toBeNull();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Open menu" }));
  const menu = await screen.findByRole("menu");
  // Radix Themes clones the radix-themes class onto portalled surfaces; the
  // page-only class (and its min-height: 100vh) must not follow it there.
  expect(menu).not.toHaveClass("rs-app-theme");
  expect(getComputedStyle(menu).minHeight).not.toBe("100vh");
});
