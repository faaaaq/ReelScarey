import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { Upload } from "lucide-react";
import { api, messageFor } from "../api";
import { Alert, FieldSet, Label, PageBody, Table } from "../ui";
import { PageHeading } from "../components";
import "./Admin.css";

type PreviewItem = {
  kind: "movie" | "series";
  tmdb_id?: number;
  tvdb_id?: number;
  title: string;
  year?: number;
  monitored: boolean;
  date?: string;
  release_kind?: string;
  in_library: boolean;
  error?: string;
};

type Preview = { source: "radarr" | "sonarr"; items: PreviewItem[] };
type ApplyResult = {
  added: number;
  skipped: number;
  errors: { title: string; message: string }[];
};

function itemKey(item: PreviewItem) {
  return `${item.kind}:${item.tmdb_id || item.tvdb_id || item.title}`;
}

export function ArrBackupImport() {
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState<"" | "review" | "apply">("");
  const [error, setError] = useState("");
  const [result, setResult] = useState<ApplyResult | null>(null);

  async function review() {
    if (!file || busy) return;
    setBusy("review");
    setError("");
    setResult(null);
    setPreview(null);
    try {
      const next = await api<Preview>("/library/import/backup/preview", {
        method: "POST",
        body: file,
        headers: { "Content-Type": "application/zip" },
        signal: AbortSignal.timeout(180000),
      });
      setPreview(next);
      const today = new Date().toISOString().slice(0, 10);
      setSelected(
        new Set(
          next.items
            .filter(
              (item) =>
                !item.in_library &&
                !item.error &&
                Boolean(item.tmdb_id || item.tvdb_id) &&
                (!item.date || item.date >= today),
            )
            .map(itemKey),
        ),
      );
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }

  async function apply() {
    if (!preview || !selected.size || busy) return;
    setBusy("apply");
    setError("");
    setResult(null);
    try {
      const items = preview.items
        .filter((item) => selected.has(itemKey(item)))
        .map((item) => ({
          kind: item.kind,
          tmdb_id: item.tmdb_id ?? 0,
          tvdb_id: item.tvdb_id ?? 0,
          monitored: item.monitored,
        }));
      const applied = await api<ApplyResult>("/library/import/backup/apply", {
        method: "POST",
        body: JSON.stringify({ items }),
        signal: AbortSignal.timeout(900000),
      });
      setResult(applied);
      setPreview({
        ...preview,
        items: preview.items.map((item) =>
          selected.has(itemKey(item)) ? { ...item, in_library: true } : item,
        ),
      });
      setSelected(new Set());
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }

  function toggle(item: PreviewItem) {
    const key = itemKey(item);
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  const candidates =
    preview?.items.filter(
      (item) =>
        !item.in_library && !item.error && (item.tmdb_id || item.tvdb_id),
    ).length ?? 0;

  return (
    <PageBody>
      <PageHeading title="Sonarr / Radarr Import" />
      <FieldSet
        legend="Import titles from a backup or calendar"
        description="Adds the saved title list and monitored choice from a Radarr/Sonarr backup ZIP or calendar export. Indexers, download clients, paths, API keys, quality profiles and other credentials are never read or imported."
      >
        <div className="rs-stack">
          <label className="rs-field">
            <span>Backup ZIP or calendar .ics</span>
            <input
              type="file"
              accept=".zip,.ics,application/zip,text/calendar"
              aria-label="Sonarr or Radarr backup or calendar"
              onChange={(event) => {
                setFile(event.target.files?.[0] ?? null);
                setPreview(null);
                setResult(null);
                setError("");
              }}
            />
          </label>
          <p className="rs-form-help">
            The file is processed in the server's temporary directory and never
            stored. Maximum size 256 MiB for a backup ZIP or 5 MiB for a
            calendar. A Radarr backup or calendar keeps TMDb movie identity and
            the monitored flag. A Sonarr backup matches TVDB to TMDb and starts
            shows with future-only monitoring; Sonarr calendar per-season
            choices are not imported.
          </p>
          <div className="rs-form-actions">
            <Button
              type="button"
              disabled={!file || busy !== ""}
              onClick={() => void review()}
            >
              <Upload size={15} aria-hidden="true" />
              {busy === "review" ? "Reading backup…" : "Review backup"}
            </Button>
          </div>
          {error && <Alert kind="danger">{error}</Alert>}
        </div>
      </FieldSet>

      {preview && (
        <FieldSet
          legend={`${
            preview.source.startsWith("radarr")
              ? "Radarr"
              : preview.source.startsWith("sonarr")
                ? "Sonarr"
                : "Calendar"
          } ${preview.source.endsWith("-ical") ? "calendar" : "backup"}`}
          description={`${preview.items.length} titles in the ${
            preview.source.endsWith("-ical") ? "calendar" : "backup"
          }. ${candidates} can be added. Unmatched or already saved titles stay unselected.`}
          actions={
            <Button
              type="button"
              disabled={!selected.size || busy !== ""}
              onClick={() => void apply()}
            >
              {busy === "apply"
                ? "Adding titles…"
                : `Add ${selected.size} selected`}
            </Button>
          }
        >
          <div className="rs-stack">
            {preview.items.length === 0 ? (
              <p className="rs-muted">The file contains no titles.</p>
            ) : (
              <Table label="Import titles" className="adm-settings-table">
                <thead>
                  <tr>
                    <th scope="col" className="is-shrink">
                      Add
                    </th>
                    <th scope="col">Title</th>
                    <th scope="col" className="is-shrink">
                      Date
                    </th>
                    <th scope="col" className="is-shrink">
                      Year
                    </th>
                    <th scope="col" className="is-shrink">
                      Monitored
                    </th>
                    <th scope="col">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.items.map((item) => {
                    const unavailable = Boolean(item.error) || !item.tmdb_id;
                    return (
                      <tr key={itemKey(item)}>
                        <td>
                          <input
                            type="checkbox"
                            aria-label={`Add ${item.title}`}
                            checked={selected.has(itemKey(item))}
                            disabled={item.in_library || unavailable}
                            onChange={() => toggle(item)}
                          />
                        </td>
                        <td>{item.title}</td>
                        <td>{item.date ?? "—"}</td>
                        <td>{item.year ?? "—"}</td>
                        <td>{item.monitored ? "Yes" : "No"}</td>
                        <td>
                          {item.error ? (
                            <Label kind="danger" title={item.error}>
                              Unmatched
                            </Label>
                          ) : item.in_library ? (
                            <Label kind="success">In library</Label>
                          ) : (
                            <Label kind="info">Will add</Label>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </Table>
            )}
            {result && (
              <Alert kind="success" role="status">
                Added {result.added} title
                {result.added === 1 ? "" : "s"}; skipped {result.skipped}
                {result.errors.length > 0 &&
                  `; ${result.errors.length} failed. ${result.errors
                    .slice(0, 5)
                    .map((failure) => `${failure.title}: ${failure.message}`)
                    .join(" ")}`}
                .
              </Alert>
            )}
          </div>
        </FieldSet>
      )}
    </PageBody>
  );
}
