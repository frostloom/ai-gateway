<script setup lang="ts">
import { toasts } from '../../utils/toast'
</script>

<template>
  <Teleport to="body">
    <div class="host" aria-live="polite">
      <TransitionGroup name="pop">
        <div v-for="t in toasts.items" :key="t.id" class="item" :class="t.tone">
          <span class="ic" aria-hidden="true">{{ t.tone === 'ok' ? '✓' : t.tone === 'danger' ? '!' : '⚠' }}</span>
          <span>{{ t.text }}</span>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.host {
  position: fixed; top: 18px; left: 50%; transform: translateX(-50%);
  z-index: var(--z-toast); display: flex; flex-direction: column; gap: 8px; align-items: center;
  pointer-events: none;
}
.item {
  display: flex; align-items: center; gap: 9px;
  background: rgb(255 255 255 / .88);
  backdrop-filter: blur(18px) saturate(180%);
  color: var(--ink-800);
  border-radius: var(--r-pill);
  padding: 8px 18px 8px 9px;
  font-size: var(--fs-sm); font-weight: 540;
  box-shadow: var(--shadow-pop), inset 0 0 0 1px rgb(17 25 23 / .05);
}
.ic {
  width: 21px; height: 21px; border-radius: 50%;
  display: grid; place-items: center; font-size: 12px; color: #fff;
}
.ok .ic { background: var(--ok); }
.warn .ic { background: var(--warn); }
.danger .ic { background: var(--danger); }
.pop-enter-active { transition: opacity var(--t-slow), transform var(--t-spring); }
.pop-leave-active { transition: opacity var(--t-fast), transform var(--t-fast); }
.pop-enter-from { opacity: 0; transform: translateY(-10px) scale(.96); }
.pop-leave-to { opacity: 0; transform: translateY(-5px); }
</style>
