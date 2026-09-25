<script setup lang="ts">
/**
 * 全局 AI 客服悬浮层（对齐阿里云 AI 助理形态）
 *
 * - 右侧停靠，可拖拽（拖头部移动，拖左边缘改宽度），位置/尺寸记忆在 sessionStorage
 * - 可最小化成浮标（带未读角标），跨栏目/跨视图常驻不丢会话
 * - 任务式对话：写操作给「确认/取消」按钮条；工具执行过程渲染成步骤卡
 * - 身份双模：租户 key（/portal/chat）或管理员会话（/admin/chat，可切目标租户）
 */
import { ref, computed, nextTick, onMounted, watch } from 'vue'
import {
  IconSparkles, IconMinus, IconArrowUp, IconCheck, IconTool,
  IconUser, IconShieldCheck, IconRefresh, IconArrowsMaximize,
} from '@tabler/icons-vue'
import { chatLayer, openChat, closeChat } from '../chatLayer'
import { auth } from '../api'

export interface ChatTrace {
  tool: string
  args: string
  confirmed: boolean
  preview: string
  result: string
}
interface Msg {
  role: 'user' | 'agent'
  text: string
  trace?: ChatTrace[]
  pending?: boolean
  /** 该挂起操作已被后续操作取代/结束（确认条不再可点，仅留痕） */
  pendingStale?: boolean
  pendingPreview?: string
  pendingTool?: string
  error?: boolean
  at: number
}

const props = withDefaults(defineProps<{
  /** 'portal' 用租户 key 打 /portal/chat；'admin' 用管理员会话打 /admin/chat */
  mode?: 'portal' | 'admin'
  /** admin 模式下的目标租户（0 = 后端默认） */
  tenants?: { id: number; name: string }[]
  title?: string
  subtitle?: string
}>(), { mode: 'portal', title: 'AI 客服', subtitle: '查余额 · 充值 · 订阅 · 退款' })

const PANEL_KEY = 'agw.chat.panel'

const msgs = ref<Msg[]>([])
const input = ref('')
const sending = ref(false)
const listEl = ref<HTMLElement | null>(null)
const panelEl = ref<HTMLElement | null>(null)
const dragging = ref(false)
const resizing = ref(false)
const sessId = ref(props.mode === 'portal' ? auth.getSid() : sessionStorage.getItem('agw.chat.adminSid') || '')

const greetings: Record<string, string> = {
  portal: '你好，我是商城 AI 客服。可以帮你查余额、看消费、充值、退款、订阅套餐。涉及资金的操作我会先给你预览，你确认后我才执行。',
  admin: '你好，我是运营助手。你正以管理员身份代客操作，可以查余额、看消费、充值、退款、订阅套餐。写操作同样需要你确认后执行。',
}

const quickActions = computed(() => props.mode === 'admin'
  ? ['查一下这个租户的余额', '看一下近 7 天消费', '这个租户订阅了什么', '帮他充 100 元']
  : ['查一下我的余额', '近 7 天消费多少', '有哪些套餐', '帮我充 100 元'])

/* ---------- 位置与尺寸（拖拽 / 记忆） ---------- */
const DOCK_RIGHT = 24
const DOCK_TOP = 76

const pos = ref({ x: -1, y: -1, w: 384, h: 620 })

function loadPanel() {
  try {
    const raw = sessionStorage.getItem(PANEL_KEY)
    if (!raw) return
    const p = JSON.parse(raw)
    if (typeof p.w === 'number') pos.value.w = Math.min(Math.max(p.w, 320), 720)
    if (typeof p.h === 'number') pos.value.h = Math.min(Math.max(p.h, 380), 900)
    if (typeof p.x === 'number' && p.x > -1) pos.value.x = p.x
    if (typeof p.y === 'number' && p.y > -1) pos.value.y = p.y
  } catch { /* ignore */ }
}
function savePanel() {
  try { sessionStorage.setItem(PANEL_KEY, JSON.stringify(pos.value)) } catch { /* ignore */ }
}

const panelStyle = computed(() => {
  const right = pos.value.x < 0
  return {
    width: pos.value.w + 'px',
    height: pos.value.h + 'px',
    top: (pos.value.y < 0 ? DOCK_TOP : pos.value.y) + 'px',
    ...(right ? { right: DOCK_RIGHT + 'px' } : { left: pos.value.x + 'px' }),
  }
})

function startDrag(e: MouseEvent) {
  if ((e.target as HTMLElement).closest('button, input, select, .no-drag')) return
  const el = panelEl.value
  if (!el) return
  const r = el.getBoundingClientRect()
  const dx = e.clientX - r.left
  const dy = e.clientY - r.top
  dragging.value = true
  const move = (ev: MouseEvent) => {
    const nx = Math.min(Math.max(ev.clientX - dx, 8), window.innerWidth - pos.value.w - 8)
    const ny = Math.min(Math.max(ev.clientY - dy, 8), window.innerHeight - 80)
    pos.value.x = nx
    pos.value.y = ny
  }
  const up = () => {
    dragging.value = false
    document.removeEventListener('mousemove', move)
    document.removeEventListener('mouseup', up)
    savePanel()
  }
  document.addEventListener('mousemove', move)
  document.addEventListener('mouseup', up)
}

function startResize(e: MouseEvent) {
  e.stopPropagation()
  const startX = e.clientX
  const startW = pos.value.w
  const el = panelEl.value
  const startRight = el ? el.getBoundingClientRect().right : window.innerWidth
  resizing.value = true
  const move = (ev: MouseEvent) => {
    // 若已脱离右侧停靠，拖左边改宽度时同步左边界
    const dw = startX - ev.clientX
    const w = Math.min(Math.max(startW + dw, 320), 720)
    pos.value.w = w
    if (pos.value.x >= 0) pos.value.x = Math.max(8, startRight - w)
  }
  const up = () => {
    resizing.value = false
    document.removeEventListener('mousemove', move)
    document.removeEventListener('mouseup', up)
    savePanel()
  }
  document.addEventListener('mousemove', move)
  document.addEventListener('mouseup', up)
}

function resetPos() {
  pos.value.x = -1
  pos.value.y = -1
  pos.value.w = 384
  pos.value.h = 620
  savePanel()
}

/* ---------- 会话 ---------- */
async function scrollBottom() {
  await nextTick()
  if (listEl.value) listEl.value.scrollTop = listEl.value.scrollHeight
}

function push(m: Msg) {
  msgs.value.push(m)
  scrollBottom()
  if (!chatLayer.open && m.role === 'agent') chatLayer.unread++
}

function greet() {
  if (msgs.value.length) return
  msgs.value.push({ role: 'agent', text: greetings[props.mode], at: Date.now() })
}

async function send(text?: string) {
  const t = (text ?? input.value).trim()
  if (!t || sending.value) return
  input.value = ''
  push({ role: 'user', text: t, at: Date.now() })
  sending.value = true
  try {
    const url = props.mode === 'admin' ? '/admin/chat' : '/portal/chat'
    const body: Record<string, unknown> = { message: t }
    if (sessId.value) body.session_id = sessId.value
    if (props.mode === 'admin' && chatLayer.tenantId > 0) body.tenant_id = chatLayer.tenantId

    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    if (props.mode === 'portal') headers['Authorization'] = 'Bearer ' + auth.getKey()

    const res = await fetch(url, { method: 'POST', headers, body: JSON.stringify(body), credentials: 'include' })
    const raw = await res.text()
    let data: any = null
    try { data = JSON.parse(raw) } catch { /* 非 JSON */ }
    if (!res.ok) {
      const msg = data?.error?.message || data?.error || `请求失败（${res.status}）`
      push({ role: 'agent', text: String(msg), error: true, at: Date.now() })
      return
    }
    sessId.value = data.session_id || sessId.value
    if (props.mode === 'portal') auth.setSid(sessId.value)
    else sessionStorage.setItem('agw.chat.adminSid', sessId.value)

    // 新一轮结果到达：此前挂起的确认条全部失效（防止对已过期操作重复点「确认执行」）。
    // 后端同一会话只会保留一个 pending_action，前端必须与之对齐。
    for (const m of msgs.value) {
      if (m.pending) {
        m.pending = false
        m.pendingStale = true
      }
    }

    push({
      role: 'agent',
      text: data.reply || '（空回复）',
      trace: data.trace?.length ? data.trace : undefined,
      pending: !!data.pending_confirm,
      pendingPreview: data.pending_preview,
      pendingTool: data.pending_tool,
      at: Date.now(),
    })
  } catch (e: any) {
    push({ role: 'agent', text: e?.message || '客服暂时不可用', error: true, at: Date.now() })
  } finally {
    sending.value = false
  }
}

async function confirm(ok: boolean) {
  await send(ok ? '确认' : '取消')
}

function reset() {
  msgs.value = []
  sessId.value = ''
  if (props.mode === 'portal') auth.clearSid()
  else sessionStorage.removeItem('agw.chat.adminSid')
  greet()
}

function closePanel() {
  closeChat()
}

onMounted(() => {
  loadPanel()
  greet()
  if (chatLayer.open) scrollBottom()
})

watch(() => chatLayer.open, v => { if (v) { greet(); scrollBottom() } })

defineExpose({ send })
</script>

<template>
  <!-- 浮标（收起态） -->
  <button
    v-if="!chatLayer.open"
    class="launcher"
    :title="`打开${title}`"
    @click="openChat"
  >
    <IconSparkles :size="22" />
    <span v-if="chatLayer.unread" class="badge">{{ chatLayer.unread > 9 ? '9+' : chatLayer.unread }}</span>
  </button>

  <!-- 面板 -->
  <Transition name="chatpop">
    <section
      v-if="chatLayer.open"
      ref="panelEl"
      class="panel"
      :class="{ dragging, resizing }"
      :style="panelStyle"
      role="dialog"
      aria-label="AI 客服"
    >
      <!-- 左边缘拖拽改宽 -->
      <div class="resizer" @mousedown="startResize" />

      <header class="phead" @mousedown="startDrag">
        <div class="hbrand">
          <span class="hdot" />
          <div class="htext">
            <strong>{{ title }}</strong>
            <small>{{ subtitle }}</small>
          </div>
        </div>
        <div class="hacts no-drag">
          <select
            v-if="mode === 'admin' && tenants?.length"
            v-model.number="chatLayer.tenantId"
            class="tsel"
            title="以哪个租户身份操作"
          >
            <option :value="0">默认租户</option>
            <option v-for="t in tenants" :key="t.id" :value="t.id">{{ t.name }}</option>
          </select>
          <button class="hbtn" title="回到默认位置" @click="resetPos"><IconArrowsMaximize :size="14" /></button>
          <button class="hbtn" title="重置会话" @click="reset"><IconRefresh :size="14" /></button>
          <button class="hbtn" title="收起" @click="closePanel"><IconMinus :size="15" /></button>
        </div>
      </header>

      <!-- 身份提示条 -->
      <div v-if="mode === 'admin'" class="idbar">
        <IconShieldCheck :size="13" />
        <span>管理员代客模式{{ chatLayer.tenantId ? ` · 租户 #${chatLayer.tenantId}` : ' · 默认租户' }}，写操作需你确认</span>
      </div>

      <div ref="listEl" class="plist">
        <div v-for="(m, i) in msgs" :key="i" class="brow" :class="m.role">
          <div v-if="m.role === 'agent'" class="avatar"><IconSparkles :size="14" /></div>
          <div v-else class="avatar user"><IconUser :size="14" /></div>
          <div class="bwrap">
            <div class="bubble" :class="{ err: m.error }">{{ m.text }}</div>

            <!-- 工具执行步骤 -->
            <div v-if="m.trace?.length" class="steps">
              <div v-for="(t, j) in m.trace" :key="j" class="step" :class="{ done: t.confirmed }">
                <IconTool :size="12" class="sic" />
                <span class="sname">{{ t.tool }}</span>
                <span class="sres">{{ t.result || t.preview }}</span>
              </div>
            </div>

            <!-- 写操作确认条（仅当前有效挂起可点） -->
            <div v-if="m.pending" class="confirm">
              <p class="cprev">{{ m.pendingPreview }}</p>
              <div class="cacts">
                <button class="cyes" @click="confirm(true)"><IconCheck :size="14" /> 确认执行</button>
                <button class="cno" @click="confirm(false)"><IconX :size="14" /> 取消</button>
              </div>
            </div>

            <!-- 已被取代的挂起：留痕但不可再点（防重复执行已过期操作） -->
            <div v-else-if="m.pendingStale" class="stale">
              <IconCheck :size="12" />
              <span>该待确认操作已结束</span>
            </div>
          </div>
        </div>

        <!-- 快捷指令（对话为空或只有问候时） -->
        <div v-if="msgs.length <= 1 && !sending" class="quick">
          <button v-for="q in quickActions" :key="q" class="qbtn" @click="send(q)">{{ q }}</button>
        </div>

        <div v-if="sending" class="brow agent">
          <div class="avatar"><IconSparkles :size="14" /></div>
          <div class="bubble typing"><i /><i /><i /></div>
        </div>
      </div>

      <footer class="pfoot">
        <textarea
          v-model="input"
          class="pin"
          rows="1"
          :placeholder="`给${mode === 'admin' ? '运营助手' : '客服'}发消息，Enter 发送 / Shift+Enter 换行`"
          @keydown.enter.exact.prevent="send()"
        />
        <button class="psend" :disabled="sending || !input.trim()" title="发送" @click="send()">
          <IconArrowUp :size="17" />
        </button>
      </footer>
    </section>
  </Transition>
</template>

<style scoped>
/* ---------- 浮标（收起态） ---------- */
.launcher {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: var(--z-chat);
  width: 56px; height: 56px;
  border-radius: 50%;
  border: none;
  background: linear-gradient(150deg, var(--accent-500), var(--accent-700));
  color: #fff;
  display: grid; place-items: center;
  box-shadow:
    0 2px 6px rgb(13 128 88 / .28),
    0 16px 34px -8px rgb(13 128 88 / .45),
    inset 0 1px 0 rgb(255 255 255 / .25);
  transition: transform var(--t-spring), box-shadow var(--t-base), filter var(--t-fast);
}
.launcher::after {
  /* 呼吸光环：明确"这是浮层"而非内嵌块 */
  content: '';
  position: absolute; inset: -7px;
  border-radius: 50%;
  border: 1px solid color-mix(in srgb, var(--accent-500) 32%, transparent);
  opacity: 0;
  transition: opacity var(--t-base), transform var(--t-spring);
}
.launcher:hover {
  transform: translateY(-3px) scale(1.05);
  filter: brightness(1.06);
}
.launcher:hover::after { opacity: 1; transform: scale(1.04); }
.launcher:active { transform: scale(.95); }

.badge {
  position: absolute; top: -3px; right: -3px;
  min-width: 20px; height: 20px; padding: 0 5px;
  border-radius: var(--r-pill);
  background: var(--danger); color: #fff;
  font-size: var(--fs-2xs); font-weight: 700;
  display: grid; place-items: center;
  box-shadow: 0 0 0 2.5px var(--bg), 0 2px 6px rgb(184 66 63 / .4);
}

/* ---------- 面板（浮层） ---------- */
.panel {
  position: fixed;
  z-index: var(--z-chat);
  display: flex; flex-direction: column;
  background: var(--surface);
  border-radius: var(--r-lg);
  /* 阶梯阴影 + 内高光：与页面拉开真实层级，不靠描边 */
  box-shadow:
    var(--shadow-pop),
    inset 0 0 0 1px rgb(17 25 23 / .05),
    var(--inset-hi-strong);
  overflow: hidden;
  min-width: 320px;
  transition: box-shadow var(--t-base);
}
.panel.dragging {
  box-shadow:
    0 3px 6px rgb(17 25 23 / .06),
    0 28px 56px -14px rgb(17 25 23 / .26),
    0 72px 140px -40px rgb(17 25 23 / .34),
    inset 0 0 0 1px rgb(17 25 23 / .06);
}
.panel.dragging, .panel.resizing { user-select: none; }
.panel.resizing { transition: none; }

.resizer {
  position: absolute; left: 0; top: 0; bottom: 0; width: 5px;
  cursor: ew-resize; z-index: 2;
}
.resizer:hover { background: color-mix(in srgb, var(--accent-500) 22%, transparent); }

.phead {
  display: flex; align-items: center; justify-content: space-between; gap: var(--sp-2);
  padding: 12px var(--sp-2) 12px var(--sp-5);
  background: rgb(255 255 255 / .72);
  backdrop-filter: blur(16px) saturate(180%);
  cursor: grab;
  flex: none;
}
.panel.dragging .phead { cursor: grabbing; }
.hbrand { display: flex; align-items: center; gap: 9px; min-width: 0; }
.hdot {
  width: 7px; height: 7px; border-radius: 50%;
  background: var(--ok);
  box-shadow: 0 0 0 3.5px var(--ok-bg);
  flex: none;
}
.htext { display: flex; flex-direction: column; min-width: 0; }
.htext strong { font-size: var(--fs-sm); font-weight: 620; color: var(--ink-900); letter-spacing: -.012em; }
.htext small {
  font-size: var(--fs-2xs); color: var(--ink-400);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.hacts { display: flex; align-items: center; gap: 2px; flex: none; }
.hbtn {
  width: 28px; height: 28px;
  border: none; border-radius: 50%;
  background: transparent; color: var(--ink-400);
  display: grid; place-items: center;
  transition: background var(--t-fast), color var(--t-fast);
}
.hbtn:hover { background: var(--well); color: var(--ink-800); }
.tsel {
  height: 28px; max-width: 130px;
  border: none; border-radius: var(--r-pill);
  background: var(--well); color: var(--ink-600);
  box-shadow: var(--inset-well);
  font-size: var(--fs-2xs); padding: 0 9px;
}

.idbar {
  display: flex; align-items: center; gap: 6px;
  padding: 8px var(--sp-5);
  background: var(--info-bg); color: var(--info);
  font-size: var(--fs-2xs); font-weight: 540;
  flex: none;
}

/* ---------- 对话区 ---------- */
.plist {
  flex: 1; min-height: 0; overflow-y: auto;
  padding: var(--sp-4);
  display: flex; flex-direction: column; gap: var(--sp-3);
  background: var(--well);
}
.brow { display: flex; gap: 8px; max-width: 92%; }
.brow.user { align-self: flex-end; flex-direction: row-reverse; }
.brow.agent { align-self: flex-start; }
.avatar {
  flex: none; width: 26px; height: 26px; border-radius: 9px;
  background: var(--accent-600); color: #fff;
  display: grid; place-items: center; margin-top: 1px;
  box-shadow: var(--shadow-xs);
}
.avatar.user { background: var(--ink-700); }
.bwrap { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.bubble {
  padding: 10px 13px;
  border-radius: 15px;
  font-size: var(--fs-sm); line-height: 1.62;
  white-space: pre-wrap; word-break: break-word;
}
.brow.agent .bubble {
  background: var(--surface);
  border-top-left-radius: 6px;
  color: var(--ink-700);
  box-shadow: var(--shadow-raise);
}
.brow.user .bubble {
  background: var(--accent-600);
  color: #fff;
  border-top-right-radius: 6px;
  box-shadow: var(--shadow-accent);
}
.bubble.err { background: var(--danger-bg); color: var(--danger); box-shadow: none; }

/* 工具步骤卡 */
.steps { display: flex; flex-direction: column; gap: 4px; }
.step {
  display: flex; align-items: center; gap: 6px;
  padding: 7px 11px;
  background: var(--surface);
  border-radius: 11px;
  box-shadow: var(--shadow-raise);
  font-size: var(--fs-2xs);
  color: var(--ink-500);
  min-width: 0;
}
.step.done { box-shadow: var(--shadow-raise), inset 2px 0 0 var(--ok); }
.sic { flex: none; color: var(--ink-400); }
.step.done .sic { color: var(--ok); }
.sname { font-weight: 620; color: var(--ink-700); font-family: var(--font-mono); flex: none; }
.sres { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* 确认条：高危操作 —— 用暖色底 + 内描边，不用生硬外框 */
.confirm {
  background: var(--warn-bg);
  border-radius: 13px;
  padding: 11px 12px;
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--warn) 22%, transparent);
  animation: pulseIn var(--t-slow);
}
@keyframes pulseIn { from { opacity: 0; transform: translateY(-5px) scale(.985); } }
.cprev { font-size: var(--fs-xs); color: var(--ink-700); line-height: 1.55; margin-bottom: 9px; }
.cacts { display: flex; gap: 8px; }
.cyes, .cno {
  flex: 1; height: 32px;
  border-radius: var(--r-sm); border: none;
  font-size: var(--fs-xs); font-weight: 600;
  display: inline-flex; align-items: center; justify-content: center; gap: 4px;
  transition: background var(--t-fast), box-shadow var(--t-base), transform var(--t-spring), color var(--t-fast);
}
.cyes { background: var(--accent-600); color: #fff; box-shadow: var(--shadow-accent); }
.cyes:hover { background: var(--accent-500); }
.cyes:active, .cno:active { transform: scale(.97); }
.cno { background: var(--surface); color: var(--ink-600); box-shadow: var(--shadow-raise); }
.cno:hover { color: var(--danger); box-shadow: var(--shadow-card); }

/* 已被取代的挂起留痕 */
.stale {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: var(--fs-2xs);
  color: var(--ink-400);
  padding: 4px 0;
}
.stale svg { color: var(--ok); }

/* 快捷指令 */
.quick { display: flex; flex-direction: column; gap: 6px; margin-top: 2px; }
.qbtn {
  text-align: left;
  padding: 9px 13px;
  background: var(--surface);
  border: none;
  border-radius: 11px;
  box-shadow: var(--shadow-raise);
  font-size: var(--fs-xs); color: var(--ink-600);
  transition: box-shadow var(--t-base), color var(--t-fast), transform var(--t-spring);
}
.qbtn:hover {
  color: var(--accent-700);
  box-shadow: var(--shadow-card);
  transform: translateX(3px);
}

/* 输入中 */
.typing {
  display: flex; gap: 4px; padding: 12px 14px;
  background: var(--surface);
  border-top-left-radius: 6px;
  border-radius: 15px;
  box-shadow: var(--shadow-raise);
  width: fit-content;
}
.typing i {
  width: 5px; height: 5px; border-radius: 50%;
  background: var(--ink-400);
  animation: blink 1.3s infinite;
}
.typing i:nth-child(2) { animation-delay: .18s; }
.typing i:nth-child(3) { animation-delay: .36s; }
@keyframes blink { 0%, 80%, 100% { opacity: .25; } 40% { opacity: 1; } }

/* ---------- 输入区 ---------- */
.pfoot {
  flex: none;
  display: flex; align-items: flex-end; gap: 8px;
  padding: 11px var(--sp-3) 11px var(--sp-5);
  background: var(--surface);
  box-shadow: 0 -1px 0 var(--hairline);
}
.pin {
  flex: 1;
  min-height: 36px; max-height: 120px;
  padding: 8px 13px;
  border: none;
  border-radius: 12px;
  background: var(--well);
  box-shadow: var(--inset-well);
  resize: none; outline: none;
  font-size: var(--fs-sm); line-height: 1.5;
  transition: background var(--t-fast), box-shadow var(--t-base);
}
.pin:hover { background: var(--well-2); }
.pin:focus {
  background: var(--surface);
  box-shadow: inset 0 0 0 1.5px var(--accent-500), 0 0 0 4px var(--accent-50);
}
.psend {
  flex: none;
  width: 36px; height: 36px;
  border-radius: 12px; border: none;
  background: var(--accent-600); color: #fff;
  display: grid; place-items: center;
  box-shadow: var(--shadow-accent);
  transition: background var(--t-fast), transform var(--t-spring);
}
.psend:hover:not(:disabled) { background: var(--accent-500); }
.psend:active:not(:disabled) { transform: scale(.93); }
.psend:disabled { opacity: .4; cursor: not-allowed; box-shadow: none; }

/* 入场 */
.chatpop-enter-active { transition: opacity var(--t-base), transform var(--t-spring); }
.chatpop-leave-active { transition: opacity var(--t-fast), transform var(--t-fast); }
.chatpop-enter-from { opacity: 0; transform: translateY(14px) scale(.96); }
.chatpop-leave-to { opacity: 0; transform: translateY(8px) scale(.98); }

@media (max-width: 720px) {
  .panel {
    left: 8px !important;
    right: 8px !important;
    width: auto !important;
    top: 8px !important;
    height: calc(100dvh - 16px) !important;
  }
  .resizer { display: none; }
}
</style>