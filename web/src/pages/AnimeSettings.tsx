import { useId, useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor, type AnimeLibrarySettings } from "../api";
import { RequestState } from "../components";
import { Pagination } from "../movieComponents";
import {
  Alert,
  FieldSet,
  FormActions,
  FormGroup,
  Table,
  episodeCode,
} from "../ui";
import { useResource } from "../useResource";
import "./AnimeSettings.css";

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
    <section aria-label="Library section and anime numbering">
      <FieldSet
        legend="Library section"
        actions={
          !open && (
            <Button
              size="1"
              variant="soft"
              onClick={() => {
                setOpen(true);
                setSuccess(false);
              }}
            >
              Edit library section
            </Button>
          )
        }
      >
        {!open ? (
          <>
            <p className="rs-muted">
              Choose Movies, TV Shows or Anime, and review absolute episode
              numbers for anime shows.
            </p>
            {success && (
              <Alert kind="success" role="status">
                Library section and episode numbering saved.
              </Alert>
            )}
          </>
        ) : (
          <>
            <RequestState
              loading={resource.loading}
              error={resource.error}
              code={resource.code}
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
              <FormActions>
                <Button
                  variant="soft"
                  color="gray"
                  onClick={() => setOpen(false)}
                >
                  Cancel
                </Button>
              </FormActions>
            )}
          </>
        )}
      </FieldSet>
    </section>
  );
}

function episodeLabel(episode: AnimeLibrarySettings["episodes"][number]) {
  return `${episodeCode(episode.season, episode.number)} · ${episode.title}`;
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
  const sectionId = useId(),
    seasonId = useId();
  const episodes = initial.episodes.filter(
    (episode) => episode.season === season,
  );
  const changed = initial.episodes.filter(
    (episode) =>
      (numbers[episode.episode_id] ?? "") !==
      (episode.absolute_number === null ? "" : String(episode.absolute_number)),
  );
  const dirty = category !== initial.category || changed.length > 0;
  const standardLabel = initial.kind === "movie" ? "Movies" : "TV Shows";
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
    <div className="rs-stack">
      <FormGroup
        label="Library section"
        htmlFor={sectionId}
        help="Keeps the title identity, monitoring and episode choices. Existing files stay in place until you organize them."
      >
        <select
          id={sectionId}
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
      </FormGroup>
      {initial.kind === "series" && (
        <FieldSet
          legend="Absolute episode numbers"
          headingLevel={3}
          description="Numbers from the release's episode order; blank means unknown. They keep the saved TMDb seasons and episodes and are never inferred. Load missing seasons from the episode list first."
        >
          {category === "anime" && (
            <p className="rs-form-help is-warning">
              Automatic Anime searches are off until enabled in this show's
              Automatic Anime search settings. Absolute mode requires these
              explicit numbers, and completed files still need mapping review.
            </p>
          )}
          {seasons.length > 0 ? (
            <>
              <FormGroup label="Season" htmlFor={seasonId}>
                <select
                  id={seasonId}
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
              </FormGroup>
              <Table label="Episode numbering">
                <thead>
                  <tr>
                    <th scope="col">Episode</th>
                    <th scope="col" className="is-shrink">
                      Absolute number
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {episodes.slice((page - 1) * 50, page * 50).map((episode) => (
                    <tr key={episode.episode_id}>
                      <th scope="row">{episodeLabel(episode)}</th>
                      <td className="is-shrink">
                        <TextField.Root
                          className="lib-anime-number"
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
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
              <Pagination
                page={page}
                pages={Math.ceil(episodes.length / 50)}
                onPage={setPage}
              />
            </>
          ) : (
            <p className="rs-muted">No episodes are loaded yet.</p>
          )}
        </FieldSet>
      )}
      {error && <Alert kind="danger">{error}</Alert>}
      {reviewed && (
        <section
          className="lib-anime-review"
          aria-label="Review library section changes"
        >
          <h3>Review changes</h3>
          <p>
            Section: {initial.category === "anime" ? "Anime" : standardLabel} →{" "}
            {category === "anime" ? "Anime" : standardLabel}
          </p>
          <p>
            {changed.length} absolute{" "}
            {changed.length === 1 ? "number" : "numbers"} will change.
          </p>
          {changed.length > 0 && (
            <Table label="Changed absolute numbers">
              <tbody>
                {changed.map((episode) => (
                  <tr key={episode.episode_id}>
                    <th scope="row">{episodeLabel(episode)}</th>
                    <td>
                      {episode.absolute_number ?? "Unknown"} →{" "}
                      {numbers[episode.episode_id] || "Unknown"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </section>
      )}
      <FormActions>
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
        <Button variant="soft" color="gray" disabled={saving} onClick={close}>
          {dirty ? "Cancel" : "Done"}
        </Button>
      </FormActions>
    </div>
  );
}
