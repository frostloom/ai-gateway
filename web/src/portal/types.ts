/** Portal 端类型定义（依据 docs/api-reference.md，wire 键与 Go 字段一致处已标注） */

/** 商品 SKU（json tags） */
export interface Model {
  id: number
  vendor: string
  model_id: string
  name: string
  input_price_cent: number   // 分/百万 token
  output_price_cent: number  // 分/百万 token
  context_len: number        // token
  tags: string                // JSON 数组串
  status: number              // 0=上架 1=下架
  created_at: string
  updated_at: string
}

/** 套餐（wire 键 = Go 字段名） */
export interface Plan {
  ID: number
  Name: string
  PlanType: string            // recurring / one_time
  PriceMoney: number          // 元
  ValidityDays: number
  RefreshHours: number
  TierQuota: string           // JSON 串 {"旗舰":2000000,...}
  Status: number              // 0=active 1=disabled
  CreatedAt: string
  UpdatedAt: string
}

/** 订阅（wire 键 = Go 字段名） */
export interface Subscription {
  ID: number
  TenantID: number
  PlanID: number
  PlanType: string
  Status: string              // active / cancelled / expired
  CycleStart: string | null
  CycleEnd: string | null
  AutoRenew: boolean
  CycleNum: number
  QuotaGranted: number
  PendingPlanID: number | null
  CancelledAt: string | null
  CreatedAt: string
  UpdatedAt: string
}

/** 订阅额度档次剩余（json tags） */
export interface TierRemain {
  tier: string
  quota: number
  consumed: number
  remaining: number
}

/** MeView（json tags，金额分） */
export interface MeView {
  balance: number
  balance_cent: number
  ledger_balance: number
  consistent: boolean
  subscription: Subscription | null
  allowance: TierRemain[]
  refresh_hours: number
  plans: Plan[]
  consumption: DayStat[]
  consumption_by_model: ModelStat[]
  topups: Topup[]
}

/** 每日消耗（分） */
export interface DayStat { day: string; tokens: number }
/** 按模型消耗 */
export interface ModelStat {
  model: string; vendor: string
  input_tokens: number; output_tokens: number
  cost_cent: number; calls: number
}

/** 充值订单（wire 键 = Go 字段名） */
export interface RechargeOrder {
  ID: number; TenantID: number; OrderNo: string
  AmountMoney: number          // 元
  AmountCent: number           // 分
  Status: string               // pending_payment / paid / refunded / cancelled
  PaidAt: string | null
  CreatedAt: string
  UpdatedAt: string
}

/** 入账流水（wire 键 = Go 字段名） */
export interface Topup {
  ID: number; TenantID: number
  Source: string               // recharge / subscription / refund
  Amount: number               // 正=入账 负=退款（分）
  RefType: string; RefID: string
  CreatedAt: string
}

export interface RefundResult {
  order_no: string
  amount_cent: number
  refund_cent: number
  balance_after: number
}

export interface CancelResult {
  plan_name: string
  refund: number
  current_cycle_end: string | null
}

/** 客服聊天 */
export interface TraceItem {
  tool: string
  args: string
  confirmed: boolean
  preview: string
  result: string
}
export interface ChatResp {
  session_id: string
  reply: string
  trace: TraceItem[]
  pending_confirm: boolean
  pending_preview: string
  pending_tool: string
}

/** tags JSON 串 → 数组 */
export function parseTags(s: string | undefined | null): string[] {
  if (!s) return []
  try {
    const v = JSON.parse(s)
    return Array.isArray(v) ? v.map(String) : []
  } catch {
    return s.split(',').map(x => x.trim()).filter(Boolean)
  }
}

export function parseTierQuota(s: string | undefined | null): Record<string, number> {
  if (!s) return {}
  try { return JSON.parse(s) } catch { return {} }
}

/** 订单/订阅状态 → 展示元 */
export const ORDER_STATUS: Record<string, { text: string; tone: 'ok' | 'warn' | 'danger' | 'muted' | 'info' }> = {
  pending_payment: { text: '待支付', tone: 'warn' },
  paid: { text: '已支付', tone: 'ok' },
  refunded: { text: '已退款', tone: 'muted' },
  cancelled: { text: '已取消', tone: 'muted' },
}
export const SUB_STATUS: Record<string, { text: string; tone: 'ok' | 'warn' | 'danger' | 'muted' | 'info' }> = {
  active: { text: '订阅中', tone: 'ok' },
  cancelled: { text: '已退订', tone: 'muted' },
  expired: { text: '已过期', tone: 'muted' },
}