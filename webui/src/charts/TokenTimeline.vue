<script setup lang="ts">
import { computed } from 'vue'
import type { BucketName, QueryRow } from '@/api/types'
import { t } from '@/i18n'
import { SERIES } from '@/lib/palette'
import { fillBuckets } from '@/lib/timeline'
import BucketTrend from './BucketTrend.vue'

const props = defineProps<{ rows: QueryRow[]; bucket: BucketName }>()

/**
 * Token flow over time as small multiples: one row per bucket, in canonical
 * order, each on its own scale, all on one calendar.
 *
 * A single stacked chart could not be read. Cache read is routinely ~97% of
 * every prompt, so the stack was one colour with three invisible slivers on
 * top, and the API's habit of omitting idle buckets meant the axis was not
 * time at all. Separate rows give every bucket a scale it can be read on, and
 * the filled calendar makes a quiet week look like a quiet week.
 */
const points = computed(() => fillBuckets(props.rows, props.bucket))
</script>

<template>
  <div class="timeline">
    <div v-if="!rows.length" class="empty">{{ t('empty.rangeHint') }}</div>
    <template v-else>
      <BucketTrend
        v-for="(s, i) in SERIES"
        :key="s.bucket"
        :series="s"
        :points="points"
        :bucket="bucket"
        :axis="i === SERIES.length - 1"
      />
      <p class="panel-note">{{ t('trend.read') }}</p>
    </template>
  </div>
</template>

<style scoped>
.timeline {
  padding: 0;
}
</style>
