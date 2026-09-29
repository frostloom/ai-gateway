<script setup lang="ts">
import { ref, shallowRef, provide, onMounted, computed } from 'vue'
import {
  IconShoppingBag, IconCategory, IconCreditCard, IconReceipt2,
  IconTrendingUp, IconTerminal2, IconWallet, IconLogout, IconUserShield, IconMenu2,
} from '@tabler/icons-vue'
import { api, auth, ApiError } from '../shared/api'
import { money } from '../shared/utils/format'
import { toastErr } from '../shared/utils/toast'
import BaseButton from '../shared/components/ui/BaseButton.vue'
import ToastHost from '../shared/components/ui/ToastHost.vue'
import ChatDock from '../shared/components/ChatDock.vue'
import HomeView from './views/HomeView.vue'
import CatalogView from './views/CatalogView.vue'
import PlansView from './views/PlansView.vue'
import OrdersView from './views/OrdersView.vue'
import ConsumptionView from './views/ConsumptionView.vue'
import ConsoleView from './views/ConsoleView.vue'

type ViewKey = 'home' | 'catalog' | 'plans' | 'orders' | 'consumption' | 'console'

const navs: { key: ViewKey; label: string; icon: any }[] = [
  { key: 'home', label: '工作台', icon: IconShoppingBag },
  { key: 'catalog', label: '模型广场', icon: IconCategory },
  { key: 'plans', label: '订阅套餐', icon: IconCreditCard },
  { key: 'orders', label: '我的订单', icon: IconReceipt2 },
  { key: 'consumption', label: '用量统计', icon: IconTrendingUp },
  { key: 'console', label: '接口调试', icon: IconTerminal2 },
]

const views: Record<ViewKey, any> = {
  home: HomeView, catalog: CatalogView, plans: PlansView,
  orders: OrdersView, consumption: ConsumptionView, console: ConsoleView,
}

const mobileOpen = ref(false)
const currentLabel = computed(() => navs.find(n => n.key === cur.value)?.label || '工作台')
const logged = ref(false)
const busy = ref(false)
const keyInput = ref('')
const cur = shallowRef<ViewKey>('home')
const balance = ref<number | null>(null)

const masked = (k: string) => (k.length <= 10 ? k : k.slice(0, 6) + '…' + k.slice(-4))

async function tryLogin() {
  const k = keyInput.value.trim()
  if (!k) return
  busy.value = true
  try {
    await api('/portal/me', { key: k })
    auth.setKey(k)
    logged.value = true
    refreshMe()
  } catch (e) {
    toastErr(e instanceof ApiError ? e.message : '登录失败')
  } finally {
    busy.value = false
  }
}

async function refreshMe() {
  try {
    const me = await api<any>('/portal/me')
    balance.value = me.balance_cent
  } catch { /* ignore */ }
}

function logout() {
  auth.clearKey()
  auth.clearSid()
  logged.value = false
  balance.value = null
  keyInput.value = ''
}

provide('go', (v: ViewKey) => { cur.value = v; window.scrollTo({ top: 0 }) })

onMounted(() => {
  if (auth.getKey()) {
    keyInput.value = auth.getKey()
    logged.value = true
    refreshMe()
  }
})
</script>

<template>
  <ToastHost />

  <!-- 登录闸门 -->
  <div v-if="!logged" class="gate">
    <div class="gate-card">
      <div class="brand">
        <div class="mark"><IconShoppingBag :size="21" stroke-width="1.9" /></div>
        <div>
          <h1 class="bt">AI 模型商城</h1>
          <p class="bs">多租户模型网关 · 按量计费 · 商品即模型额度</p>
        </div>
      </div>
      <label class="glabel" for="apikey">租户 API Key</label>
      <div class="krow">
        <input
          id="apikey"
          v-model="keyInput"
          class="keyin"
          type="password"
          placeholder="sk-..."
          autocomplete="off"
          spellcheck="false"
          @keyup.enter="tryLogin"
        />
        <BaseButton variant="primary" size="lg" :loading="busy" @click="tryLogin">进入商城</BaseButton>
      </div>
      <p class="ghint">Key 仅保存在本地浏览器，用于调用 <code>/portal</code> 与 <code>/v1</code> 接口。</p>
    </div>
  </div>

  <!-- 商城主体 -->
  <div v-else class="app" :class="{ mobileOpen }" @keydown.esc="mobileOpen = false">
    <button v-if="mobileOpen" class="nav-backdrop" aria-label="关闭导航" @click="mobileOpen = false" />
    <aside class="sidebar">
      <a class="portal-brand" href="/portal"><div class="mark sm"><IconShoppingBag :size="17" /></div><div><strong>AI Gateway</strong><small>用户控制台</small></div></a>
      <p class="nav-group">工作空间</p>
      <nav class="nav" aria-label="主导航">
        <button v-for="n in navs" :key="n.key" class="navitem" :class="{ on: cur === n.key }" :aria-current="cur === n.key ? 'page' : undefined" @click="cur = n.key; mobileOpen = false"><component :is="n.icon" :size="17" :stroke-width="1.7" />{{ n.label }}</button>
      </nav>
      <div class="sidebar-foot"><span>统一模型接口</span><code>OpenAI compatible</code></div>
    </aside>
    <header class="topbar">
      <div class="tleft">
        <button class="icon-btn mobile-menu" aria-label="打开导航" :aria-expanded="mobileOpen" @click="mobileOpen = !mobileOpen"><IconMenu2 :size="18" /></button><span class="crumb">用户控制台 /</span><span class="tb">{{ currentLabel }}</span>
      </div>

      <div class="tright">
        <span class="bal num" title="可用余额">
          <IconWallet :size="14" />
          {{ money(balance) }}
        </span>
        <a class="icon-btn" href="/" target="_blank" title="管理面板">
          <IconUserShield :size="16" />
        </a>
        <span class="keychip mono" title="当前 API Key">{{ masked(keyInput) }}</span>
        <button class="icon-btn danger" title="退出登录" @click="logout">
          <IconLogout :size="16" />
        </button>
      </div>
    </header>

    <main class="content">
      <component :is="views[cur]" :key="cur" @me="refreshMe" />
    </main>

    <!-- 全局悬浮客服（跨栏目常驻、可拖拽/最小化）-->
    <ChatDock mode="portal" />
  </div>
</template>

<style scoped>
/* ---------- 登录 ---------- */
.gate {
  min-height: 100dvh;
  display: grid; place-items: center;
  padding: var(--sp-5);
  background:
    radial-gradient(900px 460px at 88% -12%, var(--accent-50), transparent 62%),
    radial-gradient(700px 380px at -6% 108%, var(--accent-50), transparent 58%),
    var(--paper-2);
}
.gate-card {
  width: 100%; max-width: 430px;
  background: var(--paper);
  border: 1px solid var(--hairline);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-2);
  padding: var(--sp-7);
}
.brand { display: flex; align-items: center; gap: var(--sp-3); margin-bottom: var(--sp-6); }
.mark {
  width: 42px; height: 42px;
  border-radius: var(--r-md);
  background: var(--accent-600); color: #fff;
  display: grid; place-items: center;
  box-shadow: var(--shadow-accent);
  flex: none;
}
.mark.sm { width: 27px; height: 27px; border-radius: var(--r-sm); box-shadow: none; }
.bt { font-size: var(--fs-xl); letter-spacing: -.02em; }
.bs { font-size: var(--fs-xs); color: var(--ink-500); margin-top: 2px; }
.glabel {
  display: block;
  font-size: var(--fs-xs); font-weight: 600; color: var(--ink-600);
  margin-bottom: 6px;
}
.krow { display: flex; gap: var(--sp-2); }
.keyin {
  flex: 1; height: 42px; padding: 0 var(--sp-3);
  border: none; border-radius: var(--r-sm);
  font-family: var(--font-mono); font-size: var(--fs-sm);
  transition: border-color var(--t-fast), box-shadow var(--t-fast);
}
.keyin:focus {
  outline: none;
  border-color: var(--accent-500);
  box-shadow: 0 0 0 3px var(--accent-50);
}
.ghint { margin-top: var(--sp-3); font-size: var(--fs-xs); color: var(--ink-400); line-height: 1.6; }
.ghint code {
  font-family: var(--font-mono); font-size: .92em;
  background: var(--paper-3); padding: 1px 5px; border-radius: var(--r-xs);
  color: var(--ink-600);
}

/* ---------- 壳体 ---------- */
.app { min-height: 100dvh; display: flex; flex-direction: column; }

.topbar {
  position: sticky; top: 0; z-index: var(--z-sticky);
  display: flex; align-items: center; gap: var(--sp-5);
  height: 58px; padding: 0 var(--sp-6);
  background: rgb(255 255 255 / .82);
  backdrop-filter: blur(14px) saturate(180%);
  border-bottom: 1px solid var(--hairline);
}
.tleft { display: flex; align-items: center; gap: 9px; flex: none; }
.tb {
  font-size: var(--fs-md); font-weight: 650;
  letter-spacing: -.02em; color: var(--ink-900);
  white-space: nowrap;
}

.nav { display: flex; gap: 2px; flex: 1; overflow-x: auto; scrollbar-width: none; }
.nav::-webkit-scrollbar { display: none; }
.navitem {
  border: none; background: transparent;
  color: var(--ink-500);
  font-size: var(--fs-sm); font-weight: 550;
  padding: 7px 13px;
  border-radius: var(--r-sm);
  white-space: nowrap;
  transition: all var(--t-fast);
}
.navitem:hover { color: var(--ink-800); background: var(--paper-3); }
.navitem.on {
  color: var(--accent-700);
  background: var(--accent-50);
  box-shadow: inset 0 0 0 1px var(--accent-100);
}

.tright { display: flex; align-items: center; gap: var(--sp-2); flex: none; }
.bal {
  display: inline-flex; align-items: center; gap: 5px;
  height: 30px; padding: 0 12px;
  border-radius: var(--r-pill);
  background: var(--accent-50);
  border: 1px solid var(--accent-100);
  color: var(--accent-700);
  font-size: var(--fs-sm); font-weight: 650;
}
.icon-btn {
  width: 30px; height: 30px;
  display: grid; place-items: center;
  border: none; border-radius: var(--r-sm);
  background: transparent; color: var(--ink-400);
  transition: all var(--t-fast);
}
.icon-btn:hover { background: var(--paper-3); color: var(--ink-800); }
.icon-btn.danger:hover { background: var(--danger-bg); color: var(--danger); }
.keychip {
  font-size: var(--fs-2xs); color: var(--ink-400);
  padding: 4px 9px;
  background: var(--paper-3);
  border-radius: var(--r-pill);
}

.content {
  flex: 1;
  width: 100%; max-width: 1240px;
  margin: 0 auto;
  padding: var(--sp-6) var(--sp-6) var(--sp-10);
}

@media (max-width: 900px) {
  .topbar { flex-wrap: wrap; height: auto; gap: var(--sp-3); padding: var(--sp-3) var(--sp-4); }
  .nav { order: 3; width: 100%; }
  .keychip { display: none; }
  .content { padding: var(--sp-4) var(--sp-4) var(--sp-9); }
}
/* Console shell */
.gate { background: var(--bg); } .gate-card { box-shadow: none; border: 1px solid var(--hairline); padding: 32px; }
.krow { flex-direction: column; } .keyin { flex: auto; width: 100%; background: var(--surface); border: 1px solid var(--hairline-2); }
.app { display: grid; grid-template-columns: 216px minmax(0, 1fr); grid-template-rows: 65px 1fr; }
.sidebar { position: sticky; top: 0; grid-row: 1 / 3; height: 100dvh; background: var(--surface); border-right: 1px solid var(--hairline); display: flex; flex-direction: column; padding: 0 12px 20px; }
.portal-brand { height: 65px; display: flex; align-items: center; gap: 10px; padding: 0 8px; color: var(--ink-900); }
.portal-brand strong { font-size: 15px; font-weight: 650; } .portal-brand small { display: block; font-size: 11px; color: var(--ink-400); }
.nav-group { margin: 28px 12px 8px; color: var(--ink-400); font-size: 11px; }
.nav { display: flex; flex-direction: column; flex: 1; gap: 4px; overflow: auto; }
.navitem { display: flex; align-items: center; gap: 10px; text-align: left; padding: 10px 12px; min-height: 38px; border-radius: 6px; }
.navitem.on { box-shadow: none; font-weight: 600; }
.sidebar-foot { display: flex; flex-direction: column; gap: 4px; padding: 14px 12px 0; border-top: 1px solid var(--hairline); color: var(--ink-400); font-size: 11px; }
.sidebar-foot code { font-size: 10px; }
.topbar { height: 65px; padding: 0 28px; justify-content: space-between; background: var(--surface); backdrop-filter: none; }
.tb { font-size: 13px; font-weight: 500; } .crumb { font-size: 13px; color: var(--ink-400); margin-right: 5px; }
.bal { background: var(--well); color: var(--ink-700); border: 0; border-radius: 6px; }
.keychip { border-radius: 6px; } .mobile-menu, .nav-backdrop { display: none; }
.content { max-width: 1480px; padding: 28px 28px 64px; min-width: 0; }
@media (max-width: 900px) {
 .app { grid-template-columns: minmax(0, 1fr); grid-template-rows: 58px 1fr; }
 .sidebar { position: fixed; left: 0; top: 0; width: 216px; z-index: var(--z-drawer); transform: translateX(-100%); transition: transform var(--t-base); }
 .mobileOpen .sidebar { transform: translateX(0); }
 .nav { order: initial; width: auto; } .nav-backdrop { display: block; position: fixed; inset: 0; z-index: 39; border: 0; background: rgb(20 24 35 / .25); }
 .mobile-menu { display: grid; } .topbar { height: 58px; flex-wrap: nowrap; padding: 0 16px; gap: 8px; } .crumb { display: none; }
 .content { padding: 20px 16px 64px; }
}
</style>