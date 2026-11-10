<script setup lang="ts">
import { ref, computed, inject, onMounted, onActivated, onBeforeUnmount, watch, nextTick, type Ref } from "vue";
import { modelsApi } from "../api";
import type { Model } from "../types";
import { portalStore } from "../store";
import { money } from "../../shared/utils/format";
import { toastErr } from "../../shared/utils/toast";
import BaseButton from "../../shared/components/ui/BaseButton.vue";
import { parseWorkbenchSnapshot, serializeWorkbenchSnapshot, workbenchStorageKey, type WorkbenchMessage as Message } from "../workbenchState";
const account = inject<Ref<string>>("workbench-account", ref(""));
// Capture the identity at mount so logout cannot redirect a final save to another account.
const storageKey = workbenchStorageKey(account.value);
const models = ref<Model[]>([]);
const model = ref("");
const prompt = ref("");
const messages = ref<Message[]>([]);
const stream = ref(true);
const loading = ref(true);
const mobileSettings = ref(false);
const sending = ref(false);
const error = ref("");
const elapsed = ref(0);
const usage = ref<{ input: number; output: number; cost: number } | null>(null);
const modelError = ref("");
const messagesPanel = ref<HTMLElement>();
const followOutput = ref(true);
const selected = computed(() =>
  models.value.find((m) => m.model_id === model.value),
);
const MAX_STREAM_EVENT_CHARS = 1024 * 1024;
let controller: AbortController | undefined;
let persistTimer: ReturnType<typeof setTimeout> | undefined;
const storageError = ref("");
function persist() {
  if (!storageKey) return;
  const raw = serializeWorkbenchSnapshot({
    model: model.value,
    prompt: prompt.value,
    messages: messages.value,
    stream: stream.value,
    inProgress: sending.value,
    elapsed: elapsed.value,
    usage: usage.value,
  });
  if (!raw) {
    storageError.value = "当前对话超过本地保存容量，请先保存需要的内容再新建对话";
    return;
  }
  try {
    localStorage.setItem(storageKey, raw);
    storageError.value = "";
  } catch {
    storageError.value = "当前浏览器无法保存对话，刷新前请保存需要的内容";
  }
}
if (storageKey) {
  try {
    const saved = parseWorkbenchSnapshot(localStorage.getItem(storageKey));
    if (saved) {
      model.value = saved.model;
      prompt.value = saved.prompt;
      messages.value = saved.messages;
      stream.value = saved.stream;
      elapsed.value = saved.elapsed;
      usage.value = saved.usage;
      if (saved.inProgress || saved.messages.some(message => !message.complete)) {
        error.value = "上次生成已中断，已保留消息与草稿，可重新发送";
      }
    }
  } catch {
    storageError.value = "当前浏览器无法读取已保存的对话";
  }
}
watch([model, prompt, messages, stream, sending, elapsed, usage], () => {
  clearTimeout(persistTimer);
  persistTimer = setTimeout(persist, 250);
}, { deep: true });
function applyRequestedModel() {
  const requested = portalStore.tryModel;
  if (!requested || sending.value || !models.value.some(item => item.model_id === requested)) return;
  if (model.value !== requested) {
    clear();
    model.value = requested;
  }
  portalStore.tryModel = "";
}
async function load() {
  if (loading.value && models.value.length) return;
  loading.value = true;
  modelError.value = "";
  try {
    models.value = (await modelsApi()).items.filter((item) => item.status === 0);
    if (!model.value) model.value = models.value[0]?.model_id || "";
    applyRequestedModel();
  } catch (e: any) {
    modelError.value = e?.message || "读取模型目录失败，请重试";
  } finally {
    loading.value = false;
  }
}
onMounted(() => {
  void load();
  window.addEventListener("pagehide", persist);
});
onActivated(applyRequestedModel);
onBeforeUnmount(() => {
  clearTimeout(persistTimer);
  persist();
  controller?.abort();
  window.removeEventListener("pagehide", persist);
});
function clear() {
  if (!sending.value) {
    messages.value = [];
    prompt.value = "";
    usage.value = null;
    elapsed.value = 0;
    error.value = "";
    persist();
  }
}
function onPromptKeydown(event: KeyboardEvent) {
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing && event.keyCode !== 229) {
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
  // A retry replaces its unfinished pair while complete turns stay in context.
  const lastUser = messages.value.at(-2);
  const lastAnswer = messages.value.at(-1);
  if (lastUser?.role === "user" && !lastUser.complete && lastUser.content === text && lastAnswer?.role === "assistant" && !lastAnswer.complete) {
    messages.value.splice(-2);
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
  controller = new AbortController();
  const startedAt = Date.now();
  let stopped = false;
  try {
    const response = await fetch("/portal/completions", {
      method: "POST",
      credentials: "include",
      signal: controller.signal,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        model: price.model_id,
        messages: [...history, { role: "user", content: text }],
        stream: useStream,
        ...(useStream ? { stream_options: { include_usage: true } } : {}),
      }),
    });
    if (!response.ok) {
      if (response.status === 401)
        window.dispatchEvent(new Event("portal-session-expired"));
      const data = await response.json().catch(() => null);
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
        if (event.length > MAX_STREAM_EVENT_CHARS) throw new Error("响应帧超过大小限制");
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
        const data = JSON.parse(payload);
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
          if (buffer.length > MAX_STREAM_EVENT_CHARS) throw new Error("响应帧超过大小限制");
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
      const data = await response.json();
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
        <h1>对话工作台</h1>
        <p>使用已上架模型进行多轮对话，按实际用量计费。</p>
      </div>
      <BaseButton :disabled="sending || (!messages.length && !prompt)" @click="clear"
        >新建对话</BaseButton
      >
    </div>
    <button class="mobile-settings" :aria-expanded="mobileSettings" aria-controls="workbench-settings" @click="mobileSettings = !mobileSettings"><span>{{ selected?.name || model || '选择模型' }}</span><span>{{ mobileSettings ? '收起设置' : '模型设置' }}</span></button>
    <div class="workbench-layout">
      <aside id="workbench-settings" class="model-controls" :class="{ 'mobile-open': mobileSettings }" aria-label="请求设置">
        <h2>对话设置</h2>
        <label for="workbench-model">本次对话模型</label
        ><select
          id="workbench-model"
          v-model="model"
          :disabled="sending || loading"
          @change="clear"
        >
          <option value="" disabled>{{ loading ? "正在加载模型…" : "选择可用模型" }}</option>
          <option v-if="model && !selected && !loading" :value="model" disabled>{{ model }}（当前不可用）</option>
          <option v-for="m in models" :key="m.id" :value="m.model_id">
            {{ m.name }}
          </option>
        </select>
        <p v-if="modelError" class="request-error" role="alert">{{ modelError }}</p>
        <p v-else-if="!loading && model && !selected" class="request-error" role="status">当前对话模型已不可用。记录与草稿已保留，选择其他模型将开启新对话。</p>
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
        <label class="stream-setting"
          ><input
            v-model="stream"
            type="checkbox"
            :disabled="sending"
          />流式显示回复</label
        >
        <p>切换模型会开启新对话。对话与草稿保存在此浏览器，按账户区分。</p>
        <BaseButton v-if="modelError || (!models.length && !loading)" :loading="loading" @click="load">重新加载模型</BaseButton>
      </aside>
      <section class="conversation" aria-label="模型对话">
        <div
          ref="messagesPanel"
          class="messages"
          role="log"
          @scroll="onMessagesScroll"
          aria-live="polite"
          :aria-busy="sending"
        >
          <div v-if="!messages.length" class="conversation-empty">
            <h2>新对话</h2>
            <p>继续上次对话，或发送一条新消息。</p>

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
              >{{ m.role === "user" ? "你" : selected?.name || model || "模型"
              }}<small v-if="!m.complete">{{
                sending ? (m.role === "user" ? " 已发送" : " 生成中") : " 未完成"
              }}</small></span
            >
            <p>
              {{ m.content || (sending ? "正在等待回复…" : "本次未收到回复") }}
            </p>
          </article>
        </div>
        <form class="composer" @submit.prevent="send">
          <p v-if="error" class="request-error" role="alert">{{ error }}</p>
          <p v-if="storageError" class="request-error" role="status">{{ storageError }}</p>
          <textarea
            v-model="prompt"
            aria-label="发送给模型的消息"
            placeholder="输入消息，Enter 发送，Shift + Enter 换行"
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
              :disabled="!selected || !prompt.trim() || loading"
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
.mobile-settings { display: none; }
.workbench-title h1 { font-size: 21px; letter-spacing: -.04em; font-weight: 600; }
.workbench-title p { font-size: 13px; color: var(--ink-500); margin-top: 8px; line-height: 1.6; }
.workbench-layout { display: grid; grid-template-columns: 264px minmax(0, 1fr); border: 1px solid var(--hairline); border-radius: 10px; overflow: hidden; background: var(--surface); min-height: 640px; }
.model-controls { padding: 24px; background: var(--paper-2); border-right: 1px solid var(--hairline); display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.model-controls h2 { font-size: 14px; font-weight: 600; margin-bottom: 12px; }
.model-controls label { font-size: 12px; color: var(--ink-600); }
.model-controls select { width: 100%; min-height: 44px; padding: 0 12px; border: 1px solid var(--field-border); border-radius: 6px; background: var(--surface); color: var(--ink-900); }
.model-controls code { font-family: var(--font-mono); font-size: 11px; overflow-wrap: anywhere; color: var(--ink-500); line-height: 1.7; }
.model-controls dl { margin: 8px 0; font-size: 12px; display: grid; gap: 8px; }
.model-controls dt { color: var(--ink-500); }
.model-controls dd { margin: 0 0 12px; color: var(--ink-900); font-variant-numeric: tabular-nums; }
.model-controls p { font-size: 12px; line-height: 1.8; color: var(--ink-500); }
.stream-setting { display: flex; align-items: center; gap: 8px; min-height: 44px; cursor: pointer; }
.conversation { min-width: 0; display: flex; flex-direction: column; }
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
  .workbench-title { align-items: flex-start; } .workbench-title h1 { font-size: 21px; }
  .workbench { gap: 16px; }
  .mobile-settings { display: flex; justify-content: space-between; gap: 12px; width: 100%; border: 1px solid var(--hairline); background: var(--surface); border-radius: 6px; padding: 12px; color: var(--ink-800); font-size: 13px; }
  .mobile-settings span:first-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mobile-settings span:last-child { flex: none; color: var(--ink-500); }
  .workbench-layout { grid-template-columns: minmax(0, 1fr); min-height: 440px; }
  .model-controls { display: none; }
  .model-controls.mobile-open { display: flex; }
  .model-controls { border-right: 0; border-bottom: 1px solid var(--hairline); gap: 12px; }
  .model-controls h2 { margin-bottom: 0; } .model-controls dl { grid-template-columns: 1fr 1fr; margin: 0; gap: 8px; }
  .model-controls dd { margin: 0; text-align: right; } .model-controls > p { margin: 0; }
  .messages { padding: 20px; min-height: 180px; } .conversation-empty { margin: 8px auto; }
  .conversation-empty button { display: none; }
  .conversation-empty h2 { font-size: 18px; }
  .composer { padding: 16px; } .composer-footer { flex-wrap: wrap; } .composer-footer > span { flex: 1; min-width: 140px; }
}
h1, h2 { text-wrap: balance; }
</style>
