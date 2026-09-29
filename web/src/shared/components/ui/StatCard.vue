<script setup lang="ts">
withDefaults(defineProps<{
  label: string
  value?: string | number
  sub?: string
  tone?: 'default' | 'ok' | 'warn' | 'danger' | 'info'
  loading?: boolean
}>(), { tone: 'default' })
</script>

<template>
  <div class="stat" :class="tone">
    <p class="label">{{ label }}</p>
    <p v-if="loading" class="val"><span class="ph skeleton" /></p>
    <p v-else class="val num">{{ value ?? '-' }}</p>
    <p v-if="sub" class="sub" :class="{ hide: loading }">{{ loading ? ' ' : sub }}</p>
  </div>
</template>

<style scoped>
.stat {
  position: relative;
  background: var(--surface);
  border-radius: var(--r-md);
  padding: var(--sp-4) var(--sp-5);
  border: 1px solid var(--hairline);
  overflow: hidden;
  transition: box-shadow var(--t-base), transform var(--t-base);
}
.label {
  font-size: var(--fs-xs);
  color: var(--ink-500);
  font-weight: 540;
}
.val {
  font-size: var(--fs-2xl);
  font-weight: 660;
  letter-spacing: -.03em;
  line-height: 1.12;
  margin-top: 8px;
  color: var(--ink-900);
}
.sub {
  font-size: var(--fs-2xs);
  color: var(--ink-400);
  margin-top: 5px;
}
.ph {
  display: inline-block; width: 72px; height: 22px;
  vertical-align: middle;
}
.hide { visibility: hidden; }
</style>
