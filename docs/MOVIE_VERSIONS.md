# Movie versions (0.28)

One movie can own several files, including HD/4K copies and alternate editions.
Existing-library matching attaches every clearly identified movie file after its
own inspection. A second file no longer makes both copies duplicate exceptions.
Their original filenames and paths remain unchanged. TV episode overlap still
requires review. On an older completed scan, choose **Match clear files
automatically** to retry the files previously held as duplicate movie versions.

The movie's file list shows every copy, its inspected resolution, filename, path,
size and technical details. Subtitles, replacement reviews and rollback history
belong to a specific file. Resolution labels come from inspected video dimensions;
HDR/SDR labels come from the video stream's transfer characteristics. Edition
labels in filenames are not independently verified edition claims.

## Download choices

**Download another version** opens release search with **Keep existing files and
add another version** selected. This explicit manual choice allows another edition
at any resolution and imports it under a unique filename. It preserves all existing
copies. Choose a different release; submitting the same torrent/NZB again returns
its existing job instead of downloading identical content twice.

Enable **Keep HD and 4K copies** on the movie page to maintain two separate download
targets. Select a quality profile for each: HD uses resolutions up to 1080p, and 4K
requires 2160p. Each target respects its profile and the movie's monitoring setting.
Wanted/Calendar show the two targets separately. Existing files satisfy their own
resolution target, regardless of how they were acquired. Unknown video resolution
blocks automatic acquisition pending inspection rather than assuming it is missing.

This option starts disabled. Saving it does not enable monitoring, change the
automation schedule or start a download. Your existing search-only/download mode
still applies. Turning the option off preserves all files and queued downloads.
The two targets can download at the same time. Their destinations include HD/4K
suffixes, and import checks the actual video dimensions before publication. A
mislabeled release needs attention; it cannot satisfy the other target silently.

Existing copies can produce quality upgrade suggestions. Automatic search never
replaces them. In release search, choose **Acquire replacement for [path]** to
target one file, then use the existing reviewed replacement/rollback workflow.
Other copies and their sidecars remain intact. YouTube replacement currently
refuses movies with several files because that workflow has no file selector;
use the explicit file replacement workflow instead. An active YouTube acquisition
continues to reserve the entire movie until it finishes or is cancelled.

Reviewed organization gives multiple copies distinct, stable filenames with their
resolution and file ID. A changed version set invalidates a pending naming review.
Download sources remain intact for seeding; publication never overwrites a file.
Embedded-title updates pass the staging file through Unix descriptors, so they
are available in the Linux container and not in native Windows builds.

## Persistence and verification

Migration 044 retains registered IDs, observations, download provenance, episode
links and subtitle records (including edit row IDs and recovery state), removes
the one-file-per-movie restriction and adds an indexed inspected resolution.
Version settings and download targets are included in normal configuration/database
backups. Keep the pre-upgrade backup when upgrading; old binaries cannot use the
new schema. Backup restore retains the existing worker recovery barriers.

Queue reservations recheck the policy revision, quality profile, monitoring and
schedule inside the transaction. Additional acquisitions and replacements remain
explicit choices; merely opening a movie or viewing its settings starts no work.
No additional service, dependency or container is required.
