/** 数字/金额格式化（全站统一口径：金额单位=分） */

/** 分 → 元字符串（无符号，如 "12.50"） */
export function yuan(cent: number | null | undefined): string {
  return (Number(cent || 0) / 100).toFixed(2)
}

/** 分 → 带 ¥ 字符串 */
export function money(cent: number | null | undefined): string {
  return '¥' + yuan(cent)
}

/** 千分位整数 */
export function fmt(n: number | null | undefined): string {
  if (n == null) return '0'
  return Number(n).toLocaleString('en-US')
}

/** 中文万/亿缩写（用于 token 数、账单量） */
export function zhCount(n: number | null | undefined): string {
  const v = Number(n || 0)
  if (Math.abs(v) >= 1e8) return trim((v / 1e8).toFixed(2)) + ' 亿'
  if (Math.abs(v) >= 1e4) return trim((v / 1e4).toFixed(1)) + ' 万'
  return '' + v
}
function trim(s: string): string { return s.replace(/\.0+$/, '').replace(/(\.\d*?)0+$/, '$1') }

/** ISO 时间 → "MM-DD HH:mm" */
export function fmtTime(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const p = (x: number) => String(x).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

/** ISO 时间 → "YYYY-MM-DD HH:mm" */
export function fmtTimeFull(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const p = (x: number) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

/** ISO 时间 → "MM-DD" */
export function fmtDay(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const p = (x: number) => String(x).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** 百分比 */
export function pct(n: number | null | undefined, digits = 1): string {
  if (n == null) return '—'
  return Number(n).toFixed(digits) + '%'
}

/** token 数 → 可读（万/亿） */
export const tokens = zhCount

/** 状态映射文案（bill 状态） */
export const BILL_STATUS: Record<string, { text: string; tone: 'ok' | 'warn' | 'danger' | 'muted' }> = {
  settled: { text: '已结算', tone: 'ok' },
  pending: { text: '待结算', tone: 'warn' },
  reversed: { text: '已退款', tone: 'muted' },
}