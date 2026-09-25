<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { consumptionApi, meApi } from '../api'
import type { DayStat, ModelStat } from '../types'
import { money, fmtDay, zhCount } from '../../shared/utils/format'
import Card from '../../shared/components/ui/Card.vue'
import Sparkline from '../../shared/components/ui/Sparkline.vue'
import DataTable from '../../shared/components/ui/DataTable.vue'

const days = ref(7)
const daily = ref<DayStat[]>([])
const byModel = ref<ModelStat[]>([])
const loading = ref(true)

const total = computed(() => daily.value.reduce((s, d) => s + d.tokens, 0))
const maxDay = computed(() => Math.max(0, ...daily.value.map(d => d.tokens)))
const avg = computed(() => (daily.value.length ? total.value / daily.value.length : 0))

const MODEL_COLS = '1.5fr .8fr .6fr 1fr 1fr .85fr'

async function load() {
  loading.value = true
  try {
    const [c, m] = await Promise.all([
      consumptionApi(days.value),
      days.value === 7 ? meApi() : Promise.resolve(null),
    ])
    daily.value = c.items
    byModel.value = m?.consumption_by_model ?? []
  } finally {
    loading.value = false
  }
}
onMounted(load)

function switchDays(d: number) {
  days.value = d
  load()
}
</script>

<template>
  <div class="cons">
    <!-- 头部 -->
    <div class="head">
      <div>
        <h1 class="t">我的消费</h1>
        <p class="s num">
          近 {{ days }} 天共 <b>{{ money(total) }}</b>
          <span class="sep">·</span>
          日均 {{ money(avg) }}
        </p>
      </div>
      <div class="seg">
        <button
          v-for="d in [7, 30, 90]"
          :key="d"
          class="segbtn"
          :class="{ on: days === d }"
          @click="switchDays(d)"
        >{{ d }} 天</button>
      </div>
    </div>

    <!-- 趋势 -->
    <Card title="每日消耗趋势">
      <DataTable :loading="loading" :empty="!daily.length" empty-text="暂无消费记录" :skeleton-rows="3">
        <div class="trend">
          <Sparkline
            :points="daily.map(d => d.tokens)"
            :width="620"
            :height="110"
            color="var(--accent-500)"
          />
          <div class="cols">
            <div
              v-for="d in daily"
              :key="d.day"
              class="col"
              :title="`${fmtDay(d.day)} · ${money(d.tokens)}`"
            >
              <i class="cbar" :style="{ height: Math.max(3, (d.tokens / (maxDay || 1)) * 80) + 'px' }" />
              <span class="dl num">{{ fmtDay(d.day) }}</span>
            </div>
          </div>
        </div>
      </DataTable>
    </Card>

    <!-- 按模型 -->
    <Card v-if="byModel.length" title="按模型统计" sub="近 7 天" :pad="false">
      <DataTable :cols="MODEL_COLS" :loading="loading" :empty="!byModel.length">
        <template #head>
          <span>模型</span>
          <span>厂商</span>
          <span>调用</span>
          <span>输入 token</span>
          <span>输出 token</span>
          <span>消耗</span>
        </template>
        <div v-for="m in byModel" :key="m.model" class="dt-row dt-num">
          <span class="mono clip" :title="m.model">{{ m.model }}</span>
          <span class="dt-muted">{{ m.vendor }}</span>
          <span class="dt-muted">{{ m.calls }}</span>
          <span>{{ zhCount(m.input_tokens) }}</span>
          <span>{{ zhCount(m.output_tokens) }}</span>
          <span class="dt-strong">{{ money(m.cost_cent) }}</span>
        </div>
      </DataTable>
    </Card>
  </div>
</template>

<style scoped>
.cons { display: flex; flex-direction: column; gap: var(--sp-5); }

.head {
  display: flex; align-items: flex-end; justify-content: space-between;
  gap: var(--sp-4); flex-wrap: wrap;
}
.t {
  font-size: var(--fs-2xl);
  font-weight: 650;
  letter-spacing: -.026em;
}
.s { font-size: var(--fs-sm); color: var(--ink-400); margin-top: 5px; }
.s b { color: var(--ink-800); font-weight: 650; }
.sep { color: var(--ink-300); margin: 0 3px; }

/* 分段控件 */
.seg {
  display: flex; gap: 3px;
  padding: 3px;
  background: var(--paper-3);
  border-radius: var(--r-sm);
}
.segbtn {
  border: none; background: transparent;
  height: 28px; padding: 0 var(--sp-4);
  border-radius: var(--r-xs);
  font-size: var(--fs-xs); font-weight: 600;
  color: var(--ink-500);
  transition: all var(--t-fast);
}
.segbtn:hover { color: var(--ink-800); }
.segbtn.on {
  background: var(--paper);
  color: var(--ink-900);
  box-shadow: var(--shadow-1);
}

.trend {
  display: grid; grid-template-columns: 1.6fr 1fr;
  gap: var(--sp-5); align-items: center;
  padding: var(--sp-4) var(--sp-4) var(--sp-2);
}
.cols { display: flex; align-items: flex-end; gap: 4px; height: 116px; overflow: hidden; }
.col {
  flex: 1; min-width: 0;
  display: flex; flex-direction: column; align-items: center; justify-content: flex-end;
  gap: 4px;
}
.cbar {
  display: block; width: 100%; max-width: 20px;
  background: linear-gradient(180deg, var(--accent-400), var(--accent-600));
  border-radius: var(--r-xs) var(--r-xs) 1px 1px;
  transition: height var(--t-slow);
}
.dl { font-size: 9.5px; color: var(--ink-400); white-space: nowrap; }

.clip { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

@media (max-width: 760px) {
  .trend { grid-template-columns: 1fr; }
}
</style>