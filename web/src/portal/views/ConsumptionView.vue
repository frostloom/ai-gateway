<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { consumptionApi } from '../api'
import type { DayStat, ModelStat } from '../types'
import { money, fmt } from '../../shared/utils/format'
import Card from '../../shared/components/ui/Card.vue'
import AreaChart from '../../shared/components/ui/AreaChart.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'

const days = ref(7)
const daily = ref<DayStat[]>([])
const byModel = ref<ModelStat[]>([])
const loading = ref(true)
const error = ref('')
const modelAvailable = ref(false)
let requestId = 0
const total = computed(() => daily.value.reduce((sum, day) => sum + day.tokens, 0))
const average = computed(() => new Intl.NumberFormat('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 4 }).format(total.value / days.value / 100))
const calls = computed(() => byModel.value.reduce((sum, model) => sum + model.calls, 0))
const inputTokens = computed(() => byModel.value.reduce((sum, model) => sum + model.input_tokens, 0))
const outputTokens = computed(() => byModel.value.reduce((sum, model) => sum + model.output_tokens, 0))
const modelCost = computed(() => byModel.value.reduce((sum, model) => sum + model.cost_cent, 0))
const trendSeries = computed(() => [{ name: '已结算消费', points: daily.value.map(day => ({ label: day.day, value: day.tokens })) }])
const rangeLabel = computed(() => {
  const first = daily.value[0]?.day
  const last = daily.value[daily.value.length - 1]?.day
  return first && last ? `${first} 至 ${last}` : `近 ${days.value} 个自然日`
})
async function load() {
  const id = ++requestId
  loading.value = true
  error.value = ''
  try {
    const result = await consumptionApi(days.value)
    if (id !== requestId) return
    daily.value = [...(result.items ?? [])].sort((a, b) => a.day.localeCompare(b.day))
    byModel.value = [...(result.by_model ?? [])].sort((a, b) => b.cost_cent - a.cost_cent)
    modelAvailable.value = result.by_model === null || Array.isArray(result.by_model)
  } catch (e: any) {
    if (id !== requestId) return
    error.value = e?.message || '消费数据加载失败，请重试'
    daily.value = []; byModel.value = []; modelAvailable.value = false
  } finally { if (id === requestId) loading.value = false }
}
onMounted(load)
function switchDays(value: number) {
  if (days.value === value) return
  days.value = value
  daily.value = []
  byModel.value = []
  void load()
}
</script>

<template>
  <div class="cons">
    <header class="head">
      <div><h1>用量与消费</h1><p>{{ rangeLabel }}<span class="range-note">按服务端日期统计，包含今天</span></p></div>
      <div class="controls">
        <div class="seg" aria-label="消费统计范围"><button v-for="value in [7, 30, 90]" :key="value" class="segbtn" :class="{ on: days === value }" :aria-pressed="days === value" @click="switchDays(value)">{{ value }} 天</button></div>
        <BaseButton variant="ghost" :disabled="loading" @click="load">{{ loading ? '加载中' : '刷新' }}</BaseButton>
      </div>
    </header>
    <div v-if="error" class="load-error" role="alert">{{ error }}<BaseButton variant="ghost" @click="load">重试</BaseButton></div>
    <div class="metrics" :aria-busy="loading">
      <div><span>已结算消费</span><strong>{{ loading || error ? '—' : money(total) }}</strong><small>{{ loading || error ? '人民币' : `日均 ¥${average} · 人民币` }}</small></div>
      <div><span>已结算调用</span><strong>{{ loading || error || !modelAvailable ? '—' : fmt(calls) }}</strong><small>次请求</small></div>
      <div><span>输入用量</span><strong>{{ loading || error || !modelAvailable ? '—' : fmt(inputTokens) }}</strong><small>token · 发送给模型的内容</small></div>
      <div><span>输出用量</span><strong>{{ loading || error || !modelAvailable ? '—' : fmt(outputTokens) }}</strong><small>token · 模型生成的内容</small></div>
    </div>
    <Card title="每日消费" :sub="`${rangeLabel} · 人民币元`">
      <div v-if="loading" class="chart-placeholder skeleton" />
      <div v-else-if="error" class="chart-placeholder">消费数据不可用</div>
      <AreaChart v-else-if="daily.length > 1" :series="trendSeries" :height="250" :format="(value: number) => money(value)" />
      <div v-else class="chart-placeholder">{{ daily.length ? `${daily[0]!.day} 消费 ${money(daily[0]!.tokens)}` : '当前范围暂无消费记录' }}</div>
      <details v-if="!loading && !error && daily.length" class="daily-details">
        <summary>按日查看明细</summary>
        <div class="daily-scroll" tabindex="0" role="region" aria-label="每日消费明细">
          <table class="daily-table"><caption class="sr-only">{{ rangeLabel }}每日已结算消费，金额为人民币元</caption><thead><tr><th scope="col">日期</th><th scope="col">消费（元）</th></tr></thead><tbody><tr v-for="day in daily" :key="day.day"><th scope="row">{{ day.day }}</th><td>{{ money(day.tokens) }}</td></tr></tbody></table>
        </div>
      </details>
    </Card>
    <Card title="模型用量明细" :sub="`${rangeLabel} · 按消费金额排序`" :pad="false">
      <div v-if="loading" class="table-placeholder" role="status">正在读取模型用量…</div>
      <div v-else-if="error" class="table-placeholder">模型用量不可用</div>
      <div v-else-if="!modelAvailable" class="table-placeholder">当前服务未提供该范围的模型统计</div>
      <div v-else-if="!byModel.length" class="table-placeholder">当前范围暂无已结算的模型调用</div>
      <div v-else class="table-scroll" tabindex="0" role="region" aria-label="模型用量明细，可横向滚动">
        <table class="model-table">
          <caption class="sr-only">{{ rangeLabel }}模型已结算用量，输入与输出单位为 token，消费单位为人民币元</caption>
          <thead><tr><th scope="col">模型 / 厂商</th><th scope="col">调用（次）</th><th scope="col">输入 token</th><th scope="col">输出 token</th><th scope="col">消费（元）</th></tr></thead>
          <tbody><tr v-for="model in byModel" :key="model.model"><th scope="row"><span class="mono model-id">{{ model.model }}</span><small>{{ model.vendor || '厂商未提供' }}</small></th><td>{{ fmt(model.calls) }}</td><td>{{ fmt(model.input_tokens) }}</td><td>{{ fmt(model.output_tokens) }}</td><td class="cost">{{ money(model.cost_cent) }}</td></tr></tbody>
          <tfoot><tr><th scope="row">合计</th><td>{{ fmt(calls) }}</td><td>{{ fmt(inputTokens) }}</td><td>{{ fmt(outputTokens) }}</td><td class="cost">{{ money(modelCost) }}</td></tr></tfoot>
        </table>
      </div>
    </Card>
  </div>
</template>

<style scoped>
.cons { display: flex; flex-direction: column; gap: 24px; }
.head, .controls { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.head h1 { font-size: 24px; font-weight: 600; letter-spacing: -.02em; color: var(--ink-900); }
.head p { margin-top: 8px; font-size: 13px; color: var(--ink-500); line-height: 1.8; }
.range-note { display: block; color: var(--ink-400); font-size: 12px; }
.seg { display: flex; padding: 4px; border: 1px solid var(--hairline); background: var(--surface-2); border-radius: 7px; }
.segbtn { height: 34px; padding: 0 16px; border: none; background: transparent; color: var(--ink-500); border-radius: 4px; font-size: 13px; }
.segbtn.on { background: var(--surface); color: var(--ink-900); box-shadow: var(--shadow-1); }
.segbtn:active { transform: translateY(1px); }
button:disabled { color: var(--ink-300); background: var(--surface-2); cursor: wait; }
.head h1 { text-wrap: balance; }
button:focus-visible, summary:focus-visible, [tabindex]:focus-visible { outline: 2px solid var(--accent-500); outline-offset: 2px; }
.metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid var(--hairline-2); border-radius: 10px; background: var(--surface); }
.metrics > div { display: flex; flex-direction: column; gap: 12px; padding: 24px; min-width: 0; }
.metrics > div + div { border-left: 1px solid var(--hairline); }
.metrics span { font-size: 13px; color: var(--ink-500); }
.metrics strong { font-size: 28px; letter-spacing: -.035em; font-weight: 600; color: var(--ink-900); font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.metrics small { font-size: 11px; color: var(--ink-400); line-height: 1.6; }
.chart-placeholder { height: 250px; display: grid; place-items: center; font-size: 13px; color: var(--ink-400); border-radius: 6px; }
.load-error { display: flex; align-items: center; justify-content: space-between; gap: 12px; color: var(--danger); font-size: 13px; border: 1px solid var(--danger); padding: 8px 16px; border-radius: 6px; }
.table-placeholder { padding: 40px 24px; text-align: center; font-size: 13px; color: var(--ink-400); }
.table-scroll { width: 100%; overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: 13px; font-variant-numeric: tabular-nums; text-align: left; }
.model-table { min-width: 740px; }
th, td { padding: 16px 24px; border-bottom: 1px solid var(--hairline); }
th { font-weight: 500; }
thead th { background: var(--surface-2); color: var(--ink-500); font-size: 12px; white-space: nowrap; }
tbody th { width: 32%; }
td { color: var(--ink-700); white-space: nowrap; }
.model-table th:not(:first-child), .model-table td, .daily-table td, .daily-table th:last-child { text-align: right; }
.model-id { display: block; overflow-wrap: anywhere; color: var(--ink-900); }
th small { display: block; margin-top: 4px; font-size: 11px; color: var(--ink-500); }
.cost { font-weight: 600; color: var(--ink-900); }
tfoot { background: var(--surface-2); }
tfoot th, tfoot td { border-bottom: 0; }
.daily-details { margin-top: 16px; border-top: 1px solid var(--hairline); }
.daily-details summary { padding: 16px 0; cursor: pointer; font-size: 13px; color: var(--ink-600); }
.daily-scroll { max-height: 320px; overflow: auto; }
.daily-table th, .daily-table td { padding: 12px 16px; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; border: 0; }
@media (max-width: 900px) { .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .metrics > div:nth-child(3) { border-left: 0; } .metrics > div:nth-child(n + 3) { border-top: 1px solid var(--hairline); } }
@media (max-width: 480px) { .metrics > div { padding: 20px 16px; } .metrics strong { font-size: 23px; } .segbtn { min-height: 44px; } }
</style>
