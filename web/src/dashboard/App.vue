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
const tenants = ref<{ id: number; name: string }[]>([])

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
    <div class="gate-card">
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
  <div v-else class="app" :class="{ collapsed }">
    <!-- 浮动岛侧边栏：不贴边，圆角浮起 -->
    <aside class="side">
      <div class="sideInner">
        <div class="sbrand">
          <div class="mark sm"><IconShieldLock :size="15" stroke-width="1.9" /></div>
          <div v-if="!collapsed" class="stext">
            <strong>模型商城</strong>
            <small>管理后台</small>
          </div>
        </div>

        <nav class="snav">
          <template v-for="grp in grouped" :key="grp.g">
            <p v-if="!collapsed" class="sgroup">{{ grp.g }}</p>
            <p v-else class="sgroup mini">·</p>
            <button
              v-for="n in grp.items"
              :key="n.key"
              class="sitem"
              :class="{ on: cur === n.key }"
              :title="collapsed ? n.label : undefined"
              @click="cur = n.key"
            >
              <component :is="n.icon" :size="17" :stroke-width="1.7" />
              <span v-if="!collapsed">{{ n.label }}</span>
            </button>
          </template>
        </nav>

        <button class="scollapse" @click="collapsed = !collapsed">
          <IconChevronLeft :size="15" :style="{ transform: collapsed ? 'rotate(180deg)' : 'none' }" />
          <span v-if="!collapsed">收起</span>
        </button>
      </div>
    </aside>

    <div class="main">
      <!-- 浮动岛顶栏 -->
      <header class="top">
        <div class="topInner">
          <div class="tleft">
            <button class="mobmenu" @click="collapsed = !collapsed"><IconMenu2 :size="17" /></button>
            <h1 class="tt">{{ curLabel }}</h1>
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
/* ---------- 登录 ---------- */
.gate {
  min-height: 100dvh; display: grid; place-items: center;
  padding: var(--sp-5);
  background:
    radial-gradient(880px 460px at 88% -16%, var(--accent-50), transparent 60%),
    radial-gradient(700px 380px at -10% 108%, var(--accent-50), transparent 56%),
    var(--bg);
}
.gate-card {
  width: 100%; max-width: 404px;
  background: var(--surface);
  border-radius: var(--r-2xl);
  box-shadow: var(--shadow-pop), var(--inset-hi-strong);
  padding: var(--sp-8) var(--sp-7) var(--sp-7);
  animation: gate-in .7s var(--ease-out) both;
}
@keyframes gate-in {
  from { opacity: 0; transform: translateY(22px) scale(.985); filter: blur(6px); }
}
.brand { display: flex; align-items: center; gap: var(--sp-4); margin-bottom: var(--sp-7); }
.mark {
  width: 46px; height: 46px; border-radius: var(--r-md);
  background: var(--ink-900); color: #fff;
  display: grid; place-items: center; flex: none;
  box-shadow: var(--shadow-card);
}
.mark.sm { width: 32px; height: 32px; border-radius: var(--r-sm); background: var(--accent-600); box-shadow: var(--shadow-accent); }
.bt { font-size: var(--fs-xl); letter-spacing: -.028em; }
.bs { font-size: var(--fs-xs); color: var(--ink-500); margin-top: 4px; }
.gerr {
  font-size: var(--fs-xs); color: var(--danger);
  background: var(--danger-bg);
  padding: 9px 13px; border-radius: var(--r-sm);
  margin: 0 0 var(--sp-4);
}

/* ---------- 壳体：页面留白 + 浮动岛 ---------- */
.app {
  min-height: 100dvh;
  display: grid;
  grid-template-columns: 236px 1fr;
  gap: var(--sp-5);
  padding: var(--sp-5);
  background: var(--bg);
}
.app.collapsed { grid-template-columns: 78px 1fr; }

/* 浮动岛侧边栏：不贴边、圆角、浮起 */
.side {
  position: sticky; top: var(--sp-5);
  height: calc(100dvh - var(--sp-5) * 2);
}
.sideInner {
  height: 100%;
  display: flex; flex-direction: column;
  gap: var(--sp-4);
  padding: var(--sp-4);
  background: var(--surface);
  border-radius: var(--r-xl);
  box-shadow: var(--shadow-card), var(--inset-hi);
}
.sbrand {
  display: flex; align-items: center; gap: 10px;
  padding: var(--sp-2) var(--sp-2) var(--sp-4);
}
.stext { display: flex; flex-direction: column; min-width: 0; }
.stext strong {
  font-size: var(--fs-sm); font-weight: 650;
  color: var(--ink-900); letter-spacing: -.018em;
}
.stext small { font-size: var(--fs-2xs); color: var(--ink-400); }

.snav { display: flex; flex-direction: column; gap: 2px; flex: 1; overflow-y: auto; }
.sgroup {
  font-size: 10px; font-weight: 600;
  letter-spacing: .14em; text-transform: uppercase;
  color: var(--ink-400);
  padding: var(--sp-4) var(--sp-3) 7px;
}
.sgroup.mini { text-align: center; letter-spacing: 0; }
.sitem {
  position: relative;
  display: flex; align-items: center; gap: 11px;
  height: 37px; padding: 0 11px;
  border: none; border-radius: var(--r-md);
  background: transparent;
  color: var(--ink-500);
  font-size: var(--fs-sm); font-weight: 540;
  text-align: left;
  white-space: nowrap;
  transition: background var(--t-fast), color var(--t-fast), transform var(--t-spring);
}
.sitem:hover { background: var(--well); color: var(--ink-800); }
.sitem:active { transform: scale(.985); }
.sitem.on {
  background: var(--accent-600);
  color: #fff;
  box-shadow: var(--shadow-accent);
}
/* 选中态左侧指示条 */
.sitem.on::before {
  content: '';
  position: absolute; left: -9px; top: 50%;
  width: 3px; height: 17px;
  margin-top: -8.5px;
  border-radius: var(--r-pill);
  background: var(--accent-500);
}

.scollapse {
  display: flex; align-items: center; justify-content: center; gap: 6px;
  height: 34px;
  border: none; border-radius: var(--r-md);
  background: var(--well);
  box-shadow: var(--inset-well);
  color: var(--ink-500);
  font-size: var(--fs-xs); font-weight: 540;
  transition: background var(--t-fast), color var(--t-fast);
}
.scollapse:hover { background: var(--well-2); color: var(--ink-800); }

.main { display: flex; flex-direction: column; min-width: 0; }

/* 浮动岛顶栏 */
.top {
  position: sticky; top: var(--sp-5);
  z-index: var(--z-sticky);
  margin-bottom: var(--sp-5);
}
.topInner {
  display: flex; align-items: center; justify-content: space-between;
  gap: var(--sp-4);
  height: 60px;
  padding: 0 var(--sp-4) 0 var(--sp-6);
  background: rgb(255 255 255 / .82);
  backdrop-filter: blur(20px) saturate(180%);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-card), var(--inset-hi-strong);
}
.tleft { display: flex; align-items: center; gap: var(--sp-2); min-width: 0; }
.mobmenu {
  display: none;
  width: 34px; height: 34px;
  border: none; border-radius: var(--r-sm);
  background: transparent; color: var(--ink-500);
}
.tt {
  font-size: var(--fs-xl);
  font-weight: 640;
  letter-spacing: -.028em;
  white-space: nowrap;
}
.tright { display: flex; align-items: center; gap: var(--sp-2); }
.user {
  font-size: var(--fs-xs); font-weight: 540; color: var(--ink-600);
  padding: 6px 13px;
  background: var(--well);
  border-radius: var(--r-pill);
  box-shadow: var(--inset-well);
}
.logout {
  width: 34px; height: 34px;
  display: grid; place-items: center;
  border: none; border-radius: 50%;
  background: transparent; color: var(--ink-400);
  transition: background var(--t-fast), color var(--t-fast), transform var(--t-spring);
}
.logout:hover { background: var(--danger-bg); color: var(--danger); transform: rotate(-8deg); }

/* 内容：宏留白 */
.content {
  flex: 1;
  padding: 0 var(--sp-1) var(--sp-9);
  max-width: 1560px; width: 100%;
}

@media (max-width: 1100px) {
  .app, .app.collapsed { grid-template-columns: 1fr; padding: var(--sp-4); gap: var(--sp-4); }
  .side {
    position: fixed; left: var(--sp-4); top: var(--sp-4);
    z-index: var(--z-drawer);
    width: 236px; height: calc(100dvh - var(--sp-4) * 2);
    transform: translateX(calc(-100% - var(--sp-5)));
    transition: transform var(--t-base);
  }
  .app:not(.collapsed) .side { transform: translateX(0); }
  .app:not(.collapsed) .sideInner { box-shadow: var(--shadow-pop); }
  .mobmenu { display: grid; place-items: center; }
  .top { top: var(--sp-4); }
  .tt { font-size: var(--fs-lg); }
  .content { padding-bottom: var(--sp-8); }
}
</style>