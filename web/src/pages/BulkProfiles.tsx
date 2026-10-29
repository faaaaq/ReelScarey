import { useEffect, useRef, useState } from "react";
import { Button, Dialog } from "@radix-ui/themes";
import {
  api,
  messageFor,
  type CatalogProfileContext,
  type ProfileSummary,
} from "../api";
import type { Profile } from "./Automation";

type Choice = { use_default: boolean; profile_id: number | null };
type ReviewItem = {
  id: number;
  title: string;
  year: number | null;
  profile_id: number | null;
  revision: number;
  effective_profile: ProfileSummary;
};
type Review = {
  target: Choice;
  target_profile: Profile;
  schedule_revision: number;
  default_profile: ProfileSummary;
  profiles: ProfileSummary[];
  items: ReviewItem[];
};
type Result = {
  total: number;
  changed: number;
  unchanged: number;
  profile: ProfileSummary;
  use_default: boolean;
};

export function BulkProfiles({
  kind,
  ids,
  context,
  onApplied,
}: {
  kind: "movie" | "series";
  ids: number[];
  context: CatalogProfileContext;
  onApplied: (message: string) => void;
}) {
  const [open, setOpen] = useState(false),
    [choice, setChoice] = useState("default"),
    [review, setReview] = useState<Review>(),
    [busy, setBusy] = useState(""),
    [error, setError] = useState(""),
    [stale, setStale] = useState(true);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  async function loadReview(value: string) {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setOpen(true);
    setChoice(value);
    setReview(undefined);
    setBusy("preview");
    setStale(true);
    setError("");
    const target: Choice = {
      use_default: value === "default",
      profile_id: value === "default" ? null : Number(value),
    };
    try {
      const out = await api<Review>("/library/profiles/preview", {
        method: "POST",
        body: JSON.stringify({ kind, ids, target }),
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
    if (!review || stale || busy) return;
    setBusy("apply");
    setError("");
    try {
      const result = await api<Result>("/library/profiles", {
        method: "PUT",
        body: JSON.stringify({
          kind,
          target: review.target,
          target_id: review.target_profile.id,
          target_revision: review.target_profile.revision,
          schedule_revision: review.schedule_revision,
          items: review.items.map((item) => ({
            id: item.id,
            revision: item.revision,
            effective_id: item.effective_profile.id,
            effective_revision: item.effective_profile.revision,
          })),
        }),
      });
      setOpen(false);
      onApplied(
        `${result.changed} ${result.changed === 1 ? "title" : "titles"} ${result.use_default ? (result.changed === 1 ? "now follows the schedule default" : "now follow the schedule default") : "assigned to"} (${result.profile.name}). ${result.unchanged} unchanged.`,
      );
    } catch (e) {
      setError(
        `${messageFor(e)} Review again to confirm the saved assignments and rules before another attempt.`,
      );
      setStale(true);
    } finally {
      setBusy("");
    }
  }
  const changed =
    review?.items.filter((item) =>
      review.target.use_default
        ? item.profile_id !== null
        : item.profile_id !== review.target.profile_id,
    ).length ?? 0;
  const profiles = review?.profiles ?? context.profiles,
    defaultProfile = review?.default_profile ?? context.default_profile;
  const targetLabel = review
    ? `${review.target.use_default ? "Schedule default: " : "Assigned: "}${review.target_profile.name}`
    : "";
  return (
    <>
      <Button variant="soft" onClick={() => void loadReview(choice)}>
        Assign quality profile
      </Button>
      <Dialog.Root
        open={open}
        onOpenChange={(value) => {
          if (!value) close();
        }}
      >
        <Dialog.Content maxWidth="700px">
          <Dialog.Title>Assign quality profile</Dialog.Title>
          <Dialog.Description>
            Review profile assignments for the exact selected titles.
            Monitoring, existing files and downloads stay unchanged.
          </Dialog.Description>
          <label className="profile-target">
            Target quality profile
            <select
              aria-label="Target quality profile"
              value={choice}
              disabled={busy === "apply"}
              onChange={(e) => void loadReview(e.target.value)}
            >
              <option value="default">
                Follow schedule default ({defaultProfile.name})
              </option>
              {profiles.map((profile) => (
                <option key={profile.id} value={String(profile.id)}>
                  {profile.name}
                </option>
              ))}
            </select>
          </label>
          {busy === "preview" && (
            <p role="status">Loading current assignments and profile rules…</p>
          )}
          {error && (
            <p className="form-error" role="alert">
              {error}
            </p>
          )}
          {review && (
            <>
              <p className="catalog-note">
                {changed}{" "}
                {changed === 1 ? "assignment will" : "assignments will"} change;{" "}
                {review.items.length - changed} unchanged.
              </p>
              <p className="catalog-note">
                {review.target.use_default
                  ? "These titles will follow future changes to the schedule default."
                  : "These titles will keep this explicit profile when the schedule default changes."}
              </p>
              <section
                className="profile-review-rules"
                aria-label="Target profile rules"
              >
                <h2>{review.target_profile.name}</h2>
                <dl>
                  <div>
                    <dt>Allowed resolutions</dt>
                    <dd>
                      {review.target_profile.qualities
                        .map((q) => `${q}p`)
                        .join(", ")}
                    </dd>
                  </div>
                  <div>
                    <dt>Upgrade cutoff</dt>
                    <dd>{review.target_profile.cutoff}p</dd>
                  </div>
                  <div>
                    <dt>Release language</dt>
                    <dd>{review.target_profile.language}</dd>
                  </div>
                  <div>
                    <dt>Minimum torrent seeders</dt>
                    <dd>{review.target_profile.min_seeders}</dd>
                  </div>
                  <div>
                    <dt>Maximum release size</dt>
                    <dd>{review.target_profile.max_size_gib} GiB</dd>
                  </div>
                  {review.target_profile.required_terms && (
                    <div>
                      <dt>Required terms</dt>
                      <dd>{review.target_profile.required_terms}</dd>
                    </div>
                  )}
                  {review.target_profile.rejected_terms && (
                    <div>
                      <dt>Rejected terms</dt>
                      <dd>{review.target_profile.rejected_terms}</dd>
                    </div>
                  )}
                </dl>
              </section>
              <ul className="monitoring-review profile-review-items">
                {review.items.map((item) => (
                  <li key={item.id}>
                    <div>
                      <strong>{item.title}</strong>
                      <span>
                        {item.year ?? "Year unknown"} · ID {item.id}
                      </span>
                      <p>
                        {item.profile_id === null
                          ? "Schedule default: "
                          : "Assigned: "}
                        {item.effective_profile.name} → {targetLabel}
                      </p>
                    </div>
                  </li>
                ))}
              </ul>
            </>
          )}
          <p className="catalog-note">
            Enabled searches use these profiles for future decisions. This
            action starts no search or download and replaces no files. Release
            labels do not verify audio language.
          </p>
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
              <Button disabled={!!busy} onClick={() => void loadReview(choice)}>
                Review again
              </Button>
            ) : changed ? (
              <Button disabled={!!busy} onClick={() => void apply()}>
                {busy === "apply"
                  ? "Saving…"
                  : `Apply to ${changed} ${changed === 1 ? "title" : "titles"}`}
              </Button>
            ) : (
              <Button
                disabled={!!busy}
                onClick={() => {
                  setOpen(false);
                  onApplied(
                    "All selected titles already use this assignment. No changes were needed.",
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
