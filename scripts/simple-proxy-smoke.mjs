import assert from 'node:assert/strict'
import http from 'node:http'
const base = 'http://127.0.0.1:18787/api/v1'
const origin = 'https://reelscarey.example.test'
const headers = { Host: 'reelscarey.example.test', Origin: origin, 'Content-Type': 'application/json' }
const body = JSON.stringify({ username: 'backup-fixture-admin', password: 'disposable-backup-password' })
// Native HTTP preserves the explicit Host, unlike fetch implementations that
// replace it with the URL host. This models the proxy's backend request.
function request(path, options = {}) {
  return new Promise((resolve, reject) => {
    const req = http.request(base + path, options, res => {
      let text = ''
      res.setEncoding('utf8')
      res.on('data', chunk => { text += chunk })
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, text }))
    })
    req.on('error', reject)
    req.setTimeout(10000, () => req.destroy(new Error('Proxy test timed out')))
    req.end(options.body)
  })
}
let r = await request('/auth/login', { method: 'POST', headers: { ...headers, Origin: 'https://other.example.test' }, body })
assert.equal(r.status, 403)
r = await request('/auth/login', { method: 'POST', headers, body })
assert.equal(r.status, 200)
assert.match(r.headers['set-cookie'][0], /; Secure/i)
const cookie = r.headers['set-cookie'][0].split(';')[0]
const session = await request('/auth/session', { headers: { Host: headers.Host, Cookie: cookie } })
assert.equal(JSON.parse(session.text).authenticated, true)
r = await request('/auth/session', { headers: { Host: 'untrusted.example.test' } })
assert.equal(r.status, 403)
console.log('HTTPS proxy login, Secure cookie, session persistence and foreign Host/Origin rejection verified.')