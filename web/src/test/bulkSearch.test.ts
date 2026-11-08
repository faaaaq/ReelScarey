import { describe, expect, it } from "vitest";
import { bulkSearchMessage } from "../pages/details/bulkSearch";

function done(overrides: {
  mode: string;
  checked: number;
  matched: number;
  queued: number;
  interrupted?: boolean;
}) {
  return {
    status: "done" as const,
    message: "",
    interrupted: false,
    ...overrides,
  };
}

describe("bulk search result messages", () => {
  it("reports found releases and queued downloads", () => {
    expect(
      bulkSearchMessage(
        done({ mode: "download", checked: 4, matched: 2, queued: 2 }),
      ),
    ).toBe(
      "Searched 4 items; found 2 matching releases and queued 2 downloads.",
    );
    expect(
      bulkSearchMessage(
        done({ mode: "download", checked: 1, matched: 0, queued: 0 }),
      ),
    ).toBe("Searched 1 item; no matching releases were found.");
    expect(
      bulkSearchMessage(
        done({ mode: "download", checked: 3, matched: 1, queued: 0 }),
      ),
    ).toBe(
      "Searched 3 items; found 1 matching release, but queued no downloads. Open Automation for details.",
    );
  });

  it("reports releases saved for review in search-only mode", () => {
    expect(
      bulkSearchMessage(
        done({ mode: "search", checked: 5, matched: 2, queued: 0 }),
      ),
    ).toBe(
      "Searched 5 items; found 2 matching releases, saved for review in Automation.",
    );
    expect(
      bulkSearchMessage(
        done({ mode: "search", checked: 2, matched: 0, queued: 0 }),
      ),
    ).toBe("Searched 2 items; no matching releases were found.");
  });

  it("explains an empty run and flags an interruption", () => {
    expect(
      bulkSearchMessage({
        status: "done",
        mode: "search",
        checked: 0,
        queued: 0,
        matched: 0,
        interrupted: false,
        message: "Waiting for an enabled indexer.",
      }),
    ).toContain("No items needed a search.");
    expect(
      bulkSearchMessage(
        done({
          mode: "download",
          checked: 2,
          matched: 1,
          queued: 1,
          interrupted: true,
        }),
      ),
    ).toContain("The run was interrupted");
  });
});
