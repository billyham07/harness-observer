<script setup lang="ts">
import { computed, ref } from 'vue'
import type { BucketName } from '@/api/types'
import { t } from '@/i18n'
import { bucketLabel, exact, fmt } from '@/lib/format'
import { BUCKET_COLORS, CHART_CHROME, type Series } from '@/lib/palette'
import { bucketKey, type TimePoint } from '@/lib/timeline'
import { useTheme } from '@/composables/useTheme'
import { useG2Chart } from './useG2Chart'

const props = defineProps<{
  series: Series
  points: TimePoint[]
  bucket: BucketName
  /** Only the bottom row carries the time axis; the rows above share it. */
  axis: boolean
}>()

const container = ref<HTMLElement | null>(null)
const { resolved } = useTheme()

const values = computed(() => props.points.map((p) => Number(p.row?.[props.series.tokens] || 0)))
const total = computed(() => values.value.reduce((a, b) => a + b, 0))
const peak = computed(() => Math.max(0, ...values.value))
const name = computed(() => t(props.series.label))
const bucketName = computed(() => t(`bucket.name.${props.bucket}` as 'bucket.name.day'))

/** Axis ticks are read at a glance, so they drop the trailing zeros the
 * table figures keep: 40M, 2.5M, 1B. */
function axisCompact(v: number): string {
  const units: [number, string][] = [
    [1e9, 'B'],
    [1e6, 'M'],
    [1e3, 'K'],
  ]
  for (const [size, suffix] of units) {
    if (Math.abs(v) >= size) return `${Number((v / size).toPrecision(3))}${suffix}`
  }
  return String(Math.round(v))
}

/**
 * One bucket, on its own scale. A line with a faint wash under it reads as a
 * level over time; columns at a year of days are a comb. The wash is the
 * series hue at low opacity so four rows stacked together stay quiet and the
 * line carries the reading.
 */
const spec = computed(() => {
  if (!props.points.length) return null
  const chrome = CHART_CHROME[resolved.value]
  const color = BUCKET_COLORS[resolved.value][props.series.bucket]
  const bucket = props.bucket
  const label = name.value
  const data = props.points.map((p, i) => ({ t: p.t, key: p.key, value: values.value[i] }))
  const font = 'system-ui, "Segoe UI", sans-serif'
  const sparse = data.length < 3

  return {
    type: 'view',
    data,
    paddingLeft: 44,
    paddingRight: 12,
    paddingTop: 8,
    paddingBottom: props.axis ? 26 : 6,
    encode: { x: 't', y: 'value' },
    scale: {
      x: { type: 'time', nice: false },
      // Two ticks, zero and the row's top: a 70px row has no room for a
      // middle one, and the top value is the row's scale.
      y: { nice: true, domainMin: 0, tickMethod: (min: number, max: number) => [min, max] },
    },
    axis: {
      x: props.axis
        ? {
            title: false,
            tick: false,
            line: false,
            labelFill: chrome.text,
            labelFillOpacity: 1,
            labelOpacity: 1,
            labelFontSize: 11,
            labelFontFamily: font,
            labelSpacing: 8,
            labelAutoRotate: false,
            labelAutoHide: true,
            tickCount: 8,
            labelFormatter: (d: Date | number) => bucketLabel(bucketKey(+d, bucket), bucket),
          }
        : false,
      y: {
        title: false,
        tick: false,
        line: false,
        grid: true,
        gridStroke: chrome.grid,
        gridStrokeOpacity: 1,
        gridLineWidth: 1,
        gridLineDash: [0, 0],
        labelFill: chrome.text,
        labelFillOpacity: 1,
        labelOpacity: 1,
        labelFontSize: 10.5,
        labelFontFamily: font,
        labelSpacing: 6,
        labelFormatter: (v: number) => axisCompact(v),
      },
    },
    children: [
      {
        type: 'area',
        style: { fill: color, fillOpacity: 0.12 },
        tooltip: false,
        animate: false,
      },
      {
        type: sparse ? 'point' : 'line',
        style: sparse
          ? { fill: color, stroke: chrome.surface, lineWidth: 2, r: 4 }
          : { stroke: color, lineWidth: 1.5, lineJoin: 'round' },
        tooltip: {
          title: (d: { key: string }) => bucketLabel(d.key, bucket),
          items: [(d: { value: number }) => ({ name: label, value: exact(d.value), color })],
        },
        animate: false,
      },
    ],
    interaction: { tooltip: { crosshairs: true, crosshairsStroke: chrome.axis, crosshairsLineDash: [0, 0] } },
    animate: false,
  } as Record<string, unknown>
})

useG2Chart(container, () => spec.value)
</script>

<template>
  <div class="trend" :class="{ 'with-axis': axis }">
    <div class="who">
      <div class="trend-name">
        <span class="legend-key" :style="{ background: `var(${series.cssVar})` }" />
        {{ name }}
      </div>
      <div class="trend-total num" :title="exact(total)">{{ t('trend.total', { n: fmt(total) }) }}</div>
      <div class="trend-peak num">{{ t('trend.peak', { n: fmt(peak), bucket: bucketName }) }}</div>
    </div>
    <div
      ref="container"
      class="canvas"
      role="img"
      :aria-label="t('chart.aria', { series: name, bucket: bucketName })"
    />
  </div>
</template>

<style scoped>
.trend {
  display: grid;
  grid-template-columns: 168px minmax(0, 1fr);
  align-items: stretch;
  border-bottom: 1px solid var(--line-soft);
}

.trend.with-axis {
  border-bottom: 0;
}

.who {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 1px;
  padding: 8px 12px 8px 0;
}

.with-axis .who {
  padding-bottom: 28px;
}

.trend-name {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  font-weight: 600;
  color: var(--ink);
}

.trend-total {
  font-size: 12.5px;
  color: var(--muted);
}

.trend-peak {
  font-size: 11.5px;
  color: var(--faint);
}

.canvas {
  width: 100%;
  height: 80px;
}

.with-axis .canvas {
  height: 100px;
}

@media (max-width: 640px) {
  .trend {
    grid-template-columns: minmax(0, 1fr);
  }
  .who {
    flex-direction: row;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px 12px;
    padding: 10px 0 0;
  }
  .with-axis .who {
    padding-bottom: 0;
  }
}
</style>
