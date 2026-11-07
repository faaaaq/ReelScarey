// Run only against the disposable network-isolated backup acceptance container.
// /fixture is its own configuration volume, mounted read-only in the Node helper.
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'
import { createServer } from 'node:http'

assert.equal(process.env.REELSCAREY_BACKUP_SMOKE, '1', 'Explicit disposable-test opt-in is required')
const base = 'http://127.0.0.1:18787'
const hasVersionFiles = process.env.REELSCAREY_BACKUP_VERSIONS === '1'
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
const versions = await json('/library/movies/1/versions', { method: 'PUT', body: JSON.stringify({ enabled: true, hd_profile_id: 1, uhd_profile_id: 2, revision: 0 }) })
assert.equal(versions.hd_files, hasVersionFiles ? 1 : 0)
assert.equal(versions.uhd_files, hasVersionFiles ? 1 : 0)
// Own loopback protocol fixture, reachable only inside the disposable test's
// shared network namespace. Real-client/media acceptance runs separately.
const clientServer = createServer((req, res) => {
  if (req.url === '/api/v2/auth/login') {
    res.setHeader('Set-Cookie', 'SID=backup-fixture; Path=/')
    res.end('Ok.')
  } else if (req.url === '/api/v2/app/webapiVersion') res.end('2.8.3')
  else { res.statusCode = 404; res.end() }
})
await new Promise(resolve => clientServer.listen(0, '127.0.0.1', resolve))
clientServer.unref()
const clientBody = { name: 'Retained backup client', kind: 'qbittorrent', url: `http://127.0.0.1:${clientServer.address().port}`, username: 'fixture', password: 'private-disposable-client-password', download_path: '/completed', local_folder: 'owned-client' }
const client = await json('/download-clients', { method: 'POST', body: JSON.stringify(clientBody) }, 201)
assert.equal(client.enabled, false)
assert.equal(JSON.stringify(client).includes(clientBody.password), false)
const { password: _secret, ...clientUpdate } = clientBody
const enabledClient = await json(`/download-clients/${client.id}`, { method: 'PUT', body: JSON.stringify({ ...clientUpdate, enabled: true, revision: client.revision }) })
// The built-in client is provisioned by the app itself, so the saved
// preference revision is read instead of assuming a fresh value.
const currentDefaults = await json('/download-clients/defaults')
await json('/download-clients/defaults', { method: 'PUT', body: JSON.stringify({ torrent: client.id, usenet: '', revision: currentDefaults.revision }) })
const subtitlePreferences = (await json('/subtitles/preferences')).preferences
await json('/subtitles/preferences', { method: 'PUT', body: JSON.stringify({ ...subtitlePreferences, download_format: 'original' }) })
let settings = await json('/settings')
settings = await json('/settings', { method: 'PUT', body: JSON.stringify({ library_name: 'Backup fixture cinema', movie_folder: settings.movie_folder, series_folder: settings.series_folder, episode_naming: settings.episode_naming, revision: settings.revision }) })
const metadata = await json('/system/metadata-refresh')
await json('/system/metadata-refresh', { method: 'PUT', body: JSON.stringify({ enabled: true, interval_hours: 24, batch_size: 5, only_monitored: true, revision: metadata.revision }) })
const collection = await json('/youtube/collections', { method: 'POST', body: '{"title":"Backup subscription fixture"}' }, 201)
const subscriptionInput = { source_url: 'https://www.youtube.com/playlist?list=PLfixture012345', title: 'Backup fixture', collection_id: collection.id, enabled: false, mode: 'review', interval_hours: 24, subtitles: { languages: [], automatic: false } }
const subscription = await json('/youtube/subscriptions', { method: 'POST', body: JSON.stringify(subscriptionInput) })
await json('/youtube/subscriptions', { method: 'PUT', body: JSON.stringify({ ...subscriptionInput, id: subscription.id, revision: subscription.revision, enabled: true }) })
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
await json('/library/movies/1/versions', { method: 'PUT', body: JSON.stringify({ enabled: false, hd_profile_id: 1, uhd_profile_id: 2, revision: versions.revision }) })
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
assert.deepEqual(await json('/library/movies/1/versions'), versions)
const versionFiles = await json('/library/items/1')
assert.equal(versionFiles.file_count, hasVersionFiles ? 2 : 0)
assert.deepEqual(versionFiles.files.map(f => f.version_label).sort(), hasVersionFiles ? ['1080p', '4K (2160p)'] : [])
assert.equal((await json('/subtitles/preferences')).preferences.download_format, 'original')
const restoredClients = await json('/download-clients')
// The backup includes the app-provisioned built-in client next to the
// named one; find the named client by id instead of assuming a single row.
const restoredNamed = restoredClients.items.find(c => c.id === client.id)
assert.ok(restoredNamed)
assert.equal(restoredNamed.revision, enabledClient.revision)
assert.equal(restoredNamed.local_folder, 'owned-client')
assert.equal(restoredClients.defaults.torrent, client.id)
assert.equal(JSON.stringify(restoredClients).includes(clientBody.password), false)
await json(`/download-clients/${client.id}/test`, { method: 'POST', body: '{}' })
state = await json('/system/backups')
assert.equal(state.recovery_pending, true)
assert.equal(state.schedule.enabled, false)
assert.ok(state.restore_message.includes('completed'))
assert.equal((await json('/system/metadata-refresh')).enabled, false)
assert.equal((await json('/youtube/subscriptions')).items[0].enabled, false)
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
clientServer.close()
