# Interface and design system

ReelScarey's interface follows the Sonarr/Radarr design language. Owners who
know those tools should find their way around without reading anything. The
Torrents page is the deliberate exception: it is modeled on the qBittorrent
WebUI because it fronts the supervised built-in qBittorrent client. The Usenet
page stays in the shared language: it fronts the supervised bundled SABnzbd
instance with a clean queue and a settings section, not a clone of SABnzbd's
WebUI.

Docker remains the only supported deployment.

## Principles

- Content first. Posters, titles, files, states and actions lead. No slogans,
  promotional art, eyebrow labels or decorative sections.
- Functional copy. Labels say what a control does ("Search Monitored",
  "Save Changes"); help text explains consequences in one or two sentences.
  Long safety explanations belong in docs, linked when needed.
- One visual language. Colors, spacing, radii and status colors come from the
  tokens in `web/src/styles/app.css`. Page CSS may add layout, never a palette.
- Light and dark are equal themes; always use tokens.
- Unavailable features, waiting states and uncertain results stay explicit.
  Never present a fixture or roadmap item as working.
- Accessibility is part of the design: visible focus, labelled icon-only
  controls, keyboard operation and reduced-motion support.

## Shell

- **Header** (fixed, 60px): logo, global search, recent activity, theme toggle
  and the account menu (sign out). Global search (`Ctrl/Cmd+K` or `/`) finds
  saved titles, jumps to pages and offers "Search TMDb" for new titles.
- **Sidebar** (210px): Dashboard, Movies, TV Shows, Anime, YouTube, Calendar,
  Activity, Wanted, Torrents, Usenet, Requests, Settings and System. Like
  Sonarr, a section shows its child links only while it is active. The structure
  is defined once in `web/src/navigation.ts`, along with document titles.
- Mobile (≤768px): the sidebar becomes a drawer opened from the header.

## Page anatomy

Every page renders, in order:

1. `PageToolbar` (optional, sticky): icon-over-label `ToolbarButton`s. Left
   section for actions (Refresh, Add, Search, Save Changes), right section for
   view options (View, Sort, Filter). Use `ToolbarSeparator` between groups.
2. `PageBody`: 20px padded content column.

```tsx
<>
  <PageToolbar>
    <ToolbarSection>
      <ToolbarButton icon={RefreshCw} label="Refresh" onClick={reload}
        spinning={loading} />
    </ToolbarSection>
    <ToolbarSection align="right">
      <ToolbarButton icon={LayoutGrid} label="Posters" active={view === "posters"} … />
    </ToolbarSection>
  </PageToolbar>
  <PageBody>
    <PageHeading title="Movies" />   {/* optional, compact */}
    …
  </PageBody>
</>
```

## Primitives (`web/src/ui`)

| Primitive | Use |
| --- | --- |
| `PageToolbar`, `ToolbarSection`, `ToolbarButton`, `ToolbarSeparator` | Page actions |
| `PageBody` | Content area |
| `FieldSet` | Titled section with an underlined legend (settings groups, detail sections) |
| `FormGroup` | Label-left/control-right row with help and error text |
| `Check` | Checkbox with label and description |
| `FormActions` | Row of form buttons |
| `Panel` | Bordered surface for grouped content |
| `Card`, `CardGrid`, `AddCard` | Tiles for indexers, clients, profiles, schedules |
| `Label` | Small rectangular status tag: `default`, `primary`, `info`, `success`, `warning`, `danger`, `purple`, `disabled`, optional `outline` |
| `Alert` | Inline message (`info`, `success`, `warning`, `danger`) |
| `ProgressBar` | Download/coverage progress, sizes `small`/`medium`/`large` |
| `Table`, `SortHeader` | Dense data tables with optional sorting |
| `DescriptionList`, `DescriptionItem` | Key/value facts (System status, details) |
| `EmptyState`, `LoadingIndicator` | Empty and loading states |
| `Modal`, `ModalClose` | Header/body/footer dialog on Radix Dialog |
| `formatBytes`, `formatSpeed`, `formatDuration`, `formatTimestamp`, `formatRelative`, `episodeCode` | Shared formatting |

`PageHeading` and `RequestState` in `web/src/components.tsx` remain for
compatibility and use the new styles.

Radix Themes supplies buttons, text fields, switches, menus, tabs and dialogs.
Buttons: `solid` for the one primary action in a view, `soft` for secondary,
`soft color="red"` for destructive, `ghost` for tertiary.

## Status colors

Sonarr's conventions:

- `success` (green): downloaded / available / passing check
- `primary`/`info` (blue): downloading, queued, informational
- `danger` (red): missing and monitored, failed, error
- `warning` (orange): needs attention, review required, partial
- `disabled` (grey): unmonitored, paused, not configured
- `purple`: queued/importing

Poster cards and table rows carry a thin status bar or label using those colors.

## Settings

Settings pages follow the Sonarr layout. The toolbar has **Save Changes**
(disabled until the form is dirty). Content is a stack of `FieldSet`s
containing `FormGroup` rows. Collections of configured things (indexers,
download clients, schedules) are `CardGrid`s of `Card`s plus an `AddCard`;
clicking a card opens a `Modal` editor. The `/settings` index lists every
settings area with a one-line description.

## Page CSS

Page-specific layout lives next to the page (for example
`web/src/pages/Torrents.css`, imported by the page). Prefix its classes with a
short page namespace (`tor-`, `lib-`, `det-`) and use only tokens for color.
Do not add global element selectors in page files.

## Library and Calendar actions (0.38.6)

- Calendar entries open a dialog with the saved synopsis and three actions:
  View show/movie, Manual search (release review) and Automatic search (the
  scheduled pipeline for that exact target, with the outcome reported inline).
- The Calendar toolbar can search every missing monitored movie or TV episode in
  one confirmed action; both report how many targets were searched and how many
  downloads were queued.
- Series details offer Search (one title), Search All Monitored (every monitored
  missing episode) and per-episode automatic search, manual search, Replace and
  Remove from library (the file stays on disk). Seasons list newest first.
- The TV Shows and Anime libraries end with a confirmed "Monitor all future
  episodes" action. It monitors only regular episodes releasing today or later;
  past and undated regular episodes are turned off, and specials keep their
  current choices.
- Adding a title defaults monitoring to on only when it has a release today or
  later; already-released or undated titles start unmonitored. Search-result
  adds use the same default, and a newly monitored show applies it to its
  seasons and episodes.
- Anime search settings include an **English dub only** toggle. The Anime
  library page also has reviewed Enable/Disable for all anime actions, an
  editable dub rule and a test box for a sample release title.
- The Calendar remembers its month, filters and view; Reset clears them.

## Library and search (0.38.7)

- Movies, TV Shows and Anime request the whole collection (`page=all`) and
  render one grouped list. A sticky A–Z index on the right jumps to each letter
  that is present; titles are grouped by first letter after ignoring one leading
  "The ", with non-letters under `#`. If the server returns fewer items than the
  total, or rejects `page=all` past its cap, the page falls back to the
  paginated view and says so.
- Filters, sort and view live in the URL. Changing a filter resets only the
  legacy page parameter. Selection stays visible with checkboxes; "Select all
  shown" selects every title currently rendered (up to the server's 5,000-item
  all-view limit) and states the real count. If the server falls back to the
  paginated view, the control covers that page only and the notice says so;
  changing pages or filters clears the selection. Bulk review dialogs still
  review every selected ID before writing.
- Manual release search links a release title to its tracker page (`info_url`)
  in a new tab when the indexer supplies one, and leaves it as text otherwise.
  Age uses RFC3339, RFC1123Z or Go time layouts and shows an em dash when the
  date is absent or unreadable. Recent failed download titles get a "Failed
  before" label; loading that history never blocks results.
- Activity is the active queue only. Completed downloads move to History; the
  queue has no completed filter and links to `/timeline`.
- Automation keeps schedules, profiles and run history. Per-title quality
  profiles are assigned with Bulk profiles on the library pages.

## Calendar, batches and Dashboard (0.39.0)

- Calendar movie rows can carry a `release_kind` (`theatrical`, `digital` or
  `physical`) with separate `theatrical_date`, `digital_date` and
  `physical_date` values. Month chips and the agenda label the kind with a
  small tag; the entry dialog lists every saved date and shows an em dash when
  a value is missing. TV entries and their labels are unchanged.
- The calendar-only **Releases** toolbar menu filters All, Theatrical, Digital
  and Physical through the `release` query parameter. The choice is part of the
  remembered calendar filters and Reset clears it.
- Calendar and Wanted refresh in the background while the page is open
  (25 seconds and 30 seconds). A successful refresh shows "Updated just now";
  a transient failure keeps the last data and offers "Retry now". Refreshing
  never closes an entry dialog or moves the scroll position.
- Bulk searches confirm the saved Automation mode. While a run is active an
  indeterminate progress bar reports the live checked/queued counters from
  `/automation/runs`; search-only runs say that results are saved for review,
  and a run that checked nothing explains the likely waiting state from its
  saved message.
- Library monitoring and profile reviews show an indeterminate bar with
  "Reviewing…" and "Applying…" status while the single request is pending.
  These busy indicators never steal focus or block the rest of the page.
- The Dashboard is a compact working overview: a status strip (active
  downloads, missing, next seven days, system health), an Activity panel with
  live queue progress and recent History when idle, Wanted and Upcoming panels,
  and quiet Library counts with recent title events. Every value comes from an
  existing endpoint; empty states say what is actually missing.

## Usenet page

`/usenet` fronts the supervised bundled SABnzbd instance and uses the shared
tokens and primitives, not the Torrents console layout. It has:

- a status strip with the honest lifecycle state, installed/configured flags,
  queue state, rate, remaining size, ETA, current absolute speed limit and the
  count of active ReelScarey jobs;
- a dense queue table with per-row labels, progress, size, remaining, speed and
  ETA, page-scoped multi-select and bulk pause/resume/delete;
- a reviewed delete dialog that requires the review checkbox and offers the
  optional `del_files` choice;
- an Add NZB dialog accepting uploaded files only (never a URL);
- a news-server form (host, port, TLS, username, password, connections) and an
  explicit turn-on/turn-off control for the daemon. Turning it on never changes
  the Usenet default; the page links to Download clients for that choice.

When the binary is missing, no news server is saved, the client is off or the
daemon is not answering, the queue area is an `EmptyState` that says exactly
what to do instead of showing an empty working queue. The API key and NNTP
password are never rendered.

## Work in progress notices

Subtitles and YouTube show a first-visit "Work in progress" dialog. Continue
closes it for that visit; "Don't show again" stores the per-page choice in
localStorage through `web/src/ui/notices.ts`. The dialog is keyboard dismissible
and never focuses a destructive action.

## Settings and organizing

The Indexers add form starts with optional, UI-only presets: NZBGeek (Newznab)
and Prowlarr Torrent/Usenet (Torznab/Newznab). They prefill name, type and an
editable URL; the Prowlarr template requires replacing `{indexerId}` with the
numeric Prowlarr indexer ID. Presets never submit on their own and are not part
of the saved indexer payload.

The sidebar and Settings index do not list a separate Organize Files area.
Settings → Media Management links to the existing route as "Reorganize
registered files"; the route itself is unchanged.
