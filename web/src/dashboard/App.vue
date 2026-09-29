<script setup lang="ts">
import { ref, shallowRef, onMounted, computed } from 'vue'
import {
  IconLayoutDashboard, IconChartLine, IconBox, IconNetwork, IconActivity,
  IconReceipt2, IconCreditCard, IconScale, IconClipboardList, IconTerminal2,
  IconLogout, IconRefresh, IconShieldLock, IconChevronLeft, IconMenu2,
} from '@tabler/icons-vue'
import { authInitialized, authLogin, authSetup, authLogout, authMe, overviewApi } from './api'
import { toastErr, toastOk } from '../shared/utils/toast'
import ToastHost from '../shared/components/ui/ToastHost.vue'
import BaseButton from '../shared/components/ui/BaseButton.vue'
import Field from '../shared/components/ui/Field.vue'
import ChatDock from '../shared/components/ChatDock.vue'
import OverviewView from './views/OverviewView.vue'
import SalesView from './views/SalesView.vue'
import ModelsView from './views/ModelsView.vue'
import ChannelsView from './views/ChannelsView.vue'
import ProvidersView from './views/ProvidersView.vue'
import BillsView from './views/BillsView.vue'
import SubscriptionView from './views/SubscriptionView.vue'
import ReconcileView from './views/ReconcileView.vue'
import AuditView from './views/AuditView.vue'
import TryView from './views/TryView.vue'

type ViewKey = 'overview' | 'sales' | 'models' | 'channels' | 'providers'
  | 'bills' | 'subscription' | 'reconcile' | 'audit' | 'try'

interface NavItem { key: ViewKey; label: string; icon: any; group: string }

const navs: NavItem[] = [
  { key: 'overview', label: '总览', icon: IconLayoutDashboard, group: '运营' },
  { key: 'sales', label: '销售统计', icon: IconChartLine, group: '运营' },
  { key: 'bills', label: '账单明细', icon: IconReceipt2, group: '运营' },
  { key: 'subscription', label: '订阅入账', icon: IconCreditCard, group: '运营' },
  { key: 'models', label: '商品管理', icon: IconBox, group: '供给' },
  { key: 'channels', label: '渠道管理', icon: IconNetwork, group: '供给' },
  { key: 'providers', label: '熔断观测', icon: IconActivity, group: '供给' },
  { key: 'reconcile', label: '对账检查', icon: IconScale, group: '系统' },
  { key: 'audit', label: '客服审计', icon: IconClipboardList, group: '系统' },
  { key: 'try', label: '接口调试', icon: IconTerminal2, group: '系统' },
]
const groups = ['运营', '供给', '系统']
const grouped = computed(() => groups.map(g => ({ g, items: navs.filter(n => n.group === g) })))

const views: Record<ViewKey, any> = {
  overview: OverviewView, sales: SalesView, models: ModelsView, channels: ChannelsView,
  providers: ProvidersView, bills: BillsView, subscription: SubscriptionView,
  reconcile: ReconcileView, audit: AuditView, try: TryView,
}

const status = ref<'loading' | 'setup' | 'login' | 'in'>('loading')
const username = ref('')
const pass = ref('')
const pass2 = ref('')
const busy = ref(false)
const err = ref('')
const cur = shallowRef<ViewKey>('overview')
const user = ref('')
const refreshTick = ref(0)
const collapsed = ref(false)
const mobileOpen = ref(false)
const tenants = ref<{ id: number; name: string }[]>([])

const descriptions: Record<ViewKey, string> = { overview: '查看网关运行状态、资金与模型使用情况', sales: '追踪收入、消耗和模型销售表现', models: '管理可用模型、定价与展示信息', channels: '配置上游渠道与模型映射', providers: '查看节点健康状态与熔断记录', bills: '查询请求账单与结算明细', subscription: '管理套餐订阅与资金入账', reconcile: '核对账本余额与缓存投影', audit: '查看客服操作、用户确认与安全判定', try: '发送请求，验证模型接口' }

const curLabel = computed(() => navs.find(n => n.key === cur.value)?.label ?? '')

async function boot() {
  status.value = 'loading'
  try {
    const ini = await authInitialized()
    if (!ini.initialized) { status.value = 'setup'; return }
    try {
      const m = await authMe()
      user.value = m.username
      status.value = 'in'
      loadTenants()
    } catch {
      status.value = 'login'
    }
  } catch (e: any) {
    toastErr(e?.message || '无法连接网关')
    status.value = 'login'
  }
}

async function loadTenants() {
  try {
    const ov = await overviewApi()
    tenants.value = ov.tenants.map(t => ({ id: t.id, name: t.name }))
  } catch { /* ignore */ }
}

onMounted(boot)

async function submit() {
  err.value = ''
  if (!username.value.trim()) { err.value = '请输入用户名'; return }
  if (pass.value.length < 6) { err.value = '密码至少 6 位'; return }
  if (status.value === 'setup' && pass.value !== pass2.value) { err.value = '两次密码不一致'; return }
  busy.value = true
  try {
    if (status.value === 'setup') {
      await authSetup(username.value, pass.value)
      toastOk('管理员已创建')
    } else {
      await authLogin(username.value, pass.value)
    }
    user.value = username.value
    status.value = 'in'
    loadTenants()
  } catch (e: any) {
    err.value = e?.message || '登录失败'
  } finally {
    busy.value = false
  }
}

async function logout() {
  try { await authLogout() } catch { /* ignore */ }
  status.value = 'login'
  pass.value = ''
}
</script>

<template>
  <ToastHost />

  <!-- 登录 / 初始化 -->
  <div v-if="status !== 'in'" class="gate">
    <div v-if="status === 'loading'" class="gate-card" role="status" aria-live="polite">正在连接控制台…</div>
    <div v-else class="gate-card">
      <div class="brand">
        <div class="mark"><IconShieldLock :size="20" stroke-width="1.9" /></div>
        <div>
          <h1 class="bt">模型商城 · 管理后台</h1>
          <p class="bs">{{ status === 'setup' ? '首次使用，创建管理员账号' : '使用管理员账号登录' }}</p>
        </div>
      </div>
      <form @submit.prevent="submit">
        <Field label="用户名" required>
          <input v-model="username" autocomplete="username" placeholder="admin" />
        </Field>
        <Field label="密码" required>
          <input
            v-model="pass"
            type="password"
            :autocomplete="status === 'setup' ? 'new-password' : 'current-password'"
            placeholder="至少 6 位"
          />
        </Field>
        <Field v-if="status === 'setup'" label="确认密码" required>
          <input v-model="pass2" type="password" autocomplete="new-password" />
        </Field>
        <p v-if="err" class="gerr">{{ err }}</p>
        <BaseButton variant="primary" block size="lg" :loading="busy" type="submit">
          {{ status === 'setup' ? '创建并进入' : '登录' }}
        </BaseButton>
      </form>
    </div>
  </div>

  <!-- 主界面 -->
  <div v-else class="app" :class="{ collapsed, mobileOpen }" @keydown.esc="mobileOpen = false">
    <button v-if="mobileOpen" class="nav-backdrop" aria-label="关闭导航" @click="mobileOpen = false" />
    <aside class="side">
      <div class="sideInner">
        <div class="sbrand">
          <div class="mark sm"><IconShieldLock :size="15" stroke-width="1.9" /></div>
          <div v-if="!collapsed || mobileOpen" class="stext">
            <strong>AI Gateway</strong>
            <small>管理控制台</small>
          </div>
        </div>

        <nav class="snav" aria-label="管理导航">
          <template v-for="grp in grouped" :key="grp.g">
            <p v-if="!collapsed || mobileOpen" class="sgroup">{{ grp.g }}</p>
            <p v-else class="sgroup mini">·</p>
            <button
              v-for="n in grp.items"
              :key="n.key"
              class="sitem"
              :class="{ on: cur === n.key }"
              :title="collapsed ? n.label : undefined"
              @click="cur = n.key; mobileOpen = false"
              :aria-current="cur === n.key ? 'page' : undefined"
              :aria-label="n.label"
            >
              <component :is="n.icon" :size="17" :stroke-width="1.7" />
              <span v-if="!collapsed || mobileOpen">{{ n.label }}</span>
            </button>
          </template>
        </nav>

        <button class="scollapse" aria-label="切换侧栏宽度" @click="collapsed = !collapsed">
          <IconChevronLeft :size="15" :style="{ transform: collapsed ? 'rotate(180deg)' : 'none' }" />
          <span v-if="!collapsed || mobileOpen">收起</span>
        </button>
      </div>
    </aside>

    <div class="main">
      <!-- 控制台顶栏 -->
      <header class="top">
        <div class="topInner">
          <div class="tleft">
            <button class="mobmenu" aria-label="打开导航" :aria-expanded="mobileOpen" @click="mobileOpen = !mobileOpen"><IconMenu2 :size="17" /></button>
            <span class="breadcrumb">管理控制台 <span>/</span></span><span class="tt">{{ curLabel }}</span>
          </div>
          <div class="tright">
            <BaseButton variant="secondary" size="sm" @click="refreshTick++">
              <IconRefresh :size="14" /> 刷新
            </BaseButton>
            <span class="user">{{ user }}</span>
            <button class="logout" title="退出登录" @click="logout"><IconLogout :size="15" /></button>
          </div>
        </div>
      </header>

      <main class="content">
        <div class="page-heading"><div><h1>{{ curLabel }}</h1><p>{{ descriptions[cur] }}</p></div><span class="page-meta">AI Gateway / 控制台</span></div>
        <component :is="views[cur]" :key="cur + ':' + refreshTick" />
      </main>
    </div>

    <!-- 管理员也能用 AI 客服（代客模式，可切目标租户） -->
    <ChatDock
      mode="admin"
      :tenants="tenants"
      title="运营助手"
      subtitle="代客查询 · 充值 · 退款 · 订阅"
    />
  </div>
</template>

<style scoped>
.gate { min-height: 100dvh; display: grid; place-items: center; padding: 24px; background: var(--bg); }
.gate-card { width: 100%; max-width: 400px; padding: 32px; background: var(--surface); border: 1px solid var(--hairline); border-radius: 12px; }
.brand { display: flex; align-items: center; gap: 12px; margin-bottom: 28px; }
.mark { width: 40px; height: 40px; border-radius: 10px; background: var(--accent-600); color: #fff; display: grid; place-items: center; flex: none; }
.mark.sm { width: 30px; height: 30px; border-radius: 8px; }
.bt { font-size: 18px; } .bs { font-size: 12px; color: var(--ink-500); margin-top: 5px; }
.gerr { padding: 10px; background: var(--danger-bg); color: var(--danger); border-radius: 6px; margin-bottom: 16px; font-size: 12px; }
.app { min-height: 100dvh; display: grid; grid-template-columns: 216px minmax(0, 1fr); }
.app.collapsed { grid-template-columns: 68px minmax(0, 1fr); }
.side { position: sticky; top: 0; height: 100dvh; background: var(--surface); border-right: 1px solid var(--hairline); }
.sideInner { display: flex; flex-direction: column; height: 100%; padding: 0 12px 16px; }
.sbrand { display: flex; align-items: center; gap: 10px; height: 65px; padding: 0 8px; flex: none; }
.stext { display: flex; flex-direction: column; } .stext strong { font-size: 15px; font-weight: 650; color: var(--ink-900); } .stext small { font-size: 11px; color: var(--ink-400); }
.snav { flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 4px; padding-top: 12px; }
.sgroup { padding: 16px 12px 6px; font-size: 11px; color: var(--ink-400); } .sgroup.mini { text-align: center; }
.sitem { display: flex; align-items: center; gap: 10px; min-height: 38px; padding: 0 12px; border: none; border-radius: 6px; background: transparent; color: var(--ink-600); font-size: 13px; text-align: left; white-space: nowrap; transition: background var(--t-fast), color var(--t-fast); }
.sitem:hover { background: var(--well); color: var(--ink-900); } .sitem.on { background: var(--accent-50); color: var(--accent-700); font-weight: 600; }
.scollapse { display: flex; align-items: center; justify-content: center; gap: 8px; height: 34px; border: 1px solid var(--hairline); border-radius: 6px; background: var(--surface); color: var(--ink-500); font-size: 12px; }
.main { min-width: 0; } .top { position: sticky; top: 0; z-index: var(--z-sticky); }
.topInner { height: 65px; padding: 0 28px; display: flex; align-items: center; justify-content: space-between; gap: 16px; background: var(--surface); border-bottom: 1px solid var(--hairline); }
.tleft, .tright { display: flex; align-items: center; gap: 12px; } .tt { font-size: 13px; color: var(--ink-800); } .breadcrumb { color: var(--ink-400); font-size: 13px; } .breadcrumb span { margin-left: 12px; }
.user { padding: 5px 10px; background: var(--well); border-radius: 6px; font-size: 12px; color: var(--ink-600); }
.logout, .mobmenu { width: 32px; height: 32px; border: none; border-radius: 6px; background: transparent; color: var(--ink-500); display: grid; place-items: center; }
.logout:hover { background: var(--danger-bg); color: var(--danger); } .mobmenu { display: none; }
.content { display: flex; flex-direction: column; gap: 24px; width: 100%; max-width: 1640px; margin: 0 auto; padding: 28px 28px 64px; }
.nav-backdrop { display: none; }
@media (max-width: 900px) {
  .app, .app.collapsed { grid-template-columns: minmax(0, 1fr); }
  .side { position: fixed; left: 0; top: 0; z-index: var(--z-drawer); width: 216px; transform: translateX(-100%); transition: transform var(--t-base); }
  .mobileOpen .side { transform: translateX(0); } .mobileOpen .side .stext { display: flex; }
  .nav-backdrop { display: block; position: fixed; inset: 0; border: 0; background: rgb(20 24 35 / .25); z-index: 39; }
  .mobmenu { display: grid; } .scollapse { display: none; } .topInner { padding: 0 16px; height: 58px; }
  .content { padding: 20px 16px 64px; gap: 20px; } .breadcrumb { display: none; }
}
</style>
