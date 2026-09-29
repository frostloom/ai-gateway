<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { IconPlus } from '@tabler/icons-vue'
import { modelsApi, createModelApi, modelStatusApi, modelPriceApi } from '../api'
import type { Model } from '../types'
import { parseTags } from '../../portal/types'
import { money, zhCount } from '../../shared/utils/format'
import { toastOk, toastErr } from '../../shared/utils/toast'
import Card from '../../shared/components/ui/Card.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import DataTable from '../../shared/components/ui/DataTable.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import Modal from '../../shared/components/ui/Modal.vue'
import Field from '../../shared/components/ui/Field.vue'

/** 商品表列宽：ID / 厂商 / model_id / 展示名 / 输入 / 输出 / 上下文 / 标签 / 状态 / 操作 */
const COLS = '46px 80px 1.25fr 1fr .7fr .7fr .7fr .9fr .7fr 118px'

const items = ref<Model[]>([])
const loading = ref(true)
const q = ref({ vendor: '', status: '' })

const vendors = computed(() => [...new Set(items.value.map(m => m.vendor))])
const shown = computed(() => items.value.filter(m =>
  (!q.value.vendor || m.vendor === q.value.vendor) &&
  (q.value.status === '' || m.status === Number(q.value.status)),
))

const addOpen = ref(false)
const addForm = ref({ vendor: '', model_id: '', name: '', input_price_cent: 40, output_price_cent: 160, context_len: 4096, tags: '' })
const addBusy = ref(false)

const priceTarget = ref<Model | null>(null)
const priceForm = ref({ in: 0, out: 0 })
const priceBusy = ref(false)

async function load() { loading.value = true; try { items.value = (await modelsApi()).items } finally { loading.value = false } }
onMounted(load)

async function toggle(m: Model) {
  const next = m.status === 0 ? 1 : 0
  try { await modelStatusApi(m.id, next); toastOk(next === 0 ? `已上架 ${m.name}` : `已下架 ${m.name}`); await load() }
  catch (e: any) { toastErr(e?.message || '操作失败') }
}

function openPrice(m: Model) { priceTarget.value = m; priceForm.value = { in: m.input_price_cent, out: m.output_price_cent } }
async function savePrice() {
  if (!priceTarget.value) return
  priceBusy.value = true
  try {
    await modelPriceApi(priceTarget.value.id, priceForm.value.in, priceForm.value.out)
    toastOk('价格已更新')
    priceTarget.value = null
    await load()
  } catch (e: any) { toastErr(e?.message || '保存失败') }
  finally { priceBusy.value = false }
}

async function createModel() {
  const f = addForm.value
  if (!f.vendor.trim() || !f.model_id.trim()) { toastErr('厂商与 model_id 必填'); return }
  addBusy.value = true
  try {
    await createModelApi({
      vendor: f.vendor.trim(), model_id: f.model_id.trim(), name: f.name.trim() || f.model_id.trim(),
      input_price_cent: Number(f.input_price_cent), output_price_cent: Number(f.output_price_cent),
      context_len: Number(f.context_len),
      tags: f.tags ? f.tags.split(/[,，]/).map(s => s.trim()).filter(Boolean) : [],
    })
    toastOk('SKU 已创建')
    addOpen.value = false
    await load()
  } catch (e: any) { toastErr(e?.message || '创建失败') }
  finally { addBusy.value = false }
}
</script>

<template>
  <div class="models">
    <!-- 筛选与动作 -->
    <div class="head">
      <div class="filters">
        <select v-model="q.vendor" class="sel">
          <option value="">全部厂商</option>
          <option v-for="v in vendors" :key="v" :value="v">{{ v }}</option>
        </select>
        <select v-model="q.status" class="sel">
          <option value="">全部状态</option>
          <option value="0">上架</option>
          <option value="1">下架</option>
        </select>
        <span class="count num">{{ shown.length }} / {{ items.length }} 条</span>
      </div>
      <BaseButton variant="primary" @click="addOpen = true">
        <IconPlus :size="15" /> 新增 SKU
      </BaseButton>
    </div>

    <Card :pad="false">
      <DataTable
        :cols="COLS"
        :loading="loading"
        :empty="!shown.length"
        empty-text="没有符合条件的商品"
      >
        <template #head>
          <span>ID</span>
          <span>厂商</span>
          <span>model_id</span>
          <span>展示名</span>
          <span>输入价</span>
          <span>输出价</span>
          <span>上下文</span>
          <span>标签</span>
          <span>状态</span>
          <span class="ta-r">操作</span>
        </template>

        <div v-for="m in shown" :key="m.id" class="dt-row dt-num">
          <span class="dt-muted">{{ m.id }}</span>
          <span class="dt-muted">{{ m.vendor }}</span>
          <span class="mono clip" :title="m.model_id">{{ m.model_id }}</span>
          <span class="dt-strong clip" :title="m.name">{{ m.name }}</span>
          <span>{{ money(m.input_price_cent) }}</span>
          <span>{{ money(m.output_price_cent) }}</span>
          <span class="dt-muted">{{ zhCount(m.context_len) }}</span>
          <span class="tags">
            <Tag v-for="t in parseTags(m.tags).slice(0, 2)" :key="t" tone="muted">{{ t }}</Tag>
          </span>
          <span>
            <Tag :tone="m.status === 0 ? 'ok' : 'muted'" dot>
              {{ m.status === 0 ? '上架' : '下架' }}
            </Tag>
          </span>
          <span class="ops">
            <BaseButton size="xs" variant="secondary" @click="toggle(m)">
              {{ m.status === 0 ? '下架' : '上架' }}
            </BaseButton>
            <BaseButton size="xs" variant="ghost" @click="openPrice(m)">改价</BaseButton>
          </span>
        </div>
      </DataTable>
    </Card>

    <!-- 新增 SKU -->
    <Modal :open="addOpen" title="新增 SKU" @close="addOpen = false" width="460">
      <div class="form2">
        <Field label="厂商" required><input v-model="addForm.vendor" placeholder="如 DeepSeek" /></Field>
        <Field label="model_id" required><input v-model="addForm.model_id" placeholder="调用名，唯一" /></Field>
        <Field label="展示名"><input v-model="addForm.name" placeholder="留空则同 model_id" /></Field>
        <div class="two">
          <Field label="输入价（元 / M）"><input v-model.number="addForm.input_price_cent" type="number" min="0" /></Field>
          <Field label="输出价（元 / M）"><input v-model.number="addForm.output_price_cent" type="number" min="0" /></Field>
        </div>
        <Field label="上下文（token）"><input v-model.number="addForm.context_len" type="number" min="0" /></Field>
        <Field label="标签" hint="逗号分隔，如 文本,旗舰"><input v-model="addForm.tags" /></Field>
      </div>
      <template #footer>
        <BaseButton variant="ghost" @click="addOpen = false">取消</BaseButton>
        <BaseButton variant="primary" :loading="addBusy" @click="createModel">保存 SKU</BaseButton>
      </template>
    </Modal>

    <!-- 改价 -->
    <Modal :open="!!priceTarget" title="修改价格" @close="priceTarget = null" width="380">
      <p class="ptip num">{{ priceTarget?.name }}（{{ priceTarget?.model_id }}）</p>
      <div class="form2">
        <Field label="输入价（元 / 百万 token）"><input v-model.number="priceForm.in" type="number" min="0" /></Field>
        <Field label="输出价（元 / 百万 token）"><input v-model.number="priceForm.out" type="number" min="0" /></Field>
      </div>
      <template #footer>
        <BaseButton variant="ghost" @click="priceTarget = null">取消</BaseButton>
        <BaseButton variant="primary" :loading="priceBusy" @click="savePrice">保存</BaseButton>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.head {
  display: flex; align-items: center; justify-content: space-between;
  gap: var(--sp-4); margin-bottom: var(--sp-4); flex-wrap: wrap;
}
.filters { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.sel {
  height: 34px; padding: 0 var(--sp-3);
  border: none;
  border-radius: var(--r-sm);
  background: var(--paper);
  color: var(--ink-700);
  font-size: var(--fs-sm);
  transition: border-color var(--t-fast);
}
.sel:hover { border-color: var(--line-strong); }
.sel:focus { outline: none; border-color: var(--accent-500); box-shadow: 0 0 0 3px var(--accent-50); }
.count { font-size: var(--fs-xs); color: var(--ink-400); }

.tags { display: flex; gap: 4px; flex-wrap: wrap; }
.ops { display: flex; gap: 6px; justify-content: flex-end; }
.clip { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ta-r { text-align: right; }

.form2 { display: flex; flex-direction: column; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: var(--sp-3); }
.ptip {
  font-size: var(--fs-sm); color: var(--ink-500);
  margin-bottom: var(--sp-3);
  padding-bottom: var(--sp-3);
  border-bottom: 1px solid var(--hairline);
}

@media (max-width: 880px) {
  .two { grid-template-columns: 1fr; }
}
</style>