<script setup lang="ts">
import { ref, computed, provide, onMounted, onUnmounted } from "vue";
import {
  IconMessageCircle,
  IconLogout,
  IconMenu2,
  IconWallet,
} from "@tabler/icons-vue";
import { api, auth } from "../shared/api";
import { openChat, closeChat } from "../shared/chatLayer";
import { money } from "../shared/utils/format";
import { toastErr } from "../shared/utils/toast";
import ToastHost from "../shared/components/ui/ToastHost.vue";
import AuthGate from "../shared/components/AuthGate.vue";
import ChatDock from "../shared/components/ChatDock.vue";
import HomeView from "./views/HomeView.vue";
import CatalogView from "./views/CatalogView.vue";
import PlansView from "./views/PlansView.vue";
import OrdersView from "./views/OrdersView.vue";
import ConsumptionView from "./views/ConsumptionView.vue";
import ConsoleView from "./views/ConsoleView.vue";
import AccessView from "./views/AccessView.vue";
import WalletView from "./views/WalletView.vue";

const navs = [
  { key: "home", label: "概览", view: HomeView },
  { key: "catalog", label: "模型目录", view: CatalogView },
  { key: "console", label: "工作台", view: ConsoleView },
  { key: "wallet", label: "余额与兑换", view: WalletView },
  { key: "plans", label: "月卡套餐", view: PlansView },
  { key: "consumption", label: "用量", view: ConsumptionView },
  { key: "orders", label: "订单", view: OrdersView },
  { key: "access", label: "API 接入", view: AccessView },
];
const cur = ref("home");
const current = computed(
  () => navs.find((n) => n.key === cur.value) || navs[0]!,
);
const logged = ref(false);
const loading = ref(true);
const username = ref("");
const workbenchAccount = ref("");
provide("workbench-account", workbenchAccount);
interface PortalIdentity {
  id?: number;
  tenant_id?: number;
  username: string;
  role?: "admin" | "user";
}
function setIdentity(identity: PortalIdentity) {
  username.value = identity.username;
  workbenchAccount.value = Number.isSafeInteger(identity.id) && Number.isSafeInteger(identity.tenant_id) &&
    identity.id! > 0 && identity.tenant_id! > 0 ? `${identity.tenant_id}:${identity.id}` : "";
}
const balance = ref<number | null>(null);
const mobileOpen = ref(false);
function readRoute() {
  cur.value = navs.some((n) => n.key === location.hash.slice(1))
    ? location.hash.slice(1)
    : "home";
  mobileOpen.value = false;
}
function go(key: string) {
  if (!navs.some((n) => n.key === key)) return;
  location.hash = key;
  cur.value = key;
  mobileOpen.value = false;
  window.scrollTo({ top: 0 });
}
provide("go", go);
async function refreshMe() {
  try {
    balance.value = (
      await api<{ balance_cent: number }>("/portal/me")
    ).balance_cent;
  } catch {
    balance.value = null;
  }
}
async function authenticated(identity: PortalIdentity) {
  if (identity.role === "admin") {
    location.assign("/");
    return;
  }
  auth.clearKey();
  auth.clearSid();
  setIdentity(identity);
  logged.value = true;
  await refreshMe();
}
function expired() {
  logged.value = false;
  workbenchAccount.value = "";
  balance.value = null;
  auth.clearSid();
  closeChat();
}
async function logout() {
  try {
    await api("/auth/logout", { body: {} });
    auth.clearKey();
    expired();
  } catch (e: any) {
    toastErr(e.message || "退出失败，请重试");
  }
}
onMounted(async () => {
  readRoute();
  window.addEventListener("hashchange", readRoute);
  window.addEventListener("portal-session-expired", expired);
  auth.clearKey();
  try {
    const me = await api<PortalIdentity>("/auth/me");
    setIdentity(me);
    logged.value = true;
    await refreshMe();
  } catch {
    logged.value = false;
  } finally {
    loading.value = false;
  }
});
onUnmounted(() => {
  window.removeEventListener("hashchange", readRoute);
  window.removeEventListener("portal-session-expired", expired);
});
</script>

<template>
  <ToastHost />
  <div v-if="loading" class="gate" role="status">正在连接用户空间…</div>
  <AuthGate
    v-else-if="!logged"
    default-role="user"
    @authenticated="authenticated"
  />
  <div v-else class="portal-space" @keydown.esc="mobileOpen = false">
    <header class="workspace-header">
      <a class="workspace-brand" href="#home"
        ><strong>AI Gateway<small>控制台</small></strong></a
      >
      <div class="header-actions">
        <button class="support-button" @click="openChat">
          <IconMessageCircle :size="18" /> 操作客服
        </button>
        <button class="balance-button" @click="go('wallet')">
          <IconWallet :size="16" />{{ money(balance) }}
        </button>
        <span class="account-name">{{ username }}</span>
        <button class="plain-icon" aria-label="退出登录" @click="logout">
          <IconLogout :size="18" />
        </button>
        <button
          class="plain-icon menu-toggle"
          aria-label="展开导航"
          :aria-expanded="mobileOpen"
          @click="mobileOpen = !mobileOpen"
        >
          <IconMenu2 :size="20" />
        </button>
      </div>
    </header>
    <nav
      class="workspace-nav"
      :class="{ expanded: mobileOpen }"
      aria-label="用户导航"
    >
      <a
        v-for="n in navs"
        :key="n.key"
        :href="'#' + n.key"
        :class="{ selected: cur === n.key }"
        :aria-current="cur === n.key ? 'page' : undefined"
        @click="mobileOpen = false"
        >{{ n.label }}</a
      >
    </nav>
    <main
      class="workspace-content"
      :class="{ 'workbench-content': cur === 'console' }"
    >
      <KeepAlive :key="workbenchAccount || username" include="ConsoleView">
        <component :is="current.view" :key="cur" @me="refreshMe" />
      </KeepAlive>
    </main>
    <ChatDock mode="portal" subtitle="账户服务与操作指引" />
  </div>
</template>

<style scoped>
/* Hallmark · v2.0.0 · theme: custom · scope: app · monochrome controls */
/* Critique: neutral surfaces and functional labels preserve the existing data and actions. */
.gate {
  min-height: 100dvh;
  display: grid;
  place-items: center;
  background: var(--bg);
}
.portal-space {
  min-height: 100dvh;
  background: var(--shell);
}
.workspace-header {
  min-height: 56px;
  border-bottom: 1px solid var(--border);
  padding: 8px max(24px, calc((100% - 1280px) / 2));
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  background: var(--surface);
}
.workspace-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  color: var(--ink-900);
  flex: none;
}
.brand-symbol {
  width: 40px;
  height: 40px;
  background: var(--accent-600);
  color: var(--surface);
  display: grid;
  place-items: center;
  border-radius: 12px;
}
.workspace-brand strong {
  font-size: 15px;
  letter-spacing: -0.4px;
  line-height: 1.35;
}
.workspace-brand small {
  display: inline;
  margin-left: 12px;
  font-size: 12px;
  color: var(--ink-500);
  font-weight: 400;
  letter-spacing: 0;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 16px;
}
.support-button,
.balance-button {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border: 1px solid var(--hairline);
  background: var(--surface);
  border-radius: 7px;
  font-size: 13px;
  color: var(--accent-600);
  white-space: nowrap;
  flex-shrink: 0;
}
.balance-button {
  background: var(--shell);
  border-color: transparent;
  color: var(--ink-900);
}
.account-name {
  font-size: 12px;
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.plain-icon {
  display: grid;
  place-items: center;
  background: transparent;
  border: 0;
  color: var(--ink-500);
  width: 32px;
  height: 32px;
}
.menu-toggle {
  display: none;
}
.workspace-nav {
  display: flex;
  padding: 0 max(24px, calc((100% - 1280px) / 2));
  gap: 24px;
  background: var(--surface);
  border-bottom: 1px solid var(--hairline);
}
.workspace-nav a {
  padding: 12px 0;
  color: var(--ink-500);
  border-bottom: 2px solid transparent;
  white-space: nowrap;
  font-size: 13px;
}
.workspace-nav a.selected {
  color: var(--accent-600);
  border-bottom-color: var(--accent-600);
  font-weight: 530;
}
.workspace-content {
  max-width: 1328px;
  margin: auto;
  padding: 32px 24px 64px;
}
.workbench-content {
  max-width: 1440px;
}
@media (max-width: 900px) {
  .workspace-header {
    padding: 8px 16px;
  }
  .workspace-nav {
    padding: 0 16px;
    gap: 24px;
    overflow: auto;
  }
  .account-name {
    display: none;
  }
  .header-actions {
    gap: 8px;
  }
  .workspace-content {
    padding: 24px 16px 60px;
  }
}
@media (max-width: 600px) {
  .workspace-brand strong {
    font-size: 15px;
  }
  .workspace-brand small {
    display: none;
  }
  .balance-button {
    display: none;
  }
  .menu-toggle {
    display: grid;
  }
  .workspace-nav {
    display: none;
  }
  .workspace-nav.expanded {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 0 16px;
  }
  .workspace-nav a {
    font-size: 12px;
    text-align: center;
  }
  .support-button {
    font-size: 12px;
    padding: 8px;
    white-space: nowrap;
  }
}

:is(button, a, input, select, textarea):focus-visible {
  outline: 2px solid var(--ink-900);
  outline-offset: 3px;
}
button:active:not(:disabled) {
  transform: translateY(1px);
}
button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
