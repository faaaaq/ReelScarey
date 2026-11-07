import { useMemo, useState, type ReactNode } from "react";
import { Tabs } from "@radix-ui/themes";
import { ChevronDown, ChevronRight, File, Folder } from "lucide-react";
import { messageFor } from "../../api";
import {
  Alert,
  formatBytes,
  formatDuration,
  formatSpeed,
  formatTimestamp,
} from "../../ui";
import { useLiveResource } from "../../useLiveResource";
import {
  formatPercent,
  formatRatio,
  infinityEta,
  originOf,
  readStored,
  writeStored,
  type Detail,
  type Torrent,
  type TorrentFile,
} from "./model";
import { postNoContent } from "./request";

type Tab = "general" | "trackers" | "peers" | "http" | "content";
const tabKey = "reelscarey.torrents.panel.tab";

export function PropertiesPanel({ torrent }: { torrent: Torrent | null }) {
  const [tab, setTab] = useState<Tab>(() => readStored<Tab>(tabKey, "general"));
  function change(next: string) {
    setTab(next as Tab);
    writeStored(tabKey, next);
  }
  return (
    <Tabs.Root value={tab} onValueChange={change} className="tor-props">
      <Tabs.List
        size="1"
        className="tor-props-tabs"
        aria-label="Torrent properties"
      >
        <Tabs.Trigger value="general">General</Tabs.Trigger>
        <Tabs.Trigger value="trackers">Trackers</Tabs.Trigger>
        <Tabs.Trigger value="peers">Peers</Tabs.Trigger>
        <Tabs.Trigger value="http">HTTP Sources</Tabs.Trigger>
        <Tabs.Trigger value="content">Content</Tabs.Trigger>
      </Tabs.List>
      <div className="tor-props-body">
        {torrent ? (
          <DetailTabs key={torrent.hash} torrent={torrent} />
        ) : (
          <p className="tor-props-hint">
            Select one torrent to see its properties.
          </p>
        )}
      </div>
    </Tabs.Root>
  );
}

function DetailTabs({ torrent }: { torrent: Torrent }) {
  const detail = useLiveResource<Detail>(`/torrents/${torrent.hash}`, 2000);
  const data =
    detail.data &&
    detail.data.torrent.hash.toLowerCase() === torrent.hash.toLowerCase()
      ? detail.data
      : undefined;
  if (!data)
    return detail.error ? (
      <div className="tor-props-pad">
        <Alert kind="danger">{detail.error}</Alert>
      </div>
    ) : (
      <p className="tor-props-hint">Loading properties…</p>
    );
  return (
    <>
      {detail.error && (
        <p className="tor-props-stale" role="status">
          Showing the last loaded properties: {detail.error}
        </p>
      )}
      <Tabs.Content value="general" className="tor-props-content">
        <General torrent={torrent} detail={data} />
      </Tabs.Content>
      <Tabs.Content value="trackers" className="tor-props-content">
        <Trackers detail={data} />
      </Tabs.Content>
      <Tabs.Content value="peers" className="tor-props-content">
        <Peers detail={data} />
      </Tabs.Content>
      <Tabs.Content value="http" className="tor-props-content">
        <HttpSources detail={data} />
      </Tabs.Content>
      <Tabs.Content value="content" className="tor-props-content">
        <Content torrent={torrent} detail={data} reload={detail.reload} />
      </Tabs.Content>
    </>
  );
}

function limit(value: number) {
  return value > 0 ? formatSpeed(value) : "∞";
}
function date(value: number) {
  return value > 0 ? formatTimestamp(value) : "—";
}

function PiecesBar({ pieces }: { pieces: number[] }) {
  const background = useMemo(() => {
    if (!pieces.length) return undefined;
    const color = (v: number) =>
      v === 2
        ? "var(--rs-primary)"
        : v === 1
          ? "var(--rs-success)"
          : "transparent";
    const stops: string[] = [];
    let runStart = 0;
    for (let i = 1; i <= pieces.length; i++) {
      if (i === pieces.length || pieces[i] !== pieces[runStart]) {
        const a = (runStart / pieces.length) * 100;
        const b = (i / pieces.length) * 100;
        stops.push(`${color(pieces[runStart])} ${a}% ${b}%`);
        runStart = i;
      }
    }
    return `linear-gradient(to right, ${stops.join(", ")})`;
  }, [pieces]);
  const done = pieces.filter((p) => p === 2).length;
  return (
    <div
      className="tor-pieces"
      role="img"
      aria-label={
        pieces.length
          ? `Pieces: ${done} of ${pieces.length} segments complete`
          : "Pieces not available yet"
      }
      style={background ? { backgroundImage: background } : undefined}
    />
  );
}

function KV({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl className="tor-kv">
      {items.map(([k, v]) => (
        <div key={k} className="tor-kv-item">
          <dt>{k}:</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

function General({ torrent, detail }: { torrent: Torrent; detail: Detail }) {
  const p = detail.properties;
  const t = detail.torrent ?? torrent;
  return (
    <div className="tor-general">
      <div className="tor-general-bars">
        <span className="tor-general-label">Progress:</span>
        <PiecesBar pieces={detail.pieces ?? []} />
        <span className="tor-general-value">{formatPercent(t.progress)}</span>
        <span className="tor-general-label">Availability:</span>
        <div
          className="tor-bar is-availability"
          role="progressbar"
          aria-label="Availability"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.max(0, Math.min(100, t.availability * 100))}
        >
          <div
            className="tor-bar-fill"
            style={{
              width: `${Math.max(0, Math.min(1, t.availability)) * 100}%`,
            }}
          />
        </div>
        <span className="tor-general-value">
          {t.availability < 0 ? "—" : t.availability.toFixed(3)}
        </span>
      </div>
      <section className="tor-general-section" aria-labelledby="tor-transfer-h">
        <h3 id="tor-transfer-h">Transfer</h3>
        <KV
          items={[
            [
              "Time Active",
              `${formatDuration(p.time_elapsed)}${p.seeding_time > 0 ? ` (seeded for ${formatDuration(p.seeding_time)})` : ""}`,
            ],
            ["ETA", p.eta >= infinityEta ? "∞" : formatDuration(p.eta)],
            [
              "Connections",
              `${p.nb_connections} (${p.nb_connections_limit > 0 ? `${p.nb_connections_limit} max` : "∞ max"})`,
            ],
            [
              "Downloaded",
              `${formatBytes(p.total_downloaded)} (${formatBytes(t.downloaded_session)} this session)`,
            ],
            [
              "Uploaded",
              `${formatBytes(p.total_uploaded)} (${formatBytes(t.uploaded_session)} this session)`,
            ],
            ["Seeds", `${p.seeds} (${p.seeds_total} total)`],
            ["Peers", `${p.peers} (${p.peers_total} total)`],
            [
              "Download Speed",
              `${formatSpeed(p.dl_speed, "0 B/s")} (${formatSpeed(p.dl_speed_avg, "0 B/s")} avg.)`,
            ],
            [
              "Upload Speed",
              `${formatSpeed(p.up_speed, "0 B/s")} (${formatSpeed(p.up_speed_avg, "0 B/s")} avg.)`,
            ],
            ["Download Limit", limit(p.dl_limit)],
            ["Upload Limit", limit(p.up_limit)],
            ["Share Ratio", formatRatio(p.share_ratio)],
            ["Reannounce In", formatDuration(p.reannounce)],
            [
              "Last Seen Complete",
              p.last_seen > 0 ? formatTimestamp(p.last_seen) : "Never",
            ],
            ["Wasted", formatBytes(p.total_wasted)],
          ]}
        />
      </section>
      <section className="tor-general-section" aria-labelledby="tor-info-h">
        <h3 id="tor-info-h">Information</h3>
        <KV
          items={[
            ["Total Size", formatBytes(p.total_size)],
            [
              "Pieces",
              p.pieces_num > 0
                ? `${p.pieces_num} × ${formatBytes(p.piece_size)} (have ${p.pieces_have})`
                : "—",
            ],
            ["Created By", p.created_by || "—"],
            ["Added On", date(p.addition_date)],
            ["Completed On", date(p.completion_date)],
            ["Created On", date(p.creation_date)],
            ["Info Hash", <code key="hash">{t.hash}</code>],
            ["Save Path", p.save_path || t.save_path],
            [
              "Comment",
              <span className="tor-comment">{p.comment || "—"}</span>,
            ],
          ]}
        />
      </section>
    </div>
  );
}

const trackerStatus: Record<number, string> = {
  0: "Disabled",
  1: "Not contacted",
  2: "Working",
  3: "Updating",
  4: "Not working",
};

function count(value: number) {
  return value < 0 ? "N/A" : String(value);
}

function Trackers({ detail }: { detail: Detail }) {
  return (
    <div className="tor-props-table-wrap">
      <table className="tor-mini-table" aria-label="Trackers">
        <thead>
          <tr>
            <th scope="col">URL</th>
            <th scope="col" className="is-numeric">
              Tier
            </th>
            <th scope="col">Status</th>
            <th scope="col" className="is-numeric">
              Peers
            </th>
            <th scope="col" className="is-numeric">
              Seeds
            </th>
            <th scope="col" className="is-numeric">
              Leeches
            </th>
            <th scope="col" className="is-numeric">
              Downloaded
            </th>
            <th scope="col">Message</th>
          </tr>
        </thead>
        <tbody>
          {detail.trackers.map((tr, i) => (
            <tr key={`${tr.url}-${i}`}>
              <td>{tr.url}</td>
              <td className="is-numeric">{tr.tier < 0 ? "" : tr.tier}</td>
              <td>{trackerStatus[tr.status] ?? String(tr.status)}</td>
              <td className="is-numeric">{count(tr.num_peers)}</td>
              <td className="is-numeric">{count(tr.num_seeds)}</td>
              <td className="is-numeric">{count(tr.num_leeches)}</td>
              <td className="is-numeric">{count(tr.num_downloaded)}</td>
              <td>{tr.msg}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="tor-props-note">
        Tracker addresses are shortened to the host to hide private passkeys.
      </p>
    </div>
  );
}

function Peers({ detail }: { detail: Detail }) {
  if (!detail.peers.length)
    return <p className="tor-props-hint">No connected peers.</p>;
  return (
    <div className="tor-props-table-wrap">
      <table className="tor-mini-table" aria-label="Peers">
        <thead>
          <tr>
            <th scope="col">IP:Port</th>
            <th scope="col">Country</th>
            <th scope="col">Connection</th>
            <th scope="col">Flags</th>
            <th scope="col">Client</th>
            <th scope="col" className="is-numeric">
              Progress
            </th>
            <th scope="col" className="is-numeric">
              Down Speed
            </th>
            <th scope="col" className="is-numeric">
              Up Speed
            </th>
            <th scope="col" className="is-numeric">
              Downloaded
            </th>
            <th scope="col" className="is-numeric">
              Uploaded
            </th>
            <th scope="col" className="is-numeric">
              Relevance
            </th>
          </tr>
        </thead>
        <tbody>
          {detail.peers.map((peer) => {
            const address = peer.ip.includes(":")
              ? `[${peer.ip}]:${peer.port}`
              : `${peer.ip}:${peer.port}`;
            return (
              <tr key={address}>
                <td className="rs-mono">{address}</td>
                <td>{peer.country_code.toUpperCase()}</td>
                <td>{peer.connection}</td>
                <td className="rs-mono">{peer.flags}</td>
                <td>{peer.client}</td>
                <td className="is-numeric">{formatPercent(peer.progress)}</td>
                <td className="is-numeric">{formatSpeed(peer.dl_speed, "")}</td>
                <td className="is-numeric">{formatSpeed(peer.up_speed, "")}</td>
                <td className="is-numeric">{formatBytes(peer.downloaded)}</td>
                <td className="is-numeric">{formatBytes(peer.uploaded)}</td>
                <td className="is-numeric">{formatPercent(peer.relevance)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function HttpSources({ detail }: { detail: Detail }) {
  if (!detail.webseeds.length)
    return <p className="tor-props-hint">No HTTP sources.</p>;
  return (
    <ul className="tor-webseeds" aria-label="HTTP sources">
      {detail.webseeds.map((w, i) => (
        <li key={`${w.url}-${i}`}>{w.url}</li>
      ))}
    </ul>
  );
}

type Node = {
  name: string;
  path: string;
  depth: number;
  file?: TorrentFile;
  children: Node[];
  size: number;
  done: number;
  indexes: number[];
  priority: number;
  availability: number;
};

const mixed = -1;

function buildTree(files: TorrentFile[]): Node[] {
  const root: Node = {
    name: "",
    path: "",
    depth: -1,
    children: [],
    size: 0,
    done: 0,
    indexes: [],
    priority: mixed,
    availability: -1,
  };
  for (const file of files) {
    const parts = file.name.split(/[\\/]/).filter(Boolean);
    let parent = root;
    for (let i = 0; i < parts.length - 1; i++) {
      const path = parts.slice(0, i + 1).join("/");
      let next = parent.children.find((c) => !c.file && c.path === path);
      if (!next) {
        next = {
          name: parts[i],
          path,
          depth: i,
          children: [],
          size: 0,
          done: 0,
          indexes: [],
          priority: mixed,
          availability: -1,
        };
        parent.children.push(next);
      }
      parent = next;
    }
    parent.children.push({
      name: parts[parts.length - 1] ?? file.name,
      path: file.name,
      depth: parts.length - 1,
      file,
      children: [],
      size: file.size,
      done: file.size * file.progress,
      indexes: [file.index],
      priority: file.priority,
      availability: file.availability,
    });
  }
  function aggregate(node: Node) {
    if (node.file) return;
    node.children.sort((a, b) => {
      if (!a.file !== !b.file) return a.file ? 1 : -1;
      return a.name.localeCompare(b.name, undefined, { numeric: true });
    });
    const priorities = new Set<number>();
    let availability = Infinity;
    for (const child of node.children) {
      aggregate(child);
      node.size += child.size;
      node.done += child.done;
      node.indexes.push(...child.indexes);
      priorities.add(child.priority);
      if (child.availability >= 0)
        availability = Math.min(availability, child.availability);
    }
    node.priority = priorities.size === 1 ? [...priorities][0] : mixed;
    node.availability = Number.isFinite(availability) ? availability : -1;
  }
  aggregate(root);
  return root.children;
}

const priorityLabels: [number, string][] = [
  [0, "Do not download"],
  [1, "Normal"],
  [6, "High"],
  [7, "Maximum"],
];

function Content({
  torrent,
  detail,
  reload,
}: {
  torrent: Torrent;
  detail: Detail;
  reload: () => void;
}) {
  const tree = useMemo(() => buildTree(detail.files), [detail.files]);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const managed = originOf(detail.torrent ?? torrent) === "job";

  async function setPriority(node: Node, priority: number) {
    setBusy(true);
    setError("");
    try {
      await postNoContent(`/torrents/${torrent.hash}/files`, {
        indexes: node.indexes,
        priority,
      });
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  const rows: Node[] = [];
  const walk = (nodes: Node[]) => {
    for (const n of nodes) {
      rows.push(n);
      if (!n.file && !collapsed.has(n.path)) walk(n.children);
    }
  };
  walk(tree);

  if (!detail.files.length)
    return (
      <p className="tor-props-hint">
        The file list appears once qBittorrent has the torrent metadata.
      </p>
    );

  return (
    <div className="tor-props-table-wrap">
      {managed && (
        <p className="tor-props-note">
          ReelScarey chooses files for its own downloads.
        </p>
      )}
      {error && (
        <div className="tor-props-pad">
          <Alert kind="danger">{error}</Alert>
        </div>
      )}
      <table className="tor-mini-table tor-files" aria-label="Content">
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col" className="is-numeric">
              Size
            </th>
            <th scope="col">Progress</th>
            <th scope="col">Priority</th>
            <th scope="col" className="is-numeric">
              Remaining
            </th>
            <th scope="col" className="is-numeric">
              Availability
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((n) => {
            const progress = n.size > 0 ? n.done / n.size : 1;
            const open = !collapsed.has(n.path);
            return (
              <tr key={(n.file ? "f:" : "d:") + n.path}>
                <td>
                  <span
                    className="tor-file-name"
                    style={{ paddingLeft: n.depth * 16 }}
                  >
                    {n.file ? (
                      <>
                        <span className="tor-file-spacer" aria-hidden="true" />
                        <File size={14} aria-hidden="true" />
                      </>
                    ) : (
                      <>
                        <button
                          type="button"
                          className="tor-file-toggle"
                          aria-expanded={open}
                          aria-label={`${open ? "Collapse" : "Expand"} ${n.name}`}
                          onClick={() =>
                            setCollapsed((c) => {
                              const next = new Set(c);
                              if (next.has(n.path)) next.delete(n.path);
                              else next.add(n.path);
                              return next;
                            })
                          }
                        >
                          {open ? (
                            <ChevronDown size={14} aria-hidden="true" />
                          ) : (
                            <ChevronRight size={14} aria-hidden="true" />
                          )}
                        </button>
                        <Folder size={14} aria-hidden="true" />
                      </>
                    )}
                    <span className="tor-name-text" title={n.path}>
                      {n.name}
                    </span>
                  </span>
                </td>
                <td className="is-numeric">{formatBytes(n.size)}</td>
                <td>
                  <div
                    className="tor-bar"
                    role="progressbar"
                    aria-label={`Progress for ${n.name}`}
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-valuenow={Math.round(progress * 1000) / 10}
                  >
                    <div
                      className="tor-bar-fill"
                      style={{ width: `${Math.min(100, progress * 100)}%` }}
                    />
                    <span className="tor-bar-text">
                      {formatPercent(progress)}
                    </span>
                  </div>
                </td>
                <td>
                  <select
                    className="tor-priority"
                    aria-label={`Priority for ${n.name}`}
                    value={n.priority}
                    disabled={managed || busy}
                    onChange={(e) =>
                      void setPriority(n, Number(e.target.value))
                    }
                  >
                    {n.priority === mixed && (
                      <option value={mixed} disabled>
                        Mixed
                      </option>
                    )}
                    {priorityLabels.map(([value, label]) => (
                      <option key={value} value={value}>
                        {label}
                      </option>
                    ))}
                  </select>
                </td>
                <td className="is-numeric">
                  {formatBytes(Math.max(0, n.size - n.done))}
                </td>
                <td className="is-numeric">
                  {n.availability < 0 ? "" : n.availability.toFixed(3)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
