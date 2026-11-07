import { useId, useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";
import {
  Alert,
  DescriptionItem,
  DescriptionList,
  FormActions,
  FormGroup,
  Label,
  Table,
} from "../ui";
import "./AnimeOrder.css";

export type AnimeSearchPolicy = {
  media_id: number;
  canonical_title: string;
  category: string;
  revision: number;
  category_revision: number;
  numbering_revision: number;
  mode: "disabled" | "season" | "absolute" | "mapped";
  include_specials?: boolean;
  allow_packs?: boolean;
  max_pack_episodes?: number;
  order_revision?: number;
  approved_order_revision?: number;
  mapped_count?: number;
  missing_mapping_count?: number;
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
  mapped: "Saved provider mappings",
};
const modeHelp = {
  disabled: "This show is excluded from automatic episode searches.",
  season: "Searches use the saved TMDb season and episode numbers.",
  absolute:
    "Searches use only the absolute numbers entered for each episode; episodes without one are skipped.",
  mapped:
    "Searches use only the exact release titles and typed labels you reviewed. Episode order is never inferred.",
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
      className="rs-fieldset anime-section"
      aria-label="Anime automatic searches"
    >
      <div className="rs-legend">
        <h2>Anime automatic searches</h2>
        {!open && (
          <div className="rs-legend-actions">
            <Button
              size="1"
              variant="soft"
              onClick={() => {
                setOpen(true);
                setSuccess(false);
              }}
            >
              Edit automatic searches
            </Button>
          </div>
        )}
      </div>
      <p className="rs-fieldset-description">
        Off by default for each show. The schedule and search-only or download
        mode stay in Automation.
      </p>
      {open && (
        <>
          <RequestState
            loading={resource.loading}
            error={resource.error}
            retry={resource.reload}
          />
          {resource.data && !resource.loading && !resource.error && (
            <AnimeSearchForm
              key={`${resource.data.media_id}-${resource.data.revision}-${resource.data.category_revision}-${resource.data.numbering_revision}-${resource.data.order_revision ?? 0}-${resource.data.approved_order_revision ?? 0}`}
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
      {success && (
        <Alert kind="success">
          Anime search settings saved. Other automation and monitoring settings
          are unchanged.
        </Alert>
      )}
    </section>
  );
}

function OptIn({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="rs-check">
      <input
        type="checkbox"
        aria-label={label}
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span>{label}</span>
    </label>
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
  const ids = useId();
  const [mode, setMode] = useState(initial.mode);
  const [alternateText, setAlternateText] = useState(
    initial.alternate_titles.join("\n"),
  );
  const [searchTitle, setSearchTitle] = useState(initial.search_title);
  const [includeSpecials, setIncludeSpecials] = useState(
    initial.include_specials ?? false,
  );
  const [allowPacks, setAllowPacks] = useState(initial.allow_packs ?? false);
  const [maxPackEpisodes, setMaxPackEpisodes] = useState(
    String(initial.max_pack_episodes ?? 20),
  );
  const [reviewed, setReviewed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState("");
  const alternateTitles = titlesFrom(alternateText);
  const choices = [...new Set(alternateTitles)].filter(
    (title) => title !== initial.canonical_title,
  );
  const unapproved =
    (initial.order_revision ?? 0) !== (initial.approved_order_revision ?? 0);
  const dirty =
    mode !== initial.mode ||
    includeSpecials !== (initial.include_specials ?? false) ||
    allowPacks !== (initial.allow_packs ?? false) ||
    Number(maxPackEpisodes) !== (initial.max_pack_episodes ?? 20) ||
    (mode === "mapped" && unapproved) ||
    searchTitle !== initial.search_title ||
    JSON.stringify(alternateTitles) !==
      JSON.stringify(initial.alternate_titles);
  const locked = saving || stale;
  function validate() {
    if (mode === "mapped") {
      if (!(initial.order_revision && (initial.mapped_count ?? 0) > 0))
        return "Save at least one loaded episode mapping in Anime identity & episode mapping before enabling mapped searches.";
      if (
        !/^\d+$/.test(maxPackEpisodes) ||
        Number(maxPackEpisodes) < 2 ||
        Number(maxPackEpisodes) > 100
      )
        return "Choose a maximum pack size of 2–100 episodes.";
    }
    if (alternateTitles.length > 8)
      return "Use at most eight alternate titles, one per line.";
    const seen = new Set(
      mode === "season" || mode === "absolute"
        ? [titleKey(initial.canonical_title)]
        : [],
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
      (mode === "season" || mode === "absolute") &&
      searchTitle &&
      searchTitle !== initial.canonical_title &&
      !alternateTitles.includes(searchTitle)
    )
      return "Choose the canonical title or one of the alternate titles entered here.";
    if (
      (mode === "season" || mode === "absolute") &&
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
          include_specials: includeSpecials,
          allow_packs: allowPacks,
          max_pack_episodes: Number(maxPackEpisodes),
          ...(mode === "mapped"
            ? { order_revision: initial.order_revision }
            : {}),
        }),
      });
      saved();
    } catch (e) {
      setError(
        `${messageFor(e)} Reload the saved search policy, numbering and provider mappings before another attempt.`,
      );
      setStale(true);
      setReviewed(false);
    } finally {
      setSaving(false);
    }
  }
  return (
    <div className="anime-form">
      <FormGroup
        label="Numbering"
        htmlFor={`${ids}-mode`}
        help={modeHelp[mode]}
      >
        <select
          id={`${ids}-mode`}
          aria-label="Anime automatic search numbering"
          value={mode}
          disabled={locked}
          onChange={(event) => {
            setMode(event.target.value as typeof mode);
            if (
              !/^\d+$/.test(maxPackEpisodes) ||
              Number(maxPackEpisodes) < 2 ||
              Number(maxPackEpisodes) > 100
            )
              setMaxPackEpisodes(String(initial.max_pack_episodes ?? 20));
            setReviewed(false);
          }}
        >
          <option value="disabled">Off</option>
          <option value="season">Season and episode (S01E01)</option>
          <option value="absolute">Explicit absolute number (001)</option>
          <option value="mapped">
            Saved provider mappings (including specials and packs)
          </option>
        </select>
      </FormGroup>
      <FormGroup
        label="Coverage"
        help="Saved numbering coverage, not download eligibility. Searches still require monitored, released, missing episodes."
      >
        <DescriptionList>
          <DescriptionItem title="Loaded episodes">
            {initial.loaded_count}
          </DescriptionItem>
          <DescriptionItem title="Ready under saved settings">
            {initial.ready_count}
          </DescriptionItem>
          <DescriptionItem title="Without an absolute number">
            {initial.missing_number_count}
          </DescriptionItem>
          {mode === "mapped" && (
            <>
              <DescriptionItem title="Loaded episodes with a saved mapping">
                {initial.mapped_count ?? 0}
              </DescriptionItem>
              <DescriptionItem title="Loaded episodes without a mapping">
                {initial.missing_mapping_count ?? 0}
              </DescriptionItem>
              <DescriptionItem title="Mapping revision">
                <span className="rs-row">
                  {initial.order_revision ?? 0}
                  {unapproved ? (
                    <Label kind="warning">Pending approval</Label>
                  ) : (
                    <Label kind="success">Approved</Label>
                  )}
                </span>
              </DescriptionItem>
              <DescriptionItem title="Approved mapping revision">
                {initial.approved_order_revision ?? 0}
              </DescriptionItem>
            </>
          )}
        </DescriptionList>
      </FormGroup>
      {mode === "mapped" ? (
        <>
          {unapproved && (
            <Alert kind="warning" title="Mappings need approval">
              The current provider mappings have not been approved for automatic
              searching. Check them in Anime identity &amp; episode mapping,
              then review and save these settings. Mapped searches wait until
              you approve the current revision.
            </Alert>
          )}
          <FormGroup
            label="Specials"
            help="Includes season 0 and mappings labeled S00E, SP, OVA, ONA or OAD. Monitoring and valid release dates are still required."
          >
            <OptIn
              label="Include mapped specials"
              checked={includeSpecials}
              disabled={locked}
              onChange={(value) => {
                setIncludeSpecials(value);
                setReviewed(false);
              }}
            />
          </FormGroup>
          <FormGroup
            label="Packs"
            help="A pack must name the complete contiguous group saved for one release title and label family. Every mapped episode in that group must qualify."
          >
            <OptIn
              label="Allow mapped episode packs"
              checked={allowPacks}
              disabled={locked}
              onChange={(value) => {
                setAllowPacks(value);
                setReviewed(false);
              }}
            />
          </FormGroup>
          <FormGroup
            label="Maximum pack size"
            htmlFor={`${ids}-max`}
            help="2–100 episodes. The quality profile's size limit applies to the whole release."
          >
            <input
              id={`${ids}-max`}
              className="anime-number"
              aria-label="Maximum episodes per mapped pack"
              type="number"
              min={2}
              max={100}
              step={1}
              value={maxPackEpisodes}
              disabled={locked || !allowPacks}
              onChange={(event) => {
                setMaxPackEpisodes(event.target.value);
                setReviewed(false);
              }}
            />
          </FormGroup>
        </>
      ) : (
        <>
          <FormGroup
            label="Alternate titles"
            htmlFor={`${ids}-aliases`}
            help="Up to eight exact release names, one per line, 200 UTF-8 bytes each. They are matching evidence only; the catalog title is unchanged."
          >
            <textarea
              id={`${ids}-aliases`}
              aria-label="Anime alternate release titles"
              rows={5}
              value={alternateText}
              disabled={locked}
              onChange={(event) => {
                const next = event.target.value;
                setAlternateText(next);
                if (searchTitle && !titlesFrom(next).includes(searchTitle))
                  setSearchTitle("");
                setReviewed(false);
              }}
            />
          </FormGroup>
          <FormGroup
            label="Search title"
            htmlFor={`${ids}-title`}
            help="Sent to indexers; must fit 180 UTF-8 bytes. Only single matching episodes are queued automatically; packs and ranges need manual selection."
          >
            <select
              id={`${ids}-title`}
              aria-label="Anime search title"
              value={searchTitle === initial.canonical_title ? "" : searchTitle}
              disabled={locked}
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
          </FormGroup>
        </>
      )}
      <p className="rs-form-help anime-indent">
        Completed files still require mapping review in Activity before import.
      </p>
      <details className="anime-details anime-indent">
        <summary>How numbering modes work</summary>
        <p>
          Season and episode uses the saved TMDb ordering. Absolute mode uses
          only the episode numbers you entered in the episode list. Saved
          provider mappings use only the exact release titles and typed episode
          labels you reviewed; specials and packs need their own consent.
          Episode order is never inferred from provider titles or counts. Mapped
          searches select only missing, unreserved episodes.
        </p>
      </details>
      {error && <Alert kind="danger">{error}</Alert>}
      {reviewed && (
        <section
          className="anime-review"
          aria-label="Review Anime automatic search changes"
        >
          <h3>Review automatic search changes</h3>
          <Table label="Reviewed search settings" className="anime-diff">
            <tbody>
              <tr>
                <th scope="row">Numbering</th>
                <td>
                  {modeLabels[initial.mode]} → {modeLabels[mode]}
                </td>
              </tr>
              {mode === "mapped" ? (
                <>
                  <tr>
                    <th scope="row">Mapping revision being approved</th>
                    <td>{initial.order_revision ?? 0}</td>
                  </tr>
                  <tr>
                    <th scope="row">Mapped specials</th>
                    <td>{includeSpecials ? "Included" : "Excluded"}</td>
                  </tr>
                  <tr>
                    <th scope="row">Explicit range packs</th>
                    <td>
                      {allowPacks
                        ? `Allowed, up to ${maxPackEpisodes} episodes`
                        : "Excluded"}
                    </td>
                  </tr>
                </>
              ) : (
                <>
                  <tr>
                    <th scope="row">Search title</th>
                    <td>{searchTitle || initial.canonical_title}</td>
                  </tr>
                  <tr>
                    <th scope="row">Alternate titles</th>
                    <td>
                      {alternateTitles.length
                        ? alternateTitles.join(" · ")
                        : "None"}
                    </td>
                  </tr>
                </>
              )}
            </tbody>
          </Table>
          <p className="rs-form-help">
            {mode === "disabled"
              ? "This show will be excluded from automatic episode searches. Existing downloads continue."
              : "This show can be included in enabled or requested searches. The Automation mode decides whether matches are listed or downloaded. Existing files are preserved."}
            {mode === "mapped" &&
              " Only exact saved mapping titles and labels are accepted, every selected target must qualify, and file mapping review remains required before import."}
          </p>
        </section>
      )}
      <FormActions>
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
      </FormActions>
    </div>
  );
}
