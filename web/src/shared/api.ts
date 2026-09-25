/** 统一 API 请求层：portal（Bearer 租户 key）+ admin（HttpOnly cookie）两套鉴权 */

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

const PORTAL_KEY = 'portal.api_key'
const PORTAL_SID = 'portal_sid'

export const auth = {
  getKey(): string { return localStorage.getItem(PORTAL_KEY) || '' },
  setKey(k: string) { localStorage.setItem(PORTAL_KEY, k) },
  clearKey() { localStorage.removeItem(PORTAL_KEY) },
  getSid(): string { return localStorage.getItem(PORTAL_SID) || '' },
  setSid(s: string) { localStorage.setItem(PORTAL_SID, s) },
  clearSid() { localStorage.removeItem(PORTAL_SID) },
}

interface Opts {
  method?: string
  body?: unknown
  /** portal 调用强制 Bearer；默认按 auth.getKey() */
  key?: string
  /** 是否跳过鉴权头（如 /admin/auth/* 登录前接口） */
  admin?: boolean
  raw?: boolean
}

/** GET/POST JSON，错误归一为 ApiError */
export async function api<T = any>(path: string, opts: Opts = {}): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const isPortal = path.startsWith('/portal/')
  if (isPortal) {
    const k = opts.key ?? auth.getKey()
    if (k) headers['Authorization'] = 'Bearer ' + k
  }
  if (opts.body !== undefined && typeof opts.body !== 'string') {
    opts.body = JSON.stringify(opts.body)
  }
  const res = await fetch(path, {
    method: opts.method || (opts.body !== undefined ? 'POST' : 'GET'),
    headers,
    body: opts.body !== undefined ? (opts.body as string) : undefined,
    credentials: 'include', // admin 会话 HttpOnly cookie
  })
  if (res.status === 401) {
    throw new ApiError(401, '未登录或密钥无效')
  }
  if (!res.ok) {
    let msg = path + ' → ' + res.status
    try {
      const d = await res.json()
      if (d?.error) msg = d.error
    } catch { /* ignore */ }
    throw new ApiError(res.status, msg)
  }
  if (opts.raw) return (await res.text()) as unknown as T
  return (await res.json()) as T
}