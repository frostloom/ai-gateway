<script setup lang="ts">
/**
 * Sparkline —— 响应式迷你趋势线（纯 SVG，无 canvas 尺寸陷阱）
 *
 * 用 viewBox + preserveAspectRatio="none" 让它自适应容器宽度；
 * vector-effect="non-scaling-stroke" 保证线宽不随拉伸变形。
 */
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  points: number[]
  height?: number
  color?: string
  fill?: boolean
  /** 平滑曲线（默认开） */
  smooth?: boolean
}>(), { height: 40, color: 'var(--accent-500)', fill: true, smooth: true })

const VB_W = 300

const pts = computed(() => props.points.filter(v => v != null && !isNaN(v)))
const uid = `sp${Math.random().toString(36).slice(2, 9)}`

const bounds = computed(() => {
  const a = pts.value
  if (!a.length) return { min: 0, max: 1 }
  const min = Math.min(...a)
  const max = Math.max(...a)
  return { min, max: max === min ? min + 1 : max }
})

function xAt(i: number): number {
  const n = pts.value.length
  return n <= 1 ? 0 : (i / (n - 1)) * VB_W
}
function yAt(v: number): number {
  const { min, max } = bounds.value
  const h = props.height
  const pad = 4
  return h - pad - ((v - min) / (max - min)) * (h - pad * 2)
}

const line = computed(() => {
  const a = pts.value
  if (a.length < 2) return ''
  if (!props.smooth) {
    return a.map((v, i) => `${i ? 'L' : 'M'}${xAt(i).toFixed(1)},${yAt(v).toFixed(1)}`).join(' ')
  }
  const p = a.map((v, i) => ({ x: xAt(i), y: yAt(v) }))
  let d = `M${p[0].x.toFixed(1)},${p[0].y.toFixed(1)}`
  for (let i = 0; i < p.length - 1; i++) {
    const p0 = p[i - 1] ?? p[i]
    const p1 = p[i]
    const p2 = p[i + 1]
    const p3 = p[i + 2] ?? p2
    const c1x = p1.x + (p2.x - p0.x) / 6
    const c1y = p1.y + (p2.y - p0.y) / 6
    const c2x = p2.x - (p3.x - p1.x) / 6
    const c2y = p2.y - (p3.y - p1.y) / 6
    d += ` C${c1x.toFixed(1)},${c1y.toFixed(1)} ${c2x.toFixed(1)},${c2y.toFixed(1)} ${p2.x.toFixed(1)},${p2.y.toFixed(1)}`
  }
  return d
})

const area = computed(() => {
  if (!line.value) return ''
  return `${line.value} L${VB_W},${props.height} L0,${props.height} Z`
})

/** 末端点坐标（画一个小圆点强调"当前值"） */
const lastDot = computed(() => {
  const a = pts.value
  if (a.length < 2) return null
  return { x: xAt(a.length - 1), y: yAt(a[a.length - 1]) }
})
</script>

<template>
  <svg
    class="spark"
    :viewBox="`0 0 ${VB_W} ${height}`"
    preserveAspectRatio="none"
    :style="{ height: height + 'px' }"
  >
    <defs>
      <linearGradient :id="uid" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0%" :stop-color="color" stop-opacity=".2" />
        <stop offset="100%" :stop-color="color" stop-opacity="0" />
      </linearGradient>
    </defs>
    <path v-if="fill && area" :d="area" :fill="`url(#${uid})`" />
    <path
      v-if="line"
      :d="line"
      fill="none"
      :stroke="color"
      stroke-width="1.75"
      stroke-linejoin="round"
      stroke-linecap="round"
      vector-effect="non-scaling-stroke"
    />
    <circle
      v-if="lastDot"
      :cx="lastDot.x" :cy="lastDot.y" r="2.5"
      :fill="color" stroke="var(--surface)" stroke-width="1.5"
      vector-effect="non-scaling-stroke"
    />
  </svg>
</template>

<style scoped>
.spark { display: block; width: 100%; overflow: visible; }
</style>