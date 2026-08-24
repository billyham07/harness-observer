<script setup lang="ts">
import { computed } from 'vue'
import type { QueryRow } from '@/api/types'
import { t } from '@/i18n'
import { exact, fmt, money, pct, ratioPct } from '@/lib/format'
import { costKnown, costPartial, costPriced } from '@/composables/usePricing'

const props = defineProps<{ row: QueryRow | null }>()

const r = computed<Partial<QueryRow>>(() => props.row ?? {})
const priced = computed(() => costPriced(props.row))
const known = computed(() => costKnown(props.row))
const partial = computed(() => costPartial(props.row))
const coverage = computed(() => Number(r.value.pricing_coverage_ratio || 0))

/**
 * The two headline readings get the type, and everything else is a quiet
 * strip. Ten identically weighted tiles is a wall; two readings and eight
 * supporting figures is an instrument.
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

const tiles = computed(() => [
  {
    key: 'coverage',
    label: t('kpi.coverage'),
    value: priced.value ? ratioPct(coverage.value) : '–',
    sub: priced.value ? t('kpi.coverage.sub', { n: fmt(r.value.unpriced_tokens) }) : '',
    title: t('kpi.coverage.tip'),
    warn: partial.value,
  },
  {
    key: 'cacheHit',
    label: t('kpi.cacheHit'),
    value: pct(r.value.cache_read_tokens, r.value.prompt_tokens),
    sub: t('kpi.cacheHit.sub'),
  },
  { key: 'freshInput', label: t('kpi.freshInput'), value: fmt(r.value.input_tokens), sub: exact(r.value.input_tokens) },
  { key: 'output', label: t('kpi.output'), value: fmt(r.value.output_tokens), sub: exact(r.value.output_tokens) },
  {
    key: 'reasoning',
    label: t('kpi.reasoning'),
    value: fmt(r.value.reasoning_tokens),
    sub: t('kpi.reasoning.sub'),
  },
  {
    key: 'sessions',
    label: t('kpi.sessions'),
    value: exact(r.value.sessions),
    sub: t('kpi.sessions.sub', {
      n: (Number(r.value.turns || 0) / Math.max(1, Number(r.value.sessions || 0))).toFixed(1),
    }),
  },
  { key: 'turns', label: t('kpi.turns'), value: exact(r.value.turns), sub: '' },
  {
    key: 'toolCalls',
    label: t('kpi.toolCalls'),
    value: exact(r.value.tool_calls),
    sub: t('kpi.toolCalls.sub', { n: exact(r.value.tool_errors) }),
  },
])
</script>

<template>
  <section class="kpis" :aria-label="t('kpis.aria')">
    <div class="hero">
      <div class="lead">
        <div class="eyebrow">{{ t('kpi.observed') }}</div>
        <div class="readout num">{{ observed.value }}</div>
        <div class="lead-sub num">{{ observed.sub }}</div>
      </div>
      <div class="lead cost" :class="{ partial }" :title="t('kpi.estCost.tip')">
        <div class="eyebrow">
          {{ t('kpi.estCost') }}
          <span v-if="partial" class="flag" aria-hidden="true">*</span>
        </div>
        <div class="readout num">{{ cost.value }}</div>
        <div class="lead-sub">{{ cost.sub }}</div>
        <!-- Coverage rides with the estimate it qualifies. A dollar figure and
             the share of tokens behind it are one reading, not two. -->
        <div v-if="priced" class="coverage" :aria-label="t('kpi.coverage')">
          <span class="coverage-fill" :style="{ width: `${Math.min(100, coverage * 100).toFixed(2)}%` }" />
        </div>
      </div>
    </div>

    <div class="strip">
      <div v-for="tile in tiles" :key="tile.key" class="tile" :class="{ warn: tile.warn }" :title="tile.title">
        <div class="eyebrow">{{ tile.label }}</div>
        <div class="tile-value num">{{ tile.value || '0' }}</div>
        <div class="tile-sub num">{{ tile.sub }}</div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.kpis {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.hero {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.lead {
  position: relative;
  padding: 22px 24px 24px;
  background: var(--surface);
  border-radius: var(--r-3);
  box-shadow: var(--shadow-1);
  overflow: hidden;
}

.lead:first-child {
  background: var(--wash);
}

/* The readout is the one place type is allowed to be loud. Tight tracking and
 * tabular figures keep it a measurement rather than a headline. */
.readout {
  margin-top: 6px;
  font-size: clamp(32px, 4.4vw, 48px);
  font-weight: 650;
  letter-spacing: -0.035em;
  line-height: 1.02;
  color: var(--ink);
}

.lead-sub {
  margin-top: 6px;
  font-size: 11.5px;
  color: var(--muted);
}

.cost .lead-sub {
  font-family: var(--sans);
}

/* A partial estimate is footnoted, not alarmed: the asterisk and the dotted
 * rule say "this number has a qualifier", and colour stays with the data. */
.cost.partial .readout {
  text-decoration: underline dotted;
  text-decoration-color: var(--line);
  text-underline-offset: 8px;
}

.flag {
  color: var(--muted);
}

.coverage {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 3px;
  background: var(--surface-3);
}

.coverage-fill {
  display: block;
  height: 100%;
  background: var(--accent);
}

.strip {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.tile {
  padding: 16px 18px 18px;
  background: var(--surface);
  border-radius: var(--r-3);
  box-shadow: var(--shadow-1);
}

.tile-value {
  margin-top: 8px;
  font-size: 22px;
  font-weight: 650;
  letter-spacing: -0.03em;
  color: var(--ink);
}

.tile-sub {
  margin-top: 2px;
  font-size: 10.5px;
  color: var(--faint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tile.warn .tile-value {
  text-decoration: underline dotted;
  text-decoration-color: var(--line);
  text-underline-offset: 5px;
}

@media (max-width: 720px) {
  .hero {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
