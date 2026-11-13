import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { Button } from "@radix-ui/themes";
import {
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  RefreshCw,
  X,
} from "lucide-react";
import { Link } from "react-router-dom";
import { APIError, api, messageFor, type Release } from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";
import {
  Alert,
  FormActions,
  FormGroup,
  Label,
  Modal,
  Table,
  formatBytes,
  type Kind,
} from "../ui";
import "./AnimeOrder.css";

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
function hasContent(entry: AnimeOrderEntry | undefined) {
  return (
    !!entry &&
    !!(entry.label.trim() || entry.release_title.trim() || entry.provider_id)
  );
}
function sameEntry(a: AnimeOrderEntry, b: AnimeOrderEntry) {
  return (
    a.episode_id === b.episode_id &&
    a.provider_id === b.provider_id &&
    a.release_title === b.release_title &&
    a.label === b.label
  );
}
function altTitles(identity?: AnimeIdentity) {
  return [identity?.romaji, identity?.english, identity?.native]
    .filter(Boolean)
    .join(" · ");
}

/** Legend heading with a decorative step number that stays out of the name. */
function Step({
  number,
  title,
  actions,
  description,
  children,
}: {
  number: number;
  title: string;
  actions?: ReactNode;
  description?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="rs-fieldset anime-step">
      <div className="rs-legend">
        <h3>
          <span className="anime-step-number" aria-hidden="true">
            {number}
          </span>
          {title}
        </h3>
        {actions && <div className="rs-legend-actions">{actions}</div>}
      </div>
      {description && <p className="rs-fieldset-description">{description}</p>}
      {children}
    </section>
  );
}

function Pager({
  page,
  pages,
  onPage,
  label,
}: {
  page: number;
  pages: number;
  onPage: (page: number) => void;
  label: string;
}) {
  if (pages <= 1) return null;
  return (
    <nav className="rs-pagination" aria-label={label}>
      <Button
        size="1"
        variant="soft"
        color="gray"
        disabled={page <= 1}
        onClick={() => onPage(page - 1)}
      >
        <ChevronLeft size={14} aria-hidden="true" /> Previous
      </Button>
      <span>
        Page {page} of {pages}
      </span>
      <Button
        size="1"
        variant="soft"
        color="gray"
        disabled={page >= pages}
        onClick={() => onPage(page + 1)}
      >
        Next <ChevronRight size={14} aria-hidden="true" />
      </Button>
    </nav>
  );
}

const statusKinds: Record<string, Kind> = {
  Added: "success",
  Changed: "warning",
  Edited: "warning",
  Cleared: "warning",
  Removed: "danger",
  Saved: "success",
  Kept: "default",
  Unchanged: "default",
};
function Status({ value }: { value: string }) {
  return (
    <Label kind={statusKinds[value] ?? "default"} outline={value === "Kept"}>
      {value}
    </Label>
  );
}

export function AnimeOrder({ id }: { id: number }) {
  const [open, setOpen] = useState(false),
    [success, setSuccess] = useState(false);
  const path = `/library/items/${id}/anime-order`;
  const resource = useResource<AnimeOrderSettings>(open ? path : null);
  return (
    <section
      className="rs-fieldset anime-section"
      aria-label="Anime identity and episode mapping"
    >
      <div className="rs-legend">
        <h2>Anime identity & episode mapping</h2>
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
              Edit Anime identity & mapping
            </Button>
          </div>
        )}
      </div>
      <p className="rs-fieldset-description">
        Link AniList or Kitsu entries and map release labels to saved episodes,
        including specials. TMDb identity, monitoring and files stay unchanged.
      </p>
      {open && (
        <>
          <RequestState
            loading={resource.loading}
            error={resource.error}
        code={resource.code}
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
            <FormActions>
              <Button
                variant="soft"
                color="gray"
                onClick={() => setOpen(false)}
              >
                Close
              </Button>
            </FormActions>
          )}
        </>
      )}
      {success && (
        <Alert kind="success">
          Anime identity and episode mapping saved. No search or file operation
          was started.
        </Alert>
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
  const ids = useId();
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
    .filter((entry): entry is AnimeOrderEntry => hasContent(entry));
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
  const savedEntries = new Map(
    initial.entries.map((entry) => [entry.episode_id, entry]),
  );
  const providerOptions = providerIDs.map((providerID) => (
    <option key={providerID} value={providerID}>
      {identityCache[providerID]?.title ?? providerName} · {providerID}
    </option>
  ));
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
  async function refresh() {
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
  const episodic = initial.numbering_revision > 0;
  return (
    <div className="anime-form">
      <Step
        number={1}
        title={`Linked ${providerName} entries`}
        actions={
          <Button
            size="1"
            variant="soft"
            color="gray"
            disabled={locked || dirty || !initial.identities.length}
            onClick={() => void refresh()}
          >
            <RefreshCw
              size={14}
              aria-hidden="true"
              className={busy === "refresh" ? "rs-spin" : undefined}
            />
            {busy === "refresh"
              ? `Refreshing ${providerName}…`
              : `Review refreshed ${providerName} details`}
          </Button>
        }
      >
        <FormGroup
          label="Anime provider"
          htmlFor={`${ids}-provider`}
          help="Each title uses one identity provider. To switch, set linked rows to Manual label and unlink the old entries first."
        >
          <select
            id={`${ids}-provider`}
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
        </FormGroup>
        {!episodic && (
          <Alert>
            This Anime film supports identity links only. Episode-order mapping
            and pack searches apply to episodic shows.
          </Alert>
        )}
        {providerIDs.length ? (
          <Table label={`Linked ${providerName} entries`}>
            <thead>
              <tr>
                <th scope="col">Title</th>
                <th scope="col">Format</th>
                <th scope="col" className="is-numeric">
                  Year
                </th>
                <th scope="col" className="is-numeric">
                  Episodes
                </th>
                <th scope="col">ID</th>
                <th scope="col">Status</th>
                <th scope="col" className="is-actions">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {providerIDs.map((providerID) => {
                const identity = identityCache[providerID];
                const inUse = draftEntries.some(
                  (entry) => entry.provider_id === providerID,
                );
                return (
                  <tr key={providerID}>
                    <td>
                      <span className="rs-strong">
                        {identity?.title ?? `${providerName} ${providerID}`}
                      </span>
                      {altTitles(identity) && (
                        <span className="anime-sub">{altTitles(identity)}</span>
                      )}
                    </td>
                    <td>{identity?.format || "—"}</td>
                    <td className="is-numeric">
                      {identity?.year ?? "Unknown"}
                    </td>
                    <td className="is-numeric">{identity?.episodes ?? "—"}</td>
                    <td className="is-shrink">
                      {providerName} {providerID}
                    </td>
                    <td className="is-shrink">
                      {newProviderIDs.includes(providerID) ? (
                        <Label kind="warning">Pending review</Label>
                      ) : (
                        <Label kind="success">Saved</Label>
                      )}
                      {inUse && (
                        <>
                          {" "}
                          <Label outline>Used by mappings</Label>
                        </>
                      )}
                    </td>
                    <td className="is-actions">
                      <span className="anime-actions">
                        <a
                          className="anime-external"
                          href={`https://${provider === "kitsu" ? "kitsu.io" : "anilist.co"}/anime/${providerID}`}
                          target="_blank"
                          rel="noreferrer"
                        >
                          View on {providerName}
                          <ExternalLink size={12} aria-hidden="true" />
                        </a>
                        <Button
                          size="1"
                          color="red"
                          variant="soft"
                          aria-label={`Unlink ${providerID}`}
                          title={
                            inUse
                              ? "Clear or set Manual label on rows using this entry first"
                              : undefined
                          }
                          disabled={locked || inUse}
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
                          Unlink
                        </Button>
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        ) : (
          <p className="rs-muted">No {providerName} entries linked.</p>
        )}
        <p className="rs-form-help">
          Keep up to twenty linked entries. Add at most eight new links per
          review; saved identity details are reused without another lookup.
          Before unlinking an entry used by mappings, clear those rows or choose
          Manual label.
        </p>
        <details className="anime-details">
          <summary>What a provider link means</summary>
          <p>
            A provider entry can describe a season, film or special rather than
            an entire TMDb show. Check the format, year and titles: an episode
            count is not an episode mapping. Linking does not replace the TMDb
            identity or add search aliases.
          </p>
        </details>
        <FormGroup label={`Find on ${providerName}`} htmlFor={`${ids}-query`}>
          <form
            className="anime-inline-form"
            onSubmit={(event) => {
              event.preventDefault();
              void search();
            }}
          >
            <input
              id={`${ids}-query`}
              type="text"
              aria-label={`Search ${providerName} titles`}
              maxLength={200}
              value={query}
              disabled={locked}
              onChange={(event) => setQuery(event.target.value)}
            />
            <Button
              type="submit"
              variant="soft"
              disabled={locked || !query.trim()}
            >
              {busy === "search"
                ? `Searching ${providerName}…`
                : `Search ${providerName}`}
            </Button>
          </form>
        </FormGroup>
        {results && (
          <section aria-label={`${providerName} search results`}>
            {!results.length ? (
              <p className="rs-muted">No {providerName} entries found.</p>
            ) : (
              <Table label={`${providerName} search results`}>
                <thead>
                  <tr>
                    <th scope="col">Title</th>
                    <th scope="col">Format</th>
                    <th scope="col" className="is-numeric">
                      Year
                    </th>
                    <th scope="col" className="is-numeric">
                      Episodes
                    </th>
                    <th scope="col">ID</th>
                    <th scope="col" className="is-actions">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {results.map((identity) => {
                    const linked = providerIDs.includes(identity.id);
                    return (
                      <tr key={identity.id}>
                        <td>
                          <span className="rs-strong">{identity.title}</span>
                          {altTitles(identity) && (
                            <span className="anime-sub">
                              {altTitles(identity)}
                            </span>
                          )}
                        </td>
                        <td>{identity.format || "—"}</td>
                        <td className="is-numeric">
                          {identity.year ?? "Unknown"}
                        </td>
                        <td className="is-numeric">
                          {identity.episodes ?? "—"}
                        </td>
                        <td className="is-shrink">
                          {providerName} {identity.id}
                        </td>
                        <td className="is-actions">
                          <Button
                            size="1"
                            variant="soft"
                            aria-label={
                              linked
                                ? "Linked in draft"
                                : `Link ${providerName} ${identity.id}`
                            }
                            disabled={
                              locked ||
                              linked ||
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
                              setProviderIDs((current) => [
                                ...current,
                                identity.id,
                              ]);
                            }}
                          >
                            {linked ? "Linked" : "Link"}
                          </Button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </Table>
            )}
          </section>
        )}
      </Step>
      {episodic && (
        <>
          <Step
            number={2}
            title="Release labels for canonical episodes"
            description="Leave a row blank when its label is unknown. Labels look like 25, S00E01, SP1, OVA1, ONA1 or OAD1; SP1 and 1 are different labels."
            actions={
              <select
                aria-label="Mapping season"
                className="anime-compact-select"
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
            }
          >
            {initial.episodes.length === 0 ? (
              <Alert kind="warning">
                No canonical episodes are loaded. Load the show's seasons from
                its episode list first; proposals cannot create missing
                episodes.
              </Alert>
            ) : (
              <Table
                label="Canonical episode mappings"
                className="anime-mapping-table"
              >
                <thead>
                  <tr>
                    <th scope="col">Episode</th>
                    <th scope="col">Provider</th>
                    <th scope="col">Release title</th>
                    <th scope="col">Label</th>
                    <th scope="col">Status</th>
                    <th scope="col" className="is-actions">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {shown.slice((page - 1) * 50, page * 50).map((episode) => {
                    const entry = entries[episode.episode_id];
                    const savedEntry = savedEntries.get(episode.episode_id);
                    const label = `${canonicalLabel(episode)} · ${episode.title}`;
                    const filled = hasContent(entry);
                    const status = filled
                      ? savedEntry && sameEntry(entry!, savedEntry)
                        ? "Saved"
                        : "Edited"
                      : savedEntry
                        ? "Cleared"
                        : "";
                    return (
                      <tr key={episode.episode_id}>
                        <td>
                          <span className="rs-strong">
                            {canonicalLabel(episode)}
                          </span>{" "}
                          {episode.title}
                          {episode.absolute_number !== null && (
                            <span className="anime-sub">
                              Existing absolute alias: {episode.absolute_number}
                            </span>
                          )}
                        </td>
                        <td>
                          <select
                            aria-label={`Provider for ${label}`}
                            className="anime-cell-select"
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
                            {providerOptions}
                          </select>
                        </td>
                        <td>
                          <input
                            type="text"
                            className="anime-cell-title"
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
                        </td>
                        <td>
                          <input
                            type="text"
                            className="anime-cell-label"
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
                        </td>
                        <td className="is-shrink">
                          {status ? (
                            <Status value={status} />
                          ) : (
                            <span className="rs-muted">Unknown</span>
                          )}
                        </td>
                        <td className="is-actions">
                          <button
                            type="button"
                            className="anime-icon-button"
                            aria-label={`Clear mapping for ${canonicalLabel(episode)}`}
                            title="Clear mapping"
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
                            <X size={15} aria-hidden="true" />
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </Table>
            )}
            <Pager
              label="Episode mapping pages"
              page={page}
              pages={Math.ceil(shown.length / 50)}
              onPage={setPage}
            />
          </Step>
          <Step
            number={3}
            title="Propose labels from a TMDb episode order"
            description="Optional. Choose one subgroup of a TMDb episode order; its exact episode IDs propose labels. Nothing is created, guessed or saved until you merge and review."
            actions={
              <Button
                size="1"
                variant="soft"
                disabled={locked}
                onClick={() => void loadSources()}
              >
                {busy === "sources"
                  ? "Loading orders…"
                  : "Load TMDb episode orders"}
              </Button>
            }
          >
            {sources &&
              (!sources.length ? (
                <p className="rs-muted">
                  No episode orders are available. Enter reviewed labels
                  manually.
                </p>
              ) : (
                <>
                  <FormGroup label="TMDb order" htmlFor={`${ids}-group`}>
                    <select
                      id={`${ids}-group`}
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
                  </FormGroup>
                  <FormGroup
                    label="Provider entry"
                    htmlFor={`${ids}-source-provider`}
                  >
                    <select
                      id={`${ids}-source-provider`}
                      aria-label="Proposed label provider"
                      value={sourceProvider}
                      disabled={locked}
                      onChange={(event) => {
                        setSourceProvider(Number(event.target.value));
                        setProposal(undefined);
                      }}
                    >
                      <option value={0}>Manual label</option>
                      {providerOptions}
                    </select>
                  </FormGroup>
                  <FormGroup
                    label="Release title"
                    htmlFor={`${ids}-source-title`}
                    help="The exact release name the proposed labels belong to."
                  >
                    <input
                      id={`${ids}-source-title`}
                      type="text"
                      aria-label="Proposed release title"
                      maxLength={180}
                      disabled={locked}
                      value={sourceTitle}
                      onChange={(event) => {
                        setSourceTitle(event.target.value);
                        setProposal(undefined);
                      }}
                    />
                  </FormGroup>
                  <FormGroup label="Label type" htmlFor={`${ids}-prefix`}>
                    <select
                      id={`${ids}-prefix`}
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
                  </FormGroup>
                  <FormGroup
                    label="First number"
                    htmlFor={`${ids}-start`}
                    help="1–100000."
                  >
                    <input
                      id={`${ids}-start`}
                      type="text"
                      className="anime-number"
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
                  </FormGroup>
                  <div className="anime-indent">
                    <Button
                      variant="soft"
                      disabled={locked || !groupID || !sourceTitle.trim()}
                      onClick={() => void propose("")}
                    >
                      Load order subgroups
                    </Button>
                  </div>
                  {groups && (
                    <>
                      <FormGroup label="Subgroup" htmlFor={`${ids}-subgroup`}>
                        <select
                          id={`${ids}-subgroup`}
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
                      </FormGroup>
                      <div className="anime-indent">
                        <Button
                          disabled={locked || !subgroupID}
                          onClick={() => void propose(subgroupID)}
                        >
                          Review proposed labels
                        </Button>
                      </div>
                    </>
                  )}
                </>
              ))}
            {proposalMessage && <Alert kind="success">{proposalMessage}</Alert>}
          </Step>
        </>
      )}
      <Step number={episodic ? 4 : 2} title="Review and save">
        <p className="rs-form-help">
          The review replaces the saved links and mappings with the complete
          reviewed set. Saving never starts a search or changes files.
        </p>
        <div className="rs-row">
          {dirty ? (
            <Label kind="warning">Unsaved draft changes</Label>
          ) : (
            <Label kind="disabled">No draft changes</Label>
          )}
          <span className="rs-muted rs-small">
            {providerIDs.length} linked · {draftEntries.length} mapped
            {episodic ? ` of ${initial.episodes.length} loaded episodes` : ""}
          </span>
        </div>
        {error && <Alert kind="danger">{error}</Alert>}
        <FormActions>
          <Button variant="soft" color="gray" disabled={!!busy} onClick={close}>
            Close editor
          </Button>
          {stale ? (
            <Button variant="soft" onClick={reload}>
              Reload saved Anime mapping
            </Button>
          ) : (
            <Button disabled={locked || !dirty} onClick={() => void preview()}>
              {busy === "preview"
                ? "Loading review…"
                : "Review Anime mapping changes"}
            </Button>
          )}
        </FormActions>
      </Step>
      {proposal && (
        <ProposalModal
          proposal={proposal}
          episodes={initial.episodes}
          draft={entries}
          locked={locked}
          excludeConfirmed={excludeConfirmed}
          setExcludeConfirmed={setExcludeConfirmed}
          replacementCount={replacementCount}
          cancel={() => setProposal(undefined)}
          merge={() => {
            invalidate();
            setEntries((current) => ({
              ...current,
              ...Object.fromEntries(
                proposal.entries.map((entry) => [entry.episode_id, entry]),
              ),
            }));
            setProposalMessage(
              `${proposal.entries.length} proposed mappings merged into the draft. Review and save to apply them.`,
            );
            setProposal(undefined);
          }}
        />
      )}
      {review && !stale && (
        <ReviewModal
          review={review}
          initial={initial}
          providerName={providerName}
          checked={checked}
          setChecked={setChecked}
          busy={busy}
          locked={locked}
          cancel={() => {
            if (busy) return;
            setReview(undefined);
            setChecked(false);
          }}
          apply={() => void apply()}
        />
      )}
    </div>
  );
}

function ProposalModal({
  proposal,
  episodes,
  draft,
  locked,
  excludeConfirmed,
  setExcludeConfirmed,
  replacementCount,
  cancel,
  merge,
}: {
  proposal: Proposal;
  episodes: AnimeOrderSettings["episodes"];
  draft: Record<number, AnimeOrderEntry>;
  locked: boolean;
  excludeConfirmed: boolean;
  setExcludeConfirmed: (value: boolean) => void;
  replacementCount: number;
  cancel: () => void;
  merge: () => void;
}) {
  return (
    <Modal
      open
      onOpenChange={(open) => !open && cancel()}
      title={`Proposed labels · ${proposal.source_name}`}
      size="large"
    >
      <section
        className="anime-modal-section"
        aria-label="Proposed episode mappings"
      >
        <p>
          {proposal.entries.length} mappings proposed;{" "}
          {proposal.unmatched.length} provider episodes are unmatched or not
          loaded.
        </p>
        {proposal.entries.length > 0 && (
          <Table label="Proposed mappings">
            <thead>
              <tr>
                <th scope="col">Episode</th>
                <th scope="col">Release title</th>
                <th scope="col">Label</th>
                <th scope="col">Draft</th>
              </tr>
            </thead>
            <tbody>
              {proposal.entries.map((entry) => {
                const episode = episodes.find(
                  (value) => value.episode_id === entry.episode_id,
                );
                const current = draft[entry.episode_id];
                return (
                  <tr key={entry.episode_id}>
                    <td>
                      {episode
                        ? canonicalLabel(episode)
                        : `Episode ${entry.episode_id}`}{" "}
                      · {episode?.title}
                    </td>
                    <td>{entry.release_title}</td>
                    <td className="is-shrink">{entry.label}</td>
                    <td className="is-shrink">
                      {!hasContent(current) ? (
                        <Status value="Added" />
                      ) : sameEntry(current, entry) ? (
                        <Status value="Unchanged" />
                      ) : (
                        <Label kind="warning">
                          Replaces {current.release_title} · {current.label}
                        </Label>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        )}
        {proposal.unmatched.length > 0 && (
          <>
            <Alert kind="warning" title="Unmatched episodes are excluded">
              Load their canonical seasons first if they should be included.
            </Alert>
            <Table label="Unmatched provider episodes">
              <thead>
                <tr>
                  <th scope="col">Episode</th>
                  <th scope="col">Title</th>
                  <th scope="col">TMDb ID</th>
                </tr>
              </thead>
              <tbody>
                {proposal.unmatched.slice(0, 50).map((episode) => (
                  <tr key={episode.tmdb_id}>
                    <td className="is-shrink">{canonicalLabel(episode)}</td>
                    <td>{episode.title}</td>
                    <td className="is-shrink">TMDb {episode.tmdb_id}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
            {proposal.unmatched.length > 50 && (
              <p className="rs-muted rs-small">
                Showing the first 50 unmatched episodes.
              </p>
            )}
            <label className="rs-check">
              <input
                type="checkbox"
                aria-label="I understand unmatched episodes are excluded"
                checked={excludeConfirmed}
                disabled={locked}
                onChange={(event) => setExcludeConfirmed(event.target.checked)}
              />
              <span>I understand unmatched episodes are excluded</span>
            </label>
          </>
        )}
        <p className="rs-form-help">
          Merging keeps other draft rows and replaces {replacementCount}{" "}
          existing draft mappings for the same canonical episodes.
        </p>
        <div className="anime-modal-actions">
          <Button variant="soft" color="gray" onClick={cancel}>
            Cancel
          </Button>
          <Button
            disabled={
              locked ||
              !proposal.entries.length ||
              (proposal.unmatched.length > 0 && !excludeConfirmed)
            }
            onClick={merge}
          >
            Merge proposed mappings into draft
          </Button>
        </div>
      </section>
    </Modal>
  );
}

function ReviewModal({
  review,
  initial,
  providerName,
  checked,
  setChecked,
  busy,
  locked,
  cancel,
  apply,
}: {
  review: Review;
  initial: AnimeOrderSettings;
  providerName: string;
  checked: boolean;
  setChecked: (value: boolean) => void;
  busy: string;
  locked: boolean;
  cancel: () => void;
  apply: () => void;
}) {
  const savedIdentities = new Map(
    initial.identities.map((identity) => [identity.id, identity]),
  );
  const reviewedIdentityIDs = new Set(
    review.settings.identities.map((identity) => identity.id),
  );
  const removedIdentities = initial.identities.filter(
    (identity) => !reviewedIdentityIDs.has(identity.id),
  );
  const savedEntries = new Map(
    initial.entries.map((entry) => [entry.episode_id, entry]),
  );
  const reviewedEpisodeIDs = new Set(
    review.settings.entries.map((entry) => entry.episode_id),
  );
  const removedEntries = initial.entries.filter(
    (entry) => !reviewedEpisodeIDs.has(entry.episode_id),
  );
  const episodeName = (episodeID: number) => {
    const episode =
      review.settings.episodes.find(
        (value) => value.episode_id === episodeID,
      ) ?? initial.episodes.find((value) => value.episode_id === episodeID);
    return `${episode ? canonicalLabel(episode) : `Episode ${episodeID}`} · ${episode?.title ?? ""}`;
  };
  const providerLabel = (providerID: number) =>
    providerID ? `${providerName} ${providerID}` : "Manual";
  return (
    <Modal
      open
      onOpenChange={(open) => !open && cancel()}
      title="Review mapping changes"
      size="large"
      footer={
        <>
          <Button
            variant="soft"
            color="gray"
            disabled={!!busy}
            onClick={cancel}
          >
            Cancel review
          </Button>
          <Button disabled={locked || !checked} onClick={apply}>
            {busy === "save"
              ? "Saving mapping…"
              : "Save reviewed Anime mapping"}
          </Button>
        </>
      }
    >
      <section
        className="anime-modal-section"
        aria-label="Review Anime identity and episode changes"
      >
        <p>
          {review.settings.identities.length} linked {providerName} entries ·{" "}
          {review.settings.entries.length} canonical episode mappings
        </p>
        <p className="rs-form-help">
          This complete set replaces the saved links and mappings; unlisted
          episodes stay unknown. Review expires{" "}
          {new Date(expiryTime(review.expires_at)).toLocaleString()}.
        </p>
        <Table label="Reviewed identities">
          <thead>
            <tr>
              <th scope="col">Change</th>
              <th scope="col">Entry</th>
              <th scope="col">Format</th>
              <th scope="col">ID</th>
            </tr>
          </thead>
          <tbody>
            {review.settings.identities.map((identity) => (
              <tr key={identity.id}>
                <td className="is-shrink">
                  <Status
                    value={savedIdentities.has(identity.id) ? "Kept" : "Added"}
                  />
                </td>
                <td>{identity.title}</td>
                <td>{identity.format}</td>
                <td className="is-shrink">
                  {providerName} {identity.id}
                </td>
              </tr>
            ))}
            {removedIdentities.map((identity) => (
              <tr key={`removed-${identity.id}`}>
                <td className="is-shrink">
                  <Status value="Removed" />
                </td>
                <td>{identity.title}</td>
                <td>{identity.format}</td>
                <td className="is-shrink">
                  {providerName} {identity.id}
                </td>
              </tr>
            ))}
            {!review.settings.identities.length &&
              !removedIdentities.length && (
                <tr>
                  <td colSpan={4} className="rs-muted">
                    No linked entries.
                  </td>
                </tr>
              )}
          </tbody>
        </Table>
        {(review.settings.entries.length > 0 || removedEntries.length > 0) && (
          <Table label="Reviewed episode mappings">
            <thead>
              <tr>
                <th scope="col">Change</th>
                <th scope="col">Episode</th>
                <th scope="col">Release title</th>
                <th scope="col">Label</th>
                <th scope="col">Provider</th>
              </tr>
            </thead>
            <tbody>
              {review.settings.entries.map((entry) => {
                const saved = savedEntries.get(entry.episode_id);
                return (
                  <tr key={entry.episode_id}>
                    <td className="is-shrink">
                      <Status
                        value={
                          !saved
                            ? "Added"
                            : sameEntry(saved, entry)
                              ? "Unchanged"
                              : "Changed"
                        }
                      />
                    </td>
                    <td>{episodeName(entry.episode_id)}</td>
                    <td>{entry.release_title}</td>
                    <td className="is-shrink">{entry.label}</td>
                    <td className="is-shrink">
                      {providerLabel(entry.provider_id)}
                    </td>
                  </tr>
                );
              })}
              {removedEntries.map((entry) => (
                <tr key={`removed-${entry.episode_id}`}>
                  <td className="is-shrink">
                    <Status value="Removed" />
                  </td>
                  <td>{episodeName(entry.episode_id)}</td>
                  <td className="anime-removed">{entry.release_title}</td>
                  <td className="is-shrink anime-removed">{entry.label}</td>
                  <td className="is-shrink">
                    {providerLabel(entry.provider_id)}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
        <label className="rs-check">
          <input
            type="checkbox"
            aria-label="I reviewed these Anime identities and episode mappings"
            checked={checked}
            disabled={locked}
            onChange={(event) => setChecked(event.target.checked)}
          />
          <span>I reviewed these Anime identities and episode mappings</span>
        </label>
      </section>
    </Modal>
  );
}

function AnimePack({ settings }: { settings: AnimeOrderSettings }) {
  const ids = useId();
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
  const queryTooLong = new TextEncoder().encode(query.trim()).length > 200;
  return (
    <section
      className="rs-fieldset anime-section"
      aria-label="Manual Anime pack search"
    >
      <div className="rs-legend">
        <h3>Manual pack and special searches</h3>
      </div>
      <p className="rs-fieldset-description">
        Select 1–100 saved mappings, review the exact targets, then search.
        Automatic mapped searches need separate approval, and completed files
        still need explicit mapping in Activity.
      </p>
      <FormGroup
        label="Release query"
        htmlFor={`${ids}-query`}
        help="Explicit search text sent to indexers."
        error={queryTooLong ? "Use at most 200 UTF-8 bytes." : undefined}
      >
        <input
          id={`${ids}-query`}
          type="text"
          aria-label="Anime pack search query"
          value={query}
          maxLength={200}
          disabled={!!busy}
          onChange={(event) => {
            setQuery(event.target.value);
            changed();
          }}
        />
      </FormGroup>
      <Table label="Mapped pack targets">
        <thead>
          <tr>
            <th scope="col" className="is-shrink">
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
                          (id) =>
                            !shown.some((episode) => episode.episode_id === id),
                        ),
                  );
                  changed();
                }}
              />
            </th>
            <th scope="col">Episode</th>
            <th scope="col">Release title</th>
            <th scope="col">Label</th>
          </tr>
        </thead>
        <tbody>
          {shown.map((episode) => {
            const entry = settings.entries.find(
              (value) => value.episode_id === episode.episode_id,
            )!;
            const isSelected = selected.includes(episode.episode_id);
            return (
              <tr
                key={episode.episode_id}
                className={isSelected ? "is-selected" : undefined}
              >
                <td className="is-shrink">
                  <input
                    type="checkbox"
                    aria-label={`Select pack target ${canonicalLabel(episode)} · ${episode.title}`}
                    checked={isSelected}
                    disabled={!!busy || (!isSelected && selected.length >= 100)}
                    onChange={(event) => {
                      setSelected((current) =>
                        event.target.checked
                          ? [...current, episode.episode_id]
                          : current.filter((id) => id !== episode.episode_id),
                      );
                      changed();
                    }}
                  />
                </td>
                <td>
                  <span className="rs-strong">{canonicalLabel(episode)}</span>{" "}
                  {episode.title}
                </td>
                <td>{entry.release_title}</td>
                <td className="is-shrink">{entry.label}</td>
              </tr>
            );
          })}
        </tbody>
      </Table>
      <Pager
        label="Pack target pages"
        page={page}
        pages={Math.ceil(mapped.length / 50)}
        onPage={setPage}
      />
      <div className="anime-toolbar-row">
        <span className="rs-muted rs-small">
          {selected.length} selected across pages · saved order revision{" "}
          {settings.revision}
        </span>
        <Button
          variant="soft"
          disabled={!!busy || !selected.length || !query.trim() || queryTooLong}
          onClick={() => {
            setReviewed(true);
            setItems(undefined);
            setReleaseID("");
            setConfirmed(false);
          }}
        >
          Review pack targets
        </Button>
      </div>
      {reviewed && (
        <section
          className="anime-review"
          aria-label="Review Anime pack targets"
        >
          <h4>Search these exact targets</h4>
          <p>
            Query: <span className="rs-strong">{query.trim()}</span>
          </p>
          <ul className="anime-target-list">
            {targets.map((episode) => (
              <li key={episode.episode_id}>
                {canonicalLabel(episode)} · {episode.title}
              </li>
            ))}
          </ul>
          <p className="rs-form-help">
            A shared file may cover only consecutive canonical episodes from one
            season. Searching does not reserve episodes.
          </p>
          <div>
            <Button disabled={!!busy} onClick={() => void search()}>
              {busy === "search"
                ? "Searching packs…"
                : "Search reviewed pack targets"}
            </Button>
          </div>
        </section>
      )}
      {warnings.length > 0 && (
        <Alert kind="warning">
          {warnings.map((warning, index) => (
            <p key={index}>{warning}</p>
          ))}
        </Alert>
      )}
      {items && (
        <section className="rs-stack" aria-label="Anime pack releases">
          <p className="rs-muted">
            {items.length} releases returned. Check that the chosen release
            contains the reviewed targets.
          </p>
          {!items.length ? (
            <p>
              No supported releases found. Check the query and configured
              indexers.
            </p>
          ) : (
            <Table label="Anime pack releases">
              <thead>
                <tr>
                  <th scope="col" className="is-shrink">
                    <span className="sr-only">Choose</span>
                  </th>
                  <th scope="col">Release</th>
                  <th scope="col">Indexer</th>
                  <th scope="col">Protocol</th>
                  <th scope="col" className="is-numeric">
                    Size
                  </th>
                  <th scope="col" className="is-numeric">
                    Seeders
                  </th>
                </tr>
              </thead>
              <tbody>
                {items.map((release) => (
                  <tr
                    key={release.id}
                    className={
                      releaseID === release.id ? "is-selected" : undefined
                    }
                  >
                    <td className="is-shrink">
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
                      />
                    </td>
                    <td className="anime-release-title">{release.title}</td>
                    <td>{release.indexer ?? "Indexer"}</td>
                    <td className="is-shrink">
                      {release.protocol === "usenet"
                        ? "Usenet · SABnzbd"
                        : "Torrent · qBittorrent"}
                    </td>
                    <td className="is-numeric">
                      {formatBytes(release.size_bytes)}
                    </td>
                    <td className="is-numeric">
                      {release.protocol !== "usenet"
                        ? (release.seeders ?? "Unknown")
                        : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
          {releaseID && (
            <div className="anime-review">
              <label className="rs-check">
                <input
                  type="checkbox"
                  aria-label="I reviewed the selected pack release and episode targets"
                  checked={confirmed}
                  disabled={!!busy}
                  onChange={(event) => setConfirmed(event.target.checked)}
                />
                <span>
                  I reviewed the selected release and episode targets; completed
                  files still require mapping.
                </span>
              </label>
              <div>
                <Button
                  disabled={!!busy || !confirmed}
                  onClick={() => void download()}
                >
                  {busy === "download"
                    ? "Sending pack…"
                    : "Download selected release"}
                </Button>
              </div>
            </div>
          )}
        </section>
      )}
      {queued && (
        <Alert kind="success">
          Download queued.{" "}
          <Link to="/activity">
            Open Activity to track and review episode files.
          </Link>
        </Alert>
      )}
      {error && <Alert kind="danger">{error}</Alert>}
    </section>
  );
}
