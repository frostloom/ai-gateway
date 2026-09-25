<script setup lang="ts">
/**
 * Pagination —— 分段式翻页
 * 数字按钮无描边：常态透明，当前页是强调色药丸，悬停才浮起。
 */
import { computed } from 'vue'

const props = withDefaults(defineProps<{ total: number; page: number; size?: number }>(), { size: 20 })
const emit = defineEmits<{ (e: 'change', page: number): void }>()

const pages = computed(() => Math.max(1, Math.ceil(props.total / props.size)))
const range = computed(() => {
  const cur = props.page
  const p = pages.value
  if (p <= 7) return Array.from({ length: p }, (_, i) => i + 1)
  const s = new Set<number>([1, p, cur - 1, cur, cur + 1])
  const arr = [...s].filter(x => x >= 1 && x <= p).sort((a, b) => a - b)
  const out: number[] = []
  let prev = 0
  for (const x of arr) {
    if (x - prev > 1) out.push(-1)
    out.push(x)
    prev = x
  }
  return out
})
</script>

<template>
  <div v-if="total > 0" class="pg">
    <span class="info">共 {{ total.toLocaleString() }} 条 · 第 {{ page }} / {{ pages }} 页</span>
    <div class="btnrow">
      <button class="pbtn nav" :disabled="page <= 1" aria-label="上一页" @click="emit('change', page - 1)">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5l-7 7 7 7" /></svg>
      </button>
      <template v-for="(p, i) in range" :key="i">
        <span v-if="p === -1" class="dots">…</span>
        <button v-else class="pbtn" :class="{ on: p === page }" @click="emit('change', p)">{{ p }}</button>
      </template>
      <button class="pbtn nav" :disabled="page >= pages" aria-label="下一页" @click="emit('change', page + 1)">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5l7 7-7 7" /></svg>
      </button>
    </div>
  </div>
</template>

<style scoped>
.pg {
  display: flex; align-items: center; justify-content: space-between;
  gap: var(--sp-3); margin-top: var(--sp-5); flex-wrap: wrap;
}
.info { font-size: var(--fs-xs); color: var(--ink-400); }
.btnrow { display: flex; gap: 3px; align-items: center; }
.pbtn {
  min-width: 32px; height: 32px; padding: 0 9px;
  display: grid; place-items: center;
  border: none;
  border-radius: var(--r-pill);
  background: transparent; color: var(--ink-600);
  font-size: var(--fs-xs); font-weight: 580;
  transition: background var(--t-fast), color var(--t-fast), box-shadow var(--t-base), transform var(--t-spring);
}
.pbtn:hover:not(:disabled):not(.on) {
  background: var(--surface); color: var(--ink-900);
  box-shadow: var(--shadow-raise);
}
.pbtn:active:not(:disabled) { transform: scale(.94); }
.pbtn.on {
  background: var(--accent-600);
  color: #fff;
  box-shadow: var(--shadow-accent);
}
.pbtn:disabled { opacity: .3; cursor: not-allowed; }
.dots { color: var(--ink-400); padding: 0 3px; font-size: var(--fs-xs); }
</style>
