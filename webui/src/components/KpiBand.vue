<script setup lang="ts">
import { computed } from 'vue'
import type { QueryRow } from '@/api/types'
import { t, type StringKey } from '@/i18n'
import { exact, fmt, money, pct, ratioPct } from '@/lib/format'
import { costKnown, costPartial, costPriced } from '@/composables/usePricing'

const props = defineProps<{ row: QueryRow | null }>()

const r = computed<Partial<QueryRow>>(() => props.row ?? {})
const priced = computed(() => costPriced(props.row))
const known = computed(() => costKnown(props.row))
const partial = computed(() => costPartial(props.row))
const coverage = computed(() => Number(r.value.pricing_coverage_ratio || 0))

/**
 * The period is read like a statement: two totals closed by a double rule,
 * then itemised lines grouped the way the question is asked — how much was
 * used, how much work it was, and how much of it a price list could cover.
 * Ten identically weighted tiles is a wall; a summary is something you read
 * down.
 */
const observed = computed(() => ({
  value: fmt(r.value.observed_tokens),
  sub: t('kpi.observed.sub', { n: exact(r.value.observed_tokens) }),
}))

const cost = computed(() => {
  if (!priced.value) return { value: '–', sub: t('kpi.estCost.none') }
  if (!known.value) return { value: '–', sub: t('kpi.estCost.unpriced') }
  return {
    value: money(r.value.estimated_api_cost_usd),
    sub: partial.value
      ? t('kpi.estCost.partial', { n: ratioPct(coverage.value) })
      : t('kpi.estCost.sub'),
  }
})

interface Line {
  key: string
  label: string
  value: string
  note?: string
  title?: string
  warn?: boolean
}

interface Group {
  key: string
  title: StringKey
  lines: Line[]
}

const groups = computed<Group[]>(() => [
  {
    key: 'usage',
    title: 'summary.usage',
    lines: [
      { key: 'freshInput', label: t('kpi.freshInput'), value: fmt(r.value.input_tokens), title: exact(r.value.input_tokens) },
      {
        key: 'cacheHit',
        label: t('kpi.cacheHit'),
        value: pct(r.value.cache_read_tokens, r.value.prompt_tokens),
        note: t('kpi.cacheHit.sub'),
      },
      { key: 'output', label: t('kpi.output'), value: fmt(r.value.output_tokens), title: exact(r.value.output_tokens) },
      {
        key: 'reasoning',
        label: t('kpi.reasoning'),
        value: fmt(r.value.reasoning_tokens),
        note: t('kpi.reasoning.sub'),
        title: exact(r.value.reasoning_tokens),
      },
    ],
  },
  {
    key: 'activity',
    title: 'summary.activity',
    lines: [
      {
        key: 'sessions',
        label: t('kpi.sessions'),
        value: exact(r.value.sessions),
        note: t('kpi.sessions.sub', {
          n: (Number(r.value.turns || 0) / Math.max(1, Number(r.value.sessions || 0))).toFixed(1),
        }),
      },
      { key: 'turns', label: t('kpi.turns'), value: exact(r.value.turns) },
      {
        key: 'toolCalls',
        label: t('kpi.toolCalls'),
        value: exact(r.value.tool_calls),
        note: t('kpi.toolCalls.sub', { n: exact(r.value.tool_errors) }),
      },
    ],
  },
  {
    key: 'pricing',
    title: 'summary.pricing',
    lines: [
      {
        key: 'coverage',
        label: t('kpi.coverage'),
        value: priced.value ? ratioPct(coverage.value) : '–',
        title: t('kpi.coverage.tip'),
        warn: partial.value,
      },
      {
        key: 'unpriced',
        label: t('summary.unpriced'),
        value: priced.value ? fmt(r.value.unpriced_tokens) : '–',
        title: priced.value ? exact(r.value.unpriced_tokens) : undefined,
        warn: partial.value,
      },
    ],
  },
])
</script>

<template>
  <section class="panel summary" :aria-label="t('kpis.aria')">
    <div class="panel-head">
      <h2>{{ t('summary.head') }}</h2>
    </div>

    <div class="totals">
      <div class="total">
        <div class="total-label">{{ t('kpi.observed') }}</div>
        <div class="amount num" :title="exact(r.observed_tokens)">{{ observed.value }}</div>
        <div class="total-sub num">{{ observed.sub }}</div>
      </div>
      <div class="total cost" :class="{ partial }" :title="t('kpi.estCost.tip')">
        <div class="total-label">
          {{ t('kpi.estCost') }}<span v-if="partial" class="flag" aria-hidden="true">*</span>
        </div>
        <div class="amount num">{{ cost.value }}</div>
        <div class="total-sub">{{ cost.sub }}</div>
        <!-- Coverage rides with the estimate it qualifies. A dollar figure and
             the share of tokens behind it are one reading, not two. -->
        <div
          v-if="priced"
          class="coverage"
          role="meter"
          :aria-label="t('kpi.coverage')"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-valuenow="Math.round(coverage * 100)"
        >
          <span class="coverage-fill" :style="{ width: `${Math.min(100, coverage * 100).toFixed(2)}%` }" />
        </div>
      </div>
    </div>

    <div class="items">
      <div v-for="group in groups" :key="group.key" class="group">
        <h3 class="group-title">{{ t(group.title) }}</h3>
        <div v-for="line in group.lines" :key="line.key" class="line" :class="{ warn: line.warn }" :title="line.title">
          <span class="k">{{ line.label }}</span>
          <span class="leader" aria-hidden="true" />
          <span class="v num">{{ line.value || '0' }}</span>
          <span v-if="line.note" class="note num">{{ line.note }}</span>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.totals {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  border-bottom: 3px double var(--ink);
}

.total {
  position: relative;
  padding: 4px 24px 18px 0;
}

.total + .total {
  padding-left: 24px;
  border-left: 1px solid var(--line);
}

.total-label {
  font-size: 13px;
  font-weight: 550;
  color: var(--muted);
}

/* The amount is the one place type is allowed to be loud. Tight tracking and
 * tabular figures keep it a measurement rather than a headline. */
.amount {
  margin-top: 4px;
  font-size: 36px;
  font-weight: 650;
  letter-spacing: -0.03em;
  line-height: 1.05;
  color: var(--ink);
}

.total-sub {
  margin-top: 6px;
  font-size: 12px;
  color: var(--faint);
}

/* A partial estimate is footnoted, not alarmed: the asterisk and the dotted
 * rule say "this number has a qualifier", and colour stays with the data. */
.cost.partial .amount {
  text-decoration: underline dotted 2px;
  text-decoration-color: var(--line);
  text-underline-offset: 8px;
}

.flag {
  margin-left: 2px;
  color: var(--muted);
}

.coverage {
  position: absolute;
  left: 24px;
  right: 0;
  bottom: 6px;
  height: 3px;
  background: var(--surface-3);
}

.coverage-fill {
  display: block;
  height: 100%;
  background: var(--rule);
}

.items {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0 40px;
  padding-top: 16px;
}

.group {
  margin: 0;
  min-width: 0;
}

.group-title {
  margin: 0;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--line);
  font-size: 12px;
  font-weight: 650;
  color: var(--ink);
}

/* An itemised line: label, a dotted leader, the figure flush right — the
 * layout a bill uses so the eye can run from a name to its amount. */
.line {
  display: grid;
  grid-template-columns: auto minmax(12px, 1fr) auto;
  align-items: baseline;
  column-gap: 8px;
  padding: 7px 0;
  border-bottom: 1px solid var(--line-soft);
  font-size: 13.5px;
}

.line .k {
  color: var(--muted);
  white-space: nowrap;
}

.leader {
  align-self: end;
  margin-bottom: 5px;
  border-bottom: 1px dotted var(--line);
}

.line .v {
  font-weight: 600;
  color: var(--ink);
  text-align: right;
}

.note {
  grid-column: 1 / -1;
  margin-top: 1px;
  font-size: 11.5px;
  color: var(--faint);
}

.line.warn .v {
  text-decoration: underline dotted;
  text-decoration-color: var(--warn);
  text-underline-offset: 4px;
}

@media (max-width: 1000px) {
  .items {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    row-gap: 20px;
  }
}

@media (max-width: 640px) {
  .totals,
  .items {
    grid-template-columns: minmax(0, 1fr);
  }
  .total + .total {
    padding-left: 0;
    padding-top: 14px;
    border-left: 0;
    border-top: 1px solid var(--line);
  }
  .coverage {
    left: 0;
  }
  .amount {
    font-size: 30px;
  }
}
</style>
