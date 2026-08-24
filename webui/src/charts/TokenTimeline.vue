<script setup lang="ts">
import { computed, ref } from 'vue'
import type { BucketName, QueryRow } from '@/api/types'
import { t } from '@/i18n'
import { bucketLabel, exact, fmt } from '@/lib/format'
import { BUCKET_COLORS, CHART_CHROME, SERIES } from '@/lib/palette'
import { useTheme } from '@/composables/useTheme'
import { useG2Chart } from './useG2Chart'

const props = defineProps<{ rows: QueryRow[]; bucket: BucketName }>()

const container = ref<HTMLElement | null>(null)
const { resolved } = useTheme()

const totals = computed(() =>
  props.rows.map((r) => SERIES.reduce((sum, s) => sum + Number(r[s.tokens] || 0), 0)),
)
const peak = computed(() => Math.max(0, ...totals.value))

/** The stacking order is the canonical bucket order, and it does not change
 * when a bucket happens to be empty: colour follows the entity, never rank. */
const seriesNames = computed(() => SERIES.map((s) => t(s.label)))

const spec = computed(() => {
  if (!props.rows.length) return null
  const chrome = CHART_CHROME[resolved.value]
  const colors = SERIES.map((s) => BUCKET_COLORS[resolved.value][s.bucket])
  const labels = seriesNames.value
  const bucket = props.bucket
  const every = Math.max(1, Math.ceil(props.rows.length / 8))

  const data = props.rows.flatMap((row, index) =>
    SERIES.map((s, order) => ({
      bucket: row.bucket ?? '',
      index,
      order,
      series: labels[order],
      value: Number(row[s.tokens] || 0),
    })),
  )

  return {
    type: 'interval',
    data,
    encode: { x: 'bucket', y: 'value', color: 'series' },
    transform: [{ type: 'stackY', orderBy: 'order', reverse: true }],
    scale: {
      color: { domain: labels, range: colors },
      y: { nice: true },
    },
    axis: {
      x: {
        title: false,
        tick: false,
        line: true,
        lineStroke: chrome.axis,
        labelFill: chrome.text,
        labelFontSize: 10,
        labelFontFamily: 'ui-monospace, monospace',
        labelAutoRotate: false,
        labelFormatter: (value: string) => bucketLabel(value, bucket),
        labelFilter: (_: unknown, index: number, array: unknown[]) =>
          index % every === 0 || index === array.length - 1,
      },
      y: {
        title: false,
        tick: false,
        grid: true,
        gridStroke: chrome.grid,
        gridLineWidth: 1,
        labelFill: chrome.text,
        labelFontSize: 10,
        labelFontFamily: 'ui-monospace, monospace',
        labelFormatter: (value: number) => fmt(value),
      },
    },
    // A 1px surface-coloured edge is the 2px separation the palette's CVD
    // margin depends on: adjacent buckets never touch.
    style: { insetLeft: 1, insetRight: 1, stroke: chrome.surface, lineWidth: 1 },
    legend: false,
    interaction: { tooltip: { shared: true } },
    tooltip: {
      title: (d: { bucket: string }) => bucketLabel(d.bucket, bucket),
      items: [
        (d: { series: string; value: number }) => ({
          name: d.series,
          value: exact(d.value),
        }),
      ],
    },
  } as Record<string, unknown>
})

useG2Chart(container, () => spec.value)

const caption = computed(() => {
  if (!props.rows.length) return ''
  return t('chart.range', {
    from: bucketLabel(props.rows[0].bucket, props.bucket),
    to: bucketLabel(props.rows[props.rows.length - 1].bucket, props.bucket),
    peak: exact(peak.value),
    bucket: t(`bucket.name.${props.bucket}` as 'bucket.name.day'),
  })
})
</script>

<template>
  <div class="timeline">
    <div v-if="!rows.length" class="empty">{{ t('empty.rangeHint') }}</div>
    <div
      v-else
      ref="container"
      class="canvas"
      role="img"
      :aria-label="t('chart.aria', { bucket: t(`bucket.name.${bucket}` as 'bucket.name.day') })"
    />
    <div v-if="rows.length" class="foot">
      <div class="legend">
        <span v-for="s in SERIES" :key="s.bucket" class="legend-item">
          <span class="legend-key" :style="{ background: `var(${s.cssVar})` }" />
          {{ t(s.label) }}
        </span>
      </div>
      <span class="caption num">{{ caption }}</span>
    </div>
  </div>
</template>

<style scoped>
.timeline {
  padding: 4px 16px 16px;
}

.canvas {
  width: 100%;
  height: 264px;
}

.foot {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 18px;
  padding: 10px 4px 0;
}

.caption {
  font-size: 11px;
  color: var(--faint);
}
</style>
