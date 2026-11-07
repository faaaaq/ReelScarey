import { useId, useRef, useState, type FormEvent } from "react";
import { Button } from "@radix-ui/themes";
import { FileUp } from "lucide-react";
import { Alert, Check, Modal, ModalClose, formatBytes } from "../../ui";
import { KiB, maxNZB, type UsenetItem } from "./model";

export type NZBFailure = { name: string; message: string };

export function AddNZBDialog({
  open,
  onOpenChange,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (files: File[]) => Promise<NZBFailure[]>;
}) {
  const formId = useId();
  const input = useRef<HTMLInputElement>(null);
  const [files, setFiles] = useState<File[]>([]);
  const [busy, setBusy] = useState(false);
  const [failures, setFailures] = useState<NZBFailure[]>([]);
  const [problem, setProblem] = useState("");

  function change(next: boolean) {
    if (busy) return;
    if (!next) {
      setFiles([]);
      setFailures([]);
      setProblem("");
    }
    onOpenChange(next);
  }

  function choose(list: FileList | null) {
    const picked = [...(list ?? [])];
    if (input.current) input.current.value = "";
    const tooBig = picked.filter((f) => f.size > maxNZB);
    if (tooBig.length) {
      setProblem(
        `Each NZB must be at most 4 MiB. Too large: ${tooBig.map((f) => f.name).join(", ")}.`,
      );
      return;
    }
    if (picked.some((f) => !f.name.toLowerCase().endsWith(".nzb"))) {
      setProblem(
        "Choose .nzb files only. The server never downloads an NZB URL.",
      );
      return;
    }
    setProblem("");
    setFiles(picked);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!files.length) return;
    setProblem("");
    setFailures([]);
    setBusy(true);
    try {
      const failed = await onSubmit(files);
      if (failed.length === 0) {
        setFiles([]);
        setBusy(false);
        onOpenChange(false);
        return;
      }
      setFailures(failed);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={change}
      title="Add NZB"
      footer={
        <>
          <ModalClose>
            <Button variant="soft" disabled={busy}>
              Cancel
            </Button>
          </ModalClose>
          <Button type="submit" form={formId} disabled={busy || !files.length}>
            {busy ? "Adding…" : "Add to queue"}
          </Button>
        </>
      }
    >
      <form id={formId} className="use-stack" onSubmit={submit}>
        <label className="file-button">
          <input
            ref={input}
            type="file"
            accept=".nzb,application/x-nzb"
            multiple
            disabled={busy}
            aria-label="NZB files"
            onChange={(e) => choose(e.target.files)}
          />
          <FileUp size={16} aria-hidden="true" /> Choose NZB files
        </label>
        {files.length > 0 && (
          <ul className="use-file-list" aria-label="Files to add">
            {files.map((f) => (
              <li key={`${f.name}-${f.size}`}>
                {f.name} · {formatBytes(f.size)}
              </li>
            ))}
          </ul>
        )}
        <p className="rs-form-help">
          Files are uploaded directly to the bundled SABnzbd instance under its
          dedicated <code>reelscarey</code> category. They are not imported into
          the library and no remote URL is fetched.
        </p>
        {problem && <Alert kind="danger">{problem}</Alert>}
        {failures.length > 0 && (
          <Alert
            kind="danger"
            title={`${failures.length} file(s) were not added`}
          >
            <ul className="use-file-list">
              {failures.map((f, i) => (
                <li key={i}>
                  {f.name}: {f.message}
                </li>
              ))}
            </ul>
          </Alert>
        )}
      </form>
    </Modal>
  );
}

export function DeleteQueueDialog({
  open,
  onOpenChange,
  items,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: UsenetItem[];
  onConfirm: (deleteFiles: boolean) => Promise<boolean>;
}) {
  const [deleteFiles, setDeleteFiles] = useState(false);
  const [reviewed, setReviewed] = useState(false);
  const [busy, setBusy] = useState(false);
  const shown = items.slice(0, 8);

  function change(next: boolean) {
    if (busy) return;
    if (!next) {
      setDeleteFiles(false);
      setReviewed(false);
    }
    onOpenChange(next);
  }

  async function confirm() {
    if (!reviewed) return;
    setBusy(true);
    const ok = await onConfirm(deleteFiles);
    setBusy(false);
    if (ok) {
      setDeleteFiles(false);
      setReviewed(false);
      onOpenChange(false);
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={change}
      size="small"
      title={
        items.length === 1
          ? "Delete queued job"
          : `Delete ${items.length} queued jobs`
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
            disabled={busy || !reviewed || !items.length}
            onClick={() => void confirm()}
          >
            {busy ? "Deleting…" : "Delete jobs"}
          </Button>
        </>
      }
    >
      <ul className="use-file-list" aria-label="Jobs to delete">
        {shown.map((item) => (
          <li key={item.id} title={item.name}>
            {item.name}
          </li>
        ))}
        {items.length > shown.length && (
          <li className="rs-muted">and {items.length - shown.length} more</li>
        )}
      </ul>
      <Alert kind="warning" title="This removes jobs from SABnzbd">
        ReelScarey does not delete completed library files. Unfinished jobs that
        still belong to a ReelScarey download are refused; use Activity for
        those.
      </Alert>
      <Check
        label="Also delete the job's downloaded files in SABnzbd"
        checked={deleteFiles}
        onChange={setDeleteFiles}
        disabled={busy}
      />
      <Check
        label="I reviewed these jobs"
        checked={reviewed}
        onChange={setReviewed}
        disabled={busy}
      />
    </Modal>
  );
}

export function UsenetSpeedLimitDialog({
  open,
  onOpenChange,
  current,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  current: number;
  onSubmit: (bytesPerSecond: number) => Promise<boolean>;
}) {
  const formId = useId();
  const initial = current > 0 ? String(Math.round(current / KiB)) : "";
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
      title="Download speed limit"
      footer={
        <>
          <ModalClose>
            <Button variant="soft" disabled={busy}>
              Cancel
            </Button>
          </ModalClose>
          <Button type="submit" form={formId} disabled={busy}>
            Save limit
          </Button>
        </>
      }
    >
      <form id={formId} className="use-stack" onSubmit={submit}>
        <label className="use-field">
          <span>Limit in KiB/s (0 removes the limit)</span>
          <input
            type="number"
            min={0}
            max={1953125}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            autoFocus
          />
        </label>
        <p className="rs-form-help">
          This sets SABnzbd's absolute byte-per-second limit for the running
          daemon. SABnzbd's own saved percentage is not changed and applies
          again after a restart.
        </p>
        {problem && <Alert kind="danger">{problem}</Alert>}
      </form>
    </Modal>
  );
}
