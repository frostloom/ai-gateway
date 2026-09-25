<script setup lang="ts">
/**
 * MiniBars —— KPI 卡内的迷你柱
 *
 * 只为「离散计数」型序列服务（结算笔数、调用次数）。连续的金额用 Sparkline。
 * 纯 CSS：高度走 transform 的 scaleY 会失真，这里直接过渡 height（元素极小，成本可忽略）。
 */
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  points: number[]
  color?: string
  height?: number
  /** 显示条数 */
  take?: number
}>(), { color: 'var(--accent-500)', height: 34, take: 14 })

const series = computed(() => props.points.slice(-props.take))
const peak = computed(() => Math.max(1, ...series.value.filter(v => Number.isFinite(v))))
const bars = computed(() =>
  series.value.map(v => Math.max(3, (Math.max(0, v) / peak.value) * props.height)))
</script>

<template>
  <div class="bars" :style="{ height: height + 'px' }" aria-hidden="true">
    <i
      v-for="(h, i) in bars" :key="i"
      :style="{ height: h + 'px', background: color, opacity: 0.28 + (i / Math.max(1, bars.length - 1)) * 0.62 }"
    />
  </div>
</template>

<style scoped>
.bars {
  display: flex; align-items: flex-end; gap: 3px;
  width: 100%;
}
.bars i {
  flex: 1; min-width: 2px;
  border-radius: 3px 3px 2px 2px;
  transition: filter var(--t-fast);
}
.bars:hover i { filter: brightness(1.06); }
</style>
