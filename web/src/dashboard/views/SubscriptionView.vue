<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { overviewApi, meApi, topupsApi, rechargeOrdersApi } from '../api'
import type { TenantStat } from '../types'
import { ORDER_STATUS } from '../../portal/types'
import { money, fmtTime, zhCount } from '../../shared/utils/format'
import { toastErr } from '../../shared/utils/toast'
import Card from '../../shared/components/ui/Card.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import Skeleton from '../../shared/components/ui/Skeleton.vue'
import Empty from '../../shared/components/ui/Empty.vue'

const tenants = ref<TenantStat[]>([])
const tid = ref(0)
const loading = ref(false)
const me = ref<Awaited<ReturnType<typeof meApi>> | null>(null)
const topups = ref<{ ID: number; Source: string; Amount: number; RefType: string; RefID: string; CreatedAt: string }[]>([])
const orders = ref<{ OrderNo: string; AmountCent: number; Status: string; CreatedAt: string }[]>([])

const allowance = computed(() => me.value?.allowance ?? [])

async function loadTenants() {
  try { tenants.value = (await overviewApi()).tenants } catch { /* ignore */ }
}
onMounted(loadTenants)

async function query() {
  if (!tid.value) { toastErr('请选择租户'); return }
  loading.value = true
  try {
    const [m, t, o] = await Promise.all([meApi(tid.value), topupsApi(tid.value), rechargeOrdersApi(tid.value)])
    me.value = m
    topups.value = t.items.map(x => ({ ID: x.ID, Source: x.Source, Amount: x.Amount, RefType: x.RefType, RefID: x.RefID, CreatedAt: x.CreatedAt }))
    orders.value = o.items.map(x => ({ OrderNo: x.OrderNo, AmountCent: x.AmountCent, Status: x.Status, CreatedAt: x.CreatedAt }))
  } catch (e: any) { toastErr(e?.message || '查询失败') }
  finally { loading.value = false }
}
</script>

<template>
  <div class="subs">
    <div class="bar">
      <select v-model.number="tid" class="sel">
        <option :value="0">- 选择租户 -</option>
        <option v-for="t in tenants" :key="t.id" :value="t.id">{{ t.name }} (#{{ t.id }})</option>
      </select>
      <BaseButton variant="primary" :loading="loading" @click="query">查询</BaseButton>
    </div>

    <template v-if="me">
      <Card title="当前订阅" class="subcard">
        <template v-if="me.subscription">
          <div class="subrow">
            <div>
              <span class="sname">订阅 #{{ me.subscription.PlanID }} · {{ me.subscription.PlanType }}</span>
              <p class="sline num">周期 {{ me.subscription.CycleStart ? fmtTime(me.subscription.CycleStart) : '-' }} 至 {{ me.subscription.CycleEnd ? fmtTime(me.subscription.CycleEnd) : '-' }} · 自动续费 {{ me.subscription.AutoRenew ? '开' : '关' }}</p>
              <p class="sline2">余额：{{ money(me.balance_cent) }}（账本 {{ money(me.ledger_balance) }}，{{ me.consistent ? '一致' : '不一致' }}）</p>
            </div>
          </div>
          <div v-if="allowance.length" class="allow">
            <div v-for="a in allowance" :key="a.tier" class="allowi">
              <div class="atop"><span class="aname">{{ a.tier }}</span><span class="anum num">{{ zhCount(a.remaining) }} / {{ zhCount(a.quota) }}</span></div>
              <div class="abar"><i :style="{ width: Math.max(0, Math.min(100, (a.remaining / (a.quota || 1)) * 100)) + '%' }" /></div>
            </div>
          </div>
        </template>
        <Empty v-else text="该租户暂无订阅" />
      </Card>

      <div class="grid2">
        <Card title="入账流水" :pad="false">
          <Empty v-if="!topups.length" text="暂无" />
          <div v-else>
            <div v-for="t in topups" :key="t.ID" class="trow num">
              <span class="tsrc">{{ t.Source }}</span>
              <span class="tamt" :class="{ neg: t.Amount < 0 }">{{ t.Amount >= 0 ? '+' : '' }}{{ money(t.Amount) }}</span>
              <span class="tref mono">{{ t.RefID }}</span>
              <span class="ttm">{{ fmtTime(t.CreatedAt) }}</span>
            </div>
          </div>
        </Card>
        <Card title="充值订单" :pad="false">
          <Empty v-if="!orders.length" text="暂无" />
          <div v-else>
            <div v-for="(o, i) in orders.slice(0, 20)" :key="i" class="trow num">
              <span class="ono mono">{{ o.OrderNo }}</span>
              <span class="tamt">{{ money(o.AmountCent) }}</span>
              <span><Tag :tone="ORDER_STATUS[o.Status]?.tone ?? 'muted'" dot>{{ ORDER_STATUS[o.Status]?.text ?? o.Status }}</Tag></span>
              <span class="ttm">{{ fmtTime(o.CreatedAt) }}</span>
            </div>
          </div>
        </Card>
      </div>
    </template>
    <div v-else-if="loading" class="sk"><div v-for="i in 3" :key="i"><Skeleton h="120px" /></div></div>
    <div v-else class="no">选择租户后查看订阅、额度与入账流水</div>
  </div>
</template>

<style scoped>
.bar { display: flex; gap: var(--sp-3); margin-bottom: var(--sp-5); flex-wrap: wrap; }
.sel {
  height: 34px; padding: 0 var(--sp-3);
  border: none;
  border-radius: var(--r-sm);
  background: var(--paper);
  color: var(--ink-700);
  font-size: var(--fs-sm);
  min-width: 200px;
  transition: border-color var(--t-fast);
}
.sel:hover { border-color: var(--line-strong); }
.sel:focus { outline: none; border-color: var(--accent-500); box-shadow: 0 0 0 3px var(--accent-50); }

.subcard { border-color: var(--accent-200); }
.subrow { padding-bottom: var(--sp-4); }
.sname {
  font-size: var(--fs-lg); font-weight: 640;
  letter-spacing: -.018em;
  color: var(--ink-900);
}
.sline { font-size: var(--fs-xs); color: var(--ink-500); margin-top: 7px; }
.sline2 { font-size: var(--fs-xs); color: var(--accent-700); margin-top: 4px; font-weight: 600; }

.allow {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: var(--sp-5);
  border-top: 1px solid var(--hairline);
  padding-top: var(--sp-4);
}
.atop {
  display: flex; justify-content: space-between; align-items: baseline;
  gap: var(--sp-2);
  font-size: var(--fs-xs);
  margin-bottom: 7px;
}
.aname { font-weight: 620; color: var(--ink-700); }
.anum { color: var(--ink-500); font-variant-numeric: tabular-nums; }
.abar {
  height: 5px; border-radius: var(--r-pill);
  background: var(--paper-4);
  overflow: hidden;
}
.abar i {
  display: block; height: 100%;
  background: linear-gradient(90deg, var(--accent-500), var(--accent-400));
  border-radius: var(--r-pill);
  transition: width var(--t-slow);
}

.grid2 {
  display: grid; grid-template-columns: 1fr 1fr;
  gap: var(--sp-4); margin-top: var(--sp-4);
}
.trow {
  display: grid; grid-template-columns: 1.15fr .75fr 1fr 96px;
  gap: var(--sp-2); align-items: center;
  padding: 9px var(--sp-5);
  border-bottom: 1px solid var(--hairline);
  font-size: var(--fs-xs);
  font-variant-numeric: tabular-nums;
}
.trow:last-child { border-bottom: none; }
.trow:hover { background: var(--paper-2); }
.tsrc { font-weight: 620; color: var(--ink-700); }
.tamt { font-weight: 660; color: var(--ok); }
.tamt.neg { color: var(--warn); }
.tref { color: var(--ink-400); font-size: var(--fs-2xs); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ono { color: var(--ink-500); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ttm { color: var(--ink-400); text-align: right; }

.no {
  display: flex; flex-direction: column; align-items: center; gap: 6px;
  text-align: center;
  color: var(--ink-400);
  padding: var(--sp-10) 0;
  font-size: var(--fs-sm);
}

@media (max-width: 900px) {
  .grid2 { grid-template-columns: 1fr; }
}
</style>