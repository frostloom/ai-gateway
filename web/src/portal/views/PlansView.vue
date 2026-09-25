<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { IconCheck, IconClock } from '@tabler/icons-vue'
import { meApi, plansApi, subscribeApi, changePlanApi, cancelSubApi } from '../api'
import type { MeView, Plan } from '../types'
import { parseTierQuota, SUB_STATUS } from '../types'
import { money, fmtTime, zhCount } from '../../shared/utils/format'
import { toastOk, toastErr } from '../../shared/utils/toast'
import Card from '../../shared/components/ui/Card.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import ConfirmModal from '../../shared/components/ui/ConfirmModal.vue'
import Skeleton from '../../shared/components/ui/Skeleton.vue'

const me = ref<MeView | null>(null)
const plans = ref<Plan[]>([])
const loading = ref(true)
const busy = ref(false)

const active = computed(() => me.value?.subscription ?? null)
const activePlan = computed(() => active.value ? plans.value.find(p => p.ID === active.value!.PlanID) : null)
const pendingTarget = computed(() => {
  if (!active.value?.PendingPlanID) return null
  return plans.value.find(p => p.ID === active.value!.PendingPlanID)
})

const cm = ref<{ open: boolean; title: string; text: string; danger: boolean; act: () => Promise<void> }>({ open: false, title: '', text: '', danger: false, act: async () => {} })

async function load() {
  loading.value = true
  try {
    const [m, p] = await Promise.all([meApi(), plansApi()])
    me.value = m
    plans.value = p.items.filter(x => x.PlanType === 'recurring' && x.Status === 0)
  } finally { loading.value = false }
}
onMounted(load)

function askSubscribe(plan: Plan) {
  cm.value = {
    open: true, title: '订阅套餐',
    text: active.value
      ? `当前订阅 ${activePlan.value?.Name}，是否改为 ${plan.Name}？（下期生效，当期不变）`
      : `订阅「${plan.Name}」：月费 ${money(plan.PriceMoney * 100)}，开通后每 ${plan.RefreshHours} 小时滚动获得各档 token 额度（不入账余额）。`,
    danger: false,
    act: async () => {
      await (active.value ? changePlanApi(plan.ID) : subscribeApi(plan.ID))
    },
  }
}

function askCancel() {
  cm.value = {
    open: true, title: '退订',
    text: `确认退订「${activePlan.value?.Name}」？当期不退、只取消下期续费（周期至 ${active.value?.CycleEnd ? fmtTime(active.value.CycleEnd) : '本期结束'}）。`,
    danger: true,
    act: async () => { await cancelSubApi() },
  }
}

async function doAct() {
  busy.value = true
  try {
    await cm.value.act()
    toastOk('操作成功')
    cm.value.open = false
    await load()
  } catch (e: any) {
    toastErr(e?.message || '操作失败')
  } finally { busy.value = false }
}

function tiersOf(p: Plan): [string, number][] {
  return Object.entries(parseTierQuota(p.TierQuota))
}
</script>

<template>
  <div class="plans">
    <div class="head">
      <div>
        <h1 class="t">套餐与订阅</h1>
        <p class="s">月费换取每窗口 token 额度 · 额度内调用免费 · 未用不累积</p>
      </div>
    </div>

    <!-- 当前订阅 -->
    <Card v-if="active" title="当前订阅" class="subcard">
      <div class="subrow">
        <div class="sinfo">
          <span class="sname">{{ activePlan?.Name }} <Tag :tone="SUB_STATUS[active.Status].tone" dot>{{ SUB_STATUS[active.Status].text }}</Tag></span>
          <p class="sline num">周期 {{ active.CycleStart ? fmtTime(active.CycleStart) : '-' }} 至 {{ active.CycleEnd ? fmtTime(active.CycleEnd) : '-' }}<span v-if="pendingTarget"> · 下期改为「{{ pendingTarget.Name }}」</span></p>
          <p v-if="active.AutoRenew" class="sline2">自动续费开 · 本期额度每 {{ me?.refresh_hours || 5 }} 小时刷新</p>
        </div>
        <div class="sacts">
          <BaseButton variant="secondary" :loading="busy" @click="askCancel">退订</BaseButton>
        </div>
      </div>
    </Card>

    <!-- 额度一览 -->
    <Card v-if="active && me?.allowance?.length" title="本期额度" sub="滚动窗口，消费账单滑出窗口即恢复">
      <div class="allow">
        <div v-for="t in me.allowance" :key="t.tier" class="allowi">
          <span class="at">{{ t.tier }}</span>
          <span class="an num">{{ zhCount(t.remaining) }} <i class="af">/ {{ zhCount(t.quota) }} token</i></span>
          <div class="abar"><i :style="{ width: Math.max(0, Math.min(100, (t.remaining / (t.quota || 1)) * 100)) + '%' }" /></div>
        </div>
      </div>
    </Card>

    <!-- 套餐对比 -->
    <section class="sec">
      <h2 class="sechead">选择套餐</h2>
      <div v-if="loading" class="pgrid">
        <div v-for="i in 3" :key="i"><Skeleton h="260px" /></div>
      </div>
      <div v-else class="pgrid">
        <div v-for="p in plans" :key="p.ID" class="plan" :class="{ cur: active?.PlanID === p.ID }">
          <div class="ptop">
            <span class="pname">{{ p.Name }}</span>
            <span v-if="active?.PlanID === p.ID" class="pflag"><IconCheck :size="13" /> 当前</span>
          </div>
          <div class="pprice"><b class="num">{{ p.PriceMoney }}</b><span class="pcur"> 元 / 月</span></div>
          <ul class="pfeat">
            <li><IconClock :size="14" /> 每 {{ p.RefreshHours }} 小时额度刷新</li>
            <li v-for="([tier, q], i) in tiersOf(p)" :key="i">{{ tier }}档 {{ zhCount(q) }} token / 窗口</li>
            <li>{{ p.ValidityDays }} 天一个计费周期</li>
          </ul>
          <BaseButton
            variant="primary" block
            :disabled="active?.PlanID === p.ID"
            :loading="busy"
            @click="askSubscribe(p)"
          >{{ active ? (active.PlanID === p.ID ? '当前套餐' : '改为此套餐') : '订阅' }}</BaseButton>
          <p v-if="active?.PlanID !== p.ID && active" class="phint">改套餐下期生效，当期不变</p>
        </div>
      </div>
    </section>

    <ConfirmModal
      :open="cm.open" :title="cm.title" :text="cm.text" :danger="cm.danger"
      :loading="busy" ok-text="确认" @close="cm.open = false" @confirm="doAct"
    />
  </div>
</template>

<style scoped>
.head { margin-bottom: var(--sp-5); }
.t {
  font-size: var(--fs-2xl);
  font-weight: 650;
  letter-spacing: -.026em;
}
.s { font-size: var(--fs-sm); color: var(--ink-400); margin-top: 5px; }

/* ---------- 当前订阅 ---------- */
.subcard { border-color: var(--accent-200); }
.subrow {
  display: flex; align-items: center; justify-content: space-between;
  gap: var(--sp-4); flex-wrap: wrap;
}
.sname {
  display: inline-flex; align-items: center; gap: var(--sp-2);
  font-size: var(--fs-lg); font-weight: 640;
  letter-spacing: -.018em;
  color: var(--ink-900);
}
.sline { font-size: var(--fs-xs); color: var(--ink-500); margin-top: 7px; }
.sline2 { font-size: var(--fs-xs); color: var(--accent-700); margin-top: 4px; font-weight: 600; }

/* 额度进度 */
.allow {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: var(--sp-5);
}
.allowi .at { font-size: var(--fs-xs); font-weight: 620; color: var(--ink-700); }
.allowi .an {
  display: block;
  font-size: var(--fs-md); font-weight: 680;
  letter-spacing: -.018em;
  color: var(--ink-900);
  margin: 4px 0 7px;
}
.af { font-style: normal; font-size: var(--fs-2xs); color: var(--ink-400); font-weight: 500; }
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

/* ---------- 套餐区 ---------- */
.sec { margin-top: var(--sp-7); }
.sechead {
  font-size: var(--fs-xl);
  font-weight: 640;
  letter-spacing: -.022em;
  margin-bottom: var(--sp-4);
}
.pgrid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: var(--sp-4);
}
.plan {
  display: flex; flex-direction: column; gap: var(--sp-3);
  padding: var(--sp-5);
  background: var(--paper);
  border: 1px dashed var(--hairline);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-1);
  transition: border-color var(--t-fast), box-shadow var(--t-fast), transform var(--t-fast);
}
.plan:hover { transform: translateY(-2px); box-shadow: var(--shadow-2); }
.plan.cur {
  border-color: var(--accent-500);
  box-shadow: inset 0 0 0 1px var(--accent-500), var(--shadow-1);
}
.ptop { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-2); }
.pname { font-size: var(--fs-md); font-weight: 640; color: var(--ink-900); }
.pflag {
  display: inline-flex; align-items: center; gap: 3px;
  font-size: var(--fs-2xs); font-weight: 650;
  color: var(--accent-700);
  background: var(--accent-50);
  padding: 3px 8px;
  border-radius: var(--r-xs);
}
.pprice { display: flex; align-items: baseline; gap: 5px; }
.pprice b {
  font-size: var(--fs-3xl);
  font-weight: 680;
  letter-spacing: -.034em;
  color: var(--ink-900);
}
.pcur { font-size: var(--fs-sm); color: var(--ink-500); }
.pfeat {
  list-style: none; margin: 0; padding: var(--sp-3) 0 0;
  border-top: 1px dashed var(--hairline);
  display: flex; flex-direction: column; gap: 8px;
  flex: 1;
}
.pfeat li {
  display: flex; align-items: center; gap: 7px;
  font-size: var(--fs-xs);
  color: var(--ink-600);
}
.phint { font-size: var(--fs-2xs); color: var(--ink-400); text-align: center; }

@media (max-width: 640px) { .pgrid { grid-template-columns: 1fr; } }
</style>