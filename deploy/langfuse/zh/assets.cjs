// Give translated client code its own URL, including lazy Turbopack chunks.
// Upstream URLs are immutable for a year and must never serve changed bytes.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');

function files(root) {
  if (!fs.existsSync(root)) return [];
  return fs.readdirSync(root,{withFileTypes:true}).flatMap(e => {
    const file=path.join(root,e.name);
    return e.isDirectory()?files(file):e.isFile()?[file]:[];
  });
}
function installAssetPrefix(root) {
  const staticRoot=path.join(root,'.next/static');
  const serverFile=path.join(root,'server.js');
  const server=fs.readFileSync(serverFile,'utf8');
  if (!server.includes('"assetPrefix":""')) throw Error('Existing asset prefix or already installed translation');
  const loaders=files(staticRoot).filter(f=>path.basename(f).startsWith('turbopack-')&&f.endsWith('.js'));
  const marker='TURBOPACK_CHUNK_BASE_PATH:"/_next/"';
  if (loaders.length!==1 || !fs.readFileSync(loaders[0],'utf8').includes(marker)) throw Error('Unsupported Turbopack loader');
  // Langfuse's document omits __NEXT_DATA__.assetPrefix. Next's page prefetcher
  // independently defaults to the upstream URL even with a custom server prefix.
  const clientMarker='i.assetPrefix||"";self.__next_set_public_path__';
  const clients=files(staticRoot).filter(f=>f.endsWith('.js')&&fs.readFileSync(f,'utf8').includes(clientMarker));
  if (clients.length!==1) throw Error('Unsupported Next client loader');
  const hash=crypto.createHash('sha256').update(fs.readFileSync(__filename));
  for (const file of files(staticRoot).sort()) {
    hash.update(path.relative(staticRoot,file).split(path.sep).join('/'));
    hash.update(fs.readFileSync(file));
  }
  const prefix='/local-zh-'+hash.digest('hex').slice(0,12);
  const loader=fs.readFileSync(loaders[0],'utf8').replace(marker,`TURBOPACK_CHUNK_BASE_PATH:"${prefix}/_next/"`);
  fs.writeFileSync(loaders[0],loader);
  fs.writeFileSync(clients[0],fs.readFileSync(clients[0],'utf8').replace(clientMarker,`i.assetPrefix||"${prefix}";self.__next_set_public_path__`));
  fs.writeFileSync(serverFile,server.replace('"assetPrefix":""',`"assetPrefix":"${prefix}"`));
  const configFile=path.join(root,'.next/required-server-files.json');
  const config=JSON.parse(fs.readFileSync(configFile,'utf8'));
  config.config.assetPrefix=prefix;
  fs.writeFileSync(configFile,JSON.stringify(config));
  // Pre-rendered pages already contain absolute resource links.
  for (const file of files(path.join(root,'.next/server')).filter(f=>f.endsWith('.html'))) {
    const raw=fs.readFileSync(file,'utf8');
    fs.writeFileSync(file,raw.replaceAll('/_next/static/',prefix+'/_next/static/'));
  }
  fs.cpSync(staticRoot,path.join(root,'public',prefix,'_next/static'),{recursive:true});
  return prefix;
}
module.exports={installAssetPrefix};
