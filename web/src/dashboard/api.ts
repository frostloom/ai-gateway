/** Dashboard 管理端 API 封装（/admin/*，cookie 会话，由 shared/api 注入 credentials） */
import { api } from '../shared/api'
import type {
  OverviewResp, Bill, ProviderState, ProviderMargin, SalesResp,
  ReconcileResp, AuditLogItem, HealthResp, Model,
} from './types'
import type { Plan, Subscription, TierRemain, RechargeOrder, Topup } from '../portal/types'

/* ---------- auth ---------- */
export const authInitialized = () => api<{ initialized: boolean }>('/admin/auth/initialized')
export const authLogin = (username: string, password: string) =>
  api<{ username: string }>('/admin/auth/login', { body: { username, password } })
export const authSetup = (username: string, password: string) =>
  api<{ username: string }>('/admin/auth/setup', { body: { username, password } })
export const authMe = () => api<{ username: string }>('/admin/auth/me')
export const authLogout = () => api('/admin/auth/logout', { body: {} })

/* ---------- 数据 ---------- */
export const overviewApi = () => api<OverviewResp>('/admin/overview')
export const healthApi = () => api<HealthResp>('/admin/health')
export const billsApi = (q: Record<string, string | number> = {}) =>
  api<{ total: number; items: Bill[] }>('/admin/bills?' + new URLSearchParams(Object.entries(q).map(([k, v]) => [k, String(v)])))
export const providersApi = () => api<ProviderState[]>('/admin/providers')
export const channelsApi = () => api<{ items: ProviderMargin[] }>('/admin/channels')
export const upsertChannelApi = (body: Record<string, unknown>) => api('/admin/channels', { body })
export const modelsApi = (q: Record<string, string | number> = {}) =>
  api<{ items: Model[] }>('/admin/models' + (Object.keys(q).length ? '?' + new URLSearchParams(Object.entries(q).map(([k, v]) => [k, String(v)])) : ''))
export const createModelApi = (body: Record<string, unknown>) => api<Model>('/admin/models', { body })
export const modelStatusApi = (id: number, status: number) => api('/admin/models/status', { body: { id, status } })
export const modelPriceApi = (id: number, input_price_cent: number, output_price_cent: number) =>
  api('/admin/models/price', { body: { id, input_price_cent, output_price_cent } })
export const salesApi = (from: string, to: string) => api<SalesResp>(`/admin/sales?from=${from}&to=${to}`)
export const auditApi = (tenantId = 0, limit = 50) =>
  api<{ items: AuditLogItem[] }>(`/admin/audit?tenant_id=${tenantId}&limit=${limit}`)
export const reconcileApi = () => api<ReconcileResp>('/admin/reconcile-check')
export const subscriptionApi = (tenantId: number) =>
  api<{ subscription: Subscription | null; allowance: TierRemain[] | null }>(`/admin/subscription?tenant_id=${tenantId}`)
export const topupsApi = (tenantId = 0) => api<{ items: Topup[] }>(`/admin/topups?tenant_id=${tenantId}`)
export const rechargeOrdersApi = (tenantId = 0) => api<{ items: RechargeOrder[] }>(`/admin/recharge-orders?tenant_id=${tenantId}`)
export const meApi = (tenantId: number) => api<import('../portal/types').MeView>(`/admin/me?tenant_id=${tenantId}`)
export const plansApi = () => api<{ items: Plan[] }>('/admin/plans')