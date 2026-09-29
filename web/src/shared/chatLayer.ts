/** 悬浮客服层的跨视图状态（单例） */
import { reactive } from 'vue'

const LS_OPEN = 'agw.chat.open'

function initialOpen(): boolean {
  try {
    const v = localStorage.getItem(LS_OPEN)
    if (v === '0') return false
    if (v === '1') return true
  } catch { /* ignore */ }
  return false // 首次访问收起，避免遮挡工作区；记住用户后续选择
}

export const chatLayer = reactive({
  open: initialOpen(), // 面板展开
  collapsed: false,    // 最小化为浮标
  x: -1,               // 面板左上角坐标（-1 = 用默认停靠位）
  y: -1,
  w: 388,
  h: 0,                // 0 = 用默认高度
  unread: 0,           // 收起时的未读计数
  /** 目标租户（管理端切换视角用；0 = 由后端默认） */
  tenantId: 0,
})

/** 记住展开/收起选择 */
export function persistOpen(open: boolean) {
  try { localStorage.setItem(LS_OPEN, open ? '1' : '0') } catch { /* ignore */ }
}

export function openChat() {
  chatLayer.open = true
  chatLayer.collapsed = false
  chatLayer.unread = 0
  persistOpen(true)
}

export function closeChat() {
  chatLayer.open = false
  chatLayer.collapsed = true
  persistOpen(false)
}

export function toggleChat() {
  if (chatLayer.open) closeChat()
  else openChat()
}
