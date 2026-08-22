<script setup lang="ts">
import { computed, watch } from 'vue'
import { t } from '@/i18n'
import TokenTimeline from '@/charts/TokenTimeline.vue'
import AggregateTable from '@/components/AggregateTable.vue'
import BreakdownTabs, { type Breakdown } from '@/components/BreakdownTabs.vue'
import IngestPanel from '@/components/IngestPanel.vue'
import KpiBand from '@/components/KpiBand.vue'
import TurnAnatomy from '@/components/TurnAnatomy.vue'
import { useOverview } from '@/composables/useOverview'
import { filterSignature } from '@/composables/useQueryState'

const { data, loading, loaded, load } = useOverview()

// One watcher owns when this page queries: the filter changed, so the picture
// is stale. Sorting a table, switching language or opening a popover are not
// filter changes and cost nothing.
watch(filterSignature, () => void load(), { immediate: true })

const bucketNote = computed(() =>
  t('bucket.per', { bucket: t(`bucket.name.${data.value.bucket}` as 'bucket.name.day') }),
)

/** One card, one table at full width, and a tab per way of cutting it. */
const breakdowns = computed<Breakdown[]>(() => [
  { key: 'harness', label: 'panel.byHarness', dims: ['harness'], rows: data.value.harness },
  { key: 'model', label: 'panel.byModel', dims: ['model'], rows: data.value.models },
  { key: 'project', label: 'panel.byProject', dims: ['project'], rows: data.value.projects },
  { key: 'client', label: 'panel.byClient', dims: ['client'], rows: data.value.clients },
  { key: 'basis', label: 'panel.basis', dims: ['basis', 'semantics'], rows: data.value.basis },
])
</script>

<template>
  <div class="overview" :class="{ busy: loading && !loaded }">
    <KpiBand :row="data.totals" />

    <TurnAnatomy :row="data.totals" />

    <section class="panel">
      <div class="panel-head">
        <h2>{{ t('panel.timeline') }}</h2>
        <span class="muted">{{ bucketNote }}</span>
      </div>
      <TokenTimeline :rows="data.timeline" :bucket="data.bucket" />
    </section>

    <section class="panel">
      <div class="panel-head">
        <h2>{{ t('panel.tuples') }}</h2>
        <span class="muted">{{ t('panel.tuples.sub') }}</span>
      </div>
      <AggregateTable :dims="['harness', 'model', 'effort']" :rows="data.tuples" />
    </section>

    <BreakdownTabs :breakdowns="breakdowns" />

    <IngestPanel />
  </div>
</template>

<style scoped>
.overview {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 16px var(--gutter) 40px;
}

/* Only the very first load dims the page. A background refresh that greys out
 * the numbers every thirty seconds is worse than no indicator at all. */
.busy {
  opacity: 0.55;
}
</style>
