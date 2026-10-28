import { useEffect, useRef, useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Link, useSearchParams } from "react-router-dom";
import { api, messageFor } from "../api";
import { PageHeading, RequestState } from "../components";
import { Pagination } from "../movieComponents";
import { useResource } from "../useResource";
import { useLiveResource } from "../useLiveResource";
import { bytes } from "./Releases";

type Library = {
  key: string;
  name: string;
  folder: string;
  content: string;
  preset: string;
  movie_template: string;
  series_template: string;
  episode_template: string;
};
type Config = { revision: number; main_root: string; libraries: Library[] };
type Assignment = {
  media_id: number;
  library_key: string;
  revision: number;
  use_default?: boolean;
  effective_library: Library;
};
type File = {
  id: number;
  media_id: number;
  title: string;
  kind: string;
  category: string;
  path: string;
  size_bytes: number;
};
type Files = { items: File[]; page: number; total: number; limit: number };
type Planned = {
  media_file_id: number;
  embedded_title?: string;
  title: string;
  source: string;
  destination: string;
  size_bytes: number;
  unchanged: boolean;
  companions: { source: string; destination: string; size_bytes: number }[];
};
type Plan = {
  id: string;
  state: string;
  phase: string;
  message: string;
  files: Planned[];
  created_at: number;
  updated_at: number;
  update_embedded_title?: boolean;
  embedded_title?: string;
  replacement?: {
    title: string;
    media_file_id: number;
    restores_plan?: string;
    recovering?: boolean;
    tag_title?: string;
    original: { source: string; size_bytes: number };
    incoming: { source: string; size_bytes: number };
    backup: string;
    episodes: { id: number; season: number; number: number; title: string }[];
  };
  folder_move?: {
    source: string;
    destination: string;
    stage?: string;
    inventory: {
      directories: { path: string }[];
      files: { source: string; size_bytes: number }[];
    };
  };
};

const templates = {
  movie_template: "{title} ({year})/{title} ({year})",
  series_template: "{title} ({year})",
  episode_template:
    "Season {season:02}/{title} - S{season:02}E{episode:02}{episode_range} - {episode_title}",
};

export function Organization() {
  const config = useResource<Config>("/organization");
  const [tab, setTab] = useState("files");
  return (
    <>
      <PageHeading
        title="Organize library"
        description="Collections, folder structure and reviewed file changes."
      />
      <RequestState
        loading={config.loading}
        error={config.error}
        retry={config.reload}
      />
      {config.data && (
        <>
          <section className="panel organization-root">
            <h2>Main media directory</h2>
            <code>{config.data.main_root}</code>
            <p className="field-help">
              All collection folders are inside this directory. Set
              REELSCAREY_MEDIA_DIR when starting ReelScarey. Changing the main
              directory requires stopping the app and migrating the entire media
              tree; saving naming preferences moves no files.
            </p>
          </section>
          <nav className="search-kind" aria-label="Organization views">
            <Button
              variant={tab === "files" ? "solid" : "soft"}
              onClick={() => setTab("files")}
            >
              Move & organize files
            </Button>
            <Button
              variant={tab === "settings" ? "solid" : "soft"}
              onClick={() => setTab("settings")}
            >
              Folders & naming
            </Button>
          </nav>
          {tab === "settings" ? (
            <Naming
              key={config.data.revision}
              config={config.data}
              saved={config.reload}
            />
          ) : (
            <Reorganize config={config.data} />
          )}
        </>
      )}
    </>
  );
}
function Naming({ config, saved }: { config: Config; saved: () => void }) {
  const [libraries, setLibraries] = useState(config.libraries);
  const [pending, setPending] = useState(false),
    [error, setError] = useState("");
  function edit(index: number, field: keyof Library, value: string) {
    setLibraries((current) =>
      current.map((l, n) =>
        n === index
          ? {
              ...l,
              [field]: value,
              ...(field === "preset" && value === "custom" ? templates : {}),
            }
          : l,
      ),
    );
  }
  async function save() {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await api("/organization", {
        method: "PUT",
        body: JSON.stringify({ revision: config.revision, libraries }),
      });
      saved();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <section className="panel settings-form organization-settings">
      <h2>Folders & naming</h2>
      <p>
        These rules apply to newly queued downloads. Previously queued downloads
        keep their saved rules. Existing files change only through a reviewed
        reorganization.
      </p>
      <fieldset disabled={pending}>
        {libraries.map((library, index) => (
          <details
            className="organization-library"
            key={library.key}
            open={libraries.length === 3 || undefined}
          >
            <summary>
              {library.name}{" "}
              <span className="field-help">
                {library.folder} · {library.preset}
              </span>
            </summary>
            <label>
              Name
              <TextField.Root
                aria-label={`Collection name ${index + 1}`}
                value={library.name}
                maxLength={80}
                onChange={(e) => edit(index, "name", e.target.value)}
              />
            </label>
            <label>
              Folder under main directory
              <TextField.Root
                aria-label={`Collection folder ${index + 1}`}
                value={library.folder}
                maxLength={160}
                onChange={(e) => edit(index, "folder", e.target.value)}
              />
            </label>
            {!["movies", "tv", "anime"].includes(library.key) && (
              <label>
                Content
                <select
                  aria-label={`Collection content ${index + 1}`}
                  value={library.content}
                  onChange={(e) => edit(index, "content", e.target.value)}
                >
                  <option value="mixed">Movies and shows</option>
                  <option value="movie">Movies</option>
                  <option value="series">Shows</option>
                </select>
              </label>
            )}
            <p className="field-help">
              {library.content === "mixed"
                ? "Movies and TV Shows subfolders keep the two content types separate for your media server."
                : "Each title gets its own folder."}
            </p>
            <label>
              Naming preset
              <select
                aria-label={`Naming preset ${index + 1}`}
                value={library.preset}
                onChange={(e) => edit(index, "preset", e.target.value)}
              >
                <option value="legacy">Current ReelScarey naming</option>
                <option value="emby">Emby</option>
                <option value="jellyfin">Jellyfin</option>
                <option value="plex">Plex</option>
                <option value="custom">Custom</option>
              </select>
            </label>
            {library.preset === "custom" && (
              <>
                <label>
                  Movie path, without extension
                  <TextField.Root
                    aria-label={`Movie template ${index + 1}`}
                    value={library.movie_template}
                    maxLength={600}
                    onChange={(e) =>
                      edit(index, "movie_template", e.target.value)
                    }
                  />
                </label>
                <label>
                  Show folder template
                  <TextField.Root
                    aria-label={`Show template ${index + 1}`}
                    value={library.series_template}
                    maxLength={600}
                    onChange={(e) =>
                      edit(index, "series_template", e.target.value)
                    }
                  />
                </label>
                <label>
                  Episode path inside show folder, without extension
                  <TextField.Root
                    aria-label={`Episode template ${index + 1}`}
                    value={library.episode_template}
                    maxLength={600}
                    onChange={(e) =>
                      edit(index, "episode_template", e.target.value)
                    }
                  />
                </label>
              </>
            )}
            {!["movies", "tv", "anime"].includes(library.key) && (
              <Button
                variant="soft"
                color="gray"
                type="button"
                onClick={() =>
                  setLibraries((current) =>
                    current.filter((_, n) => n !== index),
                  )
                }
              >
                Remove collection
              </Button>
            )}
          </details>
        ))}
        <Button
          variant="soft"
          type="button"
          disabled={libraries.length >= 20}
          onClick={() =>
            setLibraries((current) => [
              ...current,
              {
                key: `collection_${Date.now().toString(36)}`,
                name: "New collection",
                folder: "New collection",
                content: "mixed",
                preset: "plex",
                ...templates,
              },
            ])
          }
        >
          Add collection
        </Button>
        <p className="field-help">
          Custom paths use / between folders. Tokens:{" "}
          {
            "{title}, {year}, {tmdb_id}, {season:02}, {episode:02}, {episode_range}, {episode_title}"
          }
          . Anime also supports {"{absolute:03}, {absolute_range}"} when every
          episode has an explicit absolute number. Always include the range
          token for shared episode files. Unknown years render as Unknown.
          Existing folders and video extensions are preserved until you apply a
          plan.
        </p>
      </fieldset>
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      <div className="form-footer">
        <Button
          disabled={
            pending ||
            JSON.stringify(libraries) === JSON.stringify(config.libraries)
          }
          onClick={() => void save()}
        >
          {pending ? "Saving…" : "Save folders & naming"}
        </Button>
        <Button color="gray" variant="soft" disabled={pending} onClick={saved}>
          Reload saved rules
        </Button>
      </div>
    </section>
  );
}
function Reorganize({ config }: { config: Config }) {
  const [params, setParams] = useSearchParams(),
    [input, setInput] = useState(params.get("query") ?? "");
  const query = params.get("query") ?? "",
    page = Number(params.get("page") ?? 1),
    mediaID = params.get("media_id") ?? "",
    replacementSource = params.get("replacement_source") ?? "";
  const path = `/organization/files?page=${page}&query=${encodeURIComponent(query)}${mediaID ? `&media_id=${encodeURIComponent(mediaID)}` : ""}`;
  const files = useResource<Files>(path),
    history = useLiveResource<{ items: Plan[] }>("/organization/plans", 5000);
  const [versionsFile, setVersionsFile] = useState<number>();
  const versions = useResource<{ items: Plan[] }>(
    versionsFile
      ? `/organization/replacements?media_file_id=${versionsFile}`
      : null,
  );
  const [health, setHealth] = useState<Record<number, string>>({});
  const [healthProgress, setHealthProgress] = useState({
    completed: 0,
    total: 0,
  });
  const [selected, setSelected] = useState<number[]>([]),
    [target, setTarget] = useState("");
  const [updateEmbeddedTitle, setUpdateEmbeddedTitle] = useState(false);
  useEffect(() => {
    if (selected.length === 0 && !mediaID) setUpdateEmbeddedTitle(false);
  }, [selected.length, mediaID]);
  const [plan, setPlan] = useState<Plan>(),
    [pending, setPending] = useState(""),
    [error, setError] = useState(""),
    [reviewed, setReviewed] = useState(false),
    [stale, setStale] = useState(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    request.current?.abort();
    setPending("");
    setSelected([]);
    setHealth({});
    setPlan(undefined);
    setReviewed(false);
    setStale(false);
    return () => request.current?.abort();
  }, [path]);
  const running = !!history.data?.items.some(
    (p) => p.state === "queued" || p.state === "running",
  );
  useEffect(() => {
    setPlan((current) => {
      if (!current || current.state === "review") return current;
      const saved = history.data?.items.find(
        (item) => item.id === current.id && item.state !== "review",
      );
      return saved ?? current;
    });
  }, [history.data]);
  useEffect(() => {
    if (plan?.state === "completed") {
      setHealth({});
      files.reload();
      if (versionsFile) versions.reload();
    }
  }, [plan?.state, files.reload, versionsFile, versions.reload]);
  async function checkHealth(fileID: number) {
    if (pending) return;
    const controller = new AbortController();
    request.current?.abort();
    request.current = controller;
    setPending(`health-${fileID}`);
    setHealth((current) => {
      const next = { ...current };
      delete next[fileID];
      return next;
    });
    setError("");
    try {
      const result = await api<{ status: string }>(
        `/organization/files/${fileID}/health`,
        { method: "POST", body: "{}", signal: controller.signal },
      );
      if (!controller.signal.aborted)
        setHealth((current) => ({ ...current, [fileID]: result.status }));
    } catch (e) {
      if (!controller.signal.aborted) setError(messageFor(e));
    } finally {
      if (request.current === controller) {
        request.current = null;
        setPending("");
      }
    }
  }
  async function checkSelectedHealth() {
    if (pending || !selected.length) return;
    const ids = [...selected];
    const controller = new AbortController();
    request.current?.abort();
    request.current = controller;
    setPending("batch-health");
    setError("");
    setHealthProgress({ completed: 0, total: ids.length });
    setHealth((current) => {
      const next = { ...current };
      ids.forEach((id) => delete next[id]);
      return next;
    });
    let failures = 0;
    try {
      for (const id of ids) {
        if (controller.signal.aborted) break;
        try {
          const result = await api<{ status: string }>(
            `/organization/files/${id}/health`,
            { method: "POST", body: "{}", signal: controller.signal },
          );
          if (controller.signal.aborted) break;
          setHealth((current) => ({ ...current, [id]: result.status }));
        } catch {
          if (controller.signal.aborted) break;
          failures++;
          setHealth((current) => ({ ...current, [id]: "error" }));
        }
        setHealthProgress((current) => ({
          ...current,
          completed: current.completed + 1,
        }));
      }
      if (!controller.signal.aborted && failures)
        setError(
          `${failures} file ${failures === 1 ? "check could" : "checks could"} not be completed. Try those files individually for details.`,
        );
    } finally {
      if (request.current === controller) {
        request.current = null;
        setPending("");
      }
    }
  }
  async function reviewReplacement() {
    if (pending || selected.length !== 1 || !replacementSource) return;
    setPending("replacement");
    setError("");
    setReviewed(false);
    setPlan(undefined);
    setStale(false);
    try {
      setPlan(
        await api<Plan>("/organization/replacements", {
          method: "POST",
          body: JSON.stringify({
            media_file_id: selected[0],
            scan_file_id: replacementSource,
          }),
        }),
      );
      history.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending("");
    }
  }
  async function reviewEmbeddedTitle() {
    if (pending || selected.length !== 1) return;
    setPending("metadata");
    setError("");
    setReviewed(false);
    setPlan(undefined);
    setStale(false);
    try {
      setPlan(
        await api<Plan>("/organization/embedded-title", {
          method: "POST",
          body: JSON.stringify({ media_file_id: selected[0] }),
        }),
      );
      history.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending("");
    }
  }
  async function reviewRollback(saved: Plan) {
    if (pending) return;
    setPending("rollback");
    setError("");
    setReviewed(false);
    setPlan(undefined);
    setStale(false);
    try {
      setPlan(
        await api<Plan>(`/organization/plans/${saved.id}/rollback`, {
          method: "POST",
          body: "{}",
        }),
      );
      history.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending("");
    }
  }
  async function recoverReplacement(saved: Plan) {
    if (
      pending ||
      !window.confirm(
        "Restore the retained original? The app will only restore it if its registered path is empty.",
      )
    )
      return;
    setPending("recover");
    setError("");
    try {
      setPlan(
        await api<Plan>(`/organization/plans/${saved.id}/recover-replacement`, {
          method: "POST",
          body: "{}",
        }),
      );
      history.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending("");
    }
  }
  async function preview(wholeTitle = false, wholeFolder = false) {
    if (pending || (wholeTitle ? !mediaID || !target : !selected.length))
      return;
    setPending("preview");
    setError("");
    setReviewed(false);
    setPlan(undefined);
    setStale(false);
    const controller = new AbortController();
    request.current?.abort();
    request.current = controller;
    try {
      const out = await api<Plan>("/organization/plans", {
        method: "POST",
        body: JSON.stringify(
          wholeTitle
            ? {
                media_id: Number(mediaID),
                target_library: target,
                ...(wholeFolder ? { whole_folder: true } : {}),
                ...(updateEmbeddedTitle ? { update_embedded_title: true } : {}),
              }
            : {
                file_ids: selected,
                target_library: target,
                ...(updateEmbeddedTitle ? { update_embedded_title: true } : {}),
              },
        ),
        signal: AbortSignal.any([
          controller.signal,
          AbortSignal.timeout(35000),
        ]),
      });
      if (!controller.signal.aborted) {
        setPlan(out);
        history.reload();
      }
    } catch (e) {
      if (!controller.signal.aborted) setError(messageFor(e));
    } finally {
      if (!controller.signal.aborted) setPending("");
    }
  }
  async function apply() {
    if (!plan || !reviewed || stale || pending) return;
    setPending("apply");
    setError("");
    try {
      setPlan(
        await api<Plan>(`/organization/plans/${plan.id}/apply`, {
          method: "POST",
          body: "{}",
        }),
      );
      setSelected([]);
      setReviewed(false);
      history.reload();
      files.reload();
    } catch (e) {
      setError(
        `${messageFor(e)} Create a fresh review before another attempt.`,
      );
      setStale(true);
      setReviewed(false);
    } finally {
      setPending("");
    }
  }
  async function resume(saved: Plan) {
    if (pending) return;
    setPending("resume");
    setError("");
    try {
      setPlan(
        await api<Plan>(`/organization/plans/${saved.id}/apply`, {
          method: "POST",
          body: "{}",
        }),
      );
      history.reload();
      files.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending("");
    }
  }
  async function cancelQueued(saved: Plan) {
    if (pending) return;
    setPending("cancel");
    setError("");
    try {
      setPlan(
        await api<Plan>(`/organization/plans/${saved.id}/cancel`, {
          method: "POST",
          body: "{}",
        }),
      );
      setReviewed(false);
      history.reload();
    } catch (e) {
      setError(messageFor(e));
      history.reload();
    } finally {
      setPending("");
    }
  }
  function filter(nextPage: number, nextQuery = query) {
    setParams({
      ...(mediaID ? { media_id: mediaID } : {}),
      ...(replacementSource ? { replacement_source: replacementSource } : {}),
      ...(nextQuery ? { query: nextQuery } : {}),
      page: String(nextPage),
    });
  }
  return (
    <>
      <section className="panel organization-selection">
        <h2>Move or organize registered files</h2>
        <p>
          Review up to 50 files from this page. The plan checks video identity,
          subtitles, conflicts and current naming rules. Downloads and seeding
          sources stay in place. Unrelated extras and empty folders are
          retained. File integrity checks read the entire file and can take a
          while.
        </p>
        <form
          className="movie-search-form"
          onSubmit={(e) => {
            e.preventDefault();
            filter(1, input.trim());
          }}
        >
          <TextField.Root
            aria-label="Filter files by title"
            value={input}
            maxLength={200}
            onChange={(e) => setInput(e.target.value)}
          />
          <Button disabled={!!pending} type="submit">
            Filter
          </Button>
          {mediaID && (
            <Button asChild variant="soft">
              <Link to="/organization">All titles</Link>
            </Button>
          )}
        </form>
        <RequestState
          loading={files.loading}
          error={files.error}
          retry={files.reload}
        />
        {files.data && (
          <>
            <p className="field-help">
              {files.data.total} registered{" "}
              {files.data.total === 1 ? "file" : "files"} · selections clear
              when filters or pages change.
            </p>
            {!!files.data.items.length && (
              <label>
                <input
                  type="checkbox"
                  aria-label="Select all files on this page"
                  disabled={!!pending}
                  checked={files.data.items.every((f) =>
                    selected.includes(f.id),
                  )}
                  onChange={(e) => {
                    setSelected(
                      e.target.checked
                        ? files.data!.items.map((f) => f.id)
                        : [],
                    );
                    setPlan(undefined);
                    setReviewed(false);
                  }}
                />{" "}
                Select this page
              </label>
            )}
            <div className="organization-files">
              {files.data.items.map((file) => (
                <div key={file.id}>
                  <label className="organization-file">
                    <input
                      type="checkbox"
                      disabled={!!pending}
                      aria-label={`Select file ${file.id}: ${file.title}`}
                      checked={selected.includes(file.id)}
                      onChange={(e) => {
                        setSelected((current) =>
                          e.target.checked
                            ? [...current, file.id]
                            : current.filter((id) => id !== file.id),
                        );
                        setPlan(undefined);
                        setReviewed(false);
                      }}
                    />
                    <span>
                      <strong>{file.title}</strong>
                      <span className="field-help">
                        {file.category === "anime" ? "Anime · " : ""}
                        {file.kind === "movie"
                          ? "Movie"
                          : "Episode file"} · {bytes(file.size_bytes)}
                      </span>
                      <code>{file.path}</code>
                    </span>
                  </label>
                  <Button
                    variant="soft"
                    disabled={!!pending}
                    onClick={() => checkHealth(file.id)}
                  >
                    {pending === `health-${file.id}`
                      ? "Checking…"
                      : "Check file integrity"}
                  </Button>
                  <Button
                    variant="soft"
                    disabled={!!pending}
                    onClick={() => setVersionsFile(file.id)}
                  >
                    Show saved versions
                  </Button>
                  {health[file.id] && (
                    <p role="status">
                      {health[file.id] === "verified"
                        ? "File matches its saved checksum."
                        : health[file.id] === "missing"
                          ? "File is missing from its registered path."
                          : health[file.id] === "unsafe"
                            ? "File path cannot be safely inspected."
                            : health[file.id] === "error"
                              ? "The integrity check could not be completed."
                              : "File differs from its saved checksum or changed during inspection."}
                    </p>
                  )}
                </div>
              ))}
            </div>
            {!files.data.items.length && (
              <p>
                No registered files match.{" "}
                <Link className="text-link" to="/import">
                  Import an existing library
                </Link>{" "}
                to register files before organizing them.
              </p>
            )}
            <Pagination
              page={files.data.page}
              pages={Math.ceil(files.data.total / files.data.limit)}
              onPage={filter}
            />
            <p className="field-help">
              Integrity results describe the moment each file was checked. Each
              check reads the full file and can take a while.
            </p>
            <Button
              variant="soft"
              disabled={!selected.length || !!pending || running}
              onClick={() => void checkSelectedHealth()}
            >
              Check selected files ({selected.length})
            </Button>{" "}
            {pending === "batch-health" && (
              <>
                <span role="status">
                  Checked {healthProgress.completed} of {healthProgress.total}.
                </span>{" "}
                <Button variant="soft" onClick={() => request.current?.abort()}>
                  Stop checking
                </Button>
              </>
            )}
          </>
        )}
        {versionsFile && (
          <div className="system-panel">
            <h3>Saved versions for file {versionsFile}</h3>
            <RequestState
              loading={versions.loading}
              error={versions.error}
              retry={versions.reload}
            />
            {versions.data?.items
              .filter((item) => !item.replacement?.recovering)
              .map((item) => (
                <div key={item.id}>
                  <p>
                    {new Date(item.updated_at * 1000).toLocaleString()} ·{" "}
                    {item.replacement?.title}
                  </p>
                  <Button
                    variant="soft"
                    disabled={!!pending || running}
                    onClick={() => void reviewRollback(item)}
                  >
                    Review restore previous version
                  </Button>
                </div>
              ))}
            {versions.data &&
              !versions.data.items.filter(
                (item) => !item.replacement?.recovering,
              ).length && <p>No saved versions.</p>}
          </div>
        )}
        {replacementSource && (
          <div className="system-panel">
            <p>
              Select exactly one registered file to replace with the discovered
              video. The source must have the same file extension and media
              type.
            </p>
            <Button
              disabled={selected.length !== 1 || !!pending || running}
              onClick={() => void reviewReplacement()}
            >
              Review replacement
            </Button>{" "}
            <Button asChild variant="soft">
              <Link to="/import">Choose another source</Link>
            </Button>
          </div>
        )}
        <p className="field-help">
          Choose one registered MP4 or Matroska file to review an embedded title
          update using its saved movie or episode information.
        </p>
        <Button
          variant="soft"
          disabled={selected.length !== 1 || !!pending || running}
          onClick={() => void reviewEmbeddedTitle()}
        >
          Review embedded title update
        </Button>
        <fieldset disabled={!!pending}>
          <label>
            Destination collection
            <select
              aria-label="Destination collection"
              value={target}
              onChange={(e) => {
                setTarget(e.target.value);
                setPlan(undefined);
                setReviewed(false);
              }}
            >
              <option value="">Each title’s saved collection</option>
              {config.libraries.map((l) => (
                <option key={l.key} value={l.key}>
                  {l.name} · {l.folder}
                </option>
              ))}
            </select>
          </label>
        </fieldset>
        <p className="field-help">
          Choosing another collection also sets it for these titles’ future
          imports after the batch commits. Unselected existing files retain
          their locations.
        </p>
        <label>
          <input
            type="checkbox"
            checked={updateEmbeddedTitle}
            disabled={(selected.length === 0 && !mediaID) || !!pending}
            onChange={(event) => {
              setUpdateEmbeddedTitle(event.target.checked);
              setPlan(undefined);
              setReviewed(false);
            }}
          />{" "}
          Also update embedded titles after organization
        </label>
        <p className="field-help">
          Choose MP4 or Matroska files, or review an entire title folder. Each
          verified title update retains a rollback copy.
        </p>
        {mediaID && (
          <>
            <p className="field-help">
              Move registered files for this title in one review (up to 100
              files), or choose an entire-folder review to include extras.
            </p>
            <Button
              variant="soft"
              disabled={!target || !!pending || running}
              onClick={() => void preview(true)}
            >
              Review all files for this title
            </Button>{" "}
            <Button
              variant="soft"
              disabled={!target || !!pending || running}
              onClick={() => void preview(true, true)}
            >
              Review entire folder and extras
            </Button>
            <p className="field-help">
              Moves the whole title folder with its existing names, including
              artwork, unregistered files and empty folders. Across filesystems,
              it verifies a staged copy before removing the original files.
            </p>
          </>
        )}
        <Button
          disabled={!selected.length || !!pending || running}
          onClick={() => void preview(false)}
        >
          {pending === "preview"
            ? "Checking files…"
            : `Review ${selected.length} selected ${selected.length === 1 ? "file" : "files"}`}
        </Button>
        {running && (
          <p role="status">
            An organization job is active. Its saved progress appears below.
          </p>
        )}
        {error && (
          <p role="alert" className="form-error">
            {error}
          </p>
        )}
      </section>
      {plan && (
        <section
          className="panel system-panel"
          aria-label="Organization review"
        >
          <h2>
            {plan.state === "review"
              ? "Review file changes"
              : "Organization job"}
          </h2>
          <p>
            {plan.message ||
              (plan.state === "review"
                ? "Ready for review"
                : plan.state === "queued"
                  ? "Queued"
                  : plan.state === "running"
                    ? "Applying reviewed changes"
                    : plan.state === "completed"
                      ? "Completed"
                      : "Needs attention")}
          </p>
          {!plan.replacement && (
            <p className="field-help">
              {plan.files.filter((f) => !f.unchanged).length}{" "}
              {plan.files.filter((f) => !f.unchanged).length === 1
                ? "file"
                : "files"}{" "}
              {plan.state === "completed" ? "changed" : "in this plan"};{" "}
              {plan.files.filter((f) => f.unchanged).length} unchanged.
              Subtitles listed below move with their video.
            </p>
          )}
          {plan.update_embedded_title && (
            <p>
              After the file move, update embedded titles using verified copies
              and retain each previous version.
            </p>
          )}
          <PlanFiles plan={plan} />
          {plan.state === "queued" && plan.phase === "planned" && (
            <Button
              variant="soft"
              disabled={!!pending}
              onClick={() => void cancelQueued(plan)}
            >
              Cancel queued job
            </Button>
          )}
          {plan.state === "review" && (
            <>
              <label>
                <input
                  type="checkbox"
                  aria-label="I reviewed all paths and subtitle changes"
                  checked={reviewed}
                  disabled={!!pending || stale}
                  onChange={(e) => setReviewed(e.target.checked)}
                />{" "}
                I reviewed all paths and subtitle changes
              </label>
              <div className="form-footer">
                <Button
                  disabled={!reviewed || !!pending || stale}
                  onClick={() => void apply()}
                >
                  {pending === "apply" ? "Queuing…" : "Apply reviewed changes"}
                </Button>
                <Button
                  color="gray"
                  variant="soft"
                  disabled={!!pending}
                  onClick={() => {
                    setPlan(undefined);
                    setReviewed(false);
                  }}
                >
                  Cancel review
                </Button>
              </div>
            </>
          )}
        </section>
      )}
      <section className="panel system-panel">
        <h2>Recent organization jobs</h2>
        <RequestState
          loading={history.loading}
          error={history.error}
          retry={history.reload}
        />
        {history.data?.items
          .filter((p) => p.state !== "review")
          .map((p) => (
            <details className="organization-job" key={p.id}>
              <summary>
                {p.state} ·{" "}
                {p.replacement
                  ? "replacement"
                  : `${p.files.length} ${p.files.length === 1 ? "file" : "files"}`}{" "}
                · {new Date(p.updated_at * 1000).toLocaleString()}
              </summary>
              <p>{p.message}</p>
              <PlanFiles plan={p} />
              {p.state === "queued" && p.phase === "planned" && (
                <Button
                  variant="soft"
                  disabled={!!pending}
                  onClick={() => void cancelQueued(p)}
                >
                  Cancel queued job
                </Button>
              )}
              {p.state === "completed" &&
                p.replacement &&
                !p.replacement.recovering && (
                  <Button
                    variant="soft"
                    disabled={!!pending || running}
                    onClick={() => void reviewRollback(p)}
                  >
                    Review restore previous version
                  </Button>
                )}
              {p.state === "failed" &&
                p.replacement &&
                !p.replacement.recovering &&
                (p.phase === "archived" || p.phase === "removed") && (
                  <Button
                    variant="soft"
                    disabled={!!pending || running}
                    onClick={() => void recoverReplacement(p)}
                  >
                    Restore original after interruption
                  </Button>
                )}
              {p.state === "failed" && p.phase !== "failed_safe" && (
                <Button
                  variant="soft"
                  disabled={!!pending || running}
                  onClick={() => void resume(p)}
                >
                  Resume saved job
                </Button>
              )}
            </details>
          ))}
        {history.data &&
          !history.data.items.some((p) => p.state !== "review") && (
            <p>No organization jobs yet.</p>
          )}
      </section>
    </>
  );
}
function PlanFiles({ plan }: { plan: Plan }) {
  return (
    <>
      {plan.replacement && (
        <div>
          <p>
            <strong>{plan.replacement.title}</strong>
          </p>
          {plan.replacement.tag_title && (
            <p>
              New embedded title: <strong>{plan.replacement.tag_title}</strong>
            </p>
          )}
          <p>
            Current file: <code>{plan.replacement.original.source}</code> (
            {bytes(plan.replacement.original.size_bytes)})
          </p>
          {!plan.replacement.tag_title && (
            <p>
              Incoming file: <code>{plan.replacement.incoming.source}</code> (
              {bytes(plan.replacement.incoming.size_bytes)})
            </p>
          )}
          {plan.replacement.tag_title ? (
            <p>
              A separate copy will be checked to ensure its streams, packets,
              chapters and existing tags are preserved. The prior video will be
              retained for a reviewed restore; seeding sources and external
              subtitles remain in place.
            </p>
          ) : (
            <p>
              Retained version: <code>{plan.replacement.backup}</code>
            </p>
          )}
          {!!plan.replacement.episodes?.length && (
            <p>
              This file covers:{" "}
              {plan.replacement.episodes
                .map(
                  (episode) =>
                    `S${String(episode.season).padStart(2, "0")}E${String(episode.number).padStart(2, "0")} ${episode.title}`,
                )
                .join(", ")}
            </p>
          )}
          <p>
            The source remains in place. Existing external subtitles remain
            alongside the video, but their timing may differ. The incoming
            file’s sidecars are not imported. The previous video is retained for
            a reviewed restore.
          </p>
        </div>
      )}
      {plan.folder_move && (
        <div>
          <p>
            Entire folder: <code>{plan.folder_move.source}</code> →{" "}
            <code>{plan.folder_move.destination}</code>
          </p>
          {plan.folder_move.stage && (
            <p className="field-help">
              Saved staging folder: <code>{plan.folder_move.stage}</code>
            </p>
          )}
          <p>
            Review every directory and file below. All contents move together.
          </p>
          <ul>
            {plan.folder_move.inventory.directories.map((d) => (
              <li key={d.path}>
                Folder: <code>{d.path}</code>
              </li>
            ))}
            {plan.folder_move.inventory.files.map((f) => (
              <li key={f.source}>
                <code>{f.source}</code> · {bytes(f.size_bytes)}
              </li>
            ))}
          </ul>
        </div>
      )}
      <ol className="organization-plan-files">
        {plan.files.map((f) => (
          <li key={f.media_file_id}>
            <strong>
              {f.title}
              {f.unchanged ? " · unchanged" : ""}
            </strong>
            <div className="organization-path-change">
              <code>{f.source}</code>
              <span aria-label="to">→</span>
              <code>{f.destination}</code>
            </div>
            {f.embedded_title && (
              <p className="field-help">Embedded title: {f.embedded_title}</p>
            )}
            {f.companions.map((c) => (
              <div
                className="organization-path-change field-help"
                key={c.source}
              >
                <code>{c.source}</code>
                <span>→</span>
                <code>{c.destination}</code>
              </div>
            ))}
          </li>
        ))}
      </ol>
    </>
  );
}

export function ItemOrganization({ id }: { id: number }) {
  const [open, setOpen] = useState(false);
  const config = useResource<Config>(open ? "/organization" : null),
    assignment = useResource<Assignment>(
      open ? `/library/items/${id}/organization` : null,
    );
  const [key, setKey] = useState(""),
    [pending, setPending] = useState(false),
    [error, setError] = useState(""),
    [success, setSuccess] = useState(false);
  useEffect(() => {
    if (assignment.data)
      setKey(assignment.data.use_default ? "" : assignment.data.library_key);
  }, [assignment.data]);
  async function save() {
    if (!assignment.data || pending) return;
    setPending(true);
    setError("");
    try {
      await api(`/library/items/${id}/organization`, {
        method: "PUT",
        body: JSON.stringify({
          library_key: key,
          revision: assignment.data.revision,
        }),
      });
      setSuccess(true);
      assignment.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }
  return (
    <section className="panel settings-form">
      <h2>File organization</h2>
      {!open ? (
        <Button variant="soft" onClick={() => setOpen(true)}>
          Choose collection & naming
        </Button>
      ) : (
        <>
          <RequestState
            loading={config.loading || assignment.loading}
            error={config.error || assignment.error}
            retry={() => {
              config.reload();
              assignment.reload();
            }}
          />
          {config.data && assignment.data && (
            <>
              <p>
                Current destination:{" "}
                <code>{assignment.data.effective_library.folder}</code>. This
                choice applies to newly queued downloads.
              </p>
              <label>
                Collection
                <select
                  aria-label="Title collection"
                  disabled={pending}
                  value={key}
                  onChange={(e) => {
                    setKey(e.target.value);
                    setSuccess(false);
                  }}
                >
                  <option value="">Follow library section</option>
                  {config.data.libraries.map((l) => (
                    <option key={l.key} value={l.key}>
                      {l.name}
                    </option>
                  ))}
                </select>
              </label>
              <p className="field-help">
                Saving does not move existing files. The server checks
                compatibility with this title’s movie or show format.
              </p>
              <div className="form-footer">
                <Button disabled={pending} onClick={() => void save()}>
                  {pending ? "Saving…" : "Save collection"}
                </Button>
                <Button asChild variant="soft">
                  <Link to={`/organization?media_id=${id}`}>
                    Move or organize existing files
                  </Link>
                </Button>
              </div>
              {success && <p role="status">Collection saved.</p>}
            </>
          )}
          {error && (
            <p role="alert" className="form-error">
              {error}
            </p>
          )}
        </>
      )}
    </section>
  );
}
