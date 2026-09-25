/** 轻量 Toast + Confirm 弹层（无外部依赖） */
import { reactive } from 'vue'

type ToastTone = 'ok' | 'warn' | 'danger'
interface ToastItem { id: number; text: string; tone: ToastTone }

export const toasts = reactive<{ items: ToastItem[] }>({ items: [] })
let tid = 0

/** 顶部 toast，3.2s 自动消失 */
export function toast(text: string, tone: ToastTone = 'ok') {
  const id = ++tid
  toasts.items.push({ id, text, tone })
  setTimeout(() => {
    const i = toasts.items.findIndex(t => t.id === id)
    if (i >= 0) toasts.items.splice(i, 1)
  }, 3200)
}

export const toastOk = (t: string) => toast(t, 'ok')
export const toastErr = (t: string) => toast(t, 'danger')
export const toastWarn = (t: string) => toast(t, 'warn')