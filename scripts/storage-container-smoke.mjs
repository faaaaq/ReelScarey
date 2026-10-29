// Only the disposable volumes created by storage-container-smoke.sh are used.
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
assert.equal(process.env.REELSCAREY_STORAGE_SMOKE, '1')
const base='http://127.0.0.1:18787', mode=process.argv[2]
let cookie='',csrf=''
async function call(path,body,status=200,method=body===undefined?'GET':'POST'){
 const response=await fetch(base+'/api/v1'+path,{method,headers:{Cookie:cookie,'X-CSRF-Token':csrf,Origin:base,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(20000)})
 assert.equal(response.status,status,path)
 if(path.startsWith('/auth/'))cookie=response.headers.get('set-cookie')?.split(';')[0]??cookie
 return response.json()
}
const credentials={username:'storage-fixture-admin',password:'disposable-storage-password'}
const session=mode==='before'?await call('/auth/setup',{...credentials,setup_token:(await readFile('/fixture/setup-token','utf8')).trim()},201):await call('/auth/login',credentials)
csrf=session.csrf_token
if(mode==='before'){
 const settings=await call('/settings')
 await call('/settings',{library_name:'Storage migration fixture',movie_folder:settings.movie_folder,series_folder:settings.series_folder,episode_naming:settings.episode_naming,revision:settings.revision},200,'PUT')
}
const browser=await call('/system/storage')
assert.ok(browser.items.some(v=>v.name==='Movies'&&v.kind==='directory'))
assert.equal(browser.roots.find(v=>v.id==='archive').path,'/archive')
assert.ok((await call('/system/storage?root=archive')).items.some(v=>v.name==='outside.txt'))
await call('/system/storage?path=../config',undefined,400)
await call('/system/storage?root=private',undefined,400)
assert.equal((await fetch(base+'/storage')).status,200)
const catalog=await call('/library?kind=movie')
assert.equal(catalog.total,3)
assert.equal((await call('/settings')).library_name,'Storage migration fixture')
const migration=await call('/system/storage/migration')
if(mode==='before')assert.equal(migration.state,'none')
else{
 assert.equal(migration.state,'completed')
 assert.equal(migration.files,4)
 assert.ok(migration.backup.includes('media-root-'))
 const checks=await call('/system/checks',{})
 assert.ok(checks.report.checks.every(v=>v.state==='ready'))
 if(checks.recovery_pending)await call('/system/recovery/resume',{reviewed:true})
}
console.log(JSON.stringify({phase:mode,folders:'bounded and read-only',catalog:'preserved',settings:'preserved',migration:migration.state}))
