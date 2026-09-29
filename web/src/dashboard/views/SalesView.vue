<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { salesApi } from '../api'
import type { SalesResp } from '../types'
import { money, fmt, zhCount, fmtDay } from '../../shared/utils/format'
import { toastErr } from '../../shared/utils/toast'
import StatCard from '../../shared/components/ui/StatCard.vue'
import Card from '../../shared/components/ui/Card.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import AreaChart from '../../shared/components/ui/AreaChart.vue'
import DataTable from '../../shared/components/ui/DataTable.vue'

function dstr(offsetDays: number): string {
  const d = new Date()
  d.setDate(d.getDate() + offsetDays)
  return d.toISOString().slice(0, 10)
}

const from = ref(dstr(-29))
const to = ref(dstr(0))
const data = ref<SalesResp | null>(null)
const loading = ref(false)

/** 双序列趋势：消耗 + 入账 */
const series = computed(() => {
  const days = data.value?.by_day ?? []
  return [
    { name: '消耗', points: days.map(d => ({ label: fmtDay(d.day), value: d.consumed_cent })) },
    { name: '入账', points: days.map(d => ({ label: fmtDay(d.day), value: d.topup_cent })) },
  ]
})

async function load() {
  loading.value = true
  try {
    data.value = await salesApi(from.value, to.value)
  } catch (e: any) {
    toastErr(e?.message || '查询失败')
  } finally {
    loading.value = false
  }
}
onMounted(load)

const MODEL_COLS = '1.5fr .85fr 1fr 1fr .7fr .95fr'
const VENDOR_COLS = '1.3fr .8fr 1fr 1fr'
</script>

<template>
  <div class="sales">
    <!-- 区间选择 -->
    <div class="bar">
      <div class="range">
        <label class="d">
          <span>起</span>
          <input v-model="from" type="date" class="din" />
        </label>
        <span class="dash">至</span>
        <label class="d">
          <span>止</span>
          <input v-model="to" type="date" class="din" />
        </label>
      </div>
      <BaseButton variant="primary" :loading="loading" @click="load">查询</BaseButton>
    </div>

    <div class="stats">
      <StatCard label="充值入账" :value="money(data?.summary.topup_cent)" :loading="loading" />
      <StatCard
        label="消耗总额（销售额）"
        :value="money(data?.summary.consumed_cent)"
        :loading="loading"
        tone="ok"
      />
      <StatCard label="结算单量" :value="fmt(data?.summary.settled_bills)" :loading="loading" />
      <StatCard label="活跃买家" :value="fmt(data?.summary.active_tenants)" :loading="loading" />
    </div>

    <Card title="消耗 / 入账趋势" :sub="`${from} 至 ${to}，单位：元`">
      <div v-if="loading" class="chart-skel" />
      <AreaChart
        v-else-if="series[0].points.length > 1"
        :series="series"
        :height="280"
        :format="(v: number) => money(v)"
      />
      <div v-else class="chart-empty">区间内暂无消耗</div>
    </Card>

    <div class="grid2">
      <Card title="按模型销售" :pad="false">
        <DataTable
          :cols="MODEL_COLS"
          :loading="loading"
          :empty="!data?.by_model.length"
          empty-text="暂无数据"
        >
          <template #head>
            <span>模型</span>
            <span>厂商</span>
            <span>输入 token</span>
            <span>输出 token</span>
            <span>调用</span>
            <span>销售额</span>
          </template>
          <div v-for="m in data?.by_model ?? []" :key="m.model_id" class="dt-row dt-num">
            <span class="mono clip" :title="m.model_id">{{ m.model_id }}</span>
            <span class="dt-muted">{{ m.vendor }}</span>
            <span>{{ zhCount(m.input_tokens) }}</span>
            <span>{{ zhCount(m.output_tokens) }}</span>
            <span class="dt-muted">{{ fmt(m.calls) }}</span>
            <span class="dt-strong">{{ money(m.consumed_cent) }}</span>
          </div>
        </DataTable>
      </Card>

      <Card title="按厂商" :pad="false">
        <DataTable
          :cols="VENDOR_COLS"
          :loading="loading"
          :empty="!data?.by_vendor.length"
          empty-text="暂无数据"
        >
          <template #head>
            <span>厂商</span>
            <span>调用</span>
            <span>活跃租户</span>
            <span>销售额</span>
          </template>
          <div v-for="v in data?.by_vendor ?? []" :key="v.vendor" class="dt-row dt-num">
            <span class="dt-strong">{{ v.vendor }}</span>
            <span class="dt-muted">{{ fmt(v.calls) }}</span>
            <span>{{ fmt(v.active_tenants) }}</span>
            <span class="dt-strong">{{ money(v.consumed_cent) }}</span>
          </div>
        </DataTable>
      </Card>
    </div>
  </div>
</template>

<style scoped>
.sales { display: flex; flex-direction: column; gap: var(--sp-5); }

.bar { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.range {
  display: flex; align-items: center; gap: var(--sp-3);
  padding: var(--sp-2) var(--sp-4);
  background: var(--paper);
  border: 1px solid var(--hairline);
  border-radius: var(--r-sm);
}
.d { display: inline-flex; align-items: center; gap: 7px; }
.d span { font-size: var(--fs-xs); color: var(--ink-400); }
.dash { font-size: var(--fs-xs); color: var(--ink-300); }
.din {
  height: 30px; padding: 0 var(--sp-2);
  border: 1px solid transparent;
  border-radius: var(--r-xs);
  background: var(--paper-2);
  font-size: var(--fs-xs);
  color: var(--ink-700);
}
.din:focus {
  outline: none;
  border-color: var(--accent-500);
  background: var(--paper);
}

.stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: var(--sp-4);
}

.chart-skel {
  height: 280px;
  border-radius: var(--r-sm);
  background: linear-gradient(90deg, var(--paper-3) 25%, var(--paper-2) 50%, var(--paper-3) 75%);
  background-size: 200% 100%;
  animation: shimmer 1.5s ease infinite;
}
@keyframes shimmer { from { background-position: 200% 0; } to { background-position: -200% 0; } }
.chart-empty {
  height: 280px;
  display: grid; place-items: center;
  color: var(--ink-400);
  font-size: var(--fs-sm);
}

.grid2 { display: grid; grid-template-columns: 1.55fr 1fr; gap: var(--sp-4); }
.clip { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

@media (max-width: 1080px) {
  .grid2 { grid-template-columns: 1fr; }
}
</style>