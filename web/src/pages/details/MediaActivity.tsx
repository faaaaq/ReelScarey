import { Link } from "react-router-dom";
import { EventTableRow, type Event } from "../../components/EventRow";
import { RequestState } from "../../components";
import { FieldSet, Label, Table } from "../../ui";
import { useResource } from "../../useResource";

type CandidateDecision = {
  id: string;
  title: string;
  scope: string;
  published_at: string;
  state: string;
  download_id?: string;
};

type MediaActivityResponse = {
  items: Event[];
  candidates?: CandidateDecision[];
  next?: string | null;
};

export function MediaActivity({ mediaID }: { mediaID: number }) {
  const resource = useResource<MediaActivityResponse>(
    `/events?media_id=${mediaID}`,
  );
  const candidates = resource.data?.candidates ?? [];
  const events = resource.data?.items ?? [];
  return (
    <FieldSet
      legend="Activity"
      description="Saved candidate decisions and recorded download, verification, import, edit and refresh outcomes for this title."
    >
      <RequestState
        loading={resource.loading}
        error={resource.error}
        code={resource.code}
        retry={resource.reload}
      />
      {candidates.length > 0 && (
        <Table label="Candidate decisions">
          <thead>
            <tr>
              <th scope="col">Candidate</th>
              <th scope="col">Decision</th>
              <th scope="col">Recovery</th>
            </tr>
          </thead>
          <tbody>
            {candidates.map((candidate) => (
              <tr key={candidate.id}>
                <td>{candidate.title}</td>
                <td>
                  <Label kind={candidate.download_id ? "info" : "default"}>
                    {candidate.state}
                  </Label>
                </td>
                <td>
                  {candidate.download_id ? (
                    <Link
                      to={`/activity?job=${encodeURIComponent(candidate.download_id)}`}
                    >
                      Open download
                    </Link>
                  ) : (
                    <span className="rs-muted">No download to resume</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {events.length > 0 ? (
        <Table label="Title activity">
          <thead>
            <tr>
              <th scope="col" className="is-shrink">
                <span className="sr-only">Icon</span>
              </th>
              <th scope="col">Event</th>
              <th scope="col">Title</th>
              <th scope="col">State</th>
              <th scope="col">Date</th>
            </tr>
          </thead>
          <tbody>
            {events.map((event) => (
              <EventTableRow key={event.id} event={event} />
            ))}
          </tbody>
        </Table>
      ) : (
        !resource.loading &&
        !resource.error && (
          <p className="rs-muted">
            No activity events are recorded for this title yet. A missing search
            or refresh is not presented as a successful outcome.
          </p>
        )
      )}
    </FieldSet>
  );
}
