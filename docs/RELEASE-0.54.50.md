# ReelScarey 0.54.50

- Apply the cinematic theme across the navigation, dashboard, library, detail
  pages and Music Player. The dashboard shows artwork for recent catalog items.
- Apply streaming quality changes to the current track as soon as the setting
  saves. Playback resumes near the same position and paused tracks stay paused.
- Show the loaded codec and encoder target or original file format on desktop
  and mobile. MP3 compatibility streams use the chosen bitrate cap.

Local validation passed frontend format and type checks, all 511 frontend
tests, the production build, the focused dashboard and playback Go tests,
generated FFmpeg playback, Go vet and the application build.

## Publication and live deployment

The [candidate checks](https://github.com/faaaaq/ReelScarey/actions/runs/45510663976)
passed on `d1094814bc664226c2620db7277edaf4391ea83e`, including Linux, Windows and browser tests.
The Windows job passed on retry after a Chocolatey outage.

The [publish run](https://github.com/faaaaq/ReelScarey/actions/runs/45516541902)
verified that Docker Hub tags `0.54.50` and `latest` resolve to the same signed
AMD64 and ARM64 manifest.

```text
sha256:4e763aa64fb6ad8c75e1023aa1a9a0409c1bc13236d5d2c2be31a5f0a047816e
```

The scareypit upgrade is pending. The release session could not open an SSH
connection to the host.
