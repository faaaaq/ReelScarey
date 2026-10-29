// Disposable-container acceptance for a real move plus metadata update.
// Never run this against a user's configured app or media.
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { setTimeout as delay } from 'node:timers/promises'
const base = process.env.REELSCAREY_SMOKE_URL
const container = process.env.REELSCAREY_SMOKE_CONTAINER
if (!base || !container) throw new Error('Disposable container and URL are required')
const run = (...args) => execFileSync('docker', ['exec', container, ...args], { encoding: 'utf8' }).trim()
async function ready() {
  for (let i = 0; i < 100; i++) {
    try { if ((await fetch(`${base}/readyz`, { signal: AbortSignal.timeout(1000) })).ok) return } catch {}
    await delay(200)
  }
  throw new Error('Readiness timed out')
}
await ready()
const code = run('reelscarey', 'setup-token')
const setup = await fetch(`${base}/api/v1/auth/setup`, { method: 'POST', headers: { 'Content-Type': 'application/json', Origin: base }, body: JSON.stringify({ username: 'fixtureadmin', password: 'fixture-only-test-password', setup_token: code }) })
assert.equal(setup.status, 201)
const cookie = setup.headers.get('set-cookie').split(';')[0]
const session = await setup.json()
const headers = { Cookie: cookie, 'X-CSRF-Token': session.csrf_token, 'Content-Type': 'application/json', Origin: base }
run('mkdir', '-p', '/media/Movies/Fixture')
run('ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'color=c=red:s=128x96:r=10', '-f', 'lavfi', '-i', 'sine=frequency=1000', '-t', '1', '-c:v', 'mpeg4', '-c:a', 'aac', '-metadata', 'title=Wrong Fixture', '-metadata:s:a:0', 'language=jpn', '-movflags', 'faststart', '-y', '/media/Movies/Fixture/Film.mp4')
run('cp', '/media/Movies/Fixture/Film.mp4', '/downloads/seeding.mp4')
const before = run('sha256sum', '/downloads/seeding.mp4').split(' ')[0]
run('python3', '-c', `import sqlite3, hashlib, os
p='/media/Movies/Fixture/Film.mp4'
b=open(p,'rb').read()
c=sqlite3.connect('/config/reelscarey.db', timeout=10)
c.execute("INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(4242,'movie',4242,'Correct Fixture',2020,'available')")
c.execute("INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(4242,4242,'Movies/Fixture/Film.mp4',?,?,?,1,'existing','Film.mp4')",(len(b),hashlib.sha256(b).hexdigest(),'{}'))
c.commit()
c.close()`)
async function api(path, options = {}) {
  const response = await fetch(`${base}/api/v1${path}`, { ...options, headers })
  const body = await response.json()
  if (!response.ok) throw new Error(`${path}: ${response.status} ${JSON.stringify(body)}`)
  return body
}
const plan = await api('/organization/plans', { method: 'POST', body: JSON.stringify({ file_ids: [4242], target_library: 'anime', update_embedded_title: true }) })
assert.equal(plan.embedded_title, 'Correct Fixture')
await api(`/organization/plans/${plan.id}/apply`, { method: 'POST', body: '{}' })
let parent, child
for (let i = 0; i < 150; i++) {
  parent = await api(`/organization/plans/${plan.id}`)
  const history = await api('/organization/plans')
  child = history.items.find(item => item.replacement?.tag_parent === plan.id)
  if (parent.state === 'failed' || child?.state === 'failed') throw new Error(`Metadata workflow failed: ${parent.message} ${child?.message}`)
  if (parent.state === 'completed' && child?.state === 'completed') break
  await delay(400)
}
assert.equal(parent?.state, 'completed')
assert.equal(child?.state, 'completed')
const path = run('python3', '-c', `import sqlite3
c=sqlite3.connect('/config/reelscarey.db')
print(c.execute('SELECT relative_path FROM media_files WHERE id=4242').fetchone()[0])`)
const probe = JSON.parse(run('ffprobe', '-v', 'error', '-show_entries', 'format_tags=title:stream_tags=language', '-of', 'json', `/media/${path}`))
assert.equal(probe.format.tags.title, 'Correct Fixture')
assert.ok(probe.streams.some(stream => stream.tags?.language === 'jpn'))
assert.equal(run('sha256sum', '/downloads/seeding.mp4').split(' ')[0], before)
assert.equal(run('sha256sum', `/media/${child.replacement.backup}`).split(' ')[0], before)
const rollback = await api(`/organization/plans/${child.id}/rollback`, { method: 'POST', body: '{}' })
await api(`/organization/plans/${rollback.id}/apply`, { method: 'POST', body: '{}' })
let restored
for (let i = 0; i < 150; i++) {
  restored = await api(`/organization/plans/${rollback.id}`)
  if (restored.state === 'failed') throw new Error(`Rollback failed: ${restored.message}`)
  if (restored.state === 'completed') break
  await delay(400)
}
assert.equal(restored?.state, 'completed')
assert.equal(run('sha256sum', `/media/${path}`).split(' ')[0], before)
assert.equal(run('sha256sum', '/downloads/seeding.mp4').split(' ')[0], before)
const undoMove = await api(`/organization/plans/${plan.id}/rollback`, { method: 'POST', body: '{}' })
assert.equal(undoMove.restores_plan, plan.id)
assert.equal(undoMove.files[0].destination, 'Movies/Fixture/Film.mp4')
await api(`/organization/plans/${undoMove.id}/apply`, { method: 'POST', body: '{}' })
execFileSync('docker', ['restart', container])
await ready()
let undone
for (let i = 0; i < 150; i++) {
  undone = await api(`/organization/plans/${undoMove.id}`)
  if (undone.state === 'failed') throw new Error(`Move undo failed: ${undone.message}`)
  if (undone.state === 'completed') break
  await delay(400)
}
assert.equal(undone?.state, 'completed')
assert.equal(run('sha256sum', '/media/Movies/Fixture/Film.mp4').split(' ')[0], before)
assert.equal(run('sha256sum', '/downloads/seeding.mp4').split(' ')[0], before)
console.log('PASS: real container move, verified embedded title, retained source, version rollback and reviewed move undo across restart')
