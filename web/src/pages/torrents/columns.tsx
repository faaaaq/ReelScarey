import type { ReactNode } from "react";
import {
  ArrowDown,
  ArrowUp,
  CircleAlert,
  CircleCheck,
  CircleDashed,
  CirclePause,
  Clock,
  RefreshCw,
  type LucideIcon,
} from "lucide-react";
import {
  formatBytes,
  formatDuration,
  formatSpeed,
  formatTimestamp,
} from "../../ui";
import {
  formatPercent,
  formatRatio,
  originOf,
  stateLabel,
  stateTone,
  tagsOf,
  trackerHost,
  type StateTone,
  type Torrent,
} from "./model";

export type ColumnKey =
  | "priority"
  | "name"
  | "size"
  | "progress"
  | "status"
  | "seeds"
  | "peers"
  | "dlspeed"
  | "upspeed"
  | "eta"
  | "ratio"
  | "category"
  | "tags"
  | "added_on"
  | "completion_on"
  | "tracker"
  | "dl_limit"
  | "up_limit"
  | "downloaded"
  | "uploaded"
  | "downloaded_session"
  | "uploaded_session"
  | "amount_left"
  | "time_active"
  | "seeding_time"
  | "availability"
  | "save_path"
  | "last_activity"
  | "origin";

export type Column = {
  key: ColumnKey;
  label: string;
  title?: string;
  width: number;
  numeric?: boolean;
  defaultVisible?: boolean;
  value: (t: Torrent) => number | string;
  render: (t: Torrent) => ReactNode;
};

const toneIcons: Record<StateTone, LucideIcon> = {
  downloading: ArrowDown,
  seeding: ArrowUp,
  completed: CircleCheck,
  paused: CirclePause,
  stalled: CircleDashed,
  queued: Clock,
  checking: RefreshCw,
  error: CircleAlert,
};

const originLabels = {
  job: "ReelScarey",
  manual: "Manual",
  external: "Other",
};

function limit(value: number) {
  return value > 0 ? formatSpeed(value) : "∞";
}

function time(value: number) {
  return value > 0 ? formatTimestamp(value) : "";
}

export const columns: Column[] = [
  {
    key: "priority",
    label: "#",
    title: "Queue position",
    width: 44,
    numeric: true,
    defaultVisible: true,
    value: (t) => (t.priority > 0 ? t.priority : Number.MAX_SAFE_INTEGER),
    render: (t) => (t.priority > 0 ? t.priority : ""),
  },
  {
    key: "name",
    label: "Name",
    width: 340,
    defaultVisible: true,
    value: (t) => t.name.toLowerCase(),
    render: (t) => {
      const tone = stateTone(t.state);
      const Icon = toneIcons[tone];
      return (
        <span className="tor-name">
          <Icon
            size={14}
            className={`tor-state-icon is-${tone}`}
            aria-hidden="true"
          />
          <span className="tor-name-text" title={t.name}>
            {t.name}
          </span>
        </span>
      );
    },
  },
  {
    key: "size",
    label: "Size",
    width: 86,
    numeric: true,
    defaultVisible: true,
    value: (t) => t.total_size,
    render: (t) => formatBytes(t.total_size),
  },
  {
    key: "progress",
    label: "Progress",
    width: 120,
    defaultVisible: true,
    value: (t) => t.progress,
    render: (t) => (
      <div
        className={`tor-bar is-${stateTone(t.state)}`}
        role="progressbar"
        aria-label={`Progress for ${t.name}`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(t.progress * 1000) / 10}
      >
        <div
          className="tor-bar-fill"
          style={{ width: `${Math.min(100, t.progress * 100)}%` }}
        />
        <span className="tor-bar-text">{formatPercent(t.progress)}</span>
      </div>
    ),
  },
  {
    key: "status",
    label: "Status",
    width: 120,
    defaultVisible: true,
    value: (t) => stateLabel(t.state),
    render: (t) => stateLabel(t.state),
  },
  {
    key: "seeds",
    label: "Seeds",
    width: 76,
    numeric: true,
    defaultVisible: true,
    value: (t) => t.num_seeds,
    render: (t) => `${t.num_seeds} (${Math.max(0, t.num_complete)})`,
  },
  {
    key: "peers",
    label: "Peers",
    width: 76,
    numeric: true,
    defaultVisible: true,
    value: (t) => t.num_leechs,
    render: (t) => `${t.num_leechs} (${Math.max(0, t.num_incomplete)})`,
  },
  {
    key: "dlspeed",
    label: "Down Speed",
    width: 96,
    numeric: true,
    defaultVisible: true,
    value: (t) => t.dlspeed,
    render: (t) => formatSpeed(t.dlspeed, ""),
  },
  {
    key: "upspeed",
    label: "Up Speed",
    width: 96,
    numeric: true,
    defaultVisible: true,
    value: (t) => t.upspeed,
    render: (t) => formatSpeed(t.upspeed, ""),
  },
  {
    key: "eta",
    label: "ETA",
    width: 76,
    numeric: true,
    defaultVisible: true,
    value: (t) => t.eta,
    render: (t) => formatDuration(t.eta),
  },
  {
    key: "ratio",
    label: "Ratio",
    width: 60,
    numeric: true,
    defaultVisible: true,
    value: (t) => t.ratio,
    render: (t) => formatRatio(t.ratio),
  },
  {
    key: "category",
    label: "Category",
    width: 100,
    defaultVisible: true,
    value: (t) => t.category.toLowerCase(),
    render: (t) => t.category,
  },
  {
    key: "tags",
    label: "Tags",
    width: 110,
    defaultVisible: true,
    value: (t) => tagsOf(t).join(", ").toLowerCase(),
    render: (t) => tagsOf(t).join(", "),
  },
  {
    key: "added_on",
    label: "Added On",
    width: 150,
    numeric: true,
    defaultVisible: true,
    value: (t) => t.added_on,
    render: (t) => time(t.added_on),
  },
  {
    key: "completion_on",
    label: "Completed On",
    width: 150,
    numeric: true,
    value: (t) => t.completion_on,
    render: (t) => time(t.completion_on),
  },
  {
    key: "tracker",
    label: "Tracker",
    width: 160,
    value: (t) => trackerHost(t),
    render: (t) => trackerHost(t),
  },
  {
    key: "dl_limit",
    label: "Down Limit",
    width: 90,
    numeric: true,
    value: (t) => (t.dl_limit > 0 ? t.dl_limit : Number.MAX_SAFE_INTEGER),
    render: (t) => limit(t.dl_limit),
  },
  {
    key: "up_limit",
    label: "Up Limit",
    width: 90,
    numeric: true,
    value: (t) => (t.up_limit > 0 ? t.up_limit : Number.MAX_SAFE_INTEGER),
    render: (t) => limit(t.up_limit),
  },
  {
    key: "downloaded",
    label: "Downloaded",
    width: 96,
    numeric: true,
    value: (t) => t.downloaded,
    render: (t) => formatBytes(t.downloaded),
  },
  {
    key: "uploaded",
    label: "Uploaded",
    width: 96,
    numeric: true,
    value: (t) => t.uploaded,
    render: (t) => formatBytes(t.uploaded),
  },
  {
    key: "downloaded_session",
    label: "Session Download",
    width: 110,
    numeric: true,
    value: (t) => t.downloaded_session,
    render: (t) => formatBytes(t.downloaded_session),
  },
  {
    key: "uploaded_session",
    label: "Session Upload",
    width: 110,
    numeric: true,
    value: (t) => t.uploaded_session,
    render: (t) => formatBytes(t.uploaded_session),
  },
  {
    key: "amount_left",
    label: "Remaining",
    width: 96,
    numeric: true,
    value: (t) => t.amount_left,
    render: (t) => formatBytes(t.amount_left),
  },
  {
    key: "time_active",
    label: "Time Active",
    width: 96,
    numeric: true,
    value: (t) => t.time_active,
    render: (t) => formatDuration(t.time_active),
  },
  {
    key: "seeding_time",
    label: "Seeding Time",
    width: 96,
    numeric: true,
    value: (t) => t.seeding_time,
    render: (t) => formatDuration(t.seeding_time),
  },
  {
    key: "availability",
    label: "Availability",
    width: 86,
    numeric: true,
    value: (t) => t.availability,
    render: (t) => (t.availability < 0 ? "" : t.availability.toFixed(3)),
  },
  {
    key: "save_path",
    label: "Save Path",
    width: 220,
    value: (t) => t.save_path.toLowerCase(),
    render: (t) => t.save_path,
  },
  {
    key: "last_activity",
    label: "Last Activity",
    width: 150,
    numeric: true,
    value: (t) => t.last_activity,
    render: (t) => time(t.last_activity),
  },
  {
    key: "origin",
    label: "Source",
    width: 96,
    value: (t) => originOf(t),
    render: (t) => originLabels[originOf(t)],
  },
];

export const columnByKey = new Map(columns.map((c) => [c.key, c]));

export const defaultColumns: ColumnKey[] = columns
  .filter((c) => c.defaultVisible)
  .map((c) => c.key);

export type Sort = { key: ColumnKey; direction: "asc" | "desc" };

export const defaultSort: Sort = { key: "added_on", direction: "desc" };

export function sortTorrents(items: Torrent[], sort: Sort) {
  const column = columnByKey.get(sort.key) ?? columnByKey.get("added_on")!;
  const factor = sort.direction === "asc" ? 1 : -1;
  const keyed = items.map((t) => ({ t, v: column.value(t) }));
  keyed.sort((a, b) => {
    let diff: number;
    if (typeof a.v === "number" && typeof b.v === "number") diff = a.v - b.v;
    else
      diff = String(a.v).localeCompare(String(b.v), undefined, {
        numeric: true,
      });
    if (diff === 0) diff = a.t.name.localeCompare(b.t.name);
    if (diff === 0) diff = a.t.hash < b.t.hash ? -1 : 1;
    return diff * factor;
  });
  return keyed.map((k) => k.t);
}
