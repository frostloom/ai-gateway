/**
 * 校验生成的意图评测集：均衡性、唯一性、维度覆盖。
 * 用法：node web/scripts/verify-intent-suite.mjs
 */
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const dir = resolve(here, '..', '..', 'eval', 'intent-suite')

const manifest = JSON.parse(readFileSync(resolve(dir, 'manifest.json'), 'utf8'))
const files = readdirSync(dir).filter(f => f.endsWith('.json') && f !== 'manifest.json')

let problems = 0
const fail = m => { problems++; console.log('  ✗ ' + m) }

console.log(`=== 校验 ${files.length} 个文件 ===`)
const allTexts = new Map() // 全局文本 → 来源，用于查跨类重复
const intentSeen = new Map()

for (const f of files) {
  const rows = JSON.parse(readFileSync(resolve(dir, f), 'utf8'))
  const isAdv = f === 'adversarial.json'
  const name = f.replace('.json', '')

  // 1) 条数
  if (!isAdv && rows.length !== 100) fail(`${name}: 条数 ${rows.length} != 100`)
  if (isAdv && rows.length !== 20) fail(`${name}: 条数 ${rows.length} != 20`)

  // 2) 本文件内文本唯一
  const texts = rows.map(r => r.text)
  const dup = texts.filter((t, i) => texts.indexOf(t) !== i)
  if (dup.length) fail(`${name}: 文件内重复 ${dup.length} 条，例如 "${dup[0]}"`)

  // 3) 必填字段
  for (const r of rows) {
    for (const k of ['id', 'text', 'intent', 'dimension', 'risk', 'needs_confirm']) {
      if (r[k] === undefined) fail(`${name}: ${r.id} 缺字段 ${k}`)
    }
    if (!['low', 'medium', 'high'].includes(r.risk)) fail(`${name}: ${r.id} risk 非法 "${r.risk}"`)
    if (typeof r.needs_confirm !== 'boolean') fail(`${name}: ${r.id} needs_confirm 非布尔`)
    if (!r.text || r.text.length < 1) fail(`${name}: ${r.id} text 为空`)
  }

  // 4) 维度覆盖
  const dims = new Set(rows.map(r => r.dimension))
  if (!isAdv && dims.size < 8) fail(`${name}: 维度只覆盖 ${dims.size} 种`)

  // 5) 跨文件重复
  for (const r of rows) {
    if (allTexts.has(r.text)) {
      fail(`跨类重复文本 "${r.text}"（${name} 与 ${allTexts.get(r.text)}）`)
    } else {
      allTexts.set(r.text, name)
    }
  }

  // 6) intent 一致性（同类应同一工具；对抗子集故意混合，跳过）
  const intents = new Set(rows.map(r => r.intent))
  if (!isAdv && intents.size !== 1) fail(`${name}: intent 不唯一 ${[...intents].join(',')}`)
  intentSeen.set(name, [...intents][0])

  // 7) 写操作必须 needs_confirm（对抗子集是"非法请求"，期望本就不一致，跳过）
  const tool = [...intents][0]
  const WRITE = ['recharge', 'refund_recharge', 'subscribe_plan', 'change_plan', 'cancel_subscription']
  if (!isAdv) {
    if (WRITE.includes(tool) && rows.some(r => !r.needs_confirm)) {
      fail(`${name}: 写工具 ${tool} 存在 needs_confirm=false 的样本`)
    }
    if (!WRITE.includes(tool) && rows.some(r => r.needs_confirm)) {
      fail(`${name}: 读工具 ${tool} 不应需确认`)
    }
  }

  const byDim = {}
  for (const r of rows) byDim[r.dimension] = (byDim[r.dimension] ?? 0) + 1
  console.log(`  ${name.padEnd(20)} ${String(rows.length).padStart(3)} 条  → ${tool.padEnd(20)} 维度 ${Object.keys(byDim).length} 种`)
}

console.log('')
console.log(`总样本数 = ${allTexts.size}`)
console.log(`唯一文本 = ${allTexts.size}`)
console.log(problems === 0 ? '\n校验通过：无问题' : `\n发现 ${problems} 个问题`)
process.exit(problems === 0 ? 0 : 1)
