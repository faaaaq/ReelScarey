import { Link } from "react-router-dom";
import { HardDrive, TriangleAlert } from "lucide-react";
import { useResource } from "../useResource";
import { PageHeading, RequestState } from "../components";
import { Alert, EmptyState, PageBody, Panel, Table, formatBytes } from "../ui";
import type {
  StorageInsightFile,
  StorageInsights as StorageInsightsData,
} from "../api";
import "./Admin.css";

function FileTable({
  items,
  label,
}: {
  items: StorageInsightFile[];
  label: string;
}) {
  return (
    <Table label={label}>
      <thead>
        <tr>
          <th>Title</th>
          <th>Kind</th>
          <th>Path</th>
          <th>Size</th>
          <th>Quality</th>
          <th>Profile cutoff</th>
        </tr>
      </thead>
      <tbody>
        {items.map((item) => (
          <tr key={item.id}>
            <td>{item.title}</td>
            <td>{item.kind}</td>
            <td>
              <code>{item.relative_path}</code>
            </td>
            <td>{formatBytes(item.size_bytes)}</td>
            <td>{item.quality ? `${item.quality}p` : "Not analyzed"}</td>
            <td>{item.cutoff ? `${item.cutoff}p` : "None"}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

export function StorageInsights() {
  const resource = useResource<StorageInsightsData>("/storage/insights");
  const data = resource.data;
  return (
    <PageBody>
      <PageHeading
        title="Storage insights"
        description="A read-only summary of registered files, duplicate groups, unresolved scans and files below their profile cutoff. It never scans or deletes media."
      />
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {resource.error && (
        <Alert kind="danger">
          Storage insights could not be read. The storage browser and duplicate
          review remain available.
        </Alert>
      )}
      {data && (
        <>
          <Panel title="Summary">
            <div className="storage-insight-grid">
              <div>
                <span className="rs-muted rs-small">Registered files</span>
                <strong>{data.summary.registered_files}</strong>
              </div>
              <div>
                <span className="rs-muted rs-small">Registered size</span>
                <strong>{formatBytes(data.summary.registered_bytes)}</strong>
              </div>
              <div>
                <span className="rs-muted rs-small">Duplicate groups</span>
                <strong>{data.summary.duplicate_groups}</strong>
              </div>
              <div>
                <span className="rs-muted rs-small">Below profile cutoff</span>
                <strong>{data.summary.below_cutoff}</strong>
              </div>
              <div>
                <span className="rs-muted rs-small">Unresolved scan files</span>
                <strong>{data.summary.unresolved}</strong>
              </div>
            </div>
            <p className="rs-small">
              <Link to="/duplicates">Review duplicates</Link> ·{" "}
              <Link to="/storage">Browse storage</Link> ·{" "}
              <Link to="/import">Review library imports</Link>
            </p>
          </Panel>
          <Panel title="Largest registered files">
            {data.largest.length === 0 ? (
              <p className="rs-muted">No registered files.</p>
            ) : (
              <FileTable
                items={data.largest}
                label="Largest registered files"
              />
            )}
          </Panel>
          <Panel title="Below the assigned quality cutoff">
            {data.below_cutoff.length === 0 ? (
              <p className="rs-muted">
                No analyzed file is below its assigned profile cutoff. Missing
                analysis is not treated as a quality result.
              </p>
            ) : (
              <>
                <Alert kind="warning">
                  These are suggestions only. ReelScarey does not replace or
                  delete registered files automatically.
                </Alert>
                <FileTable
                  items={data.below_cutoff}
                  label="Files below the assigned quality cutoff"
                />
              </>
            )}
          </Panel>
          {data.summary.registered_files === 0 &&
            data.summary.unresolved === 0 && (
              <EmptyState icon={HardDrive} title="No registered files">
                Import or register media to populate storage insights. The page
                reads saved facts only; it does not scan disks.
              </EmptyState>
            )}
          {data.summary.unresolved > 0 && (
            <Alert kind="info">
              <TriangleAlert size={16} aria-hidden="true" /> Some library scan
              files are unresolved or awaiting review. Open Library Import to
              review them.
            </Alert>
          )}
        </>
      )}
    </PageBody>
  );
}
