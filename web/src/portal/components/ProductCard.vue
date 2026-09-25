<script setup lang="ts">
import { computed } from 'vue'
import { IconBolt } from '@tabler/icons-vue'
import type { Model } from '../types'
import { parseTags } from '../types'
import { yuan, zhCount } from '../../shared/utils/format'
const props = defineProps<{ model: Model; featured?: boolean }>()
const emit = defineEmits<{ (e: 'buy', m: Model): void; (e: 'try', m: Model): void }>()

const tags = computed(() => parseTags(props.model.tags))
const ctx = computed(() => {
  const n = props.model.context_len || 0
  return n >= 1024 ? zhCount(n / 1024) + 'K' : String(n)
})

/** 厂商首字母作为品牌标记（比彩色小字更克制，也更像商品） */
const initials = computed(() => {
  const v = props.model.vendor || ''
  const ascii = v.match(/[A-Za-z]/g)
  if (ascii && ascii.length) return ascii.slice(0, 2).join('').toUpperCase()
  return v.slice(0, 1)
})

/** 免费模型（双价 0）单独标记 */
const isFree = computed(() => props.model.input_price_cent === 0 && props.model.output_price_cent === 0)
</script>

<template>
  <article class="card">
    <header class="top">
      <div class="vendor">
        <span class="vmark">{{ initials }}</span>
        <span class="vname">{{ model.vendor }}</span>
      </div>
      <span v-if="isFree" class="free">免费额度</span>
      <span v-else-if="featured" class="feat">热卖</span>
    </header>

    <h3 class="name">{{ model.name }}</h3>
    <p class="mid mono">{{ model.model_id }}</p>

    <div class="pricing">
      <div class="pcol">
        <span class="pk">输入</span>
        <span class="pv num">¥{{ yuan(model.input_price_cent) }}</span>
      </div>
      <div class="pdiv" />
      <div class="pcol">
        <span class="pk">输出</span>
        <span class="pv num">¥{{ yuan(model.output_price_cent) }}</span>
      </div>
      <span class="punit">/ 百万 token</span>
    </div>

    <div class="meta">
      <span class="mchip num">{{ ctx }} 上下文</span>
      <span v-for="t in tags.slice(0, 3)" :key="t" class="mchip">{{ t }}</span>
    </div>

    <footer class="acts">
      <button class="buy" @click="emit('buy', model)">购买额度</button>
      <button class="try" title="在接口调试中使用" @click="emit('try', model)">
        <IconBolt :size="14" /> 试用
      </button>
    </footer>
  </article>
</template>

<style scoped>
.card {
  display: flex; flex-direction: column;
  background: var(--surface);
  border: none;
  border-radius: var(--r-lg);
  padding: var(--sp-5);
  box-shadow: var(--shadow-raise), var(--inset-hi);
  transition: transform var(--t-base), box-shadow var(--t-base);
}
.card:hover {
  transform: translateY(-3px);
  box-shadow: var(--shadow-float), var(--inset-hi);
}

.top {
  display: flex; align-items: center; justify-content: space-between;
  gap: var(--sp-2); margin-bottom: var(--sp-3);
}
.vendor { display: flex; align-items: center; gap: 7px; min-width: 0; }
/* 品牌标记：方形小色块 + 首字母，比纯文字更有"货架感" */
.vmark {
  width: 20px; height: 20px; flex: none;
  border-radius: var(--r-xs);
  background: var(--ink-900);
  color: #fff;
  font-size: 10px; font-weight: 700;
  letter-spacing: -.02em;
  display: grid; place-items: center;
}
.vname {
  font-size: var(--fs-xs);
  font-weight: 600;
  color: var(--ink-600);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.feat {
  flex: none;
  font-size: var(--fs-2xs); font-weight: 650;
  color: var(--accent-700);
  background: var(--accent-50);
  padding: 2px 7px;
  border-radius: var(--r-xs);
}
.free {
  flex: none;
  font-size: var(--fs-2xs); font-weight: 650;
  color: var(--warn);
  background: var(--warn-bg);
  padding: 2px 7px;
  border-radius: var(--r-xs);
}

.name {
  font-size: var(--fs-lg);
  font-weight: 620;
  letter-spacing: -.018em;
  color: var(--ink-900);
  line-height: 1.3;
}
.mid {
  font-size: var(--fs-2xs);
  color: var(--ink-400);
  margin: 3px 0 var(--sp-4);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

/* 价格区：不套容器，用分隔线与排版建立层级（避免"卡片里再套卡片"） */
.pricing {
  display: flex; align-items: baseline; gap: var(--sp-3);
  padding-bottom: var(--sp-3);
  border-bottom: 1px dashed var(--hairline);
  margin-bottom: var(--sp-3);
  flex-wrap: wrap;
}
.pcol { display: flex; flex-direction: column; gap: 1px; }
.pk {
  font-size: var(--fs-2xs);
  color: var(--ink-400);
  font-weight: 550;
}
.pv {
  font-size: var(--fs-lg);
  font-weight: 680;
  letter-spacing: -.022em;
  color: var(--ink-900);
}
.pdiv { width: 1px; height: 22px; background: var(--hairline-2); align-self: center; }
.punit { font-size: var(--fs-2xs); color: var(--ink-400); margin-left: auto; }

.meta {
  display: flex; flex-wrap: wrap; gap: 5px;
  margin-bottom: var(--sp-4);
}
.mchip {
  font-size: var(--fs-2xs);
  color: var(--ink-500);
  background: var(--paper-3);
  border-radius: var(--r-xs);
  padding: 2px 7px;
  font-weight: 550;
}

.acts { display: flex; gap: var(--sp-2); margin-top: auto; }
.buy {
  flex: 1; height: 36px;
  background: var(--accent-600); color: #fff;
  border: none; border-radius: var(--r-pill);
  font-size: var(--fs-sm); font-weight: 580;
  box-shadow: var(--shadow-accent);
  transition: background var(--t-fast), transform var(--t-spring), box-shadow var(--t-base);
}
.buy:hover {
  background: var(--accent-500);
  box-shadow: var(--shadow-accent), 0 16px 32px -12px rgb(13 128 88 / .5);
}
.buy:active { transform: scale(.975); }
.try {
  display: inline-flex; align-items: center; gap: 5px;
  height: 36px; padding: 0 16px;
  background: var(--well); color: var(--ink-600);
  border: none; border-radius: var(--r-pill);
  box-shadow: var(--inset-well);
  font-size: var(--fs-sm); font-weight: 540;
  transition: background var(--t-fast), color var(--t-fast), transform var(--t-spring);
}
.try:hover {
  color: var(--accent-700);
  background: var(--accent-50);
}
.try:active { transform: scale(.975); }
</style>
