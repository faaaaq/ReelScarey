export const KiB = 1024;
export const maxNZB = 4 * 1024 * 1024;
export const maxQueueSelection = 500;

export type UsenetStatus = {
  present: boolean;
  installed: boolean;
  enabled: boolean;
  configured: boolean;
  state: "running" | "starting" | "stopped" | "failed" | string;
  message?: string;
  revision: number;
  active_jobs: number;
  name: string;
  host?: string;
  username?: string;
  port: number;
  tls: boolean;
  connections: number;
};

export type UsenetItem = {
  id: string;
  name: string;
  size: number;
  remaining: number;
  progress: number;
  status: string;
  speed: number;
  eta: string;
  category: string;
  priority: string;
};

export type UsenetQueue = {
  available: boolean;
  state: string;
  message?: string;
  paused: boolean;
  speed: number;
  remaining: number;
  eta: string;
  limit_bytes: number;
  limit_percent: number;
  items: UsenetItem[];
};

export type UsenetOverview = {
  status: UsenetStatus;
  queue: UsenetQueue;
};

export const lifecycleLabels: Record<string, string> = {
  running: "Running",
  starting: "Starting…",
  stopped: "Stopped",
  failed: "Needs attention",
};

export function lifecycleKind(
  state: string,
): "success" | "primary" | "warning" | "danger" | "disabled" {
  switch (state) {
    case "running":
      return "success";
    case "starting":
      return "primary";
    case "failed":
      return "danger";
    default:
      return "disabled";
  }
}

/**
 * The queue area explains exactly why it cannot show jobs. This is used for
 * the honest empty state instead of presenting a blank working queue.
 */
export type QueueBlocker = {
  key:
    "not-installed" | "not-configured" | "disabled" | "waiting" | "unavailable";
  title: string;
  message: string;
  action: "settings" | "address" | "clients" | "refresh" | null;
};

export function queueBlocker(
  status: UsenetStatus,
  queue: UsenetQueue,
): QueueBlocker | null {
  if (!status.installed) {
    return {
      key: "not-installed",
      title: "The bundled Usenet client is not installed",
      message:
        "This deployment does not include sabnzbdplus. The Docker image ships it; a native install must provide it or set REELSCAREY_BUNDLED_SAB. External SABnzbd/NZBGet connections remain available in Download clients.",
      action: null,
    };
  }
  if (!status.present || !status.configured) {
    return {
      key: "not-configured",
      title: "Add a news server",
      message:
        "Save your NNTP provider below, then turn on the bundled client. Without a news server it never starts.",
      action: "settings",
    };
  }
  if (!status.enabled) {
    return {
      key: "disabled",
      title: "The bundled Usenet client is off",
      message:
        "Turn it on below. Usenet downloads use it only after you also choose it as the Usenet default in Download clients.",
      action: "settings",
    };
  }
  if (status.state === "starting") {
    return {
      key: "waiting",
      title: "SABnzbd is starting",
      message:
        status.message ||
        "The daemon is starting; its queue appears when its API is ready.",
      action: "refresh",
    };
  }
  if (status.state !== "running" || !queue.available) {
    return {
      key: "unavailable",
      title:
        status.state === "failed"
          ? "The bundled Usenet client needs attention"
          : "The bundled Usenet client is not running",
      message:
        queue.message ||
        status.message ||
        "Review the client status below, then try again.",
      action: "refresh",
    };
  }
  return null;
}

export function selectedSpeed(
  items: UsenetItem[],
  selected: Set<string>,
): number | null {
  const picked = items.filter((i) => selected.has(i.id));
  if (!picked.length) return null;
  const first = picked[0].speed;
  return picked.every((i) => i.speed === first) ? first : null;
}

export function queuedCount(items: UsenetItem[]) {
  return items.length;
}

export function statusTone(status: UsenetItem): string {
  const value = status.status.toLowerCase();
  if (value === "paused") return "disabled";
  if (value === "downloading" || value === "fetching") return "primary";
  if (value === "queued" || value === "propagating") return "info";
  if (value === "failed") return "danger";
  return "default";
}
