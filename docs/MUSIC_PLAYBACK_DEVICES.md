# Music playback device acceptance checklist

Manual, executable checklist for the playback surfaces automated tests cannot
honestly cover (real Safari/iOS decoding, background/lock-screen behavior,
Bluetooth routing). Run it against a staging instance with at least one FLAC
source track and one lossy (MP3/AAC) track in the library. Record PASS/FAIL
per row and the app build (image digest) under test. A browser `canPlayType`
unit test or a headless Chromium run is NOT a substitute for this checklist.

## iOS Safari (iPhone/iPad)

| # | Step | Expected |
| --- | --- | --- |
| 1 | Open the music library, tap a track | Playback starts from the user gesture; duration appears; no player error banner |
| 2 | Inspect the stream request (developer tools on a paired Mac, or a proxy) | Default quality: `Content-Type: audio/ogg` (Opus) and it plays; if iOS cannot decode it, the app must have requested `format=mp3` and received `audio/mpeg` |
| 3 | Seek forward and backward with the scrubber | Position resumes, no stall, no error banner |
| 4 | Pause, lock the screen, unlock, resume | Playback resumes; no duplicated track |
| 5 | Background the app mid-play, reopen | Playback state honest (playing or paused, never silently stopped) |
| 6 | Rapidly skip 5+ tracks | Player never shows "paused while audible" or freezes; in-flight loads are abandoned cleanly |
| 7 | Change streaming quality (e.g. Data Saver → Original) | Change applies at the next track start; choice survives an app restart |
| 8 | Play over Bluetooth headphones/car kit | Audio routes to the device; lock-screen controls work |
| 9 | Old iOS (14/15) if available | MP3 fallback actually decodes and plays |
| 10 | Kill the server (or point at an unreachable host) mid-play | Visible error message; player recovers on the next track after the server returns |

## macOS Safari

Steps 1–7 and 10 as above (no lock-screen/Bluetooth steps), plus:

- Verify in the Web Inspector network tab that an Opus-capable desktop Safari
  negotiates `audio/ogg` and an Ogg-incapable one falls back to `audio/mpeg`.
- Confirm no uncaught console errors during normal playback and after a
  forced stream failure.

## Android Chrome (if a device is available)

Steps 1–3, 6, 7, 10 as above, plus:

- Media notification controls (play/pause/next) work and stay consistent with
  the in-page player state.
- Background audio continues with the media notification; returning to the
  app does not restart the track.

## Result recording

- PASS/FAIL per row, device + OS version, app image digest, date, tester.
- Any FAIL on the MP3 fallback (rows 2/9), quality persistence (row 7) or the
  error-recovery path (row 10) is release-blocking for the browser-compat
  gate; the remaining rows are quality findings.
