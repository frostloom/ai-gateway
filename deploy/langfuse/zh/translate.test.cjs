const {test} = require('node:test');
const assert = require('node:assert/strict');
const {translate} = require('./translate.cjs');
const words = {Traces:'追踪',Input:'输入',Output:'输出',Name:'名称',Save:'保存',Tracing:'追踪',Scores:'评分'};

test('translate only literal UI props, preserving dynamic values and enum values', () => {
  const source = 'const x=(0,r.jsx)(Panel,{title:"Traces",placeholder:"Name",value:"Input",children:answer});';
  const result = translate(source, words);
  assert.match(result.source, /title:"追踪"/);
  assert.match(result.source, /placeholder:"名称"/);
  assert.match(result.source, /value:"Input",children:answer/);
});
test('translate static mixed children but preserve expressions and business text', () => {
  const source = 'r.jsxs("div",{children:["Input",answer,r.jsx("button",{children:"Save"})]});const data={input:"Input",output:"Output",label:"Scores",children:"Traces"};';
  const result = translate(source, words);
  assert.match(result.source, /children:\["输入",answer/);
  assert.match(result.source, /children:"保存"/);
  assert.ok(result.source.endsWith('const data={input:"Input",output:"Output",label:"Scores",children:"Traces"};'));
});
test('code and pre examples are never translated', () => {
  const source = 'r.jsx("code",{children:"Input"});r.jsx("pre",{children:r.jsx("span",{children:"Output"})});';
  assert.equal(translate(source, words).source, source);
});
test('navigation display titles and table headers preserve route, IDs and filter labels', () => {
  const source = 'const nav={title:"Tracing",href:"/project/[projectId]/traces",section:"main"};const col={accessorKey:"input",id:"input",header:"Input"};const filter={label:"Input",column:"Input"};';
  const result = translate(source, words).source;
  assert.match(result, /title:"追踪",href:"\/project\/\[projectId\]\/traces"/);
  assert.match(result, /accessorKey:"input",id:"input",header:"输入"/);
  assert.ok(result.endsWith('const filter={label:"Input",column:"Input"};'));
});
test('page header title supports nested headerProps without touching payload', () => {
  const result = translate('r.jsx(Page,{headerProps:{title:"Scores"},data:{title:"Scores"}});', words).source;
  assert.equal(result, 'r.jsx(Page,{headerProps:{title:"评分"},data:{title:"Scores"}});');
});
test('idempotent and no substring translation', () => {
  const source = 'r.jsx("div",{children:"Input is business content"});r.jsx("button",{children:"Save"});';
  const result = translate(source, words).source;
  assert.match(result, /children:"Input is business content"/);
  assert.equal(translate(result, words).source, result);
});
test('string literals and comments that resemble source code are unchanged', () => {
  const source = 'const example=\'children:"Input"\';/*r.jsx("div",{children:"Input"})*/';
  assert.equal(translate(source, words).source, source);
});
test('reject malformed program instead of producing broken bundle', () => {
  assert.throws(() => translate('r.jsx("button", {children:', words));
});
test('component labels and header-derived column IDs retain original semantics', () => {
  const source = 'r.jsx(Selector,{label:"Input",value:"Input"});const col={accessorFn:r=>r.input,header:"Input"};';
  assert.equal(translate(source, words).source, source);
});
test('translate dynamic header at rendering boundary, preserving its original definition', () => {
  const source = 'const definition={header:"Input"};r.jsx("span",{children:flexRender(e.column.columnDef.header,e.getContext())});';
  const result=translate(source,words).source;
  assert.ok(result.startsWith('const definition={header:"Input"};'));
  assert.notEqual(result,source);
  assert.ok(result.includes('"输入"'));
});
test('known input/output display panel translates its label but never its value', () => {
  const source='r.jsx(IO,{label:"Input",value:e.input,fullLength:e.inputLength,truncated:e.inputTruncated});';
  assert.equal(translate(source,words).source,'r.jsx(IO,{label:"输入",value:e.input,fullLength:e.inputLength,truncated:e.inputTruncated});');
});
test('dynamic display lookup preserves unknown/prototype names and functional headers', () => {
  const source='r.jsx("span",{children:flexRender(e.column.columnDef.header,e.getContext())})';
  const translated=translate(source,words).source;
  const vm=require('node:vm');
  const render=value=>vm.runInNewContext(translated,{r:{jsx:(tag,props)=>props.children},flexRender:x=>x,
    e:{column:{columnDef:{header:value}},getContext:()=>({})}});
  assert.equal(render('Input'),'输入');
  assert.equal(render('constructor'),'constructor');
  assert.equal(render('__proto__'),'__proto__');
  const component=()=>null;
  assert.equal(render(component),component);
});
test('non-render calls and different header/context receivers stay unchanged', () => {
  for(const source of ['submit(e.column.columnDef.header,e.getContext())',
    'flexRender(e.column.columnDef.header,other.getContext())']) assert.equal(translate(source,words).source,source);
});
