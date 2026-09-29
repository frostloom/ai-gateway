<script setup lang="ts">
withDefaults(defineProps<{ label: string; hint?: string; error?: string; required?: boolean }>(), {})
</script>

<template>
  <label class="field">
    <span class="lbl">
      {{ label }}<i v-if="required" class="req">*</i>
      <span v-if="hint" class="hint">{{ hint }}</span>
    </span>
    <div class="ctl" :class="{ bad: !!error }"><slot /></div>
    <span v-if="error" class="err">{{ error }}</span>
  </label>
</template>

<style scoped>
.field { display: block; margin-bottom: var(--sp-4); }
.lbl {
  display: flex; align-items: baseline; gap: 6px;
  font-size: var(--fs-xs); font-weight: 560;
  color: var(--ink-600);
  margin-bottom: 7px;
}
.req { color: var(--danger); font-style: normal; }
.hint { font-size: var(--fs-2xs); color: var(--ink-400); font-weight: 400; }

.ctl :deep(input),
.ctl :deep(select),
.ctl :deep(textarea) {
  width: 100%; height: 38px; padding: 0 var(--sp-4);
  border: 1px solid var(--hairline-2);
  border-radius: var(--r-md);
  background: var(--surface);
  box-shadow: var(--inset-well);
  transition: box-shadow var(--t-base), background var(--t-fast);
}
.ctl :deep(textarea) { height: auto; padding: 10px var(--sp-4); resize: vertical; }
.ctl :deep(input):hover,
.ctl :deep(select):hover,
.ctl :deep(textarea):hover { background: var(--well-2); }
.ctl :deep(input):focus,
.ctl :deep(select):focus,
.ctl :deep(textarea):focus {
  outline: none;
  background: var(--surface);
  box-shadow: inset 0 0 0 1.5px var(--accent-500), 0 0 0 4px var(--accent-50);
}
.ctl.bad :deep(input),
.ctl.bad :deep(select),
.ctl.bad :deep(textarea) {
  box-shadow: inset 0 0 0 1.5px var(--danger), 0 0 0 4px var(--danger-bg);
}
.err {
  display: block; margin-top: 6px;
  font-size: var(--fs-2xs); color: var(--danger);
}
</style>
