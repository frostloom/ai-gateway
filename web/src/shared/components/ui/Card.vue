<script setup lang="ts">
withDefaults(defineProps<{
  title?: string
  sub?: string
  pad?: boolean
  /** 双圈套壳（大容器 true；卡内分区关掉） */
  bezel?: boolean
  /** 悬停抬升 */
  hover?: boolean
  /** 顶部强调色条 */
  accentBar?: boolean
  /** 兼容旧 API：flat = 不套壳 */
  flat?: boolean
  /** 内芯是否改用凹槽底（用于表单/调试类容器） */
  well?: boolean
}>(), { pad: true, bezel: true, hover: false })
</script>

<template>
  <section class="shell" :class="{ bezel: bezel && !flat, hover }">
    <div class="core" :class="{ accentBar, well }">
      <header v-if="title || $slots.extra" class="head">
        <div class="hleft">
          <h3 v-if="title" class="t">{{ title }}</h3>
          <p v-if="sub" class="s">{{ sub }}</p>
        </div>
        <div v-if="$slots.extra" class="extra"><slot name="extra" /></div>
      </header>
      <div :class="{ body: pad }"><slot /></div>
    </div>
  </section>
</template>

<style scoped>
.shell { min-width: 0; background: var(--surface); border: 1px solid var(--hairline); border-radius: var(--r-lg); overflow: hidden; }
.core { background: var(--surface); }
.core.well { background: var(--surface-2); }
.shell.hover:hover { border-color: var(--hairline-2); }
.head { display: flex; align-items: flex-start; justify-content: space-between; flex-wrap: wrap; gap: var(--sp-3); padding: var(--sp-5) var(--sp-5) 0; }
.hleft { min-width: 0; } .t { font-size: 14px; font-weight: 600; line-height: 1.5; }
.s { margin-top: 4px; font-size: 12px; color: var(--ink-500); }
.extra { display: flex; align-items: center; gap: var(--sp-2); }
.body { padding: var(--sp-5); }
</style>
