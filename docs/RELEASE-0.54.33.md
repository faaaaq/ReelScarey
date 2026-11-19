# ReelScarey 0.54.33

Track cleanup verifies live before/after stream data instead of comparing
against legacy catalog probes that lack indexes and disposition flags.
Matroska track disabling uses bounded observations by default, retains header
recovery, and offers full-checksum verification explicitly. Sampled observations
are stored separately and never presented as whole-file checksums.

MKVToolNix runs with a UTF-8 locale so non-ASCII paths and track names do not
truncate command arguments or identification JSON. Cover attachments do not
count as video tracks during mapping. Untargeted enabled flags, including
tracks already disabled, must stay unchanged. Every newly skipped target keeps
its diagnostic message instead of stopping after the first 20.

Organization separates file operations, jobs/history and naming. Current-file
stages, elapsed time and checksum bytes are visible; hidden and removed tracks
have separate counters. Cleanup acquisition reservations apply to selected
titles, while catalog additions and release searches remain available.

Validation includes backend tests and static checks, 453 frontend tests, and
real generated-media MKVToolNix acceptance under a C locale, including Unicode
paths, legacy metadata, journal persistence and unexpected-change rejection.
