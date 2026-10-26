const fs=require("fs"),{performance}=require("perf_hooks"),{randomUUID}=require("crypto");
const id="otlp-stage-"+randomUUID(),prefix="diagnostic-"+id,s3prefix="diagnostic/"+id+"/";
process.env.REDIS_KEY_PREFIX=prefix;process.env.LANGFUSE_S3_EVENT_UPLOAD_PREFIX=s3prefix;process.env.TELEMETRY_ENABLED="false";
const out={schema:"otlp-isolated-publisher-stage.v1",isolation_prefix:prefix,s3_prefix:s3prefix,model_calls:0,jev_calls:0,old_trace_replay:false,events:[],started_at:new Date().toISOString()};
const began=performance.now();const event=(stage,ms,ok)=>out.events.push({stage,elapsed_ms:ms,ok});
let R,storage,queue,ownedKey;
async function main(){
 R=require("/app/web/.next/server/chunks/[turbopack]_runtime.js")("server/pages/api/public/otel/v1/traces.js");
 const entry=fs.readFileSync("/app/web/.next/server/pages/api/public/otel/v1/traces.js","utf8");for(const m of entry.matchAll(/R\.c\("([^"\n]+)"\)/g))R.c(m[1]);
 const Processor=R.m(728163).exports.OtelIngestionProcessor;const queueClass=R.m(672595).exports.OtelIngestionQueue;
 const shared=R.m(918207).exports;const env=R.m(447885).exports.env;
 if(env.REDIS_KEY_PREFIX!==prefix || env.LANGFUSE_S3_EVENT_UPLOAD_PREFIX!==s3prefix)throw Error("isolation env rejected");
 out.load_ms=performance.now()-began;
 storage=shared.getS3EventStorageClient(env.LANGFUSE_S3_EVENT_UPLOAD_BUCKET);
 if(!storage.client?.send)throw Error("S3 client expected");
 const send=storage.client.send.bind(storage.client);storage.client.config.maxAttempts=async()=>1;
 storage.client.send=async(cmd,options)=>{if(cmd.input.Key&&!cmd.input.Key.startsWith(s3prefix))throw Error("foreign S3 key refused");return send(cmd,{...options,abortSignal:AbortSignal.timeout(2000)})};
 const upload=storage.uploadJson.bind(storage);storage.uploadJson=async(key,body)=>{if(ownedKey || !key.startsWith(s3prefix))throw Error("unexpected upload");ownedKey=key;let t=performance.now();try{const r=await upload(key,body);event("s3_upload",performance.now()-t,true);return r}catch(e){event("s3_upload",performance.now()-t,false);throw e}};
 const get=queueClass.getInstance.bind(queueClass);queueClass.getInstance=(args)=>{let t=performance.now();queue=get(args);event("queue_construct",performance.now()-t,!!queue);if(!queue || queue.opts.prefix!==prefix)throw Error("foreign queue refused");const add=queue.add.bind(queue);let count=0;queue.add=async(...args)=>{if(++count!==1)throw Error("second job refused");let t=performance.now();try{const r=await Promise.race([add(...args),new Promise((_,reject)=>setTimeout(()=>reject(Error("stage timeout")),2000).unref())]);event("queue_add",performance.now()-t,true);return r}catch(e){event("queue_add",performance.now()-t,false);throw e}};return queue};
 const proc=new Processor({projectId:"synthetic-diagnostic",publicKey:"synthetic-no-auth",orgId:"synthetic-diagnostic",ingestionVersion:4});
 const now=BigInt(Date.now())*1000000n;const body=[{resource:{attributes:[]},scopeSpans:[{scope:{name:"diagnostic-zero-inference"},spans:[{traceId:randomUUID().replaceAll("-",""),spanId:randomUUID().replaceAll("-","").slice(0,16),name:"diagnostic.synthetic.publisher",startTimeUnixNano:now.toString(),endTimeUnixNano:(now+1000000n).toString(),attributes:[]}]}]}];
 let t=performance.now();await proc.publishToOtelIngestionQueue(body);out.publisher_ms=performance.now()-t;out.ok=true;
}
(async()=>{try{await main()}catch(e){out.ok=false;out.error_type=e.name;out.error_message_allowed=["isolation env rejected","S3 client expected","foreign S3 key refused","unexpected upload","foreign queue refused","second job refused","stage timeout"].includes(e.message)?e.message:undefined}finally{
 if(queue&&queue.opts.prefix===prefix){try{out.queue_counts=await queue.getJobCounts("waiting","active","completed","failed");await queue.obliterate({force:false});await queue.close();out.queue_cleanup_ok=true}catch(e){out.queue_cleanup_ok=false}}
 if(storage&&ownedKey&&ownedKey.startsWith(s3prefix)){try{const {DeleteObjectCommand}=require("/app/node_modules/.pnpm/@aws-sdk+client-s3@3.1074.0/node_modules/@aws-sdk/client-s3");await storage.client.send(new DeleteObjectCommand({Bucket:storage.bucketName,Key:ownedKey}));out.s3_cleanup_ok=true}catch(e){out.s3_cleanup_ok=false}}
 try{R?.m(293115).exports.disconnectAllRedisInstances()}catch(e){}
 out.finished_at=new Date().toISOString();console.log("DIAGNOSTIC_RESULT "+JSON.stringify(out));process.exit(out.ok&&out.queue_cleanup_ok&&out.s3_cleanup_ok?0:1)
}})();
