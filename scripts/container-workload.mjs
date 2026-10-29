// Bounded synthetic workload against the dedicated fresh load-test container.
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'
assert.equal(process.env.REELSCAREY_WORKLOAD,'1')
const seconds=Number(process.argv[2]??300)
assert.ok(Number.isInteger(seconds)&&seconds>=60&&seconds<=1800)
const base='http://127.0.0.1:18787'
let cookie='',csrf='',failures=0,writes=0
const times=[]
async function call(path,body,method=body===undefined?'GET':'POST'){
 const start=performance.now()
 const response=await fetch(base+'/api/v1'+path,{method,headers:{Cookie:cookie,'X-CSRF-Token':csrf,Origin:base,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(10000)})
 assert.ok(response.ok,`${method} ${path} returned ${response.status}`)
 if(path==='/auth/setup')cookie=response.headers.get('set-cookie').split(';')[0]
 const out=await response.json()
 if(method==='GET'&&path!=='/settings')times.push(performance.now()-start)
 return out
}
const session=await call('/auth/setup',{username:'load-fixture-admin',password:'disposable-load-password',setup_token:(await readFile('/fixture/setup-token','utf8')).trim()})
csrf=session.csrf_token
const endpoints=['/library?kind=movie','/library?kind=series','/wanted?monitored=all','/calendar?monitored=all','/system/storage','/events?page=1','/system/metadata-refresh','/quality-profiles']
const until=performance.now()+seconds*1000
await Promise.all([...Array.from({length:8},(_,index)=>(async()=>{let n=index;while(performance.now()<until){try{await call(endpoints[n++%endpoints.length])}catch{failures++}await delay(100)}})()),(async()=>{while(performance.now()<until){try{const current=await call('/settings');await call('/settings',{library_name:`Synthetic load ${++writes}`,movie_folder:current.movie_folder,series_folder:current.series_folder,episode_naming:current.episode_naming,revision:current.revision},'PUT')}catch{failures++}await delay(5000)}})()])
times.sort((a,b)=>a-b)
const p95=times[Math.floor(times.length*0.95)]??0,p99=times[Math.floor(times.length*0.99)]??0
console.log(JSON.stringify({seconds,concurrent_readers:8,reads:times.length,settings_writes:writes,failures,p95_ms:Math.round(p95),p99_ms:Math.round(p99)}))
assert.equal(failures,0)
assert.ok(times.length>=seconds*5,'Sustained workload made insufficient progress')
assert.ok(p95<1000,'Read p95 exceeded the one-second scenario budget')
assert.equal((await call('/library?kind=movie')).total,1000)
assert.equal((await call('/system/metadata-refresh')).enabled,false)
