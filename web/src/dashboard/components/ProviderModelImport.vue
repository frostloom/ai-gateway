<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { IconRefresh, IconSearch } from '@tabler/icons-vue'
import { discoverChannelApi, importChannelModelsApi } from '../api'
import type { ChannelDiscoverRequest, ChannelDiscovery, DiscoveredModel } from '../types'
import Modal from '../../shared/components/ui/Modal.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import Field from '../../shared/components/ui/Field.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import { toastOk } from '../../shared/utils/toast'

const props = defineProps<{ open: boolean; request: ChannelDiscoverRequest | null; draft?: boolean }>()
const emit = defineEmits<{ close: []; selected: [ids: string[]]; imported: [] }>()
const result = ref<ChannelDiscovery | null>(null)
const loading = ref(false)
const importing = ref(false)
const error = ref('')
const query = ref('')
const selected = ref<string[]>([])
const inputPrice = ref<number | string>('')
const outputPrice = ref<number | string>('')
const fallbackContext = ref<number | string>('')
const contexts = ref<Record<string, number | string>>(Object.create(null))
const names = ref<Record<string, string>>({})
const vendors = ref<Record<string, string>>({})
const maxSelection = 100
const validText = (value: string, max: number) => value.trim() === value && Array.from(value).length <= max && !/\p{Cc}/u.test(value)
function identifierError(model: DiscoveredModel): string {
  return model.model_id && model.model_id !== '*' && validText(model.model_id, 64) ? '' : '模型编号须为 1 至 64 个字符，不含首尾空格或控制字符'
}
let generation = 0
const visible = computed(() => (result.value?.models || []).filter(m => `${m.model_id} ${m.name} ${m.vendor}`.toLowerCase().includes(query.value.trim().toLowerCase())))
const chosen = computed(() => (result.value?.models || []).filter(m => selected.value.includes(m.model_id)))
const newModels = computed(() => chosen.value.filter(m => !m.exists))
const selectableVisible = computed(() => visible.value.filter(m => !identifierError(m)))
const allVisible = computed(() => selectableVisible.value.length > 0 && selectableVisible.value.every(m => selected.value.includes(m.model_id)))
const positiveInteger = (n: unknown): n is number => typeof n === 'number' && Number.isSafeInteger(n) && n > 0 && n <= 2147483647
const nonNegativeInteger = (n: unknown): n is number => typeof n === 'number' && Number.isSafeInteger(n) && n >= 0 && n <= 1000000000
function contextFor(model: DiscoveredModel): number | string {
  return positiveInteger(model.context_len) ? model.context_len : contexts.value[model.model_id] ?? ''
}
function toggleModel(model: DiscoveredModel) {
  error.value = ''
  if (identifierError(model)) return
  if (selected.value.includes(model.model_id)) {
    selected.value = selected.value.filter(id => id !== model.model_id)
  } else if (selected.value.length < maxSelection) {
    selected.value = [...selected.value, model.model_id]
  } else {
    error.value = '每次最多选择 100 个模型，请分次导入'
  }
}
function toggleVisible() {
  const ids = new Set(selected.value)
  const remove = allVisible.value
  let limited = false
  for (const model of selectableVisible.value) {
    if (remove) ids.delete(model.model_id)
    else if (ids.has(model.model_id) || ids.size < maxSelection) ids.add(model.model_id)
    else limited = true
  }
  selected.value = [...ids]
  error.value = limited ? '已选择前 100 项，其余模型可在下次导入时选择' : ''
}
function applyContext() {
  if (!positiveInteger(fallbackContext.value)) { error.value = '请填写 1 至 2147483647 的整数上下文长度'; return }
  for (const model of newModels.value) if (!positiveInteger(model.context_len)) contexts.value[model.model_id] = fallbackContext.value
  error.value = ''
}
async function discover() {
  if (!props.request || importing.value) return
  const current = ++generation
  loading.value = true
  result.value = null
  selected.value = []
  contexts.value = Object.create(null)
  error.value = ''
  try {
    const response = await discoverChannelApi({ ...props.request })
    if (current !== generation || !props.open) return
    result.value = response
    names.value = Object.fromEntries(response.models.map(m => [m.model_id, m.name || m.model_id]))
    vendors.value = Object.fromEntries(response.models.map(m => [m.model_id, m.vendor || '']))
  } catch (e: any) {
    if (current === generation && props.open) error.value = e?.message || '检测失败，请检查上游地址与密钥后重试'
  } finally {
    if (current === generation) loading.value = false
  }
}
function close() {
  if (importing.value) return
  generation++
  loading.value = false
  emit('close')
}
async function submit() {
  if (loading.value || importing.value || !chosen.value.length) return
  error.value = ''
  if (chosen.value.length > maxSelection || chosen.value.some(m => identifierError(m))) {
    error.value = '每次最多选择 100 个有效模型编号'
    return
  }
  if (props.draft) { emit('selected', [...selected.value]); return }
  if (!props.request?.provider_id) return
  const invalidMetadata = newModels.value.find(m => !validText(names.value[m.model_id] || '', 128) || !validText(vendors.value[m.model_id] || '', 32))
  if (invalidMetadata) {
    error.value = `${invalidMetadata.model_id}：名称最多 128 字符、厂商最多 32 字符，不能含首尾空格或控制字符`
    return
  }
  // Existing records are sent only to merge channel routing. Their prices and status stay server-owned.
  if (newModels.value.length && (!nonNegativeInteger(inputPrice.value) || !nonNegativeInteger(outputPrice.value))) {
    error.value = '请明确填写 0 至 10亿的整数单价，免费模型填写 0'
    return
  }
  if (newModels.value.some(m => !positiveInteger(contextFor(m)))) {
    error.value = '请补充所选新模型的上下文长度'
    return
  }
  importing.value = true
  try {
    const response = await importChannelModelsApi(props.request.provider_id, chosen.value.map(m => m.exists ? { model_id: m.model_id } : ({
      model_id: m.model_id, name: names.value[m.model_id] || m.model_id, vendor: vendors.value[m.model_id] || undefined,
      context_len: Number(contextFor(m)),
      input_price_cent: Number(inputPrice.value),
      output_price_cent: Number(outputPrice.value),
    })))
    toastOk(`已新增 ${response.created.length} 个模型，保留 ${response.skipped.length} 个已有模型`)
    emit('imported')
    emit('close')
  } catch (e: any) { error.value = e?.message || '导入失败，请重试' }
  finally { importing.value = false }
}
watch(() => [props.open, props.request] as const, () => {
  generation++
  if (props.open && props.request) {
    query.value = ''; inputPrice.value = ''; outputPrice.value = ''; fallbackContext.value = ''
    void discover()
  } else { result.value = null; selected.value = []; loading.value = false }
}, { immediate: true, deep: true })
onBeforeUnmount(() => { generation++ })
</script>

<template>
  <Modal :open="open" :title="draft ? '检测上游并选择模型' : '发现并导入模型'" width="920" :close-disabled="importing" @close="close">
    <div class="importer">
      <div class="discovery-head">
        <p v-if="loading" role="status">正在验证上游连接并读取模型目录…</p>
        <p v-else-if="result">
          <Tag tone="ok">连接成功</Tag>
          <span>{{ result.latency_ms }} ms</span>
          <span>{{ result.models.length }} 个模型</span>
        </p>
        <p v-else>验证上游连接后读取支持的模型</p>
        <BaseButton size="sm" :disabled="loading || importing" @click="discover">
          <IconRefresh :size="14" />{{ result ? '刷新目录' : '重新检测' }}</BaseButton>
      </div>
      <p v-if="result" class="endpoint mono" :title="result.endpoint">{{ result.endpoint }}</p>
      <p v-for="warning in result?.warnings || []" :key="warning" class="warning">{{ warning }}</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <template v-if="result">
        <div class="list-tools">
          <label class="search">
            <IconSearch :size="16" />
            <input v-model="query" :disabled="importing" aria-label="搜索上游模型" placeholder="搜索编号、名称或厂商" />
          </label>
          <span>已选 {{ selected.length }} / {{ maxSelection }} 项</span>
        </div>
        <div class="table-scroll">
          <table>
            <thead>
              <tr>
                <th>
                  <input type="checkbox" aria-label="选择当前搜索结果" :checked="allVisible" :disabled="importing || !selectableVisible.length" @change="toggleVisible" />
                </th>
                <th>模型</th>
                <th>上下文 / token</th>
                <th>元数据来源</th>
                <th>目录状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="model in visible" :key="model.model_id">
                <td>
                  <input :checked="selected.includes(model.model_id)" type="checkbox" :aria-label="'选择 ' + model.model_id" :disabled="importing || !!identifierError(model)" @change="toggleModel(model)" />
                </td>
                <td>
                  <strong>{{ model.name || model.model_id }}</strong>
                  <span class="mono model-id">{{ model.model_id }}</span>
                  <template v-if="!draft && !model.exists && selected.includes(model.model_id)">
                    <label class="metadata-field">名称<input v-model="names[model.model_id]" :disabled="importing" :aria-label="model.model_id + ' 名称'" /></label>
                    <label class="metadata-field">厂商<input v-model="vendors[model.model_id]" :disabled="importing" :aria-label="model.model_id + ' 厂商'" /></label>
                  </template>
                  <small v-else-if="model.vendor">{{ model.vendor }}</small>
                  <small v-if="identifierError(model)" class="error">{{ identifierError(model) }}</small>
                </td>
                <td>
                  <span v-if="positiveInteger(model.context_len)">{{ model.context_len.toLocaleString() }}</span>
                  <input v-else-if="!draft && !model.exists" v-model.number="contexts[model.model_id]" class="context-input" type="number" min="1" step="1" :disabled="importing" :aria-label="model.model_id + ' 上下文长度'" placeholder="待补充" />
                  <span v-else class="muted">未提供</span>
                  <small v-if="model.max_output_tokens">最大输出 {{ model.max_output_tokens.toLocaleString() }}</small>
                </td>
                <td>{{ model.metadata_source === 'upstream' ? '上游' : model.metadata_source === 'catalog' ? '本地目录' : '未知' }}</td>
                <td>
                  <Tag :tone="model.exists ? 'muted' : 'info'">{{ model.exists ? '已有定价' : '待创建' }}</Tag>
                  <small>{{ model.configured ? '渠道已配置' : '尚未配置' }}</small>
                </td>
              </tr>
              <tr v-if="!visible.length">
                <td colspan="5" class="empty">{{ result.models.length ? '没有匹配的模型' : '上游未返回模型，检查地址或手动填写编号' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="draft" class="note">选择的编号将填入渠道表单。保存渠道后，可在渠道卡片中批量定价上架。</p>
        <div v-else class="pricing">
          <p class="note">新模型明确填写价格后上架，已有模型保留原价格与上下架状态。</p>
          <div v-if="newModels.length" class="price-fields">
            <Field label="新模型输入价（分 / 百万 token）" required>
              <input v-model.number="inputPrice" :disabled="importing" type="number" min="0" step="1" placeholder="免费填写 0" />
            </Field>
            <Field label="新模型输出价（分 / 百万 token）" required>
              <input v-model.number="outputPrice" :disabled="importing" type="number" min="0" step="1" placeholder="免费填写 0" />
            </Field>
            <Field label="未知上下文批量填充（token）">
              <div class="context-fill">
                <input v-model.number="fallbackContext" :disabled="importing" type="number" min="1" step="1" placeholder="仅填入所选未知项" />
                <BaseButton size="sm" :disabled="importing" @click="applyContext">填入</BaseButton>
              </div>
            </Field>
          </div>
        </div>
      </template>
    </div>
    <template #footer>
      <BaseButton :disabled="importing" @click="close">取消</BaseButton>
      <BaseButton variant="primary" :loading="importing" :disabled="loading || !chosen.length" @click="submit">{{ draft ? `填入 ${chosen.length} 个模型` : `导入 ${chosen.length} 个模型` }}</BaseButton>
    </template>
  </Modal>
</template>

<style scoped>
.importer { display: grid; gap: 14px; padding-top: 6px; }
.discovery-head, .discovery-head p, .list-tools { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.discovery-head, .list-tools { justify-content: space-between; }
.discovery-head p, .list-tools > span, .note, .endpoint { font-size: 12px; color: var(--ink-500); }
.endpoint { overflow-wrap: anywhere; }
.search { display: flex; align-items: center; gap: 8px; border: 1px solid var(--hairline); border-radius: 6px; padding: 0 10px; width: min(100%, 380px); }
.search input { height: 36px; width: 100%; min-width: 0; border: 0; background: transparent; }
.table-scroll { overflow: auto; max-height: 340px; border: 1px solid var(--hairline); border-radius: 8px; }
table { width: 100%; border-collapse: collapse; text-align: left; font-size: 12px; }
th { position: sticky; top: 0; background: var(--surface-2); color: var(--ink-500); font-weight: 500; z-index: 1; }
th, td { padding: 12px; border-bottom: 1px solid var(--hairline); vertical-align: middle; }
th:first-child, td:first-child { width: 38px; }
td strong { display: block; font-size: 13px; overflow-wrap: anywhere; }
.model-id { display: block; margin-top: 4px; overflow-wrap: anywhere; color: var(--ink-500); }
small { display: block; color: var(--ink-500); font-size: 11px; margin-top: 5px; }
.metadata-field { display: grid; grid-template-columns: 30px minmax(0, 1fr); align-items: center; gap: 6px; margin-top: 8px; color: var(--ink-500); }
.metadata-field input { width: 100%; min-width: 120px; padding: 6px 8px; }
.context-input { width: 120px; min-width: 90px; padding: 7px 8px; }
.empty { padding: 32px 12px; text-align: center; color: var(--ink-500); }
.price-fields { margin-top: 14px; display: grid; grid-template-columns: 1fr 1fr; gap: 0 16px; }
.price-fields > :last-child { grid-column: 1 / -1; }
.context-fill { display: flex; align-items: center; gap: 8px; }
.context-fill input { min-width: 0; flex: 1; }
.warning { color: var(--warn); font-size: 12px; }
.error { color: var(--danger); font-size: 13px; }
.muted { color: var(--ink-500); }
input:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
@media (max-width: 540px) { .price-fields { grid-template-columns: 1fr; } table { min-width: 620px; } .search { width: 100%; } }
</style>
