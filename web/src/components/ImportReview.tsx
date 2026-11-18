import "./ImportReview.css";

export type ImportReviewCountProps = {
  ready: number;
  needsDecision: number;
  skipped: number;
};

/** The same review vocabulary is used by folder, music and selected-source imports. */
export function ImportReviewSummary({
  ready,
  needsDecision,
  skipped,
}: ImportReviewCountProps) {
  return (
    <dl
      className="import-review-counts"
      role="group"
      aria-label="Import review counts"
    >
      <div data-tone="ready">
        <dt>Ready</dt>
        <dd>{ready}</dd>
      </div>
      <div data-tone={needsDecision > 0 ? "attention" : "quiet"}>
        <dt>Needs decision</dt>
        <dd>{needsDecision}</dd>
      </div>
      <div data-tone="quiet">
        <dt>Skipped</dt>
        <dd>{skipped}</dd>
      </div>
    </dl>
  );
}

export type ImportReviewFactProps = {
  identity: string;
  destination: string;
  operation: string;
  evidence: string[];
};

/** Read-only facts. Actions remain owned by the source's durable review flow. */
export function ImportReviewFacts({
  identity,
  destination,
  operation,
  evidence,
}: ImportReviewFactProps) {
  return (
    <div className="import-review-facts">
      <dl>
        <div>
          <dt>Proposed identity</dt>
          <dd>{identity || "Not confirmed"}</dd>
        </div>
        <div>
          <dt>Destination</dt>
          <dd>
            <code>{destination || "Not planned"}</code>
          </dd>
        </div>
        <div>
          <dt>Planned operation</dt>
          <dd>{operation}</dd>
        </div>
      </dl>
      <div className="import-review-evidence">
        <strong>Match evidence</strong>
        {evidence.length > 0 ? (
          <ul>
            {evidence.map((item, index) => (
              <li key={`${index}-${item}`}>{item}</li>
            ))}
          </ul>
        ) : (
          <p>No match evidence is available; review the identity explicitly.</p>
        )}
      </div>
    </div>
  );
}
