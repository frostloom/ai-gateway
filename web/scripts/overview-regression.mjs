/** Mount the actual overview components against empty/null and failed API responses. */
import assert from 'node:assert/strict'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'
import { build } from 'vite'
import vue from '@vitejs/plugin-vue'
import { JSDOM } from 'jsdom'

const root = resolve(import.meta.dirname, '..')
const dir = await mkdtemp(resolve(root, 'node_modules/.overview-test-'))
const dom = new JSDOM('<div id="app"></div>', { url: 'http://localhost/' })
const w = dom.window
for (const key of ['window', 'document', 'HTMLElement', 'Element', 'Node', 'SVGElement', 'Event', 'CustomEvent', 'localStorage']) globalThis[key] = key === 'window' ? w : w[key]
globalThis.ResizeObserver = class { observe() {} disconnect() {} }
globalThis.getComputedStyle = () => ({ getPropertyValue: () => '' })
globalThis.requestAnimationFrame = cb => setTimeout(cb, 0)
globalThis.cancelAnimationFrame = clearTimeout
const errors = []
const originalError = console.error
console.error = (...args) => errors.push(args.join(' '))
const tick = () => new Promise(resolve => setTimeout(resolve, 30))
let response = {}
let fail = false
let pending = null
globalThis.fetch = async url => {
  if (pending) await pending
  const data = String(url).includes('/overview') ? { tenants: null } : String(url).includes('/health') ? { services: null } : String(url).includes('/sales') ? { summary: { consumed_cent: 0, topup_cent: 0, settled_bills: 0, active_tenants: 0 }, by_day: null, by_model: null, by_vendor: null } : response
  return { ok: !fail, status: fail ? 503 : 200, json: async () => fail ? { error: '测试读取失败' } : data }
}
try {
  const entry = resolve(dir, 'entry.js')
  await writeFile(entry, `import {createApp} from 'vue'; import Home from ${JSON.stringify(resolve(root, 'src/portal/views/HomeView.vue'))}; import Overview from ${JSON.stringify(resolve(root, 'src/dashboard/views/OverviewView.vue'))}; export function mount(admin=false) {return createApp(admin ? Overview : Home).mount('#app')} `)
  await build({ configFile: false, root, plugins: [vue()], logLevel: 'silent', build: { outDir: resolve(dir, 'out'), lib: { entry, formats: ['es'], fileName: 'test' }, minify: false } })
  const { mount } = await import(pathToFileURL(resolve(dir, 'out/test.js')).href)
  response = { balance_cent: 12345, consumption: null, consumption_by_model: null, plans: null, allowance: null, money_allowance: null, subscription: null }
  let vm = mount()
  await tick()
  assert.match(w.document.body.textContent, /123\.45/)
  assert.match(w.document.body.textContent, /暂无使用记录/)
  assert.match(w.document.body.textContent, /暂无生效月卡/)
  assert.equal(errors.length, 0, errors.join('\n'))
  // Exact online shape: daily series is [], model aggregate is null.
  vm.$.appContext.app.unmount()
  response.consumption = []
  response.balance_cent = 0
  vm = mount(); await tick()
  assert.match(w.document.querySelector('.activity-balance').textContent, /¥0\.00/)
  assert.match(w.document.body.textContent, /暂无模型调用/)
  assert.equal(errors.length, 0, errors.join('\n'))
  response.balance_cent = 12345
  fail = true
  const refresh = [...w.document.querySelectorAll('button')].find(b => b.textContent.includes('刷新'))
  assert.ok(refresh, 'overview has explicit refresh action')
  refresh.click(); await tick()
  assert.match(w.document.body.textContent, /测试读取失败/)
  assert.doesNotMatch(w.document.querySelector('.activity-chart').textContent, /暂无使用记录/)
  assert.doesNotMatch(w.document.querySelector('.activity-balance').textContent, /¥0\.00/)
  fail = false
  w.document.querySelector('[role="alert"] button').click(); await tick()
  assert.match(w.document.body.textContent, /123\.45/)
  vm.$.appContext.app.unmount()
  // Expired subscriptions must not appear as current rights.
  response.subscription = { Status: 'active', CycleEnd: '2000-01-01T00:00:00Z' }
  vm = mount(); await tick()
  assert.match(w.document.body.textContent, /暂无生效月卡/)
  vm.$.appContext.app.unmount()
  response.subscription = { Status: 'cancelled', PlanID: 9, CycleEnd: new Date(Date.now() + 86400000).toISOString() }
  response.plans = [{ ID: 9, Name: '当前测试月卡', ValidityDays: 30 }]
  response.money_allowance = [{ period: 'cycle', quota_cent: 10000, consumed_cent: 2500, resets_at: response.subscription.CycleEnd }, { period: 'window', quota_cent: 2000, consumed_cent: 100, resets_at: new Date(Date.now() + 3600000).toISOString() }, { period: 'week', quota_cent: 5000, consumed_cent: 100, resets_at: new Date(Date.now() + 7200000).toISOString() }]
  response.consumption = [{ day: '2026-11-12', tokens: 123 }]
  response.consumption_by_model = [{ model: 'test-model', calls: 2, cost_cent: 123 }]
  vm = mount(); await tick()
  assert.match(w.document.body.textContent, /当前测试月卡/)
  assert.match(w.document.body.textContent, /已退订，到期结束/)
  assert.match(w.document.body.textContent, /¥1\.23/)
  assert.match(w.document.body.textContent, /25%/)
  const budgetLabels = [...w.document.querySelectorAll('.activity-limit small')].map(el => el.textContent.trim())
  assert.equal(budgetLabels.length, 3)
  assert.match(budgetLabels[0], /^本期截止 /, 'cancelled cycle allowance ends at CycleEnd')
  assert.match(budgetLabels[1], /^最早恢复 /, 'window allowance rolls forward')
  assert.match(budgetLabels[2], /^最早恢复 /, 'weekly allowance rolls forward')
  assert.doesNotMatch(w.document.body.textContent, /起恢复/)
  assert.match(w.document.body.textContent, /2 次/)
  vm.$.appContext.app.unmount()
  vm = mount(true); await tick()
  assert.equal(errors.length, 0, errors.join('\n'))
  assert.doesNotMatch(w.document.body.textContent, /销售统计加载失败/)
  assert.match(w.document.body.textContent, /还没有租户/)
  fail = true
  ;[...w.document.querySelectorAll('button')].find(b => b.textContent === '刷新').click(); await tick()
  assert.match(w.document.body.textContent, /租户账本加载失败/)
  assert.doesNotMatch(w.document.body.textContent, /还没有租户/)
  vm.$.appContext.app.unmount()
  let finish
  pending = new Promise(resolve => { finish = resolve })
  vm = mount(); vm.$.appContext.app.unmount(); finish(); await tick()
  assert.equal(w.document.querySelector('#app').textContent, '')
  assert.equal(errors.length, 0, errors.join('\n'))
  console.log('overview regression: null arrays, real balance, failure/retry, expired/cancelled cycle rights, rolling recovery labels, admin partial errors, unmount passed')
} finally {
  console.error = originalError
  w.close()
  await rm(dir, { recursive: true, force: true })
}
