<script setup lang="ts">
import { computed } from 'vue'
import type { QueryRow } from '@/api/types'
import { t } from '@/i18n'
import { exact, fmt, money, pct } from '@/lib/format'
import { SERIES } from '@/lib/palette'
import { costKnown, costPartial, costPriced, usePricing } from '@/composables/usePricing'

const props = defineProps<{ row: QueryRow | null }>()
const { status } = usePricing()

/**
 * The period's tokens as an itemised bill: one line per bucket with its
 * quantity, its share of the tokens and of the cost side by side, the amount,
 * and the effective rate. The last column is the reading this section exists
 * for. Relative cost (share of cost ÷ share of tokens) says outright what
 * two stacked bars only implied: cache read can be nearly every token and
 * still be cheap per token, while output is a sliver of the tokens and a
 * large slice of the bill.
 */
interface Line {
  bucket: string
  label: string
  cssVar: string
  tokens: number
  cost: number
  tokenShare: number
  costShare: number
  rate: number | null
  weight: number | null
}

const tokenTotal = computed(() => SERIES.reduce((sum, s) => sum + Number(props.row?.[s.tokens] || 0), 0))
const costTotal = computed(() => SERIES.reduce((sum, s) => sum + Number(props.row?.[s.cost] || 0), 0))
const priced = computed(() => costPriced(props.row) && costKnown(props.row))
const relativeKnown = computed(() => priced.value && costTotal.value > 0)
const partial = computed(() => priced.value && costPartial(props.row))

const lines = computed<Line[]>(() =>
  SERIES.map((s) => {
    const tokens = Number(props.row?.[s.tokens] || 0)
    const cost = Number(props.row?.[s.cost] || 0)
    const tokenShare = tokenTotal.value > 0 ? tokens / tokenTotal.value : 0
    const costShare = costTotal.value > 0 ? cost / costTotal.value : 0
    return {
      bucket: s.bucket,
      label: t(s.label),
      cssVar: s.cssVar,
      tokens,
      cost,
      tokenShare,
      costShare,
      rate: priced.value && tokens > 0 ? (cost / tokens) * 1e6 : null,
      weight: relativeKnown.value && tokenShare > 0 ? costShare / tokenShare : null,
    }
  }),
)

const averageRate = computed(() =>
  priced.value && tokenTotal.value > 0 ? (costTotal.value / tokenTotal.value) * 1e6 : null,
)

/** Shares are drawn on the full 0–100% scale; a sliver keeps a hairline so
 * it is visibly present rather than absent. */
const width = (share: number) => (share <= 0 ? '0%' : `max(2px, ${(share * 100).toFixed(3)}%)`)

const weightText = (w: number | null) => {
  if (w === null) return '–'
  if (w >= 10) return `×${w.toFixed(0)}`
  if (w >= 1) return `×${w.toFixed(1)}`
  return `×${w.toFixed(2)}`
}

/** Where the dollar figures came from, in one readable line. */
const provenance = computed(() => {
  if (!status.value) return t('cost.unavailable')
  const parts = [t('cost.disclaimer')]
  parts.push(
    t('cost.catalog', {
      source: status.value.source,
      version: status.value.version,
      n: exact(status.value.models),
    }),
  )
  if (status.value.stale) parts.push(t('cost.stale'))
  if (status.value.heuristic_cache) parts.push(t('cost.heuristic'))
  if (status.value.overrides > 0) parts.push(t('cost.overrides', { n: String(status.value.overrides) }))
  const unpriced = Number(props.row?.unpriced_tokens || 0)
  if (unpriced > 0) parts.push(t('cost.unpriced', { n: exact(unpriced) }))
  return parts.join(' ')
})
</script>

<template>
  <section class="panel anatomy">
    <div class="panel-head">
      <h2>{{ t('panel.anatomy') }}</h2>
      <span class="muted">{{ t('panel.anatomy.sub') }}</span>
    </div>

    <div v-if="!tokenTotal" class="empty">{{ t('empty.range') }}</div>

    <div v-else class="table-wrap">
      <table class="bill">
        <thead>
          <tr>
            <th class="text">{{ t('anatomy.col.bucket') }}</th>
            <th>{{ t('anatomy.col.tokens') }}</th>
            <th class="text shares-head">{{ t('anatomy.col.share') }}</th>
            <th>{{ t('anatomy.col.cost') }}</th>
            <th class="rate" :title="t('anatomy.rate.tip')">{{ t('anatomy.col.rate') }}</th>
            <th>{{ t('anatomy.col.weight') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="line in lines" :key="line.bucket">
            <td class="text">
              <span class="name">
                <span class="legend-key" :style="{ background: `var(${line.cssVar})` }" />{{ line.label }}
              </span>
            </td>
            <td class="num" :title="exact(line.tokens)">{{ fmt(line.tokens) }}</td>
            <td class="shares">
              <!-- Two readings of one bucket on one scale: the pale bar is its
                   share of the tokens, the solid bar its share of the cost. -->
              <div class="share-row">
                <span class="share-k">{{ t('anatomy.tokens') }}</span>
                <span class="track">
                  <span
                    class="fill pale"
                    :style="{ width: width(line.tokenShare), background: `var(${line.cssVar})` }"
                  />
                </span>
                <span class="share-v num">{{ pct(line.tokens, tokenTotal) }}</span>
              </div>
              <div v-if="priced" class="share-row">
                <span class="share-k">{{ t('anatomy.cost') }}</span>
                <span class="track">
                  <span class="fill" :style="{ width: width(line.costShare), background: `var(${line.cssVar})` }" />
                </span>
                <span class="share-v num">{{ pct(line.cost, costTotal) }}</span>
              </div>
            </td>
            <td class="num" :class="{ partial }">{{ priced ? money(line.cost) : '–' }}</td>
            <td class="num muted rate" :class="{ partial }">{{ line.rate === null ? '–' : money(line.rate) }}</td>
            <td
              class="num weight"
              :class="{ up: (line.weight ?? 0) >= 1 }"
              :title="
                line.weight === null
                  ? ''
                  : t('anatomy.weight.tip', { tokens: pct(line.tokens, tokenTotal), share: pct(line.cost, costTotal) })
              "
            >
              {{ weightText(line.weight) }}
            </td>
          </tr>
        </tbody>
        <tfoot>
          <tr>
            <td class="text">{{ t('anatomy.total') }}</td>
            <td class="num" :title="exact(tokenTotal)">{{ fmt(tokenTotal) }}</td>
            <td />
            <td class="num" :class="{ partial }">{{ priced ? money(costTotal) : '–' }}</td>
            <td class="num rate" :class="{ partial }">{{ averageRate === null ? '–' : money(averageRate) }}</td>
            <td class="num">{{ relativeKnown ? '×1' : '–' }}</td>
          </tr>
        </tfoot>
      </table>
    </div>

    <p v-if="tokenTotal && priced" class="panel-note read">{{ t('anatomy.read') }}</p>
    <p class="panel-note">{{ provenance }}</p>
  </section>
</template>

<style scoped>
.bill {
  width: 100%;
  border-collapse: collapse;
  font-size: 13.5px;
  font-variant-numeric: tabular-nums;
}

.bill th {
  padding: 6px 12px 8px;
  border-bottom: 1px solid var(--ink);
  font-size: 12px;
  font-weight: 500;
  color: var(--muted);
  text-align: right;
  white-space: nowrap;
}

.bill th.text {
  text-align: left;
}

.bill td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--line-soft);
  text-align: right;
  white-space: nowrap;
  vertical-align: middle;
}

.bill th:first-child,
.bill td:first-child {
  padding-left: 0;
}

.bill th:last-child,
.bill td:last-child {
  padding-right: 0;
}

.bill td.text {
  text-align: left;
}

.name {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  color: var(--ink);
}

/* The share column takes whatever width the figures leave: it is the one
 * place in the row that is a picture rather than a number. */
.shares-head,
.shares {
  width: 46%;
  min-width: 220px;
}

.shares {
  text-align: left;
}

.share-row {
  display: grid;
  grid-template-columns: 44px minmax(0, 1fr) 52px;
  align-items: center;
  gap: 10px;
  line-height: 1.5;
}

.share-k {
  font-size: 11.5px;
  color: var(--faint);
}

.track {
  position: relative;
  height: 8px;
  border-radius: 4px;
  background: var(--surface-2);
}

.fill {
  position: absolute;
  inset: 0 auto 0 0;
  border-radius: 4px;
}

.fill.pale {
  opacity: 0.38;
}

.share-v {
  font-size: 12.5px;
  color: var(--ink);
  text-align: right;
}

.partial::after {
  content: '*';
  margin-left: 2px;
  color: var(--muted);
}

.weight {
  font-weight: 650;
  color: var(--muted);
}

.weight.up {
  color: var(--ink);
}

/* The total closes on a double rule, like the summary above it. */
.bill tfoot td {
  padding-top: 10px;
  border-top: 3px double var(--ink);
  border-bottom: 0;
  font-weight: 650;
  color: var(--ink);
}

.read {
  padding-top: 14px;
  color: var(--muted);
}

/* On a narrow window the rate column goes first: relative cost already says
 * which bucket is expensive per token, and the share bars need the width. */
@media (max-width: 900px) {
  .bill .rate {
    display: none;
  }
  .shares-head,
  .shares {
    min-width: 160px;
  }
}
</style>
