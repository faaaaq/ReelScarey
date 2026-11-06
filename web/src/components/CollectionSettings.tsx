import { useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor } from "../api";

export type CollectionLibrary = {
  key: string;
  name: string;
  folder: string;
  content: string;
  preset: string;
  movie_template: string;
  series_template: string;
  episode_template: string;
};
export type CollectionConfig = {
  revision: number;
  main_root: string;
  libraries: CollectionLibrary[];
};

const templates = {
  movie_template: "{title} ({year})/{title} ({year})",
  series_template: "{title} ({year})",
  episode_template:
    "Season {season:02}/{title} - S{season:02}E{episode:02}{episode_range} - {episode_title}",
};

export function CollectionSettings({
  config,
  saved,
}: {
  config: CollectionConfig;
  saved: () => void;
}) {
  const [libraries, setLibraries] = useState(config.libraries);
  const [pending, setPending] = useState(false),
    [error, setError] = useState("");
  function edit(index: number, field: keyof CollectionLibrary, value: string) {
    setLibraries((current) =>
      current.map((library, position) =>
        position === index
          ? {
              ...library,
              [field]: value,
              ...(field === "preset" && value === "custom" ? templates : {}),
            }
          : library,
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
      <h2>Collections & folders</h2>
      <p>
        Built-in: Movies, TV Shows and Anime. Custom collections are created
        under the media directory.
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
                    current.filter((_, position) => position !== index),
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
          {pending ? "Saving…" : "Save collections & naming"}
        </Button>
        <Button color="gray" variant="soft" disabled={pending} onClick={saved}>
          Reload saved rules
        </Button>
      </div>
    </section>
  );
}
