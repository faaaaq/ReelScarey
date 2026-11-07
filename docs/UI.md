# Interface and design system

ReelScarey's interface follows the Sonarr/Radarr design language. Owners who
know those tools should find their way around without reading anything. The
Torrents page is the deliberate exception: it is modeled on the qBittorrent
WebUI because it fronts the supervised built-in qBittorrent client.

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
  Activity, Wanted, Torrents, Requests, Settings and System. Like Sonarr, a
  section shows its child links only while it is active. The structure is
  defined once in `web/src/navigation.ts`, along with document titles.
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
  episodes" action.
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
  shown" is capped at 200 IDs and states the cap. Bulk review dialogs are
  unchanged.
- Manual release search links a release title to its tracker page (`info_url`)
  in a new tab when the indexer supplies one, and leaves it as text otherwise.
  Age uses RFC3339, RFC1123Z or Go time layouts and shows an em dash when the
  date is absent or unreadable. Recent failed download titles get a "Failed
  before" label; loading that history never blocks results.
- Activity is the active queue only. Completed downloads move to History; the
  queue has no completed filter and links to `/timeline`.
- Automation keeps schedules, profiles and run history. Per-title quality
  profiles are assigned with Bulk profiles on the library pages.

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
