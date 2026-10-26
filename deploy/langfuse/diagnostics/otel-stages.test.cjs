const {test}=require('node:test');
const assert=require('node:assert/strict');
const {createOtelStageDiagnostics}=require('./otel-stages.cjs');
const deferred=()=>{let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b});return {promise,resolve,reject}};
function fixture(options={}){
 let time=0;const timers=new Set(),emitted=[];
 const d=createOtelStageDiagnostics({enabled:true,emit:s=>emitted.push(JSON.parse(s)),now:()=>time,schedule:(fn,ms)=>{assert.equal(ms,2000);const t={fn,unref(){}};timers.add(t);return t},cancel:t=>timers.delete(t),...options});
 return {d,emitted,timers,at:t=>{time=t},tick:()=>{for(const t of [...timers])t.fn()}};
}
test('disabled default preserves exact values, promises and error identity without inspecting result',()=>{
 const d=createOtelStageDiagnostics();const p=Promise.resolve(7),error=new Error('secret');const custom=Object.defineProperty({},'then',{get(){throw error}});
 assert.equal(d.run(()=>p),p);assert.equal(d.stage('s3_upload',()=>p),p);assert.equal(d.run(()=>custom),custom);
 assert.throws(()=>d.run(()=>{throw error}),e=>e===error);assert.equal(d.snapshot().records.length,0);
});
test('fast success returns business result and does not emit or retain a timer',async()=>{
 const f=fixture(),value={secret:'do not log'};const r=await f.d.run(()=>f.d.stage('s3_upload',()=>Promise.resolve(value)));
 assert.equal(r,value);assert.equal(f.emitted.length,0);assert.equal(f.timers.size,0);assert.equal(f.d.snapshot().active_requests,0);
});
for(const stage of ['s3_upload','queue_add','other'])test(`pending ${stage} identifies active phase without cancelling business work`,async()=>{
 const f=fixture(),wait=deferred();let calls=0;
 const result=f.d.run(()=>stage==='other'?wait.promise:f.d.stage(stage,()=>{calls++;return wait.promise}));
 f.at(2000);f.tick();assert.equal(f.emitted.length,1);assert.equal(f.emitted[0].event,'pending');assert.equal(f.emitted[0].stage,stage);
 assert.equal(f.d.snapshot().active_requests,1);f.at(2100);wait.resolve('unchanged');assert.equal(await result,'unchanged');
 assert.equal(calls,stage==='other'?0:1);assert.deepEqual(f.emitted.map(x=>x.event),['pending','complete']);assert.equal(f.timers.size,0);
});
test('stage failure preserves original error and emits no message, stack, body or header',async()=>{
 const f=fixture(),error=new Error('Authorization: SECRET https://private/ body');
 await assert.rejects(f.d.run(()=>f.d.stage('queue_add',()=>Promise.reject(error))),e=>e===error);
 const row=f.emitted[0];assert.equal(row.event,'failed');assert.equal(row.stage,'queue_add');assert.equal(JSON.stringify(f.d.snapshot()).includes('SECRET'),false);
 assert.equal(JSON.stringify(f.d.snapshot()).includes('https'),false);
});
test('unrelated final error after caught phase failure is other',async()=>{
 const f=fixture(),a=new Error('a'),b=new Error('b');await assert.rejects(f.d.run(async()=>{try{await f.d.stage('s3_upload',()=>Promise.reject(a))}catch{}throw b}),e=>e===b);
 assert.equal(f.emitted[0].stage,'other');
});
test('synchronous errors and values preserve identity when enabled',()=>{
 const f=fixture(),error=new Error('x');assert.equal(f.d.run(()=>23),23);assert.throws(()=>f.d.run(()=>f.d.stage('s3_upload',()=>{throw error})),e=>e===error);assert.equal(f.emitted[0].stage,'s3_upload');
 const custom=Object.defineProperty({},'then',{get(){throw Error('never inspect')}});assert.equal(f.d.run(()=>custom),custom);
});
test('logger exceptions cannot change resolved or rejected business results',async()=>{
 const f=fixture({emit:()=>{throw Error('logging failed')}}),error=new Error('business');
 await assert.rejects(f.d.run(()=>Promise.reject(error)),e=>e===error);const w=deferred(),r=f.d.run(()=>w.promise);f.at(2000);f.tick();w.resolve(9);assert.equal(await r,9);
});
test('concurrent requests have isolated sequence and phase',async()=>{
 const f=fixture(),a=deferred(),b=deferred();const x=f.d.run(()=>f.d.stage('s3_upload',()=>a.promise)),y=f.d.run(()=>f.d.stage('queue_add',()=>b.promise));f.at(2000);f.tick();
 assert.deepEqual(f.emitted.map(r=>[r.request_sequence,r.stage]),[[1,'s3_upload'],[2,'queue_add']]);a.resolve(1);b.resolve(2);await Promise.all([x,y]);
});
test('overlapping stages are reported as other, never arbitrarily assigned',async()=>{
 const f=fixture(),a=deferred(),b=deferred();const r=f.d.run(()=>Promise.all([f.d.stage('s3_upload',()=>a.promise),f.d.stage('queue_add',()=>b.promise)]));f.at(2000);f.tick();assert.equal(f.emitted[0].stage,'other');a.resolve();b.resolve();await r;
});
test('unknown phase is not inspected or logged; operations outside context are unchanged',async()=>{
 const f=fixture(),p=Promise.resolve();assert.equal(f.d.stage('queue_add',()=>p),p);
 await assert.rejects(f.d.run(()=>f.d.stage('SECRET',()=>Promise.reject(Error('SECRET')))));assert.equal(f.emitted[0].stage,'other');assert.ok(!JSON.stringify(f.d.snapshot()).includes('SECRET'));
});
test('late observation callback is inert after completion',async()=>{
 const f=fixture();let late;await f.d.run(()=>{late=[...f.timers][0].fn;return Promise.resolve()});f.at(9000);late();assert.equal(f.emitted.length,0);
});
test('completion after threshold without timer firing still emits one slow completion',async()=>{
 const f=fixture(),w=deferred();const r=f.d.run(()=>w.promise);f.at(2100);w.resolve();await r;assert.deepEqual(f.emitted.map(x=>x.event),['complete']);
});
test('record and concurrency bounds are exact and snapshot is deeply detached',async()=>{
 const f=fixture(),wait=Array.from({length:9},deferred);const runs=wait.map(w=>f.d.run(()=>f.d.stage('s3_upload',()=>w.promise)));
 assert.equal(f.d.snapshot().active_requests,8);assert.equal(f.d.snapshot().skipped_requests,1);f.at(2000);f.tick();assert.equal(f.emitted.length,8);
 f.at(2100);wait.forEach(w=>w.resolve());await Promise.all(runs);let s=f.d.snapshot();assert.equal(s.records.length,8);assert.equal(s.omitted_records,8);assert.equal(s.active_requests,0);
 s.records[0].stage='SECRET';s.records[0].completed_stages.push({secret:1});assert.equal(f.d.snapshot().records[0].stage,'s3_upload');assert.equal(f.d.snapshot().records[0].completed_stages.length,0);
 await f.d.run(()=>Promise.resolve());assert.equal(f.d.snapshot().skipped_requests,2);
});
test('completed phase summaries stay bounded and never inspect successful results',async()=>{
 const f=fixture(),w=deferred(),value={toJSON(){throw Error('SECRET')}};
 const r=f.d.run(async()=>{for(let i=0;i<12;i++)await f.d.stage('s3_upload',()=>Promise.resolve(value));await f.d.stage('queue_add',()=>Promise.resolve(value));return w.promise});
 for(let i=0;i<40;i++)await Promise.resolve();f.at(2000);f.tick();const row=f.emitted[0];assert.equal(row.completed_stages.length,2);assert.equal(row.completed_stages[0].calls,12);w.resolve(value);assert.equal(await r,value);
});
test('undefined rejection remains undefined and is attributed to other',async()=>{
 const f=fixture();let caught=false;try{await f.d.run(()=>Promise.reject(undefined))}catch(e){caught=true;assert.equal(e,undefined)}assert.ok(caught);assert.equal(f.emitted[0].stage,'other');
});
test('timer allocation failure falls back to untouched business work',()=>{
 const f=fixture({schedule:()=>{throw Error('timer unavailable')}}),p=Promise.resolve(3);
 assert.equal(f.d.run(()=>p),p);assert.equal(f.d.snapshot().active_requests,0);assert.equal(f.d.snapshot().skipped_requests,1);
});
test('after overlapping same-phase operations, remaining phase is conservative until all settle',async()=>{
 const f=fixture(),a=deferred(),b=deferred();const r=f.d.run(()=>Promise.all([f.d.stage('s3_upload',()=>a.promise),f.d.stage('s3_upload',()=>b.promise)]));a.resolve();await Promise.resolve();f.at(2000);f.tick();assert.equal(f.emitted[0].stage,'other');b.resolve();await r;
});
test('native observation timer emits pending without cancelling a slow operation',async()=>{
 const emitted=[],d=createOtelStageDiagnostics({enabled:true,emit:s=>emitted.push(JSON.parse(s))});
 const value=await d.run(()=>d.stage('s3_upload',()=>new Promise(resolve=>setTimeout(()=>resolve('done'),2050))));
 assert.equal(value,'done');assert.deepEqual(emitted.map(x=>x.event),['pending','complete']);assert.equal(emitted[0].stage,'s3_upload');assert.equal(d.snapshot().active_requests,0);
});
test('native Promise own then getter cannot introduce a run failure',async()=>{
 const f=fixture(),p=Promise.resolve('ok');Object.defineProperty(p,'then',{get(){throw Error('SECRET getter')}});
 assert.equal(await p,'ok');assert.equal(await f.d.run(()=>p),'ok');assert.equal(f.d.snapshot().active_requests,0);assert.equal(f.emitted.length,0);
});
test('native Promise own then getter cannot introduce a stage failure',async()=>{
 const f=fixture(),p=Promise.resolve('ok');Object.defineProperty(p,'then',{get(){throw Error('SECRET getter')}});
 assert.equal(await f.d.run(()=>f.d.stage('s3_upload',()=>p)),'ok');assert.equal(f.d.snapshot().active_requests,0);assert.equal(f.emitted.length,0);
});
test('skipped nested request cannot inherit and contaminate parent stages',async()=>{
 const f=fixture(),blocks=Array.from({length:7},deferred),nested=deferred();
 const runs=blocks.map(w=>f.d.run(()=>w.promise));const parent=f.d.run(()=>f.d.run(()=>f.d.stage('queue_add',()=>nested.promise)));
 f.at(2000);f.tick();try{assert.equal(f.d.snapshot().skipped_requests,1);assert.equal(f.emitted.find(x=>x.request_sequence===8).stage,'other')}
 finally{blocks.forEach(w=>w.resolve());nested.resolve();await Promise.all([...runs,parent])}
});
