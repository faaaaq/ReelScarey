# YouTube sourcing

Use **YouTube** like a downloader: paste a public video, playlist or channel
URL, fetch its entries, choose a video, choose a destination, quality
(`Best`, `1080p` or `720p`) and optional subtitles, then add it to the queue.
The queue shows each download's state, progress, speed and ETA. Cancel stops a
running yt-dlp process; isolated files are retained for inspection. Verified
files wait for an import review unless they came from an explicit per-show
source configured for automatic import.

Inspection returns at most 100 entries and indicates when results are truncated. Select a
video, then choose a saved movie, a loaded TV episode, or a named custom
collection. ReelScarey never infers a catalog match from a title. Custom
collections and videos have their own identities rather than fabricated TMDb IDs.

Enter up to eight exact subtitle language tags, such as `en, ja`, or leave the
field empty. Choose creator-provided subtitles or automatic captions. Requested
languages that are not available are reported on the job. Available captions are
downloaded as UTF-8 WebVTT sidecars. Review the exact video and target before
queueing, then review the verified media, subtitle paths, sizes and hashes before
import. Automatic queueing or importing from a playlist is not performed.

Downloads use an isolated attempt folder. The worker validates output, probes and
hashes the video, and validates and hashes each subtitle. Approved imports use a
durable journal, recheck the source, publish without replacing existing files,
and register the catalog file or custom item with its YouTube provenance. The
same target is reserved against other import and download workflows. A conflict
leaves the files in place for review and retry; completed staging links are removed
only when they still identify the committed file. Cancel is available before
publication; uncertain partial acquisition files are retained for inspection.
If a copy is interrupted before its staged phase is recorded, an existing
partial stage is left untouched and requires operator review rather than an
automatic retry. Fully recorded stages can be verified and resumed.
When a registered catalog video is reorganized, its YouTube provenance and
subtitle journal paths move atomically with the video and sidecar references.

The container image includes pinned, checksum-verified yt-dlp and FFmpeg.
The downloads mount remains read-only. YouTube staging defaults to
`youtube-staging/work` beside the configured state directory: `/youtube-staging/work`
for the original layout or `/config/youtube-staging/work` for the simple Compose
layout. `REELSCAREY_YOUTUBE_STAGING_DIR` can select a different dedicated writable
location; its parent must exist and it must remain separate from media and state.
Configuration backups do not include staging or downloaded media. Preserve the
staging storage while queued or review-pending jobs need those files. YouTube can
change its protocols, requiring a container update independently of other features.
The feature does not use browser cookies or user-supplied command flags. It does
not automatically infer episodes, download entire channels, automatically replace
library files, or embed subtitle tracks. Playlist and channel browsing is bounded;
select each desired video explicitly. Existing OpenSubtitles settings are separate
from the YouTube subtitle choices.

Existing catalog movies or single-episode files can now receive a replacement.
The initial target review explicitly labels replacement acquisition. Acquisition
and its first import review retain a verified candidate under the versions folder;
they do not change the current video. Then choose **Review acquired replacement**
to inspect the destination, previous version and selected VTT subtitle paths in
Organize library, and separately approve the swap. Shared episode files require
an acquisition covering the complete shared mapping; the individual-video YouTube
target selector supports single episodes only. Custom collections retain their
ordinary import workflow.

Selected subtitles receive separate source-specific names without overwriting
existing sidecars. Both versions, the acquisition and subtitle bytes are retained
across rollback, and the appropriate YouTube video provenance is restored. Old
or newly retained sidecars may have different timing; rollback does not erase
them. Automatic subtitle timing correction is not provided.

## Per-show YouTube sources

A saved show can have an explicit YouTube playlist or channel source on its
detail page. The mapping is never a title search:

- **Episode number in title** reads an explicit `S01E02` or `1x02` token from
  the video title and maps it to the loaded, monitored episode.
- **Playlist order** maps the Nth video to `First episode + N - 1` in the
  configured season. It requires a playlist and an explicit season.

The owner previews the video-to-episode mapping before saving. New sources start
paused. The first check records the current videos as a baseline; **Fill
missing** explicitly clears that baseline and requests the mapped missing
episodes. Future videos are checked on the source interval.

## Playlist to series

Use the **Playlist to series** tab for a one-time batch. Paste a playlist, then
confirm the saved show and season that ReelScarey identifies from the playlist
title. If several shows match, choose one; if none match, search the TV library.
Episode matching uses an explicit `S01E05`, `Ep.5`, `E5` or `#5` token, with an
episode-title fallback. The longest video for each episode is preselected, so a
playlist that also contains clips does not queue several files for one episode.

Select or clear individual videos, then **Download selected**. Missing episodes
are downloaded, verified and imported through the normal organization template,
including the inspected `[1080p SDR]`/`[2160p HDR]` quality tag. Videos that
already have an episode file are disabled until **Replace episodes already in the
library** is enabled; replacement acquisitions keep the old file and require the
separate swap review in Organize library. Unmatched videos and episodes with an
active download are skipped and reported.

The batch is explicit and does not need monitoring. **Save a paused source for
future episodes** also creates the per-show source, which can be enabled on the
show page.


Only episodes that are loaded, monitored and missing are queued. Episodes with
an existing file or another active download are skipped with a recorded reason.
When **Import verified exact matches without review** is enabled, a verified
download from that source is approved automatically; the normal no-overwrite,
journaled import still runs. If automatic approval cannot prove the exact
evidence, the job remains awaiting review. Generated videos keep their YouTube
identity and provenance.

