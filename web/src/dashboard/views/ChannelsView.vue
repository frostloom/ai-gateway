<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { IconPlus } from '@tabler/icons-vue'
import { channelsApi, upsertChannelApi } from '../api'
import type { ProviderMargin } from '../types'
import { money, fmt } from '../../shared/utils/format'
import { toastOk, toastErr } from '../../shared/utils/toast'
import Card from '../../shared/components/ui/Card.vue'
import Tag from '../../shared/components/ui/Tag.vue'
import DataTable from '../../shared/components/ui/DataTable.vue'
import BaseButton from '../../shared/components/ui/BaseButton.vue'
import Modal from '../../shared/components/ui/Modal.vue'
import Field from '../../shared/components/ui/Field.vue'

/** 渠道表列宽 */
const COLS = '40px 120px 1.3fr 1fr 48px 68px 92px 84px 84px 84px 78px'

const items = ref<ProviderMargin[]>([])
const loading = ref(true)

const addOpen = ref(false)
const busy = ref(false)
const form = ref({
  name: '', base_url: '', upstream_key: '', models: '', weight: 1, status: 0,
  cost_in_cent: 0, cost_out_cent: 0,
})

const statusTag = (s: number) => s === 0 ? { text: '启用', tone: 'ok' as const } : s === 1 ? { text: '停用', tone: 'warn' as const } : { text: '封禁', tone: 'danger' as const }

async function load() { loading.value = true; try { items.value = (await channelsApi()).items } finally { loading.value = false } }
onMounted(load)

function openAdd() {
  addOpen.value = true
  form.value = { name: '', base_url: '', upstream_key: '', models: '', weight: 1, status: 0, cost_in_cent: 0, cost_out_cent: 0 }
}

async function save() {
  if (!form.value.name.trim() || !form.value.base_url.trim()) { toastErr('name 与 base_url 必填'); return }
  busy.value = true
  try {
    await upsertChannelApi({
      name: form.value.name.trim(), base_url: form.value.base_url.trim(),
      ...(form.value.upstream_key ? { upstream_key: form.value.upstream_key } : {}),
      models: form.value.models.trim() ? form.value.models : undefined,
      weight: Number(form.value.weight), status: Number(form.value.status),
      cost_in_cent: Number(form.value.cost_in_cent), cost_out_cent: Number(form.value.cost_out_cent),
    })
    toastOk('渠道已保存')
    addOpen.value = false
    await load()
  } catch (e: any) { toastErr(e?.message || '保存失败') }
  finally { busy.value = false }
}
</script>

<template>
  <div class="channels">
    <div class="head">
      <p class="count num">共 {{ items.length }} 个渠道 · 售价 − 上游成本 = 毛利（分/百万 token 口径为快照累计）</p>
      <BaseButton variant="primary" @click="openAdd"><IconPlus :size="15" /> 新增/更新渠道</BaseButton>
    </div>

    <Card :pad="false">
      <DataTable
        :cols="COLS"
        :loading="loading"
        :empty="!items.length"
        empty-text="还没有接入任何上游渠道"
      >
        <template #head>
          <span>ID</span>
          <span>渠道</span>
          <span>base_url</span>
          <span>models</span>
          <span>权重</span>
          <span>状态</span>
          <span>成本 in/out</span>
          <span>累计售价</span>
          <span>累计成本</span>
          <span>毛利</span>
          <span>连续失败</span>
        </template>

        <div v-for="c in items" :key="c.id" class="dt-row dt-num">
          <span class="dt-muted">{{ c.id }}</span>
          <span class="dt-strong clip" :title="c.name">{{ c.name }}</span>
          <span class="mono clip dt-muted" :title="c.base_url">{{ c.base_url }}</span>
          <span class="mono clip dt-muted" :title="c.models">{{ c.models }}</span>
          <span>{{ c.weight }}</span>
          <span>
            <Tag :tone="statusTag(c.status).tone" dot>{{ statusTag(c.status).text }}</Tag>
          </span>
          <span class="mono">{{ c.cost_in_cent }} / {{ c.cost_out_cent }}</span>
          <span>{{ money(c.selling_cent) }}</span>
          <span class="dt-muted">{{ money(c.cost_cent) }}</span>
          <span class="margin" :class="{ neg: c.margin_cent < 0 }">{{ money(c.margin_cent) }}</span>
          <span :class="c.consecutive_fail >= 5 ? 'hot' : 'dt-muted'">
            {{ fmt(c.consecutive_fail) }}
          </span>
        </div>
      </DataTable>
    </Card>

    <Modal :open="addOpen" title="新增 / 更新渠道" sub="按 name 幂等：已存在则更新参数" @close="addOpen = false" width="520">
      <div class="form2">
        <div class="two">
          <Field label="渠道名" required><input v-model="form.name" placeholder="如 deepseek-official" /></Field>
          <Field label="base_url" required><input v-model="form.base_url" placeholder="https://api.deepseek.com" /></Field>
        </div>
        <Field label="上游 API key" hint="留空不修改（响应不回显明文）"><input v-model="form.upstream_key" type="password" /></Field>
        <Field label="models" hint='JSON 数组串，如 ["deepseek-chat"] 或 ["*"]'><input v-model="form.models" placeholder='["deepseek-chat"]' /></Field>
        <div class="two">
          <Field label="权重"><input v-model.number="form.weight" type="number" min="1" /></Field>
          <Field label="状态">
            <select v-model.number="form.status"><option :value="0">启用</option><option :value="1">停用</option><option :value="2">封禁</option></select>
          </Field>
        </div>
        <div class="two">
          <Field label="成本 in（分/M）"><input v-model.number="form.cost_in_cent" type="number" min="0" /></Field>
          <Field label="成本 out（分/M）"><input v-model.number="form.cost_out_cent" type="number" min="0" /></Field>
        </div>
      </div>
      <template #footer>
        <BaseButton variant="ghost" @click="addOpen = false">取消</BaseButton>
        <BaseButton variant="primary" :loading="busy" @click="save">保存</BaseButton>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.head {
  display: flex; align-items: center; justify-content: space-between;
  gap: var(--sp-4); margin-bottom: var(--sp-4); flex-wrap: wrap;
}
.count { font-size: var(--fs-xs); color: var(--ink-400); }

.clip { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.margin { font-weight: 660; color: var(--ok); }
.margin.neg { color: var(--danger); }
.hot { color: var(--danger); font-weight: 650; }

.form2 { display: flex; flex-direction: column; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: var(--sp-3); }

@media (max-width: 880px) {
  .two { grid-template-columns: 1fr; }
}
</style>