<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from "vue";
import { modelsApi } from "../api";
import type { Model } from "../types";
import { money } from "../../shared/utils/format";
import { toastErr } from "../../shared/utils/toast";
import BaseButton from "../../shared/components/ui/BaseButton.vue";
interface Message {
  role: "user" | "assistant";
  content: string;
  complete: boolean;
}
const models = ref<Model[]>([]);
const model = ref("");
const tenantKey = ref("");
const showKey = ref(false);
const settingsOpen = ref(true);
const prompt = ref("");
const messages = ref<Message[]>([]);
const stream = ref(true);
const loading = ref(true);
const sending = ref(false);
const error = ref("");
const elapsed = ref(0);
const inspectTab = ref<"conversation" | "request" | "response">("conversation");
const lastRequest = ref("");
const responseBody = ref("");
const responseTruncated = ref(false);
const responseMeta = ref<{ status: number; contentType: string } | null>(null);
const copied = ref("");
const previewLimit = 65536;
const streamFrameLimit = 1024 * 1024;
const requestJson = computed(() => lastRequest.value || JSON.stringify({
  model: model.value,
  messages: [
    ...messages.value.filter((message) => message.complete).map(({ role, content }) => ({ role, content })),
    ...(prompt.value.trim() ? [{ role: "user", content: prompt.value.trim() }] : []),
  ],
  stream: stream.value,
  ...(stream.value ? { stream_options: { include_usage: true } } : {}),
}, null, 2));
const requestPreview = computed(() => redactSecret(requestJson.value).slice(0, previewLimit));
const latestOutput = computed(() => [...messages.value].reverse().find((message) => message.role === "assistant")?.content || "");
let copiedTimer: ReturnType<typeof setTimeout> | undefined;
function redactSecret(value: string) {
  const secret = tenantKey.value.trim();
  return secret ? value.split(secret).join("[REDACTED]") : value;
}
function appendResponse(value: string) {
  const safeValue = redactSecret(value);
  const remaining = previewLimit - responseBody.value.length;
  if (safeValue.length > remaining) responseTruncated.value = true;
  if (remaining > 0) responseBody.value += safeValue.slice(0, remaining);
}
async function copyDebug(kind: "request" | "output") {
  const value = kind === "request" ? requestJson.value : inspectTab.value === "response" ? responseBody.value : latestOutput.value;
  if (!value) return;
  try {
    await navigator.clipboard.writeText(redactSecret(value));
    copied.value = kind;
    clearTimeout(copiedTimer);
    copiedTimer = setTimeout(() => { copied.value = ""; }, 1800);
  } catch {
    toastErr("复制失败，请检查浏览器剪贴板权限");
  }
}
function onTabKeydown(event: KeyboardEvent) {
  const tabs = ["conversation", "request", "response"] as const;
  const index = tabs.indexOf(inspectTab.value);
  let next = index;
  if (event.key === "ArrowRight") next = (index + 1) % tabs.length;
  else if (event.key === "ArrowLeft") next = (index + tabs.length - 1) % tabs.length;
  else if (event.key === "Home") next = 0;
  else if (event.key === "End") next = tabs.length - 1;
  else return;
  event.preventDefault();
  inspectTab.value = tabs[next]!;
  void nextTick(() => document.getElementById(`debug-tab-${inspectTab.value}`)?.focus());
}
const usage = ref<{ input: number; output: number; cost: number } | null>(null);
const modelError = ref("");
const messagesPanel = ref<HTMLElement>();
const followOutput = ref(true);
const selected = computed(() =>
  models.value.find((m) => m.model_id === model.value),
);
let controller: AbortController | undefined;
async function load() {
  if (loading.value && models.value.length) return;
  loading.value = true;
  modelError.value = "";
  try {
    models.value = (await modelsApi()).items.filter((item) => item.status === 0);
    model.value = models.value.find((item) => item.model_id === model.value)?.model_id || models.value[0]?.model_id || "";
  } catch (e: any) {
    modelError.value = e?.message || "读取模型目录失败，请重试";
  } finally {
    loading.value = false;
  }
}
onMounted(load);
onBeforeUnmount(() => {
  controller?.abort();
  clearTimeout(copiedTimer);
  tenantKey.value = "";
});
function clear() {
  if (!sending.value) {
    messages.value = [];
    lastRequest.value = "";
    responseBody.value = "";
    responseTruncated.value = false;
    responseMeta.value = null;
    copied.value = "";
    usage.value = null;
    elapsed.value = 0;
    error.value = "";
  }
}
function onPromptKeydown(event: KeyboardEvent) {
  if (event.key === "Enter" && (event.ctrlKey || event.metaKey) && !event.isComposing && event.keyCode !== 229) {
    event.preventDefault();
    void send();
  }
}
function onMessagesScroll() {
  const panel = messagesPanel.value;
  if (panel) followOutput.value = panel.scrollHeight - panel.scrollTop - panel.clientHeight < 80;
}
watch(() => messages.value.map((message) => message.content).join(""), async () => {
  await nextTick();
  if (followOutput.value && messagesPanel.value) messagesPanel.value.scrollTop = messagesPanel.value.scrollHeight;
}, { flush: "post" });
function setUsage(value: any, price: Model) {
  const input = value.prompt_tokens ?? 0;
  const output = value.completion_tokens ?? 0;
  usage.value = {
    input,
    output,
    cost:
      (input * price.input_price_cent + output * price.output_price_cent) /
      1000000,
  };
}
async function send() {
  const price = selected.value;
  const text = prompt.value.trim();
  if (sending.value || loading.value || !price || !text) return;
  if (text.length > 32000) {
    error.value = "单条消息最多 32,000 字符，请缩短后发送";
    return;
  }
  const requestKey = tenantKey.value.trim();
  if (!requestKey) {
    error.value = "请输入用于调试的租户 API Key";
    return;
  }
  const useStream = stream.value;
  followOutput.value = true;
  const history = messages.value
    .filter((m) => m.complete)
    .map(({ role, content }) => ({ role, content }));
  const user: Message = { role: "user", content: text, complete: false };
  const answer: Message = { role: "assistant", content: "", complete: false };
  messages.value.push(user, answer);
  const userIndex = messages.value.length - 2;
  const answerIndex = messages.value.length - 1;
  sending.value = true;
  error.value = "";
  usage.value = null;
  responseBody.value = "";
  responseTruncated.value = false;
  responseMeta.value = null;
  elapsed.value = 0;
  copied.value = "";
  const requestBody = {
    model: price.model_id,
    messages: [...history, { role: "user", content: text }],
    stream: useStream,
    ...(useStream ? { stream_options: { include_usage: true } } : {}),
  };
  lastRequest.value = JSON.stringify(requestBody, null, 2);
  controller = new AbortController();
  const startedAt = Date.now();
  let stopped = false;
  try {
    const response = await fetch("/v1/chat/completions", {
      method: "POST",
      signal: controller.signal,
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${requestKey}` },
      body: JSON.stringify(requestBody),
    });
    responseMeta.value = { status: response.status, contentType: response.headers.get("content-type") || "未提供" };
    if (!response.ok) {
      const body = await response.text();
      appendResponse(body);
      let data;
      try { data = JSON.parse(body); } catch { /* HTTP status remains available for non-JSON errors. */ }
      throw new Error(
        data?.error?.message || data?.error || `请求失败（${response.status}）`,
      );
    }
    if (useStream) {
      if (!response.body) throw new Error("未收到模型响应流");
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let doneMarker = false;
      let streamEnded = false;
      function consume(event: string) {
        if (event.length > streamFrameLimit) throw new Error("单个响应帧超过 1 MiB，已停止读取");
        const payload = event.split("\n")
          .filter((line) => line.startsWith("data:"))
          .map((line) => line.slice(5).replace(/^ /, ""))
          .join("\n").trim();
        if (!payload) return;
        if (payload === "[DONE]") {
          doneMarker = true;
          streamEnded = true;
          return;
        }
        let data;
        try {
          data = JSON.parse(payload);
        } catch {
          appendResponse(payload + "\n\n");
          throw new Error("响应帧不是有效 JSON，已停止读取");
        }
        appendResponse(JSON.stringify(data, null, 2) + "\n\n");
        if (data.error) throw new Error(data.error.message || "上游响应失败");
        const piece = data.choices?.[0]?.delta?.content;
        if (typeof piece === "string") messages.value[answerIndex]!.content += piece;
        if (data.choices?.[0]?.finish_reason) doneMarker = true;
        if (data.usage) setUsage(data.usage, price!);
      }
      try {
        for (;;) {
          const { done, value } = await reader.read();
          buffer += done
            ? decoder.decode()
            : decoder.decode(value, { stream: true });
          buffer = buffer.replace(/\r\n/g, "\n");
          let boundary = buffer.indexOf("\n\n");
          while (boundary !== -1 && !streamEnded) {
            consume(buffer.slice(0, boundary));
            buffer = buffer.slice(boundary + 2);
            boundary = buffer.indexOf("\n\n");
          }
          if (streamEnded) break;
          if (buffer.length > streamFrameLimit) throw new Error("单个响应帧超过 1 MiB，已停止读取");
          if (done) {
            if (buffer.trim()) consume(buffer);
            break;
          }
        }
        if (!doneMarker) throw new Error("模型响应中断，请检查后重新发送");
      } finally {
        await reader.cancel().catch(() => {});
        reader.releaseLock();
      }
    } else {
      const body = await response.text();
      appendResponse(body);
      const data = JSON.parse(body);
      const content = data.choices?.[0]?.message?.content;
      if (typeof content !== "string")
        throw new Error("模型未返回有效文本回复");
      messages.value[answerIndex]!.content = content;
      if (data.usage) setUsage(data.usage, price!);
    }
    if (!messages.value[answerIndex]!.content)
      throw new Error("模型返回空回复，可调整问题后重新发送");
    messages.value[userIndex]!.complete = true;
    messages.value[answerIndex]!.complete = true;
    prompt.value = "";
  } catch (e: any) {
    stopped = e.name === "AbortError";
    error.value = stopped
      ? "已停止生成，部分回复保留供查看。发送将重新提交当前问题。"
      : e.message || "请求失败";
    if (!stopped) toastErr(error.value);
  } finally {
    elapsed.value = Date.now() - startedAt;
    sending.value = false;
    controller = undefined;
  }
}
</script>
<template>
  <div class="workbench">
    <div class="workbench-title">
      <div>
        <h1>接口调试</h1>
        <p>通过租户 API Key 验证兼容接口，请求计入该租户实际用量。</p>
      </div>
      <BaseButton :disabled="sending || !messages.length" @click="clear"
        >新建对话</BaseButton
      >
    </div>
    <div class="workbench-layout">
      <aside id="debug-settings" class="model-controls" :class="{ 'settings-collapsed': !settingsOpen }" aria-label="请求设置">
        <div class="settings-heading">
          <div><h2>请求设置</h2><span v-if="!settingsOpen" class="collapsed-model">{{ selected?.name || '未选择模型' }}</span></div>
          <button type="button" class="settings-toggle" :aria-expanded="settingsOpen" aria-controls="debug-settings" @click="settingsOpen = !settingsOpen">{{ settingsOpen ? '收起设置' : '展开设置' }}</button>
        </div>
        <label for="workbench-model">请求模型</label
        ><select
          id="workbench-model"
          v-model="model"
          :disabled="sending || loading"
          @change="clear"
        >
          <option value="" disabled>{{ loading ? "正在加载模型…" : "选择可用模型" }}</option>
          <option v-for="m in models" :key="m.id" :value="m.model_id">
            {{ m.name }}
          </option>
        </select>
        <p v-if="modelError" class="request-error" role="alert">{{ modelError }}</p>
        <p v-else-if="!loading && !models.length" role="status">暂无已上架模型，请联系管理员配置模型目录。</p>
        <template v-if="selected"
          ><code>{{ selected.model_id }}</code>
          <dl>
            <dt>输入 / 百万 token</dt>
            <dd>{{ money(selected.input_price_cent) }}</dd>
            <dt>输出 / 百万 token</dt>
            <dd>{{ money(selected.output_price_cent) }}</dd>
            <dt>上下文长度</dt>
            <dd>{{ selected.context_len.toLocaleString() }} token</dd>
          </dl></template
        >
        <div class="key-field">
          <label for="debug-tenant-key">租户 API Key</label>
          <div class="key-input">
            <input id="debug-tenant-key" v-model="tenantKey" :type="showKey ? 'text' : 'password'" :disabled="sending" placeholder="输入 sk-…" autocomplete="off" autocapitalize="off" spellcheck="false" @input="clear" />
            <button type="button" :disabled="sending" :aria-pressed="showKey" :aria-label="showKey ? '隐藏 API Key' : '显示 API Key'" @click="showKey = !showKey">{{ showKey ? '隐藏' : '显示' }}</button>
          </div>
          <small>仅保留在当前页面内存中。</small>
        </div>
        <label class="stream-setting"
          ><input
            v-model="stream"
            type="checkbox"
            :disabled="sending"
          />流式显示回复</label
        >
        <p>切换模型或 API Key 会清空对话。调用使用租户权限与额度。</p>
        <code class="endpoint">POST /v1/chat/completions</code>
        <BaseButton v-if="modelError || (!models.length && !loading)" :loading="loading" @click="load">重新加载模型</BaseButton>
      </aside>
      <section class="conversation" aria-label="请求调试">
        <div class="inspect-toolbar">
          <div class="inspect-tabs" role="tablist" aria-label="调试视图" @keydown="onTabKeydown">
            <button v-for="tab in ([{ id: 'conversation', label: '对话' }, { id: 'request', label: '请求 JSON' }, { id: 'response', label: '原始响应' }] as const)"
              :id="`debug-tab-${tab.id}`" :key="tab.id" type="button" role="tab"
              :aria-selected="inspectTab === tab.id" :aria-controls="`debug-panel-${tab.id}`"
              :tabindex="inspectTab === tab.id ? 0 : -1" @click="inspectTab = tab.id">{{ tab.label }}</button>
          </div>
          <button class="copy-debug" type="button" :disabled="inspectTab !== 'request' && !(inspectTab === 'response' ? responseBody : latestOutput)" @click="copyDebug(inspectTab === 'request' ? 'request' : 'output')">{{ copied === (inspectTab === 'request' ? 'request' : 'output') ? '已复制' : inspectTab === 'request' ? '复制请求' : '复制输出' }}</button>
        </div>
        <div class="response-meta" role="status">
          <span v-if="responseMeta" :class="{ 'http-error': responseMeta.status >= 400 }">HTTP {{ responseMeta.status }}</span>
          <span v-else>{{ sending ? '等待响应' : '尚未发送' }}</span>
          <code v-if="responseMeta" :title="responseMeta.contentType">{{ responseMeta.contentType }}</code>
          <span class="request-time">{{ sending ? '请求中…' : elapsed ? (elapsed / 1000).toFixed(2) + 's' : '—' }}</span>
        </div>
        <div
          v-show="inspectTab === 'conversation'"
          id="debug-panel-conversation"
          role="tabpanel"
          aria-labelledby="debug-tab-conversation"
          tabindex="0"
          ref="messagesPanel"
          class="messages"
          @scroll="onMessagesScroll"
          aria-live="polite"
          :aria-busy="sending"
        >
          <div v-if="!messages.length" class="conversation-empty">
            <h2>验证一次完整请求</h2>
            <p>选择模型并填写租户 API Key。支持流式响应与连续对话。</p>

            <button
              v-for="text in [
                '帮我解释一段代码',
                '为一个新项目梳理思路',
                '将这段文字翻译为英文',
              ]"
              :key="text"
              :disabled="loading || !selected"
              @click="prompt = text"
            >
              {{ text }}
            </button>
          </div>
          <article
            v-for="(m, i) in messages"
            :key="i"
            class="message"
            :class="m.role"
          >
            <span
              >{{ m.role === "user" ? "你" : selected?.name || "模型"
              }}<small v-if="!m.complete">{{
                sending ? (m.role === "user" ? " 已发送" : " 生成中") : " 未完成"
              }}</small></span
            >
            <p>
              {{ m.content || (sending ? "正在等待回复…" : "本次未收到回复") }}
            </p>
          </article>
        </div>
        <div v-show="inspectTab === 'request'" id="debug-panel-request" class="inspect-panel" role="tabpanel" aria-labelledby="debug-tab-request" tabindex="0">
          <pre>{{ requestPreview }}</pre>
          <p v-if="requestJson.length > previewLimit" class="preview-note">预览已截断，复制可获取完整请求。</p>
        </div>
        <div v-show="inspectTab === 'response'" id="debug-panel-response" class="inspect-panel" role="tabpanel" aria-labelledby="debug-tab-response" tabindex="0">
          <pre :class="{ 'empty-response': !responseBody }">{{ responseBody || (sending ? '等待响应…' : '发送请求后查看响应内容') }}</pre>
          <p v-if="responseTruncated" class="preview-note">响应预览已达到 65,536 字符上限。</p>
        </div>
        <form class="composer" @submit.prevent="send">
          <p v-if="error" class="request-error" role="alert">{{ error }}</p>
          <textarea
            v-model="prompt"
            aria-label="发送给模型的消息"
            placeholder="输入消息，Enter 换行，Ctrl / ⌘ + Enter 发送"
            :disabled="sending"
            maxlength="32000"
            @keydown="onPromptKeydown"
          />
          <div class="composer-footer">
            <span v-if="usage"
              >输入 {{ usage.input }} / 输出 {{ usage.output }} token，估算
              {{ money(usage.cost) }} · {{ (elapsed / 1000).toFixed(2) }}s</span
            ><span v-else>计费结果以用量明细为准</span
            ><BaseButton
              v-if="sending"
              type="button"
              @click="controller?.abort()"
              >停止生成</BaseButton
            ><BaseButton
              v-else
              type="submit"
              variant="primary"
              :disabled="!selected || !tenantKey.trim() || !prompt.trim() || loading"
              >发送消息</BaseButton
            >
          </div>
        </form>
      </section>
    </div>
  </div>
</template>
<style scoped>
.workbench { display: grid; gap: 24px; min-width: 0; }
.workbench-title { display: flex; justify-content: space-between; align-items: center; gap: 16px; }
.workbench-title h1 { font-size: 21px; letter-spacing: -.04em; font-weight: 600; }
.workbench-title p { font-size: 13px; color: var(--ink-500); margin-top: 8px; line-height: 1.6; }
.workbench-layout { display: grid; grid-template-columns: 264px minmax(0, 1fr); border: 1px solid var(--hairline); border-radius: 10px; overflow: hidden; background: var(--surface); min-height: 640px; }
.model-controls { padding: 24px; background: var(--paper-2); border-right: 1px solid var(--hairline); display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.settings-heading { margin-bottom: 12px; min-width: 0; }
.model-controls h2 { font-size: 14px; font-weight: 600; }
.settings-toggle, .collapsed-model { display: none; }
.model-controls label { font-size: 12px; color: var(--ink-600); }
.model-controls select { width: 100%; min-height: 44px; padding: 0 12px; border: 1px solid var(--field-border); border-radius: 6px; background: var(--surface); color: var(--ink-900); }
.model-controls code { font-family: var(--font-mono); font-size: 11px; overflow-wrap: anywhere; color: var(--ink-500); line-height: 1.7; }
.model-controls dl { margin: 8px 0; font-size: 12px; display: grid; gap: 8px; }
.model-controls dt { color: var(--ink-500); }
.model-controls dd { margin: 0 0 12px; color: var(--ink-900); font-variant-numeric: tabular-nums; }
.model-controls p { font-size: 12px; line-height: 1.8; color: var(--ink-500); }
.key-field { display: grid; gap: 12px; }
.key-input { display: flex; min-width: 0; border: 1px solid var(--field-border); border-radius: 6px; background: var(--surface); }
.key-input input { width: 0; flex: 1; min-width: 0; border: 0; background: transparent; padding: 0 12px; height: 44px; color: var(--ink-900); font-family: var(--font-mono); font-size: 12px; }
.key-input button { white-space: nowrap; flex: none; background: transparent; border: 0; border-left: 1px solid var(--hairline); color: var(--ink-600); padding: 0 12px; min-width: 44px; font-size: 12px; }
.key-input button:hover { background: var(--paper-2); }
.key-field small { font-size: 11px; color: var(--ink-500); line-height: 1.7; }
.model-controls .endpoint { padding-top: 12px; border-top: 1px solid var(--hairline); }
.stream-setting { display: flex; align-items: center; gap: 8px; min-height: 44px; cursor: pointer; }
.conversation { min-width: 0; display: flex; flex-direction: column; }
.inspect-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 0 20px; border-bottom: 1px solid var(--hairline); }
.inspect-tabs { display: flex; min-width: 0; gap: 16px; }
.inspect-tabs button { min-height: 44px; padding: 0; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--ink-500); font-size: 12px; white-space: nowrap; }
.inspect-tabs button[aria-selected="true"] { color: var(--ink-900); border-bottom-color: var(--ink-900); }
.copy-debug { flex: none; min-height: 44px; border: 0; padding: 0; color: var(--ink-600); background: transparent; font-size: 12px; }
.copy-debug:hover:not(:disabled), .inspect-tabs button:hover { color: var(--ink-900); }
.response-meta { display: flex; align-items: center; gap: 12px; min-width: 0; padding: 12px 20px; border-bottom: 1px solid var(--hairline); color: var(--ink-500); font-size: 11px; font-variant-numeric: tabular-nums; }
.response-meta code { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); font-size: 10px; }
.response-meta .request-time { margin-left: auto; white-space: nowrap; }
.response-meta .http-error { color: var(--danger); }
.inspect-panel { flex: 1; min-width: 0; min-height: 360px; max-height: 62vh; overflow: auto; padding: 24px; background: var(--paper-2); }
.inspect-panel pre { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; color: var(--ink-900); font-family: var(--font-mono); font-size: 12px; line-height: 1.8; }
.inspect-panel pre.empty-response, .preview-note { color: var(--ink-500); }
.preview-note { margin-top: 16px; font-size: 11px; }
.messages { flex: 1; overflow: auto; max-height: 62vh; min-height: 360px; padding: 28px; scroll-behavior: auto; }
.conversation-empty { max-width: 430px; margin: 44px auto; display: grid; gap: 12px; }
.conversation-empty h2 { font-size: 24px; font-weight: 550; letter-spacing: -.035em; }
.conversation-empty p { font-size: 13px; color: var(--ink-500); line-height: 1.8; margin-bottom: 12px; }
.conversation-empty button { text-align: left; border: 1px solid var(--hairline); border-radius: 6px; background: var(--surface); padding: 12px 16px; min-height: 44px; color: var(--ink-700); transition: background .15s, border-color .15s; }
.conversation-empty button:hover:not(:disabled) { background: var(--paper-2); border-color: var(--ink-300); }
.message { margin-bottom: 24px; }
.message > span { font-size: 12px; font-weight: 600; color: var(--ink-600); }
.message small { margin-left: 8px; font-weight: 400; color: var(--ink-500); }
.message p { margin-top: 8px; white-space: pre-wrap; overflow-wrap: anywhere; padding: 16px; background: var(--paper-2); border: 1px solid var(--hairline); border-radius: 6px; color: var(--ink-900); font-size: 14px; line-height: 1.85; }
.message.user p { background: var(--surface); border-color: transparent; padding: 4px 0; }
.composer { border-top: 1px solid var(--hairline); padding: 20px; margin-top: auto; }
.composer textarea { width: 100%; min-height: 112px; resize: vertical; border: 1px solid var(--field-border); background: var(--surface); color: var(--ink-900); padding: 12px 16px; border-radius: 6px; line-height: 1.7; font-size: 14px; }
.composer-footer { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-top: 12px; }
.composer-footer > span { font-size: 11px; color: var(--ink-500); line-height: 1.7; }
.request-error, .model-controls .request-error { color: var(--danger); font-size: 12px; line-height: 1.7; padding-bottom: 12px; overflow-wrap: anywhere; }
:is(button, input, select, textarea):focus-visible { outline: 2px solid var(--ink-900); outline-offset: 3px; }
button:active:not(:disabled) { transform: translateY(1px); }
button:disabled { opacity: .5; cursor: not-allowed; }
@media (max-width: 900px) { .workbench-layout { grid-template-columns: 225px minmax(0, 1fr); } .model-controls { padding: 20px; } }
@media (max-width: 700px) {
  .workbench-layout { min-height: 0; }
  .workbench-title { align-items: flex-start; } .workbench-title h1 { font-size: 23px; }
  .workbench-layout { grid-template-columns: minmax(0, 1fr); }
  .model-controls { border-right: 0; border-bottom: 1px solid var(--hairline); gap: 12px; }
  .settings-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 0; }
  .settings-heading > div { min-width: 0; }
  .settings-toggle { display: block; flex: none; min-height: 44px; border: 1px solid var(--hairline); border-radius: 6px; padding: 0 12px; background: var(--surface); color: var(--ink-600); font-size: 12px; }
  .collapsed-model { display: block; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-top: 4px; font-size: 11px; color: var(--ink-500); }
  .model-controls.settings-collapsed > :not(.settings-heading) { display: none; }
  .model-controls h2 { margin-bottom: 0; } .model-controls dl { grid-template-columns: 1fr 1fr; margin: 0; gap: 8px; }
  .model-controls dd { margin: 0; text-align: right; } .model-controls > p { margin: 0; }
  .inspect-toolbar { padding: 0 16px; gap: 8px; } .inspect-tabs { gap: 12px; } .response-meta { padding: 12px 16px; gap: 8px; } .inspect-panel { padding: 20px; min-height: 160px; max-height: 40vh; }
  .messages { padding: 20px; min-height: 160px; max-height: 40vh; } .conversation-empty { margin: 8px auto; gap: 8px; }
  .conversation-empty .empty-mark, .conversation-empty button { display: none; }
  .conversation-empty h2 { font-size: 20px; } .conversation-empty p { margin-bottom: 0; }
  .composer textarea { min-height: 88px; }
  .composer { padding: 16px; } .composer-footer { flex-wrap: wrap; } .composer-footer > span { flex: 1; min-width: 140px; }
}
h1, h2 { text-wrap: balance; }
</style>
