# Reviewed quality-profile assignment (0.15 development)

Movie/show library cards and tables show each title's effective quality profile,
with **default** for an inherited assignment or **assigned** for a pinned profile.
The profile filter accepts an effective profile or **Follows schedule default**.
Selecting a profile includes titles inheriting that same profile; the default-only
filter includes both unassigned titles and saved NULL assignments. Names and the
schedule default come from the same database snapshot as catalog rows/counts.

Use the existing page selection, then Assign quality profile. The review reads
the exact selected IDs and shows current assignments, target rules and the
changed/unchanged count. Selection remains limited to the visible page, persists
between poster/table views and clears when filters/pages/sorting change. A target
change reloads the review; obsolete replies cannot replace newer rules.

**Follow schedule default** stores inheritance, so future default changes apply.
Choosing a named profile pins it, including when it happens to be today's default.
Pinning/unpinning is therefore a real change even if the effective profile ID is
currently the same. Entirely unchanged assignments finish without a write.

Apply checks all reviewed item revisions, current effective profile IDs/rules,
target ID/rules and the schedule revision before changing anything. A stale or
missing item/target rejects the entire batch. The schedule fence is conservative:
other schedule edits also require another review. Profile edits, assignments
changed away/back, and a changed default cannot silently reuse an old review.
The UI requires a fresh review after an uncertain response or conflict.

Only item_automation profile_id and revision change. Changed rows invalidate older
single-item assignment/bulk-monitoring editors and pending automatic queue
decisions. No-op rows do not create assignments or bump revisions. Movie/show,
season, episode and specials monitoring, schedules, profile definitions, existing
downloads and files stay unchanged. Independently enabled searches use the new
preferences for future decisions; this operation starts no search or download.
Existing files are protected and upgrade replacement remains outside scope.
Release language labels are selection evidence, not audio-language verification.

## API

- GET `/api/v1/library`: adds profile_context containing default_profile and all
  available profile summaries. Each item has quality_profile with nullable
  profile_id plus effective_profile (id/name/revision). Optional profile filter is
  all (default), default, or a canonical positive existing profile ID. Invalid or
  repeated filters return 400. Other item reads retain their existing contracts.
- POST `/api/v1/library/profiles/preview`: `{kind,ids,target}` reads 1–100 distinct
  positive IDs of kind movie/series. Target is `{use_default:true}` or
  `{profile_id:ID}`; these choices are mutually exclusive. Returns target,
  target_profile with full rules, schedule_revision, default_profile, profile
  summaries and current items (id/title/year/profile_id/revision/effective_profile).
- PUT `/api/v1/library/profiles`: `{kind,target,target_id,target_revision,
  schedule_revision,items}`. Expected items contain id, revision, effective_id and
  effective_revision from preview. Returns total/changed/unchanged, profile summary
  and use_default. Stale/missing targets return 409 without partial changes.

All endpoints require the administrator session; POST/PUT require CSRF and a valid
origin. Strict fields, the existing 16 KiB request-body limit and five-second
request budgets apply. Preview writes no preferences. There is no preview-token
permission scheme: API clients must supply current expected state. Quality owns
profile decoding; transaction-aware reads let automation/library compose coherent
snapshots. No migration, dependency or background job is introduced; schema is 10.

The graphite/blue layout and plain copy are retained. Bulk episode editing,
replacement imports, media removal, moving files and IPTorrents/XSpeeds account
testing remain deferred or outside this milestone.
