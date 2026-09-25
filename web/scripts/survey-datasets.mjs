/**
 * 调研公开意图数据集，评估可复用性。
 *
 * 目的：为「模型商城客服」建立意图路由评测集，需要判断公开数据集能否直接用。
 * 结论预期：CLINC150/Banking77 都是英文 + 领域不同（银行/语音助手），
 * 只能借鉴其**分类学与构造方法**（同义改写、OOS 样本、每类均衡），
 * 语料必须自建。
 *
 * 用法：node scripts/survey-datasets.mjs
 */
import { readFileSync, existsSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const probe = resolve(here, '..', '.probe')

const out = []
const say = s => { out.push(s); console.log(s) }

// ---------- CLINC150 ----------
const clincPath = resolve(probe, 'clinc150.json')
if (existsSync(clincPath)) {
  const raw = JSON.parse(readFileSync(clincPath, 'utf8'))
  say('=== CLINC150 (英文, 语音助手域) ===')
  // 顶层是对象：{train, val, test, oos_train, oos_val, oos_test}
  const train = raw.train ?? []
  const oosTrain = raw.oos_train ?? []
  say(`splits = ${Object.keys(raw).join(', ')}`)
  say(`train 条数 = ${train.length}`)
  const cats = [...new Set(train.map(r => r[1]))].sort()
  say(`意图类别数 = ${cats.length}`)
  say(`OOS(域外) train 条数 = ${oosTrain.length}`)
  const counts = cats.map(c => train.filter(r => r[1] === c).length)
  say(`每类样本：min=${Math.min(...counts)} max=${Math.max(...counts)} avg=${Math.round(train.length / cats.length)}`)
  say('')
  say('适用性评估：')
  say('  · 领域：语音助手（闹钟/计时器/天气/打电话），与本项目「模型商城」完全不符')
  say('  · 语言：纯英文，无中文')
  say('  · 可借鉴：① 每类 ~150 条均衡 ② 专门设 OOS 类做「域外拒识」')
  say('             ③ 同义改写覆盖多种说法（这正是我们要测的）')
  say('')
  say('类别名（前 20）：')
  say('  ' + cats.slice(0, 20).join(', '))
  say('')
}

// ---------- Banking77 ----------
const bankPath = resolve(probe, 'banking77.csv')
if (existsSync(bankPath)) {
  const text = readFileSync(bankPath, 'utf8')
  const lines = text.split(/\r?\n/).filter(l => l.trim())
  say('=== Banking77 (英文, 银行域) ===')
  say(`行数 = ${lines.length - 1}（含表头 "${lines[0].trim()}"）`)
  // 格式：text,category —— text 可能含逗号，所以从**最后一个**逗号切
  const cats = new Map()
  for (const line of lines.slice(1)) {
    const i = line.lastIndexOf(',')
    if (i < 0) continue
    const cat = line.slice(i + 1).trim().replace(/^"|"$/g, '')
    if (!cat) continue
    cats.set(cat, (cats.get(cat) ?? 0) + 1)
  }
  say(`意图类别数 = ${cats.size}`)
  const counts = [...cats.values()]
  say(`每类样本：min=${Math.min(...counts)} max=${Math.max(...counts)} avg=${Math.round(counts.reduce((a, b) => a + b, 0) / counts.length)}`)
  say('')
  say('适用性评估：')
  say('  · 领域：银行客服（转账/挂失卡/汇率/ATM），与「模型商城」部分相似（都是账户+资金）')
  say('  · 语言：纯英文')
  say('  · 可借鉴：① 细粒度意图拆分（77 类，如「卡丢失」vs「卡被吞」）')
  say('             ② 每类约 130 条均衡规模 —— 与规划的 12×100 同量级')
  say('             ③ 同义句构造方式（同一意图多种说法）')
  say('')
  say('类别名（前 30，看分类学粒度）：')
  say('  ' + [...cats.keys()].slice(0, 30).join(', '))
  say('')
}

// ---------- 结论 ----------
say('=== 结论 ===')
say('公开数据集不适合直接作为本项目的评测语料：')
say('  1. 语言不符（都是英文，无法测中文意图路由）')
say('  2. 领域不符（语音助手 / 银行，本项目的工具集是查余额/充值/退款/订阅套餐）')
say('  3. 意图体系不可迁移（它们的类别名与本项目 12 个工具无对应关系）')
say('')
say('但可借鉴其方法论，用于自建评测集：')
say('  · 规模：每类 100~150 条（CLINC150 ≈150/类，Banking77 ≈130/类）')
say('  · 均衡：每类样本数接近，避免类别不平衡带来的指标失真')
say('  · 说法多样性：同一意图收集多种自然表达，而非只换同义词')
say('  · 保留 OOS：设「域外/闲聊」类，测模型是否会乱调工具')
say('  · 单意图为主、少量多意图，分别统计准确率')
say('')

const reportPath = resolve(probe, 'survey.txt')
const { writeFileSync } = await import('node:fs')
writeFileSync(reportPath, out.join('\n'), 'utf8')
console.log(`报告已写入 ${reportPath}`)
