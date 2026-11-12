<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, inject, computed } from "vue";
import { meApi } from "../api";
import type { MeView } from "../types";
import { money, fmt, fmtDay, fmtTimeFull } from "../../shared/utils/format";
import { openChat } from "../../shared/chatLayer";
import AreaChart from "../../shared/components/ui/AreaChart.vue";
const go = inject<(key: string) => void>("go");
const emit = defineEmits<{ (e: "me"): void }>();
const me = ref<MeView | null>(null);
const loading = ref(true);
const error = ref("");
// Go's nil slices serialize to null, including accounts with no model calls.
const consumption = computed(() => me.value?.consumption ?? []);
const models = computed(() => me.value?.consumption_by_model ?? []);
const moneyAllowance = computed(() => me.value?.money_allowance ?? []);
const tierAllowances = computed(() => me.value?.allowance ?? []);
const spent = computed(() => consumption.value.reduce((sum, day) => sum + (day.tokens ?? 0), 0));
const calls = computed(() => models.value.reduce((sum, model) => sum + (model.calls ?? 0), 0));
const available = computed(() => !loading.value && !!me.value && !error.value);
const activeSubscription = computed(() => {
  const sub = me.value?.subscription;
  if (!sub || !['active', 'cancelled'].includes(sub.Status)) return null;
  if (!sub.CycleEnd || !(Date.parse(sub.CycleEnd) > Date.now())) return null;
  return sub;
});
const currentPlan = computed(() => me.value?.plans?.find(plan => plan.ID === activeSubscription.value?.PlanID));
const series = computed(() => [{
  name: "使用金额",
  color: "var(--ok)",
  points: consumption.value.map(day => ({ label: fmtDay(day.day), value: day.tokens ?? 0 })),
}]);
let requestId = 0;
function percent(consumed: number, limit: number) {
  return limit > 0
    ? Math.max(0, Math.min(100, Math.round((consumed / limit) * 100)))
    : 0;
}
function periodName(period: string) {
  return period === "window"
    ? `每 ${me.value?.refresh_hours || 5} 小时`
    : period === "week"
      ? "每周"
      : me.value?.plans?.find((plan) => plan.ID === me.value?.subscription?.PlanID)?.ValidityDays === 30
        ? "30 天周期"
        : "本周期";
}
async function load() {
  if (loading.value && requestId) return;
  const id = ++requestId;
  loading.value = true;
  error.value = "";
  try {
    const data = await meApi();
    if (id !== requestId) return;
    if (!data || !Number.isFinite(data.balance_cent)) throw new Error("账户数据不完整，请重新加载");
    me.value = data;
    emit("me");
  } catch (e: unknown) {
    if (id !== requestId) return;
    me.value = null;
    error.value = e instanceof Error ? e.message : "使用情况读取失败";
  } finally {
    if (id === requestId) loading.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => { requestId++; });
</script>
<template>
  <div class="activity-page">
    <header class="activity-heading">
      <h1>使用情况</h1>
      <div>
        <button :disabled="loading" @click="load">{{ loading ? "加载中…" : "刷新" }}</button>
        <button @click="openChat">操作客服</button
        ><button @click="go?.('console')">新对话</button>
      </div>
    </header>
    <p v-if="error" class="activity-error" role="alert">
      {{ error }} <button @click="load">重新加载</button>
    </p>
    <div class="activity-layout" :aria-busy="loading">
      <section class="activity-main" aria-label="账户使用情况">
        <div class="activity-section-heading">
          <div>
            <h2>账户用量</h2>
            <p>近 7 天</p>
          </div>
          <strong>{{ !available ? "—" : money(spent) }}</strong>
        </div>
        <div class="activity-chart">
          <p v-if="loading" role="status">加载中…</p>
          <AreaChart
            v-else-if="available && consumption.length"
            :series="series"
            :height="260"
            :format="money"
          />
          <div v-else-if="available" class="chart-empty"><span>暂无使用记录<small>开始对话或通过 API 调用后，在这里查看消耗。</small><button @click="go?.('console')">开始对话</button></span></div>
          <div v-else class="chart-empty">使用情况暂不可用，请重新加载</div>
        </div>
        <div class="activity-facts">
          <div>
            <span>调用次数</span
            ><strong>{{ !available ? "—" : calls.toLocaleString() }}</strong>
          </div>
          <div>
            <span>使用模型</span
            ><strong>{{
              !available ? "—" : models.length
            }}</strong>
          </div>
          <div>
            <span>日均使用</span
            ><strong>{{ !available ? "—" : money(spent / 7) }}</strong>
          </div>
        </div>
        <section class="model-activity">
          <header>
            <h2>模型使用</h2>
            <button @click="go?.('consumption')">查看明细</button>
          </header>
          <p v-if="available && !models.length" class="empty">
            暂无模型调用
          </p>
          <div
            v-for="model in available ? models : []"
            :key="model.model"
            class="activity-model-row"
          >
            <span
              >{{ model.model }}<small>{{ model.vendor }}</small></span
            ><span>{{ fmt(model.calls) }} 次</span
            ><strong>{{ money(model.cost_cent) }}</strong>
          </div>
        </section>
      </section>
      <aside class="activity-sidebar">
        <section class="activity-balance">
          <header>
            <h2>账户余额</h2>
            <button @click="go?.('wallet')">兑换</button>
          </header>
          <strong>{{ !available ? "—" : money(me?.balance_cent) }}</strong>
          <dl>
            <div>
              <dt>计费方式</dt>
              <dd>{{ !available ? "—" : activeSubscription ? "月卡权益 / 按量计费" : "按量计费" }}</dd>
            </div>
            <div>
              <dt>在线充值</dt>
              <dd>暂未开放</dd>
            </div>
          </dl>
        </section>
        <section class="activity-limits">
          <header>
            <h2>月卡使用进度</h2>
            <button @click="go?.('plans')">详情</button>
          </header>
          <p v-if="available && !activeSubscription" class="empty">暂无生效月卡</p>
          <p v-if="loading" class="empty">加载月卡权益…</p>
          <p v-else-if="!available" class="empty">月卡权益暂不可用</p>
          <p v-else-if="activeSubscription" class="subscription-note">{{ currentPlan?.Name || "当前月卡" }}<small v-if="activeSubscription.CycleEnd">有效至 {{ fmtTimeFull(activeSubscription.CycleEnd) }}{{ activeSubscription.Status === "cancelled" ? "（已退订，到期结束）" : "" }}</small></p>
          <div
            v-for="budget in available && activeSubscription ? moneyAllowance : []"
            :key="budget.period"
            class="activity-limit"
          >
            <div>
              <strong>{{ periodName(budget.period) }}</strong
              ><span
                >{{ percent(budget.consumed_cent, budget.quota_cent) }}%</span
              >
            </div>
            <div
              class="usage-track"
              role="progressbar"
              :aria-label="periodName(budget.period) + '使用比例'"
              :aria-valuenow="percent(budget.consumed_cent, budget.quota_cent)"
              aria-valuemin="0"
              aria-valuemax="100"
            >
              <i
                :style="{
                  transform: `scaleX(${percent(budget.consumed_cent, budget.quota_cent) / 100})`,
                }"
              />
            </div>
            <p>
              {{ money(budget.consumed_cent) }} / {{ money(budget.quota_cent)
              }}<small v-if="budget.resets_at"
                >{{ budget.period === "cycle" ? "本期截止" : "最早恢复" }} {{ fmtTimeFull(budget.resets_at) }}</small
              >
            </p>
          </div>
          <p v-if="available && activeSubscription && !moneyAllowance.length && !tierAllowances.length" class="empty">暂无可用额度明细</p>
          <template v-if="available && activeSubscription && !moneyAllowance.length">
            <div v-for="allowance in tierAllowances" :key="allowance.tier" class="activity-limit">
              <div><strong>{{ allowance.tier }}额度</strong><span>{{ percent(allowance.consumed, allowance.quota) }}%</span></div>
              <div class="usage-track" role="progressbar" :aria-label="allowance.tier + '使用比例'" :aria-valuenow="percent(allowance.consumed, allowance.quota)" aria-valuemin="0" aria-valuemax="100">
                <i :style="{ transform: `scaleX(${percent(allowance.consumed, allowance.quota) / 100})` }" />
              </div>
              <p>每 {{ me?.refresh_hours || 5 }} 小时滚动恢复</p>
            </div>
          </template>
        </section>
        <nav class="activity-shortcuts" aria-label="账户操作">
          <button @click="go?.('access')">API 接入</button
          ><button @click="go?.('orders')">订单与流水</button
          ><button @click="go?.('catalog')">模型目录</button>
        </nav>
      </aside>
    </div>
  </div>
</template>
<style scoped>
.activity-error { padding: 12px 16px; border: 1px solid var(--danger); color: var(--danger); border-radius: 6px; font-size: 13px; }
.chart-empty { text-align: center; }
.chart-empty small, .subscription-note small { display: block; margin-top: 8px; font-size: 12px; line-height: 1.6; }
.chart-empty button { margin-top: 12px; color: var(--ok-text); }
.subscription-note { margin-top: 20px; font-size: 13px; line-height: 1.6; }
.subscription-note small { color: var(--muted-foreground); }
.activity-page {
  display: grid;
  gap: 28px;
}
.activity-heading,
.activity-heading > div,
.activity-section-heading,
.activity-sidebar header,
.model-activity > header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}
h1 {
  font-size: 21px;
  font-weight: 530;
  line-height: 28px;
}
h2 {
  font-size: 15px;
  font-weight: 530;
}
button {
  border: 0;
  background: transparent;
  color: var(--foreground);
  font-size: 12px;
  min-height: 32px;
  padding: 0 4px;
}
button:hover {
  text-decoration: underline;
  text-underline-offset: 3px;
}
.activity-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 280px;
  gap: 48px;
}
.activity-main {
  min-width: 0;
}
.activity-section-heading {
  min-height: 50px;
}
.activity-section-heading p {
  color: var(--muted-foreground);
  margin-top: 4px;
  font-size: 12px;
}
.activity-section-heading > strong {
  font-size: 21px;
  color: var(--foreground);
  font-weight: 530;
}
.activity-chart {
  margin-top: 16px;
  min-height: 260px;
}
.chart-empty {
  height: 260px;
  display: grid;
  place-items: center;
  border-bottom: 1px dashed var(--border);
  color: var(--muted-foreground);
}
.activity-facts {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 24px;
  border-bottom: 1px solid var(--border);
  padding: 20px 0 28px;
}
.activity-facts > div {
  display: grid;
  gap: 8px;
}
.activity-facts span {
  color: var(--muted-foreground);
  font-size: 12px;
}
.activity-facts strong {
  font-size: 17px;
  font-weight: 530;
  color: var(--foreground);
}
.activity-sidebar {
  min-width: 0;
  border-left: 1px solid var(--border);
  padding-left: 28px;
  display: flex;
  flex-direction: column;
  gap: 28px;
}
.activity-balance > strong {
  font-size: 21px;
  font-weight: 530;
  color: var(--foreground);
  display: block;
  margin: 20px 0;
}
dl {
  display: grid;
  gap: 12px;
  margin: 0;
  font-size: 12px;
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
.activity-limits {
  border-top: 1px solid var(--border);
  padding-top: 24px;
}
.activity-limit {
  margin-top: 24px;
}
.activity-limit > div:first-child {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 8px;
}
.activity-limit strong {
  font-size: 13px;
  font-weight: 530;
  color: var(--foreground);
}
.activity-limit span,
.activity-limit p {
  font-size: 12px;
  color: var(--muted-foreground);
}
.activity-limit p {
  margin-top: 8px;
}
.activity-limit small {
  display: block;
  font-size: 11px;
  margin-top: 4px;
}
.usage-track {
  height: 6px;
  background: var(--secondary);
  border-radius: 3px;
  overflow: hidden;
}
.usage-track i {
  display: block;
  background: var(--ok);
  height: 100%;
  width: 100%;
  transform-origin: left;
}
.activity-shortcuts {
  display: grid;
  justify-items: start;
  gap: 8px;
  border-top: 1px solid var(--border);
  padding-top: 16px;
}
.model-activity {
  margin-top: 28px;
}
.activity-model-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 80px 80px;
  gap: 16px;
  align-items: center;
  border-bottom: 1px solid var(--border);
  padding: 16px 0;
  font-size: 13px;
}
.activity-model-row > span:first-child {
  overflow-wrap: anywhere;
  color: var(--foreground);
}
.activity-model-row small {
  display: block;
  font-size: 11px;
  color: var(--muted-foreground);
  margin-top: 4px;
}
.activity-model-row > strong {
  text-align: right;
  font-weight: 530;
  color: var(--foreground);
}
.empty {
  padding: 24px 0;
  color: var(--muted-foreground);
  font-size: 13px;
}
:is(button, a):focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 3px;
}
@media (max-width: 1000px) {
  .activity-layout {
    grid-template-columns: minmax(0, 1fr);
    gap: 32px;
  }
  .activity-sidebar {
    border-left: 0;
    border-top: 1px solid var(--border);
    padding: 24px 0 0;
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 28px;
  }
  .activity-limits {
    border-top: 0;
    padding-top: 0;
  }
  .activity-shortcuts {
    grid-column: 1/-1;
    display: flex;
    gap: 24px;
  }
}
@media (max-width: 600px) {
  .activity-sidebar {
    grid-template-columns: minmax(0, 1fr);
  }
  .activity-limits {
    border-top: 1px solid var(--border);
    padding-top: 24px;
  }
  .activity-facts {
    gap: 16px;
  }
  .activity-model-row {
    grid-template-columns: minmax(0, 1fr) 56px 64px;
    gap: 8px;
  }
  .activity-heading { align-items: flex-start; flex-wrap: wrap; gap: 8px; }
  .activity-heading > div { gap: 8px; flex-wrap: wrap; }
  .activity-shortcuts { flex-wrap: wrap; gap: 16px; }
}
button:active { color: var(--ink-900); }
button:disabled { cursor: not-allowed; opacity: .5; }
</style>
