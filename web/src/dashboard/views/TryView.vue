<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { IconSend } from '@tabler/icons-vue'
import { modelsApi } from '../api'
import type { Model } from '../types'
import { DEMO_TENANT_KEY } from '../types'
import { money, zhCount } from '../../shared/utils/format'
import { toastErr } from '../../shared/utils/toast'
import Card from '../../shared/components/ui/Card.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'

const models = ref<Model[]>([])
const model = ref('')
const key = ref(DEMO_TENANT_KEY)
const prompt = ref('你好，请介绍一下自己')
const stream = ref(false)
const sending = ref(false)
const out = ref('')
const usage = ref<{ input: number; output: number; cost: number; elapsed: number } | null>(null)

onMounted(async () => {
  try {
    models.value = (await modelsApi()).items
    if (models.value.length) model.value = models.value[0].model_id
  } catch { /* ignore */ }
})

async function send() {
  if (!model.value) { toastErr('请先选择模型'); return }
  if (!key.value.trim()) { toastErr('请输入租户 key'); return }
  sending.value = true
  out.value = ''
  usage.value = null
  const tm = Date.now()
  try {
    const res = await fetch('/v1/chat/completions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + key.value.trim() },
      body: JSON.stringify({ model: model.value, messages: [{ role: 'user', content: prompt.value }], stream: stream.value }),
    })
    if (!res.ok) {
      const d = await res.json().catch(() => null)
      toastErr(d?.error?.message || `请求失败（${res.status}）`)
      return
    }
    if (!stream.value) {
      const d = await res.json()
      out.value = d.choices?.[0]?.message?.content ?? JSON.stringify(d, null, 2)
      setUsage(d.usage, Date.now() - tm)
    } else {
      const reader = res.body!.getReader()
      const dec = new TextDecoder()
      let buf = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buf += dec.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop() || ''
        for (const line of lines) {
          const t = line.trim()
          if (!t.startsWith('data:')) continue
          const payload = t.slice(5).trim()
          if (payload === '[DONE]') continue
          try {
            const j = JSON.parse(payload)
            const piece = j.choices?.[0]?.delta?.content
            if (piece) out.value += piece
            if (j.usage) setUsage(j.usage, Date.now() - tm)
          } catch { /* ignore */ }
        }
      }
    }
  } catch (e: any) {
    toastErr(e?.message || '请求失败')
  } finally { sending.value = false }
}

function setUsage(u: any, elapsed: number) {
  const m = models.value.find(x => x.model_id === model.value)
  const cost = m ? ((u.prompt_tokens ?? 0) * m.input_price_cent + (u.completion_tokens ?? 0) * m.output_price_cent) / 1e6 : 0
  usage.value = { input: u.prompt_tokens ?? 0, output: u.completion_tokens ?? 0, cost, elapsed }
}
</script>

<template>
  <div class="try">
    <Card>
      <div class="cfg">
        <label class="cfgitem"><span class="clabel">模型</span>
          <select v-model="model" class="csel">
            <option v-for="m in models" :key="m.id" :value="m.model_id">{{ m.name }}（{{ m.model_id }}）</option>
          </select>
        </label>
        <label class="cfgitem"><span class="clabel">租户 key</span>
          <input v-model="key" class="kin mono" placeholder="sk-..." />
        </label>
        <label class="cfgitem chk"><input v-model="stream" type="checkbox" /> 流式 SSE</label>
      </div>

      <div class="msgrow">
        <textarea v-model="prompt" class="pinput" rows="4" placeholder="输入内容（v1/chat/completions，走真实计费）" @keydown.enter.exact.prevent="send" />
        <BaseButton variant="primary" size="lg" :loading="sending" @click="send"><IconSend :size="16" /> 发送</BaseButton>
      </div>

      <div class="outwrap">
        <pre class="output" :class="{ dim: !out }">{{ out || '输出将显示在这里…' }}</pre>
      </div>

      <div v-if="usage" class="usagenum num">
        输入 {{ zhCount(usage.input) }} · 输出 {{ zhCount(usage.output) }} token · 计费 <b>{{ money(usage.cost) }}</b> · 耗时 {{ (usage.elapsed / 1000).toFixed(2) }}s
      </div>
    </Card>
  </div>
</template>

<style scoped>
.cfg {
  display: flex; align-items: center; gap: var(--sp-5);
  margin-bottom: var(--sp-4); flex-wrap: wrap;
}
.cfgitem {
  display: flex; align-items: center; gap: var(--sp-2);
  font-size: var(--fs-sm); color: var(--ink-500);
}
.clabel { font-weight: 600; color: var(--ink-600); }
.csel, .kin {
  height: 34px; padding: 0 var(--sp-3);
  border: none;
  border-radius: var(--r-sm);
  background: var(--paper);
  color: var(--ink-700);
  font-size: var(--fs-sm);
  transition: border-color var(--t-fast);
}
.csel { min-width: 230px; }
.kin { min-width: 240px; font-family: var(--font-mono); font-size: var(--fs-xs); }
.csel:hover, .kin:hover { border-color: var(--line-strong); }
.csel:focus, .kin:focus { outline: none; border-color: var(--accent-500); box-shadow: 0 0 0 3px var(--accent-50); }
.chk { gap: 6px; cursor: pointer; user-select: none; }

.msgrow { display: flex; gap: var(--sp-3); align-items: flex-end; }
.pinput {
  flex: 1;
  border: none;
  border-radius: var(--r-sm);
  padding: 10px var(--sp-3);
  resize: vertical;
  min-height: 84px;
  font-size: var(--fs-sm);
  line-height: 1.6;
  transition: border-color var(--t-fast), box-shadow var(--t-fast);
}
.pinput:hover { border-color: var(--line-strong); }
.pinput:focus {
  outline: none;
  border-color: var(--accent-500);
  box-shadow: 0 0 0 3px var(--accent-50);
}

.outwrap {
  margin-top: var(--sp-4);
  background: var(--ink-950);
  border-radius: var(--r-sm);
  padding: var(--sp-4);
  max-height: 340px;
  overflow: auto;
}
.output {
  margin: 0;
  color: #cfe0d8;
  font-family: var(--font-mono);
  font-size: var(--fs-xs);
  line-height: 1.72;
  white-space: pre-wrap;
  word-break: break-word;
}
.output.dim { color: rgb(255 255 255 / .28); }

.usagenum {
  margin-top: var(--sp-4);
  padding-top: var(--sp-4);
  border-top: 1px solid var(--hairline);
  font-size: var(--fs-xs);
  color: var(--ink-600);
}
.usagenum b { color: var(--accent-700); font-weight: 650; }
</style>