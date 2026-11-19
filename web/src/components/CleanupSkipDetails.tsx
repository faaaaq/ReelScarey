import { Link } from "react-router-dom";
export type CleanupSkipAttempt = {
  file_id: number;
  path: string;
  message: string;
  occurred_at: number;
  resolved_by?: string;
  resolved_at?: number;
};
export type CleanupSkipReport = {
  skipped: number;
  skip_attempts?: number;
  recovered?: number;
  untracked_skips?: number;
  skip_history?: CleanupSkipAttempt[];
  issues?: string[];
};
export function CleanupSkipDetails({
  cleanup,
}: {
  cleanup: CleanupSkipReport;
}) {
  const history = cleanup.skip_history ?? [];
  const unresolved = [
    ...new Map(
      history
        .filter((item) => !item.resolved_by)
        .map((item) => [item.path, item]),
    ).values(),
  ];
  const attempts = cleanup.skip_attempts ?? cleanup.skipped;
  if (!attempts && !history.length) return null;
  return (
    <div className="cleanup-skip-details rs-stack">
      {cleanup.skipped > 0 && (
        <details>
          <summary>Unresolved skips ({cleanup.skipped})</summary>
          <ul>
            {history.length
              ? unresolved.map((item) => (
                  <li key={item.path}>
                    <code>{item.path}</code>
                    <p>{item.message}</p>
                  </li>
                ))
              : cleanup.issues?.map((issue, index) => (
                  <li key={index}>{issue}</li>
                ))}
          </ul>
          {!!cleanup.untracked_skips && (
            <p>
              {cleanup.untracked_skips} older attempts have no individual
              diagnostic and remain unresolved.
            </p>
          )}
        </details>
      )}
      <details>
        <summary>Historical failed attempts ({attempts})</summary>
        <ul>
          {history.length
            ? history.map((item, index) => (
                <li key={index}>
                  <code>{item.path}</code>
                  <p>{item.message}</p>
                  <small>
                    {item.occurred_at > 0 && (
                      <>
                        Failed {new Date(item.occurred_at).toLocaleString()}{" "}
                        ·{" "}
                      </>
                    )}
                    {item.resolved_by ? (
                      <>
                        Resolved{" "}
                        {item.resolved_at &&
                          new Date(item.resolved_at).toLocaleString()}{" "}
                        ·{" "}
                        <Link
                          to={`/organization?view=jobs&plan=${encodeURIComponent(item.resolved_by)}`}
                        >
                          View successful retry
                        </Link>
                      </>
                    ) : (
                      "Unresolved"
                    )}
                  </small>
                </li>
              ))
            : cleanup.issues?.map((issue, index) => (
                <li key={index}>{issue}</li>
              ))}
        </ul>
      </details>
    </div>
  );
}
