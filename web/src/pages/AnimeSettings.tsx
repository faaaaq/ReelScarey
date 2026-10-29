import { useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor, type AnimeLibrarySettings } from "../api";
import { RequestState } from "../components";
import { Pagination } from "../movieComponents";
import { useResource } from "../useResource";

export function AnimeSettings({
  id,
  onSaved,
}: {
  id: number;
  onSaved?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [success, setSuccess] = useState(false);
  const path = `/library/items/${id}/anime`;
  const resource = useResource<AnimeLibrarySettings>(open ? path : null);
  return (
    <section
      className="panel settings-form"
      aria-label="Library section and anime numbering"
    >
      <h2>Library section</h2>
      {!open ? (
        <>
          <p className="field-help">
            Choose Movies, TV shows or Anime, and review absolute episode
            numbers for anime shows.
          </p>
          <Button
            variant="soft"
            onClick={() => {
              setOpen(true);
              setSuccess(false);
            }}
          >
            Edit library section
          </Button>
          {success && (
            <p role="status" className="save-success">
              Library section and episode numbering saved.
            </p>
          )}
        </>
      ) : (
        <>
          <RequestState
            loading={resource.loading}
            error={resource.error}
            retry={resource.reload}
          />
          {resource.data && !resource.loading && !resource.error && (
            <AnimeSettingsForm
              key={`${resource.data.revision}-${resource.data.numbering_revision}`}
              initial={resource.data}
              path={path}
              reload={resource.reload}
              close={() => setOpen(false)}
              saved={() => {
                setOpen(false);
                setSuccess(true);
                onSaved?.();
              }}
            />
          )}
          {!resource.data && !resource.loading && (
            <Button variant="soft" color="gray" onClick={() => setOpen(false)}>
              Cancel
            </Button>
          )}
        </>
      )}
    </section>
  );
}

function episodeLabel(episode: AnimeLibrarySettings["episodes"][number]) {
  return `S${String(episode.season).padStart(2, "0")}E${String(episode.number).padStart(2, "0")} · ${episode.title}`;
}

function AnimeSettingsForm({
  initial,
  path,
  reload,
  close,
  saved,
}: {
  initial: AnimeLibrarySettings;
  path: string;
  reload: () => void;
  close: () => void;
  saved: () => void;
}) {
  const [category, setCategory] = useState(initial.category);
  const [numbers, setNumbers] = useState<Record<number, string>>(() =>
    Object.fromEntries(
      initial.episodes.map((episode) => [
        episode.episode_id,
        episode.absolute_number === null ? "" : String(episode.absolute_number),
      ]),
    ),
  );
  const seasons = [
    ...new Set(initial.episodes.map((episode) => episode.season)),
  ].sort((a, b) => a - b);
  const [season, setSeason] = useState(seasons[0] ?? 1);
  const [page, setPage] = useState(1);
  const [reviewed, setReviewed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState("");
  const episodes = initial.episodes.filter(
    (episode) => episode.season === season,
  );
  const changed = initial.episodes.filter(
    (episode) =>
      (numbers[episode.episode_id] ?? "") !==
      (episode.absolute_number === null ? "" : String(episode.absolute_number)),
  );
  const dirty = category !== initial.category || changed.length > 0;
  const standardLabel = initial.kind === "movie" ? "Movies" : "TV shows";
  function validate() {
    const seen = new Set<number>();
    for (const episode of initial.episodes) {
      const value = numbers[episode.episode_id] ?? "";
      if (!value) continue;
      if (
        !/^[1-9][0-9]*$/.test(value) ||
        !Number.isSafeInteger(Number(value)) ||
        Number(value) > 100000
      ) {
        return "Absolute episode numbers must be whole numbers from 1 to 100000, or blank when unknown.";
      }
      if (seen.has(Number(value)))
        return "Each absolute number must identify one episode. Clear or correct duplicate numbers before reviewing.";
      seen.add(Number(value));
    }
    return "";
  }
  function review() {
    const message = validate();
    setError(message);
    if (!message) setReviewed(true);
  }
  async function save() {
    if (!reviewed || saving || stale || !dirty) return;
    const message = validate();
    if (message) {
      setError(message);
      setReviewed(false);
      return;
    }
    setSaving(true);
    setError("");
    try {
      await api<AnimeLibrarySettings>(path, {
        method: "PUT",
        body: JSON.stringify({
          category,
          revision: initial.revision,
          numbering_revision: initial.numbering_revision,
          episodes: initial.episodes.map((episode) => ({
            episode_id: episode.episode_id,
            absolute_number: numbers[episode.episode_id]
              ? Number(numbers[episode.episode_id])
              : null,
          })),
        }),
      });
      saved();
    } catch (e) {
      setError(
        `${messageFor(e)} Reload the saved section and numbering before another attempt.`,
      );
      setStale(true);
      setReviewed(false);
    } finally {
      setSaving(false);
    }
  }
  return (
    <div className="settings-form">
      <label>
        Library section
        <select
          aria-label="Library section"
          value={category}
          disabled={saving || stale}
          onChange={(event) => {
            setCategory(event.target.value as typeof category);
            setReviewed(false);
          }}
        >
          <option value="standard">{standardLabel}</option>
          <option value="anime">Anime</option>
        </select>
      </label>
      <p className="field-help">
        Changing section keeps the title identity, monitoring and episode
        choices. Existing files stay in place; organize them separately after
        reviewing their destinations.
      </p>
      {initial.kind === "series" && (
        <>
          <h3>Absolute episode numbers</h3>
          <p className="field-help">
            Enter numbers from the release's episode order. Blank means unknown.
            These aliases keep the saved TMDb season and episode identities;
            numbers are never inferred from season order. Load missing seasons
            from the episode list before editing them here.
          </p>
          {category === "anime" && (
            <p className="field-help">
              Automatic Anime searches are off until enabled in this show's
              Automatic Anime search settings. Absolute mode requires these
              explicit numbers. Completed episode files still need mapping
              review.
            </p>
          )}
          {seasons.length > 0 ? (
            <>
              <label>
                Season
                <select
                  aria-label="Numbering season"
                  disabled={saving || stale}
                  value={season}
                  onChange={(event) => {
                    setSeason(Number(event.target.value));
                    setPage(1);
                  }}
                >
                  {seasons.map((number) => (
                    <option value={number} key={number}>
                      {number === 0 ? "Specials" : `Season ${number}`}
                    </option>
                  ))}
                </select>
              </label>
              <ul className="episode-list" aria-label="Episode numbering">
                {episodes.slice((page - 1) * 50, page * 50).map((episode) => (
                  <li key={episode.episode_id}>
                    <div>
                      <strong>{episodeLabel(episode)}</strong>
                    </div>
                    <label>
                      Absolute number
                      <TextField.Root
                        aria-label={`Absolute number for ${episodeLabel(episode)}`}
                        inputMode="numeric"
                        maxLength={6}
                        value={numbers[episode.episode_id] ?? ""}
                        disabled={saving || stale}
                        onChange={(event) => {
                          setNumbers((current) => ({
                            ...current,
                            [episode.episode_id]: event.target.value,
                          }));
                          setReviewed(false);
                        }}
                      />
                    </label>
                  </li>
                ))}
              </ul>
              <Pagination
                page={page}
                pages={Math.ceil(episodes.length / 50)}
                onPage={setPage}
              />
            </>
          ) : (
            <p>No episodes are loaded yet.</p>
          )}
        </>
      )}
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      {reviewed && (
        <section aria-label="Review library section changes">
          <h3>Review changes</h3>
          <p>
            {initial.category === "anime" ? "Anime" : standardLabel} →{" "}
            {category === "anime" ? "Anime" : standardLabel}
          </p>
          <p>
            {changed.length} absolute{" "}
            {changed.length === 1 ? "number" : "numbers"} will change.
          </p>
          {changed.length > 0 && (
            <ul className="episode-list">
              {changed.map((episode) => (
                <li key={episode.episode_id}>
                  <span>{episodeLabel(episode)}</span>
                  <span>
                    {episode.absolute_number ?? "Unknown"} →{" "}
                    {numbers[episode.episode_id] || "Unknown"}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
      <div className="monitoring-dialog-actions">
        <Button variant="soft" color="gray" disabled={saving} onClick={close}>
          {dirty ? "Cancel" : "Done"}
        </Button>
        {stale ? (
          <Button variant="soft" onClick={reload}>
            Reload saved section and numbering
          </Button>
        ) : reviewed ? (
          <Button disabled={saving || !dirty} onClick={() => void save()}>
            {saving ? "Saving…" : "Save reviewed changes"}
          </Button>
        ) : (
          <Button disabled={!dirty || saving} onClick={review}>
            Review changes
          </Button>
        )}
      </div>
    </div>
  );
}
