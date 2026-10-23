import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor, type DownloadJob } from "../api";
import { bytes } from "./Releases";
export function TVMapping({
  job,
  reload,
}: {
  job: DownloadJob;
  reload: () => void;
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
      reload();
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
    <section className="tv-mapping" aria-label="Episode file mapping">
      <h3>Review episode files</h3>
      <p>
        Check every suggestion against the completed file. Choose a file for
        each episode, or explicitly skip it. One file can cover up to 20
        consecutive episodes in the same season.
      </p>
      <fieldset disabled={pending}>
        {targets.map((t) => (
          <div className="mapping-row" key={t.episode_id}>
            <label
              htmlFor={`map-${job.id}-${t.episode_id}`}
            >{`S${String(t.season).padStart(2, "0")}E${String(t.number).padStart(2, "0")} · ${t.title}`}</label>
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
        <label className="mapping-review">
          <input
            type="checkbox"
            checked={reviewed}
            onChange={(e) => setReviewed(e.target.checked)}
          />{" "}
          I have reviewed these episode and file assignments
        </label>
      </fieldset>
      <Button
        disabled={!complete || !reviewed || pending}
        onClick={() => void confirm()}
      >
        {pending ? "Confirming…" : "Confirm files & import"}
      </Button>
      {error && (
        <p role="alert" className="movie-error">
          {error}
        </p>
      )}
    </section>
  );
}
