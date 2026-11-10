<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { IconPlus, IconEdit, IconTrash, IconServer, IconSearch, IconRefresh } from '@tabler/icons-vue'
import { channelsApi, upsertChannelApi, deleteChannelApi } from '../api'
import type { ProviderMargin, ChannelDiscoverRequest } from '../types'
import { money, fmt } from '../../shared/utils/format'
import { toastOk, toastErr } from '../../shared/utils/toast'
import Tag from '../../shared/components/ui/Tag.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import Modal from '../../shared/components/ui/Modal.vue'
import Field from '../../shared/components/ui/Field.vue'
import ConfirmModal from '../../shared/components/ui/ConfirmModal.vue'
import ProviderModelImport from '../components/ProviderModelImport.vue'

const items = ref<ProviderMargin[]>([])
const loading = ref(true)
const error = ref('')
const query = ref('')
const status = ref('all')
const visible = computed(() => items.value.filter(c => (status.value === 'all' || c.status === Number(status.value)) && `${c.name} ${c.base_url} ${c.models}`.toLowerCase().includes(query.value.toLowerCase().trim())))
const addOpen = ref(false)
const editing = ref(false)
const editingId = ref<number>()
const discoverTarget = ref<ChannelDiscoverRequest | null>(null)
const discoveringDraft = ref(false)
const busy = ref(false)
const deleting = ref<number | null>(null)
const deleteTarget = ref<ProviderMargin | null>(null)
const formError = ref('')
const defaults = () => ({ name: '', base_url: '', messages_base_url: '', responses_base_url: '', upstream_key: '', models: '', weight: 1, status: 0, cost_in_cent: 0, cost_out_cent: 0 })
const form = ref(defaults())
const statusTag = (s: number) => s === 0 ? { text: '启用', tone: 'ok' as const } : s === 1 ? { text: '停用', tone: 'muted' as const } : { text: '封禁', tone: 'danger' as const }
function modelNames(raw: string): string[] {
  try { const values = JSON.parse(raw); return Array.isArray(values) ? values.filter((v): v is string => typeof v === 'string') : [raw] }
  catch { return raw ? [raw] : [] }
}
async function load() {
  loading.value = true
  error.value = ''
  try { items.value = (await channelsApi()).items ?? [] }
  catch (e: any) { error.value = e?.message || '渠道加载失败，请重试' }
  finally { loading.value = false }
}
onMounted(load)
function openAdd() { editing.value = false; editingId.value = undefined; form.value = defaults(); formError.value = ''; addOpen.value = true }
function edit(c: ProviderMargin) {
  editing.value = true
  editingId.value = c.id
  form.value = { name: c.name, base_url: c.base_url, messages_base_url: c.messages_base_url || '', responses_base_url: c.responses_base_url || '', upstream_key: '', models: modelNames(c.models).join('\n'), weight: c.weight, status: c.status, cost_in_cent: c.cost_in_cent, cost_out_cent: c.cost_out_cent }
  formError.value = ''
  addOpen.value = true
}
function discoverDraft() {
  if (!form.value.base_url.trim() || (!editing.value && !form.value.upstream_key.trim())) {
    formError.value = '检测前请填写上游地址和 API key'
    return
  }
  discoveringDraft.value = true
  discoverTarget.value = { provider_id: editingId.value, name: form.value.name.trim() || undefined, base_url: form.value.base_url.trim(), upstream_key: form.value.upstream_key.trim() || undefined }
}
function discoverSaved(channel: ProviderMargin) {
  discoveringDraft.value = false
  discoverTarget.value = { provider_id: channel.id }
}
function applyDiscovered(ids: string[]) {
  const existing = form.value.models.trim().startsWith('[') ? modelNames(form.value.models) : form.value.models.split(/[,，\n]+/).map(id => id.trim()).filter(Boolean)
  form.value.models = [...new Set([...existing, ...ids])].join('\n')
  discoverTarget.value = null
  toastOk('模型编号已填入，保存渠道后可批量定价上架')
}
async function remove() {
  const c = deleteTarget.value
  if (!c || deleting.value !== null) return
  deleting.value = c.id
  try { await deleteChannelApi(c.id); toastOk('渠道已删除'); deleteTarget.value = null; await load() }
  catch (e: any) { toastErr(e?.message || '删除失败') }
  finally { deleting.value = null }
}
async function save() {
  if (busy.value) return
  formError.value = ''
  const value = form.value
  if (!value.name.trim() || !value.base_url.trim()) { formError.value = '请填写渠道名称和上游地址'; return }
  try {
    for (const address of [value.base_url, value.messages_base_url, value.responses_base_url].filter(address => address.trim())) {
      const url = new URL(address.trim())
      if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) throw new Error()
    }
  } catch { formError.value = '上游地址须为有效的 HTTP / HTTPS 地址，不含凭据、查询参数或片段'; return }
  let selected: unknown
  try {
    selected = value.models.trim().startsWith('[')
      ? JSON.parse(value.models)
      : value.models.split(/[,，\n]+/).map(id => id.trim()).filter(Boolean)
  } catch { formError.value = '请填写有效的模型编号'; return }
  if (!Array.isArray(selected) || !selected.length || !selected.every(m => typeof m === 'string' && m.trim() === m && m.length > 0) || new Set(selected).size !== selected.length) { formError.value = '至少填写一个模型编号，编号不能为空或重复'; return }
  if (!Number.isSafeInteger(value.weight) || value.weight < 1 || value.weight > 1000000 || ![0, 1, 2].includes(value.status) || ![value.cost_in_cent, value.cost_out_cent].every(n => Number.isSafeInteger(n) && n >= 0)) { formError.value = '权重须为正整数，成本须为非负整数'; return }
  if (!editing.value && !value.upstream_key.trim()) { formError.value = '新增渠道须填写上游 API key'; return }
  busy.value = true
  try {
    await upsertChannelApi({ ...value, name: value.name.trim(), base_url: value.base_url.trim(), messages_base_url: value.messages_base_url.trim(), responses_base_url: value.responses_base_url.trim(), models: JSON.stringify(selected), upstream_key: value.upstream_key.trim() || undefined })
    toastOk('渠道已保存'); addOpen.value = false; await load()
  } catch (e: any) { formError.value = e?.message || '保存失败，请重试' }
  finally { busy.value = false }
}
</script>

<template>
  <div class="channels">
    <div class="head">
      <div><h1>上游渠道</h1><p>{{ items.length }} 个渠道，{{ items.filter(c => c.status === 0).length }} 个启用</p></div>
      <BaseButton variant="primary" @click="openAdd"><IconPlus :size="16" />新增渠道</BaseButton>
    </div>
    <div class="toolbar">
      <label class="search"><IconSearch :size="17" /><input v-model="query" aria-label="搜索渠道" placeholder="搜索渠道、地址或模型" /></label>
      <select v-model="status" aria-label="筛选渠道状态"><option value="all">全部状态</option><option value="0">启用</option><option value="1">停用</option><option value="2">封禁</option></select>
      <BaseButton variant="ghost" :disabled="loading" @click="load"><IconRefresh :size="16" />刷新</BaseButton>
    </div>
    <div v-if="error" class="state error" role="alert">{{ error }}<BaseButton variant="ghost" @click="load">重新加载</BaseButton></div>
    <div v-else-if="loading" class="channel-grid" aria-label="正在加载渠道" aria-busy="true"><div v-for="n in 3" :key="n" class="channel-card skeleton placeholder" /></div>
    <div v-else-if="!visible.length" class="state">{{ items.length ? '没有匹配的渠道，请调整搜索条件' : '暂无上游渠道' }}<BaseButton v-if="!items.length" variant="primary" @click="openAdd">新增渠道</BaseButton></div>
    <div v-else class="channel-grid">
      <article v-for="c in visible" :key="c.id" class="channel-card">
        <header class="card-head">
          <div class="identity"><span class="provider-icon"><IconServer :size="20" /></span><div><h2 :title="c.name">{{ c.name }}</h2><span class="identifier">渠道 #{{ c.id }}</span></div></div>
          <div class="actions"><button :aria-label="`编辑渠道 ${c.name}`" title="编辑渠道" @click="edit(c)"><IconEdit :size="17" /></button><button class="delete" :disabled="deleting !== null" :aria-label="`删除渠道 ${c.name}`" title="删除渠道" @click="deleteTarget = c"><IconTrash :size="17" /></button></div>
        </header>
        <div class="status-row"><Tag :tone="statusTag(c.status).tone">{{ statusTag(c.status).text }}</Tag><span>权重 {{ c.weight }}</span><span :class="{ danger: c.consecutive_fail > 0 }">连续失败 {{ fmt(c.consecutive_fail) }}</span></div>
        <div class="endpoint"><span>上游地址</span><p class="mono" :title="c.base_url">{{ c.base_url }}</p></div>
        <div class="discover-action"><BaseButton size="sm" @click="discoverSaved(c)"><IconRefresh :size="14" />检测并发现模型</BaseButton></div>
        <div class="model-section"><span class="section-label">支持模型</span><div class="model-chips"><span v-for="m in modelNames(c.models)" :key="m" class="model-chip" :title="m">{{ m }}</span><span v-if="!modelNames(c.models).length" class="identifier">未配置模型</span></div></div>
        <dl class="prices"><div><dt>输入成本</dt><dd>{{ fmt(c.cost_in_cent) }} <small>分 / 百万 token</small></dd></div><div><dt>输出成本</dt><dd>{{ fmt(c.cost_out_cent) }} <small>分 / 百万 token</small></dd></div></dl>
        <footer class="financials"><div><span>累计销售</span><strong>{{ money(c.selling_cent) }}</strong></div><div><span>上游成本</span><strong>{{ money(c.cost_cent) }}</strong></div><div><span>毛利</span><strong :class="{ danger: c.margin_cent < 0 }">{{ money(c.margin_cent) }}</strong></div></footer>
      </article>
    </div>
    <Modal :open="addOpen" :title="editing ? '编辑渠道' : '新增渠道'" @close="!busy && (addOpen = false)" width="680" :close-disabled="busy">
      <form id="channel-form" @submit.prevent="save">
        <Field label="渠道名称" :hint="editing ? '渠道名称为固定标识' : undefined" required><input v-model="form.name" :disabled="editing || busy" placeholder="输入渠道名称" /></Field>
        <Field label="上游地址" required><input v-model="form.base_url" :disabled="busy" type="url" placeholder="https://api.example.com/v1" /></Field>
        <Field label="上游 API key" :hint="editing ? '留空保留现有密钥' : '密钥将安全保存'" :required="!editing"><input v-model="form.upstream_key" :disabled="busy" type="password" autocomplete="new-password" /></Field>
        <details class="advanced"><summary>协议地址（可选）</summary><Field label="Messages Base URL" hint="留空自动派生，GLM 自动映射"><input v-model="form.messages_base_url" :disabled="busy" type="url" placeholder="https://api.example.com/anthropic" /></Field><Field label="Responses Base URL" hint="留空自动派生"><input v-model="form.responses_base_url" :disabled="busy" type="url" placeholder="https://api.example.com/v1" /></Field></details>
        <div class="draft-discovery"><BaseButton :disabled="busy" @click.prevent="discoverDraft"><IconRefresh :size="16" />检测并发现模型</BaseButton><span>选择模型后自动填入编号</span></div>
        <Field label="支持模型" hint="每行一个模型编号" required><textarea v-model="form.models" :disabled="busy" rows="3" placeholder="输入模型编号" /></Field>
        <div class="two"><Field label="路由权重"><input v-model.number="form.weight" :disabled="busy" type="number" min="1" step="1" /></Field><Field label="状态"><select v-model.number="form.status" :disabled="busy"><option :value="0">启用</option><option :value="1">停用</option><option :value="2">封禁</option></select></Field></div>
        <div class="two"><Field label="输入成本（分 / 百万 token）"><input v-model.number="form.cost_in_cent" :disabled="busy" type="number" min="0" step="1" /></Field><Field label="输出成本（分 / 百万 token）"><input v-model.number="form.cost_out_cent" :disabled="busy" type="number" min="0" step="1" /></Field></div>
        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
      </form>
      <template #footer><BaseButton variant="ghost" :disabled="busy" @click="addOpen = false">取消</BaseButton><BaseButton variant="primary" :loading="busy" @click="save">保存渠道</BaseButton></template>
    </Modal>
    <ProviderModelImport :open="!!discoverTarget" :request="discoverTarget" :draft="discoveringDraft" @close="discoverTarget = null" @selected="applyDiscovered" @imported="load" />
    <ConfirmModal :open="!!deleteTarget" title="删除渠道" :text="`确认删除渠道「${deleteTarget?.name || ''}」？`" danger :loading="deleting !== null" ok-text="删除渠道" @close="deleting === null && (deleteTarget = null)" @confirm="remove" />
  </div>
</template>

<style scoped>
/* Hallmark custom reference-derived app components. Critique: real channel identity and accounting lead, no duplicated model gallery. */
.channels { display: flex; flex-direction: column; gap: 20px; }
.head, .toolbar, .card-head, .status-row { display: flex; align-items: center; gap: 12px; }
.head, .card-head { justify-content: space-between; }
.head h1 { font-size: 20px; font-weight: 600; color: var(--ink-900); }
.head p { margin-top: 4px; font-size: 13px; color: var(--ink-500); }
.toolbar { flex-wrap: wrap; }
.search { display: flex; align-items: center; gap: 8px; width: min(100%, 360px); padding: 0 12px; border: 1px solid var(--hairline-2); border-radius: 6px; background: var(--surface); color: var(--ink-400); }
.search input { width: 100%; min-width: 0; height: 40px; background: transparent; border: 0; color: var(--ink-800); }
.search:focus-within { outline: 2px solid var(--accent-500); outline-offset: 2px; }
.search input:focus { outline: none; }
.toolbar select { height: 42px; padding: 0 12px; border: 1px solid var(--hairline-2); border-radius: 6px; background: var(--surface); }
.channel-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr)); gap: 20px; }
.channel-card { min-width: 0; border: 1px solid var(--hairline-2); border-radius: 10px; background: var(--surface); overflow: hidden; box-shadow: var(--shadow-1); transition: box-shadow var(--t-fast); }
.channel-card:hover { box-shadow: var(--shadow-2, var(--shadow-1)); }
.card-head { padding: 20px 20px 16px; align-items: flex-start; }
.identity { display: flex; align-items: center; gap: 12px; min-width: 0; }
.identity > div { min-width: 0; }
.provider-icon { width: 40px; height: 40px; border: 1px solid var(--hairline); border-radius: 8px; background: var(--surface-2); display: grid; place-items: center; flex: none; color: var(--ink-700); }
.card-head h2 { font-size: 16px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--ink-900); }
.identifier { font-size: 12px; color: var(--ink-400); }
.actions { display: flex; flex: none; }
.actions button { width: 36px; height: 36px; border: 0; border-radius: 6px; background: transparent; color: var(--ink-500); display: grid; place-items: center; }
.actions button:hover { background: var(--surface-2); color: var(--ink-900); }
.actions button.delete:hover, .danger { color: var(--danger); }
.actions button:disabled { background: var(--surface-2); color: var(--ink-300); cursor: wait; }
.status-row { padding: 0 20px; font-size: 12px; color: var(--ink-500); flex-wrap: wrap; }
.endpoint { padding: 20px; }
.endpoint > span, .section-label, dt, .financials span { color: var(--ink-500); font-size: 12px; }
.endpoint p { margin-top: 8px; font-size: 12px; color: var(--ink-700); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.model-section { padding: 0 20px; min-height: 82px; }
.model-chips { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 8px; }
.model-chip { max-width: 100%; font-size: 12px; padding: 4px 8px; border-radius: 5px; border: 1px solid var(--hairline); background: var(--surface-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--ink-700); }
.prices { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; padding: 20px; }
.prices dd { margin-top: 8px; font-size: 14px; font-weight: 600; font-variant-numeric: tabular-nums; }
.prices small { font-size: 10px; color: var(--ink-400); font-weight: 400; }
.financials { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; padding: 16px 20px; border-top: 1px solid var(--hairline); background: var(--surface-2); }
.financials div { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
.financials strong { font-size: 15px; font-weight: 600; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.state { padding: 72px 20px; display: flex; flex-direction: column; align-items: center; gap: 16px; border: 1px dashed var(--hairline-2); color: var(--ink-500); border-radius: 10px; font-size: 14px; }
.error, .form-error { color: var(--danger); }
.form-error { font-size: 13px; }
.placeholder { height: 340px; }
#channel-form { padding-top: 4px; }
.advanced { margin-bottom: 16px; }
.advanced summary { cursor: pointer; font-size: 13px; margin-bottom: 12px; color: var(--ink-500); }
.discover-action { padding: 0 20px 16px; }
.draft-discovery { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-bottom: 16px; }
.draft-discovery span { font-size: 12px; color: var(--ink-500); }
.two { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
button:active:not(:disabled) { transform: translateY(1px); }
.head h1 { text-wrap: balance; }
button:focus-visible, select:focus-visible { outline: 2px solid var(--accent-500); outline-offset: 2px; }
@media (max-width: 540px) { .head { align-items: flex-start; } .two { grid-template-columns: 1fr; gap: 0; } .prices small { display: block; margin-top: 4px; } }
@media (prefers-reduced-motion: reduce) { .channel-card { transition: none; } }
</style>
