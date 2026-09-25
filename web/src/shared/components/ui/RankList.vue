<script setup lang="ts">
/**
 * RankList —— 排行
 *
 * 设计取舍：排行不需要 8 种随机颜色（那是仪表盘最典型的廉价感来源）。
 * 这里用「名次徽标（前三名着色）+ 单色占比条」表达顺序与量级。
 */
import { computed } from 'vue'

interface Row { name: string; sub?: string; value: number; icon?: any }

const props = withDefaults(defineProps<{
  rows: Row[]
  format?: (n: number) => string
  showBar?: boolean
  limit?: number
  /** 度量名（用于无障碍与空态文案），如 '消耗金额' */
  metric?: string
}>(), { showBar: true, limit: 6, metric: '数值' })

const top = computed(() => [...props.rows].sort((a, b) => b.value - a.value).slice(0, props.limit))
const max = computed(() => Math.max(1, ...top.value.map(r => Math.abs(r.value) || 0)))
const total = computed(() => top.value.reduce((s, r) => s + (Math.abs(r.value) || 0), 0))

const fmt = (v: number) => props.format ? props.format(v) : compact(v)

function compact(v: number): string {
  const a = Math.abs(v)
  if (a >= 1e8) return (v / 1e8).toFixed(1).replace(/\.0$/, '') + '亿'
  if (a >= 1e4) return (v / 1e4).toFixed(1).replace(/\.0$/, '') + '万'
  return String(Math.round(v))
}

const pct = (v: number) => total.value ? Math.round((Math.abs(v) / total.value) * 1000) / 10 : 0
</script>

<template>
  <div class="rank">
    <div v-for="(r, i) in top" :key="r.name" class="row">
      <span class="idx" :class="{ medal: i < 3 }">{{ i + 1 }}</span>

      <div class="mid">
        <span class="name" :title="r.name">
          <component v-if="r.icon" :is="r.icon" :size="14" :stroke-width="1.7" class="ic" />
          {{ r.name }}
        </span>
        <span v-if="r.sub" class="sub">{{ r.sub }}</span>
      </div>

      <div class="right">
        <span class="val num">{{ fmt(r.value) }}</span>
        <span class="share num">{{ pct(r.value) }}%</span>
      </div>

      <div v-if="showBar" class="track" aria-hidden="true">
        <i :style="{ transform: `scaleX(${(Math.abs(r.value) / max).toFixed(4)})` }" />
      </div>
    </div>

    <p v-if="!top.length" class="empty">暂无{{ metric }}数据</p>
  </div>
</template>

<style scoped>
.rank { display: flex; flex-direction: column; }

.row {
  position: relative;
  display: grid;
  grid-template-columns: 22px 1fr auto;
  align-items: center;
  gap: var(--sp-3);
  padding: var(--sp-3) var(--sp-2) calc(var(--sp-3) + 4px);
}
.row + .row { border-top: 1px solid var(--hairline); }

.idx {
  font-size: var(--fs-2xs);
  font-weight: 650;
  color: var(--ink-400);
  text-align: center;
  font-variant-numeric: tabular-nums;
}
.idx.medal {
  color: var(--accent-700);
  background: var(--accent-50);
  border-radius: var(--r-xs);
  height: 20px; line-height: 20px;
}

.mid { display: flex; flex-direction: column; min-width: 0; gap: 1px; }
.name {
  display: flex; align-items: center; gap: 6px;
  font-size: var(--fs-sm);
  font-weight: 570;
  color: var(--ink-800);
  letter-spacing: -.008em;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ic { color: var(--ink-400); flex: none; }
.sub { font-size: var(--fs-2xs); color: var(--ink-400); }

.right { display: flex; flex-direction: column; align-items: flex-end; gap: 1px; }
.val { font-size: var(--fs-sm); font-weight: 640; color: var(--ink-900); white-space: nowrap; }
.share { font-size: 10px; color: var(--ink-400); }

/* 占比条：贴在行底部，用 scaleX 过渡（GPU 安全） */
.track {
  position: absolute;
  left: var(--sp-2); right: var(--sp-2); bottom: 0;
  height: 2px;
  border-radius: var(--r-pill);
  background: var(--well);
  overflow: hidden;
}
.track i {
  display: block;
  height: 100%;
  transform-origin: left center;
  background: linear-gradient(90deg, var(--accent-500), var(--accent-400));
  border-radius: var(--r-pill);
  transition: transform var(--t-slow);
}

.empty {
  font-size: var(--fs-sm);
  color: var(--ink-400);
  text-align: center;
  padding: var(--sp-8) 0;
}
</style>
