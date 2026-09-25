<script setup lang="ts">
withDefaults(defineProps<{
  tone?: 'ok' | 'warn' | 'danger' | 'info' | 'muted' | 'accent'
  dot?: boolean
}>(), { tone: 'muted' })
</script>

<template>
  <span class="tag" :class="tone"><i v-if="dot" class="dot" /><slot /></span>
</template>

<style scoped>
.tag {
  display: inline-flex; align-items: center; gap: 5px;
  height: 21px; padding: 0 8px;
  border-radius: var(--r-xs);
  font-size: var(--fs-2xs);
  font-weight: 600;
  letter-spacing: .005em;
  white-space: nowrap;
}
.ok     { background: var(--ok-bg);     color: var(--ok); }
.warn   { background: var(--warn-bg);   color: var(--warn); }
.danger { background: var(--danger-bg); color: var(--danger); }
.info   { background: var(--info-bg);   color: var(--info); }
.muted  { background: var(--paper-3);   color: var(--ink-500); }
.accent { background: var(--accent-50); color: var(--accent-700); }
.dot {
  width: 5px; height: 5px;
  border-radius: 50%;
  background: currentColor;
  flex: none;
}
</style>