<script setup lang="ts">
import { computed, h, ref, watch } from 'vue'
import { NDataTable, NInput, NPagination, NSelect } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { LogRow } from '@/api/types'
import { t } from '@/i18n'
import { UNKNOWN, exact, fmtTime, money, shortId } from '@/lib/format'
import LogDetailDrawer from '@/components/LogDetailDrawer.vue'
import { useLogs } from '@/composables/useLogs'
import {
  filterSignature,
  setLogsPage,
  setLogsPageSize,
  setLogsSession,
  state,
} from '@/composables/useQueryState'

const { page, loading, error, openRecord, load, openDrawer, closeDrawer, emptyMessage } = useLogs()

// Paging and page size are part of what the server is asked for, so they join
// the filter in the one watcher that decides when to re-query. Nothing here
// slices a local array.
const request = computed(() => `${filterSignature.value}|${state.logsPage}|${state.logsPageSize}`)
watch(request, () => void load(), { immediate: true })

const sessionInput = ref(state.logsSession)
watch(
  () => state.logsSession,
  (v) => (sessionInput.value = v),
)

const rows = computed(() => page.value?.rows ?? [])
const openRow = computed<LogRow | null>(
  () => rows.value.find((r) => r.record_id === openRecord.value) ?? null,
)

const zone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'local'

const count = computed(() =>
  page.value?.total
    ? t('logs.count', { n: exact(page.value.total), page: page.value.page, pages: page.value.pages })
    : '',
)

const sizeOptions = [20, 50, 100].map((n) => ({ label: String(n), value: n }))

/** An unrecorded value renders as unknown. Never as a zero. */
const textCell = (v: string | null | undefined) =>
  v === null || v === undefined || v === '' ? h('span', { class: 'unknown' }, UNKNOWN) : v
const numCell = (v: number) => h('span', { class: 'num' }, exact(v))
const optionalNum = (v: number | null | undefined, cls = 'num') =>
  v === null || v === undefined ? h('span', { class: 'unknown' }, UNKNOWN) : h('span', { class: cls }, exact(v))

const columns = computed<DataTableColumns<LogRow>>(() => [
  {
    title: t('logs.col.time'),
    key: 'timestamp',
    width: 172,
    render: (r) => h('span', { class: 'num', title: r.timestamp }, fmtTime(r.timestamp)),
  },
  { title: t('logs.col.harness'), key: 'harness', width: 128 },
  { title: t('logs.col.model'), key: 'model', width: 186, render: (r) => textCell(r.model) },
  { title: t('logs.col.effort'), key: 'reasoning_effort', width: 96, render: (r) => textCell(r.reasoning_effort) },
  { title: t('logs.col.project'), key: 'project', width: 158, render: (r) => textCell(r.project) },
  { title: t('logs.col.event'), key: 'event_kind', width: 110 },
  { title: t('logs.col.input'), key: 'input_tokens', width: 96, align: 'right', render: (r) => numCell(r.input_tokens) },
  {
    title: t('logs.col.cacheRead'),
    key: 'cache_read_tokens',
    width: 108,
    align: 'right',
    render: (r) => numCell(r.cache_read_tokens),
  },
  {
    title: t('logs.col.cacheWrite'),
    key: 'cache_write_tokens',
    width: 110,
    align: 'right',
    render: (r) => numCell(r.cache_write_tokens),
  },
  {
    title: t('logs.col.output'),
    key: 'output_tokens',
    width: 96,
    align: 'right',
    render: (r) => numCell(r.output_tokens),
  },
  { title: t('logs.col.tools'), key: 'tool_calls', width: 80, align: 'right', render: (r) => numCell(r.tool_calls) },
  {
    title: t('logs.col.toolErrors'),
    key: 'tool_errors',
    width: 104,
    align: 'right',
    render: (r) => optionalNum(r.tool_errors, Number(r.tool_errors) > 0 ? 'num warn-text' : 'num'),
  },
  {
    title: t('logs.col.estCost'),
    key: 'estimated_cost',
    width: 120,
    align: 'right',
    render: (r) => {
      const value = r.estimated_cost?.estimated_api_cost_usd
      return value === null || value === undefined
        ? h('span', { class: 'unknown' }, UNKNOWN)
        : h('span', { class: 'num' }, money(value))
    },
  },
  {
    title: t('logs.col.session'),
    key: 'session_id',
    width: 130,
    render: (r) => h('span', { class: 'num', title: r.session_id }, shortId(r.session_id)),
  },
])

const rowProps = (row: LogRow) => ({
  class: openRecord.value === row.record_id ? 'is-open' : '',
  style: 'cursor: pointer',
  onClick: () => openDrawer(row),
  tabindex: '0',
  onKeydown: (e: KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      openDrawer(row)
    }
  },
})

function commitSession() {
  if (sessionInput.value.trim() !== state.logsSession) setLogsSession(sessionInput.value)
}
</script>

<template>
  <div class="logs">
    <div class="toolbar">
      <label class="field id">
        <span class="eyebrow">{{ t('logs.id') }}</span>
        <NInput
          :value="sessionInput"
          size="small"
          clearable
          :placeholder="t('logs.id.ph')"
          @update:value="sessionInput = $event"
          @change="commitSession"
          @keyup.enter="commitSession"
          @clear="setLogsSession('')"
        />
      </label>
      <label class="field size">
        <span class="eyebrow">{{ t('logs.pageSize') }}</span>
        <NSelect
          size="small"
          :value="state.logsPageSize"
          :options="sizeOptions"
          @update:value="setLogsPageSize($event as number)"
        />
      </label>
      <span class="muted">{{ t('logs.tz', { zone }) }}</span>
      <span class="muted num count">{{ count }}</span>
    </div>

    <div class="panel table-panel">
      <NDataTable
        :columns="columns"
        :data="rows"
        :loading="loading"
        :row-key="(row: LogRow) => row.record_id"
        :row-props="rowProps"
        :bordered="false"
        :single-line="false"
        :scroll-x="1600"
        max-height="calc(100vh - 290px)"
        size="small"
      >
        <template #empty>
          <div class="empty">{{ error || emptyMessage(page) }}</div>
        </template>
      </NDataTable>
    </div>

    <div v-if="page && (page.pages > 1 || page.page > 1)" class="pager">
      <NPagination
        :page="page.page"
        :page-count="Math.max(1, page.pages)"
        :page-slot="7"
        @update:page="setLogsPage"
      />
      <span class="muted num">{{ t('logs.page', { page: page.page, pages: page.pages }) }}</span>
    </div>

    <LogDetailDrawer :row="openRow" @close="closeDrawer" />
  </div>
</template>

<style scoped>
.logs {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px var(--gutter) 32px;
}

.toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 12px 16px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.id {
  width: 260px;
}

.size {
  width: 84px;
}

.toolbar .muted {
  padding-bottom: 5px;
  font-size: 11.5px;
}

.count {
  margin-left: auto;
}

.table-panel {
  overflow: hidden;
}

.table-panel :deep(.n-data-table-th),
.table-panel :deep(.n-data-table-td) {
  padding: 5px 10px;
  font-size: 12.5px;
}

.table-panel :deep(.n-data-table-th) {
  font-size: 10.5px;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.table-panel :deep(.is-open td) {
  background: var(--accent-soft);
}

.table-panel :deep(.warn-text) {
  color: var(--warn);
}

.pager {
  display: flex;
  align-items: center;
  gap: 14px;
}
</style>
