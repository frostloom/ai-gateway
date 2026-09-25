<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { IconActivity, IconAlertTriangle, IconCircleCheck } from '@tabler/icons-vue'
import { providersApi } from '../api'
import type { ProviderState } from '../types'
import { fmtTimeFull } from '../../shared/utils/format'
import { toastErr } from '../../shared/utils/toast'
import Card from '../../shared/components/ui/Card.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import Skeleton from '../../shared/components/ui/Skeleton.vue'

const items = ref<ProviderState[]>([])
const loading = ref(true)

const stateTag = (s: string) =>
  s === 'closed' ? { text: 'CLOSED', tone: 'ok' as const }
    : s === 'open' ? { text: 'OPEN', tone: 'danger' as const }
      : { text: 'HALF-OPEN', tone: 'warn' as const }

const statusTag = (s: number) =>
  s === 0 ? { text: 'active', tone: 'ok' as const }
    : s === 1 ? { text: 'disabled', tone: 'warn' as const }
      : { text: 'banned', tone: 'danger' as const }

async function load() {
  loading.value = true
  try {
    items.value = await providersApi()
  } catch (e: any) {
    toastErr(e?.message || '获取失败')
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>

<template>
  <div class="providers">
    <!-- 熔断机制说明 -->
    <div class="hint">
      <IconActivity :size="15" class="hic" />
      <div class="htext">
        <strong>熔断状态机</strong>
        <p>
          CLOSED →（5 连败）→ OPEN →（冷却）→ HALF-OPEN（1 次探测）→ 成功则 CLOSED / 失败则 OPEN。
          AutoBan 累计 20 连败写库（status=2），重启不失效，需人工解除。
        </p>
      </div>
    </div>

    <Card title="Provider 状态" :sub="`共 ${items.length} 个上游 · router 内存态 + 持久 AutoBan 分层`">
      <div v-if="loading" class="grid">
        <div v-for="i in 3" :key="i" class="skel"><Skeleton h="150px" /></div>
      </div>

      <div v-else-if="!items.length" class="none">
        <IconAlertTriangle :size="28" />
        <p>还没有接入任何上游 provider</p>
      </div>

      <div v-else class="grid">
        <div
          v-for="p in items"
          :key="p.id"
          class="pcard"
          :class="{ open: p.breaker_state === 'open', banned: p.status === 2 }"
        >
          <div class="ptop">
            <div class="pid">
              <span class="pname">{{ p.name }}</span>
              <span class="pnum num">#{{ p.id }}</span>
            </div>
            <Tag :tone="stateTag(p.breaker_state).tone" dot>
              {{ stateTag(p.breaker_state).text }}
            </Tag>
          </div>

          <p class="url mono clip" :title="p.base_url">{{ p.base_url }}</p>
          <p class="models mono clip" :title="p.models">{{ p.models }}</p>

          <div class="pfacts">
            <span class="fact">
              <i class="fk">权重</i><b class="num">{{ p.weight }}</b>
            </span>
            <span class="fact">
              <i class="fk">连续失败</i>
              <b class="num" :class="{ hot: p.consecutive_fail >= 5 }">{{ p.consecutive_fail }}</b>
            </span>
            <Tag :tone="statusTag(p.status).tone">{{ statusTag(p.status).text }}</Tag>
          </div>

          <p v-if="p.breaker_state === 'open' && p.next_closed_at" class="ntime num">
            预计冷却至 {{ fmtTimeFull(p.next_closed_at) }}
          </p>
          <p v-else-if="p.breaker_state === 'closed' && p.status === 0" class="oktime">
            <IconCircleCheck :size="12" /> 正常服务中
          </p>
        </div>
      </div>
    </Card>
  </div>
</template>

<style scoped>
.providers { display: flex; flex-direction: column; gap: var(--sp-5); }

.hint {
  display: flex; gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5);
  background: var(--paper-3);
  border-left: 2px solid var(--accent-500);
  border-radius: var(--r-sm);
}
.hic { flex: none; color: var(--accent-600); margin-top: 1px; }
.htext strong {
  display: block;
  font-size: var(--fs-sm); font-weight: 620;
  color: var(--ink-800);
  margin-bottom: 3px;
}
.htext p {
  font-size: var(--fs-xs);
  color: var(--ink-500);
  line-height: 1.65;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(272px, 1fr));
  gap: var(--sp-4);
}
.skel { border-radius: var(--r-sm); overflow: hidden; }

.pcard {
  display: flex; flex-direction: column; gap: 7px;
  padding: var(--sp-4);
  background: var(--paper-2);
  border: 1px dashed var(--hairline);
  border-radius: var(--r-sm);
  transition: border-color var(--t-fast), box-shadow var(--t-fast);
}
.pcard:hover { box-shadow: var(--shadow-1); }
.pcard.open { border-color: var(--danger); background: var(--danger-bg); }
.pcard.banned { border-color: var(--danger); }

.ptop {
  display: flex; align-items: center; justify-content: space-between;
  gap: var(--sp-2); margin-bottom: 2px;
}
.pid { display: flex; align-items: baseline; gap: 6px; min-width: 0; }
.pname {
  font-size: var(--fs-sm); font-weight: 640;
  color: var(--ink-900);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.pnum { font-size: var(--fs-2xs); color: var(--ink-400); }

.url { font-size: var(--fs-2xs); color: var(--ink-500); }
.models { font-size: var(--fs-2xs); color: var(--ink-400); }
.clip { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.pfacts {
  display: flex; align-items: center; gap: var(--sp-4);
  flex-wrap: wrap;
  margin-top: 3px;
  padding-top: var(--sp-3);
  border-top: 1px dashed var(--hairline);
}
.fact { display: inline-flex; align-items: baseline; gap: 5px; }
.fk { font-size: var(--fs-2xs); color: var(--ink-400); font-style: normal; }
.fact b { font-size: var(--fs-sm); font-weight: 650; color: var(--ink-800); }
.fact b.hot { color: var(--danger); }

.ntime { font-size: var(--fs-2xs); color: var(--warn); font-weight: 600; }
.oktime {
  display: inline-flex; align-items: center; gap: 4px;
  font-size: var(--fs-2xs); color: var(--ok);
}

.none {
  display: flex; flex-direction: column; align-items: center; gap: var(--sp-2);
  padding: var(--sp-9) 0;
  color: var(--ink-400);
}
.none p { font-size: var(--fs-sm); }
</style>