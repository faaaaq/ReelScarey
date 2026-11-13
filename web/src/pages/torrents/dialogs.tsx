import { useId, useState, type FormEvent } from "react";
import { Button } from "@radix-ui/themes";
import { messageFor } from "../../api";
import { Alert, Check, Modal, ModalClose, formatSpeed } from "../../ui";
import {
  KiB,
  formatSeedingMinutes,
  originOf,
  shareActionLabels,
  type QueueLimits,
  type ShareAction,
  type SharePolicy,
  type SharePolicyRun,
  type Torrent,
} from "./model";

export const maxMagnetLines = 50;

export type AddFailure = { input: string; message: string };

export function AddLinksDialog({
  open,
  onOpenChange,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Adds the links one by one and returns those that failed. */
  onSubmit: (links: string[]) => Promise<AddFailure[]>;
}) {
  const formId = useId();
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [failures, setFailures] = useState<AddFailure[]>([]);
  const [problem, setProblem] = useState("");
  const lines = text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);

  function change(next: boolean) {
    if (busy) return;
    if (!next) {
      setFailures([]);
      setProblem("");
    }
    onOpenChange(next);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setProblem("");
    setFailures([]);
    if (lines.length === 0) return;
    if (lines.length > maxMagnetLines) {
      setProblem(`Add at most ${maxMagnetLines} links at a time.`);
      return;
    }
    const invalid = lines.filter((line) => !/^magnet:\?/i.test(line));
    if (invalid.length) {
      setProblem(
        "Only magnet links are accepted. The server never downloads torrent URLs; use Add Torrent File instead.",
      );
      return;
    }
    setBusy(true);
    try {
      const failed = await onSubmit(lines);
      if (failed.length === 0) {
        setText("");
        setBusy(false);
        onOpenChange(false);
        return;
      }
      setFailures(failed);
      setText(failed.map((f) => f.input).join("\n"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={change}
      title="Add Torrent Links"
      footer={
        <>
          <ModalClose>
            <Button variant="soft" disabled={busy}>
              Cancel
            </Button>
          </ModalClose>
          <Button type="submit" form={formId} disabled={busy || !lines.length}>
            {busy ? "Adding…" : "Download"}
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={submit} className="tor-dialog-form">
        <label className="tor-field" htmlFor={`${formId}-links`}>
          <span>One magnet link per line (up to {maxMagnetLines})</span>
          <textarea
            id={`${formId}-links`}
            className="tor-textarea"
            rows={8}
            value={text}
            spellCheck={false}
            placeholder="magnet:?xt=urn:btih:…"
            onChange={(e) => setText(e.target.value)}
          />
        </label>
        <p className="rs-form-help">
          Torrents added here are saved under the manual downloads folder and
          are not imported into the library.
        </p>
        {problem && <Alert kind="danger">{problem}</Alert>}
        {failures.length > 0 && (
          <Alert
            kind="danger"
            title={`${failures.length} of the links could not be added`}
          >
            <ul className="tor-failures">
              {failures.map((f, i) => (
                <li key={`${f.input}-${i}`}>
                  <code>{shorten(f.input)}</code>: {f.message}
                </li>
              ))}
            </ul>
          </Alert>
        )}
      </form>
    </Modal>
  );
}

function shorten(value: string) {
  return value.length > 72 ? `${value.slice(0, 69)}…` : value;
}

export function DeleteDialog({
  torrents,
  open,
  onOpenChange,
  onConfirm,
}: {
  torrents: Torrent[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (deleteFiles: boolean) => Promise<boolean>;
}) {
  const [deleteFiles, setDeleteFiles] = useState(false);
  const [busy, setBusy] = useState(false);
  const jobs = torrents.filter((t) => originOf(t) === "job").length;
  const shown = torrents.slice(0, 8);

  function change(next: boolean) {
    if (busy) return;
    if (!next) setDeleteFiles(false);
    onOpenChange(next);
  }

  async function confirm() {
    setBusy(true);
    const ok = await onConfirm(deleteFiles);
    setBusy(false);
    if (ok) {
      setDeleteFiles(false);
      onOpenChange(false);
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={change}
      size="small"
      title={
        torrents.length === 1
          ? "Delete torrent"
          : `Delete ${torrents.length} torrents`
      }
      footer={
        <>
          <ModalClose>
            <Button variant="soft" disabled={busy}>
              Cancel
            </Button>
          </ModalClose>
          <Button
            color="red"
            disabled={busy || !torrents.length}
            onClick={confirm}
          >
            {busy ? "Deleting…" : "Delete"}
          </Button>
        </>
      }
    >
      <ul className="tor-delete-list" aria-label="Torrents to delete">
        {shown.map((t) => (
          <li key={t.hash} title={t.name}>
            {t.name}
          </li>
        ))}
        {torrents.length > shown.length && (
          <li className="rs-muted">
            and {torrents.length - shown.length} more
          </li>
        )}
      </ul>
      {jobs > 0 && (
        <Alert kind="warning" title="Added by ReelScarey">
          {jobs === torrents.length
            ? "These torrents belong to ReelScarey downloads."
            : `${jobs} of these torrents belong to ReelScarey downloads.`}{" "}
          Deleting them can break a pending import or a seeding source.
        </Alert>
      )}
      <Check
        label="Also delete files on disk"
        checked={deleteFiles}
        onChange={setDeleteFiles}
        disabled={busy}
      />
    </Modal>
  );
}

export function SpeedLimitDialog({
  open,
  onOpenChange,
  title,
  current,
  note,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** Current limit in bytes/s; null when the selection differs. */
  current: number | null;
  note?: string;
  onSubmit: (bytesPerSecond: number) => Promise<boolean>;
}) {
  const formId = useId();
  const initial =
    current && current > 0 ? String(Math.round(current / KiB)) : "";
  const [value, setValue] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const [lastOpen, setLastOpen] = useState(open);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) {
      setValue(initial);
      setProblem("");
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    const trimmed = value.trim();
    const kib = trimmed === "" ? 0 : Number(trimmed);
    if (!Number.isInteger(kib) || kib < 0 || kib * KiB > 2_000_000_000) {
      setProblem(
        "Enter a whole number of KiB/s up to 1953125, or 0 for no limit.",
      );
      return;
    }
    setBusy(true);
    const ok = await onSubmit(kib * KiB);
    setBusy(false);
    if (ok) onOpenChange(false);
  }

  return (
    <Modal
      open={open}
      onOpenChange={(next) => !busy && onOpenChange(next)}
      size="small"
      title={title}
      footer={
        <>
          <ModalClose>
            <Button variant="soft" disabled={busy}>
              Cancel
            </Button>
          </ModalClose>
          <Button type="submit" form={formId} disabled={busy}>
            Save
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={submit} className="tor-dialog-form">
        <label className="tor-field" htmlFor={`${formId}-limit`}>
          <span>Limit (KiB/s)</span>
          <input
            id={`${formId}-limit`}
            type="number"
            min={0}
            step={1}
            inputMode="numeric"
            value={value}
            placeholder="∞"
            onChange={(e) => setValue(e.target.value)}
          />
        </label>
        <p className="rs-form-help">
          Current:{" "}
          {current === null
            ? "differs between the selected torrents"
            : current > 0
              ? formatSpeed(current)
              : "unlimited"}
          . Leave empty or enter 0 for no limit.
        </p>
        {note && <p className="rs-form-help">{note}</p>}
        {problem && <Alert kind="danger">{problem}</Alert>}
      </form>
    </Modal>
  );
}

type ShareMode = "global" | "none" | "custom";

export function ShareLimitDialog({
  open,
  onOpenChange,
  torrents,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  torrents: Torrent[];
  onSubmit: (ratio: number, minutes: number) => Promise<boolean>;
}) {
  const formId = useId();
  const first = torrents[0];
  const same =
    first &&
    torrents.every(
      (t) =>
        t.ratio_limit === first.ratio_limit &&
        t.seeding_time_limit === first.seeding_time_limit,
    );
  const [mode, setMode] = useState<ShareMode>("global");
  const [useRatio, setUseRatio] = useState(false);
  const [ratio, setRatio] = useState("1.00");
  const [useTime, setUseTime] = useState(false);
  const [minutes, setMinutes] = useState("1440");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const [lastOpen, setLastOpen] = useState(open);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) {
      setProblem("");
      const r = same ? first.ratio_limit : -2;
      const m = same ? first.seeding_time_limit : -2;
      if (r === -2 && m === -2) setMode("global");
      else if (r === -1 && m === -1) setMode("none");
      else setMode("custom");
      setUseRatio(r >= 0);
      setRatio(r >= 0 ? r.toFixed(2) : "1.00");
      setUseTime(m >= 0);
      setMinutes(m >= 0 ? String(m) : "1440");
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setProblem("");
    let r = -2;
    let m = -2;
    if (mode === "none") {
      r = -1;
      m = -1;
    } else if (mode === "custom") {
      r = useRatio ? Number(ratio) : -1;
      m = useTime ? Number(minutes) : -1;
      if (useRatio && !(Number.isFinite(r) && r >= 0 && r <= 9998)) {
        setProblem("Ratio must be between 0 and 9998.");
        return;
      }
      if (useTime && !(Number.isInteger(m) && m >= 0 && m <= 525600)) {
        setProblem(
          "Seeding time must be a whole number of minutes up to 525600.",
        );
        return;
      }
    }
    setBusy(true);
    const ok = await onSubmit(r, m);
    setBusy(false);
    if (ok) onOpenChange(false);
  }

  const name = `${formId}-mode`;
  return (
    <Modal
      open={open}
      onOpenChange={(next) => !busy && onOpenChange(next)}
      size="small"
      title="Torrent Upload/Download Ratio Limiting"
      footer={
        <>
          <ModalClose>
            <Button variant="soft" disabled={busy}>
              Cancel
            </Button>
          </ModalClose>
          <Button type="submit" form={formId} disabled={busy}>
            Save
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={submit} className="tor-dialog-form">
        <fieldset className="tor-radio-group">
          <legend className="sr-only">Share limit</legend>
          <label className="rs-check">
            <input
              type="radio"
              name={name}
              checked={mode === "global"}
              onChange={() => setMode("global")}
            />
            <span>Use global share limit</span>
          </label>
          <label className="rs-check">
            <input
              type="radio"
              name={name}
              checked={mode === "none"}
              onChange={() => setMode("none")}
            />
            <span>Set no share limit</span>
          </label>
          <label className="rs-check">
            <input
              type="radio"
              name={name}
              checked={mode === "custom"}
              onChange={() => setMode("custom")}
            />
            <span>Set share limit to</span>
          </label>
        </fieldset>
        <div className="tor-share-custom">
          <label className="rs-check">
            <input
              type="checkbox"
              checked={useRatio}
              disabled={mode !== "custom"}
              onChange={(e) => setUseRatio(e.target.checked)}
            />
            <span>ratio</span>
          </label>
          <input
            type="number"
            aria-label="Ratio limit"
            min={0}
            max={9998}
            step={0.05}
            value={ratio}
            disabled={mode !== "custom" || !useRatio}
            onChange={(e) => setRatio(e.target.value)}
          />
          <label className="rs-check">
            <input
              type="checkbox"
              checked={useTime}
              disabled={mode !== "custom"}
              onChange={(e) => setUseTime(e.target.checked)}
            />
            <span>minutes</span>
          </label>
          <input
            type="number"
            aria-label="Seeding time limit in minutes"
            min={0}
            max={525600}
            step={1}
            value={minutes}
            disabled={mode !== "custom" || !useTime}
            onChange={(e) => setMinutes(e.target.value)}
          />
        </div>
        {!same && torrents.length > 1 && (
          <p className="rs-form-help">
            The selected torrents currently have different limits.
          </p>
        )}
        {problem && <Alert kind="danger">{problem}</Alert>}
      </form>
    </Modal>
  );
}

/**
 * Application-enforced seeding limit for the built-in client. qBittorrent
 * itself only pauses on its share limits, so ReelScarey evaluates the client's
 * reported ratio/time and can pause, remove or remove-and-delete.
 */
export function SharePolicyDialog({
  open,
  onOpenChange,
  policy,
  onSave,
  onRun,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  policy: SharePolicy | undefined;
  onSave: (next: SharePolicy) => Promise<void>;
  onRun: () => Promise<SharePolicyRun>;
}) {
  const formId = useId();
  const [enabled, setEnabled] = useState(false);
  const [action, setAction] = useState<ShareAction>("pause");
  const [ratio, setRatio] = useState("2.0");
  const [hours, setHours] = useState("0");
  const [minutes, setMinutes] = useState("0");
  const [includeJobs, setIncludeJobs] = useState(false);
  const [busy, setBusy] = useState(false);
  const [running, setRunning] = useState(false);
  const [problem, setProblem] = useState("");
  const [runResult, setRunResult] = useState<SharePolicyRun | null>(null);
  const [lastOpen, setLastOpen] = useState(open);
  const [seeded, setSeeded] = useState(false);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (!open) setSeeded(false);
  }
  // Populate once per opening, including when the policy finishes loading
  // after the dialog was opened.
  if (open && policy && !seeded) {
    setEnabled(policy.enabled);
    setAction(policy.action);
    setRatio(policy.max_ratio > 0 ? String(policy.max_ratio) : "0");
    setHours(String(Math.floor(policy.max_seeding_minutes / 60)));
    setMinutes(String(policy.max_seeding_minutes % 60));
    setIncludeJobs(policy.include_reelscarey);
    setProblem("");
    setRunResult(null);
    setSeeded(true);
  }

  function parse(): { ratio: number; totalMinutes: number } | string {
    const ratioValue = ratio.trim() === "" ? 0 : Number(ratio);
    if (!Number.isFinite(ratioValue) || ratioValue < 0 || ratioValue > 1000) {
      return "Enter a maximum ratio between 0 and 1000, or 0 to disable it.";
    }
    if (ratioValue > 0 && ratioValue < 0.1) {
      return "The maximum ratio must be at least 0.1, or 0 to disable it.";
    }
    const h = hours.trim() === "" ? 0 : Number(hours);
    const m = minutes.trim() === "" ? 0 : Number(minutes);
    if (!Number.isInteger(h) || h < 0 || h > 8760) {
      return "Enter a whole number of hours between 0 and 8760.";
    }
    if (!Number.isInteger(m) || m < 0 || m > 59) {
      return "Enter a whole number of minutes between 0 and 59.";
    }
    const totalMinutes = h * 60 + m;
    if (enabled && ratioValue === 0 && totalMinutes === 0) {
      return "An enabled seeding limit needs a ratio, a seeding time, or both.";
    }
    return { ratio: ratioValue, totalMinutes };
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setProblem("");
    if (!policy) return;
    const parsed = parse();
    if (typeof parsed === "string") {
      setProblem(parsed);
      return;
    }
    setBusy(true);
    try {
      await onSave({
        ...policy,
        enabled,
        action,
        max_ratio: parsed.ratio,
        max_seeding_minutes: parsed.totalMinutes,
        include_reelscarey: includeJobs,
      });
      onOpenChange(false);
    } catch (e) {
      setProblem(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  async function run() {
    setProblem("");
    setRunning(true);
    try {
      setRunResult(await onRun());
    } catch (e) {
      setProblem(messageFor(e));
    } finally {
      setRunning(false);
    }
  }

  const working = busy || running;

  return (
    <Modal
      open={open}
      onOpenChange={(next) => !working && onOpenChange(next)}
      title="Seeding policy"
      footer={
        <>
          <ModalClose>
            <Button variant="soft" disabled={working}>
              Cancel
            </Button>
          </ModalClose>
          <Button
            variant="soft"
            disabled={working || !policy}
            onClick={() => void run()}
          >
            {running ? "Running…" : "Run now"}
          </Button>
          <Button type="submit" form={formId} disabled={working || !policy}>
            {busy ? "Saving…" : "Save"}
          </Button>
        </>
      }
    >
      {policy ? (
        <form id={formId} onSubmit={submit} className="tor-dialog-form">
          <Check
            label="Enforce an automatic seeding limit"
            description="ReelScarey checks this client's completed torrents about once a minute and applies the limit itself."
            checked={enabled}
            onChange={setEnabled}
            disabled={working}
          />
          <label className="tor-field" htmlFor={`${formId}-action`}>
            <span>When a limit is reached</span>
            <select
              id={`${formId}-action`}
              value={action}
              disabled={working}
              onChange={(e) => setAction(e.target.value as ShareAction)}
            >
              <option value="pause">Pause the torrent</option>
              <option value="remove">Remove the torrent</option>
              <option value="remove_and_delete">
                Remove the torrent and delete its files
              </option>
            </select>
          </label>
          <div className="tor-policy-grid">
            <label className="tor-field" htmlFor={`${formId}-ratio`}>
              <span>Maximum ratio</span>
              <input
                id={`${formId}-ratio`}
                type="number"
                min={0}
                max={1000}
                step={0.1}
                inputMode="decimal"
                value={ratio}
                disabled={working}
                onChange={(e) => setRatio(e.target.value)}
              />
            </label>
            <div className="tor-field">
              <span>Maximum seeding time</span>
              <div className="tor-policy-time">
                <input
                  aria-label="Seeding time hours"
                  type="number"
                  min={0}
                  max={8760}
                  step={1}
                  inputMode="numeric"
                  value={hours}
                  disabled={working}
                  onChange={(e) => setHours(e.target.value)}
                />
                <span>h</span>
                <input
                  aria-label="Seeding time minutes"
                  type="number"
                  min={0}
                  max={59}
                  step={1}
                  inputMode="numeric"
                  value={minutes}
                  disabled={working}
                  onChange={(e) => setMinutes(e.target.value)}
                />
                <span>min</span>
              </div>
            </div>
          </div>
          <p className="rs-form-help">
            Enter 0 to disable the ratio or the time rule. At least one rule is
            required while the limit is on. Currently:{" "}
            {shareActionLabels[action].toLowerCase()}.
          </p>
          <Check
            label="Include ReelScarey downloads"
            description="Their torrent entries can be paused or removed when the limit is reached, but ReelScarey never deletes their files: the reviewed import and seeding source must survive."
            checked={includeJobs}
            onChange={setIncludeJobs}
            disabled={working}
          />
          {problem && <Alert kind="danger">{problem}</Alert>}
          {runResult && (
            <p className="rs-form-help" role="status">
              Checked {runResult.checked}{" "}
              {runResult.checked === 1 ? "torrent" : "torrents"}; acted on{" "}
              {runResult.acted}.
            </p>
          )}
          <p className="rs-form-help">
            Current saved limit:{" "}
            {policy.enabled
              ? [
                  policy.max_ratio > 0
                    ? `ratio ${policy.max_ratio.toFixed(1)}`
                    : "",
                  policy.max_seeding_minutes > 0
                    ? formatSeedingMinutes(policy.max_seeding_minutes)
                    : "",
                  shareActionLabels[policy.action].toLowerCase(),
                ]
                  .filter(Boolean)
                  .join(" · ")
              : "off"}
            .
          </p>
        </form>
      ) : (
        <p className="rs-form-help" role="status">
          Loading the saved seeding policy…
        </p>
      )}
    </Modal>
  );
}

export function QueueLimitsDialog({
  open,
  onOpenChange,
  initial,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initial: QueueLimits;
  onSubmit: (limits: QueueLimits) => Promise<boolean>;
}) {
  const formId = useId();
  const [enabled, setEnabled] = useState(initial.queueing_enabled);
  const [downloads, setDownloads] = useState(
    String(initial.max_active_downloads),
  );
  const [uploads, setUploads] = useState(String(initial.max_active_uploads));
  const [torrents, setTorrents] = useState(String(initial.max_active_torrents));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const [lastOpen, setLastOpen] = useState(open);
  if (open !== lastOpen) {
    setLastOpen(open);
    if (open) {
      setEnabled(initial.queueing_enabled);
      setDownloads(String(initial.max_active_downloads));
      setUploads(String(initial.max_active_uploads));
      setTorrents(String(initial.max_active_torrents));
      setProblem("");
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    const values = [downloads, uploads, torrents].map((value) =>
      Number(value.trim()),
    );
    if (
      values.some(
        (value) => !Number.isInteger(value) || value < 1 || value > 100,
      )
    ) {
      setProblem("Choose whole-number limits between 1 and 100.");
      return;
    }
    setBusy(true);
    const ok = await onSubmit({
      queueing_enabled: enabled,
      max_active_downloads: values[0],
      max_active_uploads: values[1],
      max_active_torrents: values[2],
      revision: initial.revision,
    });
    setBusy(false);
    if (ok) onOpenChange(false);
    else setProblem("The queue limits were not saved.");
  }

  return (
    <Modal
      open={open}
      onOpenChange={(next) => !busy && onOpenChange(next)}
      size="small"
      title="Torrent queue limits"
      description="Control how many torrents the built-in qBittorrent client keeps active. These values survive client and application restarts."
      footer={
        <>
          <ModalClose>
            <Button variant="soft" disabled={busy}>
              Cancel
            </Button>
          </ModalClose>
          <Button type="submit" form={formId} disabled={busy}>
            Save
          </Button>
        </>
      }
    >
      <form id={formId} onSubmit={submit} className="tor-dialog-form">
        <Check
          label="Enable torrent queueing"
          checked={enabled}
          onChange={setEnabled}
        />
        <p className="rs-form-help">
          When queueing is off, every torrent may run at once. The limits below
          are kept for when queueing is enabled again.
        </p>
        <label className="tor-field" htmlFor={`${formId}-downloads`}>
          <span>Maximum active downloads</span>
          <input
            id={`${formId}-downloads`}
            type="number"
            min={1}
            max={100}
            step={1}
            inputMode="numeric"
            value={downloads}
            onChange={(event) => setDownloads(event.target.value)}
          />
        </label>
        <label className="tor-field" htmlFor={`${formId}-uploads`}>
          <span>Maximum active uploads</span>
          <input
            id={`${formId}-uploads`}
            type="number"
            min={1}
            max={100}
            step={1}
            inputMode="numeric"
            value={uploads}
            onChange={(event) => setUploads(event.target.value)}
          />
        </label>
        <label className="tor-field" htmlFor={`${formId}-torrents`}>
          <span>Maximum active torrents overall</span>
          <input
            id={`${formId}-torrents`}
            type="number"
            min={1}
            max={100}
            step={1}
            inputMode="numeric"
            value={torrents}
            onChange={(event) => setTorrents(event.target.value)}
          />
        </label>
        {problem && <Alert kind="danger">{problem}</Alert>}
      </form>
    </Modal>
  );
}
