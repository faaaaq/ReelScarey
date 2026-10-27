# YouTube sourcing

Use **YouTube** to inspect a public video, playlist or channel URL. Inspection
returns at most 100 entries and indicates when results are truncated. Select a
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

The container image includes a pinned, checksum-verified yt-dlp release and
FFmpeg, with a dedicated writable YouTube staging volume. The existing downloads
mount remains read-only. For a native installation, install a current yt-dlp executable on the
server's PATH, or set `REELSCAREY_YTDLP_PATH` to its absolute path before starting
ReelScarey. FFprobe must also be available. Restart the app after installing or
changing the executable. If it is unavailable, inspection and queueing report
that requirement; the rest of the application remains usable. YouTube may change
its delivery protocols, so the tool can require updates independent of ReelScarey.
Native staging defaults to `.reelscarey-youtube` inside the configured downloads
directory; set `REELSCAREY_YOUTUBE_STAGING_DIR` to a dedicated writable path to
use a separate location. Its parent must exist and be separate from media and
configuration roots.
Configuration backups do not include the staging volume or downloaded media;
retain the staging volume while queued or review-pending jobs need their files.

The feature does not use browser cookies or user-supplied command flags. It does
not automatically infer episodes, download entire channels, replace existing
library files, or embed subtitle tracks. Playlist and channel browsing is bounded;
select each desired video explicitly. Existing OpenSubtitles settings are separate
from the YouTube subtitle choices.
