<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { accessApi, apiKeysApi, apiKeyStatusApi, createApiKeyApi, deleteApiKeyApi, modelsApi } from '../api'
import type { Model } from '../types'
import Card from '../../shared/components/ui/Card.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import Modal from '../../shared/components/ui/Modal.vue'
import { toastErr, toastOk } from '../../shared/utils/toast'

const openaiBase = ref('')
const claudeBase = ref('')
const models = ref<Model[]>([])
const keys = ref<{ id: number; name: string; key_prefix: string; status: number; created_at: string }[]>([])
const modelID = ref('')
const name = ref('默认调用')
const loading = ref(false)
const loadError = ref('')
const creating = ref(false)
const updatingID = ref<number | null>(null)
const freshKey = ref('')
const freshKeyID = ref<number | null>(null)
const deletingKey = ref<{ id: number; name: string; key_prefix: string } | null>(null)
const deleteError = ref('')
const keyAcknowledged = ref(false)
const canCreate = computed(() => !creating.value && updatingID.value === null && !deletingKey.value && !loading.value && !loadError.value && (!freshKey.value || keyAcknowledged.value))
const selectedModel = computed(() => models.value.find(m => m.model_id === modelID.value))
const openaiReady = computed(() => !loading.value && !loadError.value && !!openaiBase.value && !!selectedModel.value)
const claudeReady = computed(() => !loading.value && !loadError.value && !!claudeBase.value && !!selectedModel.value)
// Single-quote server-provided values safely for a POSIX shell.
function shellQuote(value: string) { return "'" + value.replace(/'/g, "'\\''") + "'" }
const requestBody = computed(() => JSON.stringify({ model: modelID.value, messages: [{ role: 'user', content: '你好' }] }, null, 2))
const openaiSample = computed(() => openaiReady.value ? `curl ${shellQuote(openaiBase.value + '/chat/completions')} \\\n  -H "Authorization: Bearer $GATEWAY_API_KEY" \\\n  -H 'Content-Type: application/json' \\\n  -d ${shellQuote(requestBody.value)}` : '')
const claudeSample = computed(() => claudeReady.value ? `curl ${shellQuote(claudeBase.value + '/v1/messages')} \\\n  -H "x-api-key: $GATEWAY_API_KEY" \\\n  -H 'anthropic-version: 2023-06-01' \\\n  -H 'Content-Type: application/json' \\\n  -d ${shellQuote(JSON.stringify({ model: modelID.value, max_tokens: 256, messages: [{ role: 'user', content: '你好' }] }, null, 2))}` : '')

async function load() {
  if (loading.value || creating.value || updatingID.value !== null) return
  loading.value = true
  loadError.value = ''
  try {
    const [access, listed, catalog] = await Promise.all([accessApi(), apiKeysApi(), modelsApi()])
    openaiBase.value = (access.openai_base_url || '').replace(/\/+$/, '')
    claudeBase.value = (access.claude_base_url || '').replace(/\/+$/, '')
    keys.value = listed.items || []
    models.value = (catalog.items || []).filter(m => m.status === 0 && !!m.model_id.trim())
    if (!models.value.some(m => m.model_id === modelID.value)) modelID.value = models.value[0]?.model_id || ''
  } catch (e: any) {
    loadError.value = e?.message || '接入信息加载失败，请重试'
  } finally { loading.value = false }
}
onMounted(load)

async function create() {
  if (!canCreate.value) return
  creating.value = true
  try {
    const res = await createApiKeyApi(name.value.trim() || '默认调用')
    freshKey.value = res.key
    freshKeyID.value = res.item.id
    keyAcknowledged.value = false
    toastOk('密钥已生成，请立即保存')
    // Keep the one-time secret visible even when refreshing the list fails.
    try { keys.value = (await apiKeysApi()).items } catch { toastErr('密钥已生成，列表刷新失败，请点击重新加载') }
  } catch (e: any) { toastErr(e?.message || '创建失败') }
  finally { creating.value = false }
}
async function setStatus(id: number, status: number) {
  if (updatingID.value !== null || creating.value || loading.value || deletingKey.value) return
  updatingID.value = id
  try {
    await apiKeyStatusApi(id, status)
    const key = keys.value.find(k => k.id === id)
    if (key) key.status = status
    toastOk(status === 0 ? '密钥已启用' : '密钥已停用')
  } catch (e: any) { toastErr(e?.message || '修改失败') }
  finally { updatingID.value = null }
}
function requestDelete(key: { id: number; name: string; key_prefix: string }) {
  if (updatingID.value !== null || creating.value || loading.value) return
  deletingKey.value = key
  deleteError.value = ''
}
async function removeKey() {
  const key = deletingKey.value
  if (!key || updatingID.value !== null) return
  updatingID.value = key.id
  deleteError.value = ''
  try {
    await deleteApiKeyApi(key.id)
    keys.value = keys.value.filter(item => item.id !== key.id)
    if (freshKeyID.value === key.id) {
      freshKey.value = ''
      freshKeyID.value = null
      keyAcknowledged.value = false
    }
    deletingKey.value = null
    toastOk('密钥已删除')
  } catch (e: any) { deleteError.value = e?.message || '删除失败，请重试' }
  finally { updatingID.value = null }
}
async function copy(text: string) {
  if (!text.trim()) return
  try { await navigator.clipboard.writeText(text); toastOk('已复制') }
  catch { toastErr('复制失败，请手动选择') }
}
</script>

<template>
  <div class="access" :aria-busy="loading">
    <header class="head">
      <div><h1 class="t">API 接入</h1><p class="s">选择可用模型，生成调用密钥，再把请求示例接入你的程序。</p></div>
      <BaseButton variant="ghost" :loading="loading" :disabled="creating || updatingID !== null" @click="load">重新加载</BaseButton>
    </header>
    <div v-if="loadError" class="notice error" role="alert">{{ loadError }}。请重新加载接入信息。</div>
    <div v-else-if="loading" class="notice" role="status">正在加载地址、模型与密钥…</div>

    <Card title="选择模型" sub="列表来自当前启用上游支持的模型目录。">
      <div class="model-field">
        <label for="access-model">请求模型</label>
        <select id="access-model" v-model="modelID" :disabled="loading || !!loadError || !models.length">
          <option v-if="!models.length" value="">暂无可用模型</option>
          <option v-for="m in models" :key="m.id" :value="m.model_id">{{ m.name }}（{{ m.model_id }}）</option>
        </select>
      </div>
      <p v-if="!loading && !loadError && !models.length" class="note">当前没有上游支持的可用模型，请联系管理员配置后重新加载。</p>
      <p v-else class="note">下方两种协议使用所选模型 ID。可用额度与模型价格以账户当前配置为准。</p>
    </Card>

    <div class="protocols">
      <Card title="OpenAI 兼容接口" sub="用于 OpenAI SDK 及支持自定义 Base URL 的客户端。">
        <dl class="endpoint"><dt>Base URL</dt><dd>{{ openaiBase || '等待接入地址' }}</dd><dt>请求路径</dt><dd>POST /chat/completions</dd><dt>鉴权</dt><dd>Authorization: Bearer &lt;调用密钥&gt;</dd></dl>
        <p class="note">将 SDK 的 base_url 设为上方地址。Pi 的 API 类型选择 <code>openai-completions</code>。</p>
        <pre v-if="openaiSample" class="sample" tabindex="0" aria-label="OpenAI 请求示例">{{ openaiSample }}</pre>
        <p v-else class="note">地址与模型加载完成后显示请求示例。</p>
        <BaseButton variant="secondary" :disabled="!openaiReady" @click="copy(openaiSample)">复制请求示例</BaseButton>
      </Card>
      <Card title="Anthropic 兼容接口" sub="用于 Anthropic SDK 及支持 Messages 协议的客户端。">
        <dl class="endpoint"><dt>Base URL</dt><dd>{{ claudeBase || '等待接入地址' }}</dd><dt>请求路径</dt><dd>POST /v1/messages</dd><dt>鉴权</dt><dd>x-api-key: &lt;调用密钥&gt;</dd></dl>
        <p class="note">将 SDK 的 base_url 设为上方地址，请求中传入 <code>max_tokens</code> 和 <code>anthropic-version</code> 请求头。</p>
        <pre v-if="claudeSample" class="sample" tabindex="0" aria-label="Anthropic 请求示例">{{ claudeSample }}</pre>
        <p v-else class="note">地址与模型加载完成后显示请求示例。</p>
        <BaseButton variant="secondary" :disabled="!claudeReady" @click="copy(claudeSample)">复制请求示例</BaseButton>
      </Card>
    </div>
    <p class="sample-help">示例从环境变量 <code>GATEWAY_API_KEY</code> 读取调用密钥。请先在本地设置该变量，再运行请求。</p>

    <Card title="调用密钥" sub="完整密钥仅在生成后展示，请保存到你的密钥管理工具。">
      <form class="row" @submit.prevent="create">
        <label class="name-field" for="access-key-name"><span>密钥名称</span><input id="access-key-name" v-model="name" class="name" maxlength="64" placeholder="例如，本地开发" :disabled="creating" /></label>
        <BaseButton type="submit" variant="primary" :loading="creating" :disabled="!canCreate || loading || !!loadError">生成密钥</BaseButton>
      </form>
      <div v-if="freshKey" class="fresh" role="status">
        <strong>请保存这枚新密钥</strong><p class="note">离开本页后无法再次查看全文。</p>
        <code class="key">{{ freshKey }}</code>
        <div class="fresh-actions"><BaseButton variant="secondary" @click="copy(freshKey)">复制密钥</BaseButton><label class="ack"><input v-model="keyAcknowledged" type="checkbox" />我已安全保存，可以生成下一枚</label></div>
      </div>
      <div v-if="!loading && !loadError && !keys.length" class="empty">还没有调用密钥。生成一枚后即可用于外部程序。</div>
      <div v-for="k in keys" :key="k.id" class="keyrow">
        <div class="key-info"><b>{{ k.name }}</b><code class="prefix">{{ k.key_prefix }}…</code><span class="state">{{ k.status === 0 ? '可用' : '已停用' }}</span></div>
        <div class="key-actions">
          <BaseButton variant="ghost" :loading="updatingID === k.id && !deletingKey" :disabled="creating || updatingID !== null || loading || !!loadError" :aria-label="`${k.status === 0 ? '停用' : '启用'}密钥 ${k.name}`" @click="setStatus(k.id, k.status === 0 ? 1 : 0)">{{ k.status === 0 ? '停用' : '启用' }}</BaseButton>
          <BaseButton variant="ghost" :disabled="creating || updatingID !== null || loading || !!loadError" :aria-label="`删除密钥 ${k.name}`" @click="requestDelete(k)">删除</BaseButton>
        </div>
      </div>
    </Card>
    <Modal :open="!!deletingKey" title="删除调用密钥" :close-disabled="updatingID !== null" @close="deletingKey = null">
      <div v-if="deletingKey" class="delete-content">
        <strong>{{ deletingKey.name }}</strong>
        <code>{{ deletingKey.key_prefix }}…</code>
        <p>删除后，这枚密钥将立即失效且无法恢复。使用它的程序需要更换密钥。</p>
        <p v-if="deleteError" class="error" role="alert">{{ deleteError }}</p>
      </div>
      <template #footer>
        <BaseButton :disabled="updatingID !== null" @click="deletingKey = null">取消</BaseButton>
        <BaseButton variant="primary" :loading="updatingID !== null" @click="removeKey">确认删除</BaseButton>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.access { display: flex; flex-direction: column; gap: var(--sp-5); min-width: 0; }
.head { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-4); }
.t { font-size: var(--fs-xl); font-weight: 650; letter-spacing: -.026em; }
.s, .sample-help { font-size: var(--fs-sm); color: var(--ink-500); margin-top: 4px; line-height: 1.6; }
.protocols { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--sp-5); }
.protocols > * { min-width: 0; }
.notice { padding: var(--sp-4); border: 1px solid var(--hairline); border-radius: var(--r-sm); color: var(--ink-500); font-size: var(--fs-sm); }
.error { color: var(--danger); background: var(--danger-bg); }
.model-field, .row { display: flex; gap: var(--sp-4); align-items: center; }
.model-field > label { flex: none; font-size: var(--fs-sm); color: var(--ink-600); }
.model-field select { min-width: 0; width: min(100%, 520px); }
.model-field select, .name { height: 40px; padding: 0 var(--sp-3); border: 1px solid var(--field-border); border-radius: var(--r-sm); background: var(--surface); color: var(--ink-800); }
.endpoint { display: grid; grid-template-columns: 72px minmax(0, 1fr); gap: 12px; font-size: var(--fs-xs); line-height: 1.6; }
.endpoint dt { color: var(--ink-500); }
.endpoint dd { margin: 0; overflow-wrap: anywhere; font-family: var(--font-mono); }
.note { margin: var(--sp-3) 0; font-size: var(--fs-sm); color: var(--ink-500); line-height: 1.65; }
.sample { margin: var(--sp-4) 0; padding: var(--sp-4); background: var(--surface-2); color: var(--ink-800); border: 1px solid var(--hairline); border-radius: var(--r-sm); font-family: var(--font-mono); font-size: var(--fs-xs); line-height: 1.75; white-space: pre-wrap; overflow-wrap: anywhere; max-height: 380px; overflow: auto; }
.row { align-items: flex-end; }
.name-field { display: flex; flex-direction: column; gap: 8px; flex: 1; min-width: 0; font-size: var(--fs-sm); }
.name { width: 100%; }
.fresh { margin-top: var(--sp-4); padding: var(--sp-4); background: var(--surface-2); border: 1px solid var(--hairline); border-radius: var(--r-sm); font-size: var(--fs-sm); }
.key { display: block; margin-bottom: var(--sp-4); overflow-wrap: anywhere; user-select: all; }
.fresh-actions { display: flex; align-items: center; gap: var(--sp-4); flex-wrap: wrap; }
.ack { display: flex; align-items: center; gap: 8px; color: var(--ink-500); font-size: var(--fs-xs); }
.empty { margin-top: var(--sp-4); color: var(--ink-500); font-size: var(--fs-sm); }
.keyrow { margin-top: var(--sp-3); display: flex; justify-content: space-between; align-items: center; gap: var(--sp-3); padding-top: var(--sp-3); border-top: 1px solid var(--hairline); }
.key-actions { display: flex; align-items: center; gap: var(--sp-2); flex-shrink: 0; }
.delete-content { display: grid; gap: var(--sp-3); font-size: var(--fs-sm); line-height: 1.65; }
.delete-content code { color: var(--ink-500); }
.key-info { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; min-width: 0; overflow-wrap: anywhere; font-size: var(--fs-sm); }
.prefix, .state { color: var(--ink-500); font-size: var(--fs-xs); }
input:focus-visible, select:focus-visible, pre:focus-visible { outline: 2px solid var(--accent-500); outline-offset: 3px; }
@media (max-width: 1000px) { .protocols { grid-template-columns: minmax(0, 1fr); } }
@media (max-width: 540px) { .head { align-items: flex-start; } .row, .model-field { align-items: stretch; flex-direction: column; } .model-field select { width: 100%; } .row :deep(button) { min-height: 44px; } .ack { min-height: 44px; } }
</style>
