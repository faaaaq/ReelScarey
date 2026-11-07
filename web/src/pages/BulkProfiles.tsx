import { useEffect, useId, useRef, useState } from "react";
import { Button } from "@radix-ui/themes";
import {
  api,
  messageFor,
  type CatalogProfileContext,
  type ProfileSummary,
} from "../api";
import {
  Alert,
  DescriptionItem,
  DescriptionList,
  FormGroup,
  LoadingIndicator,
  Modal,
  Table,
} from "../ui";
import type { Profile } from "./Automation";
import "./Library.css";

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

/**
 * Mass-editor quality profile control. The complete review, including the
 * target rules and schedule revision, is sent back with the write.
 */
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
  const targetId = useId();
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
      <Button size="2" variant="soft" onClick={() => void loadReview(choice)}>
        Assign quality profile
      </Button>
      <Modal
        open={open}
        onOpenChange={(value) => {
          if (!value) close();
        }}
        size="large"
        title="Assign quality profile"
        description="Monitoring, existing files and downloads stay unchanged. Nothing is searched, downloaded or replaced."
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
          </>
        }
      >
        <FormGroup
          label="Quality profile"
          htmlFor={targetId}
          help={
            review
              ? review.target.use_default
                ? "These titles will follow future changes to the schedule default."
                : "These titles will keep this explicit profile when the schedule default changes."
              : undefined
          }
        >
          <select
            id={targetId}
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
        </FormGroup>
        {busy === "preview" && (
          <LoadingIndicator label="Loading current assignments and profile rules…" />
        )}
        {error && <Alert kind="danger">{error}</Alert>}
        {review && (
          <>
            <section
              className="lib-review-rules"
              aria-label="Target profile rules"
            >
              <h3>{review.target_profile.name}</h3>
              <DescriptionList>
                <DescriptionItem title="Allowed resolutions">
                  {review.target_profile.qualities
                    .map((q) => `${q}p`)
                    .join(", ")}
                </DescriptionItem>
                <DescriptionItem title="Upgrade cutoff">
                  {review.target_profile.cutoff}p
                </DescriptionItem>
                <DescriptionItem title="Release language">
                  {review.target_profile.language}
                </DescriptionItem>
                <DescriptionItem title="Minimum torrent seeders">
                  {review.target_profile.min_seeders}
                </DescriptionItem>
                <DescriptionItem title="Maximum release size">
                  {review.target_profile.max_size_gib} GiB
                </DescriptionItem>
                {review.target_profile.required_terms && (
                  <DescriptionItem title="Required terms">
                    {review.target_profile.required_terms}
                  </DescriptionItem>
                )}
                {review.target_profile.rejected_terms && (
                  <DescriptionItem title="Rejected terms">
                    {review.target_profile.rejected_terms}
                  </DescriptionItem>
                )}
              </DescriptionList>
            </section>
            <p>
              {changed} {changed === 1 ? "assignment will" : "assignments will"}{" "}
              change; {review.items.length - changed} unchanged.
            </p>
            <Table label="Assignments to review">
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
                    <td>
                      {item.profile_id === null
                        ? "Schedule default: "
                        : "Assigned: "}
                      {item.effective_profile.name} → {targetLabel}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </>
        )}
        <p className="rs-muted rs-small">
          Enabled searches use these profiles for future decisions. Release
          labels do not verify audio language.
        </p>
      </Modal>
    </>
  );
}
