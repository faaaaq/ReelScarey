// Uses a disposable config directory and never reads a real media library.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
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
  REELSCAREY_CONFIG_DIR: path.join(directory, 'config'),
  REELSCAREY_MEDIA_DIR: path.join(directory, 'media'),
  REELSCAREY_DOWNLOADS_DIR: path.join(directory, 'downloads'),
}
let child
let logs = ''

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
  const system = await (await fetch(`${base}/api/v1/system`)).json()
  assert.equal(system.database, 'connected')
  assert.equal(system.capabilities.metadata_search, false)
  const before = await (await fetch(`${base}/api/v1/settings`)).json()
  const saved = await fetch(`${base}/api/v1/settings`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', Origin: base },
    body: JSON.stringify({ library_name: 'Restart verification', movie_folder: 'Features', series_folder: 'TV', revision: before.revision }),
  })
  assert.equal(saved.status, 200)
  await stop()
  await start()
  const after = await (await fetch(`${base}/api/v1/settings`)).json()
  assert.equal(after.library_name, 'Restart verification')
  assert.equal(after.revision, before.revision + 1)
  const dashboard = await (await fetch(`${base}/api/v1/dashboard`)).json()
  assert.equal(dashboard.library_name, 'Restart verification')
  assert.deepEqual(dashboard.counts, { movies: 0, series: 0 })
  const check = spawn(binary, ['healthcheck'], { env, windowsHide: true, stdio: 'inherit' })
  const code = await new Promise((resolve, reject) => { check.once('error', reject); check.once('exit', resolve) })
  assert.equal(code, 0)
  console.log('PASS: native startup, health/readiness, embedded UI/assets, API, settings persistence across restart, and CLI healthcheck.')
} catch (error) {
  console.error(logs)
  throw error
} finally {
  await stop()
  assert.equal(path.dirname(path.resolve(directory)), path.resolve(tmpdir()), 'cleanup must remain inside the system temporary directory')
  assert.ok(path.basename(directory).startsWith('reelscarey-smoke-'), 'cleanup must target the test-created directory')
  await rm(directory, { recursive: true, force: true })
}
