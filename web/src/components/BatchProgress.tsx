import { releaseBytes } from "../ui";
export type WorkProgress = {
  file?: string;
  stage: string;
  bytes_done?: number;
  bytes_total?: number;
  started_at: number;
  updated_at: number;
};
export function BatchProgress({
  progress,
  processed,
  total,
  state,
}: {
  progress?: WorkProgress;
  processed?: number;
  total?: number;
  state: string;
}) {
  const bytes = progress?.bytes_total ?? 0;
  return (
    <div className="organization-progress">
      {total !== undefined && total > 0 && (
        <>
          <progress
            aria-label="Batch completion"
            max={total}
            value={processed ?? 0}
          />
          <span>
            {processed ?? 0} of {total} files complete ·{" "}
            {Math.floor((100 * (processed ?? 0)) / total)}%
          </span>
        </>
      )}
      {state === "running" && progress && (
        <>
          <strong>{progress.stage}</strong>
          {progress.file && (
            <code className="organization-current-file">{progress.file}</code>
          )}
          {bytes > 0 && (
            <>
              <progress
                aria-label="Current file verification"
                max={bytes}
                value={progress.bytes_done ?? 0}
              />
              <span>
                {releaseBytes(progress.bytes_done ?? 0)} of{" "}
                {releaseBytes(bytes)} checked
              </span>
            </>
          )}
          <small>
            Step updated{" "}
            {new Date(progress.updated_at * 1000).toLocaleTimeString()} · file
            started {new Date(progress.started_at * 1000).toLocaleTimeString()}
          </small>
        </>
      )}
      {state === "queued" && <span>Queued · waiting for the worker</span>}
    </div>
  );
}
