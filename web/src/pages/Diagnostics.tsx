import { Link } from "react-router-dom";
import { AlertTriangle } from "lucide-react";
import { Button } from "@radix-ui/themes";
import { useResource } from "../useResource";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  EmptyState,
  PageBody,
  Panel,
  Table,
  formatTimestamp,
} from "../ui";
import type { Diagnostics as DiagnosticsData } from "../api";
import "./Admin.css";

export function Diagnostics() {
  const resource = useResource<DiagnosticsData>("/diagnostics");
  const data = resource.data;
  return (
    <PageBody>
      <PageHeading
        title="Diagnostics"
        description="A read-only view of already recorded failures, blocked releases and indexer errors. Nothing on this page retries, rescans or changes your library."
      />
      <RequestState
        loading={resource.loading}
        error={resource.error}
        code={resource.code}
        retry={resource.reload}
      />
      {resource.error && (
        <Alert kind="danger">
          Diagnostics could not be read. Other pages continue to work.
        </Alert>
      )}
      {data && (
        <>
          <Panel title="Failed or blocked downloads">
            {data.downloads.length === 0 ? (
              <p className="rs-muted">No failed or blocked download records.</p>
            ) : (
              <Table label="Failed or blocked downloads">
                <thead>
                  <tr>
                    <th>Media</th>
                    <th>Release</th>
                    <th>State</th>
                    <th>Error</th>
                    <th>Updated</th>
                    <th aria-label="Actions" />
                  </tr>
                </thead>
                <tbody>
                  {data.downloads.map((item) => (
                    <tr key={item.id}>
                      <td>{item.media}</td>
                      <td>{item.title}</td>
                      <td>{item.state}</td>
                      <td>
                        {item.error_code ? (
                          <code>{item.error_code}</code>
                        ) : null}{" "}
                        {item.error}
                      </td>
                      <td>{formatTimestamp(item.updated_at)}</td>
                      <td>
                        <Button asChild size="1" variant="soft">
                          <Link
                            to={`/activity?download=${encodeURIComponent(item.id)}`}
                          >
                            Open activity
                          </Link>
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </Panel>
          <Panel title="Release blacklist">
            {data.blacklist.length === 0 ? (
              <p className="rs-muted">No blacklisted releases.</p>
            ) : (
              <Table label="Blacklisted releases">
                <thead>
                  <tr>
                    <th>Title</th>
                    <th>Indexer</th>
                    <th>Reason</th>
                    <th>Added</th>
                  </tr>
                </thead>
                <tbody>
                  {data.blacklist.map((item) => (
                    <tr key={item.id}>
                      <td>{item.title}</td>
                      <td>{item.indexer || "Unknown"}</td>
                      <td>{item.reason}</td>
                      <td>{formatTimestamp(item.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            <p className="rs-small">
              <Link to="/releases/blacklist">Manage the release blacklist</Link>
            </p>
          </Panel>
          <Panel title="Indexer errors">
            {data.indexers.length === 0 ? (
              <p className="rs-muted">No saved indexer errors.</p>
            ) : (
              <Table label="Indexer errors">
                <thead>
                  <tr>
                    <th>Indexer</th>
                    <th>Error</th>
                    <th>Last test</th>
                  </tr>
                </thead>
                <tbody>
                  {data.indexers.map((item) => (
                    <tr key={item.id}>
                      <td>{item.name}</td>
                      <td>{item.last_error}</td>
                      <td>
                        {item.last_test_at
                          ? formatTimestamp(item.last_test_at)
                          : "Never"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </Panel>
          <Panel title="Automation runs needing attention">
            {data.automation_runs.length === 0 ? (
              <p className="rs-muted">
                No unfinished or failed automation runs.
              </p>
            ) : (
              <Table label="Automation runs">
                <thead>
                  <tr>
                    <th>State</th>
                    <th>Message</th>
                    <th>Started</th>
                  </tr>
                </thead>
                <tbody>
                  {data.automation_runs.map((item) => (
                    <tr key={item.id}>
                      <td>{item.state}</td>
                      <td>{item.message}</td>
                      <td>{formatTimestamp(item.started_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </Panel>
          <Panel title="Recent failed events">
            {data.events.length === 0 ? (
              <p className="rs-muted">
                No failed, blocked or uncertain recorded events.
              </p>
            ) : (
              <Table label="Recent failed events">
                <thead>
                  <tr>
                    <th>When</th>
                    <th>Kind</th>
                    <th>Title</th>
                    <th>State</th>
                  </tr>
                </thead>
                <tbody>
                  {data.events.map((item) => (
                    <tr key={item.id}>
                      <td>{formatTimestamp(item.occurred_at)}</td>
                      <td>{item.kind}</td>
                      <td>{item.title}</td>
                      <td>{item.state}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            <p className="rs-small">
              <Link to="/timeline">Open the full timeline</Link>
            </p>
          </Panel>
          {data.wal && (
            <Panel title="Database write-ahead log">
              <p className="rs-muted">
                Read-only counters for the SQLite write-ahead log. Sustained
                growth across restarts would be worth investigating; the file
                normally returns to zero after checkpoints.
              </p>
              <Table label="Database write-ahead log">
                <thead>
                  <tr>
                    <th>Counter</th>
                    <th>Value</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td>WAL file size</td>
                    <td>{data.wal.wal_file_bytes.toLocaleString()} bytes</td>
                  </tr>
                  <tr>
                    <td>Database pages</td>
                    <td>{data.wal.page_count.toLocaleString()}</td>
                  </tr>
                  <tr>
                    <td>Unused pages</td>
                    <td>{data.wal.freelist_pages.toLocaleString()}</td>
                  </tr>
                  <tr>
                    <td>Page size</td>
                    <td>{data.wal.page_size.toLocaleString()} bytes</td>
                  </tr>
                  <tr>
                    <td>Autocheckpoint threshold</td>
                    <td>
                      {data.wal.autocheckpoint_pages.toLocaleString()} pages
                    </td>
                  </tr>
                </tbody>
              </Table>
            </Panel>
          )}
          {data.counts.downloads +
            data.counts.blacklist +
            data.counts.indexers +
            data.counts.automation_runs +
            data.counts.events ===
            0 && (
            <EmptyState icon={AlertTriangle} title="No recorded problems">
              ReelScarey has not recorded a failed download, blocked release,
              indexer error or failed event. This is not a live health check.
            </EmptyState>
          )}
        </>
      )}
    </PageBody>
  );
}
