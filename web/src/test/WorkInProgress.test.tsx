import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { expect, it } from "vitest";
import { WorkInProgress } from "../ui";

function mount(key = "work-in-progress:subtitles") {
  return render(
    <Theme>
      <WorkInProgress noticeKey={key}>
        <p>Fixture area copy</p>
      </WorkInProgress>
    </Theme>,
  );
}

it("continues for this visit, closes with Escape, and persists Don't show again", async () => {
  const first = mount();
  expect(await screen.findByRole("dialog")).toBeVisible();
  expect(screen.getByText("Fixture area copy")).toBeVisible();
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(
    window.localStorage.getItem("reelscarey-notice:work-in-progress:subtitles"),
  ).toBeNull();
  first.unmount();

  const second = mount();
  await userEvent.click(
    await screen.findByRole("button", { name: "Continue" }),
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(
    window.localStorage.getItem("reelscarey-notice:work-in-progress:subtitles"),
  ).toBeNull();
  second.unmount();

  const third = mount();
  await userEvent.click(
    await screen.findByRole("button", { name: "Don't show again" }),
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(
    window.localStorage.getItem("reelscarey-notice:work-in-progress:subtitles"),
  ).toBe("hidden");
  third.unmount();

  mount();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("keeps each page's dismissal separate", async () => {
  window.localStorage.setItem(
    "reelscarey-notice:work-in-progress:subtitles",
    "hidden",
  );
  mount("work-in-progress:youtube");
  expect(await screen.findByRole("dialog")).toBeVisible();
  expect(screen.getByText("Fixture area copy")).toBeVisible();
});
