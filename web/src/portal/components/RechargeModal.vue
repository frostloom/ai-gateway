<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { IconWallet, IconCircleCheck, IconReceipt } from '@tabler/icons-vue'
import Modal from '../../shared/components/ui/Modal.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import { rechargeApi, payApi } from '../api'
import { money, fmtTime } from '../../shared/utils/format'
import { toastOk, toastErr } from '../../shared/utils/toast'
import type { RechargeOrder } from '../types'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'changed'): void }>()

const PRESETS = [10, 50, 100, 500]
const amount = ref(100)
const custom = ref('')
const step = ref<'pick' | 'pay' | 'done'>('pick')
const order = ref<RechargeOrder | null>(null)
const busy = ref(false)

const showCustom = ref(false)
const amountValue = computed(() => {
  if (showCustom.value) {
    const v = Math.floor(Number(custom.value))
    return Number.isFinite(v) && v > 0 ? v : 0
  }
  return amount.value
})

function pick(v: number) { showCustom.value = false; amount.value = v }
function useCustom() {
  showCustom.value = true
  const v = Math.floor(Number(custom.value))
  if (v > 0) amount.value = INF // 占位，实际用 amountValue
}
const INF = 10 ** 9

watch(() => props.open, v => {
  if (v) { step.value = 'pick'; order.value = null; custom.value = ''; showCustom.value = false }
})

async function createOrder() {
  const v = amountValue.value
  if (!v || v <= 0) { toastErr('请输入有效金额'); return }
  busy.value = true
  try {
    order.value = await rechargeApi(v)
    step.value = 'pay'
  } catch (e: any) {
    toastErr(e?.message || '下单失败')
  } finally { busy.value = false }
}

async function pay() {
  if (!order.value) return
  busy.value = true
  try {
    order.value = await payApi(order.value.OrderNo)
    step.value = 'done'
    emit('changed')
    toastOk(`已入账 ${money(order.value.AmountCent)}`)
  } catch (e: any) {
    toastErr(e?.message || '支付失败')
  } finally { busy.value = false }
}

function close() { emit('close') }
function finish() { close(); setTimeout(() => emit('changed'), 50) }
</script>

<template>
  <Modal :open="open" title="充值" @close="close" width="440">
    <template #extra><IconWallet :size="17" color="var(--accent-600)" /></template>

    <!-- 第一步：选金额 -->
    <div v-if="step === 'pick'">
      <p class="tip">选择充值金额（模拟支付，仅入账演示环境余额）</p>
      <div class="presets">
        <button v-for="v in PRESETS" :key="v" class="preset" :class="{ on: !showCustom && amount === v }" @click="pick(v)">
          <span class="pv num">¥{{ v }}</span>
        </button>
        <button class="preset" :class="{ on: showCustom }" @click="useCustom">
          <span class="pv">自定义</span>
        </button>
      </div>
      <div v-if="showCustom" class="customrow">
        <span class="cpre">¥</span>
        <input v-model="custom" class="cinput num" type="number" min="1" placeholder="输入金额（整数元）" @keyup.enter="createOrder" />
      </div>
      <div class="foot">
        <span class="total">应付 <b class="num">{{ money(amountValue * 100) }}</b></span>
        <BaseButton variant="primary" size="lg" :loading="busy" :disabled="amountValue <= 0" @click="createOrder">去支付</BaseButton>
      </div>
    </div>

    <!-- 第二步：确认支付 -->
    <div v-else-if="step === 'pay' && order">
      <div class="receipt">
        <div class="rrow"><span>订单号</span><span class="num mono">{{ order.OrderNo }}</span></div>
        <div class="rrow"><span>金额</span><span class="num strong">{{ money(order.AmountCent) }}</span></div>
        <div class="rrow"><span>创建时间</span><span class="num">{{ fmtTime(order.CreatedAt) }}</span></div>
      </div>
      <p class="sim">模拟收银台：点击下方按钮确认支付。</p>
      <div class="foot">
        <BaseButton variant="ghost" size="lg" @click="step = 'pick'">返回</BaseButton>
        <BaseButton variant="primary" size="lg" :loading="busy" @click="pay">确认支付（模拟）</BaseButton>
      </div>
    </div>

    <!-- 第三步：入账成功 -->
    <div v-else-if="step === 'done' && order" class="done">
      <div class="okic"><IconCircleCheck :size="34" /></div>
      <p class="dtitle">充值成功</p>
      <p class="dsub num"><IconReceipt :size="14" /> 订单 {{ order.OrderNo }} 已入账 <b>{{ money(order.AmountCent) }}</b></p>
      <div class="foot center">
        <BaseButton variant="primary" size="lg" @click="finish">完成</BaseButton>
      </div>
    </div>
  </Modal>
</template>

<style scoped>
.tip { font-size: var(--fs-sm); color: var(--ink-500); margin-bottom: var(--sp-4); }

/* 金额选择 */
.presets {
  display: grid; grid-template-columns: repeat(3, 1fr);
  gap: var(--sp-3); margin-bottom: var(--sp-3);
}
.preset {
  height: 62px;
  border-radius: var(--r-sm);
  border: 1.5px solid var(--line-2);
  background: var(--paper);
  display: grid; place-items: center;
  transition: all var(--t-fast);
}
.preset:hover { border-color: var(--line-strong); background: var(--paper-2); }
.preset.on {
  border-color: var(--accent-500);
  background: var(--accent-50);
  box-shadow: inset 0 0 0 1px var(--accent-500);
}
.pv {
  font-size: var(--fs-lg); font-weight: 660;
  letter-spacing: -.02em;
  color: var(--ink-900);
}
.preset.on .pv { color: var(--accent-700); }

.customrow {
  display: flex; align-items: center; gap: 5px;
  border: none;
  border-radius: var(--r-sm);
  padding: 0 var(--sp-3);
  margin-bottom: var(--sp-2);
  transition: border-color var(--t-fast);
}
.customrow:focus-within { border-color: var(--accent-500); box-shadow: 0 0 0 3px var(--accent-50); }
.cpre { font-size: var(--fs-md); font-weight: 650; color: var(--ink-400); }
.cinput {
  flex: 1; height: 40px;
  border: none; outline: none; background: transparent;
  font-size: var(--fs-md);
}

/* 订单回执 */
.receipt {
  background: var(--paper-2);
  border: 1px solid var(--hairline);
  border-radius: var(--r-sm);
  padding: var(--sp-4);
  margin-bottom: var(--sp-3);
}
.rrow {
  display: flex; justify-content: space-between; align-items: center;
  padding: 7px 0;
  font-size: var(--fs-sm); color: var(--ink-500);
}
.rrow + .rrow { border-top: 1px solid var(--hairline); }
.rrow .strong { color: var(--ink-900); font-size: var(--fs-md); font-weight: 680; }
.mono { font-family: var(--font-mono); font-size: var(--fs-2xs); }

.sim {
  font-size: var(--fs-xs); color: var(--warn);
  background: var(--warn-bg);
  border-radius: var(--r-sm);
  padding: 9px var(--sp-3);
  margin-bottom: var(--sp-2);
  line-height: 1.6;
}

.foot {
  display: flex; justify-content: flex-end; align-items: center;
  gap: var(--sp-3); margin-top: var(--sp-5);
}
.foot.center { justify-content: center; }
.total { font-size: var(--fs-sm); color: var(--ink-500); }
.total b { font-size: var(--fs-lg); color: var(--ink-900); margin-left: 2px; }

/* 成功态 */
.done { text-align: center; padding: var(--sp-3) 0; }
.okic {
  width: 64px; height: 64px;
  margin: 0 auto var(--sp-3);
  border-radius: 50%;
  background: var(--ok-bg); color: var(--ok);
  display: grid; place-items: center;
  animation: popIn var(--t-spring);
}
@keyframes popIn { from { transform: scale(.6); opacity: 0; } }
.dtitle { font-size: var(--fs-lg); font-weight: 650; color: var(--ink-900); }
.dsub {
  display: inline-flex; align-items: center; gap: 5px;
  margin-top: 7px;
  font-size: var(--fs-sm); color: var(--ink-500);
}
.dsub b { color: var(--accent-700); }
</style>