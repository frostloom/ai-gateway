'use strict';
const {AsyncLocalStorage}=require('node:async_hooks');
const {performance}=require('node:perf_hooks');
const {isPromise}=require('node:util').types;
const promiseThen=Function.call.bind(Promise.prototype.then);

const THRESHOLD_MS=2000, RECORD_LIMIT=8, CONCURRENT_LIMIT=8;
const PHASES=['s3_upload','queue_add'];

// Deliberately accepts no request, response, payload or error-serialization input.
function createOtelStageDiagnostics(options={}){
 const enabled=options.enabled===true;
 const emit=options.emit??(line=>console.warn(line));
 const now=options.now??(()=>performance.now());
 const schedule=options.schedule??setTimeout, cancel=options.cancel??clearTimeout;
 const local=enabled?new AsyncLocalStorage():null;
 const records=[];let sequence=0,active=0,omitted=0,skipped=0;
 const increment=n=>Math.min(n+1,Number.MAX_SAFE_INTEGER);
 const ms=value=>Math.max(0,Math.round(value*1000)/1000);
 function copyRecord(row){return {...row,completed_stages:row.completed_stages.map(x=>({...x}))}}
 function write(ctx,event,error){
  if(records.length>=RECORD_LIMIT){omitted=increment(omitted);return}
  const running=PHASES.filter(p=>ctx.running[p]?.count>0).map(p=>({stage:p,...ctx.running[p]}));
  const single=running.length===1&&running[0].count===1&&!ctx.overlapped;
  const phase=event==='failed'?(ctx.phaseError&&ctx.phaseError.error===error?ctx.phaseError.stage:'other'):
   (single?running[0].stage:'other');
  const row={schema:'otel-receiver-stages.v1',request_sequence:ctx.sequence,event,stage:phase,
   elapsed_ms:ms(now()-ctx.start),completed_stages:PHASES.filter(p=>ctx.completed[p]).map(p=>({stage:p,...ctx.completed[p]}))};
  if(event==='pending'&&single)row.stage_elapsed_ms=ms(now()-running[0].start);
  records.push(row);
  try{emit(JSON.stringify(row))}catch{/* Diagnostic logging must not affect ingestion. */}
 }
 function run(fn){
  if(!enabled)return fn();
  if(active>=CONCURRENT_LIMIT||records.length>=RECORD_LIMIT){skipped=increment(skipped);return local.run(undefined,fn)}
  sequence=increment(sequence);active++;
  const ctx={sequence,start:now(),closed:false,running:{},overlapped:false,completed:{},phaseError:null};
  let timer;
  const finish=(ok,error)=>{
   if(ctx.closed)return;ctx.closed=true;active--;try{cancel(timer)}catch{/* Closed context makes a late callback inert. */}
   if(!ok||now()-ctx.start>=THRESHOLD_MS)write(ctx,ok?'complete':'failed',error);
  };
  return local.run(ctx,()=>{
   try{timer=schedule(()=>{if(!ctx.closed)write(ctx,'pending')},THRESHOLD_MS);timer?.unref?.()}catch{ctx.closed=true;active--;skipped=increment(skipped);return fn()}
   let result;
   try{result=fn()}catch(error){finish(false,error);throw error}
   // Native promises only: do not read arbitrary business-result `then` getters.
   if(!isPromise(result)){finish(true);return result}
   try{return promiseThen(result,value=>{finish(true);return value},error=>{finish(false,error);throw error})}
   catch(error){finish(false,error);throw error}
  });
 }
 function stage(name,fn){
  const ctx=local?.getStore();
  if(!ctx||ctx.closed||!PHASES.includes(name))return fn();
  const start=now();const phase=ctx.running[name]??{count:0,start};
  if(phase.count===0)phase.start=start;phase.count++;ctx.running[name]=phase;
  if(PHASES.reduce((n,p)=>n+(ctx.running[p]?.count??0),0)>1)ctx.overlapped=true;
  const finish=(ok,error)=>{
   phase.count--;
   if(PHASES.every(p=>!ctx.running[p]?.count))ctx.overlapped=false;
   if(ctx.closed)return;
   const previous=ctx.completed[name];
   ctx.completed[name]={elapsed_ms:ms((previous?.elapsed_ms??0)+now()-start),calls:increment(previous?.calls??0),outcome:!ok||previous?.outcome==='error'?'error':'ok'};
   if(!ok)ctx.phaseError={error,stage:name};
  };
  let result;
  try{result=fn()}catch(error){finish(false,error);throw error}
  if(!isPromise(result)){finish(true);return result}
  try{return promiseThen(result,value=>{finish(true);return value},error=>{finish(false,error);throw error})}
   catch(error){finish(false,error);throw error}
 }
 function snapshot(){return {schema:'otel-receiver-stages-snapshot.v1',enabled,records:records.map(copyRecord),omitted_records:omitted,skipped_requests:skipped,active_requests:active}}
 return {run,stage,snapshot};
}
module.exports={createOtelStageDiagnostics};
