<script setup lang="ts">
withDefaults(defineProps<{
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'accent-soft'
  size?: 'xs' | 'sm' | 'md' | 'lg'
  block?: boolean
  loading?: boolean
  disabled?: boolean
  /** 尾部图标（组件）→ 嵌进圆形凹槽 */
  trailingIcon?: any
  /** 药丸形（CTA 用 true；表格内小按钮可 false） */
  pill?: boolean
}>(), { variant: 'secondary', size: 'md', pill: false })
</script>

<template>
  <button
    class="btn"
    :class="[variant, size, { block, loading, pill }]"
    :disabled="disabled || loading"
  >
    <span v-if="loading" class="spinner" aria-hidden="true" />
    <span class="label"><slot /></span>
    <span v-if="trailingIcon && !loading" class="iconSlot">
      <component :is="trailingIcon" :size="13" :stroke-width="2" />
    </span>
  </button>
</template>

<style scoped>
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border-radius: var(--r-sm);
  font-weight: 570;
  letter-spacing: -.008em;
  border: 1px solid transparent;
  transition:
    background var(--t-fast),
    border-color var(--t-fast),
    color var(--t-fast),
    transform var(--t-spring),
    box-shadow var(--t-base);
  white-space: nowrap;
  user-select: none;
  position: relative;
}
.btn.pill { border-radius: var(--r-pill); }
.btn:active:not(:disabled) { transform: scale(.975); }
.btn:disabled { opacity: .42; cursor: not-allowed; }
.btn.block { width: 100%; }

.btn.xs { height: 26px; padding: 0 10px; font-size: var(--fs-2xs); gap: 6px; }
.btn.sm { height: 32px; padding: 0 13px; font-size: var(--fs-xs); }
.btn.md { height: 38px; padding: 0 18px; font-size: var(--fs-sm); }
.btn.lg { height: 46px; padding: 0 26px; font-size: var(--fs-base); }
.btn.pill.sm { padding: 0 15px; }
.btn.pill.md { padding: 0 20px; }
.btn.pill.lg { padding: 0 28px; }

/* 尾部图标凹槽（Button-in-Button） */
.iconSlot {
  display: grid; place-items: center;
  width: 22px; height: 22px;
  border-radius: 50%;
  background: rgb(255 255 255 / .18);
  flex: none;
  margin-right: -6px;
  transition: transform var(--t-spring), background var(--t-fast);
}
.btn:hover .iconSlot {
  transform: translate(2px, -1px) scale(1.06);
  background: rgb(255 255 255 / .28);
}
.btn.secondary .iconSlot,
.btn.ghost .iconSlot,
.btn.accent-soft .iconSlot { background: rgb(17 25 23 / .06); }
.btn.secondary:hover .iconSlot { background: rgb(17 25 23 / .1); }

.btn.primary {
  background: var(--accent-600);
  color: #fff;
  box-shadow: var(--shadow-xs), inset 0 1px 0 rgb(255 255 255 / .16);
}
.btn.primary:hover:not(:disabled) {
  background: var(--accent-500);
  box-shadow: var(--shadow-accent);
}

.btn.secondary {
  background: var(--surface);
  color: var(--ink-700);
  border-color: var(--hairline-2);
  box-shadow: var(--shadow-xs), var(--inset-hi);
}
.btn.secondary:hover:not(:disabled) {
  border-color: var(--ink-300);
  color: var(--ink-900);
  box-shadow: none;
}

.btn.ghost { background: transparent; color: var(--ink-500); }
.btn.ghost:hover:not(:disabled) { background: var(--surface-3); color: var(--ink-800); }

.btn.accent-soft {
  background: var(--accent-50);
  color: var(--accent-700);
  border-color: var(--accent-100);
}
.btn.accent-soft:hover:not(:disabled) {
  background: var(--accent-100);
  border-color: var(--accent-200);
}

.btn.danger {
  background: var(--danger);
  color: #fff;
  box-shadow: var(--shadow-xs), inset 0 1px 0 rgb(255 255 255 / .16);
}
.btn.danger:hover:not(:disabled) { filter: brightness(.95); box-shadow: none; }

.spinner {
  width: 13px; height: 13px;
  border: 1.8px solid rgb(255 255 255 / .3);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin .65s cubic-bezier(.5, .15, .5, .85) infinite;
}
.btn.secondary .spinner,
.btn.ghost .spinner,
.btn.accent-soft .spinner {
  border-color: var(--ink-200);
  border-top-color: var(--ink-500);
}
@keyframes spin { to { transform: rotate(360deg); } }
</style>