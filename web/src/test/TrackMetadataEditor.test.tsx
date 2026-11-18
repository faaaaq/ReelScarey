import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Theme } from "@radix-ui/themes";
import { describe, expect, it, vi } from "vitest";
import { TrackMetadataEditor } from "../pages/details/TrackMetadataEditor";
import type { DetailFile } from "../pages/details/useItemDetails";

const file: DetailFile = {
  id: 42,
  path: "/media/Movies/Owned.mkv",
  directory: "/media/Movies",
  size_bytes: 1000,
  imported_at: 1,
  download_id: "",
  release: "Owned",
  episodes: "",
  probe: {
    codec: "h264",
    width: 1920,
    height: 1080,
    duration_seconds: 60,
    audio_streams: [
      {
        index: 2,
        codec: "aac",
        language: "eng",
        title: "English",
        default: true,
        forced: false,
      },
    ],
    subtitle_streams: [
      {
        index: 3,
        language: "und",
        title: "Signs/songs",
        default: false,
        forced: true,
        hearing_impaired: false,
      },
    ],
  },
};

describe("Matroska track field editor", () => {
  it("submits only changed fields, reviews them, and completes through the plan worker", async () => {
    const done = vi.fn();
    const fetch = vi.fn(async (input: string, options?: RequestInit) => {
      const url = new URL(input, "http://local");
      if (url.pathname === "/api/v1/organization/track-metadata") {
        const body = JSON.parse(String(options?.body));
        return Response.json(
          {
            id: "plan1",
            state: "review",
            message: "",
            replacement: {
              before: { path: "Movies/Owned.mkv" },
              track_edits: body.edits,
            },
          },
          { status: 201 },
        );
      }
      if (url.pathname === "/api/v1/organization/plans/plan1/apply")
        return Response.json({ queued: true }, { status: 202 });
      if (url.pathname === "/api/v1/organization/plans/plan1")
        return Response.json({
          id: "plan1",
          state: "completed",
          message: "",
          replacement: {
            before: { path: "Movies/Owned.mkv" },
            track_edits: [],
          },
        });
      return Response.json(
        { error: { message: "Unexpected request" } },
        { status: 404 },
      );
    });
    vi.stubGlobal("fetch", fetch);
    render(
      <Theme>
        <TrackMetadataEditor file={file} onDone={done} />
      </Theme>,
    );

    await userEvent.click(
      screen.getByRole("button", { name: "Edit track fields" }),
    );
    expect(screen.getByLabelText("Title for audio stream 2")).toHaveValue(
      "English",
    );
    expect(screen.getByLabelText("Title for subtitles stream 3")).toHaveValue(
      "Signs/songs",
    );
    await userEvent.clear(screen.getByLabelText("Title for audio stream 2"));
    await userEvent.type(
      screen.getByLabelText("Title for audio stream 2"),
      "Commentary",
    );
    await userEvent.clear(
      screen.getByLabelText("Language for subtitles stream 3"),
    );
    await userEvent.type(
      screen.getByLabelText("Language for subtitles stream 3"),
      "eng",
    );
    await userEvent.click(
      screen.getByRole("checkbox", { name: "Forced audio stream 2" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review track changes" }),
    );

    expect(await screen.findByText("Movies/Owned.mkv")).toBeVisible();
    const preview = fetch.mock.calls.find(([url]) =>
      String(url).includes("track-metadata"),
    );
    expect(JSON.parse(String(preview?.[1]?.body))).toEqual({
      media_file_id: 42,
      edits: [
        { index: 2, title: "Commentary", forced: true },
        { index: 3, language: "eng" },
      ],
    });
    expect(screen.getByText(/title=Commentary, forced=true/)).toBeVisible();
    expect(screen.getByText(/language=eng/)).toBeVisible();

    await userEvent.click(
      screen.getByRole("button", { name: "Confirm & edit tracks" }),
    );
    await waitFor(() => expect(done).toHaveBeenCalledTimes(1));
    expect(
      fetch.mock.calls.some(([url]) => String(url).includes("/apply")),
    ).toBe(true);
  });
});
