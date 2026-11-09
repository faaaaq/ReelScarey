import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { Ban, Trash2 } from "lucide-react";
import { Link } from "react-router-dom";
import { api, messageFor, type ReleaseBlacklistEntry } from "../api";
import { PageHeading, RequestState } from "../components";
import {
  Alert,
  EmptyState,
  Label,
  Modal,
  ModalClose,
  PageBody,
  Table,
  formatTimestamp,
} from "../ui";
import { useResource } from "../useResource";
import "./Admin.css";

function protocolLabel(protocol: ReleaseBlacklistEntry["protocol"]) {
  return protocol === "usenet" ? "Usenet" : "Torrent";
}

export function ReleaseBlacklist() {
  const resource = useResource<{ items: ReleaseBlacklistEntry[] }>(
    "/releases/blacklist",
  );
  const [removing, setRemoving] = useState<ReleaseBlacklistEntry>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function remove() {
    if (!removing || busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`/releases/blacklist/${removing.id}`, { method: "DELETE" });
      setNotice(
        `Removed “${removing.title}” from the blacklist. Automatic searches may try it again.`,
      );
      setRemoving(undefined);
      resource.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  const items = resource.data?.items ?? [];
  return (
    <>
      <PageBody>
        <PageHeading
          title="Release blacklist"
          description="Releases blocked automatically after a terminal download or import failure. ReelScarey skips these entries in automatic searches until they expire or you remove them."
        />
        <RequestState
          loading={resource.loading}
          error={resource.error}
          retry={resource.reload}
        />
        {notice && <Alert kind="success">{notice}</Alert>}
        {resource.data && !items.length && (
          <EmptyState icon={Ban} title="No blocked releases">
            Releases are blocked automatically after terminal failures, such as
            a failed import or a download the client reports as failed. Blocked
            releases appear here and can be removed when the underlying problem
            is fixed. Nothing is blocked right now.
          </EmptyState>
        )}
        {items.length > 0 && (
          <>
            <p className="rs-muted rs-small">
              {items.length === 1
                ? "1 blacklist entry."
                : `${items.length} blacklist entries.`}{" "}
              Entries whose expiry has passed no longer block releases; expired
              rows stay listed until removed.
            </p>
            <Table label="Blocked releases">
              <thead>
                <tr>
                  <th scope="col">Title</th>
                  <th scope="col">Protocol</th>
                  <th scope="col">Indexer</th>
                  <th scope="col">Reason</th>
                  <th scope="col">Added</th>
                  <th scope="col">Expires</th>
                  <th scope="col">Actions</th>
                </tr>
              </thead>
              <tbody>
                {items.map((entry) => (
                  <tr key={entry.id}>
                    <td className="blk-title">{entry.title}</td>
                    <td>
                      <Label
                        kind={entry.protocol === "usenet" ? "info" : "default"}
                      >
                        {protocolLabel(entry.protocol)}
                      </Label>
                    </td>
                    <td>{entry.indexer || "Unknown indexer"}</td>
                    <td className="blk-reason">{entry.reason}</td>
                    <td className="blk-time">
                      {formatTimestamp(entry.created_at)}
                    </td>
                    <td className="blk-time">
                      {entry.expires_at === 0
                        ? "Permanent"
                        : entry.expires_at * 1000 <= Date.now()
                          ? `${formatTimestamp(entry.expires_at)} (expired)`
                          : formatTimestamp(entry.expires_at)}
                    </td>
                    <td className="blk-actions">
                      <Button
                        size="1"
                        variant="soft"
                        color="red"
                        onClick={() => {
                          setError("");
                          setNotice("");
                          setRemoving(entry);
                        }}
                      >
                        <Trash2 size={14} aria-hidden="true" />
                        Remove
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
            <p className="rs-muted rs-small">
              Removing an entry does not retry the download. Use{" "}
              <Link to="/releases">Releases</Link> to search again when you
              choose to.
            </p>
          </>
        )}
      </PageBody>
      <Modal
        open={!!removing}
        onOpenChange={(open) => {
          if (!open && !busy) setRemoving(undefined);
        }}
        title="Remove this release from the blacklist?"
        description={
          removing
            ? `${removing.title} (${protocolLabel(removing.protocol)}${
                removing.indexer ? `, ${removing.indexer}` : ""
              })`
            : undefined
        }
        footer={
          <>
            <ModalClose>
              <Button variant="soft" color="gray" disabled={busy}>
                Cancel
              </Button>
            </ModalClose>
            <Button color="red" disabled={busy} onClick={() => void remove()}>
              {busy ? "Removing…" : "Remove entry"}
            </Button>
          </>
        }
      >
        <p>
          This only clears the block. It does not start a new download and does
          not change files already imported into the library.
        </p>
        {error && (
          <Alert kind="danger" role="alert">
            {error}
          </Alert>
        )}
      </Modal>
    </>
  );
}
