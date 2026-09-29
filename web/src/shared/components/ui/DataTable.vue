<script setup lang="ts">
withDefaults(defineProps<{
  /** 列宽模板，如 '60px 1fr 120px'，与行内元素一一对应 */
  cols?: string
  loading?: boolean
  empty?: boolean
  emptyText?: string
  /** 骨架行数 */
  skeletonRows?: number
  /** 行号列（宽表用，便于沟通"第几行"） */
  numbered?: boolean
}>(), { skeletonRows: 6, emptyText: '暂无数据' })
</script>

<template>
  <div class="dt">
    <div v-if="loading" class="dt-skel">
      <div v-for="i in skeletonRows" :key="i" class="dt-skelrow skeleton" />
    </div>

    <div v-else-if="empty" class="dt-empty">
      <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor"
           stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M4 7h16M4 12h16M4 17h9" />
      </svg>
      <p>{{ emptyText }}</p>
    </div>

    <div v-else class="dt-body" :style="cols ? { '--cols': cols } : undefined">
      <div v-if="$slots.head" class="dt-head">
        <span v-if="numbered" class="dt-nohead">#</span>
        <slot name="head" />
      </div>
      <div class="dt-rows">
        <slot />
      </div>
    </div>

    <div v-if="$slots.footer" class="dt-foot"><slot name="footer" /></div>
  </div>
</template>

<style scoped>
.dt { width: 100%; }

.dt-skel { display: flex; flex-direction: column; gap: var(--sp-2); }
.dt-skelrow { height: 44px; border-radius: var(--r-sm); }

/* 空态 */
.dt-empty {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: var(--sp-3); padding: var(--sp-9) var(--sp-4);
  color: var(--ink-300);
}
.dt-empty p { font-size: var(--fs-sm); color: var(--ink-400); }

.dt-body { width: 100%; overflow-x: auto; }

.dt-head {
  display: grid;
  grid-template-columns: var(--cols, repeat(auto-fit, minmax(0, 1fr)));
  gap: var(--sp-3);
  align-items: center;
  padding: 11px var(--sp-4);
  background: var(--surface-3);
  font-size: 12px;
  font-weight: 650;
  letter-spacing: 0;
  text-transform: uppercase;
  color: var(--ink-400);
  white-space: nowrap;
}
.dt-nohead { color: var(--ink-300); }

.dt-rows { display: flex; flex-direction: column; }

/* 行：虚线分隔 + hover 抬起 */
.dt-rows :deep(.dt-row) {
  position: relative;
  display: grid;
  grid-template-columns: var(--cols, repeat(auto-fit, minmax(0, 1fr)));
  gap: var(--sp-3);
  align-items: center;
  padding: var(--sp-3) var(--sp-4);
  font-size: var(--fs-sm);
  color: var(--ink-700);
  border-radius: 0;
  transition: background var(--t-fast), box-shadow var(--t-fast);
  min-width: 0;
}
.dt-rows :deep(.dt-row + .dt-row)::before {
  content: '';
  position: absolute;
  top: 0; left: var(--sp-4); right: var(--sp-4);
  border-top: 1px solid var(--hairline-2);
}
.dt-rows :deep(.dt-row:hover) {
  background: var(--surface-2);
}
.dt-rows :deep(.dt-row:hover)::before,
.dt-rows :deep(.dt-row:hover + .dt-row)::before { border-color: transparent; }

.dt-rows :deep(.dt-row > *) { min-width: 0; overflow: hidden; text-overflow: ellipsis; }
.dt-rows :deep(.dt-num) { font-variant-numeric: tabular-nums; }
.dt-rows :deep(.dt-strong) { font-weight: 610; color: var(--ink-900); }
.dt-rows :deep(.dt-muted) { color: var(--ink-400); font-size: var(--fs-xs); }
.dt-rows :deep(.dt-actions) { display: flex; gap: 6px; justify-content: flex-end; }

.dt-foot { padding-top: var(--sp-5); }

/* Preserve column labels and alignment on narrow screens. */
@media (max-width: 880px) {
  .dt-head, .dt-rows { min-width: 760px; }
}
</style>
