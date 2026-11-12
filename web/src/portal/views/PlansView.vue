<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { IconCheck, IconClock, IconWallet } from '@tabler/icons-vue'
import { meApi, plansApi, modelsApi, cancelSubApi } from '../api'
import type { MeView, Plan, MoneyRemain } from '../types'
import { planModelScope } from '../../shared/planModels'
import { SUB_STATUS } from '../types'
import { money, fmtTimeFull } from '../../shared/utils/format'
import { toastOk, toastErr } from '../../shared/utils/toast'
import Tag from '../../shared/components/ui/Tag.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import ConfirmModal from '../../shared/components/ui/ConfirmModal.vue'
import Empty from '../../shared/components/ui/Empty.vue'
import Skeleton from '../../shared/components/ui/Skeleton.vue'

const me = ref<MeView | null>(null)
const models = ref<{ model_id: string; name: string }[]>([])
const profileError = ref('')
let requestId = 0
let disposed = false
const modelName = (id: string) => models.value.find(model => model.model_id === id)?.name || id
const plans = ref<Plan[]>([])
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const cancelOpen = ref(false)
const active = computed(() => {
  const sub = me.value?.subscription
  return sub && ['active', 'cancelled'].includes(sub.Status) && sub.CycleEnd && Date.parse(sub.CycleEnd) > Date.now() ? sub : null
})
const activePlan = computed(() => me.value?.plans?.find(p => p.ID === active.value?.PlanID))
const pendingTarget = computed(() => me.value?.plans?.find(p => p.ID === active.value?.PendingPlanID))

async function load() {
  const id = ++requestId
  loading.value = true
  error.value = ''
  profileError.value = ''
  const [profile, catalog, available] = await Promise.allSettled([meApi(), plansApi(), modelsApi()])
  if (disposed || id !== requestId) return
  if (profile.status === 'fulfilled') me.value = profile.value
  else { me.value = null; profileError.value = '当前月卡信息加载失败' }
  if (catalog.status === 'fulfilled') plans.value = (Array.isArray(catalog.value.items) ? catalog.value.items : []).filter(p => p.PlanType === 'recurring' && p.Status === 0 && ((p.WindowQuotaCent ?? 0) > 0 || (p.WeekQuotaCent ?? 0) > 0 || (p.CycleQuotaCent ?? 0) > 0))
  else error.value = catalog.reason?.message || '套餐加载失败'
  models.value = available.status === 'fulfilled' && Array.isArray(available.value.items) ? available.value.items : []
  loading.value = false
}
onBeforeUnmount(() => { disposed = true; requestId++ })

async function cancel() {
  if (busy.value || !active.value?.AutoRenew) return
  busy.value = true
  try {
    await cancelSubApi()
    cancelOpen.value = false
    toastOk('已取消下期续费')
    await load()
  } catch (e: any) { toastErr(e.message || '退订失败') }
  finally { busy.value = false }
}

function usedPercent(allowance: MoneyRemain) {
  if (allowance.quota_cent <= 0) return 0
  return Math.round(Math.max(0, Math.min(100, allowance.consumed_cent / allowance.quota_cent * 100)) * 10) / 10
}
function legacyPercent(consumed: number, quota: number) {
  return quota > 0 ? Math.round(Math.max(0, Math.min(100, consumed / quota * 100))) : 0
}
function periodLabel(period: MoneyRemain['period']) {
  if (period === 'window') return `${activePlan.value?.RefreshHours || me.value?.refresh_hours || 5} 小时额度`
  if (period === 'week') return '每周额度'
  return activePlan.value?.ValidityDays === 30 ? '每月额度' : '本周期额度'
}
onMounted(load)
</script>

<template>
  <div class="plans">
    <header class="page-head">
      <h1>月卡套餐</h1>
      <p>按模型实际费用使用金额额度</p>
    </header>

    <div v-if="error" class="load-error" role="alert">
      <span>{{ error }}</span><BaseButton @click="load">重新加载</BaseButton>
    </div>
    <template v-else-if="loading">
      <Skeleton h="136px" />
      <div class="plan-grid"><Skeleton v-for="i in 3" :key="i" h="320px" /></div>
    </template>
    <template v-else>
      <div v-if="profileError" class="load-error" role="alert"><span>{{ profileError }}</span><BaseButton @click="load">重新加载</BaseButton></div>
      <section v-if="active" class="subscription" aria-labelledby="subscription-title">
        <div class="subscription-top">
          <div>
            <h2 id="subscription-title">当前订阅</h2>
            <div class="subscription-name">
              <strong>{{ activePlan?.Name || '月卡订阅' }}</strong>
              <Tag :tone="SUB_STATUS[active.Status]?.tone ?? 'muted'">{{ SUB_STATUS[active.Status]?.text ?? active.Status }}</Tag>
            </div>
            <p class="cycle-time">{{ fmtTimeFull(active.CycleStart) }} 至 {{ fmtTimeFull(active.CycleEnd) }}</p>
            <p class="renewal">{{ active.AutoRenew ? '下期自动续费' : '已关闭自动续费' }}<span v-if="pendingTarget">，下期套餐「{{ pendingTarget.Name }}」</span></p>
          </div>
          <BaseButton v-if="active.AutoRenew" :loading="busy" @click="cancelOpen = true">取消续费</BaseButton>
        </div>
        <div v-if="me?.money_allowance?.length" class="allowance-grid">
          <div v-for="allowance in me.money_allowance" :key="allowance.period" class="allowance">
            <div class="allowance-heading"><h3>{{ periodLabel(allowance.period) }}</h3><span>已使用 {{ usedPercent(allowance) }}%</span></div>
            <div class="budget-track" role="progressbar" :aria-label="periodLabel(allowance.period) + '使用比例'" :aria-valuenow="usedPercent(allowance)" :aria-valuemin="0" :aria-valuemax="100">
              <span :style="{ transform: `scaleX(${usedPercent(allowance) / 100})` }" />
            </div>
            <div class="allowance-meta"><p>剩余 {{ money(allowance.remaining_cent) }} / {{ money(allowance.quota_cent) }}</p><p v-if="allowance.resets_at">{{ allowance.period === 'cycle' ? '本期截止' : '最早恢复' }} {{ fmtTimeFull(allowance.resets_at) }}</p></div>
          </div>
        </div>
        <div v-else-if="me?.allowance?.length" class="allowance-grid">
          <div v-for="allowance in me.allowance" :key="allowance.tier" class="allowance">
            <div class="allowance-heading"><h3>{{ allowance.tier }}额度</h3><span>已使用 {{ legacyPercent(allowance.consumed, allowance.quota) }}%</span></div>
            <div class="budget-track" role="progressbar" :aria-label="allowance.tier + '使用比例'" :aria-valuenow="legacyPercent(allowance.consumed, allowance.quota)" aria-valuemin="0" aria-valuemax="100">
              <span :style="{ transform: `scaleX(${legacyPercent(allowance.consumed, allowance.quota) / 100})` }" />
            </div>
            <div class="allowance-meta"><p>剩余 {{ 100 - legacyPercent(allowance.consumed, allowance.quota) }}%</p><p>每 {{ me.refresh_hours }} 小时滚动恢复</p></div>
          </div>
        </div>
      </section>

      <section v-else-if="!profileError" class="no-subscription"><div><h2>当前没有生效月卡</h2><p>模型调用按实际用量从账户余额扣费。</p></div><IconWallet :size="24" :stroke-width="1.4" /></section>
      <section aria-labelledby="catalog-title" class="catalog">
        <div class="section-head"><h2 id="catalog-title">可选套餐</h2><span>{{ plans.length }} 款套餐</span></div>
        <p class="availability">月卡开通暂未开放，已有月卡可继续使用。</p>
        <Empty v-if="!plans.length" text="暂无已上架月卡套餐" />
        <div v-else class="plan-grid">
          <article v-for="plan in plans" :key="plan.ID" class="plan-card" :class="{ current: active?.PlanID === plan.ID }">
            <div class="plan-heading">
              <h3>{{ plan.Name }}</h3>
              <span v-if="active?.PlanID === plan.ID" class="current-label"><IconCheck :size="13" />当前套餐</span>
              <span v-else class="duration">{{ plan.ValidityDays }} 天</span>
            </div>
            <div class="price"><strong>{{ money(plan.PriceMoney * 100) }}</strong><span>/ {{ plan.ValidityDays }} 天</span></div>
            <div class="cycle-budget" v-if="(plan.CycleQuotaCent ?? 0) > 0"><span>{{ plan.ValidityDays }} 天最多可用</span><strong>{{ money(plan.CycleQuotaCent) }}</strong></div>
            <div class="model-scope"><span>适用模型</span><div v-if="planModelScope(plan.AllowedModels).kind === 'models'" class="model-chips"><span v-for="id in planModelScope(plan.AllowedModels).ids" :key="id">{{ modelName(id) }}</span></div><strong v-else>{{ planModelScope(plan.AllowedModels).kind === 'all' ? '全部模型' : '适用模型配置异常' }}</strong></div>
            <dl class="budget-details">
              <div v-if="(plan.WindowQuotaCent ?? 0) > 0">
                <dt><IconClock :size="16" />{{ plan.RefreshHours }} 小时滚动额度</dt>
                <dd>{{ money(plan.WindowQuotaCent) }}</dd>
              </div>
              <div v-if="(plan.WeekQuotaCent ?? 0) > 0">
                <dt><IconClock :size="16" />每周额度</dt>
                <dd>{{ money(plan.WeekQuotaCent) }}</dd>
              </div>
            </dl>
            <p class="plan-terms">按适用模型的实际费用扣减额度，{{ (plan.WindowQuotaCent ?? 0) > 0 ? '窗口内消费逐笔到期后恢复。' : '' }}月卡超额不自动转扣余额{{ planModelScope(plan.AllowedModels).kind === 'models' ? '，其他模型使用账户余额' : '' }}。</p>
            <footer><BaseButton block disabled>{{ active?.PlanID === plan.ID ? '当前套餐' : '暂未开放开通' }}</BaseButton></footer>
          </article>
        </div>
      </section>
    </template>
    <ConfirmModal :open="cancelOpen" title="取消月卡续费" :text="`确认取消「${activePlan?.Name || '月卡订阅'}」的下期续费？本期权益保留至 ${fmtTimeFull(active?.CycleEnd)}。`" :loading="busy" ok-text="取消续费" danger @close="!busy && (cancelOpen = false)" @confirm="cancel" />
  </div>
</template>

<style scoped>
/* Hallmark · v2.0.0 · theme: opencode-console · scope: app · signature: flat usage sections */
/* waive 26b: BaseButton.vue owns shared active and disabled control states. */
/* Critique: thin dividers and horizontal usage bars keep the console view focused on actual budget use. */
.plans { display: grid; gap: 28px; min-width: 0; }
.page-head h1 { text-wrap: balance; font-size: 21px; font-weight: 530; letter-spacing: -.3px; }
.page-head p { margin-top: 8px; font-size: var(--fs-base); color: var(--ink-500); }
.load-error, .subscription-top, .section-head, .plan-heading { display: flex; justify-content: space-between; align-items: center; gap: 16px; }
.load-error { color: var(--danger); }
.subscription { border-top: 1px solid var(--hairline); padding-top: 24px; }
.subscription-top { align-items: flex-start; }
h2 { text-wrap: balance; font-size: var(--fs-lg); font-weight: 600; }
.subscription-top h2 { text-wrap: balance; font-size: var(--fs-sm); color: var(--ink-500); font-weight: 500; }
.subscription-name { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-top: 12px; }
.subscription-name strong { font-size: var(--fs-xl); font-weight: 600; overflow-wrap: anywhere; }
.cycle-time, .renewal { font-size: var(--fs-xs); color: var(--ink-500); margin-top: 8px; }
.allowance-grid { display: grid; gap: 28px; padding-top: 28px; margin-top: 28px; border-top: 1px solid var(--hairline); }
.allowance-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.allowance-heading h3 { font-size: var(--fs-base); font-weight: 500; }
.allowance-heading > span { color: var(--ink-700); font-size: var(--fs-sm); font-variant-numeric: tabular-nums; }
.budget-track { height: 6px; background: var(--well); overflow: hidden; margin-top: 12px; }
.budget-track span { display: block; height: 100%; background: var(--primary); transform-origin: left; }
.allowance-meta { display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap; margin-top: 8px; }
.allowance-meta p { font-size: var(--fs-xs); color: var(--ink-500); font-variant-numeric: tabular-nums; }
.section-head { margin-bottom: 24px; }
.section-head > span { color: var(--ink-500); font-size: var(--fs-sm); }
.plan-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; }
.plan-card { display: flex; flex-direction: column; min-width: 0; padding: 24px; border: 1px solid var(--hairline); border-radius: 8px; background:var(--surface); }
.plan-card.current { border-color: var(--ink-900); }
.plan-heading { align-items: flex-start; }
h3 { font-size: var(--fs-lg); font-weight: 600; overflow-wrap: anywhere; min-width: 0; }
.current-label { display: inline-flex; gap: 4px; align-items: center; flex-shrink: 0; font-size: var(--fs-2xs); color: var(--ink-700); }
.price { display: flex; align-items: baseline; flex-wrap: wrap; gap: 8px; margin: 24px 0; }
.price strong { font-size: var(--fs-2xl); font-weight: 500; letter-spacing: -.03em; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.price > span { font-size: var(--fs-sm); color: var(--ink-500); }
.budget-details { display: grid; gap: 16px; }
.budget-details > div { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
dt { display: flex; align-items: center; gap: 8px; color: var(--ink-500); font-size: var(--fs-sm); }
dt svg { flex: none; }
dd { font-size: var(--fs-base); font-weight: 500; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.plan-terms { font-size: var(--fs-xs); line-height: 1.8; color: var(--ink-500); margin: 20px 0 24px; }
.plan-card footer { margin-top: auto; }
:deep(button) { border-radius: 4px; }
:deep(button:focus-visible) { outline: 2px solid var(--ring); outline-offset: 3px; }
@media (max-width: 1100px) { .plan-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 640px) { .plan-grid { grid-template-columns: minmax(0, 1fr); } .subscription-top { flex-direction: column; } .subscription-top > button { width: 100%; } .page-head h1 { font-size: var(--fs-2xl); } }
</style>

<style scoped>
.no-subscription { display:flex; justify-content:space-between; align-items:center; gap:20px; padding:20px 24px; border:1px solid var(--hairline); border-radius:8px; background:var(--surface-2); }
.no-subscription h2 { font-size:14px; font-weight:530; }
.no-subscription p { font-size:12px; color:var(--ink-500); margin-top:6px; }
.no-subscription svg { color:var(--ink-500); flex:none; }
.availability { font-size:12px; color:var(--ink-500); margin:-10px 0 20px; }
.duration { color:var(--ink-500); font-size:11px; white-space:nowrap; }
.cycle-budget { display:grid; gap:6px; padding:16px; border-radius:6px; background:var(--surface-2); margin-bottom:20px; }
.cycle-budget span { font-size:12px; color:var(--ink-500); }
.cycle-budget strong { color:var(--ink-900); font-size:24px; font-weight:550; font-variant-numeric:tabular-nums; }
.model-scope { display:grid; gap:10px; margin-bottom:20px; font-size:12px; }
.model-scope > span { color:var(--ink-500); }
.model-scope strong { color:var(--ink-900); font-weight:500; }
.model-chips { display:flex; flex-wrap:wrap; gap:6px; }
.model-chips span { background:var(--surface-2); border:1px solid var(--hairline); border-radius:5px; padding:3px 8px; color:var(--ink-900); overflow-wrap:anywhere; }
.budget-details { border-top:1px solid var(--hairline); padding-top:18px; }
.plan-terms { line-height:1.8; }
.plan-card footer { border-top:1px solid var(--hairline); padding-top:18px; }
@media(max-width:640px) { .plan-card { padding:20px; } .no-subscription { padding:18px; } }
</style>
