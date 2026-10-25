import { useState } from "react";
import { Button, TextArea } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";

export type AnimeSearchPolicy = {
  media_id: number;
  canonical_title: string;
  category: string;
  revision: number;
  category_revision: number;
  numbering_revision: number;
  mode: "disabled" | "season" | "absolute";
  search_title: string;
  alternate_titles: string[];
  loaded_count: number;
  ready_count: number;
  missing_number_count: number;
};

const modeLabels = {
  disabled: "Off",
  season: "Season and episode",
  absolute: "Absolute episode number",
};
function titlesFrom(value: string) {
  return value
    .split(/\r?\n/)
    .map((title) => title.trim())
    .filter(Boolean);
}
function titleKey(title: string) {
  return title
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim();
}

export function AnimeSearch({ id }: { id: number }) {
  const [open, setOpen] = useState(false);
  const [success, setSuccess] = useState(false);
  const path = `/library/items/${id}/anime-search`;
  const resource = useResource<AnimeSearchPolicy>(open ? path : null);
  return (
    <section
      className="panel settings-form"
      aria-label="Anime automatic searches"
    >
      <h2>Anime automatic searches</h2>
      <p className="field-help">
        Automatic searches start off for each anime show. Choose its release
        numbering and explicit alternate titles before enabling it. The schedule
        and search-only or download mode remain controlled in Automation.
      </p>
      {!open ? (
        <Button
          variant="soft"
          onClick={() => {
            setOpen(true);
            setSuccess(false);
          }}
        >
          Edit automatic searches
        </Button>
      ) : (
        <>
          <RequestState
            loading={resource.loading}
            error={resource.error}
            retry={resource.reload}
          />
          {resource.data && !resource.loading && !resource.error && (
            <AnimeSearchForm
              key={`${resource.data.revision}-${resource.data.category_revision}-${resource.data.numbering_revision}`}
              initial={resource.data}
              path={path}
              reload={() => {
                setSuccess(false);
                resource.reload();
              }}
              close={() => setOpen(false)}
              saved={() => {
                setSuccess(true);
                resource.reload();
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
      {success && (
        <p role="status" className="save-success">
          Anime search settings saved. Other automation and monitoring settings
          are unchanged.
        </p>
      )}
    </section>
  );
}

function AnimeSearchForm({
  initial,
  path,
  reload,
  close,
  saved,
}: {
  initial: AnimeSearchPolicy;
  path: string;
  reload: () => void;
  close: () => void;
  saved: () => void;
}) {
  const [mode, setMode] = useState(initial.mode);
  const [alternateText, setAlternateText] = useState(
    initial.alternate_titles.join("\n"),
  );
  const [searchTitle, setSearchTitle] = useState(initial.search_title);
  const [reviewed, setReviewed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState("");
  const alternateTitles = titlesFrom(alternateText);
  const choices = [...new Set(alternateTitles)].filter(
    (title) => title !== initial.canonical_title,
  );
  const dirty =
    mode !== initial.mode ||
    searchTitle !== initial.search_title ||
    JSON.stringify(alternateTitles) !==
      JSON.stringify(initial.alternate_titles);
  function validate() {
    if (alternateTitles.length > 8)
      return "Use at most eight alternate titles, one per line.";
    const seen = new Set(
      mode === "disabled" ? [] : [titleKey(initial.canonical_title)],
    );
    for (const title of alternateTitles) {
      if (
        new TextEncoder().encode(title).length > 200 ||
        /[\u0000-\u001f\u007f]/u.test(title) ||
        !titleKey(title)
      )
        return "Each alternate title must contain letters or numbers, no control characters, and at most 200 bytes.";
      const key = titleKey(title);
      if (seen.has(key))
        return "Alternate titles must be distinct from each other and the canonical title.";
      seen.add(key);
    }
    if (
      mode !== "disabled" &&
      searchTitle &&
      searchTitle !== initial.canonical_title &&
      !alternateTitles.includes(searchTitle)
    )
      return "Choose the canonical title or one of the alternate titles entered here.";
    if (
      mode !== "disabled" &&
      new TextEncoder().encode(searchTitle || initial.canonical_title).length >
        180
    )
      return "Choose a search title that fits 180 UTF-8 bytes, leaving room for episode numbers.";
    return "";
  }
  function review() {
    const message = validate();
    setError(message);
    if (!message) setReviewed(true);
  }
  async function save() {
    if (!reviewed || stale || saving || !dirty) return;
    const message = validate();
    if (message) {
      setError(message);
      setReviewed(false);
      return;
    }
    setSaving(true);
    setError("");
    try {
      await api<AnimeSearchPolicy>(path, {
        method: "PUT",
        body: JSON.stringify({
          revision: initial.revision,
          category_revision: initial.category_revision,
          numbering_revision: initial.numbering_revision,
          mode,
          search_title:
            searchTitle === initial.canonical_title ? "" : searchTitle,
          alternate_titles: alternateTitles,
        }),
      });
      saved();
    } catch (e) {
      setError(
        `${messageFor(e)} Reload the saved search policy and episode numbering before another attempt.`,
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
        Automatic search numbering
        <select
          aria-label="Anime automatic search numbering"
          value={mode}
          disabled={saving || stale}
          onChange={(event) => {
            setMode(event.target.value as typeof mode);
            setReviewed(false);
          }}
        >
          <option value="disabled">Off</option>
          <option value="season">Season and episode (S01E01)</option>
          <option value="absolute">Explicit absolute number (001)</option>
        </select>
      </label>
      <p className="field-help">
        Season and episode uses the saved TMDb ordering. Absolute mode uses only
        the episode numbers you entered in Library section; episodes without a
        number are skipped. Neither mode guesses an episode order. Specials
        require manual searches; the counts below cover loaded regular seasons.
      </p>
      <dl className="detail-paths">
        <dt>Loaded episodes</dt>
        <dd>{initial.loaded_count}</dd>
        <dt>Ready under saved settings</dt>
        <dd>{initial.ready_count}</dd>
        <dt>Without an absolute number</dt>
        <dd>{initial.missing_number_count}</dd>
      </dl>
      <label>
        Alternate release titles, one per line
        <TextArea
          aria-label="Anime alternate release titles"
          rows={5}
          value={alternateText}
          disabled={saving || stale}
          onChange={(event) => {
            const next = event.target.value;
            setAlternateText(next);
            if (searchTitle && !titlesFrom(next).includes(searchTitle))
              setSearchTitle("");
            setReviewed(false);
          }}
        />
      </label>
      <p className="field-help">
        Add up to eight exact names used by releases, such as a romanized or
        translated title. Each may contain up to 200 UTF-8 bytes. These names
        are matching evidence, not a change to the catalog title.
      </p>
      <label>
        Title used for the search
        <select
          aria-label="Anime search title"
          value={searchTitle === initial.canonical_title ? "" : searchTitle}
          disabled={saving || stale}
          onChange={(event) => {
            setSearchTitle(event.target.value);
            setReviewed(false);
          }}
        >
          <option value="">{initial.canonical_title} (canonical)</option>
          {choices.map((title) => (
            <option key={title} value={title}>
              {title}
            </option>
          ))}
        </select>
      </label>
      <p className="field-help">
        The chosen name is sent to indexers and must fit 180 UTF-8 bytes when
        enabled. The canonical name and explicit alternate titles are accepted
        for exact matching. Only a single matching episode can be queued
        automatically; packs and ranges need manual selection. Completed files
        still require mapping review in Activity before import.
      </p>
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      {reviewed && (
        <section aria-label="Review Anime automatic search changes">
          <h3>Review automatic search changes</h3>
          <dl className="detail-paths">
            <dt>Numbering</dt>
            <dd>
              {modeLabels[initial.mode]} → {modeLabels[mode]}
            </dd>
            <dt>Search title</dt>
            <dd>{searchTitle || initial.canonical_title}</dd>
            <dt>Alternate titles</dt>
            <dd>
              {alternateTitles.length ? alternateTitles.join(" · ") : "None"}
            </dd>
          </dl>
          {mode === "disabled" ? (
            <p>
              This show will be excluded from automatic episode searches.
              Existing downloads continue.
            </p>
          ) : (
            <p>
              This show can be included in enabled or requested searches. The
              global Automation mode controls whether matches are listed or
              downloaded. Existing files are preserved.
            </p>
          )}
        </section>
      )}
      <div className="monitoring-dialog-actions">
        <Button color="gray" variant="soft" disabled={saving} onClick={close}>
          {dirty ? "Cancel" : "Done"}
        </Button>
        {stale ? (
          <Button variant="soft" onClick={reload}>
            Reload saved search policy
          </Button>
        ) : reviewed ? (
          <Button disabled={saving || !dirty} onClick={() => void save()}>
            {saving ? "Saving…" : "Save reviewed search settings"}
          </Button>
        ) : (
          <Button disabled={!dirty || saving} onClick={review}>
            Review search changes
          </Button>
        )}
      </div>
    </div>
  );
}
