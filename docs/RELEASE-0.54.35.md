# ReelScarey 0.54.35

This release packages the current primary branch, including the trackfix.2
and 0.54.34 cleanup work, with consistent 0.54.35 application and image
metadata.

Included track-cleanup improvements verify Matroska title and track changes
against fresh stream metadata, scope cleanup acquisition reservations to the
selected titles, and report processed counts and elapsed file progress more
clearly. Cleanup summaries reconcile unresolved videos only against matching
later successes while preserving the original failure evidence and retry
links.

The UX remediation remains included: mobile movie actions, truthful playback
recovery, visible failed music acquisitions, restored dialog focus, clearer
search labels and requester guidance, and the related accessibility fixes.

No additional behavior change is claimed beyond the changes already reviewed
and released through the current primary branch.
