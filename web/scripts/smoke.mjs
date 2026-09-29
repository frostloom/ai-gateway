/**
 * 前端运行时冒烟测试（Node + jsdom）
 *
 * 目的：在没有浏览器的环境里真实挂载 Vue 应用，捕获「构建通过但运行时报错」的问题
 * （例如组件引用不存在、模板运行时求值异常、ChatDock 挂载失败等）。
 *
 * 用法：cd web && node scripts/smoke.mjs
 */
import { JSDOM } from 'jsdom'
import { readFileSync, writeFileSync, unlinkSync, readdirSync } from 'node:fs'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { dirname, resolve } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const dist = resolve(here, '../..', 'internal/gateway/web/dist')

function bootHtml(html) {
  const dom = new JSDOM(html, {
    url: 'http://127.0.0.1:18080/',
    runScripts: 'outside-only',
    pretendToBeVisual: true,
  })
  const { window } = dom
  // 最小化的浏览器能力补齐（Vue 运行时需要）
  window.matchMedia = window.matchMedia || (q => ({
    matches: false, media: q, onchange: null,
    addListener() {}, removeListener() {},
    addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false },
  }))
  window.scrollTo = () => {}
  return dom
}

async function run(entry, label) {
  const html = readFileSync(resolve(dist, entry), 'utf8')
  const jsRef = html.match(/src="([^"]+\.js)"/)?.[1]
  if (!jsRef) throw new Error(`${entry}: 未找到入口 JS`)

  const dom = bootHtml(html)
  const { window } = dom

  // 用 fetch 把构建产物喂给 jsdom 的 ESM 加载（改写 import 说明符依赖真实文件系统，
  // 这里采用更稳的做法：直接在 Node 侧以 data URL 动态 import，注入 jsdom 全局）
  global.window = window
  global.document = window.document
  if (!Object.getOwnPropertyDescriptor(globalThis, 'navigator')?.get) {
    global.navigator = window.navigator
  }
  global.HTMLElement = window.HTMLElement
  global.Element = window.Element
  global.Node = window.Node
  global.SVGElement = window.SVGElement
  global.CustomEvent = window.CustomEvent
  global.Event = window.Event
  global.getComputedStyle = () => ({ getPropertyValue: () => '' })
  global.requestAnimationFrame = cb => setTimeout(() => cb(Date.now()), 0)
  global.cancelAnimationFrame = id => clearTimeout(id)
  global.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }
  global.IntersectionObserver = class { observe() {} unobserve() {} disconnect() {} }
  global.MutationObserver = window.MutationObserver
  global.DOMParser = window.DOMParser
  global.Text = window.Text
  global.Comment = window.Comment
  global.DocumentFragment = window.DocumentFragment
  global.HTMLInputElement = window.HTMLInputElement
  global.HTMLSelectElement = window.HTMLSelectElement
  global.HTMLTextAreaElement = window.HTMLTextAreaElement
  global.MouseEvent = window.MouseEvent
  global.KeyboardEvent = window.KeyboardEvent
  global.matchMedia = window.matchMedia
  global.localStorage = window.localStorage
  global.sessionStorage = window.sessionStorage
  global.location = window.location

  global.fetch = window.fetch = async () => ({ ok: true, status: 200, json: async () => ({ initialized: false }) })
  const errors = []
  const origError = console.error
  console.error = (...a) => { errors.push(a.map(String).join(' ')); origError(...a) }
  window.addEventListener('error', e => errors.push('window.error: ' + e.message))

  const code = readFileSync(resolve(dist, jsRef.replace(/^\//, '')), 'utf8')
  // 以 file:// 动态 import：入口 chunk 里含相对 import（./Empty-*.js），
  // 必须让模块解析器按真实路径解析，且临时文件要与 chunk 同目录。
  const chunkDir = resolve(dist, jsRef.replace(/^\//, '').replace(/\/[^/]+$/, ''))
  const tmp = resolve(chunkDir, `.__smoke_${label}_${Date.now()}.mjs`)
  writeFileSync(tmp, code)
  const mod = await import(pathToFileURL(tmp).href)
    .catch(e => { errors.push('import: ' + e.message); return null })
  try { unlinkSync(tmp) } catch { /* ignore */ }

  // 等待 Vue 挂载完成
  await new Promise(r => setTimeout(r, 120))

  const app = window.document.getElementById('app')
  const mounted = !!(app && app.innerHTML.trim().length > 0)

  console.error = origError
  return { label, jsRef, mounted, htmlLen: app?.innerHTML.length ?? 0, errors, mod: !!mod, window, app }
}

/** 进入已登录态并检查悬浮客服层（两个页面都要有） */
async function checkChatDock(entry, label, { admin = false } = {}) {
  const html = readFileSync(resolve(dist, entry), 'utf8')
  const jsRef = html.match(/src="([^"]+\.js)"/)?.[1]
  const dom = bootHtml(html)
  const { window } = dom
  const errors = []

  // 注入登录态：portal 用 localStorage key；admin 用 fetch stub 让鉴权通过
  window.localStorage.setItem('portal.api_key', 'sk-demo-8f3a2b1c9d4e5f60')
  const json = (data, status = 200) => Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(JSON.stringify(data)),
    headers: { get: () => 'application/json' },
  })
  window.fetch = global.fetch = (url, opts) => {
    const u = String(url)
    if (u.includes('/admin/auth/initialized')) return json({ initialized: true })
    if (u.includes('/admin/auth/me')) return json({ username: 'zlx' })
    if (u.includes('/admin/overview')) return json({ tenants: [{ id: 1, name: 'demo' }], totals: {} })
    if (u.includes('/admin/health')) return json({ ok: true, services: {} })
    if (u.includes('/portal/me')) return json({ balance: 100000, balance_cent: 100000, ledger_balance: 100000, consistent: true, plans: [], consumption: [], consumption_by_model: [], topups: [] })
    if (u.includes('/portal/models') || u.includes('/admin/models')) return json({ items: [] })
    return json({})
  }

  Object.assign(globalThis, {
    window, document: window.document, HTMLElement: window.HTMLElement,
    Element: window.Element, Node: window.Node, SVGElement: window.SVGElement,
    CustomEvent: window.CustomEvent, Event: window.Event,
    MutationObserver: window.MutationObserver, DOMParser: window.DOMParser,
    Text: window.Text, Comment: window.Comment, DocumentFragment: window.DocumentFragment,
    localStorage: window.localStorage, sessionStorage: window.sessionStorage,
    location: window.location, matchMedia: window.matchMedia,
    getComputedStyle: () => ({ getPropertyValue: () => '' }),
    requestAnimationFrame: cb => setTimeout(() => cb(Date.now()), 0),
    cancelAnimationFrame: id => clearTimeout(id),
    ResizeObserver: class { observe() {} unobserve() {} disconnect() {} },
    IntersectionObserver: class { observe() {} unobserve() {} disconnect() {} },
  })

  const origError = console.error
  console.error = (...a) => { errors.push(a.map(String).join(' ')) }

  const code = readFileSync(resolve(dist, jsRef.replace(/^\//, '')), 'utf8')
  const chunkDir = resolve(dist, jsRef.replace(/^\//, '').replace(/\/[^/]+$/, ''))
  const tmp = resolve(chunkDir, `.__smoke_${label}_dock_${Date.now()}.mjs`)
  writeFileSync(tmp, code)
  await import(pathToFileURL(tmp).href).catch(e => errors.push('import: ' + e.message))
  try { unlinkSync(tmp) } catch { /* ignore */ }

  await new Promise(r => setTimeout(r, 200))
  console.error = origError

  const d = window.document

  // Open explicitly: support starts minimized so it does not cover the console.
  const minimizedInitially = !!d.querySelector('.launcher') && !d.querySelector('.panel')
  d.querySelector('.launcher')?.dispatchEvent(new window.MouseEvent('click', { bubbles: true }))
  await new Promise(r => setTimeout(r, 150))
  const openByDefault = !!d.querySelector('.panel')
  const hasInput = !!d.querySelector('.pin')
  const hasHeader = !!d.querySelector('.phead')
  const quick = d.querySelectorAll('.qbtn').length

  // 点最后一个头部按钮 = 收起 → 应变成浮标
  const btns = d.querySelectorAll('.hbtn')
  btns[btns.length - 1]?.dispatchEvent(new window.MouseEvent('click', { bubbles: true }))
  await new Promise(r => setTimeout(r, 150))
  const collapsedToLauncher = !!d.querySelector('.launcher') && !d.querySelector('.panel')

  // 再点浮标 → 应重新展开
  d.querySelector('.launcher')?.dispatchEvent(new window.MouseEvent('click', { bubbles: true }))
  await new Promise(r => setTimeout(r, 150))
  const reopened = !!d.querySelector('.panel')

  return {
    label,
    openByDefault, minimizedInitially,
    collapsedToLauncher,
    reopened,
    hasHeader,
    hasInput,
    quick,
    errors,
  }
}

/** 模拟一次「写操作挂起 → 确认执行」的客服任务式对话 */
async function checkTaskFlow(entry, label) {
  const html = readFileSync(resolve(dist, entry), 'utf8')
  const jsRef = html.match(/src="([^"]+\.js)"/)?.[1]
  const dom = bootHtml(html)
  const { window } = dom
  const errors = []
  let chatCalls = 0

  const json = (data) => Promise.resolve({
    ok: true, status: 200,
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(JSON.stringify(data)),
  })
  window.localStorage.setItem('portal.api_key', 'sk-demo-8f3a2b1c9d4e5f60')
  window.fetch = global.fetch = (url) => {
    const u = String(url)
    if (u.includes('/portal/me')) return json({ balance: 100000, balance_cent: 100000, ledger_balance: 100000, consistent: true, plans: [], consumption: [], consumption_by_model: [], topups: [] })
    if (u.includes('/portal/models') || u.includes('/admin/models')) return json({ items: [] })
    if (u.includes('/portal/chat') || u.includes('/admin/chat')) {
      chatCalls++
      // 第一次：写操作挂起（返回 pending）；确认后：执行成功
      if (chatCalls === 1) {
        return json({
          session_id: 'sid_smoke_1',
          reply: '将为你充值 100 元（10000 分），请确认。',
          trace: [{ tool: 'recharge', args: '{"amount_money":100}', confirmed: false, preview: '将为你充值 100 元', result: '等待确认' }],
          pending_confirm: true,
          pending_preview: '将为你充值 100 元（10000 分）',
          pending_tool: 'recharge',
        })
      }
      return json({
        session_id: 'sid_smoke_1',
        reply: '充值成功，已入账 10000 分。',
        trace: [{ tool: 'recharge', args: '{"amount_money":100}', confirmed: true, preview: '将为你充值 100 元', result: '充值成功' }],
        pending_confirm: false, pending_preview: '', pending_tool: '',
      })
    }
    return json({})
  }

  Object.assign(globalThis, {
    window, document: window.document, HTMLElement: window.HTMLElement,
    Element: window.Element, Node: window.Node, SVGElement: window.SVGElement,
    CustomEvent: window.CustomEvent, Event: window.Event,
    MutationObserver: window.MutationObserver, DOMParser: window.DOMParser,
    Text: window.Text, Comment: window.Comment, DocumentFragment: window.DocumentFragment,
    localStorage: window.localStorage, sessionStorage: window.sessionStorage,
    location: window.location, matchMedia: window.matchMedia,
    getComputedStyle: () => ({ getPropertyValue: () => '' }),
    requestAnimationFrame: cb => setTimeout(() => cb(Date.now()), 0),
    cancelAnimationFrame: id => clearTimeout(id),
    ResizeObserver: class { observe() {} unobserve() {} disconnect() {} },
    IntersectionObserver: class { observe() {} unobserve() {} disconnect() {} },
  })

  const origError = console.error
  console.error = (...a) => { errors.push(a.map(String).join(' ')) }
  const code = readFileSync(resolve(dist, jsRef.replace(/^\//, '')), 'utf8')
  const chunkDir = resolve(dist, jsRef.replace(/^\//, '').replace(/\/[^/]+$/, ''))
  const tmp = resolve(chunkDir, `.__smoke_${label}_task_${Date.now()}.mjs`)
  writeFileSync(tmp, code)
  await import(pathToFileURL(tmp).href).catch(e => errors.push('import: ' + e.message))
  try { unlinkSync(tmp) } catch { /* ignore */ }
  await new Promise(r => setTimeout(r, 220))
  console.error = origError

  const d = window.document
  d.querySelector('.launcher')?.dispatchEvent(new window.MouseEvent('click', { bubbles: true }))
  await new Promise(r => setTimeout(r, 120))

  // 点击第一条快捷指令（模拟用户发消息）
  d.querySelector('.qbtn')?.dispatchEvent(new window.MouseEvent('click', { bubbles: true }))
  await new Promise(r => setTimeout(r, 220))

  const bubbles = d.querySelectorAll('.bubble').length
  const steps = d.querySelectorAll('.step').length
  const confirmBar = !!d.querySelector('.confirm')
  const confirmPreview = d.querySelector('.cprev')?.textContent?.trim() ?? ''
  const confirmBtn = d.querySelector('.cyes')

  // 点击「确认执行」
  confirmBtn?.dispatchEvent(new window.MouseEvent('click', { bubbles: true }))
  await new Promise(r => setTimeout(r, 220))

  const afterConfirmBar = !!d.querySelector('.confirm')
  const staleNote = !!d.querySelector('.stale')
  const text = d.querySelector('.plist')?.textContent ?? ''

  return {
    label, chatCalls, bubbles, steps, confirmBar, confirmPreview,
    confirmResolved: !afterConfirmBar,
    staleNote,
    sawSuccess: text.includes('充值成功'),
    errors,
  }
}

/** 审计风险分级：用真实记录形态验证，防止「预览行被误报为异常」 */
async function checkAuditRisk(entry = 'dashboard.html', label = 'audit-risk') {
  const html = readFileSync(resolve(dist, entry), 'utf8')
  const jsRef = html.match(/src="([^"]+\.js)"/)?.[1]
  const dom = bootHtml(html)
  const { window } = dom
  const errors = []

  // 真实数据形态（取自本机 agent_audit_log）：
  //  #27 recharge confirmed=true  → 已确认
  //  #26 recharge confirmed=false result=pending_confirmation → 待确认（正常预览行，不得报异常）
  //  #20 refund_recharge confirmed=true → 高危（资金流出）
  //  #29 get_recent_bills 只读 → 常规
  const records = [
    { ID: 27, SessionID: 'sid_a', TenantID: 1, Tool: 'recharge', Args: '{"amount_money":100}', Confirmed: true, Preview: '将模拟充值 ¥100', Result: '充值成功：订单 x 已支付', Guard: '', CreatedAt: '2026-08-21T21:37:32+08:00' },
    { ID: 26, SessionID: 'sid_a', TenantID: 1, Tool: 'recharge', Args: '{"amount_money":100}', Confirmed: false, Preview: '将模拟充值 ¥100', Result: 'pending_confirmation', Guard: '', CreatedAt: '2026-08-21T21:37:28+08:00' },
    { ID: 20, SessionID: 'sid_b', TenantID: 1, Tool: 'refund_recharge', Args: '{"amount_money":50}', Confirmed: true, Preview: '将退款 ¥50', Result: '退款成功', Guard: '', CreatedAt: '2026-08-21T20:00:00+08:00' },
    { ID: 29, SessionID: 'sid_c', TenantID: 1, Tool: 'get_recent_bills', Args: '{}', Confirmed: false, Preview: '', Result: '最近 3 笔', Guard: '', CreatedAt: '2026-09-01T20:16:15+08:00' },
  ]

  const json = (data) => Promise.resolve({
    ok: true, status: 200,
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(JSON.stringify(data)),
  })
  window.localStorage.setItem('portal.api_key', 'sk-demo-x')
  window.fetch = global.fetch = (url) => {
    const u = String(url)
    if (u.includes('/admin/auth/initialized')) return json({ initialized: true })
    if (u.includes('/admin/auth/me')) return json({ username: 'zlx' })
    if (u.includes('/admin/overview')) return json({ tenants: [{ id: 1, name: 'demo' }], totals: {} })
    if (u.includes('/admin/health')) return json({ ok: true, services: {} })
    if (u.includes('/admin/audit')) return json({ items: records })
    if (u.includes('/admin/sales')) return json({ summary: {}, by_model: [], by_vendor: [], by_day: [] })
    return json({ items: [], tenants: [] })
  }

  Object.assign(globalThis, {
    window, document: window.document, HTMLElement: window.HTMLElement,
    Element: window.Element, Node: window.Node, SVGElement: window.SVGElement,
    CustomEvent: window.CustomEvent, Event: window.Event,
    MutationObserver: window.MutationObserver, DOMParser: window.DOMParser,
    Text: window.Text, Comment: window.Comment, DocumentFragment: window.DocumentFragment,
    localStorage: window.localStorage, sessionStorage: window.sessionStorage,
    location: window.location, matchMedia: window.matchMedia,
    getComputedStyle: () => ({ getPropertyValue: () => '' }),
    requestAnimationFrame: cb => setTimeout(() => cb(Date.now()), 0),
    cancelAnimationFrame: id => clearTimeout(id),
    ResizeObserver: class { observe() {} unobserve() {} disconnect() {} },
    IntersectionObserver: class { observe() {} unobserve() {} disconnect() {} },
  })

  const origError = console.error
  console.error = (...a) => { errors.push(a.map(String).join(' ')) }
  const code = readFileSync(resolve(dist, jsRef.replace(/^\//, '')), 'utf8')
  const chunkDir = resolve(dist, jsRef.replace(/^\//, '').replace(/\/[^/]+$/, ''))
  const tmp = resolve(chunkDir, `.__smoke_${label}_${Date.now()}.mjs`)
  writeFileSync(tmp, code)
  await import(pathToFileURL(tmp).href).catch(e => errors.push('import: ' + e.message))
  try { unlinkSync(tmp) } catch { /* ignore */ }
  await new Promise(r => setTimeout(r, 250))
  console.error = origError

  const d = window.document
  // 切到「客服审计」视图：点侧边栏对应按钮
  const navBtns = [...d.querySelectorAll('.sitem')]
  const auditBtn = navBtns.find(b => (b.textContent || '').includes('客服审计'))
  auditBtn?.dispatchEvent(new window.MouseEvent('click', { bubbles: true }))
  await new Promise(r => setTimeout(r, 350))

  const rows = [...d.querySelectorAll('.dt-row')]
  const rowText = rows.map(r => r.textContent || '')

  // 行内不渲染审计 ID（列里是租户号），因此按「工具 + 结果特征」定位
  const refundRow = rowText.find(t => t.includes('refund_recharge')) || ''
  const previewRow = rowText.find(t => t.includes('pending_confirmation')) || ''
  const confirmedRow = rowText.find(t => t.includes('充值成功')) || ''
  const readRow = rowText.find(t => t.includes('get_recent_bills')) || ''

  const previewRowUntouched = !/未确认写/.test(previewRow)   // 预览行不得误报异常
  const previewRowTagged = /待确认/.test(previewRow)          // 应标「待确认」
  const confirmedRowOk = /已确认/.test(confirmedRow)          // 确认执行的写操作
  const refundRowHighRisk = /高危/.test(refundRow)            // 退款=资金流出
  const readRowNormal = /常规/.test(readRow)                  // 只读=常规

  return {
    label, rowCount: rows.length, errors,
    previewRowUntouched, previewRowTagged, confirmedRowOk, refundRowHighRisk, readRowNormal,
    debugRows: rowText.map(t => t.replace(/\s+/g, ' ').slice(0, 150)),
  }
}

let failed = 0

console.log('--- 源码编码完整性（防 mojibake 回归）---')
{
  // 历史事故：PowerShell 5.1 默认 GBK，误读无 BOM 的 UTF-8 源码后再写回，
  // 中文与全角标点被替换成 U+FFFD / '?'。构建不会报错，只有肉眼看页面才发现。
  // 这里扫源码，把这类损坏挡在提交之前。
  const SRC = resolve(here, '../src')
  const BAD = [
    { re: /\uFFFD/, name: 'U+FFFD 替换符' },
    { re: /[\u00C0-\u00FF]{3,}/, name: '疑似 latin1 乱码串' },
    { re: /[锟斤拷]/, name: '中文乱码特征字' },
    { re: /[兪庪彪]{1}/, name: 'GBK 误码残留字' },
  ]
  const violations = []
  const walk = dir => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = resolve(dir, e.name)
      if (e.isDirectory()) { walk(p); continue }
      if (!/\.(vue|ts|css)$/.test(e.name)) continue
      const text = readFileSync(p, 'utf8')
      const rel = p.slice(SRC.length + 1)
      text.split('\n').forEach((line, i) => {
        for (const b of BAD) {
          if (b.re.test(line)) {
            violations.push(`${rel}:${i + 1} [${b.name}] ${line.trim().slice(0, 90)}`)
            break
          }
        }
      })
    }
  }
  walk(SRC)
  const ok = violations.length === 0
  if (!ok) failed++
  console.log(`[${ok ? 'PASS' : 'FAIL'}] 源码无 mojibake`)
  for (const v of violations.slice(0, 12)) console.log('        ! ' + v)
  if (violations.length > 12) console.log(`        ... 另有 ${violations.length - 12} 处`)
}

for (const [entry, label] of [['dashboard.html', 'dashboard'], ['portal.html', 'portal']]) {
  try {
    const r = await run(entry, label)
    const ok = r.mounted && r.errors.length === 0
    if (!ok) failed++
    console.log(`[${ok ? 'PASS' : 'FAIL'}] ${r.label}  入口=${r.jsRef}`)
    console.log(`        mounted=${r.mounted}  innerHTML=${r.htmlLen} chars  runtimeErrors=${r.errors.length}`)
    for (const e of r.errors.slice(0, 6)) console.log('        ! ' + e.slice(0, 220))
  } catch (e) {
    failed++
    console.log(`[FAIL] ${label}: ${e.message}`)
  }
}

console.log('\n--- 悬浮客服层（ChatDock）---')
for (const [entry, label, opts] of [
  ['dashboard.html', 'dashboard(admin)', { admin: true }],
  ['portal.html', 'portal(tenant)', {}],
]) {
  try {
    const r = await checkChatDock(entry, label, opts)
    const ok = r.openByDefault && r.hasHeader && r.hasInput && r.collapsedToLauncher && r.reopened && r.errors.length === 0
    if (!ok) failed++
    console.log(`[${ok ? 'PASS' : 'FAIL'}] ${r.label}`)
    console.log(`        打开面板=${r.openByDefault} 头部=${r.hasHeader} 输入框=${r.hasInput} 快捷指令=${r.quick}`)
    console.log(`        可收起=${r.collapsedToLauncher} 可再展开=${r.reopened} 错误=${r.errors.length}`)
    for (const e of r.errors.slice(0, 6)) console.log('        ! ' + e.slice(0, 220))
  } catch (e) {
    failed++
    console.log(`[FAIL] ${label}: ${e.message}`)
  }
}

console.log('\n--- 任务式对话（挂起 → 确认执行）---')
for (const [entry, label] of [['portal.html', 'portal 写操作确认流']]) {
  try {
    const r = await checkTaskFlow(entry, label)
    const ok = r.chatCalls === 2 && r.confirmBar && r.confirmResolved && r.staleNote && r.sawSuccess && r.errors.length === 0
    if (!ok) failed++
    console.log(`[${ok ? 'PASS' : 'FAIL'}] ${r.label}`)
    console.log(`        请求=${r.chatCalls} 气泡=${r.bubbles} 步骤卡=${r.steps} 确认条出现=${r.confirmBar}`)
    console.log(`        预览="${r.confirmPreview}" 确认后收起=${r.confirmResolved} 留痕=${r.staleNote} 见成功文案=${r.sawSuccess} 错误=${r.errors.length}`)
    for (const e of r.errors.slice(0, 6)) console.log('        ! ' + e.slice(0, 220))
  } catch (e) {
    failed++
    console.log(`[FAIL] ${label}: ${e.message}`)
  }
}

console.log('\n--- 客服审计：风险分级（真实记录形态）---')
try {
  const r = await checkAuditRisk()
  const ok = r.rowCount === 4 && r.previewRowUntouched && r.previewRowTagged &&
             r.confirmedRowOk && r.refundRowHighRisk && r.readRowNormal && r.errors.length === 0
  if (!ok) failed++
  console.log(`[${ok ? 'PASS' : 'FAIL'}] ${r.label}（渲染 ${r.rowCount} 行）`)
  console.log(`        预览行不误报=${r.previewRowUntouched} 预览行标「待确认」=${r.previewRowTagged}`)
  console.log(`        已确认行=${r.confirmedRowOk} 退款标高危=${r.refundRowHighRisk} 只读标常规=${r.readRowNormal} 错误=${r.errors.length}`)
  for (const e of r.errors.slice(0, 5)) console.log('        ! ' + e.slice(0, 220))
} catch (e) {
  failed++
  console.log(`[FAIL] audit-risk: ${e.message}`)
}

console.log(failed === 0 ? '\n冒烟测试全部通过' : `\n${failed} 项失败`)
process.exit(failed === 0 ? 0 : 1)
