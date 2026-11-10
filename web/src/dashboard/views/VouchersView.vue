<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { IconTicket, IconPlus, IconCopy } from "@tabler/icons-vue";
import { vouchersApi, createVouchersApi } from "../api";
import type { BalanceVoucher } from "../api";
import { money, fmtTime } from "../../shared/utils/format";
import { toastErr, toastOk } from "../../shared/utils/toast";
import BaseButton from "../../shared/components/ui/BaseButton.vue";
import Modal from "../../shared/components/ui/Modal.vue";
const items = ref<BalanceVoucher[]>([]);
const issued = ref<{ code: string; item: BalanceVoucher }[]>([]);
const loading = ref(true);
const busy = ref(false);
const open = ref(false);
const error = ref("");
const amount = ref(50);
const count = ref(1);
const days = ref(30);
const valid = computed(
  () =>
    Number.isInteger(amount.value) &&
    amount.value >= 1 &&
    amount.value <= 1000000 &&
    Number.isInteger(count.value) &&
    count.value >= 1 &&
    count.value <= 100 &&
    Number.isInteger(days.value) &&
    days.value >= 1 &&
    days.value <= 366,
);
async function load() {
  loading.value = true;
  error.value = "";
  try {
    items.value = (await vouchersApi()).items;
  } catch (e: any) {
    error.value = e.message;
  } finally {
    loading.value = false;
  }
}
async function generate() {
  if (busy.value || !valid.value) return;
  busy.value = true;
  try {
    issued.value = (
      await createVouchersApi(amount.value, count.value, days.value)
    ).items;
    open.value = false;
    await load();
  } catch (e: any) {
    toastErr(e.message);
  } finally {
    busy.value = false;
  }
}
async function copy() {
  try {
    await navigator.clipboard.writeText(
      issued.value.map((x) => x.code).join("\n"),
    );
    toastOk("兑换码已复制");
  } catch {
    toastErr("复制失败，请手动选择兑换码");
  }
}
function status(v: BalanceVoucher) {
  if (v.redeemed_at) return "已兑换";
  return new Date(v.expires_at).getTime() <= Date.now() ? "已过期" : "未使用";
}
onMounted(load);
</script>
<template>
  <div class="voucher-management">
    <header class="toolbar">
      <span>{{ items.length }} 条记录</span
      ><BaseButton variant="primary" @click="open = true"
        ><IconPlus :size="16" />生成兑换码</BaseButton
      >
    </header>
    <section v-if="issued.length" class="issued-codes">
      <header>
        <div>
          <h2>新生成的兑换码</h2>
          <p>完整兑换码仅本次显示</p>
        </div>
        <BaseButton @click="copy"><IconCopy :size="16" />复制全部</BaseButton>
      </header>
      <div v-for="v in issued" :key="v.code" class="issued-row">
        <code>{{ v.code }}</code
        ><strong>{{ money(v.item.amount_cent) }}</strong>
      </div>
      <button class="dismiss" @click="issued = []">收起</button>
    </section>
    <p v-if="loading" role="status">加载中…</p>
    <p v-else-if="error" role="alert">
      {{ error }} <button @click="load">重试</button>
    </p>
    <p v-else-if="!items.length" class="empty">暂无兑换码</p>
    <div v-else class="voucher-list">
      <article v-for="v in items" :key="v.id" class="voucher-card">
        <header>
          <span class="voucher-icon"><IconTicket :size="20" /></span>
          <div>
            <strong>{{ money(v.amount_cent) }}</strong
            ><code>{{ v.prefix }}…</code>
          </div>
          <span class="state" :class="{ used: v.redeemed_at }">{{
            status(v)
          }}</span>
        </header>
        <dl>
          <div>
            <dt>有效期至</dt>
            <dd>{{ fmtTime(v.expires_at) }}</dd>
          </div>
          <div>
            <dt>生成时间</dt>
            <dd>{{ fmtTime(v.created_at) }}</dd>
          </div>
          <div v-if="v.redeemed_tenant_id">
            <dt>兑换账户</dt>
            <dd>#{{ v.redeemed_tenant_id }}</dd>
          </div>
        </dl>
      </article>
    </div>
    <Modal
      :open="open"
      title="生成余额兑换码"
      :width="480"
      @close="!busy && (open = false)"
      ><form @submit.prevent="generate">
        <fieldset :disabled="busy">
          <label
            >单码金额 / 元<input
              v-model.number="amount"
              type="number"
              min="1"
              max="1000000"
              step="1"
              required
          /></label>
          <div class="two">
            <label
              >数量<input
                v-model.number="count"
                type="number"
                min="1"
                max="100"
                step="1"
                required /></label
            ><label
              >有效期 / 天<input
                v-model.number="days"
                type="number"
                min="1"
                max="366"
                step="1"
                required
            /></label>
          </div>
          <div class="generation-total">
            <span>可兑换总额</span
            ><strong>{{ valid ? money(amount * count * 100) : "—" }}</strong>
          </div>
          <div class="form-actions">
            <BaseButton type="button" @click="open = false">取消</BaseButton
            ><BaseButton
              type="submit"
              variant="primary"
              :loading="busy"
              :disabled="!valid"
              >生成 {{ valid ? count : "" }} 个兑换码</BaseButton
            >
          </div>
        </fieldset>
      </form></Modal
    >
  </div>
</template>
<style scoped>
/* Hallmark · theme: aixcoder-reference · scope: app · administrator voucher inventory */
/* Critique: amount, status and expiry remain visible in every card at phone width. */
.voucher-management {
  display: grid;
  gap: 24px;
}
.toolbar,
.issued-codes > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.toolbar > span {
  font-size: 13px;
  color: var(--muted-foreground);
}
.voucher-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 20px;
}
.voucher-card,
.issued-codes {
  padding: 24px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--card);
}
.voucher-card header {
  display: flex;
  gap: 12px;
  align-items: center;
}
.voucher-icon {
  width: 40px;
  height: 40px;
  border: 1px solid var(--border);
  border-radius: 8px;
  display: grid;
  place-items: center;
  color: var(--foreground);
}
.voucher-card strong {
  font-size: 22px;
  font-weight: 600;
  color: var(--foreground);
}
.voucher-card code {
  display: block;
  font-size: 11px;
  color: var(--muted-foreground);
  margin-top: 4px;
}
.state {
  margin-left: auto;
  padding: 4px 8px;
  border: 1px solid var(--border);
  border-radius: 5px;
  font-size: 12px;
}
.state.used {
  background: var(--secondary);
}
dl {
  display: grid;
  gap: 12px;
  font-size: 12px;
  margin: 24px 0 0;
}
dl > div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
dt {
  color: var(--muted-foreground);
}
dd {
  margin: 0;
  color: var(--foreground);
}
.issued-codes h2 {
  font-size: 17px;
}
.issued-codes p {
  font-size: 12px;
  margin-top: 5px;
  color: var(--muted-foreground);
}
.issued-row {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  padding: 14px 0;
  border-bottom: 1px solid var(--border);
}
.issued-row code {
  overflow-wrap: anywhere;
  font-size: 12px;
}
.dismiss {
  padding: 0;
  margin-top: 16px;
  border: 0;
  background: transparent;
  color: var(--foreground);
  font-size: 13px;
}
fieldset {
  border: 0;
  padding: 0;
  min-width: 0;
}
label {
  display: grid;
  gap: 8px;
  font-size: 13px;
  margin-top: 20px;
}
input {
  border: 1px solid var(--input);
  border-radius: 6px;
  padding: 10px 12px;
  width: 100%;
  min-width: 0;
}
.two {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}
.generation-total {
  display: flex;
  justify-content: space-between;
  padding: 20px 0;
  font-size: 14px;
}
.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}
.empty {
  padding: 48px;
  text-align: center;
  border: 1px solid var(--border);
  border-radius: 10px;
  color: var(--muted-foreground);
}
:is(button, input):focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}
@media (max-width: 600px) {
  .voucher-list {
    grid-template-columns: minmax(0, 1fr);
  }
  .issued-row {
    flex-direction: column;
    gap: 8px;
  }
}
</style>
