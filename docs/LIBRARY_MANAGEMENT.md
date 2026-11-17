# Library management controls

| Request | Location and behavior |
| --- | --- |
| Delete multiple movies, shows, or anime | Enable **Select**, choose entries, then **Delete selected** in the selection bar. |
| Delete multiple music entries | Music's Artists, Albums, and Tracks views each support **Select** and **Delete selected**. |
| Select by clicking anywhere | In selection mode, titles, artwork, metadata, and row/card backgrounds toggle selection. Normal detail navigation resumes after leaving selection mode. |
| Delete a whole movie or series | **Delete movie** or **Delete series** in the detail-page toolbar. |
| Delete individual video files | **Delete file** beside each registered file under **Files & History**. Series episode rows also expose their attached file's delete action. Shared episode files remove every reference to that file. |
| Delete music artists, albums, tracks, or files | Artist and album toolbars delete the entire saved entry. Track tables offer **Delete track**; the album's registered-file table offers **Delete file** for individual copies. |
| Delete subtitles in batches | Settings → Subtitles → **Delete subtitles**. Choose all subtitles, subtitles outside configured languages, or non-forced subtitles outside configured languages. Choose all video libraries or one library. Both embedded and external subtitles are included. |
| Clean up audio tracks | Settings → Subtitles → **Delete audio tracks**. Keep the original language, configured languages, and tracks with unknown language tags. Items without a known original language or a safe remaining audio track are skipped and reported. |
| Clean tracks for one movie/show | Open **Files & History** → **Remove media tracks** on that item's detail page. |
| Correct titles after matching/import | Durable workers inspect registered videos and matched music. Video titles follow the matched movie or episode; a multi-episode file uses its show and episode range. Music tags follow the matched track, album, and artist. Older registrations are also queued on upgrade. Already-correct tags need no rewrite. Failed music writes stay queued for retry. |
| Remove redundant Automation links | Movie and show detail toolbars no longer link to the Automation page. Search actions remain available there. |
| Artist albums and singles | Artist pages initially show albums with registered files. **Show all albums** opens the rest of the discography. Single releases are grouped in one expandable **Singles** item. |
| Stopped downloads | **Tracking stopped** entries are excluded from Activity Queue and retained in **History**. |

Deletion first presents a review. By default registered files move to the media
trash; **Keep files on disk** removes the catalog registrations while retaining
the files. Download-client sources remain intact.

Embedded metadata and track changes use a verified container rewrite that keeps
the retained media packets. External subtitles move to the media trash. Cleanup
does not require a subtitle-provider account or a successful provider settings
request. Save Languages & tracks changes before reviewing cleanup.

Metadata checks run in the background and can take time for a large existing
collection. Pending organization or recovery work delays these workers. No
operation tests or changes an owner's media during development validation.
