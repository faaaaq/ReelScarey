import { useMemo, useState, type FormEvent } from "react";
import { Badge, Button, Dialog, TextField } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { useLiveResource } from "../useLiveResource";

type Torrent = {
  hash: string;
  name: string;
  state: string;
  save_path: string;
  progress: number;
  amount_left: number;
  total_size: number;
  completed: number;
  downloaded: number;
  uploaded: number;
  dlspeed: number;
  upspeed: number;
  eta: number;
  ratio: number;
  num_seeds: number;
  num_leechs: number;
  added_on: number;
  completion_on: number;
  tags: string;
  category: string;
  content_path: string;
};
type Overview = {
  available: boolean;
  state: string;
  message: string;
  transfer: {
    dl_info_speed: number;
    up_info_speed: number;
    dl_info_data: number;
    up_info_data: number;
    connection_status: string;
  };
  items: Torrent[];
};
type Detail = {
  torrent: Torrent;
  files: {
    index: number;
    name: string;
    size: number;
    progress: number;
    priority: number;
  }[];
};
type Filter =
  "all" | "downloading" | "seeding" | "paused" | "completed" | "error";

function bytes(value: number) {
  if (!value) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let v = value;
  let unit = 0;
  while (v >= 1024 && unit < units.length - 1) {
    v /= 1024;
    unit++;
  }
  return `${v.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}
function rate(value: number) {
  return value > 0 ? `${bytes(value)}/s` : "—";
}
function eta(seconds: number) {
  if (!seconds || seconds >= 8640000) return "—";
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m ${s}s`;
  return `${s}s`;
}
function stateLabel(state: string) {
  const labels: Record<string, string> = {
    downloading: "Downloading",
    metaDL: "Fetching metadata",
    allocating: "Allocating",
    stalledDL: "Stalled",
    queuedDL: "Queued",
    checkingDL: "Checking",
    forcedDL: "Downloading",
    uploading: "Seeding",
    stalledUP: "Seeding",
    queuedUP: "Queued",
    checkingUP: "Checking",
    forcedUP: "Seeding",
    pausedDL: "Paused",
    pausedUP: "Paused",
    checkingResumeData: "Checking",
    moving: "Moving",
    missingFiles: "Missing files",
    error: "Error",
    unknown: "Unknown",
  };
  return labels[state] ?? state;
}
function isDownloading(t: Torrent) {
  return [
    "downloading",
    "metaDL",
    "allocating",
    "stalledDL",
    "queuedDL",
    "checkingDL",
    "forcedDL",
  ].includes(t.state);
}
function isSeeding(t: Torrent) {
  return [
    "uploading",
    "stalledUP",
    "queuedUP",
    "checkingUP",
    "forcedUP",
  ].includes(t.state);
}
function isPaused(t: Torrent) {
  return t.state === "pausedDL" || t.state === "pausedUP";
}
function isError(t: Torrent) {
  return t.state === "error" || t.state === "missingFiles";
}
function managed(t: Torrent) {
  return t.tags
    .split(",")
    .map((tag) => tag.trim())
    .some(
      (tag) =>
        tag === "reelscarey" ||
        tag.startsWith("reelscarey-") ||
        tag.startsWith("reelscarey:"),
    );
}

export function Torrents() {
  const overview = useLiveResource<Overview>("/torrents", 3000);
  const [magnet, setMagnet] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const items = overview.data?.items ?? [];
  const counts = useMemo(
    () => ({
      all: items.length,
      downloading: items.filter(isDownloading).length,
      seeding: items.filter(isSeeding).length,
      paused: items.filter(isPaused).length,
      completed: items.filter((t) => t.progress >= 1 && !isSeeding(t)).length,
      error: items.filter(isError).length,
    }),
    [items],
  );
  const visible = useMemo(() => {
    const term = query.trim().toLowerCase();
    return items
      .filter((t) => {
        if (filter === "downloading") return isDownloading(t);
        if (filter === "seeding") return isSeeding(t);
        if (filter === "paused") return isPaused(t);
        if (filter === "error") return isError(t);
        if (filter === "completed") return t.progress >= 1 && !isSeeding(t);
        return true;
      })
      .filter((t) => !term || t.name.toLowerCase().includes(term))
      .sort((a, b) => b.added_on - a.added_on);
  }, [items, filter, query]);

  async function run(
    operation: string,
    task: () => Promise<unknown>,
    done?: string,
  ) {
    if (busy) return;
    setBusy(operation);
    setError("");
    setNotice("");
    try {
      await task();
      if (done) setNotice(done);
      overview.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }
  function addMagnet(e: FormEvent) {
    e.preventDefault();
    if (!magnet.trim()) return;
    void run(
      "add",
      () =>
        api("/torrents", {
          method: "POST",
          body: JSON.stringify({ magnet: magnet.trim() }),
        }),
      "Magnet added.",
    );
    setMagnet("");
  }
  async function addFile(file: File | undefined) {
    if (!file) return;
    const body = await file.arrayBuffer();
    void run(
      "upload",
      () =>
        api("/torrents", {
          method: "POST",
          headers: { "Content-Type": "application/x-bittorrent" },
          body,
        }),
      "Torrent added.",
    );
  }
  function action(torrent: Torrent, kind: "pause" | "resume" | "recheck") {
    void run(`${kind}-${torrent.hash}`, () =>
      api(`/torrents/${torrent.hash}/${kind}`, {
        method: "POST",
        body: "{}",
      }),
    );
  }

  if (overview.data && !overview.data.available) {
    return (
      <>
        <PageHeading title="Torrents" />
        <section className="panel empty-state">
          <h2>The built-in client is not enabled</h2>
          <p>{overview.data.message}</p>
          <Button asChild>
            <Link to="/download-clients">Open download clients</Link>
          </Button>
        </section>
      </>
    );
  }

  return (
    <>
      <PageHeading title="Torrents">
        <Button
          variant="soft"
          onClick={overview.reload}
          disabled={overview.loading}
        >
          Refresh
        </Button>
      </PageHeading>
      <RequestState
        loading={!overview.data && overview.loading}
        error={overview.error}
        retry={overview.reload}
      />
      {overview.data && (
        <>
          <section className="torrent-summary" aria-label="Transfer rates">
            <div>
              <span>Download</span>
              <strong>{rate(overview.data.transfer.dl_info_speed)}</strong>
              <small>{bytes(overview.data.transfer.dl_info_data)} total</small>
            </div>
            <div>
              <span>Upload</span>
              <strong>{rate(overview.data.transfer.up_info_speed)}</strong>
              <small>{bytes(overview.data.transfer.up_info_data)} total</small>
            </div>
            <div>
              <span>Torrents</span>
              <strong>{counts.downloading + counts.seeding}</strong>
              <small>{counts.all} in client</small>
            </div>
            <div>
              <span>Connection</span>
              <strong>
                <Badge
                  color={overview.data.state === "running" ? "green" : "red"}
                >
                  {overview.data.state === "running" ? "Online" : "Offline"}
                </Badge>
              </strong>
              <small>
                {overview.data.transfer.connection_status || "unknown"}
              </small>
            </div>
          </section>

          <section className="panel torrent-add" aria-label="Add torrent">
            <form onSubmit={addMagnet}>
              <TextField.Root
                value={magnet}
                onChange={(e) => setMagnet(e.target.value)}
                placeholder="magnet:?xt=urn:btih:…"
                aria-label="Magnet link"
              />
              <Button type="submit" disabled={!!busy || !magnet.trim()}>
                {busy === "add" ? "Adding…" : "Add magnet"}
              </Button>
            </form>
            <label className="file-button">
              <input
                type="file"
                accept=".torrent,application/x-bittorrent"
                onChange={(e) => {
                  void addFile(e.target.files?.[0]);
                  e.target.value = "";
                }}
              />
              {busy === "upload" ? "Uploading…" : "Upload .torrent"}
            </label>
          </section>

          {error && (
            <p role="alert" className="error-box">
              {error}
            </p>
          )}
          {notice && (
            <p role="status" className="save-success">
              {notice}
            </p>
          )}

          <div className="torrent-toolbar">
            <div
              className="torrent-filters"
              role="group"
              aria-label="Torrent filters"
            >
              {(
                [
                  "all",
                  "downloading",
                  "seeding",
                  "completed",
                  "paused",
                  "error",
                ] as Filter[]
              ).map((key) => (
                <button
                  key={key}
                  type="button"
                  aria-pressed={filter === key}
                  onClick={() => setFilter(key)}
                >
                  {key[0].toUpperCase() + key.slice(1)}
                  <span>{counts[key]}</span>
                </button>
              ))}
            </div>
            <TextField.Root
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter torrents"
              aria-label="Filter torrents"
            />
          </div>

          {visible.length === 0 ? (
            <p className="torrent-empty">
              {items.length === 0
                ? "No torrents in the built-in client."
                : "No torrents match this filter."}
            </p>
          ) : (
            <ul className="torrent-list" aria-label="Torrents">
              {visible.map((torrent) => (
                <TorrentRow
                  key={torrent.hash}
                  torrent={torrent}
                  busy={busy}
                  onAction={action}
                  onRemove={(deleteFiles) =>
                    run(
                      `remove-${torrent.hash}`,
                      () =>
                        api(`/torrents/${torrent.hash}/remove`, {
                          method: "POST",
                          body: JSON.stringify({
                            delete_files: deleteFiles,
                            reviewed: true,
                          }),
                        }),
                      deleteFiles
                        ? "Torrent and data removed."
                        : "Torrent removed.",
                    )
                  }
                />
              ))}
            </ul>
          )}
        </>
      )}
    </>
  );
}

function TorrentRow({
  torrent,
  busy,
  onAction,
  onRemove,
}: {
  torrent: Torrent;
  busy: string;
  onAction: (torrent: Torrent, kind: "pause" | "resume" | "recheck") => void;
  onRemove: (deleteFiles: boolean) => void;
}) {
  const [confirm, setConfirm] = useState(false);
  const [deleteFiles, setDeleteFiles] = useState(false);
  const active = isDownloading(torrent) || isSeeding(torrent);
  const percent = Math.min(
    100,
    Math.max(0, Math.round(torrent.progress * 100)),
  );
  return (
    <li className="torrent-row">
      <div className="torrent-name">
        <div>
          <strong title={torrent.name}>{torrent.name}</strong>
          <span>
            {bytes(torrent.total_size)} · {stateLabel(torrent.state)}
            {torrent.category ? ` · ${torrent.category}` : ""}
            {managed(torrent) ? " · ReelScarey" : ""}
          </span>
        </div>
        <div className="torrent-row-actions">
          {active ? (
            <Button
              size="1"
              variant="soft"
              disabled={!!busy}
              onClick={() => onAction(torrent, "pause")}
            >
              Pause
            </Button>
          ) : (
            <Button
              size="1"
              variant="soft"
              disabled={!!busy}
              onClick={() => onAction(torrent, "resume")}
            >
              Resume
            </Button>
          )}
          <TorrentDetails hash={torrent.hash} />
          <Dialog.Root open={confirm} onOpenChange={setConfirm}>
            <Dialog.Trigger>
              <Button size="1" variant="soft" color="red" disabled={!!busy}>
                Remove
              </Button>
            </Dialog.Trigger>
            <Dialog.Content maxWidth="440px">
              <Dialog.Title>Remove torrent</Dialog.Title>
              <Dialog.Description size="2">{torrent.name}</Dialog.Description>
              <label className="check-label">
                <input
                  type="checkbox"
                  checked={deleteFiles}
                  onChange={(e) => setDeleteFiles(e.target.checked)}
                />{" "}
                Delete downloaded data
              </label>
              <p className="field-help">
                {managed(torrent)
                  ? "This torrent was added by ReelScarey. Deleting its data can break an import or seeding source."
                  : "Removing the torrent keeps its data unless deletion is checked."}
              </p>
              <div className="import-actions">
                <Button
                  color="red"
                  disabled={!!busy}
                  onClick={() => {
                    onRemove(deleteFiles);
                    setConfirm(false);
                  }}
                >
                  Remove torrent
                </Button>
                <Dialog.Close>
                  <Button variant="soft">Cancel</Button>
                </Dialog.Close>
              </div>
            </Dialog.Content>
          </Dialog.Root>
        </div>
      </div>
      <div className="torrent-progress">
        <progress
          value={torrent.progress}
          max={1}
          aria-label={`Progress for ${torrent.name}`}
        />
        <span>
          {percent}% of {bytes(torrent.total_size)}
          {torrent.amount_left > 0
            ? ` · ${bytes(torrent.amount_left)} left`
            : ""}
        </span>
      </div>
      <div className="torrent-metrics">
        <span title="Download speed">{rate(torrent.dlspeed)}</span>
        <span title="Upload speed">{rate(torrent.upspeed)}</span>
        <span title="ETA">{eta(torrent.eta)}</span>
        <span title="Seeds and peers">
          {torrent.num_seeds} / {torrent.num_leechs}
        </span>
        <span title="Ratio">{torrent.ratio.toFixed(2)}</span>
      </div>
    </li>
  );
}

function TorrentDetails({ hash }: { hash: string }) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Trigger>
        <Button size="1" variant="soft">
          Details
        </Button>
      </Dialog.Trigger>
      <Dialog.Content maxWidth="620px">
        <Dialog.Title>Torrent details</Dialog.Title>
        <Dialog.Description size="1">Files and save path.</Dialog.Description>
        {open && <TorrentDetailsBody hash={hash} />}
        <Dialog.Close>
          <Button variant="soft">Close</Button>
        </Dialog.Close>
      </Dialog.Content>
    </Dialog.Root>
  );
}

function TorrentDetailsBody({ hash }: { hash: string }) {
  const detail = useLiveResource<Detail>(`/torrents/${hash}`, 0);
  return (
    <>
      <RequestState
        loading={!detail.data && detail.loading}
        error={detail.error}
        retry={detail.reload}
      />
      {detail.data && (
        <>
          <p className="field-help">
            Save path: {detail.data.torrent.save_path}
          </p>
          <ul className="torrent-files">
            {detail.data.files.map((file) => (
              <li key={file.index}>
                <span title={file.name}>{file.name}</span>
                <span>{bytes(file.size)}</span>
                <span>{Math.round(file.progress * 100)}%</span>
              </li>
            ))}
          </ul>
        </>
      )}
    </>
  );
}
