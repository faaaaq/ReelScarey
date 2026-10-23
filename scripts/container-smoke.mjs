// Run against the disposable container created by CI, never an existing install.
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { setTimeout as delay } from 'node:timers/promises'
const base = 'http://127.0.0.1:8787'
const container = 'reelscarey-ci'
async function ready() {
  for (let i = 0; i < 100; i++) {
    try { if ((await fetch(`${base}/readyz`, { signal: AbortSignal.timeout(1000) })).ok) return } catch { /* Wait for startup. */ }
    await delay(200)
  }
  throw new Error('Container readiness timed out')
}
await ready()
assert.match(await (await fetch(base)).text(), /ReelScarey/)
assert.equal((await fetch(`${base}/automation`)).status, 200)
assert.equal((await fetch(`${base}/subtitles`)).status, 200)
assert.equal((await fetch(`${base}/api/v1/subtitles/preferences`)).status, 401)
assert.equal((await fetch(`${base}/api/v1/settings`)).status, 401)
const code = execFileSync('docker', ['exec', container, 'reelscarey', 'setup-token'], { encoding: 'utf8' }).trim()
const setup = await fetch(`${base}/api/v1/auth/setup`, { method: 'POST', headers: { 'Content-Type': 'application/json', Origin: base }, body: JSON.stringify({ username: 'ciadmin', password: 'ci-only-test-password', setup_token: code }) })
assert.equal(setup.status, 201)
const cookie = setup.headers.get('set-cookie').split(';')[0]
const session = await setup.json()
const headers = { Cookie: cookie, 'X-CSRF-Token': session.csrf_token, 'Content-Type': 'application/json', Origin: base }
const system = await (await fetch(`${base}/api/v1/system`, { headers })).json()
assert.equal(system.capabilities.subtitles, true)
const subtitles = await (await fetch(`${base}/api/v1/subtitles/preferences`, { headers })).json()
assert.equal(subtitles.preferences.enabled, false)
assert.deepEqual(subtitles.preferences.tracks, [{ language: 'en', variant: 'full' }, { language: 'en', variant: 'signs' }, { language: 'en', variant: 'forced' }])
const subtitlePreferences = { ...subtitles.preferences, enabled: true, match_policy: 'hash', tracks: [...subtitles.preferences.tracks, { language: 'ja', variant: 'signs' }] }
assert.equal((await fetch(`${base}/api/v1/subtitles/preferences`, { method: 'PUT', headers, body: JSON.stringify(subtitlePreferences) })).status, 200)
const saved = await fetch(`${base}/api/v1/settings`, { method: 'PUT', headers, body: JSON.stringify({ library_name: 'CI cinema', movie_folder: 'Movies', series_folder: 'TV', revision: 1 }) })
assert.equal(saved.status, 200)
const schedule = await (await fetch(`${base}/api/v1/automation`, { headers })).json()
assert.equal(schedule.enabled, false)
const profiles = await (await fetch(`${base}/api/v1/quality-profiles`, { headers })).json()
const profile = { ...profiles.items[0], name: 'Container profile', language: 'english' }
assert.equal((await fetch(`${base}/api/v1/quality-profiles/${profile.id}`, { method: 'PUT', headers, body: JSON.stringify(profile) })).status, 200)
assert.equal((await fetch(`${base}/api/v1/automation`, { method: 'PUT', headers, body: JSON.stringify({ enabled: true, mode: 'search', interval_minutes: 60, batch_size: 5, default_profile_id: profile.id, revision: schedule.revision }) })).status, 200)
execFileSync('docker', ['restart', container])
await ready()
const after = await fetch(`${base}/api/v1/settings`, { headers })
assert.equal(after.status, 200)
assert.equal((await after.json()).library_name, 'CI cinema')
const subtitlesAfter = await (await fetch(`${base}/api/v1/subtitles/preferences`, { headers })).json()
assert.equal(subtitlesAfter.preferences.enabled, true)
assert.equal(subtitlesAfter.preferences.match_policy, 'hash')
assert.deepEqual(subtitlesAfter.preferences.tracks, subtitlePreferences.tracks)
assert.equal((await (await fetch(`${base}/api/v1/providers/opensubtitles`, { headers })).json()).configured, false)
const automationAfter = await (await fetch(`${base}/api/v1/automation`, { headers })).json()
assert.equal(automationAfter.enabled, true)
assert.equal(automationAfter.state, 'waiting_indexer')
assert.equal((await (await fetch(`${base}/api/v1/quality-profiles`, { headers })).json()).items[0].name, 'Container profile')
assert.equal((await fetch(`${base}/api/v1/metadata/movies?query=Test`, { headers })).status, 409)
execFileSync('docker', ['exec', container, 'reelscarey', 'healthcheck'])
execFileSync('docker', ['exec', container, 'ffprobe', '-version'])
console.log('PASS: container frontend, authentication, health, FFprobe, session/settings/profile/schedule/subtitle preference persistence across restart and deferred providers.')
