import assert from 'node:assert/strict'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'
import { build } from 'vite'
import vue from '@vitejs/plugin-vue'
import { JSDOM } from 'jsdom'
const root = resolve(import.meta.dirname, '..')
const dir = await mkdtemp(resolve(root, 'node_modules/.plans-test-'))
const dom = new JSDOM('<div id="app"></div>', { url: 'http://localhost/' })
const w = dom.window
for (const key of ['window', 'document', 'HTMLElement', 'Element', 'Node', 'SVGElement', 'Event', 'CustomEvent', 'localStorage']) globalThis[key] = key === 'window' ? w : w[key]
const errors = []
const previousError = console.error
console.error = (...args) => errors.push(args.join(' '))
const tick = () => new Promise(resolve => setTimeout(resolve, 35))
const models = [{ id: 37, model_id: 'deepseek-flash', name: 'DeepSeek Flash', status: 0, vendor: 'DeepSeek' }]
const plan = { ID: 1, Name: 'DeepSeek 标准月卡', PlanType: 'recurring', PriceMoney: 100, ValidityDays: 30, RefreshHours: 5, WindowQuotaCent: 2000, CycleQuotaCent: 20000, AllowedModels: '["deepseek-flash"]', TierQuota: '{}', Status: 0 }
let profileFails = false
let captured = null
globalThis.fetch = async (url, options = {}) => {
  const path = String(url).split('?')[0]
  if (path === '/admin/plans' && options.method === 'POST') captured = JSON.parse(options.body)
  const fails = profileFails && path === '/portal/me'
  const data = path.endsWith('/models') ? { items: models } : path === '/portal/me' ? { subscription: null, plans: null, allowance: null, money_allowance: null } : options.method === 'POST' ? { ID: 2, ...captured } : { items: [plan] }
  return { ok: !fails, status: fails ? 503 : 200, json: async () => fails ? { error: 'profile unavailable' } : data }
}
try {
  const entry = resolve(dir, 'entry.js')
  await writeFile(entry, `import {createApp} from 'vue';import Plans from ${JSON.stringify(resolve(root, 'src/portal/views/PlansView.vue'))};import Manage from ${JSON.stringify(resolve(root, 'src/dashboard/views/PlanManageView.vue'))};export {planModelScope} from ${JSON.stringify(resolve(root, 'src/shared/planModels.ts'))};export function mount(admin=false){return createApp(admin?Manage:Plans).mount('#app')}`)
  await build({ configFile:false, root, plugins:[vue()], logLevel:'silent', build:{outDir:resolve(dir,'out'),lib:{entry,formats:['es'],fileName:'test'},minify:false} })
  const { mount, planModelScope } = await import(pathToFileURL(resolve(dir,'out/test.js')).href)
  assert.equal(planModelScope(undefined).kind, 'all')
  for (const invalid of ['null','{}','["*"]','["deepseek-flash","deepseek-flash"]']) assert.equal(planModelScope(invalid).kind,'invalid',invalid)
  let vm = mount(); await tick()
  assert.match(w.document.querySelector('.plan-card').textContent, /200\.00/)
  assert.match(w.document.querySelector('.model-chips').textContent, /DeepSeek Flash/)
  assert.equal(w.document.querySelector('.plan-card footer button').disabled, true)
  assert.match(w.document.body.textContent, /其他模型使用账户余额/)
  vm.$.appContext.app.unmount()
  profileFails = true; vm = mount(); await tick()
  assert.match(w.document.body.textContent, /当前月卡信息加载失败/)
  assert.ok(w.document.querySelector('.plan-card'), 'profile failure does not hide public plan catalog')
  vm.$.appContext.app.unmount(); profileFails = false
  vm = mount(true); await tick()
  ;[...w.document.querySelectorAll('button')].find(b => b.textContent.includes('新增套餐')).click(); await tick()
  const form = w.document.querySelector('form')
  const name = form.querySelector('input'); name.value = 'DeepSeek 验证月卡'; name.dispatchEvent(new w.Event('input',{bubbles:true})); await tick()
  assert.equal(form.querySelector('[type="submit"]').disabled,true,'cannot create accidental all-model plan')
  const selected = form.querySelector('.model-options input'); selected.checked = true; selected.dispatchEvent(new w.Event('change',{bubbles:true})); await tick()
  assert.equal(form.querySelector('[type="submit"]').disabled,false)
  form.dispatchEvent(new w.Event('submit',{bubbles:true,cancelable:true})); await tick(); await tick()
  assert.equal(captured.AllowedModels,'["deepseek-flash"]')
  assert.equal(captured.PriceMoney,100); assert.equal(captured.WindowQuotaCent,2000); assert.equal(captured.CycleQuotaCent,20000)
  vm.$.appContext.app.unmount()
  assert.equal(errors.length,0,errors.join('\n'))
  console.log('plans regression: monetary terms, explicit model scope, closed activation, partial profile error, validated admin save passed')
} finally { console.error=previousError; w.close(); await rm(dir,{recursive:true,force:true}) }
