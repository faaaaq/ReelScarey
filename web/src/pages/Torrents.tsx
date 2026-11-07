import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { Button, DropdownMenu } from "@radix-ui/themes";
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronUp,
  ChevronsDown,
  ChevronsUp,
  Columns3,
  Ellipsis,
  FastForward,
  FilePlus,
  Gauge,
  HardDrive,
  Link2,
  Megaphone,
  PanelBottom,
  PanelLeft,
  Pause,
  Play,
  RefreshCw,
  ShieldAlert,
  Trash,
  Turtle,
  Wifi,
  WifiOff,
  X,
} from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import {
  Alert,
  EmptyState,
  PageBody,
  PageToolbar,
  ToolbarButton,
  ToolbarSection,
  ToolbarSeparator,
  formatBytes,
  formatSpeed,
} from "../ui";
import { useLiveResource } from "../useLiveResource";
import {
  columnByKey,
  columns as allColumns,
  defaultColumns,
  defaultSort,
  sortTorrents,
  type ColumnKey,
  type Sort,
} from "./torrents/columns";
import {
  AddLinksDialog,
  DeleteDialog,
  ShareLimitDialog,
  SharePolicyDialog,
  SpeedLimitDialog,
  type AddFailure,
} from "./torrents/dialogs";
import { FilterSidebar } from "./torrents/FilterSidebar";
import { DropdownMenuEntries, type MenuEntry } from "./torrents/menu";
import {
  defaultFilters,
  isPaused,
  magnetFor,
  matchesFilters,
  originOf,
  readStored,
  searchTerms,
  sharePolicySummary,
  writeStored,
  type Filters,
  type Overview,
  type SharePolicy,
  type SharePolicyRun,
  type Torrent,
} from "./torrents/model";
import { PropertiesPanel } from "./torrents/PropertiesPanel";
import { postNoContent } from "./torrents/request";
import { useSelection } from "./torrents/selection";
import { TorrentTable } from "./torrents/TorrentTable";
import "./Torrents.css";

const keys = {
  columns: "reelscarey.torrents.columns",
  sort: "reelscarey.torrents.sort",
  sidebar: "reelscarey.torrents.sidebar",
  panel: "reelscarey.torrents.panel",
  panelHeight: "reelscarey.torrents.panel.height",
};
const maxUpload = 10 * 1024 * 1024;
const maxActionHashes = 500;
const minPanel = 120;

type Message = { kind: "success" | "danger" | "warning"; text: string };
type LimitDialog = "dl" | "ul" | "global-dl" | "global-ul" | null;

function useNarrow() {
  const query = "(max-width: 768px)";
  const [narrow, setNarrow] = useState(
    () =>
      typeof window.matchMedia === "function" &&
      window.matchMedia(query).matches,
  );
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const list = window.matchMedia(query);
    const update = () => setNarrow(list.matches);
    list.addEventListener("change", update);
    return () => list.removeEventListener("change", update);
  }, []);
  return narrow;
}

function readFile(file: File): Promise<ArrayBuffer> {
  if (typeof file.arrayBuffer === "function") return file.arrayBuffer();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as ArrayBuffer);
    reader.onerror = () => reject(reader.error);
    reader.readAsArrayBuffer(file);
  });
}

function storedColumns(): ColumnKey[] {
  const stored = readStored<string[]>(keys.columns, defaultColumns);
  const valid = Array.isArray(stored)
    ? stored.filter((k): k is ColumnKey => columnByKey.has(k as ColumnKey))
    : defaultColumns;
  return valid.includes("name") ? valid : ["name", ...valid];
}

function storedSort(): Sort {
  const stored = readStored<Sort>(keys.sort, defaultSort);
  return stored &&
    columnByKey.has(stored.key) &&
    (stored.direction === "asc" || stored.direction === "desc")
    ? stored
    : defaultSort;
}

export function Torrents() {
  const overview = useLiveResource<Overview>("/torrents", 2000);
  const sharePolicy = useLiveResource<SharePolicy>(
    "/torrents/share-policy",
    30000,
  );
  const navigate = useNavigate();
  const narrow = useNarrow();

  const [filters, setFilters] = useState<Filters>(defaultFilters);
  const [query, setQuery] = useState("");
  const [sort, setSortState] = useState<Sort>(storedSort);
  const [visibleColumns, setVisibleColumns] =
    useState<ColumnKey[]>(storedColumns);
  const [sidebarOpen, setSidebarOpen] = useState(() =>
    readStored<boolean>(keys.sidebar, true),
  );
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [panelOpen, setPanelOpen] = useState(() =>
    readStored<boolean>(keys.panel, true),
  );
  const [panelHeight, setPanelHeight] = useState(() =>
    Math.max(minPanel, readStored<number>(keys.panelHeight, 260)),
  );
  const [message, setMessage] = useState<Message | null>(null);
  const [busy, setBusy] = useState(false);
  const [addLinksOpen, setAddLinksOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [limitDialog, setLimitDialog] = useState<LimitDialog>(null);
  const [shareOpen, setShareOpen] = useState(false);
  const [sharePolicyOpen, setSharePolicyOpen] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const centerRef = useRef<HTMLDivElement>(null);

  const data = overview.data;
  const items = useMemo(() => data?.items ?? [], [data]);
  const transfer = data?.transfer ?? {};
  const server = data?.server ?? null;
  const queueing = server?.queueing_enabled ?? true;

  const visible = useMemo(() => {
    const terms = searchTerms(query);
    return sortTorrents(
      items.filter((t) => matchesFilters(t, filters, terms)),
      sort,
    );
  }, [items, filters, query, sort]);
  const order = useMemo(() => visible.map((t) => t.hash), [visible]);
  const selection = useSelection(order);
  const selected = useMemo(
    () => visible.filter((t) => selection.selected.has(t.hash)),
    [visible, selection.selected],
  );
  const hashes = useMemo(() => selected.map((t) => t.hash), [selected]);
  const single = selected.length === 1 ? selected[0] : null;
  const none = selected.length === 0;

  const columns = useMemo(
    () => allColumns.filter((c) => visibleColumns.includes(c.key)),
    [visibleColumns],
  );

  function setSort(next: Sort) {
    setSortState(next);
    writeStored(keys.sort, next);
  }
  function toggleColumn(key: ColumnKey) {
    if (key === "name") return;
    setVisibleColumns((current) => {
      const next = current.includes(key)
        ? current.filter((k) => k !== key)
        : [...current, key];
      writeStored(keys.columns, next);
      return next;
    });
  }
  function resetColumns() {
    setVisibleColumns(defaultColumns);
    writeStored(keys.columns, defaultColumns);
  }
  function toggleSidebar() {
    if (narrow) {
      setDrawerOpen((open) => !open);
      return;
    }
    setSidebarOpen((open) => {
      writeStored(keys.sidebar, !open);
      return !open;
    });
  }
  function togglePanel() {
    setPanelOpen((open) => {
      writeStored(keys.panel, !open);
      return !open;
    });
  }
  const changeFilters = useCallback(
    (next: Filters) => {
      setFilters(next);
      if (narrow) setDrawerOpen(false);
    },
    [narrow],
  );

  const reload = overview.reload;
  const send = useCallback(
    async (
      targets: string[],
      action: string,
      extra: Record<string, unknown> = {},
      done?: string,
    ) => {
      if (!targets.length) return false;
      if (targets.length > maxActionHashes) {
        setMessage({
          kind: "danger",
          text: `Select at most ${maxActionHashes} torrents for one action.`,
        });
        return false;
      }
      setBusy(true);
      setMessage(null);
      try {
        await postNoContent("/torrents/actions", {
          hashes: targets,
          action,
          ...extra,
        });
        if (done) setMessage({ kind: "success", text: done });
        return true;
      } catch (e) {
        setMessage({ kind: "danger", text: messageFor(e) });
        return false;
      } finally {
        setBusy(false);
        reload();
      }
    },
    [reload],
  );
  const act = useCallback(
    (action: string, extra?: Record<string, unknown>, done?: string) =>
      send(hashes, action, extra, done),
    [send, hashes],
  );

  async function setSpeedLimits(body: Record<string, unknown>) {
    setBusy(true);
    setMessage(null);
    try {
      await postNoContent("/torrents/speed-limits", body);
      return true;
    } catch (e) {
      setMessage({ kind: "danger", text: messageFor(e) });
      return false;
    } finally {
      setBusy(false);
      reload();
    }
  }

  async function saveSharePolicy(next: SharePolicy) {
    await api<SharePolicy>("/torrents/share-policy", {
      method: "PUT",
      body: JSON.stringify(next),
    });
    sharePolicy.reload();
    setMessage({ kind: "success", text: "Seeding policy saved." });
  }

  async function runSharePolicy() {
    const result = await api<SharePolicyRun>("/torrents/share-policy/run", {
      method: "POST",
    });
    sharePolicy.reload();
    return result;
  }

  async function addLinks(links: string[]): Promise<AddFailure[]> {
    const failures: AddFailure[] = [];
    setMessage(null);
    for (const magnet of links) {
      try {
        await api("/torrents", {
          method: "POST",
          body: JSON.stringify({ magnet }),
        });
      } catch (e) {
        failures.push({ input: magnet, message: messageFor(e) });
      }
    }
    reload();
    const added = links.length - failures.length;
    if (added)
      setMessage({
        kind: "success",
        text: added === 1 ? "Added 1 torrent." : `Added ${added} torrents.`,
      });
    return failures;
  }

  async function addFiles(files: File[]) {
    if (!files.length) return;
    setBusy(true);
    setMessage(null);
    const failures: string[] = [];
    let added = 0;
    for (const file of files) {
      if (file.size > maxUpload) {
        failures.push(`${file.name}: larger than 10 MiB`);
        continue;
      }
      try {
        const body = await readFile(file);
        await api("/torrents", {
          method: "POST",
          headers: { "Content-Type": "application/x-bittorrent" },
          body,
        });
        added++;
      } catch (e) {
        failures.push(`${file.name}: ${messageFor(e)}`);
      }
    }
    setBusy(false);
    reload();
    if (failures.length)
      setMessage({
        kind: "danger",
        text: `${added ? `Added ${added}. ` : ""}Not added: ${failures.join("; ")}`,
      });
    else
      setMessage({
        kind: "success",
        text: added === 1 ? "Added 1 torrent." : `Added ${added} torrents.`,
      });
  }

  const copy = useCallback(async (text: string, what: string) => {
    try {
      if (!navigator.clipboard) throw new Error("unavailable");
      await navigator.clipboard.writeText(text);
      setMessage({ kind: "success", text: `Copied ${what}.` });
    } catch {
      setMessage({
        kind: "danger",
        text: "The browser did not allow copying to the clipboard.",
      });
    }
  }, []);

  const onDoubleClick = useCallback(
    (hash: string) => {
      const t = items.find((i) => i.hash === hash);
      if (!t) return;
      void send([hash], isPaused(t) ? "resume" : "pause");
    },
    [items, send],
  );

  const openDelete = useCallback(() => {
    if (selection.selected.size) setDeleteOpen(true);
  }, [selection.selected]);

  const menuEntries = useMemo<MenuEntry[]>(() => {
    const all = (pick: (t: Torrent) => boolean) =>
      selected.length > 0 && selected.every(pick);
    const complete = all((t) => t.progress >= 1);
    const job = single && originOf(single) === "job" && single.job_id;
    const queueDisabled = none || !queueing;
    const entries: MenuEntry[] = [
      {
        kind: "item",
        label: "Resume",
        disabled: none,
        onSelect: () => void act("resume"),
      },
      {
        kind: "item",
        label: "Pause",
        disabled: none,
        onSelect: () => void act("pause"),
      },
      {
        kind: "item",
        label: "Force Resume",
        disabled: none,
        onSelect: () => void act("force_start", { enabled: true }),
      },
      {
        kind: "item",
        label: "Delete…",
        danger: true,
        shortcut: "Del",
        disabled: none,
        onSelect: () => setDeleteOpen(true),
      },
      { kind: "separator" },
      {
        kind: "item",
        label: "Limit download rate…",
        disabled: none,
        onSelect: () => setLimitDialog("dl"),
      },
      {
        kind: "item",
        label: "Limit upload rate…",
        disabled: none,
        onSelect: () => setLimitDialog("ul"),
      },
      {
        kind: "item",
        label: "Limit share ratio…",
        disabled: none,
        onSelect: () => setShareOpen(true),
      },
      { kind: "separator" },
      {
        kind: "check",
        label: "Sequential download",
        checked: all((t) => t.seq_dl),
        disabled: none,
        onSelect: () =>
          void act("sequential", { enabled: !all((t) => t.seq_dl) }),
      },
      {
        kind: "check",
        label: "Download first and last pieces first",
        checked: all((t) => t.f_l_piece_prio),
        disabled: none,
        onSelect: () =>
          void act("first_last_piece", {
            enabled: !all((t) => t.f_l_piece_prio),
          }),
      },
      {
        kind: "check",
        label: "Super seeding mode",
        checked: all((t) => t.super_seeding),
        disabled: none || !complete,
        onSelect: () =>
          void act("super_seeding", { enabled: !all((t) => t.super_seeding) }),
      },
      { kind: "separator" },
      {
        kind: "item",
        label: "Force recheck",
        disabled: none,
        onSelect: () => void act("recheck"),
      },
      {
        kind: "item",
        label: "Force reannounce",
        disabled: none,
        onSelect: () => void act("reannounce"),
      },
      { kind: "separator" },
      {
        kind: "sub",
        label: queueing ? "Queue" : "Queue (disabled in client)",
        disabled: queueDisabled,
        entries: [
          {
            kind: "item",
            label: "Move to top",
            onSelect: () => void act("queue_top"),
          },
          {
            kind: "item",
            label: "Move up",
            onSelect: () => void act("queue_up"),
          },
          {
            kind: "item",
            label: "Move down",
            onSelect: () => void act("queue_down"),
          },
          {
            kind: "item",
            label: "Move to bottom",
            onSelect: () => void act("queue_bottom"),
          },
        ],
      },
      {
        kind: "sub",
        label: "Copy",
        disabled: none,
        entries: [
          {
            kind: "item",
            label: "Name",
            onSelect: () =>
              void copy(selected.map((t) => t.name).join("\n"), "name"),
          },
          {
            kind: "item",
            label: "Info hash",
            onSelect: () =>
              void copy(selected.map((t) => t.hash).join("\n"), "info hash"),
          },
          {
            kind: "item",
            label: "Magnet link",
            onSelect: () =>
              void copy(selected.map(magnetFor).join("\n"), "magnet link"),
          },
        ],
      },
    ];
    if (job)
      entries.push(
        { kind: "separator" },
        {
          kind: "item",
          label: "Open in Activity",
          onSelect: () =>
            navigate(`/activity?job=${encodeURIComponent(String(job))}`),
        },
      );
    return entries;
  }, [selected, single, none, queueing, act, copy, navigate]);

  // Splitter between the torrent list and the properties panel.
  const drag = useRef<{ y: number; height: number } | null>(null);
  function clampPanel(height: number) {
    const total = centerRef.current?.clientHeight ?? 0;
    const max = total > 0 ? Math.max(minPanel, total - 120) : 2000;
    return Math.round(Math.max(minPanel, Math.min(max, height)));
  }
  function splitterDown(e: ReactPointerEvent<HTMLDivElement>) {
    drag.current = { y: e.clientY, height: panelHeight };
    e.currentTarget.setPointerCapture(e.pointerId);
  }
  function splitterMove(e: ReactPointerEvent<HTMLDivElement>) {
    if (!drag.current) return;
    setPanelHeight(
      clampPanel(drag.current.height - (e.clientY - drag.current.y)),
    );
  }
  function splitterUp(e: ReactPointerEvent<HTMLDivElement>) {
    if (!drag.current) return;
    drag.current = null;
    e.currentTarget.releasePointerCapture(e.pointerId);
    setPanelHeight((h) => {
      writeStored(keys.panelHeight, h);
      return h;
    });
  }
  function splitterKey(e: ReactKeyboardEvent<HTMLDivElement>) {
    const step = e.shiftKey ? 80 : 20;
    let next: number | null = null;
    if (e.key === "ArrowUp") next = panelHeight + step;
    if (e.key === "ArrowDown") next = panelHeight - step;
    if (next === null) return;
    e.preventDefault();
    const h = clampPanel(next);
    setPanelHeight(h);
    writeStored(keys.panelHeight, h);
  }

  if (data && !data.available) {
    return (
      <PageBody>
        <h1 className="sr-only">Torrents</h1>
        <EmptyState
          icon={HardDrive}
          title="The built-in client is not enabled"
          actions={
            <Button asChild>
              <Link to="/download-clients">Open download clients</Link>
            </Button>
          }
        >
          <p>{data.message}</p>
        </EmptyState>
      </PageBody>
    );
  }

  if (!data)
    return (
      <PageBody>
        <h1 className="sr-only">Torrents</h1>
        <RequestState
          loading={overview.loading}
          error={overview.error}
          retry={overview.reload}
        />
      </PageBody>
    );

  const showSidebar = narrow ? drawerOpen : sidebarOpen;
  const singleLimit = (pick: (t: Torrent) => number) => {
    if (!selected.length) return 0;
    const first = pick(selected[0]);
    return selected.every((t) => pick(t) === first) ? first : null;
  };
  const altOn = !!server?.alt_speed_enabled;
  const connection = transfer.connection_status ?? "disconnected";
  const ConnectionIcon =
    connection === "connected"
      ? Wifi
      : connection === "firewalled"
        ? ShieldAlert
        : WifiOff;
  const connectionLabel =
    connection === "connected"
      ? "Connected"
      : connection === "firewalled"
        ? "Firewalled"
        : "Disconnected";

  return (
    <div className="tor-app">
      <h1 className="sr-only">Torrents</h1>
      <PageToolbar label="Torrent actions">
        <ToolbarSection>
          <ToolbarButton
            icon={Link2}
            label="Add Link"
            aria-label="Add Torrent Link"
            title="Add torrent links (magnet)"
            className="tor-tool"
            onClick={() => setAddLinksOpen(true)}
          />
          <ToolbarButton
            icon={FilePlus}
            label="Add File"
            aria-label="Add Torrent File"
            title="Add .torrent files"
            className="tor-tool"
            disabled={busy}
            onClick={() => fileInput.current?.click()}
          />
          <input
            ref={fileInput}
            type="file"
            multiple
            hidden
            accept=".torrent,application/x-bittorrent"
            aria-label="Torrent files"
            onChange={(e) => {
              const files = [...(e.target.files ?? [])];
              e.target.value = "";
              void addFiles(files);
            }}
          />
          <ToolbarSeparator />
          <ToolbarButton
            icon={Play}
            label="Resume"
            className="tor-tool"
            disabled={none || busy}
            onClick={() => void act("resume")}
          />
          <ToolbarButton
            icon={Pause}
            label="Pause"
            className="tor-tool"
            disabled={none || busy}
            onClick={() => void act("pause")}
          />
          <ToolbarButton
            icon={FastForward}
            label="Force Resume"
            className="tor-tool"
            disabled={none || busy}
            onClick={() => void act("force_start", { enabled: true })}
          />
          <ToolbarButton
            icon={Trash}
            label="Delete"
            danger
            className="tor-tool"
            disabled={none || busy}
            onClick={() => setDeleteOpen(true)}
          />
          <ToolbarSeparator />
          {(
            [
              [ChevronsUp, "Top", "Move to top of queue", "queue_top"],
              [ChevronUp, "Up", "Move up in queue", "queue_up"],
              [ChevronDown, "Down", "Move down in queue", "queue_down"],
              [
                ChevronsDown,
                "Bottom",
                "Move to bottom of queue",
                "queue_bottom",
              ],
            ] as const
          ).map(([icon, label, title, action]) => (
            <ToolbarButton
              key={action}
              icon={icon}
              label={label}
              aria-label={title}
              title={queueing ? title : "Queueing is disabled in qBittorrent"}
              className="tor-tool"
              disabled={none || busy || !queueing}
              onClick={() => void act(action)}
            />
          ))}
          <ToolbarSeparator />
          <ToolbarButton
            icon={RefreshCw}
            label="Recheck"
            className="tor-tool"
            disabled={none || busy}
            onClick={() => void act("recheck")}
          />
          <ToolbarButton
            icon={Megaphone}
            label="Reannounce"
            className="tor-tool"
            disabled={none || busy}
            onClick={() => void act("reannounce")}
          />
          <DropdownMenu.Root>
            <DropdownMenu.Trigger disabled={none}>
              <ToolbarButton
                icon={Ellipsis}
                label="More"
                aria-label="More torrent actions"
                className="tor-tool"
                disabled={none}
              />
            </DropdownMenu.Trigger>
            <DropdownMenu.Content size="1" className="tor-menu">
              <DropdownMenuEntries entries={menuEntries} />
            </DropdownMenu.Content>
          </DropdownMenu.Root>
          <ToolbarSeparator />
          <ToolbarButton
            icon={Gauge}
            label="Seeding policy"
            title="Automatically limit seeding for this client"
            className="tor-tool"
            onClick={() => setSharePolicyOpen(true)}
          />
        </ToolbarSection>
        <ToolbarSection align="right">
          <div className="tor-search">
            <input
              type="search"
              value={query}
              placeholder="Filter torrent list…"
              aria-label="Filter torrent list"
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
          <DropdownMenu.Root>
            <DropdownMenu.Trigger>
              <ToolbarButton
                icon={Columns3}
                label="Columns"
                className="tor-tool"
              />
            </DropdownMenu.Trigger>
            <DropdownMenu.Content
              size="1"
              className="tor-menu tor-columns-menu"
            >
              {allColumns.map((c) => (
                <DropdownMenu.CheckboxItem
                  key={c.key}
                  checked={visibleColumns.includes(c.key)}
                  disabled={c.key === "name"}
                  onSelect={(e) => {
                    e.preventDefault();
                    toggleColumn(c.key);
                  }}
                >
                  {c.key === "priority" ? "# (Queue position)" : c.label}
                </DropdownMenu.CheckboxItem>
              ))}
              <DropdownMenu.Separator />
              <DropdownMenu.Item onSelect={resetColumns}>
                Reset to defaults
              </DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu.Root>
          <ToolbarButton
            icon={PanelLeft}
            label="Filters"
            aria-label="Show filter sidebar"
            className="tor-tool"
            active={showSidebar}
            onClick={toggleSidebar}
          />
          <ToolbarButton
            icon={PanelBottom}
            label="Properties"
            aria-label="Show properties panel"
            className="tor-tool"
            active={panelOpen}
            onClick={togglePanel}
          />
        </ToolbarSection>
      </PageToolbar>

      {(message || data.state === "disconnected") && (
        <div className="tor-messages">
          {data.state === "disconnected" && (
            <Alert kind="warning" title="The built-in client is not responding">
              {data.message || "ReelScarey is trying to reconnect."}
            </Alert>
          )}
          {message && (
            <div className="tor-message">
              <Alert
                kind={message.kind}
                role={message.kind === "danger" ? "alert" : "status"}
              >
                {message.text}
              </Alert>
              <button
                type="button"
                className="tor-message-close"
                aria-label="Dismiss message"
                onClick={() => setMessage(null)}
              >
                <X size={16} aria-hidden="true" />
              </button>
            </div>
          )}
        </div>
      )}

      <div className="tor-main">
        {showSidebar && (
          <aside
            className={`tor-sidebar ${narrow ? "is-drawer" : ""}`}
            aria-label="Filters"
          >
            <FilterSidebar
              items={items}
              filters={filters}
              onChange={changeFilters}
            />
          </aside>
        )}
        {narrow && drawerOpen && (
          <button
            type="button"
            className="tor-drawer-backdrop"
            aria-label="Close filters"
            onClick={() => setDrawerOpen(false)}
          />
        )}
        <div className="tor-center" ref={centerRef}>
          <TorrentTable
            rows={visible}
            columns={columns}
            sort={sort}
            onSort={setSort}
            selected={selection.selected}
            focus={selection.focus}
            onRowClick={selection.click}
            onRowDoubleClick={onDoubleClick}
            onRowContext={selection.context}
            onMove={selection.move}
            onToggleFocus={selection.toggleFocus}
            onSelectAll={selection.selectAll}
            onClear={selection.clear}
            onDelete={openDelete}
            menuEntries={menuEntries}
            emptyText={
              items.length === 0
                ? "No torrents in the built-in client."
                : "No torrents match the current filters."
            }
          />
          {panelOpen && (
            <>
              <div
                className="tor-splitter"
                role="separator"
                aria-orientation="horizontal"
                aria-label="Resize properties panel"
                aria-valuenow={panelHeight}
                aria-valuemin={minPanel}
                tabIndex={0}
                onPointerDown={splitterDown}
                onPointerMove={splitterMove}
                onPointerUp={splitterUp}
                onPointerCancel={splitterUp}
                onKeyDown={splitterKey}
              />
              <section
                className="tor-panel"
                style={{ height: panelHeight }}
                aria-label="Properties"
              >
                <PropertiesPanel torrent={single} />
              </section>
            </>
          )}
        </div>
      </div>

      <footer className="tor-statusbar" aria-label="Transfer rates">
        {overview.error && (
          <span className="tor-status-item is-lost" role="status">
            Connection lost — retrying
          </span>
        )}
        <span
          className={`tor-status-item is-${connection}`}
          title={`Connection status: ${connectionLabel}`}
        >
          <ConnectionIcon size={14} aria-hidden="true" />
          {connectionLabel}
        </span>
        <span className="tor-status-item">
          DHT: {transfer.dht_nodes ?? 0} nodes
        </span>
        <span className="tor-status-sep" aria-hidden="true" />
        <span className="tor-status-item rs-muted">
          {visible.length === items.length
            ? `${items.length} torrents`
            : `${visible.length} of ${items.length} torrents`}
          {selected.length > 0 && `, ${selected.length} selected`}
        </span>
        {sharePolicy.data && (
          <span
            className="tor-status-item rs-muted"
            title="Automatic seeding limit for the built-in client"
          >
            Seeding limit: {sharePolicySummary(sharePolicy.data)}
          </span>
        )}
        <span className="tor-status-spacer" />
        <button
          type="button"
          className={`tor-status-button ${altOn ? "is-on" : ""}`}
          aria-pressed={altOn}
          aria-label="Alternative speed limits"
          title={
            altOn
              ? "Alternative speed limits are on. Click to use the regular limits."
              : "Click to switch on the alternative speed limits."
          }
          disabled={!server || busy}
          onClick={() => void setSpeedLimits({ alt_enabled: !altOn })}
        >
          <Turtle size={16} aria-hidden="true" />
        </button>
        <button
          type="button"
          className="tor-status-button"
          title="Global download speed limit"
          aria-label={`Download speed ${formatSpeed(transfer.dl_info_speed, "0 B/s")}, set global download limit`}
          onClick={() => setLimitDialog("global-dl")}
        >
          <ArrowDown size={14} aria-hidden="true" className="tor-dl" />
          <span>
            {formatSpeed(transfer.dl_info_speed, "0 B/s")}
            {transfer.dl_rate_limit
              ? ` (${formatSpeed(transfer.dl_rate_limit)})`
              : ""}
          </span>
          <span className="rs-muted">
            [{formatBytes(transfer.dl_info_data)}]
          </span>
        </button>
        <button
          type="button"
          className="tor-status-button"
          title="Global upload speed limit"
          aria-label={`Upload speed ${formatSpeed(transfer.up_info_speed, "0 B/s")}, set global upload limit`}
          onClick={() => setLimitDialog("global-ul")}
        >
          <ArrowUp size={14} aria-hidden="true" className="tor-ul" />
          <span>
            {formatSpeed(transfer.up_info_speed, "0 B/s")}
            {transfer.up_rate_limit
              ? ` (${formatSpeed(transfer.up_rate_limit)})`
              : ""}
          </span>
          <span className="rs-muted">
            [{formatBytes(transfer.up_info_data)}]
          </span>
        </button>
      </footer>

      <AddLinksDialog
        open={addLinksOpen}
        onOpenChange={setAddLinksOpen}
        onSubmit={addLinks}
      />
      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        torrents={selected}
        onConfirm={(deleteFiles) =>
          act(
            "remove",
            { delete_files: deleteFiles, reviewed: true },
            deleteFiles
              ? "Torrents and their files deleted."
              : "Torrents removed.",
          )
        }
      />
      <SpeedLimitDialog
        open={limitDialog === "dl" || limitDialog === "ul"}
        onOpenChange={(open) => !open && setLimitDialog(null)}
        title={
          limitDialog === "ul"
            ? "Torrent Upload Speed Limiting"
            : "Torrent Download Speed Limiting"
        }
        current={singleLimit((t) =>
          Math.max(0, limitDialog === "ul" ? t.up_limit : t.dl_limit),
        )}
        onSubmit={(limit) =>
          act(
            limitDialog === "ul" ? "set_upload_limit" : "set_download_limit",
            {
              limit,
            },
          )
        }
      />
      <SpeedLimitDialog
        open={limitDialog === "global-dl" || limitDialog === "global-ul"}
        onOpenChange={(open) => !open && setLimitDialog(null)}
        title={
          limitDialog === "global-ul"
            ? "Global Upload Speed Limit"
            : "Global Download Speed Limit"
        }
        current={
          (limitDialog === "global-ul"
            ? transfer.up_rate_limit
            : transfer.dl_rate_limit) ?? 0
        }
        note={
          altOn
            ? "Alternative speed limits are on, so this changes the alternative limit."
            : undefined
        }
        onSubmit={(limit) =>
          setSpeedLimits(
            limitDialog === "global-ul"
              ? { upload_limit: limit }
              : { download_limit: limit },
          )
        }
      />
      <ShareLimitDialog
        open={shareOpen}
        onOpenChange={setShareOpen}
        torrents={selected}
        onSubmit={(ratio, minutes) =>
          act("set_share_limits", {
            ratio_limit: ratio,
            seeding_time_limit: minutes,
          })
        }
      />
      <SharePolicyDialog
        open={sharePolicyOpen}
        onOpenChange={setSharePolicyOpen}
        policy={sharePolicy.data}
        onSave={saveSharePolicy}
        onRun={runSharePolicy}
      />
    </div>
  );
}
