<script setup lang="ts">
withDefaults(defineProps<{ text?: string }>(), { text: '暂无数据' })
</script>

<template>
  <div class="empty">
    <div class="glyph">
      <svg width="34" height="34" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true">
        <path d="M4 6h16M4 12h16M4 18h10" />
      </svg>
    </div>
    <slot />
    <p class="t">{{ text }}</p>
  </div>
</template>

<style scoped>
.empty {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 4px; padding: 36px 16px; color: var(--ink-400);
}
.glyph {
  width: 52px; height: 52px; border-radius: 50%;
  background: var(--paper-3); display: grid; place-items: center; color: var(--ink-400);
  margin-bottom: 6px;
}
.t { font-size: var(--fs-sm); }
</style>