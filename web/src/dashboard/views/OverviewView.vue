<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import {
  IconWallet, IconTrendingUp, IconReceipt2, IconShieldCheck,
} from '@tabler/icons-vue'
import { overviewApi, healthApi, salesApi } from '../api'
import type { OverviewResp, HealthResp, TenantStat, SalesResp } from '../types'
import { money, fmt, fmtDay } from '../../shared/utils/format'
import KpiCard from '../../shared/components/ui/KpiCard.vue'
import AreaChart from '../../shared/components/ui/AreaChart.vue'
import RankList from '../../shared/components/ui/RankList.vue'
import Card from '../../shared/components/ui/Card.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import DataTable from '../../shared/components/ui/DataTable.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'

const ov = ref<OverviewResp | null>(null)
const health = ref<HealthResp | null>(null)
const sales = ref<SalesResp | null>(null)
const loading = ref(true)
let timer: number | null = null
const days = ref(14)
const errors = ref<string[]>([])
const refreshing = ref(false)
const updatedAt = ref('')
let requestId = 0
function calendarDay(d: Date) { return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}` }
const range = computed(() => { const to = new Date(); const from = new Date(); from.setDate(from.getDate() - days.value + 1); return { from: calendarDay(from), to: calendarDay(to) } })
const dailySeries = computed(() => {
  if (!sales.value) return []
  const recorded = new Map(sales.value.by_day.map(d => [d.day.slice(0, 10), d]))
  return Array.from({ length: days.value }, (_, i) => {
    const date = new Date(`${range.value.from}T12:00:00`); date.setDate(date.getDate() + i)
    const day = calendarDay(date)
    return recorded.get(day) ?? { day, consumed_cent: 0, topup_cent: 0 }
  })
})

const byTenant = (key: keyof TenantStat) =>
  (ov.value?.tenants ?? []).reduce((s, t) => s + ((t[key] as number) || 0), 0)

const totalBalance = computed(() => byTenant('balance'))
const totalPending = computed(() => byTenant('pending'))
const inconsistentCount = computed(() => ov.value?.tenants.filter(t => !t.consistent).length ?? 0)
const upCount = computed(() => Object.values(health.value?.services ?? {}).filter(s => s.ok).length)
const svcCount = computed(() => Object.keys(health.value?.services ?? {}).length)
const tenantCount = computed(() => ov.value?.tenants.length ?? 0)

const svcLabel: Record<string, string> = {
  billing: 'billing', router: 'router', reconciler: 'reconciler', agent: 'agent',
}

// ---------- 图表数据 ----------

/** 近 14 天消耗趋势（面积图双序列：消耗 + 入账） */
const trendSeries = computed(() => {
  const daily = dailySeries.value
  return [
    { name: '消耗', points: daily.map(d => ({ label: fmtDay(d.day), value: d.consumed_cent })) },
    { name: '入账', points: daily.map(d => ({ label: fmtDay(d.day), value: d.topup_cent })) },
  ]
})

const hasTrend = computed(() => (trendSeries.value[0]?.points.length ?? 0) > 1)

/** KPI 迷你趋势：近 14 天消耗 */
const spendTrend = computed(() => dailySeries.value.map(d => d.consumed_cent))
/** KPI 迷你趋势：近 14 天入账 */
const topupTrend = computed(() => dailySeries.value.map(d => d.topup_cent))

/** 模型排行（按销售额） */
const modelRank = computed(() =>
  (sales.value?.by_model ?? []).map(m => ({
    name: m.name || m.model_id,
    sub: m.vendor,
    value: m.consumed_cent,
  }))
)

/** 厂商排行 */
const vendorRank = computed(() =>
  (sales.value?.by_vendor ?? []).map(v => ({ name: v.vendor, sub: `${fmt(v.calls)} 次调用`, value: v.consumed_cent }))
)

/** 一致性：正常租户 vs 异常租户（占比条） */
const consistency = computed(() => {
  const total = tenantCount.value
  const bad = inconsistentCount.value
  return { good: total - bad, bad, total, pct: total ? Math.round(((total - bad) / total) * 100) : 0 }
})

/** 半程环比：把近 14 天切成两段，比较后 7 天与前 7 天，给出真实涨跌而不是装饰性进度条 */
function halfDelta(series: number[]): number | null {
  if (series.length < 4 || series.length % 2 !== 0) return null
  const mid = Math.floor(series.length / 2)
  const prev = series.slice(0, mid).reduce((s, v) => s + (v || 0), 0)
  const next = series.slice(mid).reduce((s, v) => s + (v || 0), 0)
  if (prev === 0) return next === 0 ? 0 : null
  return ((next - prev) / prev) * 100
}

const spendDelta = computed(() => halfDelta(spendTrend.value))
const topupDelta = computed(() => halfDelta(topupTrend.value))

/** 销售汇总（后端可能返回空对象/缺字段，统一兜底，避免模板解引用炸掉） */
const sum = computed(() => ({
  consumed: sales.value?.summary?.consumed_cent ?? 0,
  topup: sales.value?.summary?.topup_cent ?? 0,
  settledBills: sales.value?.summary?.settled_bills ?? 0,
  activeTenants: sales.value?.summary?.active_tenants ?? 0,
}))

const COLS = '1.1fr 1fr 1.15fr 1.15fr .9fr .8fr .7fr .95fr'

async function load() {
  const id = ++requestId
  refreshing.value = true
  const selected = { ...range.value }
  const results = await Promise.allSettled([overviewApi(), healthApi(), salesApi(selected.from, selected.to)])
  if (id !== requestId) return
  errors.value = []
  const [o, h, s] = results
  if (o.status === 'fulfilled') ov.value = o.value
  else { ov.value = null; errors.value.push('租户账本加载失败') }
  if (h.status === 'fulfilled') health.value = h.value
  else { health.value = null; errors.value.push('服务健康检查失败') }
  if (s.status === 'fulfilled' && s.value?.summary && Array.isArray(s.value.by_day)) sales.value = s.value
  else { sales.value = null; errors.value.push('销售统计加载失败') }
  updatedAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  loading.value = false
  refreshing.value = false
}
function changeRange(value: number) {
  if (value === days.value) return
  days.value = value
  sales.value = null
  loading.value = true
  load()
}

onMounted(() => {
  load()
  timer = window.setInterval(load, 30000) // 30s 刷新（含趋势查询，不必 3s）
})
onBeforeUnmount(() => { requestId++; if (timer) clearInterval(timer) })
</script>

<template>
  <div class="overview">
    <div class="analysis-head"><div><h2>运营概览</h2><p>{{ range.from }} 至 {{ range.to }}<span v-if="updatedAt">，更新于 {{ updatedAt }}</span></p></div><div class="range-controls"><div class="range-tabs" aria-label="统计时间范围"><button v-for="d in [7, 14, 30]" :key="d" :aria-pressed="days === d" :class="{ selected: days === d }" @click="changeRange(d)">{{ d }} 天</button></div><BaseButton variant="ghost" :disabled="refreshing" @click="load">{{ refreshing ? '刷新中' : '刷新' }}</BaseButton></div></div>
    <div v-if="errors.length" class="load-error" role="alert">{{ errors.join('，') }}。请刷新重试。</div>
    <!-- 顶部：关键指标 + 趋势 -->
    <div class="kpis">
      <KpiCard
        label="总余额（账本口径）"
        :value="ov ? money(totalBalance) : '—'"
        :loading="loading"
        :icon="IconWallet"
        tone="default"
        :sub="ov ? `${money(totalPending)} 在途` : '数据不可用'"
        chart="none"
      />
      <KpiCard
        :label="`近 ${days} 天消耗`"
        :value="sales ? money(sum.consumed) : '—'"
        :loading="loading"
        :icon="IconTrendingUp"
        tone="ok"
        :delta-pct="spendDelta"
        :sub="sales ? `${fmt(sum.settledBills)} 笔结算${days % 2 === 0 ? '，较前半程' : ''}` : '数据不可用'"
        :trend="spendTrend"
        chart="spark"
      />
      <KpiCard
        :label="`近 ${days} 天入账`"
        :value="sales ? money(sum.topup) : '—'"
        :loading="loading"
        :icon="IconReceipt2"
        tone="info"
        :delta-pct="topupDelta"
        :sub="sales ? `${fmt(sum.activeTenants)} 个活跃租户${days % 2 === 0 ? '，较前半程' : ''}` : '数据不可用'"
        :trend="topupTrend"
        chart="bars"
      />
      <KpiCard
        label="账本一致性"
        :value="!ov || !tenantCount ? '—' : inconsistentCount ? `${inconsistentCount} 个不一致` : '全部一致'"
        :loading="loading"
        :icon="IconShieldCheck"
        :tone="inconsistentCount ? 'danger' : 'ok'"
        :sub="ov ? `${tenantCount} 个租户` : '数据不可用'"
        chart="none"
      />
    </div>

    <!-- 主图 + 排行 -->
    <div class="main-grid">
      <Card title="消耗 / 入账趋势" :sub="`近 ${days} 天，金额以人民币计`">
        <div v-if="loading" class="chart-skel skeleton" />
        <AreaChart
          v-else-if="hasTrend"
          :series="trendSeries"
          :height="268"
          :format="(v: number) => money(v)"
        />
        <div v-else class="chart-empty">{{ sales ? '暂无趋势数据' : '销售统计不可用' }}</div>
      </Card>

      <Card title="模型销售排行" :sub="`近 ${days} 天，按消耗金额`">
        <RankList :rows="modelRank" :limit="6" :format="(v: number) => money(v)" />
      </Card>
    </div>

    <!-- 服务健康 + 厂商 -->
    <div class="main-grid reverse">
      <Card title="服务健康" :sub="`${upCount}/${svcCount} 正常`">
        <div class="services">
          <div v-for="(v, k) in health?.services ?? {}" :key="k" class="svc" :class="{ bad: !v.ok }">
            <span class="sdot" />
            <div class="sinfo">
              <span class="sname">{{ svcLabel[k] ?? k }}</span>
              <span class="sstate">{{ v.ok ? '运行中' : (v.error || '不可达') }}</span>
            </div>
          </div>
          <div v-if="!health" class="svc"><div class="sinfo"><span class="sname">{{ loading ? '加载中' : '健康数据不可用' }}</span></div></div>
        </div>

        <!-- 一致性占比条 -->
        <div v-if="ov && tenantCount" class="consist">
          <div class="cbar-top">
            <span class="clabel">账本 / 投影一致性</span>
            <span class="cval num">{{ consistency.pct }}%</span>
          </div>
          <div class="cbar">
            <i class="good" :style="{ width: consistency.pct + '%' }" />
            <i v-if="consistency.bad" class="bad" :style="{ width: (100 - consistency.pct) + '%' }" />
          </div>
          <p class="cnote">
            {{ consistency.bad ? `${consistency.bad} 个租户不一致，需排查` : '所有租户账本与 Redis 投影一致' }}
          </p>
        </div>
      </Card>

      <Card title="厂商销售排行" :sub="`近 ${days} 天，按消耗金额`">
        <RankList :rows="vendorRank" :limit="6" :format="(v: number) => money(v)" />
      </Card>
    </div>

    <!-- 租户余额明细 -->
    <Card title="租户余额" sub="账本口径 vs Redis 投影，30s 自动刷新">
      <DataTable
        :cols="COLS"
        :loading="loading"
        :empty="!(ov?.tenants ?? []).length"
        empty-text="还没有租户"
      >
        <template #head>
          <span>租户</span>
          <span>初始额度</span>
          <span>余额（账本）</span>
          <span>Redis 投影</span>
          <span>已消耗</span>
          <span>在途</span>
          <span>账单数</span>
          <span>一致性</span>
        </template>

        <div v-for="t in ov?.tenants ?? []" :key="t.id" class="dt-row dt-num">
          <span class="dt-strong">{{ t.name }}</span>
          <span class="dt-muted">{{ money(t.initial_quota) }}</span>
          <span class="dt-strong">{{ money(t.balance) }}</span>
          <span :class="t.redis_balance !== t.balance ? 'mismatch' : 'dt-muted'">
            {{ money(t.redis_balance) }}
          </span>
          <span>{{ money(t.settled) }}</span>
          <span>{{ money(t.pending) }}</span>
          <span>{{ fmt(t.bill_count) }}</span>
          <span>
            <Tag :tone="t.consistent ? 'ok' : 'danger'" dot>
              {{ t.consistent ? '一致' : '不一致' }}
            </Tag>
          </span>
        </div>
      </DataTable>
    </Card>
  </div>
</template>

<style scoped>
.overview { display: flex; flex-direction: column; gap: var(--sp-5); }

.kpis {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  gap: var(--sp-4);
}

/* 主图与排行：图表宽、排行窄 */
.main-grid {
  display: grid;
  grid-template-columns: 1.9fr 1fr;
  gap: var(--sp-4);
}
.main-grid.reverse { grid-template-columns: 1fr 1.9fr; }

@media (max-width: 1180px) {
  .main-grid, .main-grid.reverse { grid-template-columns: 1fr; }
}

.chart-skel {
  height: 268px;
  border-radius: var(--r-sm);
}
.chart-empty {
  height: 268px;
  display: grid; place-items: center;
  color: var(--ink-400);
  font-size: var(--fs-sm);
}

/* ---------- 服务健康 ---------- */
.services {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(132px, 1fr));
  gap: 10px;
}
.svc {
  display: flex; align-items: center; gap: 9px;
  padding: var(--sp-3) var(--sp-3);
  background: var(--well);
  border-radius: var(--r-md);
  box-shadow: var(--inset-well);
}
.sdot {
  width: 7px; height: 7px; border-radius: 50%;
  background: var(--ok);
  box-shadow: 0 0 0 3.5px var(--ok-bg);
  flex: none;
}
.svc.bad .sdot { background: var(--danger); box-shadow: 0 0 0 3.5px var(--danger-bg); }
.sinfo { display: flex; flex-direction: column; min-width: 0; }
.sname {
  font-size: var(--fs-xs); font-weight: 600;
  color: var(--ink-700);
  font-family: var(--font-mono);
}
.sstate { font-size: var(--fs-2xs); color: var(--ink-400); }
.svc.bad .sstate { color: var(--danger); }

/* ---------- 一致性 ---------- */
.consist {
  margin-top: var(--sp-6);
  padding-top: var(--sp-5);
  border-top: 1px solid var(--hairline-2);
}
.cbar-top {
  display: flex; align-items: baseline; justify-content: space-between;
  margin-bottom: 9px;
}
.clabel { font-size: var(--fs-xs); color: var(--ink-500); font-weight: 540; }
.cval { font-size: var(--fs-lg); font-weight: 670; color: var(--ok); letter-spacing: -.02em; }
.cbar {
  display: flex;
  height: 6px;
  border-radius: var(--r-pill);
  overflow: hidden;
  background: var(--well);
  box-shadow: var(--inset-well);
}
.cbar i { display: block; height: 100%;  }
.cbar i.good { background: var(--accent-500); }
.cbar i.bad { background: var(--danger); }
.cnote { font-size: var(--fs-2xs); color: var(--ink-400); margin-top: 8px; }

.mismatch { color: var(--danger); font-weight: 650; }
.analysis-head, .range-controls { display: flex; align-items: center; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
.analysis-head h2 { font-size: 20px; font-weight: 600; }
.analysis-head p { margin-top: 6px; font-size: 12px; color: var(--ink-500); }
.range-tabs { display: flex; padding: 3px; border: 1px solid var(--hairline); background: var(--surface-2); border-radius: 7px; }
.range-tabs button { height: 34px; padding: 0 14px; border: 0; background: transparent; color: var(--ink-500); font-size: 12px; border-radius: 4px; }
.range-tabs button.selected { color: var(--ink-900); background: var(--surface); box-shadow: var(--shadow-1); }
.range-tabs button:focus-visible { outline: 2px solid var(--accent-500); outline-offset: 2px; }
.load-error { color: var(--danger); border: 1px solid var(--danger); border-radius: 6px; padding: 12px 16px; font-size: 13px; }
@media (max-width: 540px) { .kpis { grid-template-columns: minmax(0, 1fr); } .main-grid { min-width: 0; } }
</style>