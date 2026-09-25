<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { billsApi, overviewApi } from '../api'
import type { Bill, TenantStat } from '../types'
import { BILL_STATUS, money, fmtTime, fmt } from '../../shared/utils/format'
import Card from '../../shared/components/ui/Card.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import DataTable from '../../shared/components/ui/DataTable.vue'
import Pagination from '../../shared/components/ui/Pagination.vue'

const tenants = ref<TenantStat[]>([])
const items = ref<Bill[]>([])
const total = ref(0)
const loading = ref(true)

const f = ref({ tenant: '', phase: '', status: '' })
const limit = 20
const offset = ref(0)

const COLS = '52px 1.3fr 52px 1fr 1fr .8fr .7fr .7fr 1fr .7fr .9fr'

async function load() {
  loading.value = true
  try {
    const q: Record<string, string | number> = { limit, offset: offset.value }
    if (f.value.tenant) q.tenant_id = f.value.tenant
    if (f.value.phase) q.phase = f.value.phase
    if (f.value.status) q.status = f.value.status
    const r = await billsApi(q)
    items.value = r.items
    total.value = r.total
  } finally {
    loading.value = false
  }
}

async function loadTenants() {
  try { tenants.value = (await overviewApi()).tenants } catch { /* ignore */ }
}

onMounted(() => { loadTenants(); load() })

const phaseTag = (p: string) =>
  p === 'reserve' ? { text: '预占', tone: 'muted' as const } : { text: '结算', tone: 'info' as const }
const fundingTag = (s: string) =>
  s === 'subscription' ? { text: '订阅额度', tone: 'accent' as const } : { text: '余额', tone: 'muted' as const }

function page(p: number) {
  offset.value = (p - 1) * limit
  load()
  window.scrollTo({ top: 0, behavior: 'smooth' })
}
</script>

<template>
  <div class="bills">
    <!-- 筛选条 -->
    <div class="bar">
      <select v-model="f.tenant" class="sel" @change="offset = 0; load()">
        <option value="">全部租户</option>
        <option v-for="t in tenants" :key="t.id" :value="t.id">{{ t.name }} (#{{ t.id }})</option>
      </select>
      <select v-model="f.phase" class="sel" @change="offset = 0; load()">
        <option value="">全部阶段</option>
        <option value="reserve">reserve 预占</option>
        <option value="settle">settle 结算</option>
      </select>
      <select v-model="f.status" class="sel" @change="offset = 0; load()">
        <option value="">全部状态</option>
        <option value="settled">settled</option>
        <option value="pending">pending</option>
        <option value="reversed">reversed</option>
      </select>
      <span class="count num">共 {{ fmt(total) }} 条</span>
    </div>

    <Card :pad="false">
      <DataTable
        :cols="COLS"
        :loading="loading"
        :empty="!items.length"
        empty-text="没有符合条件的账单"
      >
        <template #head>
          <span>#</span>
          <span>request_id</span>
          <span>租户</span>
          <span>模型</span>
          <span>阶段 / 资金</span>
          <span>状态</span>
          <span>预占</span>
          <span>实际</span>
          <span>token (p/c)</span>
          <span>错误</span>
          <span>时间</span>
        </template>

        <div v-for="b in items" :key="b.ID" class="dt-row dt-num">
          <span class="dt-muted">{{ b.ID }}</span>
          <span class="mono clip" :title="b.RequestID">{{ b.RequestID }}</span>
          <span class="dt-muted">{{ b.TenantID }}</span>
          <span class="mono clip" :title="b.Model">{{ b.Model }}</span>
          <span class="tags">
            <Tag :tone="phaseTag(b.Phase).tone">{{ phaseTag(b.Phase).text }}</Tag>
            <Tag :tone="fundingTag(b.Funding).tone">{{ fundingTag(b.Funding).text }}</Tag>
          </span>
          <span>
            <Tag :tone="BILL_STATUS[b.Status]?.tone ?? 'muted'" dot>
              {{ BILL_STATUS[b.Status]?.text ?? b.Status }}
            </Tag>
          </span>
          <span>{{ money(b.PreQuota) }}</span>
          <span :class="(b.DeltaQuota ?? 0) < 0 ? 'refund' : ''">
            {{ b.ActualQuota != null ? money(b.ActualQuota) : '-' }}
          </span>
          <span class="mono dt-muted">
            {{ b.PromptTokens ?? '-' }} / {{ b.CompletionTokens ?? '-' }}
          </span>
          <span class="err clip" :title="b.ErrorCode">{{ b.ErrorCode || '-' }}</span>
          <span class="dt-muted">{{ fmtTime(b.CreatedAt) }}</span>
        </div>

        <template #footer>
          <Pagination
            :total="total"
            :page="Math.floor(offset / limit) + 1"
            :size="limit"
            @change="page"
          />
        </template>
      </DataTable>
    </Card>
  </div>
</template>

<style scoped>
.bills { display: flex; flex-direction: column; gap: var(--sp-4); }

.bar { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.sel {
  height: 34px; padding: 0 var(--sp-3);
  border: none;
  border-radius: var(--r-sm);
  background: var(--paper);
  color: var(--ink-700);
  font-size: var(--fs-sm);
  transition: border-color var(--t-fast);
}
.sel:hover { border-color: var(--line-strong); }
.sel:focus { outline: none; border-color: var(--accent-500); box-shadow: 0 0 0 3px var(--accent-50); }
.count { font-size: var(--fs-xs); color: var(--ink-400); margin-left: auto; }

.tags { display: flex; gap: 4px; flex-wrap: wrap; }
.clip { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mono { font-family: var(--font-mono); font-size: var(--fs-2xs); }
.refund { color: var(--warn); font-weight: 620; }
.err { color: var(--danger); font-size: var(--fs-2xs); }
</style>