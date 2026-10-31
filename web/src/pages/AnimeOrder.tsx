import { useEffect, useRef, useState } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { Link } from "react-router-dom";
import { APIError, api, messageFor, type Release } from "../api";
import { RequestState } from "../components";
import { Pagination } from "../movieComponents";
import { useResource } from "../useResource";
import { bytes } from "./Releases";

export type AnimeIdentity = {
  provider: "anilist" | "kitsu";
  id: number;
  title: string;
  romaji: string;
  english: string;
  native: string;
  format: string;
  year: number | null;
  episodes: number | null;
  url: string;
};
export type AnimeOrderEntry = {
  episode_id: number;
  provider_id: number;
  release_title: string;
  label: string;
};
export type AnimeOrderSettings = {
  provider?: "anilist" | "kitsu";
  media_id: number;
  revision: number;
  category_revision: number;
  numbering_revision: number;
  identities: AnimeIdentity[];
  entries: AnimeOrderEntry[];
  episodes: {
    episode_id: number;
    season: number;
    number: number;
    title: string;
    absolute_number: number | null;
  }[];
};
type Review = {
  review_id: string;
  settings: AnimeOrderSettings;
  expires_at: number;
};
type Source = {
  id: string;
  name: string;
  type: number;
  episode_count: number;
  group_count: number;
};
type Proposal = {
  source_name: string;
  groups: { id: string; name: string; order: number; episode_count: number }[];
  entries: AnimeOrderEntry[];
  unmatched: {
    tmdb_id: number;
    season: number;
    number: number;
    title: string;
  }[];
};
function canonicalLabel(episode: { season: number; number: number }) {
  return `S${String(episode.season).padStart(2, "0")}E${String(episode.number).padStart(2, "0")}`;
}
function titleKey(value: string) {
  return value
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim();
}
function labelKey(raw: string): string | null {
  const value = raw.trim().toUpperCase();
  if (/^[0-9]{1,6}$/.test(value)) {
    const number = Number(value);
    return number >= 1 && number <= 100000 ? String(number) : null;
  }
  const season = /^S([0-9]{1,5})E([0-9]{1,6})$/.exec(value);
  if (season)
    return Number(season[1]) <= 10000 &&
      Number(season[2]) >= 1 &&
      Number(season[2]) <= 100000
      ? `S${Number(season[1])}E${Number(season[2])}`
      : null;
  const typed = /^(SP|OVA|ONA|OAD)([0-9]{1,6})$/.exec(value);
  return typed && Number(typed[2]) >= 1 && Number(typed[2]) <= 100000
    ? typed[1] + Number(typed[2])
    : null;
}
function validTitle(value: string) {
  return (
    !!titleKey(value) &&
    new TextEncoder().encode(value).length <= 180 &&
    !/[\u0000-\u001f\u007f]/u.test(value)
  );
}
function expiryTime(value: number) {
  return value < 1e12 ? value * 1000 : value;
}

export function AnimeOrder({ id }: { id: number }) {
  const [open, setOpen] = useState(false),
    [success, setSuccess] = useState(false);
  const path = `/library/items/${id}/anime-order`;
  const resource = useResource<AnimeOrderSettings>(open ? path : null);
  return (
    <section
      className="panel settings-form"
      aria-label="Anime identity and episode mapping"
    >
      <h2>Anime identity & episode mapping</h2>
      <p className="field-help">
        Link exact AniList or Kitsu entries and map release labels to saved
        episodes, including specials. These choices support reviewed automatic
        searches, manual pack searches and completed-file suggestions. Canonical
        TMDb identities, monitoring and existing files stay unchanged. Automatic
        searching keeps its separate settings.
      </p>
      {!open ? (
        <Button
          variant="soft"
          onClick={() => {
            setOpen(true);
            setSuccess(false);
          }}
        >
          Edit Anime identity & mapping
        </Button>
      ) : (
        <>
          <RequestState
            loading={resource.loading}
            error={resource.error}
            retry={resource.reload}
          />
          {resource.data && !resource.loading && !resource.error && (
            <>
              <OrderEditor
                key={`${resource.data.revision}-${resource.data.category_revision}-${resource.data.numbering_revision}`}
                initial={resource.data}
                path={path}
                close={() => setOpen(false)}
                reload={() => {
                  setSuccess(false);
                  resource.reload();
                }}
                saved={() => {
                  setSuccess(true);
                  resource.reload();
                }}
              />
              {resource.data.numbering_revision > 0 &&
                resource.data.entries.length > 0 && (
                  <AnimePack
                    key={`pack-${resource.data.revision}-${resource.data.numbering_revision}`}
                    settings={resource.data}
                  />
                )}
            </>
          )}
          {!resource.data && !resource.loading && (
            <Button variant="soft" color="gray" onClick={() => setOpen(false)}>
              Close
            </Button>
          )}
        </>
      )}
      {success && (
        <p role="status" className="save-success">
          Anime identity and episode mapping saved. No search or file operation
          was started.
        </p>
      )}
    </section>
  );
}

function OrderEditor({
  initial,
  path,
  close,
  reload,
  saved,
}: {
  initial: AnimeOrderSettings;
  path: string;
  close: () => void;
  reload: () => void;
  saved: () => void;
}) {
  const [provider, setProvider] = useState<"anilist" | "kitsu">(
    initial.provider ?? "anilist",
  );
  const providerName = provider === "kitsu" ? "Kitsu" : "AniList";
  const [providerIDs, setProviderIDs] = useState(
    initial.identities.map((identity) => identity.id),
  );
  const [identityCache, setIdentityCache] = useState<
    Record<number, AnimeIdentity>
  >(() =>
    Object.fromEntries(
      initial.identities.map((identity) => [identity.id, identity]),
    ),
  );
  const [entries, setEntries] = useState<Record<number, AnimeOrderEntry>>(() =>
    Object.fromEntries(
      initial.entries.map((entry) => [entry.episode_id, entry]),
    ),
  );
  const [query, setQuery] = useState(""),
    [results, setResults] = useState<AnimeIdentity[]>();
  const [season, setSeason] = useState("all"),
    [page, setPage] = useState(1);
  const [review, setReview] = useState<Review>(),
    [checked, setChecked] = useState(false),
    [busy, setBusy] = useState(""),
    [error, setError] = useState(""),
    [stale, setStale] = useState(false);
  const [sources, setSources] = useState<Source[]>(),
    [groupID, setGroupID] = useState(""),
    [groups, setGroups] = useState<Proposal["groups"]>(),
    [subgroupID, setSubgroupID] = useState("");
  const [sourceProvider, setSourceProvider] = useState(0),
    [sourceTitle, setSourceTitle] = useState(""),
    [prefix, setPrefix] = useState("episode"),
    [start, setStart] = useState("1");
  const [proposal, setProposal] = useState<Proposal>(),
    [excludeConfirmed, setExcludeConfirmed] = useState(false),
    [proposalMessage, setProposalMessage] = useState("");
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const locked = !!busy || stale;
  const seasons = [
    ...new Set(initial.episodes.map((episode) => episode.season)),
  ].sort((a, b) => a - b);
  const shown = initial.episodes.filter(
    (episode) => season === "all" || episode.season === Number(season),
  );
  const draftEntries = initial.episodes
    .map((episode) => entries[episode.episode_id])
    .filter(
      (entry): entry is AnimeOrderEntry =>
        !!entry &&
        !!(
          entry.label.trim() ||
          entry.release_title.trim() ||
          entry.provider_id
        ),
    );
  const newProviderIDs = providerIDs.filter(
    (providerID) =>
      !initial.identities.some(
        (identity) =>
          identity.id === providerID && identity.provider === provider,
      ),
  );
  const dirty =
    provider !== (initial.provider ?? "anilist") ||
    JSON.stringify(providerIDs) !==
      JSON.stringify(initial.identities.map((identity) => identity.id)) ||
    JSON.stringify(draftEntries) !== JSON.stringify(initial.entries);
  function invalidate() {
    setReview(undefined);
    setChecked(false);
    setError("");
  }
  function editEpisode(
    episodeID: number,
    field: keyof AnimeOrderEntry,
    value: string | number,
  ) {
    invalidate();
    setEntries((current) => ({
      ...current,
      [episodeID]: {
        ...(current[episodeID] ?? {
          episode_id: episodeID,
          provider_id: 0,
          release_title: "",
          label: "",
        }),
        [field]: value,
      },
    }));
  }
  function validate() {
    if (providerIDs.length > 20) return "Link at most 20 provider entries.";
    if (newProviderIDs.length > 8)
      return "Add at most eight new provider links per review. Saved links can be retained, up to twenty total.";
    const labels = new Set<string>();
    for (const entry of draftEntries) {
      if (!validTitle(entry.release_title.trim()) || !labelKey(entry.label))
        return "Each mapped episode needs a release title of 1–180 UTF-8 bytes and a label such as 25, S00E01, SP1, OVA1, ONA1 or OAD1.";
      if (entry.provider_id && !providerIDs.includes(entry.provider_id))
        return "Every provider mapping must use a linked entry, or choose Manual label.";
      const key = `${titleKey(entry.release_title)}:${labelKey(entry.label)}`;
      if (labels.has(key))
        return "A release title and label must identify only one canonical episode, even when provider entries differ.";
      labels.add(key);
    }
    return "";
  }
  async function search() {
    if (locked || !query.trim()) return;
    setBusy("search");
    setError("");
    setResults(undefined);
    const controller = new AbortController();
    request.current?.abort();
    request.current = controller;
    try {
      const out = await api<{ items: AnimeIdentity[] }>(
        `/metadata/anime?query=${encodeURIComponent(query.trim())}${provider === "anilist" ? "" : "&provider=kitsu"}`,
        {
          signal: AbortSignal.any([
            controller.signal,
            AbortSignal.timeout(20000),
          ]),
        },
      );
      if (!controller.signal.aborted) {
        setResults(out.items);
        setIdentityCache((current) => ({
          ...current,
          ...Object.fromEntries(
            out.items.map((identity) => [identity.id, identity]),
          ),
        }));
      }
    } catch (e) {
      if (!controller.signal.aborted) setError(messageFor(e));
    } finally {
      if (!controller.signal.aborted) setBusy("");
    }
  }
  async function preview() {
    if (locked || !dirty) return;
    const message = validate();
    if (message) {
      setError(message);
      return;
    }
    setBusy("preview");
    invalidate();
    try {
      setReview(
        await api<Review>(path + "/preview", {
          method: "POST",
          body: JSON.stringify({
            revision: initial.revision,
            category_revision: initial.category_revision,
            numbering_revision: initial.numbering_revision,
            provider_ids: providerIDs,
            ...(provider === "kitsu" ? { provider } : {}),
            entries: draftEntries.map((entry) => ({
              ...entry,
              release_title: entry.release_title.trim(),
              label: entry.label.trim(),
            })),
          }),
          signal: AbortSignal.timeout(35000),
        }),
      );
    } catch (e) {
      setError(messageFor(e));
      if (e instanceof APIError && e.status === 409) setStale(true);
    } finally {
      setBusy("");
    }
  }
  async function apply() {
    if (!review || !checked || locked) return;
    if (expiryTime(review.expires_at) <= Date.now()) {
      setError(
        "This review expired. Reload the saved mappings before creating another review.",
      );
      setStale(true);
      setChecked(false);
      return;
    }
    setBusy("save");
    setError("");
    try {
      await api<AnimeOrderSettings>(path, {
        method: "PUT",
        body: JSON.stringify({ review_id: review.review_id }),
      });
      saved();
    } catch (e) {
      setError(
        `${messageFor(e)} Reload the saved mappings before another attempt.`,
      );
      setStale(true);
      setChecked(false);
    } finally {
      setBusy("");
    }
  }
  async function loadSources() {
    if (locked) return;
    setBusy("sources");
    setError("");
    try {
      const out = await api<{ items: Source[] }>(path + "/sources");
      setSources(out.items);
      setGroups(undefined);
      setProposal(undefined);
      setGroupID("");
      setSubgroupID("");
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }
  async function propose(subgroup: string) {
    if (locked || !groupID) return;
    if (
      !validTitle(sourceTitle.trim()) ||
      !/^[1-9][0-9]{0,5}$/.test(start) ||
      Number(start) > 100000
    ) {
      setError(
        "Choose an explicit release title and starting number from 1 to 100000 for the proposed labels.",
      );
      return;
    }
    setBusy("propose");
    setError("");
    setProposal(undefined);
    setExcludeConfirmed(false);
    try {
      const out = await api<Proposal>(path + "/propose", {
        method: "POST",
        body: JSON.stringify({
          group_id: groupID,
          subgroup_id: subgroup,
          provider_id: sourceProvider,
          release_title: sourceTitle.trim(),
          label_prefix: prefix,
          start_number: Number(start),
        }),
      });
      setGroups(out.groups);
      if (subgroup) {
        if (
          out.entries.some(
            (entry) =>
              !initial.episodes.some(
                (episode) => episode.episode_id === entry.episode_id,
              ),
          )
        ) {
          setError(
            "The loaded episode list changed. Reload the saved mapping before reviewing these proposals.",
          );
          setStale(true);
        } else setProposal(out);
      }
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy("");
    }
  }
  const replacementCount =
    proposal?.entries.filter(
      (entry) =>
        entries[entry.episode_id] &&
        JSON.stringify(entries[entry.episode_id]) !== JSON.stringify(entry),
    ).length ?? 0;
  return (
    <div className="settings-form">
      <h3>Linked {providerName} entries</h3>
      <label>
        Anime provider
        <select
          value={provider}
          disabled={locked || providerIDs.length > 0 || !!review}
          onChange={(e) => {
            invalidate();
            setProvider(e.target.value as "anilist" | "kitsu");
            setIdentityCache({});
            setResults(undefined);
            setSourceProvider(0);
            setProposal(undefined);
          }}
        >
          <option value="anilist">AniList</option>
          <option value="kitsu">Kitsu</option>
        </select>
      </label>
      <p className="field-help">
        Each title uses one identity provider. To switch, change linked episode
        rows to Manual label and unlink the old identities first. Saved release
        labels remain explicit.
      </p>
      <Button
        variant="soft"
        disabled={locked || dirty || !initial.identities.length}
        onClick={() =>
          void (async () => {
            setBusy("refresh");
            invalidate();
            try {
              setReview(
                await api<Review>(path + "/refresh", {
                  method: "POST",
                  body: JSON.stringify({ revision: initial.revision }),
                  signal: AbortSignal.timeout(75000),
                }),
              );
            } catch (e) {
              setError(messageFor(e));
            } finally {
              setBusy("");
            }
          })()
        }
      >
        {busy === "refresh"
          ? `Refreshing ${providerName}…`
          : `Review refreshed ${providerName} details`}
      </Button>
      {initial.numbering_revision === 0 && (
        <p className="field-help">
          This Anime film supports identity links only. Episode-order mapping
          and pack searches apply to episodic shows.
        </p>
      )}
      <p className="field-help">
        Keep up to twenty linked entries. Add at most eight new links per
        review; saved identity details are retained without another provider
        lookup.
      </p>
      <p className="field-help">
        Provider entries can describe a season, film or special rather than an
        entire TMDb show. Review the format, year and titles; an episode count
        is not an episode mapping. Linking does not replace TMDb identity or
        automatically add search aliases.
      </p>
      <ul className="episode-list">
        {providerIDs.map((providerID) => {
          const identity = identityCache[providerID];
          return (
            <li key={providerID}>
              <div>
                <strong>
                  {identity?.title ?? `${providerName} ${providerID}`}
                </strong>
                <p className="field-help">
                  {providerName} {providerID} · {identity?.format ?? ""} ·{" "}
                  {identity?.year ?? "Year unknown"}
                  {identity?.episodes ? ` · ${identity.episodes} episodes` : ""}
                </p>
                <p className="field-help">
                  {[identity?.romaji, identity?.english, identity?.native]
                    .filter(Boolean)
                    .join(" · ")}
                </p>
                <a
                  className="text-link"
                  href={`https://${provider === "kitsu" ? "kitsu.io" : "anilist.co"}/anime/${providerID}`}
                  target="_blank"
                  rel="noreferrer"
                >
                  View on {providerName}
                </a>
              </div>
              <Button
                color="gray"
                variant="soft"
                disabled={
                  locked ||
                  draftEntries.some((entry) => entry.provider_id === providerID)
                }
                onClick={() => {
                  invalidate();
                  setProviderIDs((current) =>
                    current.filter((value) => value !== providerID),
                  );
                  if (sourceProvider === providerID) {
                    setSourceProvider(0);
                    setProposal(undefined);
                  }
                }}
              >
                Unlink {providerID}
              </Button>
            </li>
          );
        })}
      </ul>
      {!providerIDs.length && <p>No {providerName} entries linked.</p>}
      <p className="field-help">
        Before unlinking an entry used by mappings, clear those rows or choose
        Manual label for them.
      </p>
      <form
        className="movie-search-form"
        onSubmit={(event) => {
          event.preventDefault();
          void search();
        }}
      >
        <TextField.Root
          aria-label={`Search ${providerName} titles`}
          maxLength={200}
          value={query}
          disabled={locked}
          onChange={(event) => setQuery(event.target.value)}
        />
        <Button type="submit" variant="soft" disabled={locked || !query.trim()}>
          {busy === "search"
            ? `Searching ${providerName}…`
            : `Search ${providerName}`}
        </Button>
      </form>
      {results && (
        <section aria-label={`${providerName} search results`}>
          {!results.length && <p>No {providerName} entries found.</p>}
          <ul className="episode-list">
            {results.map((identity) => (
              <li key={identity.id}>
                <div>
                  <strong>{identity.title}</strong>
                  <p className="field-help">
                    {identity.format} · {identity.year ?? "Year unknown"}
                    {identity.episodes
                      ? ` · ${identity.episodes} episodes`
                      : ""}{" "}
                    · {providerName} {identity.id}
                  </p>
                  <p className="field-help">
                    {[identity.romaji, identity.english, identity.native]
                      .filter(Boolean)
                      .join(" · ")}
                  </p>
                </div>
                <Button
                  variant="soft"
                  disabled={
                    locked ||
                    providerIDs.includes(identity.id) ||
                    providerIDs.length >= 20 ||
                    (newProviderIDs.length >= 8 &&
                      !initial.identities.some(
                        (savedIdentity) =>
                          savedIdentity.id === identity.id &&
                          savedIdentity.provider === provider,
                      ))
                  }
                  onClick={() => {
                    invalidate();
                    setProviderIDs((current) => [...current, identity.id]);
                  }}
                >
                  {providerIDs.includes(identity.id)
                    ? "Linked in draft"
                    : `Link ${providerName} ${identity.id}`}
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
      {initial.numbering_revision > 0 && (
        <>
          <h3>Release labels for canonical episodes</h3>
          {initial.episodes.length === 0 && (
            <p className="field-help">
              No canonical episodes are loaded. Load the show's seasons from its
              episode list before mapping labels. Provider-order proposals
              cannot create missing canonical episodes.
            </p>
          )}
          <p className="field-help">
            Keep a row blank when its label is unknown. Use a number, a
            season/episode label, or a typed special label such as SP1 or OVA1.
            SP1 and episode 1 are different labels. Existing absolute aliases
            and automatic-search settings are separate.
          </p>
          <label>
            Episode list
            <select
              aria-label="Mapping season"
              value={season}
              disabled={locked}
              onChange={(event) => {
                setSeason(event.target.value);
                setPage(1);
              }}
            >
              <option value="all">All seasons</option>
              {seasons.map((number) => (
                <option key={number} value={number}>
                  {number === 0 ? "Specials" : `Season ${number}`}
                </option>
              ))}
            </select>
          </label>
          <ul className="episode-list" aria-label="Canonical episode mappings">
            {shown.slice((page - 1) * 50, page * 50).map((episode) => {
              const entry = entries[episode.episode_id];
              const label = `${canonicalLabel(episode)} · ${episode.title}`;
              return (
                <li key={episode.episode_id}>
                  <div>
                    <strong>{label}</strong>
                    {episode.absolute_number !== null && (
                      <p className="field-help">
                        Existing absolute alias: {episode.absolute_number}
                      </p>
                    )}
                    <label>
                      Provider entry
                      <select
                        aria-label={`Provider for ${label}`}
                        value={entry?.provider_id ?? 0}
                        disabled={locked}
                        onChange={(event) =>
                          editEpisode(
                            episode.episode_id,
                            "provider_id",
                            Number(event.target.value),
                          )
                        }
                      >
                        <option value={0}>Manual label</option>
                        {providerIDs.map((providerID) => (
                          <option key={providerID} value={providerID}>
                            {identityCache[providerID]?.title ?? providerName} ·{" "}
                            {providerID}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label>
                      Release title
                      <TextField.Root
                        aria-label={`Release title for ${label}`}
                        maxLength={180}
                        value={entry?.release_title ?? ""}
                        disabled={locked}
                        onChange={(event) =>
                          editEpisode(
                            episode.episode_id,
                            "release_title",
                            event.target.value,
                          )
                        }
                      />
                    </label>
                    <label>
                      Release label
                      <TextField.Root
                        aria-label={`Release label for ${label}`}
                        maxLength={20}
                        value={entry?.label ?? ""}
                        disabled={locked}
                        onChange={(event) =>
                          editEpisode(
                            episode.episode_id,
                            "label",
                            event.target.value,
                          )
                        }
                      />
                    </label>
                    <Button
                      color="gray"
                      variant="soft"
                      disabled={locked || !entry}
                      onClick={() => {
                        invalidate();
                        setEntries((current) => {
                          const next = { ...current };
                          delete next[episode.episode_id];
                          return next;
                        });
                      }}
                    >
                      Clear mapping for {canonicalLabel(episode)}
                    </Button>
                  </div>
                </li>
              );
            })}
          </ul>
          <Pagination
            page={page}
            pages={Math.ceil(shown.length / 50)}
            onPage={setPage}
          />
          <section aria-label="TMDb episode order proposals">
            <h3>Propose labels from a TMDb episode order</h3>
            <p className="field-help">
              Load an available order, choose one subgroup and review the exact
              canonical episode IDs it contains. Group positions propose labels;
              they do not create missing episodes, guess identities or change
              files.
            </p>
            <Button
              variant="soft"
              disabled={locked}
              onClick={() => void loadSources()}
            >
              Load TMDb episode orders
            </Button>
            {sources &&
              (!sources.length ? (
                <p>
                  No episode orders are available. Enter reviewed labels
                  manually.
                </p>
              ) : (
                <>
                  <label>
                    TMDb order
                    <select
                      aria-label="TMDb episode order"
                      value={groupID}
                      disabled={locked}
                      onChange={(event) => {
                        setGroupID(event.target.value);
                        setGroups(undefined);
                        setSubgroupID("");
                        setProposal(undefined);
                      }}
                    >
                      <option value="">Choose an order…</option>
                      {sources.map((source) => (
                        <option key={source.id} value={source.id}>
                          {source.name} · {source.episode_count} episodes
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    Provider for proposed labels
                    <select
                      aria-label="Proposed label provider"
                      value={sourceProvider}
                      disabled={locked}
                      onChange={(event) => {
                        setSourceProvider(Number(event.target.value));
                        setProposal(undefined);
                      }}
                    >
                      <option value={0}>Manual label</option>
                      {providerIDs.map((providerID) => (
                        <option key={providerID} value={providerID}>
                          {identityCache[providerID]?.title ?? providerName} ·{" "}
                          {providerID}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    Release title for proposed labels
                    <TextField.Root
                      aria-label="Proposed release title"
                      maxLength={180}
                      disabled={locked}
                      value={sourceTitle}
                      onChange={(event) => {
                        setSourceTitle(event.target.value);
                        setProposal(undefined);
                      }}
                    />
                  </label>
                  <label>
                    Label type
                    <select
                      aria-label="Proposed label type"
                      value={prefix}
                      disabled={locked}
                      onChange={(event) => {
                        setPrefix(event.target.value);
                        setProposal(undefined);
                      }}
                    >
                      {["episode", "SP", "OVA", "ONA", "OAD"].map((value) => (
                        <option key={value} value={value}>
                          {value === "episode" ? "Episode number" : value}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    First release number
                    <TextField.Root
                      aria-label="First proposed release number"
                      inputMode="numeric"
                      maxLength={6}
                      disabled={locked}
                      value={start}
                      onChange={(event) => {
                        setStart(event.target.value);
                        setProposal(undefined);
                      }}
                    />
                  </label>
                  <Button
                    variant="soft"
                    disabled={locked || !groupID || !sourceTitle.trim()}
                    onClick={() => void propose("")}
                  >
                    Load order subgroups
                  </Button>
                  {groups && (
                    <>
                      <label>
                        Subgroup
                        <select
                          aria-label="TMDb order subgroup"
                          value={subgroupID}
                          disabled={locked}
                          onChange={(event) => {
                            setSubgroupID(event.target.value);
                            setProposal(undefined);
                          }}
                        >
                          <option value="">Choose a subgroup…</option>
                          {groups.map((group) => (
                            <option key={group.id} value={group.id}>
                              {group.name} · {group.episode_count} episodes
                            </option>
                          ))}
                        </select>
                      </label>
                      <Button
                        variant="soft"
                        disabled={locked || !subgroupID}
                        onClick={() => void propose(subgroupID)}
                      >
                        Review proposed labels
                      </Button>
                    </>
                  )}
                </>
              ))}
            {proposal && (
              <section aria-label="Proposed episode mappings">
                <h4>{proposal.source_name}</h4>
                <p>
                  {proposal.entries.length} mappings proposed;{" "}
                  {proposal.unmatched.length} provider episodes are unmatched or
                  not loaded.
                </p>
                <ul className="episode-list">
                  {proposal.entries.map((entry) => {
                    const episode = initial.episodes.find(
                      (value) => value.episode_id === entry.episode_id,
                    );
                    return (
                      <li key={entry.episode_id}>
                        <span>
                          {episode
                            ? canonicalLabel(episode)
                            : `Episode ${entry.episode_id}`}{" "}
                          · {episode?.title}
                        </span>
                        <span>
                          {entry.release_title} · {entry.label}
                        </span>
                      </li>
                    );
                  })}
                </ul>
                {proposal.unmatched.length > 0 && (
                  <>
                    <p className="field-help">
                      Unmatched episodes will be excluded. Load their canonical
                      seasons first if they should be included.
                    </p>
                    <ul>
                      {proposal.unmatched.slice(0, 50).map((episode) => (
                        <li key={episode.tmdb_id}>
                          {canonicalLabel(episode)} · {episode.title} · TMDb{" "}
                          {episode.tmdb_id}
                        </li>
                      ))}
                    </ul>
                    {proposal.unmatched.length > 50 && (
                      <p>Showing the first 50 unmatched episodes.</p>
                    )}
                    <label>
                      <input
                        type="checkbox"
                        aria-label="I understand unmatched episodes are excluded"
                        checked={excludeConfirmed}
                        disabled={locked}
                        onChange={(event) =>
                          setExcludeConfirmed(event.target.checked)
                        }
                      />{" "}
                      I understand unmatched episodes are excluded
                    </label>
                  </>
                )}
                <p className="field-help">
                  Merging keeps other draft rows and replaces {replacementCount}{" "}
                  existing draft mappings for the same canonical episodes.
                </p>
                <Button
                  disabled={
                    locked ||
                    !proposal.entries.length ||
                    (proposal.unmatched.length > 0 && !excludeConfirmed)
                  }
                  onClick={() => {
                    invalidate();
                    setEntries((current) => ({
                      ...current,
                      ...Object.fromEntries(
                        proposal.entries.map((entry) => [
                          entry.episode_id,
                          entry,
                        ]),
                      ),
                    }));
                    setProposalMessage(
                      `${proposal.entries.length} proposed mappings merged into the draft. Review and save to apply them.`,
                    );
                    setProposal(undefined);
                  }}
                >
                  Merge proposed mappings into draft
                </Button>
              </section>
            )}
            {proposalMessage && <p role="status">{proposalMessage}</p>}
          </section>
        </>
      )}
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      {review && (
        <section aria-label="Review Anime identity and episode changes">
          <h3>Review saved identity and episode changes</h3>
          <p>
            {review.settings.identities.length} linked {providerName} entries ·{" "}
            {review.settings.entries.length} canonical episode mappings
          </p>
          <p className="field-help">
            This complete reviewed set replaces the saved links and mappings.
            Blank/unlisted mappings remain unknown. Review expires{" "}
            {new Date(expiryTime(review.expires_at)).toLocaleString()}.
          </p>
          <ul className="episode-list">
            {review.settings.identities.map((identity) => (
              <li key={identity.id}>
                <span>
                  {identity.title} · {identity.format} · {providerName}{" "}
                  {identity.id}
                </span>
              </li>
            ))}
            {review.settings.entries.map((entry) => {
              const episode = review.settings.episodes.find(
                (value) => value.episode_id === entry.episode_id,
              );
              return (
                <li key={entry.episode_id}>
                  <span>
                    {episode
                      ? canonicalLabel(episode)
                      : `Episode ${entry.episode_id}`}{" "}
                    · {episode?.title}
                  </span>
                  <span>
                    {entry.release_title} · {entry.label} ·{" "}
                    {entry.provider_id
                      ? `${providerName} ${entry.provider_id}`
                      : "Manual"}
                  </span>
                </li>
              );
            })}
          </ul>
          <label>
            <input
              type="checkbox"
              aria-label="I reviewed these Anime identities and episode mappings"
              checked={checked}
              disabled={locked}
              onChange={(event) => setChecked(event.target.checked)}
            />{" "}
            I reviewed these Anime identities and episode mappings
          </label>
        </section>
      )}
      <div className="monitoring-dialog-actions">
        <Button variant="soft" color="gray" disabled={!!busy} onClick={close}>
          Close editor
        </Button>
        {stale ? (
          <Button variant="soft" onClick={reload}>
            Reload saved Anime mapping
          </Button>
        ) : review ? (
          <>
            <Button
              variant="soft"
              color="gray"
              disabled={!!busy}
              onClick={() => {
                setReview(undefined);
                setChecked(false);
              }}
            >
              Cancel review
            </Button>
            <Button disabled={locked || !checked} onClick={() => void apply()}>
              {busy === "save"
                ? "Saving mapping…"
                : "Save reviewed Anime mapping"}
            </Button>
          </>
        ) : (
          <Button disabled={locked || !dirty} onClick={() => void preview()}>
            {busy === "preview"
              ? "Loading review…"
              : "Review Anime mapping changes"}
          </Button>
        )}
      </div>
    </div>
  );
}

function AnimePack({ settings }: { settings: AnimeOrderSettings }) {
  const [selected, setSelected] = useState<number[]>([]),
    [page, setPage] = useState(1),
    [query, setQuery] = useState("");
  const [reviewed, setReviewed] = useState(false),
    [items, setItems] = useState<Release[]>(),
    [warnings, setWarnings] = useState<string[]>([]),
    [releaseID, setReleaseID] = useState(""),
    [confirmed, setConfirmed] = useState(false),
    [busy, setBusy] = useState(""),
    [error, setError] = useState(""),
    [queued, setQueued] = useState(false);
  const mapped = settings.episodes.filter((episode) =>
    settings.entries.some((entry) => entry.episode_id === episode.episode_id),
  );
  const shown = mapped.slice((page - 1) * 50, page * 50);
  const allPage = shown.every((episode) =>
    selected.includes(episode.episode_id),
  );
  const changed = () => {
    setReviewed(false);
    setItems(undefined);
    setReleaseID("");
    setConfirmed(false);
    setQueued(false);
    setError("");
    setWarnings([]);
  };
  const targets = mapped.filter((episode) =>
    selected.includes(episode.episode_id),
  );
  async function search() {
    if (busy || !reviewed || !targets.length) return;
    setBusy("search");
    setItems(undefined);
    setReleaseID("");
    setConfirmed(false);
    setError("");
    try {
      const out = await api<{ items: Release[]; warnings?: string[] }>(
        "/releases/anime-pack",
        {
          method: "POST",
          body: JSON.stringify({
            series_id: settings.media_id,
            episode_ids: targets.map((episode) => episode.episode_id),
            query: query.trim(),
            order_revision: settings.revision,
          }),
        },
      );
      setItems(out.items);
      setWarnings(out.warnings ?? []);
    } catch (e) {
      setError(messageFor(e));
      setReviewed(false);
    } finally {
      setBusy("");
    }
  }
  async function download() {
    if (busy || !confirmed || !releaseID) return;
    setBusy("download");
    setError("");
    try {
      await api("/downloads", {
        method: "POST",
        body: JSON.stringify({ release_id: releaseID }),
      });
      setQueued(true);
      setItems(undefined);
      setReleaseID("");
      setConfirmed(false);
    } catch (e) {
      setError(
        `${messageFor(e)} Check Activity before retrying an interrupted request, then search again for a fresh selection.`,
      );
      setItems(undefined);
      setReleaseID("");
      setConfirmed(false);
      setReviewed(false);
    } finally {
      setBusy("");
    }
  }
  return (
    <section className="settings-form" aria-label="Manual Anime pack search">
      <h3>Manual pack and special searches</h3>
      <p className="field-help">
        Choose 1–100 saved mappings and review the exact targets before
        searching. Automatic mapped searches require separate approval in Anime
        automatic searches. Completed files still need explicit mapping in
        Activity. A shared file may cover only consecutive canonical episodes
        from one season.
      </p>
      <label>
        Pack release query
        <TextField.Root
          aria-label="Anime pack search query"
          value={query}
          maxLength={200}
          disabled={!!busy}
          onChange={(event) => {
            setQuery(event.target.value);
            changed();
          }}
        />
      </label>
      <label>
        <input
          type="checkbox"
          aria-label="Select mapped episodes on this page"
          checked={allPage}
          disabled={
            !!busy ||
            (!allPage &&
              new Set([
                ...selected,
                ...shown.map((episode) => episode.episode_id),
              ]).size > 100)
          }
          onChange={(event) => {
            setSelected((current) =>
              event.target.checked
                ? [
                    ...new Set([
                      ...current,
                      ...shown.map((episode) => episode.episode_id),
                    ]),
                  ]
                : current.filter(
                    (id) => !shown.some((episode) => episode.episode_id === id),
                  ),
            );
            changed();
          }}
        />{" "}
        Select mapped episodes on this page
      </label>
      <ul className="episode-list">
        {shown.map((episode) => {
          const entry = settings.entries.find(
            (value) => value.episode_id === episode.episode_id,
          )!;
          return (
            <li key={episode.episode_id}>
              <label>
                <input
                  type="checkbox"
                  aria-label={`Select pack target ${canonicalLabel(episode)} · ${episode.title}`}
                  checked={selected.includes(episode.episode_id)}
                  disabled={
                    !!busy ||
                    (!selected.includes(episode.episode_id) &&
                      selected.length >= 100)
                  }
                  onChange={(event) => {
                    setSelected((current) =>
                      event.target.checked
                        ? [...current, episode.episode_id]
                        : current.filter((id) => id !== episode.episode_id),
                    );
                    changed();
                  }}
                />{" "}
                {canonicalLabel(episode)} · {episode.title}
              </label>
              <span>
                {entry.release_title} · {entry.label}
              </span>
            </li>
          );
        })}
      </ul>
      <Pagination
        page={page}
        pages={Math.ceil(mapped.length / 50)}
        onPage={setPage}
      />
      <p className="field-help">
        {selected.length} selected across pages · saved order revision{" "}
        {settings.revision}
      </p>
      <Button
        variant="soft"
        disabled={
          !!busy ||
          !selected.length ||
          !query.trim() ||
          new TextEncoder().encode(query.trim()).length > 200
        }
        onClick={() => {
          setReviewed(true);
          setItems(undefined);
          setReleaseID("");
          setConfirmed(false);
        }}
      >
        Review pack targets
      </Button>
      {reviewed && (
        <section aria-label="Review Anime pack targets">
          <h4>Search these exact targets</h4>
          <p>Query: {query.trim()}</p>
          <ul>
            {targets.map((episode) => (
              <li key={episode.episode_id}>
                {canonicalLabel(episode)} · {episode.title}
              </li>
            ))}
          </ul>
          <Button disabled={!!busy} onClick={() => void search()}>
            {busy === "search"
              ? "Searching packs…"
              : "Search reviewed pack targets"}
          </Button>
        </section>
      )}
      {warnings.map((warning, index) => (
        <p key={index} className="field-help">
          {warning}
        </p>
      ))}
      {items && (
        <section aria-label="Anime pack releases">
          <p>
            {items.length} releases returned. Check that the selected release
            contains the reviewed targets.
          </p>
          {!items.length && (
            <p>
              No supported releases found. Check the query and configured
              indexers.
            </p>
          )}
          <ul className="episode-list">
            {items.map((release) => (
              <li key={release.id}>
                <label>
                  <input
                    type="radio"
                    name={`anime-pack-release-${settings.media_id}`}
                    aria-label={`Choose release ${release.title}`}
                    value={release.id}
                    checked={releaseID === release.id}
                    disabled={!!busy}
                    onChange={() => {
                      setReleaseID(release.id);
                      setConfirmed(false);
                    }}
                  />{" "}
                  {release.title}
                  <span className="field-help">
                    {release.indexer ?? "Indexer"} ·{" "}
                    {release.protocol === "usenet"
                      ? "Usenet · SABnzbd"
                      : "Torrent · qBittorrent"}{" "}
                    · {bytes(release.size_bytes)}
                    {release.protocol !== "usenet"
                      ? ` · ${release.seeders ?? "Unknown"} seeders`
                      : ""}
                  </span>
                </label>
              </li>
            ))}
          </ul>
          {releaseID && (
            <>
              <label>
                <input
                  type="checkbox"
                  aria-label="I reviewed the selected pack release and episode targets"
                  checked={confirmed}
                  disabled={!!busy}
                  onChange={(event) => setConfirmed(event.target.checked)}
                />{" "}
                I reviewed the selected release and episode targets; completed
                files still require mapping.
              </label>
              <Button
                disabled={!!busy || !confirmed}
                onClick={() => void download()}
              >
                {busy === "download"
                  ? "Sending pack…"
                  : "Download selected release"}
              </Button>
            </>
          )}
        </section>
      )}
      {queued && (
        <p role="status" className="save-success">
          Download queued.{" "}
          <Link className="text-link" to="/activity">
            Open Activity to track and review episode files.
          </Link>
        </p>
      )}
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
    </section>
  );
}
