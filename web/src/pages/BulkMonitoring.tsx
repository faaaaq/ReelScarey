import { useEffect, useRef, useState } from "react";
import { Button, Dialog } from "@radix-ui/themes";
import { api, messageFor } from "../api";

type ReviewItem = {
  id: number;
  title: string;
  year: number | null;
  monitored: boolean;
  series_revision: number;
  automation_revision: number;
};
type Review = { kind: string; items: ReviewItem[] };
type Result = {
  total: number;
  changed: number;
  unchanged: number;
  monitored: boolean;
};

export function BulkMonitoring({
  kind,
  ids,
  onClear,
  onApplied,
}: {
  kind: "movie" | "series";
  ids: number[];
  onClear: () => void;
  onApplied: (message: string) => void;
}) {
  const [monitored, setMonitored] = useState(false),
    [open, setOpen] = useState(false),
    [review, setReview] = useState<Review>(),
    [busy, setBusy] = useState(""),
    [error, setError] = useState(""),
    [stale, setStale] = useState(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  async function loadReview() {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setOpen(true);
    setBusy("preview");
    setError("");
    setStale(true);
    try {
      const out = await api<Review>("/library/monitoring/preview", {
        method: "POST",
        body: JSON.stringify({ kind, ids }),
        signal: AbortSignal.any([
          controller.signal,
          AbortSignal.timeout(15000),
        ]),
      });
      if (!controller.signal.aborted) {
        setReview(out);
        setStale(false);
      }
    } catch (e) {
      if (!controller.signal.aborted) setError(messageFor(e));
    } finally {
      if (!controller.signal.aborted) setBusy("");
    }
  }
  function close() {
    if (busy === "apply") return;
    request.current?.abort();
    setBusy("");
    setOpen(false);
  }
  async function apply() {
    if (!review || busy || stale) return;
    setBusy("apply");
    setError("");
    try {
      const result = await api<Result>("/library/monitoring", {
        method: "PUT",
        body: JSON.stringify({
          kind,
          monitored,
          items: review.items.map(
            ({ id, monitored, series_revision, automation_revision }) => ({
              id,
              monitored,
              series_revision,
              automation_revision,
            }),
          ),
        }),
      });
      setOpen(false);
      onApplied(
        `${result.monitored ? "Resumed" : "Paused"} ${result.changed} ${result.changed === 1 ? "title" : "titles"}. ${result.unchanged} unchanged.`,
      );
    } catch (e) {
      setError(
        `${messageFor(e)} Review again to confirm the current saved state before another attempt.`,
      );
      setStale(true);
    } finally {
      setBusy("");
    }
  }
  const changed =
    review?.items.filter((item) => item.monitored !== monitored).length ?? 0;
  return (
    <>
      <div className="bulk-toolbar" role="region" aria-label="Selected titles">
        <strong>{ids.length} selected on this page</strong>
        <label>
          Monitoring action
          <select
            aria-label="Monitoring action"
            value={monitored ? "resume" : "pause"}
            onChange={(e) => setMonitored(e.target.value === "resume")}
          >
            <option value="pause">Pause monitoring</option>
            <option value="resume">Resume monitoring</option>
          </select>
        </label>
        <Button variant="soft" onClick={() => void loadReview()}>
          Review monitoring
        </Button>
        <Button variant="ghost" color="gray" onClick={onClear}>
          Clear selection
        </Button>
      </div>
      <Dialog.Root
        open={open}
        onOpenChange={(v) => {
          if (!v) close();
        }}
      >
        <Dialog.Content maxWidth="620px">
          <Dialog.Title>
            {monitored ? "Resume" : "Pause"} monitoring
          </Dialog.Title>
          <Dialog.Description>
            Review the exact titles below. Changes are applied together only if
            their saved preferences still match this review.
          </Dialog.Description>
          {busy === "preview" && (
            <p role="status">Loading current monitoring preferences…</p>
          )}
          {error && (
            <p role="alert" className="form-error">
              {error}
            </p>
          )}
          {review && (
            <>
              <p className="catalog-note">
                {changed} {changed === 1 ? "title will" : "titles will"} change;{" "}
                {review.items.length - changed} already{" "}
                {monitored ? "monitored" : "paused"}.
              </p>
              <ul className="monitoring-review">
                {review.items.map((item) => (
                  <li key={item.id}>
                    <div>
                      <strong>{item.title}</strong>
                      <span>
                        {item.year ?? "Year unknown"} · ID {item.id}
                      </span>
                    </div>
                    <span>
                      {item.monitored ? "Monitored" : "Paused"} →{" "}
                      {monitored ? "Monitored" : "Paused"}
                    </span>
                  </li>
                ))}
              </ul>
            </>
          )}
          <p className="catalog-note">
            Resumed titles can be picked up by enabled automatic searches.
            Pausing monitoring keeps existing downloads running. This action
            does not start a search or download.
          </p>
          {kind === "series" && (
            <p className="catalog-note">
              Only show monitoring changes. Season and episode choices are
              retained.
            </p>
          )}
          <div className="monitoring-dialog-actions">
            <Button
              variant="soft"
              color="gray"
              disabled={busy === "apply"}
              onClick={close}
            >
              Cancel
            </Button>
            {stale || !review ? (
              <Button disabled={!!busy} onClick={() => void loadReview()}>
                Review again
              </Button>
            ) : changed ? (
              <Button disabled={!!busy} onClick={() => void apply()}>
                {busy === "apply"
                  ? "Saving…"
                  : `${monitored ? "Resume" : "Pause"} ${changed} ${changed === 1 ? "title" : "titles"}`}
              </Button>
            ) : (
              <Button
                disabled={!!busy}
                onClick={() => {
                  setOpen(false);
                  onApplied(
                    "All selected titles already have this monitoring preference. No changes were needed.",
                  );
                }}
              >
                Done
              </Button>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Root>
    </>
  );
}
