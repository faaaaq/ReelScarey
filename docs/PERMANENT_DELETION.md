# Permanent file deletion

The library delete controls now create explicit `mode:"delete"` reviews. Files
are permanently unlinked after confirmation; **Keep files on disk** uses
`mode:"keep"`. New external-subtitle cleanup plans also permanently delete their
selected sidecars. Unrelated extras, download-client copies and retained versions
are outside the selection.

Review binds each file's identity, size, modification time and bounded content
observation. It does not promote an entire movie to SHA-256 or copy it to trash.
If another media operation holds the filesystem gate, review returns a busy
message immediately. The browser allows up to five minutes for a large review
and distinguishes a review timeout from a confirmed deletion.

After checking the complete catalog snapshot, confirmation commits the catalog
removal and the saved cleanup journal in one transaction. The worker then
unlinks only the reviewed files. Completion and its Activity event commit only
after file cleanup succeeds. On interruption, the saved committed phase resumes
outstanding unlinks; missing files are idempotent, and changed/replaced paths are
refused. Failed cleanup keeps the media reservation until resolved and resumed.

Old, already confirmed retained-trash jobs preserve their original behavior.
New requests for `mode:"trash"` return `409 reload_required`, so an old browser
tab cannot create new trash moves or silently turn a previously displayed trash
choice into permanent deletion. Existing trash is not purged automatically.
Unconfirmed old trash reviews must also be recreated after reloading.

Validation includes permanent video/music deletion at all selection levels,
subtitle deletion without trash, preserved seeding hardlinks and unrelated
files, stale file observations, catalog failure before unlink, interrupted
completion, replaced paths during recovery, and a large sparse-video review.
Browser coverage follows the complete review/confirm workflow against the built
application with disposable files and checks no retained trash exists.

The restricted Windows shell cannot reach its fixture loopback server and the
old staged-removal recovery test fails there on an unchanged 0.54.24 checkout
as well. The browser cases pass through the available isolated test runtime;
Linux release validation remains the supported Docker-target check.
