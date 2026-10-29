// Uses a disposable config directory and never reads a real media library.
import assert from 'node:assert/strict'
import { spawn, spawnSync } from 'node:child_process'
import { mkdir, mkdtemp, rm, readFile } from 'node:fs/promises'
import net from 'node:net'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

const binary = path.resolve(process.argv[2] ?? (process.platform === 'win32' ? 'bin/reelscarey.exe' : 'bin/reelscarey'))
const directory = await mkdtemp(path.join(tmpdir(), 'reelscarey-smoke-'))
const listener = net.createServer()
await new Promise((resolve, reject) => { listener.once('error', reject); listener.listen(0, '127.0.0.1', resolve) })
const port = listener.address().port
await new Promise(resolve => listener.close(resolve))
const base = `http://127.0.0.1:${port}`
const env = {
  ...process.env,
  REELSCAREY_LISTEN_ADDR: `127.0.0.1:${port}`,
  REELSCAREY_PUBLIC_URL: base,
  REELSCAREY_CONFIG_DIR: path.join(directory, 'config'),
  REELSCAREY_MEDIA_DIR: path.join(directory, 'media'),
  REELSCAREY_DOWNLOADS_DIR: path.join(directory, 'downloads'),
}
let child
let logs = ''
let cookie = ''
let csrf = ''
async function authenticated(endpoint, options = {}) {
  return fetch(`${base}${endpoint}`, { ...options, headers: { Cookie: cookie, 'X-CSRF-Token': csrf, ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...options.headers } })
}

async function start() {
  child = spawn(binary, [], { env, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] })
  let spawnError
  child.on('error', error => { spawnError = error })
  child.stdout.on('data', chunk => { logs += chunk })
  child.stderr.on('data', chunk => { logs += chunk })
  for (let attempt = 0; attempt < 80; attempt++) {
    if (spawnError) throw spawnError
    if (child.exitCode !== null) throw new Error(`App exited during startup: ${logs}`)
    try { const response = await fetch(`${base}/readyz`, { signal: AbortSignal.timeout(1000) }); if (response.ok) return } catch { /* Wait for startup. */ }
    await delay(100)
  }
  throw new Error(`App did not become ready: ${logs}`)
}

async function stop() {
  if (!child || child.exitCode !== null) return
  const stopped = new Promise(resolve => child.once('exit', resolve))
  child.kill('SIGTERM')
  const timeout = setTimeout(() => child.kill('SIGKILL'), 12000)
  try { await stopped } finally { clearTimeout(timeout); child = undefined }
}

try {
  await mkdir(env.REELSCAREY_MEDIA_DIR)
  await mkdir(env.REELSCAREY_DOWNLOADS_DIR)
  await start()
  const duplicate = spawnSync(binary, [], { env: { ...env, REELSCAREY_LISTEN_ADDR: `127.0.0.1:${port === 65535 ? port - 1 : port + 1}` }, windowsHide: true, encoding: 'utf8', timeout: 15000 })
  assert.notEqual(duplicate.status, 0)
  assert.match(duplicate.stdout + duplicate.stderr, /config directory is in use/)
  const activeBackup = spawnSync(binary, ['backup', path.join(directory, 'active-backup')], { env, windowsHide: true, encoding: 'utf8', timeout: 15000 })
  assert.notEqual(activeBackup.status, 0, 'backup must refuse a running server')
  const health = await fetch(`${base}/healthz`)
  assert.equal(health.status, 200)
  const page = await fetch(`${base}/automation`)
  assert.equal(page.status, 200)
  const html = await page.text()
  assert.match(html, /ReelScarey/)
  const asset = html.match(/src="([^"]+\.js)"/)?.[1]
  assert.ok(asset, 'built JavaScript reference missing')
  assert.equal((await fetch(`${base}${asset}`)).status, 200)
  assert.equal((await fetch(`${base}/assets/missing.js`)).status, 404)
  assert.equal((await fetch(`${base}/api/v1/settings`)).status, 401)
  assert.equal((await fetch(`${base}/subtitles`)).status, 200)
  assert.equal((await fetch(`${base}/api/v1/subtitles/preferences`)).status, 401)
  const status = await (await fetch(`${base}/api/v1/auth/session`)).json()
  assert.equal(status.setup_required, true)
  const setupCode = (await readFile(path.join(directory, 'config', 'setup-token'), 'utf8')).trim()
  const printed = spawnSync(binary, ['setup-token'], { env, windowsHide: true, encoding: 'utf8' })
  assert.equal(printed.status, 0)
  assert.equal(printed.stdout.trim(), setupCode)
  const setup = await fetch(`${base}/api/v1/auth/setup`, { method: 'POST', headers: { 'Content-Type': 'application/json', Origin: base }, body: JSON.stringify({ username: 'smoketest', password: 'test-only-smoke-password', setup_token: setupCode }) })
  assert.equal(setup.status, 201)
  cookie = setup.headers.get('set-cookie').split(';')[0]
  const session = await setup.json()
  csrf = session.csrf_token
  assert.ok(!JSON.stringify(session).includes(cookie.split('=')[1]), 'raw session exposed to JavaScript')
  const system = await (await authenticated('/api/v1/system')).json()
  assert.equal(system.database, 'connected')
  assert.equal((await authenticated('/api/v1/library/monitoring/preview', { method: 'POST', body: JSON.stringify({kind: 'movie', ids: []}) })).status, 400)
  assert.equal((await authenticated('/api/v1/library/monitoring', { method: 'PUT', body: JSON.stringify({kind: 'movie', monitored: false, items: []}) })).status, 400)
  const catalog = await authenticated('/api/v1/library?kind=movie&query=100%25_&status=missing&monitored=monitored&sort=added')
  assert.equal(catalog.status, 200)
  const catalogPage = await catalog.json()
  const {profile_context, ...catalogRows} = catalogPage
  assert.deepEqual(catalogRows, { items: [], page: 1, total: 0, limit: 24, collection_total: 0 })
  assert.equal((await authenticated('/api/v1/library/profiles', {method:'PUT',body:'{}'})).status,400)
  assert.equal(profile_context.default_profile.id, 1)
  assert.equal(profile_context.profiles.length, 2)
  assert.equal((await authenticated('/api/v1/library/profiles/preview', {method: 'POST',body:JSON.stringify({kind:'movie',ids:[],target:{use_default:true}})})).status,400)
  assert.equal(system.capabilities.metadata_search, true)
  assert.equal(system.capabilities.automation, true)
  assert.equal(system.capabilities.subtitles, true)
  assert.deepEqual((await (await authenticated('/api/v1/indexers')).json()).items, [])
  assert.equal((await (await authenticated('/api/v1/indexers/status')).json()).configured, false)
  assert.equal((await authenticated('/indexers')).status, 200)
  assert.equal((await authenticated('/movies/1')).status, 200)
  assert.equal((await authenticated('/import')).status, 200)
  assert.deepEqual((await (await authenticated('/api/v1/library/imports')).json()).items, [])
  for (const route of ['/wanted', '/calendar']) assert.equal((await authenticated(route)).status, 200)
  for (const route of ['/api/v1/wanted', '/api/v1/calendar?month=2026-10']) { const response = await authenticated(route); assert.equal(response.status, 200); const planning = await response.json(); assert.deepEqual(planning.items, []); assert.equal(planning.total, 0); assert.equal(planning.limit, 50); assert.equal(planning.coverage.unloaded_seasons, 0) }
  const subtitles = await (await authenticated('/api/v1/subtitles/preferences')).json()
  assert.equal(subtitles.preferences.enabled, false)
  assert.deepEqual(subtitles.preferences.tracks, [{ language: 'en', variant: 'full' }, { language: 'en', variant: 'signs' }, { language: 'en', variant: 'forced' }])
  const subtitlePreferences = { ...subtitles.preferences, enabled: true, match_policy: 'hash', tracks: [...subtitles.preferences.tracks, { language: 'ja', variant: 'full' }] }
  assert.equal((await authenticated('/api/v1/subtitles/preferences', { method: 'PUT', body: JSON.stringify(subtitlePreferences) })).status, 200)
  assert.equal((await (await authenticated('/api/v1/providers/opensubtitles')).json()).configured, false)
  const schedule = await (await authenticated('/api/v1/automation')).json()
  assert.equal(schedule.enabled, false)
  assert.equal(schedule.indexer_connected, false)
  const profiles = await (await authenticated('/api/v1/quality-profiles')).json()
  const profile = { ...profiles.items[0], name: 'Persistent smoke profile', language: 'english' }
  assert.equal((await authenticated(`/api/v1/quality-profiles/${profile.id}`, { method: 'PUT', body: JSON.stringify(profile) })).status, 200)
  assert.equal((await authenticated('/api/v1/automation', { method: 'PUT', body: JSON.stringify({ enabled: true, mode: 'search', interval_minutes: 60, batch_size: 5, default_profile_id: profile.id, revision: schedule.revision }) })).status, 200)
  assert.equal((await authenticated('/api/v1/metadata/movies?query=Test')).status, 409)
  const before = await (await authenticated('/api/v1/settings')).json()
  const saved = await authenticated('/api/v1/settings', {
    method: 'PUT', headers: { Origin: base },
    body: JSON.stringify({ library_name: 'Restart verification', movie_folder: 'Features', series_folder: 'TV', revision: before.revision }),
  })
  assert.equal(saved.status, 200)
  await mkdir(path.join(env.REELSCAREY_MEDIA_DIR, 'Features'))
  const scanStart = await authenticated('/api/v1/library/imports', { method: 'POST', body: JSON.stringify({ kind: 'movie' }) })
  assert.equal(scanStart.status, 202)
  const scanId = (await scanStart.json()).id
  let scanState
  for (let attempt = 0; attempt < 80; attempt++) {
    scanState = await (await authenticated(`/api/v1/library/imports/${scanId}?page=1`)).json()
    if (scanState.scan.state === 'completed') break
    assert.notEqual(scanState.scan.state, 'failed')
    await delay(100)
  }
  assert.equal(scanState.scan.state, 'completed')
  assert.equal(scanState.total, 0)
  await stop()
  await start()
  assert.equal((await (await authenticated('/api/v1/library/imports')).json()).items[0].id, scanId)
  const after = await (await authenticated('/api/v1/settings')).json()
  assert.equal(after.library_name, 'Restart verification')
  assert.equal(after.revision, before.revision + 1)
  const subtitlesAfter = await (await authenticated('/api/v1/subtitles/preferences')).json()
  assert.equal(subtitlesAfter.preferences.enabled, true)
  assert.equal(subtitlesAfter.preferences.match_policy, 'hash')
  assert.deepEqual(subtitlesAfter.preferences.tracks, subtitlePreferences.tracks)
  assert.equal((await (await authenticated('/api/v1/providers/opensubtitles')).json()).configured, false)
  const automationAfter = await (await authenticated('/api/v1/automation')).json()
  assert.equal(automationAfter.enabled, true)
  assert.equal(automationAfter.interval_minutes, 60)
  assert.equal(automationAfter.state, 'waiting_indexer')
  assert.equal((await (await authenticated('/api/v1/quality-profiles')).json()).items[0].name, 'Persistent smoke profile')
  const dashboard = await (await authenticated('/api/v1/dashboard')).json()
  assert.equal(dashboard.library_name, 'Restart verification')
  assert.deepEqual(dashboard.counts, { movies: 0, series: 0, anime: 0 })
  const check = spawn(binary, ['healthcheck'], { env, windowsHide: true, stdio: 'inherit' })
  const code = await new Promise((resolve, reject) => { check.once('error', reject); check.once('exit', resolve) })
  assert.equal(code, 0)
  const reset = spawnSync(binary, ['reset-password'], { env, input: 'new-test-only-password\n', windowsHide: true, encoding: 'utf8' })
  assert.equal(reset.status, 0)
  assert.equal((await authenticated('/api/v1/settings')).status, 401)
  const login = await fetch(`${base}/api/v1/auth/login`, { method: 'POST', headers: { 'Content-Type': 'application/json', Origin: base }, body: JSON.stringify({ username: 'smoketest', password: 'new-test-only-password' }) })
  assert.equal(login.status, 200)
  cookie = login.headers.get('set-cookie').split(';')[0]; csrf = (await login.json()).csrf_token
  const guide = await (await authenticated('/api/v1/setup')).json()
  assert.equal(guide.steps.find(step => step.id === 'torznab').optional, true)
  assert.equal((await (await authenticated('/api/v1/system/checks')).json()).report, null, 'GET must not run probes')
  await stop()
  const backup = path.join(directory, 'verified-backup')
  const recovered = path.join(directory, 'recovered-config')
  for (const args of [['backup', backup], ['backup-check', backup], ['restore', backup, recovered]]) {
    const command = spawnSync(binary, args, { env, windowsHide: true, encoding: 'utf8', timeout: 60000 })
    assert.equal(command.status, 0, `Maintenance command failed: ${command.stdout}${command.stderr}`)
  }
  const secondRestore = spawnSync(binary, ['restore', backup, recovered], { env, windowsHide: true, encoding: 'utf8', timeout: 15000 })
  assert.notEqual(secondRestore.status, 0, 'restore must preserve an existing config')
  env.REELSCAREY_CONFIG_DIR = recovered
  await start()
  assert.equal((await authenticated('/api/v1/settings')).status, 401, 'restore must revoke copied sessions')
  const recoveredLogin = await fetch(`${base}/api/v1/auth/login`, { method: 'POST', headers: { 'Content-Type': 'application/json', Origin: base }, body: JSON.stringify({ username: 'smoketest', password: 'new-test-only-password' }) })
  assert.equal(recoveredLogin.status, 200)
  cookie = recoveredLogin.headers.get('set-cookie').split(';')[0]; csrf = (await recoveredLogin.json()).csrf_token
  const recoveredState = await (await authenticated('/api/v1/system/checks')).json()
  assert.equal(recoveredState.recovery_pending, true)
  assert.equal((await (await authenticated('/api/v1/automation')).json()).enabled, false)
  assert.equal((await (await authenticated('/api/v1/subtitles/preferences')).json()).preferences.enabled, false)
  assert.equal((await (await authenticated('/api/v1/settings')).json()).library_name, 'Restart verification')
  assert.equal((await authenticated('/api/v1/system/recovery/resume', { method: 'POST', body: '{"reviewed":false}' })).status, 400)
  const checks = await authenticated('/api/v1/system/checks', { method: 'POST', body: '{}' })
  assert.equal(checks.status, 200)
  for (const check of (await checks.json()).report.checks) assert.equal(check.state, 'ready', `${check.title}: ${check.message}`)
  assert.equal((await authenticated('/api/v1/system/recovery/resume', { method: 'POST', body: '{"reviewed":true}' })).status, 200)
  assert.equal((await (await authenticated('/api/v1/system/checks')).json()).recovery_pending, false)
  assert.equal((await (await authenticated('/api/v1/automation')).json()).enabled, false)
  const checkBackup = spawnSync(binary, ['backup-check', backup], { env, windowsHide: true, encoding: 'utf8', timeout: 15000 })
  assert.equal(checkBackup.status, 0, 'restore must not alter the backup')
  assert.equal((await authenticated('/api/v1/auth/logout', { method: 'POST', body: '{}' })).status, 200)
  assert.equal((await authenticated('/api/v1/settings')).status, 401)
  console.log('PASS: native startup/restart, embedded UI, auth and preferences; duplicate instance and live-backup refusal; verified offline backup, new-folder restore, revoked sessions, held workers, explicit recovery review and paused schedules; deferred Torznab and logout.')
} catch (error) {
  console.error(logs)
  throw error
} finally {
  await stop()
  assert.equal(path.dirname(path.resolve(directory)), path.resolve(tmpdir()), 'cleanup must remain inside the system temporary directory')
  assert.ok(path.basename(directory).startsWith('reelscarey-smoke-'), 'cleanup must target the test-created directory')
  await rm(directory, { recursive: true, force: true })
}
