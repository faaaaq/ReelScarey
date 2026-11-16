import { useCallback, useEffect, useState } from "react";
import { Button } from "@radix-ui/themes";
import { Alert, Modal, ModalClose } from "../../ui";
import { api, messageFor } from "../../api";

type DirectImportEntry = {
  name: string;
  path: string;
  dir: boolean;
  size_bytes?: number;
  mtime_ns?: number;
};

type DirectImportBrowse = {
  roots: string[];
  dir: string;
  items: DirectImportEntry[];
};

type SeasonOption = {
  number: number;
  title: string;
  loaded_at?: string;
};

type EpisodeOption = {
  id: number;
  number: number;
  title: string;
  file_id?: number;
};

function formatBytes(size?: number) {
  if (!size) return "";
  const units = ["B", "KiB", "MiB", "GiB"];
  let value = size;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value >= 10 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

/**
 * Point at one specific video already on disk inside a configured library
 * root and register it in place for the selected movie or episode. The path
 * stays inside the configured roots; registration keeps the explicit
 * reviewed confirmation and the server-side verification of every existing
 * import.
 */
export function ImportExistingFile({
  open,
  onOpenChange,
  kind,
  collection,
  mediaID,
  title,
  seasons,
  onQueued,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  kind: "movie" | "series";
  collection?: string;
  mediaID: number;
  title: string;
  /** Loaded seasons for a series; the picker narrows episodes to one season. */
  seasons?: SeasonOption[];
  onQueued: () => void;
}) {
  const [browse, setBrowse] = useState<DirectImportBrowse>();
  const [selected, setSelected] = useState<DirectImportEntry>();
  const [season, setSeason] = useState<number | null>(null);
  const [episodes, setEpisodes] = useState<EpisodeOption[]>([]);
  const [checked, setChecked] = useState<number[]>([]);
  const [reviewed, setReviewed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loadingEpisodes, setLoadingEpisodes] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(
    async (target: string) => {
      setError("");
      try {
        const params = new URLSearchParams({ kind });
        if (collection) params.set("collection", collection);
        if (target) params.set("dir", target);
        setBrowse(
          await api<DirectImportBrowse>(
            `/library/imports/direct?${params.toString()}`,
          ),
        );
      } catch (e) {
        setError(messageFor(e));
      }
    },
    [kind, collection],
  );

  useEffect(() => {
    if (open) {
      setSelected(undefined);
      setSeason(seasons?.length ? seasons[0].number : null);
      setChecked([]);
      setReviewed(false);
      setError("");
      void load("");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    if (!open || kind !== "series" || season === null) {
      setEpisodes([]);
      setChecked([]);
      return;
    }
    let cancelled = false;
    setLoadingEpisodes(true);
    api<{ episodes: EpisodeOption[] }>(
      `/library/series/${mediaID}/seasons/${season}`,
    )
      .then((out) => {
        if (cancelled) return;
        setEpisodes(out.episodes ?? []);
        setChecked(
          (out.episodes ?? [])
            .filter((episode) => !episode.file_id)
            .slice(0, 20)
            .map((episode) => episode.id),
        );
      })
      .catch((e) => {
        if (!cancelled) setError(messageFor(e));
      })
      .finally(() => {
        if (!cancelled) setLoadingEpisodes(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, kind, mediaID, season]);

  function crumbs(path: string) {
    if (!path) return [];
    const parts = path.split("/");
    return parts.map((part, index) => ({
      name: part,
      path: parts.slice(0, index + 1).join("/"),
    }));
  }

  async function submit() {
    if (!selected || busy) return;
    setBusy(true);
    setError("");
    try {
      await api("/library/imports/direct", {
        method: "POST",
        body: JSON.stringify({
          kind,
          ...(collection ? { collection } : {}),
          media_id: mediaID,
          ...(kind === "series" ? { episode_ids: checked } : {}),
          root: browse?.roots.find(
            (root) =>
              selected.path === root || selected.path.startsWith(root + "/"),
          ),
          relative: selected.path,
          reviewed,
        }),
      });
      onQueued();
      onOpenChange(false);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }

  const seasonReady =
    kind === "movie" || (season !== null && checked.length > 0);
  const canSubmit = !!selected && reviewed && seasonReady && !busy;

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title="Import an existing file"
      description={`Point at one video already inside a configured library root and register it in place for ${title}. Nothing is copied or renamed.`}
      footer={
        <>
          <ModalClose>
            <Button variant="soft" color="gray">
              Cancel
            </Button>
          </ModalClose>
          <Button disabled={!canSubmit} onClick={() => void submit()}>
            {busy ? "Queueing…" : "Verify & add in place"}
          </Button>
        </>
      }
    >
      {error && (
        <Alert kind="danger">
          <p>{error}</p>
        </Alert>
      )}
      <div className="rs-form-help">
        {browse?.dir ? (
          <>
            <button
              type="button"
              className="rs-link-button"
              onClick={() => {
                const parent = browse.dir.includes("/")
                  ? browse.dir.slice(0, browse.dir.lastIndexOf("/"))
                  : "";
                setSelected(undefined);
                void load(parent);
              }}
            >
              Up one level
            </button>
            {" / "}
            {crumbs(browse.dir).map((crumb, index) => (
              <span key={crumb.path}>
                {index > 0 && " / "}
                <button
                  type="button"
                  className="rs-link-button"
                  onClick={() => {
                    setSelected(undefined);
                    void load(crumb.path);
                  }}
                >
                  {crumb.name}
                </button>
              </span>
            ))}
          </>
        ) : (
          "Choose a configured library root"
        )}
      </div>
      <div className="rs-browse-list" role="listbox" aria-label="Library files">
        {(browse?.items ?? []).length === 0 && (
          <p className="rs-muted">No entries here.</p>
        )}
        {(browse?.items ?? []).map((item) => (
          <div key={item.path} className="rs-browse-row">
            {item.dir ? (
              <button
                type="button"
                className="rs-link-button"
                onClick={() => {
                  setSelected(undefined);
                  void load(item.path);
                }}
              >
                {item.name}/
              </button>
            ) : (
              <label className="rs-check-row">
                <input
                  type="radio"
                  name="direct-import-file"
                  checked={selected?.path === item.path}
                  onChange={() => setSelected(item)}
                />
                <span>{item.name}</span>
                <span className="rs-muted">{formatBytes(item.size_bytes)}</span>
              </label>
            )}
          </div>
        ))}
      </div>
      {kind === "series" && (
        <div className="rs-attach-field">
          <label htmlFor="direct-import-season">Season</label>
          <select
            id="direct-import-season"
            value={season ?? ""}
            onChange={(e) => setSeason(Number(e.target.value))}
          >
            {(seasons ?? [])
              .filter((entry) => entry.loaded_at)
              .map((entry) => (
                <option key={entry.number} value={entry.number}>
                  {entry.title}
                </option>
              ))}
          </select>
          {loadingEpisodes && <p className="rs-muted">Loading episodes…</p>}
          {episodes.length > 0 && (
            <div
              className="rs-browse-list"
              role="listbox"
              aria-label="Episodes"
            >
              {episodes.map((episode) => (
                <label key={episode.id} className="rs-check-row">
                  <input
                    type="checkbox"
                    disabled={!!episode.file_id}
                    checked={checked.includes(episode.id)}
                    onChange={(event) =>
                      setChecked((current) =>
                        event.target.checked
                          ? [...current, episode.id]
                          : current.filter((id) => id !== episode.id),
                      )
                    }
                  />
                  <span>
                    E{String(episode.number).padStart(2, "0")} — {episode.title}
                  </span>
                  {episode.file_id && (
                    <span className="rs-muted">Registered</span>
                  )}
                </label>
              ))}
            </div>
          )}
          {season !== null && !loadingEpisodes && checked.length === 0 && (
            <p className="rs-muted">
              Every loaded episode of this season already has a registered file.
            </p>
          )}
        </div>
      )}
      <label className="rs-check-row">
        <input
          type="checkbox"
          checked={reviewed}
          onChange={(event) => setReviewed(event.target.checked)}
        />
        <span>
          I confirm this file belongs to <strong>{title}</strong>
          {kind === "series" ? " and the selected episodes" : ""}. It will be
          verified in place, not copied.
        </span>
      </label>
    </Modal>
  );
}
