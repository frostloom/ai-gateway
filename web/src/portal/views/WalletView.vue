<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { IconWallet, IconTicket, IconCheck } from "@tabler/icons-vue";
import { meApi, redeemVoucherApi } from "../api";
import type { MeView } from "../types";
import { money } from "../../shared/utils/format";
import BaseButton from "../../shared/components/ui/BaseButton.vue";
import OrdersView from "./OrdersView.vue";
const emit = defineEmits<{ (e: "me"): void }>();
const me = ref<MeView | null>(null);
const code = ref("");
const busy = ref(false);
const loading = ref(true);
const error = ref("");
const success = ref("");
const revision = ref(0);
const valid = computed(() => /^AGV-[A-F0-9]{40}$/i.test(code.value.trim()));
async function load() {
  loading.value = true;
  try {
    me.value = await meApi();
    emit("me");
  } catch (e: any) {
    error.value = e.message || "账户读取失败";
  } finally {
    loading.value = false;
  }
}
async function redeem() {
  if (busy.value || !valid.value) return;
  busy.value = true;
  error.value = "";
  success.value = "";
  try {
    const result = await redeemVoucherApi(code.value.trim());
    success.value = result.already_redeemed
      ? `此码已兑换，入账 ${money(result.amount_cent)}`
      : `已兑换 ${money(result.amount_cent)}`;
    code.value = "";
    revision.value++;
    await load();
  } catch (e: any) {
    error.value = e.message || "兑换失败，请检查兑换码";
  } finally {
    busy.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="wallet-page">
    <header><h1>余额与兑换</h1></header>
    <section class="wallet-grid">
      <div class="balance-panel">
        <span class="section-label"><IconWallet :size="18" />账户余额</span
        ><strong>{{ loading ? "—" : money(me?.balance_cent) }}</strong>
        <div class="billing-status"><span>计费方式</span><b>按量计费</b></div>
        <div class="billing-status">
          <span>在线充值</span><b class="muted">暂未开放</b>
        </div>
      </div>
      <form class="redeem-panel" @submit.prevent="redeem">
        <div class="section-label">
          <IconTicket :size="18" />
          <h2>兑换余额</h2>
        </div>
        <label for="voucher-code">兑换码</label>
        <div class="code-input">
          <input
            id="voucher-code"
            v-model="code"
            placeholder="AGV-"
            autocomplete="off"
            spellcheck="false"
            maxlength="48"
            :disabled="busy"
            :aria-invalid="!!error"
            aria-describedby="voucher-result"
            @input="
              error = '';
              success = '';
            "
          /><BaseButton
            type="submit"
            variant="primary"
            :disabled="!valid"
            :loading="busy"
            >兑换</BaseButton
          >
        </div>
        <p id="voucher-result" aria-live="polite" :class="{ error, success }">
          <template v-if="success"
            ><IconCheck :size="15" />{{ success }}</template
          ><template v-else-if="error">{{ error }}</template
          ><template v-else>每个兑换码可使用一次。</template>
        </p>
      </form>
    </section>
    <OrdersView :key="revision" @me="load" />
  </div>
</template>
<style scoped>
/* Hallmark · theme: aixcoder-reference · scope: app · balance redemption */
/* Critique: one balance, one financial action, explicit closed payment state. */
.wallet-page {
  display: grid;
  gap: 28px;
}
.wallet-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.5fr);
  gap: 24px;
}
.balance-panel,
.redeem-panel {
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 28px;
  background: var(--card);
}
.section-label {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  font-weight: 600;
  color: var(--foreground);
}
h1 {
  font-size: 26px;
}
h2 {
  font-size: 16px;
}
.balance-panel > strong {
  font-size: 40px;
  font-weight: 600;
  color: var(--foreground);
  display: block;
  margin: 24px 0;
}
.billing-status {
  display: flex;
  justify-content: space-between;
  font-size: 13px;
  margin-top: 12px;
}
.billing-status b {
  font-weight: 500;
  color: var(--foreground);
}
.billing-status b.muted {
  color: var(--muted-foreground);
}
label {
  display: block;
  font-size: 13px;
  margin: 28px 0 8px;
}
.code-input {
  display: flex;
  gap: 12px;
}
.code-input input {
  min-width: 0;
  flex: 1;
  padding: 11px 12px;
  border: 1px solid var(--input);
  border-radius: 6px;
  background: var(--background);
  font-family: var(--font-mono);
  font-size: 13px;
}
.redeem-panel p {
  min-height: 24px;
  margin-top: 12px;
  font-size: 12px;
  color: var(--muted-foreground);
  display: flex;
  gap: 6px;
  align-items: center;
}
.redeem-panel p.error {
  color: var(--destructive);
}
.redeem-panel p.success {
  color: var(--foreground);
}
input:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}
@media (max-width: 750px) {
  .wallet-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .balance-panel,
  .redeem-panel {
    padding: 24px;
  }
  .balance-panel > strong {
    font-size: 34px;
  }
  .code-input {
    flex-direction: column;
  }
  .code-input input {
    min-height: 42px;
  }
}
</style>
