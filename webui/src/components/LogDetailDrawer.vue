<script setup lang="ts">
import { computed, ref } from 'vue'
import { NDrawer, NDrawerContent } from 'naive-ui'
import type { LogRow } from '@/api/types'
import { t } from '@/i18n'
import { UNKNOWN, dash, exact, fmtTime, money, ratioPct } from '@/lib/format'

const props = defineProps<{ row: LogRow | null }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const copied = ref('')

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    copied.value = value
    setTimeout(() => (copied.value = ''), 1200)
  } catch {
    /* Clipboard denied: the value is on screen and selectable anyway. */
  }
}

interface Entry {
  label: string
  value: string
  copy?: string | null
}

const optional = (v: number | null | undefined) => (v === null || v === undefined ? UNKNOWN : exact(v))
const optionalMoney = (v: number | null | undefined) => (v === null || v === undefined ? UNKNOWN : money(v))

const sections = computed<{ title: string; entries: Entry[] }[]>(() => {
  const row = props.row
  if (!row) return []
  const cost = row.estimated_cost
  return [
    {
      title: t('logs.sec.identity'),
      entries: [
        { label: t('logs.col.time'), value: fmtTime(row.timestamp) },
        { label: t('logs.col.harness'), value: row.harness },
        { label: t('facet.client'), value: dash(row.client) },
        { label: t('facet.provider'), value: dash(row.provider) },
        { label: t('logs.col.model'), value: dash(row.model) },
        { label: t('logs.col.effort'), value: dash(row.reasoning_effort) },
        { label: t('logs.col.project'), value: dash(row.project) },
        { label: t('logs.col.event'), value: row.event_kind },
        { label: 'session_id', value: row.session_id, copy: row.session_id },
        { label: 'turn_id', value: dash(row.turn_id), copy: row.turn_id },
        { label: 'record_id', value: row.record_id, copy: row.record_id },
      ],
    },
    {
      title: t('logs.sec.usage'),
      entries: [
        { label: t('logs.col.input'), value: exact(row.input_tokens) },
        { label: t('logs.col.cacheRead'), value: exact(row.cache_read_tokens) },
        { label: t('logs.col.cacheWrite'), value: exact(row.cache_write_tokens) },
        { label: t('logs.col.output'), value: exact(row.output_tokens) },
        { label: t('kpi.reasoning'), value: exact(row.reasoning_tokens) },
        { label: 'prompt', value: exact(row.prompt_tokens) },
        { label: 'observed', value: exact(row.observed_tokens) },
        {
          label: t('logs.col.speed'),
          value:
            row.est_output_tokens_per_second === null ? UNKNOWN : row.est_output_tokens_per_second.toFixed(1),
        },
        { label: t('logs.interval'), value: optional(row.interval_ms) },
        { label: t('facet.semantics'), value: row.usage_semantics },
        { label: t('facet.basis'), value: row.usage_basis },
      ],
    },
    {
      title: t('logs.sec.tools'),
      entries: [
        { label: t('logs.col.tools'), value: exact(row.tool_calls) },
        { label: t('logs.col.toolErrors'), value: optional(row.tool_errors) },
        { label: t('logs.evidence'), value: row.tool_error_evidence || UNKNOWN },
      ],
    },
    {
      title: t('logs.sec.cost'),
      entries: [
        { label: t('logs.reported'), value: optionalMoney(row.reported_cost_usd) },
        { label: t('logs.col.estCost'), value: optionalMoney(cost?.estimated_api_cost_usd ?? null) },
        { label: t('col.coverage'), value: cost ? ratioPct(cost.pricing_coverage_ratio) : UNKNOWN },
        {
          label: t('logs.match'),
          value: cost
            ? dash(cost.match_rule) + (cost.canonical_model ? ` · ${cost.canonical_model}` : '')
            : UNKNOWN,
        },
      ],
    },
    {
      // Provenance is not an appendix. Every number above is traceable to this
      // file and line, which is the whole claim the product makes.
      title: t('logs.sec.evidence'),
      entries: [
        { label: t('logs.sourceFile'), value: row.source_file, copy: row.source_file },
        { label: t('logs.sourceLine'), value: String(row.source_line) },
      ],
    },
  ]
})
</script>

<template>
  <NDrawer :show="!!row" :width="430" placement="right" @update:show="!$event && emit('close')">
    <NDrawerContent :title="t('logs.detail')" closable>
      <div v-if="row" class="detail">
        <section v-for="section in sections" :key="section.title">
          <h3 class="eyebrow">{{ section.title }}</h3>
          <dl class="kv">
            <template v-for="entry in section.entries" :key="entry.label">
              <dt>{{ entry.label }}</dt>
              <dd>
                <span class="num" :class="{ unknown: entry.value === '—' }">{{ entry.value }}</span>
                <button
                  v-if="entry.copy"
                  type="button"
                  class="btn btn-ghost copy"
                  @click="copy(entry.copy)"
                >
                  {{ copied === entry.copy ? t('logs.copied') : t('logs.copy') }}
                </button>
              </dd>
            </template>
          </dl>
        </section>
      </div>
    </NDrawerContent>
  </NDrawer>
</template>

<style scoped>
.detail {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

h3 {
  margin-bottom: 8px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--line-soft);
  font-size: 13px;
  font-weight: 600;
  color: var(--ink);
}

.kv {
  display: grid;
  grid-template-columns: 118px minmax(0, 1fr);
  gap: 4px 12px;
  margin: 0;
  font-size: 12px;
}

.kv dt {
  color: var(--faint);
  overflow-wrap: anywhere;
}

.kv dd {
  display: flex;
  align-items: baseline;
  gap: 6px;
  margin: 0;
  color: var(--ink);
  overflow-wrap: anywhere;
}

.copy {
  height: 20px;
  padding: 0 6px;
  font-size: 10.5px;
}
</style>
