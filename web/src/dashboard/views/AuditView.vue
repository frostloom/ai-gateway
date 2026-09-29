<script setup lang="ts">
/**
 * 客服审计（重点视图）
 *
 * 设计目标：让「谁在什么时候以谁的身份做了什么、有没有确认、有没有被拦、碰没碰到钱」
 * 一眼可查。高危操作（退款、退订、改套餐、触发守门）显式标红并置顶。
 * 风险分级依据（与 agent 工具定义一致）：
 *   - refund_recharge     资金流出，最高危
 *   - cancel_subscription 影响计费周期（当期不退但停续费）
 *   - change_plan         下期计费变更
 *   - subscribe_plan      产生订阅合同
 *   - recharge            资金流入（模拟支付），风险中等
 *   - 读工具               get_balance / get_consumption / ...
 *   - 未确认的写操作 / guard 拦截 → 重点提示
 */
import { ref, computed, onMounted } from 'vue'
import {
  IconAlertTriangle, IconCoins, IconSearch, IconCircleCheck, IconLock, IconFilter,
} from '@tabler/icons-vue'
import { auditApi, overviewApi } from '../api'
import type { AuditLogItem, TenantStat } from '../types'
import { fmtTimeFull, money } from '../../shared/utils/format'
import { toastErr } from '../../shared/utils/toast'
import Card from '../../shared/components/ui/Card.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import KpiCard from '../../shared/components/ui/KpiCard.vue'
import RankList from '../../shared/components/ui/RankList.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import DataTable from '../../shared/components/ui/DataTable.vue'

const tenants = ref<TenantStat[]>([])
const items = ref<AuditLogItem[]>([])
const tid = ref(0)
const loading = ref(false)
const onlyRisky = ref(false)

/* ---------- 风险模型 ---------- */
type Risk = 'critical' | 'high' | 'medium' | 'low'

const WRITE_TOOLS = ['recharge', 'refund_recharge', 'subscribe_plan', 'change_plan', 'cancel_subscription']

const TOOL_META: Record<string, { label: string; risk: Risk; desc: string }> = {
  refund_recharge:     { label: '退款',     risk: 'critical', desc: '资金流出' },
  cancel_subscription: { label: '退订',     risk: 'high',     desc: '停续费' },
  change_plan:         { label: '改套餐',   risk: 'high',     desc: '下期计费变更' },
  subscribe_plan:      { label: '订阅',     risk: 'medium',   desc: '开通订阅' },
  recharge:            { label: '充值',     risk: 'medium',   desc: '资金流入' },
  get_balance:         { label: '查余额',   risk: 'low',      desc: '只读' },
  get_consumption:     { label: '查消费',   risk: 'low',      desc: '只读' },
  get_recent_bills:    { label: '查账单',   risk: 'low',      desc: '只读' },
  list_models:         { label: '查商品',   risk: 'low',      desc: '只读' },
  list_plans:          { label: '查套餐',   risk: 'low',      desc: '只读' },
  get_my_subscription: { label: '查订阅',   risk: 'low',      desc: '只读' },
  cancel_confirmation: { label: '取消确认', risk: 'low',      desc: '用户主动取消' },
}

const meta = (a: AuditLogItem) =>
  TOOL_META[a.Tool] ?? {
    label: a.Tool,
    risk: (WRITE_TOOLS.includes(a.Tool) ? 'high' : 'low') as Risk,
    desc: '',
  }

const isWrite = (a: AuditLogItem) => WRITE_TOOLS.includes(a.Tool)

/** 是否被 Jev 守门拦截（guard 字段形如 decision:block） */
const isBlocked = (a: AuditLogItem) => {
  const g = (a as unknown as { Guard?: string }).Guard
  return !!g && String(g).includes('block')
}

/**
 * 预览行：写操作挂起时写的那一条审计（result = pending_confirmation）。
 * 这是正常流程的一部分——用户还没点确认，所以 confirmed=false 是 **预期** 的，
 * 不能当成异常，否则每笔写操作都会被误报。
 */
const isPreviewRow = (a: AuditLogItem) => a.Result === 'pending_confirmation'

/** 用户主动取消（也没有执行，同样是正常流程） */
const isCancelRow = (a: AuditLogItem) => a.Tool === 'cancel_confirmation'

/** 异常：写操作 **本该执行完** 却未经确认（排除预览行、取消行、被拦截行） */
const isUnconfirmedWrite = (a: AuditLogItem) =>
  isWrite(a) && !a.Confirmed && !isBlocked(a) && !isPreviewRow(a)

const isRisky = (a: AuditLogItem) =>
  meta(a).risk === 'critical' || meta(a).risk === 'high' || isBlocked(a) || isUnconfirmedWrite(a)

/** 从 Args 里抽出金额（元），用于统计与展示 */
function amountOf(a: AuditLogItem): number | null {
  try {
    const j = JSON.parse(a.Args || '{}') as Record<string, unknown>
    const v = j.amount_money ?? j.amount
    return typeof v === 'number' ? v : null
  } catch {
    return null
  }
}

/* ---------- 统计 ---------- */
const stats = computed(() => {
  const all = items.value
  const writes = all.filter(isWrite)
  const confirmedWrites = writes.filter(a => a.Confirmed)
  const refunds = all.filter(a => a.Tool === 'refund_recharge')
  return {
    total: all.length,
    writes: writes.length,
    refunds: refunds.length,
    refundAmount: refunds.reduce((s, a) => s + (amountOf(a) ?? 0), 0),
    confirmRate: writes.length ? Math.round((confirmedWrites.length / writes.length) * 100) : 100,
  }
})

const riskyCount = computed(() => items.value.filter(isRisky).length)

/** 工具分布（排行） */
const toolRank = computed(() => {
  const m = new Map<string, number>()
  for (const a of items.value) m.set(a.Tool, (m.get(a.Tool) ?? 0) + 1)
  return [...m.entries()].map(([name, value]) => ({
    name: TOOL_META[name]?.label ?? name,
    sub: TOOL_META[name]?.desc || (WRITE_TOOLS.includes(name) ? '写操作' : '只读'),
    value,
  }))
})

/** 各租户操作量排行 */
const tenantRank = computed(() => {
  const m = new Map<number, number>()
  for (const a of items.value) m.set(a.TenantID, (m.get(a.TenantID) ?? 0) + 1)
  const byId = new Map(tenants.value.map(t => [t.id, t.name]))
  return [...m.entries()]
    .map(([id, value]) => ({ name: byId.get(id) ?? `租户 #${id}`, sub: `#${id}`, value }))
    .sort((a, b) => b.value - a.value)
})

/** 风险权重：越小越靠前 */
function weight(a: AuditLogItem): number {
  if (isBlocked(a) || isUnconfirmedWrite(a)) return 0
  switch (meta(a).risk) {
    case 'critical': return 1
    case 'high': return 2
    case 'medium': return 3
    default: return 4
  }
}

const shown = computed(() => {
  const list = onlyRisky.value ? items.value.filter(isRisky) : items.value
  return [...list].sort((a, b) => weight(a) - weight(b))
})

const COLS = '76px 132px 54px 130px 74px 1.5fr 122px'

async function loadTenants() {
  try { tenants.value = (await overviewApi()).tenants } catch { /* ignore */ }
}

async function load() {
  loading.value = true
  try {
    items.value = (await auditApi(tid.value, 200)).items
  } catch (e: any) {
    toastErr(e?.message || '加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => { loadTenants(); load() })

/** 是否已执行完毕的写操作（用于确认列展示：已确认 / 待确认 / 已取消） */
function writeState(a: AuditLogItem): { text: string; tone: 'ok' | 'warn' | 'danger' | 'muted' } {
  if (isBlocked(a)) return { text: '已拦截', tone: 'danger' }
  if (isUnconfirmedWrite(a)) return { text: '未确认', tone: 'danger' }
  if (a.Confirmed) return { text: '已确认', tone: 'ok' }
  if (isPreviewRow(a)) return { text: '待确认', tone: 'warn' }
  if (isCancelRow(a)) return { text: '已取消', tone: 'muted' }
  return { text: '未确认', tone: 'muted' }
}

/** 风险标签（行首） */
function riskTag(a: AuditLogItem): { text: string; tone: 'danger' | 'warn' | 'info' | 'muted' } {
  if (isBlocked(a)) return { text: '已拦截', tone: 'danger' }
  if (isUnconfirmedWrite(a)) return { text: '未确认写', tone: 'danger' }
  switch (meta(a).risk) {
    case 'critical': return { text: '高危', tone: 'danger' }
    case 'high': return { text: '较高', tone: 'warn' }
    case 'medium': return { text: '关注', tone: 'info' }
    default: return { text: '常规', tone: 'muted' }
  }
}
</script>

<template>
  <div class="audit">
    <!-- 风险概览 -->
    <div class="kpis">
      <KpiCard
        label="审计记录"
        :value="stats.total"
        :loading="loading"
        :icon="IconSearch"
        sub="最近 200 条"
        chart="none"
      />
      <KpiCard
        label="写操作"
        :value="stats.writes"
        tone="info"
        :loading="loading"
        :icon="IconCoins"
        :sub="`确认率 ${stats.confirmRate}%`"
        chart="none"
      />
      <KpiCard
        label="高危 / 异常"
        :value="riskyCount"
        :tone="riskyCount ? 'danger' : 'ok'"
        :loading="loading"
        :icon="IconAlertTriangle"
        :sub="riskyCount ? '需复核' : '无异常'"
        chart="none"
      />
      <KpiCard
        label="退款金额"
        :value="money(stats.refundAmount * 100)"
        :tone="stats.refundAmount > 0 ? 'warn' : 'default'"
        :loading="loading"
        :icon="IconCoins"
        :sub="`${stats.refunds} 笔退款`"
        chart="none"
      />
    </div>

    <!-- 风险提示 -->
    <div v-if="riskyCount" class="alert">
      <IconAlertTriangle :size="16" class="aic" />
      <div class="atext">
        <strong>发现 {{ riskyCount }} 条需要关注的操作</strong>
        <p>
          包含高危写操作（退款 / 退订 / 改套餐）、被安全策略拦截、或未经用户确认的写操作。
          全部写入 <code>agent_audit_log</code>，可按租户与会话追溯。
        </p>
      </div>
      <BaseButton
        :variant="onlyRisky ? 'primary' : 'secondary'"
        size="sm"
        @click="onlyRisky = !onlyRisky"
      >
        <IconFilter :size="14" /> {{ onlyRisky ? '显示全部' : '只看风险' }}
      </BaseButton>
    </div>
    <div v-else-if="!loading && items.length" class="alert ok">
      <IconCircleCheck :size="16" class="aic ok" />
      <div class="atext">
        <strong>未发现异常操作</strong>
        <p>所有写操作均经过用户确认，没有触发安全策略拦截。</p>
      </div>
    </div>

    <div class="grid2">
      <Card title="工具调用分布" sub="按调用次数排序">
        <RankList :rows="toolRank" :limit="8" />
      </Card>

      <Card title="各租户操作量" sub="按审计记录数">
        <RankList :rows="tenantRank" :limit="6" />
      </Card>
    </div>

    <Card title="风险分级口径">
      <ul class="legend">
        <li>
          <Tag tone="danger">高危</Tag>
          <span><b>退款</b>（refund_recharge）：资金流出，退款量 = min(订单/金额, 当前余额)</span>
        </li>
        <li>
          <Tag tone="warn">较高</Tag>
          <span><b>退订 / 改套餐</b>：影响后续计费周期（当期不退、下期生效）</span>
        </li>
        <li>
          <Tag tone="info">关注</Tag>
          <span><b>充值 / 订阅</b>：资金流入或产生订阅合同</span>
        </li>
        <li>
          <Tag tone="danger">已拦截</Tag>
          <span><b>Jev 守门拦截</b>：合理性置信度低于阈值，操作未执行</span>
        </li>
        <li>
          <Tag tone="danger">未确认写</Tag>
          <span><b>异常</b>：写操作应当执行完毕却未见用户确认（预览行、取消行不计入）</span>
        </li>
        <li>
          <Tag tone="warn">待确认</Tag>
          <span><b>预览行</b>：写操作已挂起等待用户确认，属正常流程</span>
        </li>
        <li>
          <Tag tone="muted">常规</Tag>
          <span><b>只读查询</b>：余额 / 消费 / 账单 / 目录 / 订阅</span>
        </li>
      </ul>
      <div class="note">
        <IconLock :size="13" />
        <span>执行层不信任 LLM：tenant 只来自验证过的 key，写操作必须用户确认，参数服务端重新校验。</span>
      </div>
    </Card>

    <!-- 明细 -->
    <Card :pad="false" title="审计明细" :sub="onlyRisky ? '仅显示风险操作' : '高危置顶，其余按时间'">
      <template #extra>
        <select v-model.number="tid" class="sel" @change="load">
          <option :value="0">全部租户</option>
          <option v-for="t in tenants" :key="t.id" :value="t.id">{{ t.name }} (#{{ t.id }})</option>
        </select>
        <BaseButton variant="secondary" size="sm" :loading="loading" @click="load">刷新</BaseButton>
      </template>

      <DataTable
        :cols="COLS"
        :loading="loading"
        :empty="!shown.length"
        empty-text="暂无审计记录，去 AI 客服聊两句就会产生"
      >
        <template #head>
          <span>风险</span>
          <span>时间</span>
          <span>租户</span>
          <span>操作</span>
          <span>确认</span>
          <span>参数 / 结果</span>
          <span>会话</span>
        </template>

        <div
          v-for="a in shown"
          :key="a.ID"
          class="dt-row dt-num"
          :class="{ 'row-danger': weight(a) <= 1 }"
        >
          <span><Tag :tone="riskTag(a).tone" :dot="weight(a) <= 2">{{ riskTag(a).text }}</Tag></span>
          <span class="dt-muted time">{{ fmtTimeFull(a.CreatedAt) }}</span>
          <span class="dt-muted">#{{ a.TenantID }}</span>
          <span class="tool">
            <span class="tlabel">{{ meta(a).label }}</span>
            <span class="tname mono">{{ a.Tool }}</span>
          </span>
          <span>
            <Tag v-if="isWrite(a)" :tone="writeState(a).tone" dot>{{ writeState(a).text }}</Tag>
            <span v-else-if="a.Tool === 'cancel_confirmation'" class="dt-muted dash">已取消</span>
            <span v-else class="dt-muted dash">-</span>
          </span>
          <span class="detail">
            <span class="dargs mono clip" :title="a.Args">{{ a.Args || a.Preview || '-' }}</span>
            <span v-if="a.Result" class="dres clip" :title="a.Result">{{ a.Result }}</span>
          </span>
          <span class="mono clip dt-muted" :title="a.SessionID">{{ a.SessionID }}</span>
        </div>
      </DataTable>
    </Card>
  </div>
</template>

<style scoped>
.audit { display: flex; flex-direction: column; gap: var(--sp-5); }

.kpis {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: var(--sp-4);
}

/* ---------- 提示条 ---------- */
.alert {
  display: flex; align-items: flex-start; gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5);
  background: var(--danger-bg);
  border: 1px solid var(--danger);
  border-left-width: 3px;
  border-radius: var(--r-sm);
}
.alert.ok { background: var(--ok-bg); border-color: var(--ok); }
.aic { flex: none; color: var(--danger); margin-top: 2px; }
.aic.ok { color: var(--ok); }
.atext { flex: 1; min-width: 0; }
.atext strong {
  display: block;
  font-size: var(--fs-sm); font-weight: 650;
  color: var(--ink-900);
  margin-bottom: 3px;
}
.atext p { font-size: var(--fs-xs); color: var(--ink-600); line-height: 1.6; }
.atext code {
  font-family: var(--font-mono); font-size: .92em;
  background: rgb(255 255 255 / .6);
  padding: 1px 4px; border-radius: var(--r-xs);
}

.grid2 { display: grid; grid-template-columns: 1fr 1.25fr; gap: var(--sp-4); }

/* ---------- 风险口径 ---------- */
.legend {
  list-style: none; margin: 0; padding: 0;
  display: flex; flex-direction: column; gap: 9px;
}
.legend li {
  display: flex; align-items: flex-start; gap: var(--sp-3);
  font-size: var(--fs-xs); color: var(--ink-600); line-height: 1.6;
}
.legend li :deep(.tag) { flex: none; margin-top: 1px; }
.legend b { color: var(--ink-800); }

.note {
  display: flex; align-items: flex-start; gap: 7px;
  margin-top: var(--sp-4);
  padding-top: var(--sp-4);
  border-top: 1px solid var(--hairline);
  font-size: var(--fs-2xs); color: var(--ink-500); line-height: 1.6;
}
.note svg { flex: none; margin-top: 2px; color: var(--accent-600); }

/* ---------- 表格细节 ---------- */
.sel {
  height: 30px; padding: 0 var(--sp-3);
  border: none;
  border-radius: var(--r-sm);
  background: var(--paper);
  color: var(--ink-700);
  font-size: var(--fs-xs);
  min-width: 150px;
}
.sel:focus { outline: none; border-color: var(--accent-500); box-shadow: 0 0 0 3px var(--accent-50); }

.audit :deep(.dt-row.row-danger) { background: color-mix(in srgb, var(--danger) 4%, transparent); }
.audit :deep(.dt-row.row-danger:hover) { background: color-mix(in srgb, var(--danger) 8%, transparent); }

.time { font-size: var(--fs-2xs); white-space: nowrap; }
.tool { display: flex; flex-direction: column; min-width: 0; }
.tlabel { font-size: var(--fs-sm); font-weight: 600; color: var(--ink-800); }
.tname { font-size: var(--fs-2xs); color: var(--ink-400); }
.dash { font-size: var(--fs-xs); }

.detail { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
.dargs { font-size: var(--fs-2xs); color: var(--ink-600); }
.dres { font-size: var(--fs-2xs); color: var(--ink-400); }
.clip { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

@media (max-width: 1080px) {
  .grid2 { grid-template-columns: 1fr; }
}
</style>