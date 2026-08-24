<script setup lang="ts">
import { computed } from 'vue'
import type { QueryRow } from '@/api/types'
import { t } from '@/i18n'
import { exact, money, pct } from '@/lib/format'
import { SERIES } from '@/lib/palette'
import { costPriced, usePricing } from '@/composables/usePricing'

const props = defineProps<{ row: QueryRow | null }>()
const { status } = usePricing()

interface Slice {
  bucket: string
  label: string
  cssVar: string
  tokens: number
  cost: number
  /** Cumulative fractions, used by both the bars and the ribbons between them. */
  tokenStart: number
  tokenSpan: number
  costStart: number
  costSpan: number
}

const tokenTotal = computed(() =>
  SERIES.reduce((sum, s) => sum + Number(props.row?.[s.tokens] || 0), 0),
)
const costTotal = computed(() => SERIES.reduce((sum, s) => sum + Number(props.row?.[s.cost] || 0), 0))
const priced = computed(() => costPriced(props.row) && costTotal.value > 0)

/**
 * The two bars are the same four buckets weighed twice: once in tokens, once
 * in dollars. Laying them out on one geometry — identical order, identical
 * width, ribbons joining each bucket to itself — turns a comparison people
 * otherwise do in their head into a shape. A bucket that is almost all of the
 * tokens can be a fraction of the cost, and a rounding error in tokens can be
 * a sixth of the bill; the ribbons are exactly that difference.
 */
const slices = computed<Slice[]>(() => {
  let tokenCursor = 0
  let costCursor = 0
  return SERIES.map((s) => {
    const tokens = Number(props.row?.[s.tokens] || 0)
    const cost = Number(props.row?.[s.cost] || 0)
    const tokenSpan = tokenTotal.value > 0 ? tokens / tokenTotal.value : 0
    const costSpan = costTotal.value > 0 ? cost / costTotal.value : 0
    const slice: Slice = {
      bucket: s.bucket,
      label: t(s.label),
      cssVar: s.cssVar,
      tokens,
      cost,
      tokenStart: tokenCursor,
      tokenSpan,
      costStart: costCursor,
      costSpan,
    }
    tokenCursor += tokenSpan
    costCursor += costSpan
    return slice
  })
})

const visibleTokenSlices = computed(() => slices.value.filter((s) => s.tokens > 0))
const visibleCostSlices = computed(() => slices.value.filter((s) => s.cost > 0))

/** A label only goes inside a segment wide enough to hold it honestly. */
const LABEL_FLOOR = 0.11

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
      <span class="muted num">{{
        tokenTotal ? t('ribbon.total', { n: exact(tokenTotal) }) : ''
      }}</span>
    </div>

    <div v-if="!tokenTotal" class="empty">{{ t('empty.range') }}</div>

    <div v-else class="body">
      <div class="lane">
        <span class="lane-label eyebrow">{{ t('anatomy.tokens') }}</span>
        <div class="bar">
          <span
            v-for="slice in visibleTokenSlices"
            :key="slice.bucket"
            class="seg"
            :style="{ width: `${(slice.tokenSpan * 100).toFixed(4)}%`, background: `var(${slice.cssVar})` }"
            :title="`${slice.label}: ${exact(slice.tokens)} (${pct(slice.tokens, tokenTotal)})`"
          >
            <span v-if="slice.tokenSpan >= LABEL_FLOOR" class="seg-label">
              {{ slice.label }} <b class="num">{{ pct(slice.tokens, tokenTotal) }}</b>
            </span>
          </span>
        </div>
      </div>

      <template v-if="priced">
        <!-- The join. Each ribbon is one bucket, so the collapse from token
             share to cost share is a shape, not an arithmetic exercise. -->
        <svg class="join" viewBox="0 0 100 1" preserveAspectRatio="none" aria-hidden="true">
          <path
            v-for="slice in slices"
            :key="slice.bucket"
            :d="`M${(slice.tokenStart * 100).toFixed(4)},0 L${((slice.tokenStart + slice.tokenSpan) * 100).toFixed(4)},0 L${((slice.costStart + slice.costSpan) * 100).toFixed(4)},1 L${(slice.costStart * 100).toFixed(4)},1 Z`"
            :fill="`var(${slice.cssVar})`"
            fill-opacity="0.3"
          />
        </svg>

        <div class="lane">
          <span class="lane-label eyebrow">{{ t('anatomy.cost') }}</span>
          <div class="bar">
            <span
              v-for="slice in visibleCostSlices"
              :key="slice.bucket"
              class="seg"
              :style="{ width: `${(slice.costSpan * 100).toFixed(4)}%`, background: `var(${slice.cssVar})` }"
              :title="`${slice.label}: ${money(slice.cost)} (${pct(slice.cost, costTotal)})`"
            >
              <span v-if="slice.costSpan >= LABEL_FLOOR" class="seg-label">
                {{ slice.label }} <b class="num">{{ pct(slice.cost, costTotal) }}</b>
              </span>
            </span>
          </div>
        </div>
      </template>

      <p v-else class="unavailable">{{ t('cost.unavailable') }}</p>

      <!-- The legend is the shared key for both bars, and the only place every
           bucket is guaranteed to be named even when its segment is a sliver. -->
      <div class="legend key">
        <span v-for="slice in slices" :key="slice.bucket" class="legend-item">
          <span class="legend-key" :style="{ background: `var(${slice.cssVar})` }" />
          <span class="legend-name">{{ slice.label }}</span>
          <b class="num">{{ pct(slice.tokens, tokenTotal) }}</b>
          <span v-if="priced" class="legend-cost num">{{ money(slice.cost) }}</span>
        </span>
        <span v-if="priced" class="legend-total num">{{ t('ribbon.costTotal', { n: money(costTotal) }) }}</span>
      </div>

      <p v-if="priced" class="read">{{ t('anatomy.read') }}</p>
    </div>

    <p class="panel-note">{{ provenance }}</p>
  </section>
</template>

<style scoped>
.body {
  padding: 8px 20px 8px;
}

.lane {
  display: flex;
  align-items: center;
  gap: 12px;
}

.lane-label {
  flex: none;
  width: 92px;
  text-align: right;
}

.bar {
  flex: 1;
  display: flex;
  height: 30px;
  min-width: 0;
  border-radius: var(--r-1);
  overflow: hidden;
  background: var(--surface-3);
}

/* The 2px gap is painted onto the segment rather than taken out of the layout,
 * so a segment's left edge is exactly its cumulative share — which is what
 * lets the ribbons below line up with it to the pixel. */
.seg {
  position: relative;
  display: flex;
  align-items: center;
  min-width: 0;
  overflow: hidden;
  border-right: 2px solid var(--surface);
  box-sizing: border-box;
}

.seg:last-child {
  border-right: 0;
}

.seg-label {
  padding-left: 8px;
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: #fff;
  white-space: nowrap;
  mix-blend-mode: normal;
}

.seg-label b {
  font-weight: 600;
  opacity: 0.8;
}

.join {
  display: block;
  width: calc(100% - 104px);
  margin-left: 104px;
  height: 34px;
}

.unavailable {
  margin: 12px 0 0 104px;
  font-size: 11.5px;
  color: var(--faint);
}

.key {
  margin: 16px 0 0 104px;
  padding-top: 12px;
  border-top: 1px solid var(--line-soft);
  gap: 6px 20px;
}

.legend-name {
  color: var(--muted);
}

.legend-item b {
  color: var(--ink);
  font-weight: 600;
}

.legend-cost {
  color: var(--faint);
}

.legend-total {
  margin-left: auto;
  font-size: 11.5px;
  color: var(--muted);
}

/* How to read the panel, once. The provenance note below answers a different
 * question — where the dollars came from — so the two stay separate. */
.read {
  max-width: 62ch;
  margin: 10px 0 0 104px;
  font-size: 11.5px;
  line-height: 1.55;
  color: var(--muted);
}

@media (max-width: 720px) {
  .lane {
    flex-direction: column;
    align-items: stretch;
    gap: 5px;
  }
  .lane-label {
    width: auto;
    text-align: left;
  }
  .join,
  .key,
  .read,
  .unavailable {
    width: 100%;
    margin-left: 0;
  }
  .join {
    height: 24px;
  }
}
</style>
