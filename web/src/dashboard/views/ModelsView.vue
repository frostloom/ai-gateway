<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { IconPlus, IconSearch, IconEdit } from '@tabler/icons-vue'
import { modelsApi, channelsApi, createModelApi, modelStatusApi, modelPriceApi } from '../api'
import type { Model, ProviderMargin } from '../types'
import { money, zhCount } from '../../shared/utils/format'
import { toastOk } from '../../shared/utils/toast'
import Tag from '../../shared/components/ui/Tag.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import Modal from '../../shared/components/ui/Modal.vue'
import Field from '../../shared/components/ui/Field.vue'
import ProviderModelImport from '../components/ProviderModelImport.vue'

const items = ref<Model[]>([])
const importOpen = ref(false)
const channelPickerOpen = ref(false)
const channels = ref<ProviderMargin[]>([])
const importProviderId = ref<number>()
function openImport() {
  importProviderId.value = channels.value[0]?.id
  channelPickerOpen.value = true
}
function selectChannel() {
  if (!importProviderId.value) return
  channelPickerOpen.value = false
  importOpen.value = true
}
const importRequest = computed(() => importProviderId.value ? { provider_id: importProviderId.value } : null)
const configured = ref<string[]>([])
const loading = ref(true)
const error = ref('')
const query = ref('')
const status = ref('')
const shown = computed(() => items.value.filter(m => (status.value === '' || m.status === Number(status.value)) && `${m.name} ${m.model_id} ${m.vendor}`.toLowerCase().includes(query.value.trim().toLowerCase())))
const candidates = computed(() => configured.value.filter(id => !items.value.some(m => m.model_id === id)))
const selected = ref<Model | null>(null)
const prices = ref({ input: 0, output: 0 })
const addOpen = ref(false)
const busy = ref(false)
const formError = ref('')
const defaults = () => ({ model_id: '', vendor: '', name: '', input_price_cent: '' as number | string, output_price_cent: '' as number | string, context_len: '' as number | string })
const form = ref(defaults())

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [catalog, channelList] = await Promise.all([modelsApi(), channelsApi()])
    const ids = new Set<string>()
    channels.value = channelList.items
    for (const channel of channelList.items) {
      if (channel.status !== 0 || channel.weight < 1 || channel.weight > 1000000 || !channel.base_url.trim()) continue
      try {
        const names: unknown = JSON.parse(channel.models)
        if (Array.isArray(names)) for (const id of names) {
          if (typeof id === 'string' && id && id !== '*' && id.trim() === id) ids.add(id)
        }
      } catch { /* Invalid upstream model lists cannot be published. */ }
    }
    configured.value = [...ids].sort()
    items.value = catalog.items.filter(m => ids.has(m.model_id))
  } catch (e: any) { error.value = e.message || '模型加载失败' }
  finally { loading.value = false }
}
function openModel(model: Model) {
  selected.value = model
  prices.value = { input: model.input_price_cent, output: model.output_price_cent }
  formError.value = ''
}
function openAdd() {
  form.value = { ...defaults(), model_id: candidates.value[0] || '' }
  formError.value = ''
  addOpen.value = true
}
async function savePrice() {
  if (!selected.value || busy.value) return
  if (![prices.value.input, prices.value.output].every(n => Number.isSafeInteger(n) && n >= 0)) { formError.value = '价格须为非负整数，单位为分 / 百万 token'; return }
  busy.value = true
  formError.value = ''
  try {
    await modelPriceApi(selected.value.id, prices.value.input, prices.value.output)
    toastOk('价格已更新')
    selected.value = null
    await load()
  } catch (e: any) { formError.value = e.message || '保存失败' }
  finally { busy.value = false }
}
async function toggle() {
  if (!selected.value || busy.value) return
  const model = selected.value
  busy.value = true
  formError.value = ''
  try {
    await modelStatusApi(model.id, model.status === 0 ? 1 : 0)
    selected.value = null
    toastOk(model.status === 0 ? '模型已下架' : '模型已上架')
    await load()
  } catch (e: any) { formError.value = e.message || '操作失败' }
  finally { busy.value = false }
}
async function create() {
  if (busy.value) return
  const value = form.value
  if (!candidates.value.includes(value.model_id) || !value.vendor.trim()) { formError.value = '请选择已配置模型并填写厂商'; return }
  if (![value.input_price_cent, value.output_price_cent].every(n => typeof n === 'number' && Number.isSafeInteger(n) && n >= 0) || typeof value.context_len !== 'number' || !Number.isSafeInteger(value.context_len) || value.context_len < 1) { formError.value = '请填写非负整数单价和正整数上下文长度'; return }
  busy.value = true
  formError.value = ''
  try {
    await createModelApi({ ...value, vendor: value.vendor.trim(), name: value.name.trim() || value.model_id, tags: [] })
    addOpen.value = false
    toastOk('模型定价已保存')
    await load()
  } catch (e: any) { formError.value = e.message || '保存失败' }
  finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <div class="models">
    <div class="toolbar">
      <label class="search"><IconSearch :size="16" /><input v-model="query" aria-label="搜索模型" placeholder="搜索模型、编号或厂商" /></label>
      <select v-model="status" aria-label="模型上架状态"><option value="">全部状态</option><option value="0">已上架</option><option value="1">已下架</option></select>
      <BaseButton :disabled="loading || !channels.length" @click="openImport">从渠道发现并导入</BaseButton>
      <BaseButton variant="primary" :disabled="loading || !candidates.length" @click="openAdd"><IconPlus :size="16" />配置模型定价</BaseButton>
    </div>
    <div v-if="error" class="state" role="alert"><p>{{ error }}</p><BaseButton @click="load">重新加载</BaseButton></div>
    <p v-else-if="loading" class="state" role="status">正在读取模型…</p>
    <div v-else-if="!shown.length" class="state"><p>{{ configured.length ? '暂无匹配的模型定价' : '暂无已配置的上游模型' }}</p><BaseButton v-if="candidates.length" @click="openAdd">配置模型定价</BaseButton></div>
    <div v-else class="model-grid">
      <article v-for="model in shown" :key="model.id" class="model-card">
        <div class="card-head"><div><span class="vendor">{{ model.vendor }}</span><h2>{{ model.name }}</h2></div><button class="edit" :aria-label="'编辑模型 ' + model.name" @click="openModel(model)"><IconEdit :size="18" /></button></div>
        <p class="model-id mono">{{ model.model_id }}</p>
        <div class="metadata"><Tag :tone="model.status === 0 ? 'ok' : 'muted'">{{ model.status === 0 ? '已上架' : '已下架' }}</Tag><span>上下文 {{ zhCount(model.context_len) }}</span></div>
        <dl class="price-grid"><div><dt>输入 / 百万 token</dt><dd>{{ money(model.input_price_cent) }}</dd></div><div><dt>输出 / 百万 token</dt><dd>{{ money(model.output_price_cent) }}</dd></div></dl>
      </article>
    </div>
    <Modal :open="channelPickerOpen" title="选择上游渠道" width="520" @close="channelPickerOpen = false"><Field label="检测渠道" required><select v-model.number="importProviderId"><option v-for="channel in channels" :key="channel.id" :value="channel.id">{{ channel.name }}（#{{ channel.id }}）</option></select></Field><template #footer><BaseButton @click="channelPickerOpen = false">取消</BaseButton><BaseButton variant="primary" :disabled="!importProviderId" @click="selectChannel">检测并发现模型</BaseButton></template></Modal>
    <ProviderModelImport :open="importOpen" :request="importRequest" @close="importOpen = false" @imported="load" />
    <Modal :open="!!selected" title="模型与定价" width="600" :close-disabled="busy" @close="selected = null">
      <form v-if="selected" id="model-price-form" @submit.prevent="savePrice">
        <div class="model-summary"><strong>{{ selected.name }}</strong><span class="mono">{{ selected.model_id }}</span></div>
        <div class="two"><Field label="输入单价（分 / 百万 token）" required><input v-model.number="prices.input" type="number" min="0" step="1" :disabled="busy" /></Field><Field label="输出单价（分 / 百万 token）" required><input v-model.number="prices.output" type="number" min="0" step="1" :disabled="busy" /></Field></div>
        <p v-if="formError" class="error" role="alert">{{ formError }}</p>
      </form>
      <template #footer><BaseButton :disabled="busy" @click="toggle">{{ selected?.status === 0 ? '下架模型' : '上架模型' }}</BaseButton><span class="spacer" /><BaseButton :disabled="busy" @click="selected = null">取消</BaseButton><BaseButton variant="primary" :loading="busy" @click="savePrice">保存价格</BaseButton></template>
    </Modal>
    <Modal :open="addOpen" title="配置模型定价" width="640" :close-disabled="busy" @close="addOpen = false">
      <form id="model-create-form" @submit.prevent="create">
        <Field label="上游模型编号" required><select v-model="form.model_id" :disabled="busy"><option v-for="id in candidates" :key="id" :value="id">{{ id }}</option></select></Field>
        <div class="two"><Field label="厂商" required><input v-model="form.vendor" :disabled="busy" maxlength="64" /></Field><Field label="展示名称"><input v-model="form.name" :disabled="busy" maxlength="128" /></Field></div>
        <div class="two"><Field label="输入单价（分 / 百万 token）" required><input v-model.number="form.input_price_cent" type="number" min="0" step="1" :disabled="busy" /></Field><Field label="输出单价（分 / 百万 token）" required><input v-model.number="form.output_price_cent" type="number" min="0" step="1" :disabled="busy" /></Field></div>
        <Field label="上下文长度（token）" required><input v-model.number="form.context_len" type="number" min="1" step="1" :disabled="busy" /></Field>
        <p v-if="formError" class="error" role="alert">{{ formError }}</p>
      </form>
      <template #footer><BaseButton :disabled="busy" @click="addOpen = false">取消</BaseButton><BaseButton variant="primary" :loading="busy" @click="create">保存并上架</BaseButton></template>
    </Modal>
  </div>
</template>

<style scoped>
/* Hallmark · v2.0.0 · theme: aixcoder · scope: app */
/* Critique: aligned price pairs and a centered editor keep configuration separate from browsing. */
.models { display: grid; gap: 24px; }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }
.search { display: flex; align-items: center; gap: 8px; width: min(100%, 360px); padding: 0 12px; border: 1px solid var(--hairline); border-radius: 6px; color: var(--ink-500); }
.search input { min-width: 0; width: 100%; height: 40px; border: 0; background: var(--surface); }
.toolbar > select { height: 42px; border: 1px solid var(--hairline); border-radius: 6px; padding: 0 12px; background: var(--surface); }
.model-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr)); gap: 20px; }
.model-card { border: 1px solid var(--hairline); border-radius: 8px; overflow: hidden; padding: 20px; min-width: 0; }
.card-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.card-head > div { min-width: 0; }
.vendor { color: var(--ink-500); font-size: 12px; }
h2 { margin-top: 8px; font-size: 16px; font-weight: 600; overflow-wrap: anywhere; }
.edit { display: grid; place-items: center; width: 36px; height: 36px; flex: none; border: 0; background: var(--surface); color: var(--ink-700); border-radius: 6px; }
.edit:hover { background: var(--surface-2); }
.model-id { font-size: 12px; color: var(--ink-500); margin-top: 12px; overflow-wrap: anywhere; }
.metadata { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; font-size: 12px; color: var(--ink-500); margin-top: 20px; }
.price-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; border-top: 1px solid var(--hairline); padding-top: 20px; margin-top: 20px; }
dt { font-size: 12px; color: var(--ink-500); }
dd { font-size: 18px; font-weight: 600; margin-top: 8px; font-variant-numeric: tabular-nums; }
.two { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.model-summary { display: flex; flex-direction: column; gap: 8px; padding: 4px 0 24px; }
.model-summary span { color: var(--ink-500); font-size: 12px; }
.state { padding: 64px 20px; display: grid; gap: 16px; place-items: center; color: var(--ink-500); border: 1px dashed var(--hairline); border-radius: 8px; }
.error { color: var(--danger); font-size: 13px; }
.spacer { flex: 1; }
button:focus-visible, input:focus-visible, select:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
button:active:not(:disabled) { transform: translateY(1px); }
button:disabled { cursor: not-allowed; }
@media (max-width: 540px) { .two { grid-template-columns: minmax(0, 1fr); gap: 0; } .search { width: 100%; } }
</style>
