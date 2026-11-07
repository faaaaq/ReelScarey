import { useEffect, useRef, useState, type ReactNode } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { Alert, LoadingIndicator, Modal, Table } from "../ui";
import "./Library.css";

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

/**
 * Mass-editor monitoring control for an explicit, page-scoped selection.
 * Nothing is written until the exact saved state has been reviewed.
 */
export function BulkMonitoring({
  kind,
  ids,
  onClear,
  onApplied,
  label,
  children,
}: {
  kind: "movie" | "series";
  ids: number[];
  /** Optional clear action; the library footer renders its own. */
  onClear?: () => void;
  onApplied: (message: string) => void;
  /** Optional group label shown before the controls (mixed Anime pages). */
  label?: ReactNode;
  children?: ReactNode;
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
  const state = (value: boolean) => (value ? "Monitored" : "Paused");
  return (
    <>
      <div className="lib-editor" role="region" aria-label="Selected titles">
        {label && <span className="lib-editor-label">{label}</span>}
        <label className="lib-editor-field">
          <span>Monitoring</span>
          <select
            aria-label="Monitoring action"
            value={monitored ? "resume" : "pause"}
            onChange={(e) => setMonitored(e.target.value === "resume")}
          >
            <option value="pause">Pause monitoring</option>
            <option value="resume">Resume monitoring</option>
          </select>
        </label>
        <Button size="2" variant="soft" onClick={() => void loadReview()}>
          Review monitoring
        </Button>
        {children}
        {onClear && (
          <Button size="2" variant="ghost" color="gray" onClick={onClear}>
            Clear selection
          </Button>
        )}
      </div>
      <Modal
        open={open}
        onOpenChange={(v) => {
          if (!v) close();
        }}
        title={`${monitored ? "Resume" : "Pause"} monitoring`}
        description="Changes apply together, and only if each title's saved preferences still match this review."
        footer={
          <>
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
          </>
        }
      >
        {busy === "preview" && (
          <LoadingIndicator label="Loading current monitoring preferences…" />
        )}
        {error && <Alert kind="danger">{error}</Alert>}
        {review && (
          <>
            <p>
              {changed} {changed === 1 ? "title will" : "titles will"} change;{" "}
              {review.items.length - changed} already{" "}
              {monitored ? "monitored" : "paused"}.
            </p>
            <Table label="Titles to review">
              <thead>
                <tr>
                  <th scope="col">Title</th>
                  <th scope="col">Year</th>
                  <th scope="col">ID</th>
                  <th scope="col">Change</th>
                </tr>
              </thead>
              <tbody>
                {review.items.map((item) => (
                  <tr key={item.id}>
                    <th scope="row">{item.title}</th>
                    <td>{item.year ?? "—"}</td>
                    <td className="is-numeric">{item.id}</td>
                    <td
                      className={
                        item.monitored === monitored ? "rs-muted" : undefined
                      }
                    >
                      {state(item.monitored)} → {state(monitored)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </>
        )}
        <p className="rs-muted rs-small">
          Resumed titles can be picked up by enabled automatic searches. Pausing
          keeps existing downloads running. Nothing is searched or downloaded by
          this action.
        </p>
        {kind === "series" && (
          <p className="rs-muted rs-small">
            Only show monitoring changes. Season and episode choices are
            retained.
          </p>
        )}
      </Modal>
    </>
  );
}
