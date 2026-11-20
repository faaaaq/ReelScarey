# ReelScarey 0.54.37

TIDAL connection status now distinguishes provider installation, credential-file
presence, device authorization and a real remote session check. A token file is
never treated as proof of login. Settings shows the verification result, last
check time, device-flow state, effective requested quality and provider detail.
Completed device authorization triggers a fresh verification before Connected is
shown; failed refreshes are not presented as a current verified connection.

Music settings exposes a default download source and TIDAL requested quality.
Quality can inherit the server environment or request LOW, HIGH, LOSSLESS or
HI_RES_LOSSLESS. The setting applies to subsequent provider processes without a
restart and remains revision-fenced with the other music preferences. Requested
quality is not represented as an upsampling guarantee.

Album download dialogs now offer Automatic, TIDAL only and YouTube only. The
choice is sent as `audio_source`, persisted on the acquisition job and honored by
search, download, restart recovery and retries. Choosing Automatic explicitly
also overrides a saved provider-only default; leaving the selector untouched
inherits that default. Automatic retains TIDAL-first
fallback behavior when installed. An explicit provider does not silently reroute
to another source. A duplicate active job with a different source returns an
honest conflict instead of reusing the wrong request. Schema 113 adds these
settings additively; existing jobs retain Automatic behavior.

Acquisition failures preserve bounded provider details and identify the actual
source instead of replacing every error with YouTube/yt-dlp-specific text.
Session failures link the owner toward verification/reconnection or another
source. Credential-like assignment values are redacted from provider error text.
TIDAL-only submission is disabled in the popup until a current verified session
is available, with a direct link to Music settings.

Provider library chatter is isolated from the JSON protocol during token/session
checks, including the transition while device authorization is still waiting.
Verification therefore returns a structured result rather than device text mixed
with JSON. The offline protocol harness rejects any additional stdout line.

Offline and real-server coverage includes source routing without fallback calls,
persisted source retries after a service reload, saved defaults, unavailable
providers, duplicate-source conflicts, additive migration, authenticated API
contracts, stale-token UI behavior, settings persistence and mobile album source
controls. A real TIDAL account, verified account session and successful TIDAL
media download remain unverified until the owner completes device authorization.
