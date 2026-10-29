import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'

assert.equal(process.env.REELSCAREY_SEVEN_SMOKE, '1')
const base = 'http://127.0.0.1:18787'
let cookie = '', csrf = ''
async function call(path, body, status = 200, method = body === undefined ? 'GET' : 'POST') {
  const response = await fetch(base + '/api/v1' + path, { method, headers: {
    Cookie: cookie, 'X-CSRF-Token': csrf, Origin: base, 'Content-Type': 'application/json',
  }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(60000) })
  const text = await response.text()
  assert.equal(response.status, status, `${method} ${path}: ${text}`)
  if (path === '/auth/setup') cookie = response.headers.get('set-cookie').split(';')[0]
  return JSON.parse(text)
}
async function until(read, accept, label) {
  for (let i = 0; i < 140; i++) {
    const value = await read()
    if (accept(value)) return value
    await delay(500)
  }
  throw new Error(label + ' timed out')
}
const session = await call('/auth/setup', { username: 'seven-fixture-admin', password: 'disposable-seven-password', setup_token: (await readFile('/fixture/setup-token', 'utf8')).trim() }, 201)
csrf = session.csrf_token
let refresh = await call('/system/metadata-refresh')
assert.equal(refresh.enabled, false)
refresh = await call('/system/metadata-refresh', { enabled: false, interval_hours: 48, batch_size: 2, only_monitored: false, revision: refresh.revision }, 200, 'PUT')
await call('/system/metadata-refresh/run', {}, 202)
await until(() => call('/system/metadata-refresh'), v => v.state === 'waiting_provider', 'Missing provider waiting state')
assert.equal((await call('/calendar')).items instanceof Array, true)
await call('/library/items/1/anime-order', undefined, 409)
await call('/library/items/1/anime-order/refresh', { revision: 1 }, 409)

const inherited = await call('/library/series/2/subtitles')
assert.equal(inherited.inherited, true)
const override = await call('/library/series/2/subtitles', { revision: inherited.revision, global_revision: inherited.global_revision, enabled: true, tracks: [{ language: 'ja', variant: 'full' }], hearing_impaired: inherited.preferences.hearing_impaired, match_policy: 'hash' }, 200, 'PUT')
assert.equal(override.inherited, false)
assert.equal(override.preferences.tracks[0].language, 'ja')
await call('/library/series/2/subtitles', { revision: override.revision, global_revision: override.global_revision }, 200, 'DELETE')

const edit = await call('/subtitles/files/1/edits', { language: 'en', variant: 'full', action: 'offset', offset_ms: 250 })
assert.equal(edit.cues, 1)
await call(`/subtitles/edits/${edit.id}/apply`, { reviewed: false }, 400)
await call(`/subtitles/edits/${edit.id}/apply`, { reviewed: true }, 202)
await until(() => call('/subtitles/files/1/edits'), v => v.items.some(e => e.id === edit.id && e.state === 'completed'), 'Subtitle edit')
const rollback = await call('/subtitles/files/1/edits', { language: 'en', variant: 'full', action: 'rollback', previous_edit_id: edit.id })
await call(`/subtitles/edits/${rollback.id}/apply`, { reviewed: true }, 202)
await until(() => call('/subtitles/files/1/edits'), v => v.items.some(e => e.id === rollback.id && e.state === 'completed'), 'Subtitle rollback')
const replacement = await call('/subtitles/files/1/edits', { language: 'en', variant: 'full', action: 'replace', content: '1\n00:00:00,500 --> 00:00:01,500\nReviewed generated replacement.\n' })
await call(`/subtitles/edits/${replacement.id}/apply`, { reviewed: true }, 202)
await until(() => call('/subtitles/files/1/edits'), v => v.items.some(e => e.id === replacement.id && e.state === 'completed'), 'Subtitle replacement')

const profiles = await call('/quality-profiles')
const profile = profiles.items[0]
profile.scoring = { sources: ['remux', 'webdl'], codecs: ['av1', 'h265'], preferred_terms: 'Fixture', max_usenet_age_days: 30, min_usenet_age_minutes: 15 }
const saved = await call('/quality-profiles/' + profile.id, profile, 200, 'PUT')
assert.deepEqual(saved.scoring, profile.scoring)

const collection = await call('/youtube/collections', { title: 'Disposable collection' }, 201)
const sourceURL = 'https://www.youtube.com/playlist?list=PLfixture012345'
const sub = await call('/youtube/subscriptions', { source_url: sourceURL, title: 'Disposable playlist', collection_id: collection.id, enabled: false, mode: 'review', interval_hours: 24, subtitles: { languages: [], automatic: false } })
assert.equal(sub.enabled, false)
await call('/youtube/subscriptions/' + sub.id, { revision: sub.revision }, 202)
await until(() => call('/youtube/subscriptions'), v => v.items.some(s => s.id === sub.id && s.initialized), 'Subscription baseline')
const entries = await call('/youtube/subscriptions/' + sub.id)
assert.equal(entries.items.length, 2)
assert.ok(entries.items.every(v => v.state === 'baseline'))
const bulk = await call('/youtube/bulk/review', { source_url: sourceURL, video_ids: entries.items.map(v => v.id), target: { kind: 'custom', collection_id: collection.id }, subtitles: { languages: [], automatic: false } })
await call('/youtube/bulk/queue', { review_ids: bulk.items.map(v => v.id), reviewed: false }, 400)
assert.equal((await call('/youtube/bulk/queue', { review_ids: bulk.items.map(v => v.id), reviewed: true }, 201)).items.length, 2)
await call('/youtube/subscriptions/' + sub.id, { revision: sub.revision }, 200, 'DELETE')

const scan = await call('/library/imports', { kind: 'movie', library_key: 'movies' }, 202)
const found = await until(() => call('/library/imports/' + scan.id), v => v.scan.state === 'completed', 'Library scan')
const files = found.items.filter(f => f.path.includes('Bulk '))
assert.equal(files.length, 2)
const suggestions = await call(`/library/imports/${scan.id}/matches/suggest`, { file_ids: files.map(f => f.id), include_provider: false })
const choices = suggestions.items.map(v => ({ file_id: v.file.id, media_id: v.candidates[0].media_id, episode_ids: [] }))
const review = await call(`/library/imports/${scan.id}/matches/review`, { items: choices })
await call(`/library/imports/${scan.id}/matches/apply`, { review_id: review.review_id, reviewed: false }, 400)
await call(`/library/imports/${scan.id}/matches/apply`, { review_id: review.review_id, reviewed: true }, 202)
await until(() => call('/library/imports/' + scan.id), v => v.items.filter(f => f.path.includes('Bulk ')).every(f => f.state === 'registered'), 'Bulk registration')
for (const path of ['/metadata-refresh', '/youtube/subscriptions', '/subtitles', '/calendar', '/import', '/movies/1']) {
  assert.equal((await fetch(base + path)).status, 200, path)
}
console.log(JSON.stringify({ metadata: 'provider wait and paused schedule verified', anilist: 'non-Anime refresh rejected; refreshed facts covered by provider contract tests', show_subtitles: 'saved and reset', subtitle_edits: 'offset, rollback and upload completed', scoring: 'persisted', youtube: 'fixture baseline and atomic bulk queue', bulk_matching: 'generated videos registered after review' }))
