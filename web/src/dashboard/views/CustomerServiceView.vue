<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { customerServiceConfigApi, saveCustomerServiceConfigApi } from '../api'
import type { CustomerServiceConfig, CustomerServiceUpdate } from '../types'
import Card from '../../shared/components/ui/Card.vue'
import Field from '../../shared/components/ui/Field.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import { fmtTimeFull } from '../../shared/utils/format'
import { toastOk } from '../../shared/utils/toast'

const saved = ref<CustomerServiceConfig | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const conflict = ref(false)
const validation = ref<Record<string, string>>({})
let generation = 0
const form = reactive<CustomerServiceUpdate>({
  revision: 0,
  llm: { base_url: '', model: '', protocol: 'openai', max_output_tokens: 8192, api_key: '' },
  jev: { enabled: false, endpoint: 'https://api.typesafe.ai/v1/systemone', model: 'jev-latest', api_key: '' },
})
const busy = computed(() => loading.value || saving.value)
const canSave = computed(() => !!saved.value?.encryption_ready && !busy.value && !conflict.value)
const status = computed(() => saved.value?.jev.enabled ? 'JEV 已启用' : '主模型模式')
function apply(config: CustomerServiceConfig) {
  saved.value = config
  form.revision = config.revision
  form.llm = { base_url: config.llm.base_url, model: config.llm.model, protocol: config.llm.protocol, max_output_tokens: config.llm.max_output_tokens, api_key: '' }
  form.jev = { enabled: config.jev.enabled, endpoint: config.jev.endpoint, model: config.jev.model, api_key: '' }
}
async function load() {
  if (saving.value || (loading.value && saved.value)) return
  const current = ++generation
  loading.value = true
  error.value = ''
  try {
    const config = await customerServiceConfigApi()
    if (current !== generation) return
    apply(config)
    conflict.value = false
    validation.value = {}
  } catch (e: any) {
    if (current === generation) error.value = e?.message || '客服配置读取失败，请重试'
  } finally {
    if (current === generation) loading.value = false
  }
}
function parsedURL(value: string): URL | null {
  try {
    const url = new URL(value.trim())
    return ['http:', 'https:'].includes(url.protocol) && !!url.hostname && !url.username && !url.password && !url.search && !url.hash ? url : null
  } catch { return null }
}
function validate() {
  const errors: Record<string, string> = {}
  const llmURL = parsedURL(form.llm.base_url)
  const jevURL = parsedURL(form.jev.endpoint)
  if (!llmURL) errors.llm_url = '填写不含凭据、查询参数和片段的 HTTP(S) 地址'
  if (!form.llm.model.trim() || [...form.llm.model.trim()].length > 128) errors.llm_model = '填写 1 至 128 个字符的模型编号'
  if (!Number.isInteger(form.llm.max_output_tokens) || form.llm.max_output_tokens < 1 || form.llm.max_output_tokens > 65536) errors.max_tokens = '填写 1 至 65536 的整数'
  if (!form.llm.api_key.trim() && !saved.value?.llm.api_key_configured) errors.llm_key = '填写客服主模型密钥'
  if (llmURL && saved.value?.llm.api_key_configured && !form.llm.api_key.trim() && llmURL.origin !== parsedURL(saved.value.llm.base_url)?.origin) errors.llm_key = '服务地址已改变，请填写此地址使用的密钥'
  if (form.jev.endpoint.trim() && !jevURL) errors.jev_url = '填写不含凭据、查询参数和片段的 HTTP(S) 地址'
  if (form.jev.enabled && !jevURL) errors.jev_url = '填写 JEV 请求地址'
  if ((form.jev.enabled && !form.jev.model.trim()) || [...form.jev.model.trim()].length > 128) errors.jev_model = '填写 1 至 128 个字符的模型编号'
  if (form.jev.enabled && !form.jev.api_key.trim() && !saved.value?.jev.api_key_configured) errors.jev_key = '启用前请填写 JEV 密钥'
  if (jevURL && saved.value?.jev.api_key_configured && !form.jev.api_key.trim() && jevURL.origin !== parsedURL(saved.value.jev.endpoint)?.origin) errors.jev_key = '服务地址已改变，请填写此地址使用的密钥'
  validation.value = errors
  return Object.keys(errors).length === 0
}
async function save() {
  if (!canSave.value) return
  error.value = ''
  if (!validate()) { error.value = '请修正标出的配置后保存'; return }
  saving.value = true
  const current = ++generation
  try {
    const config = await saveCustomerServiceConfigApi({
      revision: form.revision,
      llm: { base_url: form.llm.base_url.trim(), model: form.llm.model.trim(), protocol: form.llm.protocol, max_output_tokens: form.llm.max_output_tokens, api_key: form.llm.api_key.trim() },
      jev: { enabled: form.jev.enabled, endpoint: form.jev.endpoint.trim(), model: form.jev.model.trim(), api_key: form.jev.api_key.trim() },
    })
    if (current !== generation) return
    apply(config)
    validation.value = {}
    toastOk('客服配置已保存，新请求使用更新后的配置')
  } catch (e: any) {
    if (current !== generation) return
    conflict.value = e?.status === 409
    error.value = conflict.value ? '配置已被其他管理员更新，当前输入已保留。读取最新配置将替换表单内容，并清空尚未保存的密钥。' : e?.message || '客服配置保存失败，请重试'
  } finally {
    if (current === generation) saving.value = false
  }
}
onMounted(() => load())
onBeforeUnmount(() => { generation++ })
</script>

<template>
  <div class="service-config" :aria-busy="busy">
    <header class="page-head">
      <p>配置客服主模型与 JEV 前置判断，保存后对新请求生效。</p>
      <BaseButton variant="ghost" :disabled="busy" title="用最新配置替换当前输入" aria-label="读取最新配置并替换当前输入" @click="load()">读取最新配置</BaseButton>
    </header>
    <div v-if="error" class="message error" role="alert">{{ error }}</div>
    <div v-if="loading && !saved" class="message" role="status">正在读取客服配置…</div>
    <div v-else-if="!saved" class="message"><BaseButton :disabled="loading" @click="load()">重新读取</BaseButton></div>
    <template v-if="saved">
      <div class="config-status">
        <Tag :tone="saved.jev.enabled ? 'ok' : 'muted'">{{ status }}</Tag>
        <span>{{ saved.configured ? `版本 ${saved.revision}` : '使用服务启动配置' }}</span>
        <span v-if="saved.configured && saved.updated_at">更新于 {{ fmtTimeFull(saved.updated_at) }}</span>
      </div>
      <p v-if="!saved.encryption_ready" class="message error" role="alert">服务端尚未配置密钥加密主密钥，请完成部署配置后再保存。</p>
      <form novalidate @submit.prevent="save">
        <fieldset :disabled="busy" class="config-fields">
          <Card title="客服主模型" sub="处理复杂请求、补充参数与多轮业务对话。">
            <Field label="服务地址" :error="validation.llm_url" required><input v-model="form.llm.base_url" type="url" aria-label="客服主模型服务地址" placeholder="https://api.example.com/v1" autocomplete="off" /></Field>
            <Field label="模型编号" :error="validation.llm_model" required><input v-model="form.llm.model" aria-label="客服主模型编号" autocomplete="off" /></Field>
            <Field label="API Key" :error="validation.llm_key" :hint="saved.llm.api_key_configured ? '已配置，留空保留当前密钥。填写新密钥后替换。' : '填写服务商提供的调用密钥。'" required><input v-model="form.llm.api_key" type="password" aria-label="客服主模型 API Key" autocomplete="new-password" :placeholder="saved.llm.api_key_configured ? '已配置，留空保留' : '输入调用密钥'" /></Field>
            <div class="field-row">
              <Field label="请求协议" required><select v-model="form.llm.protocol" aria-label="客服主模型请求协议"><option value="openai">OpenAI Chat Completions</option><option value="anthropic">Anthropic Messages</option></select></Field>
              <Field label="最大输出 token" :error="validation.max_tokens" required><input v-model.number="form.llm.max_output_tokens" type="number" min="1" max="65536" step="1" aria-label="客服主模型最大输出 token" /></Field>
            </div>
          </Card>
          <Card title="JEV 前置判断" sub="在主模型处理前判断查询类型、风险与业务范围。">
            <template #extra><label class="enable-jev"><input v-model="form.jev.enabled" type="checkbox" />启用前置判断</label></template>
            <Field label="请求地址" :error="validation.jev_url" :required="form.jev.enabled"><input v-model="form.jev.endpoint" type="url" aria-label="JEV 请求地址" placeholder="https://api.typesafe.ai/v1/systemone" autocomplete="off" /></Field>
            <Field label="模型编号" :error="validation.jev_model" :required="form.jev.enabled"><input v-model="form.jev.model" aria-label="JEV 模型编号" autocomplete="off" /></Field>
            <Field label="API Key" :error="validation.jev_key" :hint="saved.jev.api_key_configured ? '已配置，留空保留当前密钥。填写新密钥后替换。' : '启用 JEV 前请填写调用密钥。'" :required="form.jev.enabled"><input v-model="form.jev.api_key" type="password" aria-label="JEV API Key" autocomplete="new-password" :placeholder="saved.jev.api_key_configured ? '已配置，留空保留' : '输入调用密钥'" /></Field>
            <p class="policy">明确的简单查询直接读取业务结果。高置信度高风险请求、与业务无关的内容按现有规则拒绝。复杂或无法确定的请求交给客服主模型。</p>
          </Card>
        </fieldset>
        <footer class="save-bar"><p>新请求读取保存后的配置，进行中的请求使用原配置。</p><BaseButton type="submit" variant="primary" :loading="saving" :disabled="!canSave">保存客服配置</BaseButton></footer>
      </form>
    </template>
  </div>
</template>

<style scoped>
.service-config { display: grid; gap: var(--sp-5); min-width: 0; }
.page-head, .config-status, .save-bar { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-4); flex-wrap: wrap; }
.page-head p, .save-bar p { margin: 0; color: var(--ink-500); font-size: var(--fs-sm); line-height: 1.65; }
.config-status { justify-content: flex-start; color: var(--ink-500); font-size: var(--fs-xs); }
.config-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: stretch; gap: var(--sp-5); min-width: 0; border: 0; padding: 0; margin: 0; }
.field-row { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--sp-4); }
.enable-jev { display: flex; gap: var(--sp-2); align-items: center; min-height: 24px; font-size: var(--fs-xs); color: var(--ink-800); }
.policy { color: var(--ink-500); font-size: var(--fs-xs); line-height: 1.75; padding-top: var(--sp-3); border-top: 1px solid var(--hairline); }
.message { padding: var(--sp-4); border: 1px solid var(--hairline); border-radius: var(--r-sm); color: var(--ink-500); font-size: var(--fs-sm); line-height: 1.65; }
.error { color: var(--danger); border-color: var(--danger); }
.save-bar { margin-top: var(--sp-5); padding-top: var(--sp-4); border-top: 1px solid var(--hairline); }
.service-config :deep(:is(button, input, select):focus-visible) { outline: 2px solid var(--accent-500); outline-offset: 3px; }
@media (max-width: 1000px) { .config-fields { grid-template-columns: minmax(0, 1fr); } }
@media (max-width: 540px) { .field-row { grid-template-columns: minmax(0, 1fr); gap: 0; } .save-bar { align-items: stretch; flex-direction: column; } .save-bar :deep(button) { width: 100%; min-height: 44px; } }
</style>
