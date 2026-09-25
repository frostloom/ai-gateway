<script setup lang="ts">
/**
 * Modal —— 结构分层弹窗
 *
 * 遮罩做重模糊 + 深色渐层；面板用与 Card 同源的阶梯阴影抬起，
 * 不做 1px 描边，页脚用虚线分隔。
 */
import { watch, onBeforeUnmount } from 'vue'

const props = withDefaults(defineProps<{ open: boolean; title?: string; width?: number | string }>(), { width: 480 })
const emit = defineEmits<{ (e: 'close'): void }>()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open) emit('close')
}
watch(() => props.open, v => {
  if (v) document.addEventListener('keydown', onKey)
  else document.removeEventListener('keydown', onKey)
})
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="overlay" @click.self="emit('close')">
        <div class="modal" :style="{ maxWidth: width + 'px' }" role="dialog" aria-modal="true">
          <header v-if="title || $slots.extra" class="mhead">
            <h3 class="mt">{{ title }}</h3>
            <div class="mextra"><slot name="extra" /></div>
            <button class="x" aria-label="关闭" @click="emit('close')">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor"
                   stroke-width="1.8" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18" /></svg>
            </button>
          </header>
          <div class="mbody"><slot /></div>
          <footer v-if="$slots.footer" class="mfoot"><slot name="footer" /></footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.overlay {
  position: fixed; inset: 0; z-index: var(--z-modal);
  background:
    radial-gradient(120% 90% at 50% 0%, rgb(17 25 23 / .34), rgb(17 25 23 / .52));
  backdrop-filter: blur(10px) saturate(120%);
  display: flex; align-items: center; justify-content: center;
  padding: var(--sp-5);
}
.modal {
  width: 100%;
  background: var(--surface);
  border-radius: var(--r-xl);
  box-shadow: var(--shadow-pop), var(--inset-hi-strong);
  max-height: min(84vh, 760px);
  display: flex; flex-direction: column;
  overflow: hidden;
}
.mhead {
  display: flex; align-items: center; gap: var(--sp-3);
  padding: var(--sp-6) var(--sp-6) var(--sp-3);
}
.mt {
  font-size: var(--fs-md);
  font-weight: 620;
  letter-spacing: -.018em;
  flex: 1;
}
.mextra { display: flex; gap: var(--sp-2); align-items: center; }
.x {
  border: none; background: transparent; color: var(--ink-400);
  width: 30px; height: 30px;
  border-radius: 50%;
  display: grid; place-items: center;
  transition: background var(--t-fast), color var(--t-fast), transform var(--t-spring);
}
.x:hover { background: var(--well); color: var(--ink-800); transform: rotate(90deg); }
.mbody { padding: 0 var(--sp-6) var(--sp-4); overflow: auto; }
.mfoot {
  display: flex; justify-content: flex-end; gap: var(--sp-2);
  padding: var(--sp-5) var(--sp-6);
  border-top: 1px dashed var(--hairline-2);
  background: var(--surface-2);
}
.fade-enter-active { transition: opacity var(--t-base); }
.fade-leave-active { transition: opacity var(--t-fast); }
.fade-enter-from, .fade-leave-to { opacity: 0; }
.fade-enter-active .modal { animation: modalIn var(--t-slow); }
@keyframes modalIn {
  from { opacity: 0; transform: translateY(14px) scale(.975); }
}
</style>
