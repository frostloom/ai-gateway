export interface WorkbenchMessage {
  role: "user" | "assistant";
  content: string;
  complete: boolean;
}

export interface WorkbenchSnapshot {
  model: string;
  prompt: string;
  messages: WorkbenchMessage[];
  stream: boolean;
  inProgress: boolean;
  elapsed: number;
  usage: { input: number; output: number; cost: number } | null;
}

const MAX_SNAPSHOT_CHARS = 1024 * 1024;
const MAX_MESSAGES = 1000;

export function workbenchStorageKey(account: string): string | null {
  return /^\d+:\d+$/.test(account) ? `agw.workbench.v1.${account}` : null;
}

export function parseWorkbenchSnapshot(raw: string | null): WorkbenchSnapshot | null {
  if (!raw || raw.length > MAX_SNAPSHOT_CHARS) return null;
  try {
    const state = JSON.parse(raw);
    if (!state || typeof state !== "object" ||
      typeof state.model !== "string" || state.model.length > 128 ||
      typeof state.prompt !== "string" || state.prompt.length > 32000 ||
      typeof state.stream !== "boolean" || typeof state.inProgress !== "boolean" ||
      !Number.isFinite(state.elapsed) || state.elapsed < 0 ||
      !Array.isArray(state.messages) || state.messages.length > MAX_MESSAGES) return null;
    if (!state.messages.every((message: unknown) => {
      if (!message || typeof message !== "object") return false;
      const row = message as Record<string, unknown>;
      return (row.role === "user" || row.role === "assistant") &&
        typeof row.content === "string" && typeof row.complete === "boolean";
    })) return null;
    if (state.usage !== null && (!state.usage ||
      !Number.isSafeInteger(state.usage.input) || state.usage.input < 0 ||
      !Number.isSafeInteger(state.usage.output) || state.usage.output < 0 ||
      !Number.isFinite(state.usage.cost) || state.usage.cost < 0)) return null;
    return {
      model: state.model,
      prompt: state.prompt,
      messages: state.messages.map((message: WorkbenchMessage) => ({ role: message.role, content: message.content, complete: message.complete })),
      stream: state.stream,
      inProgress: state.inProgress,
      elapsed: state.elapsed,
      usage: state.usage === null ? null : { input: state.usage.input, output: state.usage.output, cost: state.usage.cost },
    };
  } catch {
    return null;
  }
}

export function serializeWorkbenchSnapshot(state: WorkbenchSnapshot): string | null {
  const raw = JSON.stringify(state);
  return raw.length <= MAX_SNAPSHOT_CHARS && state.messages.length <= MAX_MESSAGES ? raw : null;
}
