<script setup lang="ts">
import { ref, computed, onMounted, inject } from 'vue'
import { toastErr } from '../../shared/utils/toast'
import { modelsApi } from '../api'
import type { Model } from '../types'
import ProductCard from '../components/ProductCard.vue'
import Empty from '../../shared/components/ui/Empty.vue'
import Skeleton from '../../shared/components/ui/Skeleton.vue'
import { portalStore } from '../store'

const go = inject<(v: string) => void>('go') as ((v: string) => void) | undefined

const models = ref<Model[]>([])
const loading = ref(true)
const vendor = ref('')
const sortPrice = ref(false)
const search = ref('')
const error = ref('')

const vendors = computed(() => [...new Set(models.value.map(m => m.vendor))])
const shown = computed(() => {
  let list = vendor.value ? models.value.filter(m => m.vendor === vendor.value) : models.value
  list = list.filter(m => `${m.name} ${m.model_id} ${m.vendor}`.toLowerCase().includes(search.value.trim().toLowerCase()))
  if (sortPrice.value) list = [...list].sort((a, b) => (a.input_price_cent + a.output_price_cent) - (b.input_price_cent + b.output_price_cent))
  return list
})

async function load() {
  loading.value = true
  error.value = ''
  try { models.value = (await modelsApi()).items } catch (e: any) { error.value = e.message; toastErr(e.message) } finally { loading.value = false }
}
onMounted(load)

function onBuy(_m: Model) { go?.('wallet') }
function onTry(m: Model) {
  portalStore.tryModel = m.model_id
  go?.('console')
}
</script>

<template>
  <div class="catalog">
    <div class="head">
      <div>
        <h1 class="t">模型目录</h1>
        <p class="s num">{{ models.length }} 款模型，价格单位为元 / 百万 token。</p>
      </div>
      <button class="sort" :class="{ on: sortPrice }" @click="sortPrice = !sortPrice">
        按输入与输出单价之和排序 {{ sortPrice ? '↑' : '↓' }}
      </button>
    </div>

    <label class="search-field">搜索模型<input v-model="search" placeholder="模型名称、ID 或厂商" type="search" /></label>
    <div class="filters">
      <button class="f" :class="{ on: vendor === '' }" @click="vendor = ''">全部</button>
      <button v-for="v in vendors" :key="v" class="f" :class="{ on: vendor === v }" @click="vendor = v">{{ v }}</button>
    </div>

    <div v-if="loading" class="grid">
      <div v-for="i in 8" :key="i" class="skel"><Skeleton h="230px" /></div>
    </div>
    <p v-else-if="error" role="alert">{{ error }} <button @click="load">重新加载</button></p>
    <Empty v-else-if="!shown.length" text="没有符合条件的可用模型" />
    <div v-else class="grid">
      <ProductCard v-for="m in shown" :key="m.id" :model="m" @buy="onBuy" @try="onTry" />
    </div>


  </div>
</template>

<style scoped>
.search-field{display:flex;align-items:center;gap:16px;font-size:13px;margin-bottom:20px;} .search-field input{width:min(400px,75%);padding:12px 16px;border:1px solid var(--hairline-2);border-radius:7px;background:var(--surface);}
.head {
  display: flex; align-items: flex-end; justify-content: space-between;
  gap: var(--sp-4); margin-bottom: var(--sp-5); flex-wrap: wrap;
}
.t {
  font-size: 22px;
  font-weight: 650;
  letter-spacing: -.026em;
}
.s { font-size: var(--fs-sm); color: var(--ink-400); margin-top: 4px; }

.sort {
  white-space: nowrap;
  height: 32px; padding: 0 var(--sp-4);
  border-radius: var(--r-sm);
  border: none;
  background: var(--paper);
  color: var(--ink-500);
  font-size: var(--fs-xs); font-weight: 600;
  transition: background var(--t-fast), color var(--t-fast), border-color var(--t-fast);
}
.sort:hover { border-color: var(--line-strong); color: var(--ink-700); }
.sort.on {
  border-color: var(--accent-200);
  color: var(--accent-700);
  background: var(--accent-50);
}

.filters {
  display: flex; flex-wrap: wrap; gap: var(--sp-2);
  margin-bottom: var(--sp-6);
}
.f {
  height: 30px; padding: 0 var(--sp-4);
  border-radius: var(--r-sm);
  border: none;
  background: var(--paper);
  color: var(--ink-500);
  font-size: var(--fs-xs); font-weight: 560;
  transition: background var(--t-fast), color var(--t-fast), border-color var(--t-fast);
}
.f:hover { border-color: var(--line-strong); color: var(--ink-800); }
.f.on {
  background: var(--accent-50);
  border-color: var(--accent-100);
  color: var(--accent-700);
}

.grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--sp-4);
}
.skel { border-radius: var(--r-md); overflow: hidden; }

@media (max-width: 1080px) { .grid { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 640px) { .grid { grid-template-columns: 1fr; } }
</style>