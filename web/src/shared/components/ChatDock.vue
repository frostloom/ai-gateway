<script setup lang="ts">
/**
 * 全局 AI 客服悬浮层
 *
 * - 右侧停靠，可拖拽（拖头部移动，拖左边缘改宽度），位置/尺寸记忆在 sessionStorage
 * - 可关闭成浮标（带未读角标），跨栏目/跨视图常驻不丢会话
 * - 任务式对话：写操作给「确认/取消」按钮条；工具执行过程渲染成步骤卡
 * - 身份双模：租户 key（/portal/chat）或管理员会话（/admin/chat，可切目标租户）
 */
import { ref, computed, nextTick, onMounted, onBeforeUnmount, watch, inject } from 'vue'
import {
  IconMessageCircle, IconArrowUp, IconCheck, IconTool,
  IconUser, IconShieldCheck, IconRefresh, IconArrowsMaximize, IconArrowsMinimize, IconX,
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

const go = inject<((key: string) => void) | undefined>('go', undefined)
const props = withDefaults(defineProps<{
  /** 'portal' 用租户 key 打 /portal/chat；'admin' 用管理员会话打 /admin/chat */
  mode?: 'portal' | 'admin'
  /** admin 模式下的目标租户（0 = 后端默认） */
  tenants?: { id: number; name: string }[]
  title?: string
  subtitle?: string
}>(), { mode: 'portal', title: 'AI 客服', subtitle: '账户服务与操作指引' })

const PANEL_KEY = 'agw.chat.panel'

const msgs = ref<Msg[]>([])
const input = ref('')
const sending = ref(false)
const inputEl = ref<HTMLTextAreaElement | null>(null)
const composing = ref(false)
const INPUT_LIMIT = 2000
const inputError = ref('')
const resetRequested = ref(false)
const launcherEl = ref<HTMLButtonElement | null>(null)
const followLatest = ref(true)
function resizeInput() {
  const el = inputEl.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 150) + 'px'
}
function onInputKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || event.shiftKey || event.isComposing || composing.value || event.keyCode === 229) return
  event.preventDefault()
  send()
}
async function focusInput() {
  await nextTick()
  if (!chatLayer.open) return
  resizeInput()
  inputEl.value?.focus({ preventScroll: true })
}
function clearInput() { input.value = ''; inputError.value = ''; focusInput() }
watch(input, () => { inputError.value = input.value.length > INPUT_LIMIT ? `消息最多 ${INPUT_LIMIT} 字` : ''; nextTick(resizeInput) })
const listEl = ref<HTMLElement | null>(null)
const panelEl = ref<HTMLElement | null>(null)
const dragging = ref(false)
const resizing = ref(false)
function savedSession() {
  try { return props.mode === 'portal' ? auth.getSid() : sessionStorage.getItem('agw.chat.adminSid') || '' }
  catch { return '' }
}
const sessId = ref(savedSession())

const greetings: Record<string, string> = {
  portal: '你好，可以帮你查询余额、消费明细和订阅额度，也可以解答 API 接入问题。',
  admin: '你好，可以帮你查询租户余额、消费和订阅状态。涉及账户变更的操作会先展示预览。',
}

const quickActions = computed(() => props.mode === 'admin'
  ? ['查一下这个租户的余额', '看一下近 7 天消费', '这个租户订阅了什么', '如何核对租户的账单']
  : ['查一下我的余额', '近 7 天消费多少', '月卡额度如何使用', '如何查看我的账单明细'])

/* ---------- 位置与尺寸（拖拽 / 记忆） ---------- */
const DOCK_RIGHT = 24
const DOCK_TOP = 76

const pos = ref({ x: -1, y: -1, w: 400, h: 600 })
const expanded = ref(false)
const viewport = ref({ w: window.innerWidth, h: window.innerHeight, top: 0, left: 0 })
function updateViewport() {
  const vv = window.visualViewport
  viewport.value = { w: vv?.width || window.innerWidth, h: vv?.height || window.innerHeight, top: vv?.offsetTop || 0, left: vv?.offsetLeft || 0 }
}
let endPointer: (() => void) | undefined

function loadPanel() {
  try {
    const raw = sessionStorage.getItem(PANEL_KEY)
    if (!raw) return
    const p = JSON.parse(raw)
    if (Number.isFinite(p.w)) pos.value.w = Math.min(Math.max(p.w, 320), 720)
    if (Number.isFinite(p.h)) pos.value.h = Math.min(Math.max(p.h, 380), 900)
    if (Number.isFinite(p.x) && p.x > -1) pos.value.x = p.x
    if (Number.isFinite(p.y) && p.y > -1) pos.value.y = p.y
    expanded.value = p.expanded === true
  } catch { /* ignore */ }
}
function savePanel() {
  try { sessionStorage.setItem(PANEL_KEY, JSON.stringify({ ...pos.value, expanded: expanded.value })) } catch { /* ignore */ }
}

const panelStyle = computed(() => {
  const { w: vw, h: vh, top, left } = viewport.value
  const mobile = vw <= 720
  const margin = expanded.value ? (mobile ? 0 : 16) : 8
  const width = Math.max(1, Math.min(expanded.value ? 960 : mobile ? vw : pos.value.w, vw - margin * 2))
  const height = Math.max(1, Math.min(expanded.value ? vh : pos.value.h, vh - margin * 2))
  const x = expanded.value ? (vw - width) / 2 : mobile ? margin : Math.max(margin, Math.min(pos.value.x < 0 ? vw - width - DOCK_RIGHT : pos.value.x, vw - width - margin))
  const y = expanded.value ? margin : Math.max(margin, Math.min(pos.value.y < 0 ? DOCK_TOP : pos.value.y, vh - height - margin))
  return { width: width + 'px', height: height + 'px', top: (top + y) + 'px', left: (left + x) + 'px' }
})

function startPointer(e: PointerEvent, resize: boolean) {
  if (e.button !== 0 || viewport.value.w <= 720 || expanded.value) return
  if (!resize && (e.target as HTMLElement).closest('button, input, select, .no-drag')) return
  const el = panelEl.value
  if (!el) return
  e.preventDefault()
  endPointer?.()
  const r = el.getBoundingClientRect()
  const dx = e.clientX - r.left
  const dy = e.clientY - r.top
  dragging.value = !resize
  resizing.value = resize
  const move = (ev: PointerEvent) => {
    if (ev.pointerId !== e.pointerId) return
    if (resize) {
      pos.value.w = Math.min(Math.max(r.right - ev.clientX, 320), 720, viewport.value.w - 16, r.right - 8)
      pos.value.x = r.right - pos.value.w
    } else {
      pos.value.x = Math.max(8, Math.min(ev.clientX - dx, viewport.value.w - r.width - 8))
      pos.value.y = Math.max(8, Math.min(ev.clientY - dy, viewport.value.h - r.height - 8))
    }
  }
  const stop = (ev: PointerEvent) => { if (ev.pointerId === e.pointerId) endPointer?.() }
  endPointer = () => {
    dragging.value = false
    resizing.value = false
    document.removeEventListener('pointermove', move)
    document.removeEventListener('pointerup', stop)
    document.removeEventListener('pointercancel', stop)
    endPointer = undefined
    savePanel()
  }
  document.addEventListener('pointermove', move)
  document.addEventListener('pointerup', stop)
  document.addEventListener('pointercancel', stop)
}

function toggleExpanded() {
  endPointer?.()
  expanded.value = !expanded.value
  savePanel()
  nextTick(() => { resizeInput(); scrollBottom() })
}

/* ---------- 会话 ---------- */
function onScroll() {
  const el = listEl.value
  if (el) followLatest.value = el.scrollHeight - el.scrollTop - el.clientHeight < 64
}

async function scrollBottom(force = false) {
  await nextTick()
  if (listEl.value && (force || followLatest.value)) listEl.value.scrollTop = listEl.value.scrollHeight
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
  if (!t || sending.value || composing.value) return
  if (t.length > INPUT_LIMIT) { inputError.value = `消息最多 ${INPUT_LIMIT} 字`; focusInput(); return }
  resetRequested.value = false
  if (text === undefined) input.value = ''
  followLatest.value = true
  // Once a new request starts, the previous operation may have been consumed upstream.
  for (const m of msgs.value) { if (m.pending) { m.pending = false; m.pendingStale = true } }
  push({ role: 'user', text: t, at: Date.now() })
  sending.value = true
  const requestTenant = chatLayer.tenantId
  try {
    const url = props.mode === 'admin' ? '/admin/chat' : '/portal/chat'
    const body: Record<string, unknown> = { message: t }
    if (sessId.value) body.session_id = sessId.value
    if (props.mode === 'admin' && chatLayer.tenantId > 0) body.tenant_id = chatLayer.tenantId

    const headers: Record<string, string> = { 'Content-Type': 'application/json' }

    const res = await fetch(url, { method: 'POST', headers, body: JSON.stringify(body), credentials: 'include' })
    const raw = await res.text()
    if (chatLayer.tenantId !== requestTenant) return
    let data: any = null
    try { data = JSON.parse(raw) } catch { /* 非 JSON */ }
    if (!res.ok) {
      if (res.status === 401) window.dispatchEvent(new Event(props.mode === 'admin' ? 'admin-session-expired' : 'portal-session-expired'))
      const msg = data?.error?.message || data?.error || `请求失败（${res.status}）`
      push({ role: 'agent', text: String(msg), error: true, at: Date.now() })
      return
    }
    if (!data || typeof data.reply !== 'string') throw new Error('客服返回了无效响应，请稍后重试')
    sessId.value = data.session_id || sessId.value
    try {
      if (props.mode === 'portal') auth.setSid(sessId.value)
      else sessionStorage.setItem('agw.chat.adminSid', sessId.value)
    } catch { /* The in-memory conversation remains usable when storage is unavailable. */ }

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
    if (chatLayer.tenantId !== requestTenant) reset()
    // Replies must not pull focus from another control or a collapsed panel.
    if (chatLayer.open && document.activeElement === inputEl.value) resizeInput()
  }
}

async function confirm(ok: boolean) {
  if (sending.value) return
  const request = send(ok ? '确认' : '取消')
  focusInput()
  await request
}

function reset() {
  if (sending.value) return
  resetRequested.value = false
  input.value = ''
  inputError.value = ''
  msgs.value = []
  sessId.value = ''
  try {
    if (props.mode === 'portal') auth.clearSid()
    else sessionStorage.removeItem('agw.chat.adminSid')
  } catch { /* The active session has already been cleared in memory. */ }
  greet()
  followLatest.value = true
  focusInput()
}

function requestReset() {
  if (sending.value) return
  resetRequested.value = true
  nextTick(() => panelEl.value?.querySelector<HTMLButtonElement>('.reset-cancel')?.focus())
}

function onPanelEscape(event: KeyboardEvent) {
  if (event.isComposing || composing.value || event.keyCode === 229) return
  event.preventDefault()
  event.stopPropagation()
  if (resetRequested.value) { resetRequested.value = false; focusInput() }
  else closePanel()
}

function closePanel() {
  endPointer?.()
  resetRequested.value = false
  closeChat()
  nextTick(() => launcherEl.value?.focus({ preventScroll: true }))
}

onMounted(() => {
  loadPanel()
  updateViewport()
  window.addEventListener('resize', updateViewport)
  window.visualViewport?.addEventListener('resize', updateViewport)
  window.visualViewport?.addEventListener('scroll', updateViewport)
  greet()
  if (chatLayer.open) { scrollBottom(); focusInput() }

})

onBeforeUnmount(() => {
  endPointer?.()
  window.removeEventListener('resize', updateViewport)
  window.visualViewport?.removeEventListener('resize', updateViewport)
  window.visualViewport?.removeEventListener('scroll', updateViewport)
})
watch(() => chatLayer.open, v => { if (v) { greet(); followLatest.value = true; scrollBottom(true); focusInput() } })
watch(() => chatLayer.tenantId, () => reset())

defineExpose({ send })
</script>

<template>
  <!-- 浮标（收起态） -->
  <button
    v-if="!chatLayer.open"
    ref="launcherEl"
    class="launcher"
    :aria-label="`打开${title}${chatLayer.unread ? `，${chatLayer.unread} 条未读` : ''}`"
    aria-expanded="false"
    :title="`打开${title}`"
    @click="openChat"
  >
    <IconMessageCircle :size="22" />
    <span v-if="chatLayer.unread" class="badge">{{ chatLayer.unread > 9 ? '9+' : chatLayer.unread }}</span>
  </button>

  <!-- 面板 -->
  <Transition name="chatpop">
    <section
      v-if="chatLayer.open"
      ref="panelEl"
      class="panel"
      :class="{ dragging, resizing, expanded }"
      :style="panelStyle"
      role="dialog"
      :aria-label="title"
      @keydown.esc="onPanelEscape"
    >
      <!-- 普通窗口可拖拽改宽，放大时保留原窗口位置。 -->
      <div v-if="!expanded" class="resizer" @pointerdown="startPointer($event, true)" />

      <div class="phead" @pointerdown="startPointer($event, false)">
        <div class="hbrand">
          <span class="hdot" />
          <div class="htext">
            <strong>{{ title }}</strong>
            <small>{{ subtitle }}</small>
          </div>
        </div>
        <div class="hacts no-drag">
          <button class="hbtn" :title="expanded ? '还原客服窗口' : '放大客服窗口'" :aria-label="expanded ? '还原客服窗口' : '放大客服窗口'" :aria-pressed="expanded" @click="toggleExpanded"><IconArrowsMinimize v-if="expanded" :size="14" /><IconArrowsMaximize v-else :size="14" /></button>
          <button class="hbtn" title="新建会话" aria-label="新建会话" :disabled="sending" @click="requestReset"><IconRefresh :size="14" /></button>
          <button class="hbtn" title="关闭客服，保留会话" aria-label="关闭客服，保留会话" @click="closePanel"><IconX :size="16" /></button>
        </div>
      </div>

      <!-- 身份提示条 -->
      <div v-if="mode === 'admin'" class="idbar">
          <select
            v-if="mode === 'admin' && tenants?.length"
            v-model.number="chatLayer.tenantId"
            :disabled="sending"
            class="tsel"
            aria-label="目标租户"
            title="以哪个租户身份操作"
          >
            <option :value="0">默认租户</option>
            <option v-for="t in tenants" :key="t.id" :value="t.id">{{ t.name }}</option>
          </select>
        <IconShieldCheck :size="13" />
        <span>管理员代客模式{{ chatLayer.tenantId ? `，租户 #${chatLayer.tenantId}` : '，默认租户' }}，写操作需你确认</span>
      </div>

      <div v-if="resetRequested" class="reset-bar" role="group" aria-label="确认新建会话">
        <p>新建会话将清除当前对话与待确认操作，是否继续？</p>
        <div><button class="cno reset-cancel" @click="resetRequested = false; focusInput()">保留会话</button><button class="cyes" @click="reset">新建会话</button></div>
      </div>
      <div ref="listEl" @scroll="onScroll" class="plist" role="log" aria-live="polite" aria-relevant="additions" :aria-busy="sending">
        <div v-for="(m, i) in msgs" :key="i" class="brow" :class="m.role">
          <div v-if="m.role === 'agent'" class="avatar"><IconMessageCircle :size="14" /></div>
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
                <button class="cyes" :disabled="sending" @click="confirm(true)"><IconCheck :size="14" /> 确认执行</button>
                <button class="cno" :disabled="sending" @click="confirm(false)"><IconX :size="14" /> 取消</button>
              </div>
            </div>

            <!-- 已被取代的挂起：留痕但不可再点（防重复执行已过期操作） -->
            <div v-else-if="m.pendingStale" class="stale">
              <IconCheck :size="12" />
              <span>该操作的确认入口已关闭，请核对后续回复</span>
            </div>
          </div>
        </div>

        <!-- 快捷指令（对话为空或只有问候时） -->
        <div v-if="msgs.length <= 1 && !sending" class="quick">
          <template v-if="mode === 'portal' && go"><button class="qbtn" @click="go('access'); closePanel()">指引 API 接入</button><button class="qbtn" @click="go('wallet'); closePanel()">查看钱包记录</button><button class="qbtn" @click="go('plans'); closePanel()">查看月卡规则</button></template>
          <button v-for="q in quickActions" :key="q" class="qbtn" @click="send(q)">{{ q }}</button>
        </div>

        <div v-if="sending" class="brow agent">
          <div class="avatar"><IconMessageCircle :size="14" /></div>
          <div class="bubble typing"><i /><i /><i /></div>
        </div>
      </div>

      <button v-if="!followLatest" class="latest" @click="followLatest = true; scrollBottom(true)">回到最新消息</button>
      <div class="pfoot">
        <div class="composer">
          <textarea
            ref="inputEl"
            v-model="input"
            class="pin"
            rows="1"
            enterkeyhint="send"
            aria-label="客服消息"
            aria-describedby="chat-input-hint"
            :aria-invalid="!!inputError"
            :placeholder="sending ? '正在回复，可继续编辑下一条消息' : '输入你的问题…'"
            @compositionstart="composing = true"
            @compositionend="composing = false"
            @keydown="onInputKeydown"
          />
          <div class="composer-meta"><span id="chat-input-hint" :class="{ 'input-error': inputError }">{{ inputError || 'Enter 发送，Shift + Enter 换行' }}</span><span>{{ input.length }} / {{ INPUT_LIMIT }}</span></div>
        </div>
        <div class="send-actions">
          <button v-if="input" class="hbtn" aria-label="清空输入" title="清空输入" @click="clearInput"><IconX :size="15" /></button>
          <button class="psend" :disabled="sending || !input.trim() || !!inputError || composing" :aria-label="sending ? '正在发送' : '发送消息'" :title="sending ? '正在发送' : '发送消息'" @click="send()"><IconArrowUp :size="17" /></button>
        </div>
      </div>
    </section>
  </Transition>
</template>

<style scoped>
/* Hallmark · v2.0.0 · theme: custom · scope: app */
/* Critique: compact message bubbles, a fixed composer and clear service controls. */
/* Hallmark · waive 24b · inherited compact chat controls use 2px increments · keep the established input and message density */
/* ---------- 浮标（收起态） ---------- */
.launcher {
  white-space: nowrap;
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: var(--z-chat);
  width: 56px; height: 56px;
  border-radius: 50%;
  border: none;
  background: var(--ink-900);
  color: var(--primary-foreground);
  display: grid; place-items: center;
  box-shadow: var(--shadow-float);
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
  transform: translateY(-2px);
  filter: brightness(1.06);
}
.launcher:hover::after { opacity: 0; }
.launcher:active { transform: scale(.95); }

.badge {
  position: absolute; top: -3px; right: -3px;
  min-width: 20px; height: 20px; padding: 0 5px;
  border-radius: var(--r-pill);
  background: var(--danger); color: var(--primary-foreground);
  font-size: var(--fs-2xs); font-weight: 700;
  display: grid; place-items: center;
  box-shadow: 0 0 0 2px var(--bg), var(--shadow-float);
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
    inset 0 0 0 1px var(--hairline),
    var(--inset-hi-strong);
  overflow: hidden;
  min-width: 320px;
  transition: box-shadow var(--t-base);
}
.panel.dragging { box-shadow: var(--shadow-pop); }
.panel.dragging, .panel.resizing { user-select: none; }
.panel.resizing { transition: none; }

.resizer {
  position: absolute; left: 0; top: 0; bottom: 0; width: 5px;
  cursor: ew-resize; z-index: 2; touch-action: none;
}
.resizer:hover { background: color-mix(in srgb, var(--accent-500) 22%, transparent); }

.phead {
  touch-action: none;
  display: flex; align-items: center; justify-content: space-between; gap: var(--sp-2);
  padding: 12px var(--sp-2) 12px var(--sp-5);
  background: var(--surface);
  backdrop-filter: blur(16px) saturate(180%);
  cursor: grab;
  flex: none;
}
.panel.dragging .phead { cursor: grabbing; }
.panel.expanded .phead { cursor: default; }
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
  flex-wrap: wrap;
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
  background: var(--accent-600); color: var(--primary-foreground);
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
  color: var(--primary-foreground);
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
.sres { min-width: 0; white-space: pre-wrap; overflow-wrap: anywhere; }
.sname { max-width: 42%; overflow-wrap: anywhere; }
.step { align-items: flex-start; }

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
.cyes { background: var(--accent-600); color: var(--primary-foreground); box-shadow: var(--shadow-accent); }
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
  background: var(--accent-600); color: var(--primary-foreground);
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
.chatpop-enter-from { transform: translateY(14px) scale(.96); }
.chatpop-leave-to { transform: translateY(8px) scale(.98); }

@media (max-width: 720px) {
  .panel { min-width: 0; }
  .panel.expanded { border-radius: 0; }
  .resizer { display: none; }
  .phead { cursor: default; }
  .composer .pin { font-size: 16px; }
  .hbtn, .psend, .cyes, .cno, .qbtn { min-height: 44px; }
  .hbtn { width: 36px; }
  .launcher { right: 16px; bottom: max(16px, env(safe-area-inset-bottom)); }
}
/* Quiet support drawer */
.launcher { width: 46px; height: 46px; border-radius: 14px; background: var(--accent-600); box-shadow: var(--shadow-float); }
.launcher::after { display: none; } .launcher:hover { transform: translateY(-1px); }
.panel { border: 1px solid var(--hairline-2); box-shadow: var(--shadow-pop); border-radius: 12px; }
.phead { background: var(--surface); backdrop-filter: none; border-bottom: 1px solid var(--hairline); }
.plist { background: var(--surface-2); } .bubble { border-radius: 8px; }
.brow.agent .bubble { border: 1px solid var(--hairline); box-shadow: none; }
.qbtn { border: 1px solid var(--hairline); border-radius: 6px; box-shadow: none; }
.qbtn:hover { transform: none; box-shadow: none; background: var(--accent-50); }
.hbtn { border-radius: 6px; } .pin { border: 1px solid var(--hairline-2); border-radius: 6px; background: var(--surface); }
.composer { flex: 1; min-width: 0; }
.composer .pin { width: 100%; min-height: 42px; max-height: 150px; resize: none; overflow-y: auto; }
.composer-meta { display: flex; justify-content: space-between; gap: 6px; margin-top: 7px; font-size: 10px; color: var(--ink-400); }
.input-error { color: var(--danger); }
.send-actions { display: flex; flex-direction: column; align-items: center; gap: 5px; }
.pfoot { align-items: flex-start; }
button:focus-visible, textarea:focus-visible, select:focus-visible { outline: 2px solid var(--accent-500); outline-offset: 2px; }
button:disabled { cursor: not-allowed; }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { animation: none !important; transition: none !important; } }
.reset-bar { padding: 12px 16px; border-bottom: 1px solid var(--hairline); font-size: var(--fs-xs); background: var(--surface-2); }
.reset-bar p { margin-bottom: 10px; line-height: 1.6; }
.reset-bar > div { display: flex; gap: 8px; }
.latest { position: absolute; bottom: 130px; left: 50%; transform: translateX(-50%); padding: 8px 14px; border: 1px solid var(--hairline-2); border-radius: var(--r-pill); background: var(--surface); color: var(--ink-700); box-shadow: var(--shadow-xs); font-size: var(--fs-xs); white-space: nowrap; }
.plist { overscroll-behavior: contain; }
.hdot { background: var(--ink-500); box-shadow: none; }
.htext small { max-width: 200px; }
.hbtn:disabled { background: var(--surface-2); color: var(--ink-400); cursor: not-allowed; }
@media (max-width: 720px) { .htext small { max-width: 155px; } .idbar .tsel { height: 36px; } }
</style>