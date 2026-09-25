/** Dashboard 管理端类型（依据 docs/api-reference.md） */
import type { MeView, Plan, Subscription, TierRemain, RechargeOrder, Topup, Model, DayStat } from '../portal/types'
export type { MeView, Plan, Subscription, TierRemain, RechargeOrder, Topup, Model, DayStat }

/** overview 租户行 */
export interface TenantStat {
  id: number
  name: string
  initial_quota: number
  balance: number           // 分
  redis_balance: number     // 分
  consistent: boolean
  topups: number
  settled: number
  pending: number
  bill_count: number
  today_bills: number
  today_settled: number
}
export interface OverviewResp {
  tenants: TenantStat[]
  totals: { settled: number; pending: number; bills: number; today_bills: number; today_settled: number }
}

/** 账单（wire 键 = Go 字段名） */
export interface Bill {
  ID: number
  RequestID: string
  Phase: string             // reserve / settle
  Status: string            // pending / settled / reversed
  TenantID: number
  APIKeyID: number
  ProviderID: number | null
  Model: string
  Funding: string           // balance / subscription
  PreQuota: number
  ActualQuota: number | null
  DeltaQuota: number | null
  PromptTokens: number | null
  CompletionTokens: number | null
  InPriceCent: number | null
  OutPriceCent: number | null
  UsageReportedAt: string | null
  ErrorCode: string
  CreatedAt: string
}

/** router provider（裸数组） */
export interface ProviderState {
  id: number
  name: string
  base_url: string
  models: string
  weight: number
  status: number             // 0=active 1=disabled 2=banned
  consecutive_fail: number
  breaker_state: string      // closed / open / half_open
  next_closed_at?: string
}

/** billing 渠道毛利（{items}） */
export interface ProviderMargin {
  id: number
  name: string
  base_url: string
  upstream_key: string       // 已脱敏 sk-***
  cost_in_cent: number
  cost_out_cent: number
  models: string
  weight: number
  status: number
  fail_count: number
  consecutive_fail: number
  cooldown_until: string | null
  created_at: string
  updated_at: string
  selling_cent: number
  cost_cent: number
  margin_cent: number
}

export interface SalesResp {
  summary: { topup_cent: number; consumed_cent: number; settled_bills: number; active_tenants: number }
  by_model: { model_id: string; name: string; vendor: string; consumed_cent: number; calls: number; input_tokens: number; output_tokens: number; active_tenants: number }[]
  by_vendor: { vendor: string; consumed_cent: number; calls: number; active_tenants: number }[]
  by_day: { day: string; consumed_cent: number; topup_cent: number }[]
}

export interface ReconcileResp {
  checked: number
  inconsistent: { id: number; name: string; ledger_cent: number; redis_cent: number }[]
  ok: boolean
}

/** 客服审计（wire 键 = Go 字段名） */
export interface AuditLogItem {
  ID: number
  SessionID: string
  TenantID: number
  Tool: string
  Args: string
  Confirmed: boolean
  Preview: string
  Result: string
  CreatedAt: string
}

export interface HealthResp {
  ok: boolean
  services: Record<string, { ok: boolean; error?: string }>
}

/** 试一发 demo key（与 scripts/seed 的 demo 租户一致，管理端便利） */
export const DEMO_TENANT_KEY = 'sk-demo-8f3a2b1c9d4e5f60'