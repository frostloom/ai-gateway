// Local UI-only translation for the pinned official Langfuse image.
// Edit literal display positions in the syntax tree; never globally replace text.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const parser = require('/app/node_modules/.pnpm/meriyah@6.1.4/node_modules/meriyah');

function parse(source) {
  try { return parser.parseScript(source, {ranges: true, next: true}); }
  catch { return parser.parseModule(source, {ranges: true, next: true}); }
}
const keyName = p => p.key?.name ?? p.key?.value;
function translate(source, dictionary) {
  const edits = new Map();
  const put = value => {
    if (value?.type !== 'Literal' || typeof value.value !== 'string' || !Object.hasOwn(dictionary, value.value)) return;
    edits.set(value.start, {end: value.end, text: JSON.stringify(dictionary[value.value]), original: value.value});
  };
  function displayProps(object) {
    if (object?.type !== 'ObjectExpression') return;
    const fields=object.properties.filter(p=>p.type==='Property'&&!p.computed);
    const get=key=>fields.find(p=>keyName(p)===key)?.value;
    const ioPanel=get('value') && (get('fullLength') && get('truncated') || ['input','output'].includes(get('schemaType')?.value));
    for (const p of object.properties) {
      if (p.type !== 'Property' || p.computed) continue;
      const key = keyName(p);
      if (['title','placeholder','aria-label','tooltip'].includes(key)) put(p.value);
      if (key==='label' && ioPanel) put(p.value);
      if (key === 'children') {
        put(p.value);
        if (p.value.type === 'ArrayExpression') p.value.elements.forEach(put);
      }
      if (key === 'headerProps') displayProps(p.value);
    }
  }
  function walk(node) {
    if (!node || typeof node !== 'object') return;
    if (node.type === 'CallExpression') {
      let callee = node.callee;
      if (callee.type === 'SequenceExpression') callee = callee.expressions.at(-1);
      const [header, context] = node.arguments;
      // T is the pinned image's minified flexRender export, confirmed in its
      // shared table-rendering chunks. Never match arbitrary same-shape calls.
      const isFlexRender=callee.type==='Identifier'&&callee.name==='flexRender' ||
        callee.type==='MemberExpression'&&callee.property?.name==='T';
      if (header?.type === 'MemberExpression' && header.property?.name === 'header' &&
          header.object?.property?.name === 'columnDef' && context?.type === 'CallExpression' &&
          context.callee?.property?.name === 'getContext' && isFlexRender &&
          header.object.object?.property?.name==='column' &&
          source.slice(header.object.object.object.start,header.object.object.object.end)===
            source.slice(context.callee.object.start,context.callee.object.end)) {
        const original=source.slice(header.start,header.end);
        edits.set(header.start,{end:header.end,
          text:`((__lfZhValue)=>{const __lfZhWords=${JSON.stringify(dictionary)};return typeof __lfZhValue==="string"&&Object.hasOwn(__lfZhWords,__lfZhValue)?__lfZhWords[__lfZhValue]:__lfZhValue})(${original})`,
          original:'dynamic table header display'});
      }
      if (callee.type === 'MemberExpression' && ['jsx','jsxs','jsxDEV','createElement'].includes(callee.property?.name)) {
        if (['pre','code','script','style'].includes(node.arguments[0]?.value)) return;
        displayProps(node.arguments[1]);
      }
    }
    if (node.type === 'ObjectExpression') {
      const props = node.properties.filter(p => p.type === 'Property' && !p.computed);
      const get = key => props.find(p => keyName(p) === key)?.value;
      // Navigation config and table display headings; IDs and filter labels stay English.
      if (get('section') && get('href')?.type === 'Literal' && get('href').value.startsWith('/')) {
        put(get('title'));
      }
      // accessorFn-only columns derive their ID from header; leave those unchanged.
      if (get('accessorKey') || get('id')) put(get('header'));
    }
    for (const [key, value] of Object.entries(node)) {
      if (key === 'range' || key === 'loc') continue;
      if (Array.isArray(value)) value.forEach(walk);
      else if (value && typeof value === 'object') walk(value);
    }
  }
  walk(parse(source));
  let result = source;
  for (const [start, edit] of [...edits].sort((a,b) => b[0]-a[0])) result = result.slice(0,start)+edit.text+result.slice(edit.end);
  if (edits.size) parse(result);
  return {source: result, count: edits.size, terms: [...new Set([...edits.values()].map(x => x.original))]};
}

function install(root, dictionary) {
  const version = JSON.parse(fs.readFileSync(path.join(root,'package.json'),'utf8')).version;
  if (version !== '4.58.0') throw Error('Pinned Langfuse 4.58.0 required; review translation for new versions');
  const changed = [], terms = new Set();
  function walk(directory) {
    for (const entry of fs.readdirSync(directory,{withFileTypes:true})) {
      const file = path.join(directory,entry.name);
      if (entry.isDirectory()) { if (entry.name !== 'node_modules') walk(file); continue; }
      if (!entry.isFile() || !file.endsWith('.js')) continue;
      const raw = fs.readFileSync(file,'utf8');
      if (!/(children|placeholder|headerProps|accessorKey|section):/.test(raw)) continue;
      const result = translate(raw,dictionary);
      if (result.count) {
        fs.writeFileSync(file,result.source);
        result.terms.forEach(x => terms.add(x));
        changed.push({file:path.relative(root,file),replacements:result.count,
          before_sha256:crypto.createHash('sha256').update(raw).digest('hex'),
          after_sha256:crypto.createHash('sha256').update(result.source).digest('hex')});
      }
    }
  }
  for (const directory of ['.next/static/chunks','.next/server/chunks','.next/server/pages']) walk(path.join(root,directory));
  if (!changed.some(x=>x.file.startsWith('.next/static/')) || !changed.some(x=>x.file.startsWith('.next/server/'))) throw Error('Both client and server UI translations required');
  const manifest = {schema:'langfuse-local-zh.v1',version,changed_files:changed,translated_terms:[...terms].sort()};
  fs.writeFileSync(path.join(root,'public/local-zh-manifest.json'),JSON.stringify(manifest,null,2)+'\n');
  console.log(JSON.stringify({version,changed_files:changed.length,replacements:changed.reduce((n,x)=>n+x.replacements,0),translated_terms:terms.size}));
}
module.exports = {translate};
if (require.main === module) install('/app/web',JSON.parse(fs.readFileSync(path.join(__dirname,'zh-CN.json'),'utf8')));
