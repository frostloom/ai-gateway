const {test}=require('node:test');const assert=require('node:assert/strict');const fs=require('node:fs');const os=require('node:os');const path=require('node:path');const {installAssetPrefix}=require('./assets.cjs');
test('translated assets have new URLs across HTML, Next config and lazy Turbopack loading',()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'lf-zh-assets-'));
 try{
 fs.mkdirSync(path.join(root,'.next/static/chunks'),{recursive:true});fs.mkdirSync(path.join(root,'.next/server/pages'),{recursive:true});fs.mkdirSync(path.join(root,'public'));
 fs.writeFileSync(path.join(root,'.next/static/chunks/turbopack-test.js'),'const base=typeof TURBOPACK_CHUNK_BASE_PATH==="string"?TURBOPACK_CHUNK_BASE_PATH:"/_next/";');
 fs.writeFileSync(path.join(root,'.next/static/chunks/page.js'),'const title="追踪";');
 fs.writeFileSync(path.join(root,'.next/static/chunks/next-client.js'),'let n=i.assetPrefix||"";self.__next_set_public_path__(`${n}/_next/`);');
 fs.writeFileSync(path.join(root,'server.js'),'const nextConfig={"assetPrefix":"","basePath":""};');
 fs.writeFileSync(path.join(root,'.next/required-server-files.json'),JSON.stringify({config:{assetPrefix:'',basePath:''}}));
 fs.writeFileSync(path.join(root,'.next/server/pages/index.html'),'<script src="/_next/static/chunks/page.js"></script>');
 const prefix=installAssetPrefix(root);
 assert.match(prefix,/^\/local-zh-[a-f0-9]{12}$/);
 assert.ok(fs.readFileSync(path.join(root,'server.js'),'utf8').includes('"assetPrefix":"'+prefix+'"'));
 assert.equal(JSON.parse(fs.readFileSync(path.join(root,'.next/required-server-files.json'))).config.assetPrefix,prefix);
 const asset=path.join(root,'public',prefix,'_next/static/chunks/turbopack-test.js');
 assert.ok(fs.readFileSync(asset,'utf8').includes(prefix+'/_next/'));
 assert.ok(fs.readFileSync(path.join(root,'.next/server/pages/index.html'),'utf8').includes(prefix+'/_next/static/'));
 assert.equal(fs.readFileSync(path.join(root,'public',prefix,'_next/static/chunks/page.js'),'utf8'),'const title="追踪";');
 assert.ok(fs.readFileSync(path.join(root,'public',prefix,'_next/static/chunks/next-client.js'),'utf8').includes('i.assetPrefix||"'+prefix+'"'));
 assert.throws(()=>installAssetPrefix(root),/already|prefix/i);
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('unsupported loader fails before changing the standalone server',()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'lf-zh-assets-'));try{
 fs.mkdirSync(path.join(root,'.next/static/chunks'),{recursive:true});fs.writeFileSync(path.join(root,'.next/static/chunks/turbopack-test.js'),'unknownLoader()');fs.writeFileSync(path.join(root,'server.js'),'const nextConfig={"assetPrefix":""};');
 assert.throws(()=>installAssetPrefix(root),/loader/i);assert.equal(fs.readFileSync(path.join(root,'server.js'),'utf8'),'const nextConfig={"assetPrefix":""};');
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
