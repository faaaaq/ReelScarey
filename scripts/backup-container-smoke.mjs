// Run only against the disposable network-isolated backup acceptance container.
// /fixture is its own configuration volume, mounted read-only in the Node helper.
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'

assert.equal(process.env.REELSCAREY_BACKUP_SMOKE, '1', 'Explicit disposable-test opt-in is required')
const base = 'http://127.0.0.1:18787'
let cookie = '', csrf = ''
async function request(path, options = {}) {
  return fetch(base + '/api/v1' + path, { ...options, headers: {
    ...(cookie ? { Cookie: cookie } : {}), ...(csrf ? { 'X-CSRF-Token': csrf } : {}),
    ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...options.headers,
  }, signal: AbortSignal.timeout(60000) })
}
async function json(path, options = {}, status = 200) {
  const response = await request(path, options)
  assert.equal(response.status, status, `${options.method ?? 'GET'} ${path}`)
  return response.json()
}
async function signIn(path, body, status = 200) {
  const response = await request(path, { method: 'POST', body: JSON.stringify(body) })
  assert.equal(response.status, status)
  cookie = response.headers.get('set-cookie').split(';')[0]
  csrf = (await response.json()).csrf_token
}
const credentials = { username: 'backup-fixture-admin', password: 'disposable-backup-password' }
const first = await json('/auth/session')
assert.equal(first.setup_required, true)
const setupToken = (await readFile('/fixture/setup-token', 'utf8')).trim()
await signIn('/auth/setup', { ...credentials, setup_token: setupToken }, 201)
let settings = await json('/settings')
settings = await json('/settings', { method: 'PUT', body: JSON.stringify({ library_name: 'Backup fixture cinema', movie_folder: settings.movie_folder, series_folder: settings.series_folder, episode_naming: settings.episode_naming, revision: settings.revision }) })
let state = await json('/system/backups')
assert.equal(state.schedule.enabled, false)
state = await json('/system/backups/schedule', { method: 'PUT', body: JSON.stringify({ enabled: true, interval_days: 2, keep_count: 2, keep_days: 14, revision: state.schedule.revision }) })
assert.ok(state.schedule.next_run > Date.now() / 1000)
await json('/system/backups', { method: 'POST', body: '{}' }, 202)
for (let attempt = 0; attempt < 100; attempt++) {
  state = await json('/system/backups')
  if (!state.schedule.running && state.archives.length) break
  await delay(100)
}
assert.equal(state.schedule.last_error, '')
assert.equal(state.schedule.running, false)
const archive = state.archives.find(a => a.kind === 'manual')
assert.ok(archive)
const download = await request(`/system/backups/${archive.id}/download`)
assert.equal(download.status, 200)
assert.equal(download.headers.get('content-type'), 'application/zip')
assert.equal(download.headers.get('cache-control'), 'no-store')
const zip = Buffer.from(await download.arrayBuffer())
assert.equal(zip.subarray(0, 2).toString(), 'PK')
const uploaded = await json('/system/backups/upload', { method: 'POST', headers: { 'Content-Type': 'application/zip' }, body: zip }, 201)
const review = await json(`/system/backups/${uploaded.id}/preview`, { method: 'POST', body: '{}' })
assert.equal(review.movies, 1)
assert.equal(review.administrator, credentials.username)
await json(`/system/backups/${uploaded.id}/restore`, { method: 'POST', body: JSON.stringify({ sha256: 'stale', confirmation: 'RESTORE' }) }, 409)
await json('/settings', { method: 'PUT', body: JSON.stringify({ library_name: 'Current fixture cinema', movie_folder: settings.movie_folder, series_folder: settings.series_folder, episode_naming: settings.episode_naming, revision: settings.revision }) })
await json(`/system/backups/${uploaded.id}/restore`, { method: 'POST', body: JSON.stringify({ sha256: review.archive.sha256, confirmation: 'RESTORE' }) }, 202)
let revoked = false
for (let attempt = 0; attempt < 120; attempt++) {
  try { const session = await json('/auth/session'); if (!session.authenticated) { revoked = true; break } } catch { /* Listener is deliberately closed during the restart. */ }
  await delay(500)
}
assert.ok(revoked, 'Restore must restart and revoke the original session')
cookie = ''; csrf = ''
await signIn('/auth/login', credentials)
settings = await json('/settings')
assert.equal(settings.library_name, 'Backup fixture cinema')
state = await json('/system/backups')
assert.equal(state.recovery_pending, true)
assert.equal(state.schedule.enabled, false)
assert.ok(state.restore_message.includes('completed'))
const safety = state.archives.find(a => a.kind === 'pre_restore')
assert.ok(safety, 'Current state must be protected by a separate safety backup')
const safetyReview = await json(`/system/backups/${safety.id}/preview`, { method: 'POST', body: '{}' })
assert.equal(safetyReview.movies, 1)
const catalog = await json('/library?kind=movie')
assert.equal(catalog.total, 1)
assert.equal(catalog.items[0].title, 'Disposable backup fixture')
const html = await fetch(base + '/backups')
assert.equal(html.status, 200)
const markup = await html.text()
for (const asset of [...markup.matchAll(/(?:src|href)="(\/assets\/[^\"]+)"/g)]) assert.equal((await fetch(base + asset[1])).status, 200)
const checks = await json('/system/checks', { method: 'POST', body: '{}' })
for (const check of checks.report.checks.filter(c => ['database', 'secrets', 'paths'].includes(c.id))) assert.equal(check.state, 'ready', check.id)
await json('/system/recovery/resume', { method: 'POST', body: '{"reviewed":true}' })
assert.equal((await json('/system/backups')).recovery_pending, false)
await json(`/system/backups/${uploaded.id}`, { method: 'DELETE' })
assert.equal((await json('/system/backups')).archives.length, 2)
console.log(JSON.stringify({ backup: 'verified', upload: 'verified', restore: 'restarted', sessions: 'revoked', catalog: 'preserved', safety_backup: 'retained', schedules: 'paused', recovery_review: 'passed', assets: 'served' }))
