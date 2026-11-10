<script lang="ts">
let openDialogCount = 0;
let savedBodyOverflow = "";
</script>
<script setup lang="ts">
import { ref, watch, nextTick, computed, onBeforeUnmount } from "vue";

const props = withDefaults(
  defineProps<{ open: boolean; title?: string; width?: number | string; closeDisabled?: boolean }>(),
  { width: 480 },
);
const emit = defineEmits<{ (e: "close"): void }>();
const panel = ref<HTMLElement | null>(null);
let previousFocus: HTMLElement | null = null;
let ownsScrollLock = false;
function releaseScrollLock() {
  if (!ownsScrollLock) return;
  ownsScrollLock = false;
  openDialogCount -= 1;
  if (openDialogCount === 0) document.body.style.overflow = savedBodyOverflow;
}
function close() {
  if (!props.closeDisabled) emit("close");
}
const maxWidth = computed(() =>
  typeof props.width === "number" || /^\d+$/.test(props.width)
    ? props.width + "px"
    : props.width,
);

function focusable() {
  return Array.from(
    panel.value?.querySelectorAll<HTMLElement>(
      'button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href], [tabindex="0"]',
    ) || [],
  ).filter(
    (element) =>
      !element.hidden && element.getAttribute("aria-hidden") !== "true",
  );
}
function onKey(event: KeyboardEvent) {
  if (!props.open) return;
  const dialogs = document.querySelectorAll('[role="dialog"]');
  if (dialogs[dialogs.length - 1] !== panel.value) return;
  if (event.key === "Escape") {
    event.preventDefault();
    close();
    return;
  }
  if (event.key !== "Tab") return;
  const elements = focusable();
  const first = elements[0];
  const last = elements[elements.length - 1];
  if (!first) {
    event.preventDefault();
    panel.value?.focus();
    return;
  }
  if (
    event.shiftKey &&
    (document.activeElement === first ||
      !panel.value?.contains(document.activeElement))
  ) {
    event.preventDefault();
    last?.focus();
  } else if (
    !event.shiftKey &&
    (document.activeElement === last ||
      !panel.value?.contains(document.activeElement))
  ) {
    event.preventDefault();
    first.focus();
  }
}
watch(
  () => props.open,
  async (open) => {
    if (open) {
      if (!ownsScrollLock) {
        if (openDialogCount === 0) savedBodyOverflow = document.body.style.overflow;
        openDialogCount += 1;
        ownsScrollLock = true;
        document.body.style.overflow = "hidden";
      }
      previousFocus = document.activeElement as HTMLElement | null;
      document.addEventListener("keydown", onKey);
      await nextTick();
      if (props.open) (focusable()[0] || panel.value)?.focus();
    } else {
      releaseScrollLock();
      document.removeEventListener("keydown", onKey);
      if (previousFocus?.isConnected) previousFocus.focus();
    }
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  releaseScrollLock();
  document.removeEventListener("keydown", onKey);
  if (props.open && previousFocus?.isConnected) previousFocus.focus();
});
</script>

<template>
  <Teleport to="body">
      <div v-if="open" class="overlay" @click.self="close">
        <div
          ref="panel"
          class="modal"
          tabindex="-1"
          :style="{ maxWidth }"
          role="dialog"
          aria-modal="true"
          :aria-label="title"
        >
          <header v-if="title || $slots.extra" class="mhead">
            <h2 class="mt">{{ title }}</h2>
            <div class="mextra"><slot name="extra" /></div>
            <button class="x" aria-label="关闭" :disabled="closeDisabled" @click="close">
              <svg
                width="15"
                height="15"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.8"
                stroke-linecap="round"
              >
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </header>
          <div class="mbody"><slot /></div>
          <footer v-if="$slots.footer" class="mfoot">
            <slot name="footer" />
          </footer>
        </div>
      </div>
  </Teleport>
</template>

<style scoped>
/* Hallmark · v2.0.0 · theme: custom · scope: component */
/* Critique: immediate centered dialogs preserve form alignment and keyboard focus. */
.overlay {
  isolation: isolate;
  position: fixed;
  inset: 0;
  z-index: var(--z-modal);
  background: var(--overlay-backdrop);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--sp-5);
}
.modal {
  width: 100%;
  background: var(--surface);
  border-radius: var(--r-xl);
  box-shadow: var(--shadow-pop), var(--inset-hi-strong);
  max-height: min(90dvh, 860px);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.mhead {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  padding: var(--sp-6) var(--sp-6) var(--sp-3);
}
.mt {
  font-size: var(--fs-md);
  font-weight: 620;
  letter-spacing: -0.018em;
  flex: 1;
}
.mextra {
  display: flex;
  gap: var(--sp-2);
  align-items: center;
}
.x {
  border: none;
  background: transparent;
  color: var(--ink-400);
  width: 30px;
  height: 30px;
  border-radius: var(--r-sm);
  display: grid;
  place-items: center;
  transition:
    background var(--t-fast),
    color var(--t-fast),
    transform var(--t-spring);
}
.x:hover {
  background: var(--well);
  color: var(--ink-800);
}
.mbody {
  min-height: 0;
  padding: 0 var(--sp-6) var(--sp-4);
  overflow: auto;
}
.mfoot {
  display: flex;
  justify-content: flex-end;
  gap: var(--sp-2);
  padding: var(--sp-5) var(--sp-6);
  border-top: 1px solid var(--hairline-2);
  background: var(--surface-2);
}
@media (max-width: 600px) {
  .overlay { padding: 12px; }
  .mhead { padding: 20px 20px 16px; }
  .mbody { padding: 0 20px 20px; }
  .mfoot { padding: 16px 20px; }
}
</style>
