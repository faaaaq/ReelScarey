import { useEffect, useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import {
  api,
  messageFor,
  type LibraryRoots,
  type LibraryRootsConfig,
} from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";
import "../pages/Admin.css";

type Draft = { key: string; name: string; roots: string[] };

function drafts(config: LibraryRootsConfig): Draft[] {
  return (config.libraries ?? []).map((library) => ({
    key: library.key,
    name: library.name,
    roots: library.roots.map((root) => root.path),
  }));
}

function availability(library: LibraryRoots, path: string) {
  return library.roots.find((root) => root.path === path);
}

export function LibraryRootsEditor() {
  const resource = useResource<LibraryRootsConfig>("/library/roots");
  const config = resource.data;
  const [libraries, setLibraries] = useState<Draft[]>();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (config) setLibraries(drafts(config));
  }, [config]);
  const active = libraries ?? (config ? drafts(config) : []);
  const original = config ? drafts(config) : [];

  function edit(index: number, position: number, value: string) {
    setLibraries((current) =>
      (current ?? drafts(config!)).map((library, at) =>
        at === index
          ? {
              ...library,
              roots: library.roots.map((root, slot) =>
                slot === position ? value : root,
              ),
            }
          : library,
      ),
    );
  }

  function add(index: number) {
    setLibraries((current) =>
      (current ?? drafts(config!)).map((library, at) =>
        at === index ? { ...library, roots: [...library.roots, ""] } : library,
      ),
    );
  }

  function remove(index: number, position: number) {
    setLibraries((current) =>
      (current ?? drafts(config!)).map((library, at) =>
        at === index
          ? {
              ...library,
              roots: library.roots.filter((_, slot) => slot !== position),
            }
          : library,
      ),
    );
  }

  async function save() {
    if (!config || pending) return;
    setPending(true);
    setError("");
    try {
      const updated = await api<LibraryRootsConfig>("/library/roots", {
        method: "PUT",
        body: JSON.stringify({
          revision: config.revision,
          libraries: active.map((library) => ({
            key: library.key,
            roots: library.roots.map((root) => root.trim()).filter(Boolean),
          })),
        }),
      });
      setLibraries(drafts(updated));
      resource.reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setPending(false);
    }
  }

  const dirty = JSON.stringify(active) !== JSON.stringify(original);
  const empty = active.some((library) => library.roots.length === 0);

  return (
    <section className="rs-panel rs-stack organization-settings">
      <h2>Library roots</h2>
      <p>
        Choose the read-only folders each collection scans for existing files.
        These are discovery sources only. Destination folders and naming stay
        unchanged in Collections &amp; folders.
      </p>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {config && (
        <fieldset disabled={pending}>
          {(config.libraries ?? []).map((library, index) => (
            <details className="organization-library" key={library.key} open>
              <summary>
                {library.name}{" "}
                <span className="rs-form-help">
                  {library.kind === "music" ? "MusicBrainz scan" : "Video scan"}
                  {library.configured ? " · custom roots" : " · default"}
                </span>
              </summary>
              {active[index]?.roots.map((root, position) => {
                const state = availability(library, root);
                return (
                  <div key={`${library.key}-${position}`}>
                    <label>
                      Root folder {position + 1}
                      <TextField.Root
                        aria-label={`${library.name} scan root ${position + 1}`}
                        value={root}
                        maxLength={400}
                        onChange={(e) => edit(index, position, e.target.value)}
                      />
                    </label>
                    {state && !state.available && (
                      <p className="rs-form-error" role="status">
                        {state.message || "This folder is unavailable."}
                      </p>
                    )}
                    {active[index].roots.length > 1 && (
                      <Button
                        variant="soft"
                        color="gray"
                        type="button"
                        onClick={() => remove(index, position)}
                      >
                        Remove root
                      </Button>
                    )}
                  </div>
                );
              })}
              <Button
                variant="soft"
                type="button"
                disabled={active[index]?.roots.length >= 20}
                onClick={() => add(index)}
              >
                Add scan root
              </Button>
              {library.message && (
                <p className="rs-form-help">{library.message}</p>
              )}
            </details>
          ))}
        </fieldset>
      )}
      {error && (
        <p role="alert" className="rs-form-error">
          {error}
        </p>
      )}
      <div className="rs-form-actions">
        <Button
          disabled={pending || !dirty || empty}
          onClick={() => void save()}
        >
          {pending ? "Saving…" : "Save library roots"}
        </Button>
        <Button
          color="gray"
          variant="soft"
          disabled={pending}
          onClick={resource.reload}
        >
          Reload roots
        </Button>
      </div>
      <p className="rs-form-help">
        Roots are relative to the media root, must exist and cannot be symbolic
        links or overlap another collection. Removing every root is refused so a
        collection never appears scannable without a source.
      </p>
    </section>
  );
}
