<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { IconArrowUpRight, IconArrowDownLeft, IconRotate } from '@tabler/icons-vue'
import { ordersApi, topupsApi, refundOrderApi, refundAmountApi } from '../api'
import type { RechargeOrder, Topup, RefundResult } from '../types'
import { ORDER_STATUS } from '../types'
import { money, fmtTime } from '../../shared/utils/format'
import { toastOk, toastErr } from '../../shared/utils/toast'
import Card from '../../shared/components/ui/Card.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import ConfirmModal from '../../shared/components/ui/ConfirmModal.vue'
import Modal from '../../shared/components/ui/Modal.vue'
import Field from '../../shared/components/ui/Field.vue'
import Skeleton from '../../shared/components/ui/Skeleton.vue'
import Empty from '../../shared/components/ui/Empty.vue'

const orders = ref<RechargeOrder[]>([])
const topups = ref<Topup[]>([])
const loading = ref(true)
const busy = ref(false)

const refundTarget = ref<RechargeOrder | null>(null)
const amountModal = ref(false)
const refundAmt = ref(100)

async function load() {
  loading.value = true
  try {
    const [o, t] = await Promise.all([ordersApi(), topupsApi()])
    orders.value = o.items
    topups.value = t.items
  } finally { loading.value = false }
}
onMounted(load)

function askRefund(o: RechargeOrder) { refundTarget.value = o }

async function doRefundOrder() {
  if (!refundTarget.value) return
  busy.value = true
  try {
    const r: RefundResult = await refundOrderApi(refundTarget.value.OrderNo)
    toastOk(`已退款 ${money(r.refund_cent)}，当前余额 ${money(r.balance_after)}`)
    refundTarget.value = null
    await load()
  } catch (e: any) {
    toastErr(e?.message || '退款失败')
  } finally { busy.value = false }
}

async function doRefundAmount() {
  const v = Math.floor(Number(refundAmt.value))
  if (!v || v <= 0) { toastErr('请输入有效金额'); return }
  busy.value = true
  try {
    const r: RefundResult = await refundAmountApi(v)
    toastOk(`已退款 ${money(r.refund_cent)}，当前余额 ${money(r.balance_after)}`)
    amountModal.value = false
    await load()
  } catch (e: any) {
    toastErr(e?.message || '退款失败')
  } finally { busy.value = false }
}
</script>

<template>
  <div class="orders">
    <div class="head">
      <h1 class="t">订单与流水</h1>
      <BaseButton variant="secondary" @click="amountModal = true"><IconRotate :size="15" /> 按金额退款</BaseButton>
    </div>

    <Card title="充值订单" sub="退款量 = min(订单额度, 当前余额)，绝不退成负">
      <div v-if="loading" class="rows"><div v-for="i in 5" :key="i"><Skeleton h="46px" /></div></div>
      <Empty v-else-if="!orders.length" text="暂无充值订单" />
      <template v-else>
        <div class="thead num">
          <span>订单号</span><span>金额</span><span>状态</span><span>时间</span><span class="ra">操作</span>
        </div>
        <div v-for="o in orders" :key="o.ID" class="row num">
          <span class="ono mono">{{ o.OrderNo }}</span>
          <span class="amt">{{ money(o.AmountCent) }}</span>
          <span><Tag :tone="ORDER_STATUS[o.Status].tone" dot>{{ ORDER_STATUS[o.Status].text }}</Tag></span>
          <span class="tm">{{ fmtTime(o.CreatedAt) }}</span>
          <span class="ra">
            <BaseButton v-if="o.Status === 'paid'" size="sm" variant="secondary" @click="askRefund(o)">退款</BaseButton>
            <span v-else class="ndash">-</span>
          </span>
        </div>
      </template>
    </Card>

    <div class="grid2">
      <Card title="入账流水" sub="余额 = 初始额度 + 入账 － 消费 － 在途">
        <Empty v-if="!topups.length" text="暂无流水" />
        <div v-else class="tlist">
          <div v-for="t in topups" :key="t.ID" class="trow">
            <span class="tic" :class="{ out: t.Amount < 0 }">
              <IconArrowUpRight v-if="t.Amount >= 0" :size="14" />
              <IconArrowDownLeft v-else :size="14" />
            </span>
            <div class="tinfo">
              <span class="tsrc">{{ t.Source === 'recharge' ? '充值' : t.Source === 'refund' ? '退款' : '订阅' }}</span>
              <span class="tref mono">{{ t.RefID }}</span>
            </div>
            <span class="tamt num" :class="{ out: t.Amount < 0 }">{{ t.Amount >= 0 ? '+' : '' }}{{ money(t.Amount) }}</span>
            <span class="ttm num">{{ fmtTime(t.CreatedAt) }}</span>
          </div>
        </div>
      </Card>
    </div>

    <!-- 按订单退款确认 -->
    <ConfirmModal
      :open="!!refundTarget" title="退款确认"
      :text="refundTarget ? `确认退款订单 ${refundTarget.OrderNo}（${money(refundTarget.AmountCent)}）？退款量 = min(订单额度, 当前余额)。` : ''"
      danger :loading="busy" ok-text="确认退款"
      @close="refundTarget = null" @confirm="doRefundOrder"
    />

    <!-- 按金额退款 -->
    <Modal :open="amountModal" title="按金额退款" @close="amountModal = false" width="400">
      <Field label="退款金额" hint="最多退到当前余额（元，整数）" required>
        <input v-model.number="refundAmt" type="number" min="1" />
      </Field>
      <template #footer>
        <BaseButton variant="ghost" @click="amountModal = false">取消</BaseButton>
        <BaseButton variant="danger" :loading="busy" @click="doRefundAmount">确认退款</BaseButton>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 18px; }
.t { font-size: 22px; }
.rows { display: flex; flex-direction: column; gap: 8px; }
.thead, .row {
  display: grid; grid-template-columns: 1.4fr .7fr .7fr .9fr .6fr;
  gap: 10px; align-items: center; padding: 10px 6px;
  font-size: 13px;
}
.thead { color: var(--ink-400); font-size: 12px; border-bottom: 1px dashed var(--hairline); margin-bottom: 4px; }
.row { border-bottom: 1px dashed var(--line); }
.row:hover { background: var(--paper-2); border-radius: 8px; }
.ono { color: var(--ink-500); }
.mono { font-family: var(--font-mono); }
.amt { font-weight: 650; color: var(--ink-900); }
.tm { color: var(--ink-400); }
.ra { display: flex; justify-content: flex-end; }
.ndash { color: var(--ink-300); }
.grid2 { margin-top: 20px; }
.tlist { display: flex; flex-direction: column; }
.trow { display: grid; grid-template-columns: 26px 1fr auto 96px; gap: 10px; align-items: center; padding: 8px 4px; border-bottom: 1px dashed var(--line); font-size: 12.5px; }
.tic { width: 26px; height: 26px; border-radius: 8px; background: var(--ok-bg); color: var(--ok); display: grid; place-items: center; }
.tic.out { background: var(--warn-bg); color: var(--warn); }
.tinfo { display: flex; flex-direction: column; }
.tsrc { font-weight: 650; color: var(--ink-800); }
.tref { font-size: 11px; color: var(--ink-400); }
.tamt { font-weight: 700; color: var(--ok); }
.tamt.out { color: var(--warn); }
.ttm { color: var(--ink-400); text-align: right; }
@media (max-width: 720px) { .thead { display: none; } .row { grid-template-columns: 1fr auto; } .tm, .ono { grid-column: 1; } }
</style>