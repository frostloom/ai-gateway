<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { IconPlus, IconCopy, IconClock, IconWallet } from '@tabler/icons-vue'
import { plansApi, savePlanApi } from '../api'
import type { Plan } from '../../portal/types'
import { money } from '../../shared/utils/format'
import { toastErr, toastOk } from '../../shared/utils/toast'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import Modal from '../../shared/components/ui/Modal.vue'
import ConfirmModal from '../../shared/components/ui/ConfirmModal.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import Empty from '../../shared/components/ui/Empty.vue'
import Skeleton from '../../shared/components/ui/Skeleton.vue'

const items = ref<Plan[]>([])
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const open = ref(false)
const sourceName = ref('')
const toggleTarget = ref<Plan | null>(null)
const form = ref({ Name: '', PriceMoney: 29, ValidityDays: 30, RefreshHours: 5, Status: 0, windowEnabled: true, weekEnabled: false, cycleEnabled: true, windowYuan: 5, weekYuan: 0, cycleYuan: 100 })

function validYuan(value: number) {
  const cent = value * 100
  return Number.isFinite(value) && value > 0 && Math.round(cent) >= 1 && cent <= 100000000 && Math.abs(cent - Math.round(cent)) < 0.000001
}
const quotaOrderValid = computed(() => {
  const f = form.value
  const caps = [f.windowEnabled ? f.windowYuan : 0, f.weekEnabled ? f.weekYuan : 0, f.cycleEnabled ? f.cycleYuan : 0].filter(value => value > 0)
  return caps.every((value, index) => index === 0 || Math.round(caps[index - 1]! * 100) <= Math.round(value * 100))
})
const valid = computed(() => {
  const f = form.value
  return f.Name.trim().length > 0 && [...f.Name.trim()].length <= 64 &&
    Number.isInteger(f.PriceMoney) && f.PriceMoney >= 0 && f.PriceMoney <= 1000000 &&
    Number.isInteger(f.ValidityDays) && f.ValidityDays >= 1 && f.ValidityDays <= 366 &&
    Number.isInteger(f.RefreshHours) && f.RefreshHours >= 1 && f.RefreshHours <= 744 &&
    (f.windowEnabled || f.weekEnabled || f.cycleEnabled) &&
    (!f.windowEnabled || validYuan(f.windowYuan)) &&
    (!f.weekEnabled || validYuan(f.weekYuan)) &&
    (!f.cycleEnabled || validYuan(f.cycleYuan)) && quotaOrderValid.value
})
const monetary = (plan: Plan) => (plan.WindowQuotaCent ?? 0) > 0 || (plan.WeekQuotaCent ?? 0) > 0 || (plan.CycleQuotaCent ?? 0) > 0

async function load() {
  loading.value = true
  error.value = ''
  try { items.value = (await plansApi()).items || [] }
  catch (e: any) { error.value = e.message || '套餐加载失败'; toastErr(error.value) }
  finally { loading.value = false }
}

function create(plan?: Plan) {
  sourceName.value = plan?.Name || ''
  form.value = {
    Name: plan ? [...plan.Name].slice(0, 61).join('') + ' 副本' : '',
    PriceMoney: plan?.PriceMoney ?? 29,
    ValidityDays: plan?.ValidityDays ?? 30,
    RefreshHours: plan?.RefreshHours ?? 5,
    Status: 0,
    windowEnabled: plan && monetary(plan) ? (plan.WindowQuotaCent ?? 0) > 0 : true,
    weekEnabled: (plan?.WeekQuotaCent ?? 0) > 0,
    cycleEnabled: plan && monetary(plan) ? (plan.CycleQuotaCent ?? 0) > 0 : true,
    windowYuan: (plan?.WindowQuotaCent ?? 0) > 0 ? plan!.WindowQuotaCent! / 100 : 5,
    weekYuan: (plan?.WeekQuotaCent ?? 0) / 100,
    cycleYuan: (plan?.CycleQuotaCent ?? 0) > 0 ? plan!.CycleQuotaCent! / 100 : 100,
  }
  open.value = true
}

async function save() {
  if (busy.value || !valid.value) return
  busy.value = true
  try {
    const f = form.value
    await savePlanApi({
      ID: 0, Name: f.Name.trim(), PlanType: 'recurring', PriceMoney: f.PriceMoney,
      ValidityDays: f.ValidityDays, RefreshHours: f.RefreshHours, Status: f.Status,
      WindowQuotaCent: f.windowEnabled ? Math.round(f.windowYuan * 100) : 0,
      WeekQuotaCent: f.weekEnabled ? Math.round(f.weekYuan * 100) : 0,
      CycleQuotaCent: f.cycleEnabled ? Math.round(f.cycleYuan * 100) : 0,
      TierQuota: '{}',
    })
    open.value = false
    toastOk('已创建月卡套餐')
    await load()
  } catch (e: any) { toastErr(e.message || '创建失败') }
  finally { busy.value = false }
}

async function toggle() {
  const plan = toggleTarget.value
  if (busy.value || !plan) return
  busy.value = true
  try {
    await savePlanApi({ ...plan, Status: plan.Status === 0 ? 1 : 0 })
    toggleTarget.value = null
    toastOk('套餐状态已更新')
    await load()
  } catch (e: any) { toastErr(e.message || '更新失败') }
  finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <div class="plan-management">
    <header class="toolbar">
      <div><h2>月卡套餐</h2><p>配置周期价格与可用金额额度</p></div>
      <BaseButton variant="primary" :disabled="busy" @click="create()"><IconPlus :size="16" />新增套餐</BaseButton>
    </header>
    <div v-if="loading" class="plan-grid"><Skeleton v-for="i in 3" :key="i" h="300px" /></div>
    <div v-else-if="error" class="load-error" role="alert"><span>{{ error }}</span><BaseButton @click="load">重新加载</BaseButton></div>
    <Empty v-else-if="!items.length" text="暂无月卡套餐" />
    <div v-else class="plan-grid">
      <article v-for="plan in items" :key="plan.ID" class="plan-card">
        <header class="card-head">
          <div><h3>{{ plan.Name }}</h3><span class="plan-id">#{{ plan.ID }}</span></div>
          <Tag :tone="plan.Status === 0 ? 'ok' : 'muted'">{{ plan.Status === 0 ? '已启用' : '已停用' }}</Tag>
        </header>
        <div class="price"><strong>{{ money(plan.PriceMoney * 100) }}</strong><span>/ {{ plan.ValidityDays }} 天</span></div>
        <dl v-if="monetary(plan)" class="budgets">
          <div v-if="(plan.WindowQuotaCent ?? 0) > 0"><dt><IconClock :size="16" />每 {{ plan.RefreshHours }} 小时额度</dt><dd>{{ money(plan.WindowQuotaCent) }}</dd></div>
          <div v-if="(plan.WeekQuotaCent ?? 0) > 0"><dt><IconClock :size="16" />每周额度</dt><dd>{{ money(plan.WeekQuotaCent) }}</dd></div>
          <div v-if="(plan.CycleQuotaCent ?? 0) > 0"><dt><IconWallet :size="16" />每周期额度</dt><dd>{{ money(plan.CycleQuotaCent) }}</dd></div>
        </dl>
        <p v-else class="legacy">历史套餐</p>
        <footer class="card-actions">
          <BaseButton :disabled="busy" @click="create(plan)"><IconCopy :size="15" />复制新套餐</BaseButton>
          <BaseButton variant="ghost" :disabled="busy" @click="toggleTarget = plan">{{ plan.Status === 0 ? '停用' : '启用' }}</BaseButton>
        </footer>
      </article>
    </div>
    <Modal :open="open" :title="sourceName ? '复制为新套餐' : '新增月卡套餐'" :width="620" @close="!busy && (open = false)">
      <form @submit.prevent="save">
        <fieldset :disabled="busy">
          <p v-if="sourceName" class="source">复制自「{{ sourceName }}」</p>
          <label class="field">套餐名称<input v-model="form.Name" maxlength="64" required placeholder="填写套餐名称" /></label>
          <div class="form-grid">
            <label class="field">周期价格（元）<input v-model.number="form.PriceMoney" type="number" min="0" max="1000000" step="1" required /><small>整数元</small></label>
            <label class="field">计费周期（天）<input v-model.number="form.ValidityDays" type="number" min="1" max="366" step="1" required /></label>
          </div>
          <div class="quota-section">
            <label class="quota-switch"><input v-model="form.windowEnabled" type="checkbox" />滚动窗口额度</label>
            <div v-if="form.windowEnabled" class="form-grid quota-inputs">
              <label class="field">窗口时长（小时）<input v-model.number="form.RefreshHours" type="number" min="1" max="744" step="1" required /></label>
              <label class="field">窗口额度（元）<input v-model.number="form.windowYuan" type="number" min="0.01" max="1000000" step="0.01" required /></label>
            </div>
          </div>
          <div class="quota-section">
            <label class="quota-switch"><input v-model="form.weekEnabled" type="checkbox" />每周额度</label>
            <label v-if="form.weekEnabled" class="field quota-inputs">每周额度（元）<input v-model.number="form.weekYuan" type="number" min="0.01" max="1000000" step="0.01" required /></label>
          </div>
          <div class="quota-section">
            <label class="quota-switch"><input v-model="form.cycleEnabled" type="checkbox" />计费周期额度</label>
            <label v-if="form.cycleEnabled" class="field quota-inputs">每周期额度（元）<input v-model.number="form.cycleYuan" type="number" min="0.01" max="1000000" step="0.01" required /></label>
          </div>
          <p v-if="!form.windowEnabled && !form.weekEnabled && !form.cycleEnabled" class="invalid" role="alert">请至少设置一种金额额度。</p>
          <p v-else-if="!quotaOrderValid" class="invalid" role="alert">已配置额度须满足窗口 ≤ 每周 ≤ 周期。</p>
          <label class="field status-field">上架状态<select v-model.number="form.Status"><option :value="0">启用</option><option :value="1">停用</option></select></label>
          <p class="terms">创建后计费条款固定，调整时复制为新套餐。</p>
          <div class="form-footer"><BaseButton type="button" @click="open = false">取消</BaseButton><BaseButton type="submit" variant="primary" :disabled="!valid" :loading="busy">创建套餐</BaseButton></div>
        </fieldset>
      </form>
    </Modal>
    <ConfirmModal :open="!!toggleTarget" :title="toggleTarget?.Status === 0 ? '停用套餐' : '启用套餐'" :text="toggleTarget?.Status === 0 ? `确认停用「${toggleTarget.Name}」？停用后将停止新订阅和后续续期。` : `确认启用「${toggleTarget?.Name}」？`" :loading="busy" :danger="toggleTarget?.Status === 0" @close="!busy && (toggleTarget = null)" @confirm="toggle" />
  </div>
</template>

<style scoped>
/* Hallmark · v2.0.0 · theme: aixcoder-reference · scope: app · signature: immutable monetary plans */
/* waive 26b: BaseButton.vue owns shared active and disabled control states. */
/* Critique: neutral cards follow the model catalog reference, with money and actions on shared baselines. */
.plan-management { display: grid; gap: 24px; min-width: 0; }
.toolbar, .card-head, .load-error { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.toolbar h2 { text-wrap: balance; font-size: var(--fs-xl); font-weight: 600; }
.toolbar p { font-size: var(--fs-sm); color: var(--ink-500); margin-top: 8px; }
.toolbar :deep(.label), .card-actions :deep(.label) { display: inline-flex; align-items: center; gap: 8px; }
.load-error { color: var(--danger); font-size: var(--fs-sm); }
.plan-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; }
.plan-card { display: flex; flex-direction: column; min-width: 0; border: 1px solid var(--hairline); border-radius: var(--r-md); background: var(--surface); padding: 24px; }
.card-head { align-items: flex-start; }
.card-head > div { min-width: 0; }
h3 { font-size: var(--fs-lg); font-weight: 600; overflow-wrap: anywhere; }
.plan-id { display: block; margin-top: 8px; color: var(--ink-500); font-size: var(--fs-xs); }
.price { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; margin: 24px 0; }
.price strong { font-size: var(--fs-3xl); font-weight: 600; letter-spacing: -.03em; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.price > span { font-size: var(--fs-sm); color: var(--ink-500); }
.budgets { display: grid; gap: 16px; padding-top: 20px; border-top: 1px solid var(--hairline); }
.budgets > div { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
dt { display: flex; align-items: center; gap: 8px; color: var(--ink-500); font-size: var(--fs-sm); }
dt svg { flex: none; }
dd { font-weight: 600; font-size: var(--fs-base); font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.legacy { color: var(--ink-500); font-size: var(--fs-sm); border-top: 1px solid var(--hairline); padding-top: 20px; }
.card-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; padding-top: 28px; margin-top: auto; }
fieldset { margin: 0; padding: 0; border: none; min-width: 0; }
.field { display: grid; gap: 8px; color: var(--ink-700); font-size: var(--fs-sm); }
.field input, .field select { width: 100%; min-width: 0; border: 1px solid var(--hairline-2); border-radius: var(--r-sm); background: var(--surface); color: var(--ink-900); padding: 12px; min-height: 44px; }
.field small { font-size: var(--fs-xs); color: var(--ink-500); }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; margin-top: 20px; }
.quota-section { padding-top: 20px; margin-top: 20px; border-top: 1px solid var(--hairline); }
.quota-switch { display: flex; align-items: center; gap: 8px; font-size: var(--fs-base); font-weight: 600; min-height: 24px; cursor: pointer; }
.quota-switch input { width: 16px; height: 16px; accent-color: var(--primary); }
.quota-inputs { margin-top: 16px; }
.status-field { margin-top: 24px; }
.source { font-size: var(--fs-xs); color: var(--ink-500); margin-bottom: 16px; overflow-wrap: anywhere; }
.terms { font-size: var(--fs-xs); color: var(--ink-500); margin-top: 16px; line-height: 1.7; }
.invalid { color: var(--danger); font-size: var(--fs-xs); margin-top: 16px; }
.form-footer { display: flex; justify-content: flex-end; gap: 12px; border-top: 1px solid var(--hairline); padding-top: 20px; margin-top: 24px; }
input:focus-visible, select:focus-visible, :deep(button:focus-visible) { outline: 2px solid var(--ring); outline-offset: 3px; }
@media (max-width: 1150px) { .plan-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 680px) { .plan-grid, .form-grid { grid-template-columns: minmax(0, 1fr); } .toolbar { align-items: stretch; flex-direction: column; } .plan-card { padding: 20px; } }
</style>
