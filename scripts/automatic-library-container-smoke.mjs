import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'
assert.equal(process.env.REELSCAREY_AUTOMATIC_SMOKE, '1')
const base = 'http://127.0.0.1:18787'
let cookie = '', csrf = ''
async function call(path, body, status = 200) {
  const response = await fetch(base+'/api/v1'+path, {method:body === undefined ? 'GET':'POST', headers:{Cookie:cookie,'X-CSRF-Token':csrf,Origin:base,'Content-Type':'application/json'},body:body === undefined ? undefined:JSON.stringify(body),signal:AbortSignal.timeout(20000)})
  const text = await response.text()
  assert.equal(response.status,status,`${path}: ${text}`)
  if(path === '/auth/setup') cookie = response.headers.get('set-cookie').split(';')[0]
  return JSON.parse(text)
}
async function until(read, accept) {
  for(let i=0;i<120;i++) {const value=await read(); if(accept(value)) return value; await delay(500)}
  throw new Error('Automatic registration timed out')
}
const session=await call('/auth/setup',{username:'automatic-fixture',password:'disposable-automatic-password',setup_token:(await readFile('/fixture/setup-token','utf8')).trim()},201)
csrf=session.csrf_token
const movie=await call('/library/imports',{kind:'movie'},202)
const discovered=await until(()=>call(`/library/imports/${movie.id}?page=1`),p=>p.scan.state==='completed')
assert.equal(discovered.scan.automatic_matching,undefined)
assert.equal(discovered.scan.registered,0)
await call(`/library/imports/${movie.id}/matches/automatic`,{action:'start'},202)
const matched=await until(()=>call(`/library/imports/${movie.id}?page=1`),p=>p.scan.automatic_matching?.state==='completed' && p.scan.queued===0)
assert.equal(matched.scan.registered,1)
assert.equal(matched.scan.automatic_matching.review,3)
const tv=await call('/library/imports',{kind:'series',automatic_matching:true},202)
const episodes=await until(()=>call(`/library/imports/${tv.id}?page=1`),p=>p.scan.automatic_matching?.state==='completed' && p.scan.queued===0)
assert.equal(episodes.scan.registered,2)
assert.equal(episodes.scan.automatic_matching.queued,2)
console.log('Authenticated scan controls and movie/TV automatic matching passed.')
