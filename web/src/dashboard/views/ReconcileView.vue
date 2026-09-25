<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { IconScale, IconAlertTriangle } from '@tabler/icons-vue'
import { reconcileApi } from '../api'
import type { ReconcileResp } from '../types'
import { money, fmt } from '../../shared/utils/format'
import { toastErr } from '../../shared/utils/toast'
import Card from '../../shared/components/ui/Card.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import StatCard from '../../shared/components/ui/StatCard.vue'
import DataTable from '../../shared/components/ui/DataTable.vue'

const r = ref<ReconcileResp | null>(null)
const busy = ref(false)

const COLS = '70px 1fr 140px 140px 120px'

async function check() {
  busy.value = true
  try {
    r.value = await reconcileApi()
  } catch (e: any) {
    toastErr(e?.message || '检查失败')
  } finally {
    busy.value = false
  }
}

onMounted(check)
</script>

<template>
  <div class="recon">
    <!-- 机制说明 -->
    <Card>
      <div class="intro">
        <div class="ihead">
          <IconScale :size="17" class="iic" />
          <h3>对账机制</h3>
        </div>
        <p class="ip">
          Redis 余额是投影，MySQL <code>bills</code> 是权威账本。
          reconciler 周期扫描滞留 <code>pending</code> 账单，按 marker 一锤定音：
        </p>
        <ul class="il">
          <li><b>无</b> <code>usage_reported_at</code>：流从未产出完整结果 → <span class="rev">Reverse 全退</span></li>
          <li><b>有</b> <code>usage_reported_at</code>：流已跑完但结算丢失 → <span class="set">补 Settle</span></li>
        </ul>
        <BaseButton variant="primary" :loading="busy" @click="check">
          {{ busy ? '检查中' : '立即检查一致性' }}
        </BaseButton>
      </div>
    </Card>

    <template v-if="r">
      <div class="stats">
        <StatCard label="检查租户数" :value="fmt(r.checked)" tone="default" />
        <StatCard
          label="一致性结论"
          :value="r.ok ? '全部一致' : `${r.inconsistent.length} 个不一致`"
          :tone="r.ok ? 'ok' : 'danger'"
          :sub="r.ok ? '账本 == Redis 投影' : '需人工介入排查'"
        />
      </div>

      <Card
        title="不一致租户明细"
        :sub="r.ok ? '当前无差异' : '账本口径 vs Redis 投影（分）'"
        :pad="false"
      >
        <DataTable
          :cols="COLS"
          :empty="!r.inconsistent.length"
          empty-text="所有租户账本与投影一致"
        >
          <template #head>
            <span>ID</span>
            <span>租户</span>
            <span>账本（分）</span>
            <span>Redis（分）</span>
            <span>差值</span>
          </template>
          <div v-for="i in r.inconsistent" :key="i.id" class="dt-row dt-num">
            <span class="dt-muted">{{ i.id }}</span>
            <span class="dt-strong">{{ i.name }}</span>
            <span>{{ money(i.ledger_cent) }}</span>
            <span>{{ money(i.redis_cent) }}</span>
            <span class="diff">{{ money(i.redis_cent - i.ledger_cent) }}</span>
          </div>
        </DataTable>
      </Card>
    </template>

    <Card v-else-if="!busy" class="emptycard">
      <div class="estate">
        <IconAlertTriangle :size="26" />
        <p>尚未检查</p>
        <span>点击上方按钮开始一致性检查</span>
      </div>
    </Card>
  </div>
</template>

<style scoped>
.recon { display: flex; flex-direction: column; gap: var(--sp-5); }

/* ---------- 机制说明 ---------- */
.intro { display: flex; flex-direction: column; align-items: flex-start; gap: var(--sp-3); }
.ihead { display: flex; align-items: center; gap: 8px; }
.iic { color: var(--accent-600); }
.ihead h3 { font-size: var(--fs-md); font-weight: 640; letter-spacing: -.014em; }
.ip { font-size: var(--fs-sm); color: var(--ink-600); line-height: 1.7; max-width: 68ch; }
.ip code,
.il code {
  font-family: var(--font-mono);
  font-size: .92em;
  background: var(--paper-3);
  color: var(--ink-700);
  padding: 1px 5px;
  border-radius: var(--r-xs);
}
.il {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex; flex-direction: column; gap: 7px;
  width: 100%;
}
.il li {
  font-size: var(--fs-sm);
  color: var(--ink-600);
  padding: var(--sp-3) var(--sp-4);
  background: var(--paper-2);
  border-left: 2px solid var(--line-2);
  border-radius: var(--r-sm);
}
.il b { color: var(--ink-800); }
.rev { color: var(--warn); font-weight: 620; }
.set { color: var(--accent-700); font-weight: 620; }

.stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
  gap: var(--sp-4);
}

.diff { color: var(--danger); font-weight: 660; }

.estate {
  display: flex; flex-direction: column; align-items: center; gap: 7px;
  padding: var(--sp-9) 0;
  color: var(--ink-400);
}
.estate p { font-size: var(--fs-sm); font-weight: 600; color: var(--ink-600); }
.estate span { font-size: var(--fs-xs); }
.emptycard { border-style: dashed; }
</style>