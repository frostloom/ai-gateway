<script setup lang="ts">
import { ref, onMounted, inject } from 'vue'
import { IconWallet, IconCreditCard, IconReceipt2, IconApps, IconArrowRight } from '@tabler/icons-vue'
import { meApi, modelsApi } from '../api'
import type { MeView, Model } from '../types'
import { money, zhCount, fmtDay } from '../../shared/utils/format'
import Card from '../../shared/components/ui/Card.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import Sparkline from '../../shared/components/ui/Sparkline.vue'
import { toastErr } from '../../shared/utils/toast'
import Skeleton from '../../shared/components/ui/Skeleton.vue'
import ProductCard from '../components/ProductCard.vue'
import RechargeModal from '../components/RechargeModal.vue'

const go = inject<(v: string) => void>('go') as ((v: string) => void) | undefined
const emit = defineEmits<{ (e: 'me'): void }>()

const me = ref<MeView | null>(null)
const models = ref<Model[]>([])
const loading = ref(true)
const loadError = ref('')
const rechargeOpen = ref(false)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [m, cat] = await Promise.all([meApi(), modelsApi()])
    me.value = m
    models.value = cat.items.slice(0, 4)
    emit('me')
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '加载失败，请重试'
    toastErr(loadError.value)
  } finally { loading.value = false }
}
onMounted(load)

function onBuy(_m: Model) { rechargeOpen.value = true }
function onTry(_m: Model) { if (go) go('console') }
</script>

<template>
  <div class="home">
    <!-- 欢迎区 -->
    <div class="hero">
      <div class="hleft">
        <p class="eyebrow">AI 模型商城</p>
        <h1 class="htitle">欢迎回来</h1>
        <p class="hsub">在一个工作台管理模型、用量与账户余额。</p>
        <div class="hacts">
          <BaseButton variant="primary" size="lg" @click="rechargeOpen = true">立即充值</BaseButton>
          <BaseButton variant="secondary" size="lg" @click="go?.('catalog')">浏览模型</BaseButton>
        </div>
      </div>
      <div class="hright">
        <div class="bcard">
          <p class="blabel">可用余额</p>
          <p class="bval num">{{ me ? money(me.balance_cent) : '-' }}<span class="bcur"></span></p>
          <p class="bsub">
            {{ me ? (me.consistent ? '与账本一致' : '账本口径 ' + money(me.ledger_balance)) : '加载中…' }}
          </p>
        </div>
        <div class="hmini">
          <div class="mini">
            <p class="mlabel">近 7 天消费</p>
            <p class="mval num">{{ me ? money(me.consumption.reduce((s, d) => s + d.tokens, 0)) : '-' }}</p>
          </div>
          <div class="mini">
            <p class="mlabel">计费方式</p>
            <p class="mval num">{{ me ? (me.subscription ? '订阅中' : '按量') : '-' }}</p>
          </div>
        </div>
      </div>
    </div>

    <!-- 订阅额度区 -->
    <Card v-if="me?.subscription && me.allowance?.length" title="本期订阅额度" sub="每窗口滚动刷新 · 未用不累积" class="allowcard">
      <div class="tiers">
        <div v-for="t in me.allowance" :key="t.tier" class="tier">
          <div class="thead">
            <span class="tname">{{ t.tier }}</span>
            <span class="tnum num">{{ zhCount(t.remaining) }} / {{ zhCount(t.quota) }}</span>
          </div>
          <div class="bar"><i :style="{ width: Math.max(0, Math.min(100, (t.remaining / (t.quota || 1)) * 100)) + '%' }" /></div>
        </div>
      </div>
    </Card>

    <!-- 可用模型 -->
    <section class="sec">
      <div class="sechead">
        <h2>可用模型</h2>
        <button class="more" @click="go?.('catalog')">查看全部 <IconArrowRight :size="15" /></button>
      </div>
      <div v-if="loading" class="grid">
        <div v-for="i in 4" :key="i" class="skel"><Skeleton h="180px" /></div>
      </div>
      <p v-else-if="loadError" class="load-error" role="alert">{{ loadError }} <button class="more" @click="load">重新加载</button></p>
      <p v-else-if="!models.length" class="catalog-empty">暂无可用模型</p>
      <div v-else class="grid">
        <ProductCard v-for="m in models" :key="m.id" :model="m"  @buy="onBuy" @try="onTry" />
      </div>
    </section>

    <!-- 快捷入口 -->
    <section class="sec">
      <h2 class="sechead2">快速入口</h2>
      <div class="quick">
        <button class="q" @click="rechargeOpen = true"><span class="qic"><IconWallet :size="19" /></span><span class="qt">充值</span><span class="qd">模拟支付，即时入账</span></button>
        <button class="q" @click="go?.('plans')"><span class="qic"><IconCreditCard :size="19" /></span><span class="qt">套餐订阅</span><span class="qd">月费换每窗口额度</span></button>
        <button class="q" @click="go?.('orders')"><span class="qic"><IconReceipt2 :size="19" /></span><span class="qt">我的订单</span><span class="qd">充值 / 退款流水</span></button>
        <button class="q" @click="go?.('console')"><span class="qic"><IconApps :size="19" /></span><span class="qt">接口调试</span><span class="qd">控制台调 /v1 接口</span></button>
      </div>
    </section>

    <!-- 近 7 天消费趋势 -->
    <Card v-if="me && me.consumption.length" title="近 7 天消费趋势" class="trend">
      <div class="trendrow">
        <Sparkline :points="me.consumption.map(d => d.tokens)" :width="560" :height="90" />
        <div class="days">
          <div v-for="d in me.consumption" :key="d.day" class="day">
            <span class="dl">{{ fmtDay(d.day) }}</span>
            <span class="dv num">{{ money(d.tokens) }}</span>
          </div>
        </div>
      </div>
    </Card>

    <RechargeModal :open="rechargeOpen" @close="rechargeOpen = false" @changed="load" />
  </div>
</template>

<style scoped>
.home { display: flex; flex-direction: column; gap: var(--sp-7); }

/* ---------- Hero ---------- */
.hero {
  display: grid; grid-template-columns: 1.35fr 1fr; gap: var(--sp-7);
  background:
    radial-gradient(560px 260px at 96% -30%, rgb(16 148 102 / .22), transparent 62%),
    linear-gradient(180deg, var(--ink-900), var(--ink-950));
  border-radius: var(--r-lg);
  padding: var(--sp-8) var(--sp-8);
  color: #fff;
  box-shadow: var(--shadow-2);
}
.hleft { display: flex; flex-direction: column; justify-content: center; }
.eyebrow {
  font-size: var(--fs-2xs); font-weight: 650; letter-spacing: .16em;
  color: var(--accent-400); text-transform: uppercase;
}
.htitle {
  font-size: var(--fs-3xl);
  font-weight: 680;
  letter-spacing: -.032em;
  line-height: 1.15;
  margin: var(--sp-3) 0 var(--sp-2);
  color: #fff;
}
.hsub {
  font-size: var(--fs-md);
  color: rgb(255 255 255 / .62);
  line-height: 1.6;
  max-width: 46ch;
  margin-bottom: var(--sp-6);
}
.hacts { display: flex; gap: var(--sp-3); flex-wrap: wrap; }

.hright { display: flex; flex-direction: column; gap: var(--sp-3); justify-content: center; }
.bcard {
  background: var(--accent-600);
  border-radius: var(--r-md);
  padding: var(--sp-5) var(--sp-6);
  box-shadow: var(--shadow-accent);
}
.blabel {
  font-size: var(--fs-xs); font-weight: 600;
  color: rgb(255 255 255 / .74);
  letter-spacing: .01em;
}
.bval {
  font-size: var(--fs-3xl);
  font-weight: 700;
  letter-spacing: -.032em;
  line-height: 1.1;
  margin: 6px 0 4px;
}
.bcur { font-size: var(--fs-lg); font-weight: 600; margin-left: 2px; }
.bsub { font-size: var(--fs-2xs); color: rgb(255 255 255 / .76); }

.hmini { display: grid; grid-template-columns: 1fr 1fr; gap: var(--sp-3); }
.mini {
  background: rgb(255 255 255 / .06);
  border: 1px solid rgb(255 255 255 / .1);
  border-radius: var(--r-md);
  padding: var(--sp-4);
}
.mlabel { font-size: var(--fs-2xs); color: rgb(255 255 255 / .52); }
.mval {
  font-size: var(--fs-xl); font-weight: 680;
  letter-spacing: -.02em;
  margin-top: 4px;
}

/* ---------- 订阅额度 ---------- */
.allowcard { border-color: var(--accent-100); }
.tiers { display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr)); gap: var(--sp-5); }
.tier .thead {
  display: flex; justify-content: space-between; align-items: baseline;
  gap: var(--sp-2);
  font-size: var(--fs-xs);
  margin-bottom: 7px;
}
.tname { font-weight: 620; color: var(--ink-700); }
.tnum { color: var(--ink-500); font-variant-numeric: tabular-nums; }
.bar {
  height: 5px; border-radius: var(--r-pill);
  background: var(--paper-4);
  overflow: hidden;
}
.bar i {
  display: block; height: 100%;
  border-radius: var(--r-pill);
  background: linear-gradient(90deg, var(--accent-500), var(--accent-400));
  transition: width var(--t-slow);
}

/* ---------- 分区 ---------- */
.sec { display: flex; flex-direction: column; gap: var(--sp-4); }
.sechead {
  display: flex; align-items: baseline; justify-content: space-between;
  gap: var(--sp-4);
}
.sechead h2,
.sechead2 {
  font-size: var(--fs-xl);
  font-weight: 640;
  letter-spacing: -.022em;
}
.more {
  display: inline-flex; align-items: center; gap: 4px;
  border: none; background: transparent;
  color: var(--accent-600);
  font-size: var(--fs-sm); font-weight: 600;
  padding: 5px 9px;
  border-radius: var(--r-sm);
  transition: all var(--t-fast);
}
.more:hover { background: var(--accent-50); color: var(--accent-700); }
.grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--sp-4); }
.skel { border-radius: var(--r-md); overflow: hidden; }

/* ---------- 快捷入口 ---------- */
.quick { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--sp-4); }
.q {
  display: flex; flex-direction: column; align-items: flex-start; gap: 5px;
  padding: var(--sp-4);
  text-align: left;
  background: var(--paper);
  border: 1px solid var(--hairline);
  border-radius: var(--r-md);
  transition: all var(--t-base);
}
.q:hover {
  border-color: var(--accent-200);
  transform: translateY(-1px);
  box-shadow: var(--shadow-2);
}
.qic {
  width: 34px; height: 34px;
  border-radius: var(--r-sm);
  background: var(--accent-50);
  color: var(--accent-600);
  display: grid; place-items: center;
  margin-bottom: 5px;
}
.qt { font-size: var(--fs-sm); font-weight: 640; color: var(--ink-900); }
.qd { font-size: var(--fs-xs); color: var(--ink-400); line-height: 1.5; }

/* ---------- 趋势 ---------- */
.trendrow { display: grid; grid-template-columns: 1.7fr 1fr; gap: var(--sp-5); align-items: center; }
.days { display: flex; flex-direction: column; gap: 2px; }
.day {
  display: flex; justify-content: space-between;
  font-size: var(--fs-xs);
  padding: 5px 0;
  border-bottom: 1px solid var(--hairline);
}
.day:last-child { border-bottom: none; }
.dl { color: var(--ink-500); }
.dv { font-weight: 600; color: var(--ink-800); font-variant-numeric: tabular-nums; }

@media (max-width: 1080px) {
  .grid, .quick { grid-template-columns: repeat(2, 1fr); }
}
@media (max-width: 860px) {
  .hero { grid-template-columns: 1fr; padding: var(--sp-6) var(--sp-5); gap: var(--sp-6); }
  .htitle { font-size: var(--fs-2xl); }
  .hright { display: grid; grid-template-columns: 1fr; }
  .trendrow { grid-template-columns: 1fr; }
  .grid { grid-template-columns: 1fr; }
}
/* Account overview */
.home { gap: 24px; }
.catalog-empty, .load-error { padding: 32px; background: var(--surface); border: 1px solid var(--hairline); border-radius: 10px; text-align: center; color: var(--ink-500); }
.load-error { color: var(--danger); }
.hero { background: transparent; color: var(--ink-900); padding: 0; border-radius: 0; box-shadow: none; grid-template-columns: 1fr; gap: 24px; }
.hleft { position: relative; padding-right: 280px; min-height: 64px; }
.eyebrow { display: none; } .htitle { color: var(--ink-900); font-size: 22px; margin: 0 0 6px; font-weight: 600; }
.hsub { color: var(--ink-500); font-size: 13px; margin: 0; max-width: none; }
.hacts { position: absolute; right: 0; top: 8px; gap: 8px; }
.hacts :deep(.btn) { height: 34px; padding: 0 14px; font-size: 12px; }
.hright { display: grid; grid-template-columns: 1fr 2fr; gap: 16px; }
.bcard, .mini { background: var(--surface); border: 1px solid var(--hairline); border-radius: 10px; padding: 20px; box-shadow: none; }
.blabel, .mlabel { font-size: 12px; color: var(--ink-500); font-weight: 500; }
.bval, .mval { color: var(--ink-900); font-size: 28px; font-weight: 600; margin: 10px 0 0; line-height: 1.4; }
.bsub { color: var(--ink-400); margin-top: 6px; } .hmini { gap: 16px; }
.sechead h2, .sechead2 { font-size: 15px; font-weight: 600; }
.q { display: grid; grid-template-columns: 36px 1fr; column-gap: 12px; }
.qic { grid-row: span 2; margin: 0; background: var(--well); color: var(--ink-600); }
.q:hover { transform: none; box-shadow: none; background: var(--surface-2); }
@media (max-width: 720px) {
 .hleft { padding-right: 0; } .hacts { position: static; margin-top: 16px; }
 .hright { grid-template-columns: 1fr; } .hmini { grid-template-columns: 1fr 1fr; }
 .quick { grid-template-columns: 1fr; }
}
</style>