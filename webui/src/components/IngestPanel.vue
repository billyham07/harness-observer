<script setup lang="ts">
import { computed } from 'vue'
import { t, type StringKey } from '@/i18n'
import { exact } from '@/lib/format'
import { useRefresh } from '@/composables/useRefresh'

const { status } = useRefresh()

const rows = computed(() => status.value?.last_stats ?? [])
const warnings = computed(() => rows.value.flatMap((r) => r.warnings ?? []))

const cols: { label: StringKey; key: keyof (typeof rows.value)[number] }[] = [
  { label: 'ingest.source', key: 'source' },
  { label: 'ingest.files', key: 'files_discovered' },
  { label: 'ingest.parsed', key: 'files_parsed' },
  { label: 'ingest.skipped', key: 'files_skipped' },
  { label: 'ingest.rewound', key: 'files_rewound' },
  { label: 'ingest.turnsParsed', key: 'turns_parsed' },
  { label: 'ingest.newRows', key: 'turns_ingested' },
]
</script>

<template>
  <section class="panel">
    <div class="panel-head">
      <h2>{{ t('panel.lastScan') }}</h2>
      <span class="muted num path">{{ status?.db ?? '' }}</span>
    </div>
    <div class="table-wrap">
      <div v-if="!rows.length" class="empty">{{ t('ingest.none') }}</div>
      <table v-else class="grid">
        <thead>
          <tr>
            <th v-for="(col, i) in cols" :key="col.key" :class="{ text: i === 0 }">{{ t(col.label) }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.source">
            <td v-for="(col, i) in cols" :key="col.key" :class="i === 0 ? 'text' : 'num'">
              {{ i === 0 ? row.source : exact(row[col.key]) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="warnings.length" class="panel-note warn">
      {{ t('ingest.warnings', { n: warnings.length, list: warnings.slice(0, 3).join(' | ') }) }}
    </p>
  </section>
</template>

<style scoped>
.path {
  font-size: 10.5px;
  max-width: 60%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.warn {
  color: var(--warn);
}
</style>
