'use strict';
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto'),vm=require('node:vm');
const layout=require('./pinned-layout.json');
const RUNTIME='/opt/ai-gateway-otel-stages/receiver.cjs';
const META='.otel-stage-diagnostics.json',BACKUP='.otel-stage-diagnostics-backup';
const sha=data=>crypto.createHash('sha256').update(data).digest('hex');
function once(source,needle,replacement){const i=source.indexOf(needle);if(i<0||source.indexOf(needle,i+needle.length)>=0)throw Error('Unique source anchor required');return source.slice(0,i)+replacement+source.slice(i+needle.length)}
function transform(role,source){
 if(source.includes(RUNTIME))throw Error('Already contains diagnostic adapter');
 const runtime=`require(${JSON.stringify(RUNTIME)})`;
 let result;
 if(role==='handler'){
  const begin='fn:async({req:t,res:r,auth:o})=>{let n,s;',end='return"http"===v.kind?(r.status(v.status),v.body):v.body??{}';
  result=once(source,begin,'fn:async({req:t,res:r,auth:o})=>{return '+runtime+'.run(async()=>{let n,s;');
  result=once(result,end,end+'});');
 }else if(role==='publisher'){
  const upload='(0,i.getS3EventStorageClient)(l.env.LANGFUSE_S3_EVENT_UPLOAD_BUCKET).uploadJson(t,e)';
  result=once(source,'await '+upload+';','await '+runtime+'.stage("s3_upload",()=>'+upload+');');
  result=once(result,'return r?r.add(i.QueueJobs.OtelIngestionJob,','return r?'+runtime+'.stage("queue_add",()=>r.add(i.QueueJobs.OtelIngestionJob,');
  const end='):Promise.reject("Failed to instantiate otel ingestion queue")';result=once(result,end,')'+end);
 }else throw Error('Unknown adapter role');
 new vm.Script(result,{filename:role+'.js'});return result;
}
function regular(root,relative){
 if(path.isAbsolute(relative)||relative.split(/[\\/]/).includes('..'))throw Error('Unsafe relative path');
 let current=root;for(const part of relative.split('/')){current=path.join(current,part);const stat=fs.lstatSync(current);if(stat.isSymbolicLink())throw Error('Symlink refused')}
 const stat=fs.statSync(current);if(!stat.isFile())throw Error('Regular source file required');return current;
}
function validate(root){
 const stat=fs.lstatSync(root);if(stat.isSymbolicLink()||!stat.isDirectory())throw Error('Regular root directory required');
 if(layout.schema!=='langfuse-otel-stage-layout.v1'||layout.version!=='4.58.0'||layout.files.length!==2||new Set(layout.files.map(f=>f.role)).size!==2)throw Error('Unsupported pinned layout');
 for(const f of layout.files)if(!['handler','publisher'].includes(f.role)||!/^\.next\/server\/chunks\/[^/]+\.js$/.test(f.path)||!/^[a-f0-9]{64}$/.test(f.sha256))throw Error('Unsupported pinned layout');
 if(JSON.parse(fs.readFileSync(regular(root,'package.json'),'utf8')).version!==layout.version)throw Error('Pinned Langfuse4.58.0 required');
}
function atomic(file,data,mode,owner){
 const temp=path.join(path.dirname(file),'.otel-'+crypto.randomUUID()+'.tmp');
 try{
  const fd=fs.openSync(temp,'wx',mode);
  try{fs.writeFileSync(fd,data);const current=fs.fstatSync(fd);if(owner&&(current.uid!==owner.uid||current.gid!==owner.gid))fs.fchownSync(fd,owner.uid,owner.gid);fs.fchmodSync(fd,mode)}finally{fs.closeSync(fd)}
  fs.renameSync(temp,file);
 }finally{if(fs.existsSync(temp))fs.unlinkSync(temp)}
}
function checkBackup(root,exact){
 const folder=path.join(root,BACKUP);let stat;
 try{stat=fs.lstatSync(folder)}catch(error){if(error.code==='ENOENT'&&!exact)return [];throw error}
 if(stat.isSymbolicLink()||!stat.isDirectory())throw Error('Regular backup directory required');
 const names=fs.readdirSync(folder).sort(),expected=['handler.js','publisher.js'];
 if(names.some(n=>!expected.includes(n))||(exact&&names.length!==2))throw Error('Backup directory contents mismatch');
 return names;
}
function removeOwn(root){
 const names=checkBackup(root,false);
 for(const name of names)fs.unlinkSync(path.join(root,BACKUP,name));
 if(fs.existsSync(path.join(root,BACKUP)))fs.rmdirSync(path.join(root,BACKUP));
 fs.unlinkSync(path.join(root,META));
}
function install(root){
 root=path.resolve(root);validate(root);
 if(fs.existsSync(path.join(root,META))||fs.existsSync(path.join(root,BACKUP)))throw Error('Existing manifest/backup; adapter already installed or recovery required');
 const records=layout.files.map(spec=>{
  const file=regular(root,spec.path),before=fs.readFileSync(file);if(sha(before)!==spec.sha256)throw Error('Pinned source hash mismatch');
  const stat=fs.statSync(file);return {...spec,file,before,after:Buffer.from(transform(spec.role,before.toString('utf8'))),mode:stat.mode&0o777,owner:{uid:stat.uid,gid:stat.gid}};
 });
 const moduleHashes=Object.fromEntries(['otel-stages.cjs','receiver.cjs'].map(f=>[f,sha(fs.readFileSync(regular(__dirname,f)))]));
 const manifest={schema:'langfuse-otel-stage-install.v1',version:layout.version,status:'prepared',enabled_by_default:false,runtime_path:RUNTIME,module_sha256:moduleHashes,files:records.map(r=>({role:r.role,path:r.path,before_sha256:sha(r.before),after_sha256:sha(r.after),mode:r.mode,uid:r.owner.uid,gid:r.owner.gid}))};
 const changed=[];let owned=false;
 try{
  fs.mkdirSync(path.join(root,BACKUP));owned=true;
  for(const r of records)fs.writeFileSync(path.join(root,BACKUP,r.role+'.js'),r.before,{flag:'wx',mode:0o600});
  atomic(path.join(root,META),JSON.stringify(manifest,null,2)+'\n',0o600);
  for(const r of records){atomic(r.file,r.after,r.mode,r.owner);changed.push(r)}
  manifest.status='installed';atomic(path.join(root,META),JSON.stringify(manifest,null,2)+'\n',0o600);return manifest;
 }catch(error){
  const failures=[];for(const r of changed.reverse())try{atomic(r.file,r.before,r.mode,r.owner)}catch(e){failures.push(e)}
  if(failures.length)throw new AggregateError([error,...failures],'Install failed; recovery backup retained');
  if(owned){
   manifest.status='restored';
   try{atomic(path.join(root,META),JSON.stringify(manifest,null,2)+'\n',0o600);removeOwn(root)}
   catch(cleanupError){throw new AggregateError([error,cleanupError],'Original sources restored; cleanup pending')}
  }
  throw error;
 }
}
function restore(root){
 root=path.resolve(root);validate(root);const meta=regular(root,META),manifest=JSON.parse(fs.readFileSync(meta,'utf8'));
 if(manifest.schema!=='langfuse-otel-stage-install.v1'||manifest.version!==layout.version||!['prepared','installed','restored'].includes(manifest.status)||manifest.files?.length!==2)throw Error('Manifest mismatch');
 const cleaned=manifest.status==='restored',remaining=checkBackup(root,!cleaned);
 const records=layout.files.map(spec=>{
  const file=regular(root,spec.path),current=fs.readFileSync(file);
  const before=cleaned?current:fs.readFileSync(regular(root,BACKUP+'/'+spec.role+'.js'));
  if(sha(before)!==spec.sha256)throw Error('Backup/source hash mismatch');
  if(cleaned&&remaining.includes(spec.role+'.js')&&sha(fs.readFileSync(regular(root,BACKUP+'/'+spec.role+'.js')))!==spec.sha256)throw Error('Backup hash mismatch');
  const after=Buffer.from(transform(spec.role,before.toString('utf8'))),m=manifest.files.find(f=>f.role===spec.role);
  if(!m||m.path!==spec.path||m.before_sha256!==sha(before)||m.after_sha256!==sha(after)||m.mode!==(fs.statSync(file).mode&0o777)||m.uid!==fs.statSync(file).uid||m.gid!==fs.statSync(file).gid)throw Error('Backup/manifest hash mismatch');
  if(!cleaned&&sha(current)!==sha(after)&&(manifest.status!=='prepared'||sha(current)!==sha(before)))throw Error('Patched file changed; refusing restore');
  return {file,before,current,mode:m.mode,owner:{uid:m.uid,gid:m.gid}};
 });
 if(cleaned){removeOwn(root);return {restored:true,files:records.length,cleanup_resumed:true}}
 const originalMeta=fs.readFileSync(meta);manifest.status='prepared';atomic(meta,JSON.stringify(manifest,null,2)+'\n',0o600);const changed=[];
 try{for(const r of records){atomic(r.file,r.before,r.mode,r.owner);changed.push(r)}}catch(error){
  const failures=[];for(const r of changed.reverse())try{atomic(r.file,r.current,r.mode,r.owner)}catch(e){failures.push(e)}
  if(!failures.length)atomic(meta,originalMeta,0o600);
  if(failures.length)throw new AggregateError([error,...failures],'Restore failed; recovery backup retained');throw error;
 }
 // Commit restoration before deleting any backup. Cleanup may then be resumed.
 manifest.status='restored';atomic(meta,JSON.stringify(manifest,null,2)+'\n',0o600);
 removeOwn(root);return {restored:true,files:records.length};
}
module.exports={transform,install,restore};
if(require.main===module){const [action,root,...extra]=process.argv.slice(2);if(!['install','restore'].includes(action)||!root||extra.length)throw Error('Usage: install.cjs install|restore ROOT');const result=action==='install'?install(root):restore(root);console.log(JSON.stringify({status:result.status??'restored',files:result.files?.length??result.files,enabled_by_default:false}))}
