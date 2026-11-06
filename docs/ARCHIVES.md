# Archive extraction

Completed torrents that contain only an archive set are unpacked before the
normal movie or TV import. A download that already contains a playable video is
imported from that video; archives are not unpacked on top of it.

## Supported sets

- RAR and RAR5, including old-style `.rar`/`.r00` volumes and
  `.part1.rar`/`.part2.rar` volumes. A complete set is required; the first
  volume must be present.
- ZIP and 7z.
- Split `.001` sets handled by 7-Zip.
- One level only. Archives nested inside an extracted archive are left alone.

RAR extraction uses Debian's non-free `unrar`; ZIP/7z uses `p7zip-full`. Both are
installed in the Docker image. If the published image must avoid non-free
software, replace the RAR extractor and adjust the image; the application fails
closed with an honest error when the tool is missing.

## Safety and lifecycle

The archive is extracted into
`.reelscarey-extracted-<job id>` inside that job's dedicated download folder. The
original archive volumes stay in place for seeding.

- Paths are re-validated inside the extraction root. Links, device nodes and
  other special files are rejected.
- The expanded size is limited to 20× the compressed archive size, with a 10 GiB
  floor and 200 GiB ceiling, and at most 5000 files. An archive bomb stops with
  a reviewable error.
- A verified `.reelscarey-extract.json` manifest lets an import retry reuse the
  extraction. An unmarked partial extraction is discarded before the next
  attempt.
- Password-protected and encrypted archives stop with `archive_password`; no
  password is guessed, logged or sent anywhere. Extract that archive manually.
- Missing volumes, truncated archives and integrity failures stop with
  `archive_incomplete` or `archive_invalid`.
- Extracted copies are deleted after a successful movie import or after every
  mapped TV file is imported. They are retained on failure, while TV mapping is
  pending, and while an acquired replacement awaits its swap review.
- TV archives still use the existing episode-mapping review; archive extraction
  does not infer episodes.

Archive extraction currently applies to completed torrent downloads. SABnzbd and
NZBGet are expected to unpack their own completed jobs; files they leave packed
are not unpacked by this path.
