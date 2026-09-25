<script setup lang="ts">
/**
 * KpiCard —— 指标卡
 *
 * 结构：白面 + 受光顶边 + 右上圆形图标凹槽 + 底部趋势图。
 * 指标卡不套双圈壳（避免仪表盘变成"卡中卡"），改由外壳的阶梯阴影分层。
 */
import { computed } from 'vue'
import MiniBars from './MiniBars.vue'
import Sparkline from './Sparkline.vue'
import { IconArrowUpRight, IconArrowDownRight, IconMinus } from '@tabler/icons-vue'

const props = withDefaults(defineProps<{
  label: string
  value?: string | number
  /** 环比百分比，如 12.4 / -3.1 */
  deltaPct?: number | null
  /** 涨跌语义：up 好 / down 坏（由调用方决定，避免"消耗涨了算红还是绿"的歧义） */
  deltaGood?: boolean
  sub?: string
  icon?: any
  tone?: 'default' | 'ok' | 'warn' | 'danger' | 'info'
  trend?: number[]
  chart?: 'spark' | 'bars' | 'none'
  loading?: boolean
}>(), { tone: 'default', chart: 'spark' })

const toneColor = computed(() => ({
  default: 'var(--accent-500)',
  ok: 'var(--ok)',
  warn: 'var(--warn)',
  danger: 'var(--danger)',
  info: 'var(--info)',
}[props.tone]))

const hasTrend = computed(() => (props.trend?.length ?? 0) > 1)
const hasDelta = computed(() => typeof props.deltaPct === 'number' && Number.isFinite(props.deltaPct))

const deltaTone = computed(() => {
  if (!hasDelta.value) return 'flat'
  const v = props.deltaPct as number
  if (Math.abs(v) < 0.05) return 'flat'
  return props.deltaGood === false ? 'warn' : 'good'
})

const deltaIcon = computed(() => {
  if (!hasDelta.value) return IconMinus
  const v = props.deltaPct as number
  if (Math.abs(v) < 0.05) return IconMinus
  return v > 0 ? IconArrowUpRight : IconArrowDownRight
})

const deltaText = computed(() =>
  hasDelta.value ? `${Math.abs(props.deltaPct as number).toFixed(1)}%` : '')
</script>

<template>
  <article class="kpi" :class="[tone, { loading }]">
    <div class="top">
      <div class="info">
        <p class="label">{{ label }}</p>
        <p v-if="loading" class="ph skeleton" />
        <p v-else class="value num">{{ value ?? '-' }}</p>
      </div>

      <div
        v-if="icon"
        class="iconSlot"
        :style="{ color: toneColor, background: `color-mix(in srgb, ${toneColor} 10%, transparent)` }"
      >
        <component :is="icon" :size="17" :stroke-width="1.6" />
      </div>
    </div>

    <div v-if="!loading" class="foot">
      <div class="meta">
        <span v-if="hasDelta" class="delta" :class="deltaTone">
          <component :is="deltaIcon" :size="12" :stroke-width="2.2" />
          {{ deltaText }}
        </span>
        <span v-if="sub" class="sub">{{ sub }}</span>
      </div>

      <div v-if="chart !== 'none' && hasTrend" class="trend">
        <MiniBars v-if="chart === 'bars'" :points="trend!" :color="toneColor" />
        <Sparkline v-else :points="trend!" :height="34" :color="toneColor" />
      </div>
    </div>
    <div v-else class="foot"><p class="ph sm skeleton" /></div>
  </article>
</template>

<style scoped>
.kpi {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
  padding: var(--sp-6) var(--sp-6) var(--sp-5);
  background: var(--surface);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-raise), var(--inset-hi);
  transition: box-shadow var(--t-base), transform var(--t-base);
}
.kpi:hover { box-shadow: var(--shadow-float), var(--inset-hi); transform: translateY(-2px); }

/* 左侧语义色条 */
.kpi::before {
  content: '';
  position: absolute; left: 0; top: var(--sp-6); bottom: var(--sp-6);
  width: 3px;
  border-radius: 0 var(--r-pill) var(--r-pill) 0;
  background: var(--ink-200);
}
.kpi.ok::before { background: var(--ok); }
.kpi.warn::before { background: var(--warn); }
.kpi.danger::before { background: var(--danger); }
.kpi.info::before { background: var(--info); }
.kpi.default::before { background: var(--accent-500); }

.top { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-4); }
.info { min-width: 0; padding-left: 8px; }

.label {
  font-size: var(--fs-xs);
  color: var(--ink-500);
  font-weight: 540;
}
.value {
  font-size: var(--fs-2xl);
  font-weight: 650;
  letter-spacing: -.035em;
  line-height: 1.05;
  margin-top: 12px;
  color: var(--ink-900);
}

/* 图标凹槽：与 Island 按钮同源的圆形 */
.iconSlot {
  flex: none;
  width: 38px; height: 38px;
  border-radius: 50%;
  display: grid; place-items: center;
  box-shadow: inset 0 0 0 1px rgb(255 255 255 / .6);
  transition: transform var(--t-spring);
}
.kpi:hover .iconSlot { transform: scale(1.07) rotate(-5deg); }

.foot { display: flex; align-items: flex-end; justify-content: space-between; gap: var(--sp-4); }
.meta { display: flex; flex-direction: column; gap: 7px; padding-left: 8px; min-width: 0; }

.delta {
  display: inline-flex; align-items: center; gap: 2px;
  align-self: flex-start;
  height: 21px; padding: 0 8px 0 6px;
  border-radius: var(--r-pill);
  font-size: var(--fs-2xs);
  font-weight: 640;
  font-variant-numeric: tabular-nums;
}
.delta.good { color: var(--ok); background: var(--ok-bg); }
.delta.warn { color: var(--danger); background: var(--danger-bg); }
.delta.flat { color: var(--ink-500); background: var(--well); }

.sub { font-size: var(--fs-2xs); color: var(--ink-400); }

.trend { flex: none; width: 92px; }
.trend :deep(svg) { display: block; width: 100%; }

.ph { display: inline-block; width: 92px; height: 28px; margin-top: 12px; }
.ph.sm { width: 120px; height: 16px; margin: 0; }
.kpi.loading { opacity: .8; }
</style>
