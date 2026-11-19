# ReelScarey 0.54.34

Cleanup summaries now count unresolved videos. Successfully retried videos
leave the skipped count automatically; original failures remain in job details
with reasons, timestamps and links to their successful retry jobs. Multiple
failures on one video count once in the summary and remain separate historical
attempts. Successful no-op retries also resolve skips.

Resolution requires the same file, path, action, cleanup mode and language
policy, with a later successful outcome. Failed or unrelated retries cannot
clear a skip. Legacy capped diagnostics remain explicit unknowns until backed
by individual audit evidence. Read reconciliation never overwrites a running
worker's journal. Old retry links open even outside the recent-job list.
