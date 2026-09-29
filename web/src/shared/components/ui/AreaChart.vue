<script setup lang="ts">
/**
 * 面积图（趋势）——纯 SVG，无依赖，白底友好。
 *
 * 相比 Sparkline（canvas 迷你线）的区别：带坐标轴、网格、hover 提示、双序列、
 * 渐变填充，用于「API 调用趋势」这类主图表位。
 */
import { ref, computed } from 'vue'

interface Point { label: string; value: number }
interface Series { name: string; points: Point[]; color?: string }

const props = withDefaults(defineProps<{
  series: Series[]
  height?: number
  /** 是否显示网格线 */
  grid?: boolean
  /** 数值格式化（hover 提示 / 轴标签） */
  format?: (n: number) => string
}>(), { height: 260, grid: true })

const PAD_L = 52
const PAD_R = 16
const PAD_T = 18
const PAD_B = 34

const W = 900 // 逻辑宽度（viewBox），实际由 CSS 拉伸
const H = computed(() => props.height)

const hover = ref<{ i: number; x: number } | null>(null)

/** Full data range: never visually truncate high-value days. */
const yMax = computed(() => Math.max(1, ...props.series.flatMap(s => s.points.map(p => p.value))) * 1.12)

const labels = computed(() => props.series[0]?.points.map(p => p.label) ?? [])
const n = computed(() => labels.value.length)

/** 第 i 个点的 X 坐标（plot 区） */
function xAt(i: number): number {
  const count = n.value
  if (count <= 1) return PAD_L
  return PAD_L + (i / (count - 1)) * (W - PAD_L - PAD_R)
}
/** 值 → Y 坐标（削顶到 PAD_T） */
function yAt(v: number): number {
  const plotH = H.value - PAD_T - PAD_B
  const clamped = Math.min(v, yMax.value)
  return PAD_T + plotH - (clamped / yMax.value) * plotH
}

/** Y 轴刻度（4 档） */
const yTicks = computed(() => {
  const out: { v: number; y: number }[] = []
  for (let k = 0; k <= 4; k++) {
    const v = (yMax.value / 4) * k
    out.push({ v, y: yAt(v) })
  }
  return out
})

/**
 * 单调三次插值（Catmull-Rom 风格）：把折线变成柔顺曲线，
 * 比直线更有质感，又不会像普通贝塞尔那样过冲。
 */
function smoothPath(points: { value: number }[]): string {
  const pts = points.map((p, i) => ({ x: xAt(i), y: yAt(p.value) }))
  if (pts.length < 2) return ''
  if (pts.length === 2) return `M${pts[0].x},${pts[0].y} L${pts[1].x},${pts[1].y}`
  let d = `M${pts[0].x.toFixed(1)},${pts[0].y.toFixed(1)}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i - 1] ?? pts[i]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[i + 2] ?? p2
    // 张力 0.5 的 Catmull-Rom → 三次贝塞尔控制点
    const c1x = p1.x + (p2.x - p0.x) / 6
    const lo = Math.min(p1.y, p2.y), hi = Math.max(p1.y, p2.y)
    const c1y = Math.max(lo, Math.min(hi, p1.y + (p2.y - p0.y) / 6))
    const c2x = p2.x - (p3.x - p1.x) / 6
    const c2y = Math.max(lo, Math.min(hi, p2.y - (p3.y - p1.y) / 6))
    d += ` C${c1x.toFixed(1)},${c1y.toFixed(1)} ${c2x.toFixed(1)},${c2y.toFixed(1)} ${p2.x.toFixed(1)},${p2.y.toFixed(1)}`
  }
  return d
}

/** 折线路径（平滑曲线） */
function linePath(points: Point[]): string {
  return smoothPath(points)
}
/** 面积路径（沿平滑曲线闭合到底部） */
function areaPath(points: Point[]): string {
  const base = (H.value - PAD_B).toFixed(1)
  const line = smoothPath(points)
  return `${line} L${xAt(points.length - 1).toFixed(1)},${base} L${xAt(0).toFixed(1)},${base} Z`
}

const fmt = (v: number) => props.format ? props.format(v) : compact(v)

function compact(v: number): string {
  const a = Math.abs(v)
  if (a >= 1e8) return (v / 1e8).toFixed(1).replace(/\.0$/, '') + '亿'
  if (a >= 1e4) return (v / 1e4).toFixed(1).replace(/\.0$/, '') + '万'
  return String(Math.round(v))
}

/** X 轴标签抽稀：最多显示 8 个 */
const xLabels = computed(() => {
  const total = n.value
  if (total === 0) return []
  const step = Math.max(1, Math.ceil(total / 8))
  const out: { i: number; label: string }[] = []
  for (let i = 0; i < total; i += step) out.push({ i, label: labels.value[i] })
  return out
})

const palette = ['var(--accent-500)', 'var(--ink-400)', 'var(--warn)']

function onMove(e: MouseEvent) {
  const svg = e.currentTarget as SVGSVGElement
  const rect = svg.getBoundingClientRect()
  const relX = ((e.clientX - rect.left) / rect.width) * W
  const count = n.value
  if (count === 0) { hover.value = null; return }
  const t = (relX - PAD_L) / (W - PAD_L - PAD_R)
  const i = Math.round(Math.max(0, Math.min(1, t)) * (count - 1))
  hover.value = { i, x: xAt(i) }
}

function hoverRows() {
  if (!hover.value) return []
  return props.series.map((s, si) => ({
    name: s.name,
    color: s.color ?? palette[si % palette.length],
    value: s.points[hover.value!.i]?.value ?? 0,
    y: yAt(s.points[hover.value!.i]?.value ?? 0),
  }))
}
</script>

<template>
  <div class="area">
    <svg
      :viewBox="`0 0 ${W} ${H}`"
      preserveAspectRatio="none"
      class="svg"
      :style="{ height: H + 'px' }"
      @mousemove="onMove"
      @mouseleave="hover = null"
    >
      <defs>
        <linearGradient v-for="(s, si) in series" :key="'g' + si" :id="`ag${si}`" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" :stop-color="s.color ?? palette[si % palette.length]" stop-opacity=".22" />
          <stop offset="100%" :stop-color="s.color ?? palette[si % palette.length]" stop-opacity="0" />
        </linearGradient>
      </defs>

      <!-- 网格 + Y 轴刻度 -->
      <g v-if="grid">
        <line
          v-for="(t, i) in yTicks" :key="'gy' + i"
          :x1="PAD_L" :x2="W - PAD_R" :y1="t.y" :y2="t.y"
          class="gridline"
        />
      </g>
      <text
        v-for="(t, i) in yTicks" :key="'ty' + i"
        :x="PAD_L - 8" :y="t.y + 4"
        class="axis" text-anchor="end"
      >{{ fmt(t.v) }}</text>

      <!-- 面积 + 折线 -->
      <g v-for="(s, si) in series" :key="'s' + si">
        <path :d="areaPath(s.points)" :fill="`url(#ag${si})`" />
        <path
          :d="linePath(s.points)"
          fill="none"
          :stroke="s.color ?? palette[si % palette.length]"
          stroke-width="2"
          stroke-linejoin="round"
          stroke-linecap="round"
          vector-effect="non-scaling-stroke"
        />
      </g>

      <!-- hover 十字线 + 圆点 -->
      <g v-if="hover">
        <line :x1="hover.x" :x2="hover.x" :y1="PAD_T" :y2="H - PAD_B" class="crosshair" />
        <circle
          v-for="(r, ri) in hoverRows()" :key="'h' + ri"
          :cx="hover.x" :cy="r.y" r="3.5"
          :fill="r.color" stroke="#fff" stroke-width="1.5"
        />
      </g>

      <!-- X 轴标签 -->
      <text
        v-for="l in xLabels" :key="'x' + l.i"
        :x="xAt(l.i)" :y="H - 10"
        class="axis" text-anchor="middle"
      >{{ l.label }}</text>
    </svg>

    <!-- hover 提示 -->
    <div
      v-if="hover"
      class="tip"
      :style="{ left: `${(hover.x / W) * 100}%` }"
    >
      <p class="tlabel">{{ labels[hover.i] }}</p>
      <p v-for="(r, ri) in hoverRows()" :key="'tr' + ri" class="trow">
        <span class="tdot" :style="{ background: r.color }" />
        <span class="tname">{{ r.name }}</span>
        <span class="tval num">{{ fmt(r.value) }}</span>
      </p>
    </div>
  </div>
</template>

<style scoped>
.area { position: relative; width: 100%; }
.svg { display: block; width: 100%; overflow: visible; }
.gridline { stroke: var(--hairline); stroke-width: 1; vector-effect: non-scaling-stroke; }
.crosshair { stroke: var(--ink-300); stroke-width: 1; stroke-dasharray: 3 4; vector-effect: non-scaling-stroke; }
.axis {
  font-size: 11px;
  fill: var(--ink-400);
  font-family: var(--font-sans);
  font-variant-numeric: tabular-nums;
}

.tip {
  position: absolute; top: 8px;
  transform: translateX(-50%);
  pointer-events: none;
  background: rgb(255 255 255 / .92);
  backdrop-filter: blur(14px) saturate(180%);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-pop), inset 0 0 0 1px rgb(17 25 23 / .05);
  padding: 9px 12px;
  min-width: 132px;
  z-index: 3;
}
.tlabel {
  font-size: var(--fs-2xs);
  color: var(--ink-400);
  margin-bottom: 6px;
  padding-bottom: 5px;
  border-bottom: 1px solid var(--hairline-2);
}
.trow { display: flex; align-items: center; gap: 6px; font-size: var(--fs-2xs); }
.tdot { width: 7px; height: 7px; border-radius: 2px; flex: none; }
.tname { color: var(--ink-500); }
.tval { margin-left: auto; font-weight: 640; color: var(--ink-900); }
</style>