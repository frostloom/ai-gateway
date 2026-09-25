<script setup lang="ts">
withDefaults(defineProps<{ w?: string; h?: string; r?: string }>(), { w: '100%', h: '14px', r: '8px' })
</script>

<template>
  <span class="sk" :style="{ width: w, height: h, borderRadius: r }" />
</template>

<style scoped>
.sk {
  display: inline-block;
  background: linear-gradient(90deg, var(--paper-3) 25%, var(--paper-2) 50%, var(--paper-3) 75%);
  background-size: 200% 100%;
  animation: shimmer 1.4s ease infinite;
}
@keyframes shimmer { from { background-position: 200% 0; } to { background-position: -200% 0; } }
</style>