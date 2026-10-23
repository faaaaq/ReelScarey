// Uses a disposable config directory and never reads a real media library.
import assert from 'node:assert/strict'
import { spawn, spawnSync } from 'node:child_process'
import { mkdtemp, rm, readFile } from 'node:fs/promises'
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
  await start()
  const health = await fetch(`${base}/healthz`)
  assert.equal(health.status, 200)
  const page = await fetch(`${base}/settings`)
  assert.equal(page.status, 200)
  const html = await page.text()
  assert.match(html, /ReelScarey/)
  const asset = html.match(/src="([^"]+\.js)"/)?.[1]
  assert.ok(asset, 'built JavaScript reference missing')
  assert.equal((await fetch(`${base}${asset}`)).status, 200)
  assert.equal((await fetch(`${base}/assets/missing.js`)).status, 404)
  assert.equal((await fetch(`${base}/api/v1/settings`)).status, 401)
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
  assert.equal(system.capabilities.metadata_search, true)
  assert.equal((await authenticated('/api/v1/metadata/movies?query=Test')).status, 409)
  const before = await (await authenticated('/api/v1/settings')).json()
  const saved = await authenticated('/api/v1/settings', {
    method: 'PUT', headers: { Origin: base },
    body: JSON.stringify({ library_name: 'Restart verification', movie_folder: 'Features', series_folder: 'TV', revision: before.revision }),
  })
  assert.equal(saved.status, 200)
  await stop()
  await start()
  const after = await (await authenticated('/api/v1/settings')).json()
  assert.equal(after.library_name, 'Restart verification')
  assert.equal(after.revision, before.revision + 1)
  const dashboard = await (await authenticated('/api/v1/dashboard')).json()
  assert.equal(dashboard.library_name, 'Restart verification')
  assert.deepEqual(dashboard.counts, { movies: 0, series: 0 })
  const check = spawn(binary, ['healthcheck'], { env, windowsHide: true, stdio: 'inherit' })
  const code = await new Promise((resolve, reject) => { check.once('error', reject); check.once('exit', resolve) })
  assert.equal(code, 0)
  const reset = spawnSync(binary, ['reset-password'], { env, input: 'new-test-only-password\n', windowsHide: true, encoding: 'utf8' })
  assert.equal(reset.status, 0)
  assert.equal((await authenticated('/api/v1/settings')).status, 401)
  const login = await fetch(`${base}/api/v1/auth/login`, { method: 'POST', headers: { 'Content-Type': 'application/json', Origin: base }, body: JSON.stringify({ username: 'smoketest', password: 'new-test-only-password' }) })
  assert.equal(login.status, 200)
  cookie = login.headers.get('set-cookie').split(';')[0]; csrf = (await login.json()).csrf_token
  assert.equal((await authenticated('/api/v1/auth/logout', { method: 'POST', body: '{}' })).status, 200)
  assert.equal((await authenticated('/api/v1/settings')).status, 401)
  console.log('PASS: native startup, embedded UI/API, admin setup, session and settings persistence, CLI healthcheck/password reset, session revocation and logout.')
} catch (error) {
  console.error(logs)
  throw error
} finally {
  await stop()
  assert.equal(path.dirname(path.resolve(directory)), path.resolve(tmpdir()), 'cleanup must remain inside the system temporary directory')
  assert.ok(path.basename(directory).startsWith('reelscarey-smoke-'), 'cleanup must target the test-created directory')
  await rm(directory, { recursive: true, force: true })
}
