import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor, type DownloadJob } from "../../api";
import { Alert, Check, episodeCode } from "../../ui";
import { bytes } from "../Releases";

/**
 * Explicit file-to-episode review for a completed TV download. Nothing is
 * imported until every target has a file or an explicit skip and the owner
 * confirms the review.
 */
export function EpisodeMappingReview({
  job,
  onDone,
}: {
  job: DownloadJob;
  onDone: () => void;
}) {
  const targets = job.targets ?? [];
  const [values, setValues] = useState<Record<number, string>>(() =>
    Object.fromEntries(
      targets.map((t) => {
        const matches = job.choices.filter((f) =>
          f.suggestions?.includes(t.episode_id),
        );
        return [
          t.episode_id,
          matches.length === 1 ? String(matches[0].index) : "",
        ];
      }),
    ),
  );
  const [reviewed, setReviewed] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function confirm() {
    if (pending || !reviewed) return;
    setPending(true);
    setError("");
    const grouped = new Map<number, number[]>();
    const skip: number[] = [];
    for (const t of targets) {
      const v = values[t.episode_id];
      if (v === "skip") skip.push(t.episode_id);
      else {
        const index = Number(v);
        grouped.set(index, [...(grouped.get(index) ?? []), t.episode_id]);
      }
    }
    try {
      await api(`/downloads/${job.id}/mapping`, {
        method: "POST",
        body: JSON.stringify({
          files: Array.from(grouped, ([file_index, episode_ids]) => ({
            file_index,
            episode_ids,
          })),
          skip_episode_ids: skip,
        }),
      });
      onDone();
    } catch (e) {
      setError(messageFor(e));
      setReviewed(false);
    } finally {
      setPending(false);
    }
  }
  const complete =
    targets.length > 0 &&
    targets.every(
      (t) => values[t.episode_id] !== undefined && values[t.episode_id] !== "",
    ) &&
    targets.some((t) => values[t.episode_id] !== "skip");
  return (
    <section className="act-mapping" aria-label="Episode file mapping">
      <h3>Review episode files</h3>
      <p className="rs-muted rs-small">
        Choose the completed file for each episode, or skip it. One file can
        cover up to 20 consecutive episodes of the same season.
      </p>
      <fieldset disabled={pending} className="act-mapping-fields">
        <div className="act-mapping-table">
          {targets.map((t) => (
            <div className="act-mapping-row" key={t.episode_id}>
              <label htmlFor={`map-${job.id}-${t.episode_id}`}>
                {`${episodeCode(t.season, t.number)}${t.absolute_number ? ` · Absolute ${t.absolute_number}` : ""} · ${t.title}`}
              </label>
              <select
                id={`map-${job.id}-${t.episode_id}`}
                value={values[t.episode_id] ?? ""}
                onChange={(e) => {
                  setValues((v) => ({ ...v, [t.episode_id]: e.target.value }));
                  setReviewed(false);
                }}
              >
                <option value="">Choose a completed file…</option>
                <option value="skip">Skip this episode</option>
                {job.choices.map((f) => (
                  <option key={f.index} value={f.index}>
                    {f.name} ({bytes(f.size_bytes)})
                  </option>
                ))}
              </select>
            </div>
          ))}
        </div>
        <Check
          label="I have reviewed these episode and file assignments"
          checked={reviewed}
          onChange={setReviewed}
        />
      </fieldset>
      <div className="rs-form-actions">
        <Button
          disabled={!complete || !reviewed || pending}
          onClick={() => void confirm()}
        >
          {pending ? "Confirming…" : "Confirm files & import"}
        </Button>
      </div>
      {error && <Alert kind="danger">{error}</Alert>}
    </section>
  );
}
