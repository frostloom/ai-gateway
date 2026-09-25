/** Portal 端 API 封装（全部走 /portal/*，Bearer 由 shared/api 注入） */
import { api } from '../shared/api'
import type {
  MeView, Plan, Model, RechargeOrder, Topup, DayStat,
  RefundResult, CancelResult, ChatResp,
} from './types'

export const meApi = () => api<MeView>('/portal/me')
export const plansApi = () => api<{ items: Plan[] }>('/portal/plans')
export const modelsApi = (vendor?: string) =>
  api<{ items: Model[] }>('/portal/models' + (vendor ? '?vendor=' + encodeURIComponent(vendor) : ''))
export const topupsApi = () => api<{ items: Topup[] }>('/portal/topups')
export const ordersApi = () => api<{ items: RechargeOrder[] }>('/portal/recharge-orders')
export const consumptionApi = (days = 7) => api<{ items: DayStat[] }>(`/portal/consumption?days=${days}`)

export const rechargeApi = (amountMoney: number, idemKey?: string) =>
  api<RechargeOrder>('/portal/recharge', { body: { amount_money: amountMoney, ...(idemKey ? { idem_key: idemKey } : {}) } })
export const payApi = (orderNo: string) =>
  api<RechargeOrder>('/portal/recharge/pay', { body: { order_no: orderNo } })
export const refundOrderApi = (orderNo: string) =>
  api<RefundResult>('/portal/recharge/refund', { body: { order_no: orderNo } })
export const refundAmountApi = (amountMoney: number, idemKey?: string) =>
  api<RefundResult>('/portal/recharge/refund', { body: { amount_money: amountMoney, ...(idemKey ? { idem_key: idemKey } : {}) } })

export const subscribeApi = (planId: number) =>
  api('/portal/subscribe', { body: { plan_id: planId } })
export const changePlanApi = (planId: number) =>
  api('/portal/change-plan', { body: { plan_id: planId } })
export const cancelSubApi = () =>
  api<CancelResult>('/portal/cancel-subscription', { body: {} })

export const chatApi = (message: string, sessionId?: string) =>
  api<ChatResp>('/portal/chat', { body: sessionId ? { session_id: sessionId, message } : { message } })