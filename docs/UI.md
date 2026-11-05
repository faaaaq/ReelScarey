# Interface and design system

The 0.29 interface migrates ReelScarey to an Emby-inspired layout with a
shared light/dark theme. Docker remains the only supported deployment.

## Current scope

Migrated: the application shell, Dashboard, Movies/TV/Anime library, movie
details and Settings. The remaining pages keep the legacy layout until they
are migrated in batches. Both themes must stay readable during this
transition.

## Principles

- Content first: posters, titles, files and status lead; avoid decorative
  sections, slogans and promotional art.
- One shared visual language: spacing, radii, borders, typography, focus and
  status colors come from tokens, not page-specific palettes.
- Light and dark are equal themes. Never hard-code text or surface colors in a
  page; use the tokens in `web/src/redesign.css`.
- Unavailable features and waiting states stay explicit.
- Accessibility is part of the design: visible focus, labelled icon-only
  controls, keyboard navigation and reduced-motion support.

## Shell

- Collapsible left sidebar. It contains only Library, Browse and Tools groups,
  plus Settings and the account controls.
- Management pages live under Settings → Administration so the primary
  navigation stays short. Routes remain directly addressable.
- `Ctrl/Cmd+K` opens the command palette for page navigation and saved-title
  search.
- The topbar holds the command trigger, theme toggle and recent activity.
- Mobile uses a drawer and a compact header; desktop collapse does not hide
  accessible names.

## Theme

- `ThemeProvider` stores `reelscarey-theme` in local storage, follows the
  system preference on first visit and sets `data-theme` on the document.
- Accent colors and surfaces are defined as tokens. The current accent is a
  restrained Emby-style green; changing it is a token change, not a page
  rewrite.
- Legacy pages receive compatibility overrides in `redesign.css` while they
  are migrated. Those overrides should shrink as pages move.

## Migration rule

Migrate a page by replacing its one-off layout classes with shared primitives,
then remove the corresponding legacy rules. Do not add a third styling layer.
Keep the 186 frontend tests passing and verify both themes before calling a
page migrated.
