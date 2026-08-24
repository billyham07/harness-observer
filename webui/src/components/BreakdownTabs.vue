<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { QueryRow } from '@/api/types'
import { t, type StringKey } from '@/i18n'
import { exact } from '@/lib/format'
import AggregateTable from './AggregateTable.vue'

export interface Breakdown {
  key: string
  label: StringKey
  dims: string[]
  rows: QueryRow[]
}

const props = defineProps<{ breakdowns: Breakdown[] }>()

/**
 * The same comparison table, cut a different way each time.
 *
 * These used to be four small panels in a grid, which gave a fourteen-column
 * table about a third of the width it needs — every one of them was read by
 * dragging sideways. One card at full width with a tab bar trades a
 * side-by-side view nobody could actually read for one that fits.
 */
const active = ref(props.breakdowns[0]?.key ?? '')
const current = computed(
  () => props.breakdowns.find((b) => b.key === active.value) ?? props.breakdowns[0],
)

// A filter change can retire the active cut entirely; fall back rather than
// render an empty card.
watch(
  () => props.breakdowns.map((b) => b.key).join('|'),
  () => {
    if (!props.breakdowns.some((b) => b.key === active.value)) {
      active.value = props.breakdowns[0]?.key ?? ''
    }
  },
)

const tabs = ref<HTMLButtonElement[]>([])

/** Arrow keys move between tabs, which is what a tablist owes its user. */
function onKeydown(event: KeyboardEvent, index: number) {
  const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
  if (!step) return
  event.preventDefault()
  const count = props.breakdowns.length
  const next = (index + step + count) % count
  active.value = props.breakdowns[next].key
  tabs.value[next]?.focus()
}
</script>

<template>
  <section class="panel">
    <div class="panel-head head">
      <div class="tabs" role="tablist" :aria-label="t('panel.breakdowns')">
        <button
          v-for="(breakdown, index) in breakdowns"
          :key="breakdown.key"
          ref="tabs"
          type="button"
          role="tab"
          class="tab"
          :class="{ on: active === breakdown.key }"
          :aria-selected="active === breakdown.key"
          :tabindex="active === breakdown.key ? 0 : -1"
          @click="active = breakdown.key"
          @keydown="onKeydown($event, index)"
        >
          {{ t(breakdown.label) }}
        </button>
      </div>
      <span class="muted num count">{{ t('panel.rows', { n: exact(current.rows.length) }) }}</span>
    </div>

    <div role="tabpanel" :aria-label="t(current.label)">
      <!-- Keyed on the cut: each tab opens on its own default sort instead of
           inheriting a sort by a column the new cut does not have. -->
      <AggregateTable :key="current.key" :dims="current.dims" :rows="current.rows" />
    </div>
  </section>
</template>

<style scoped>
.head {
  padding: 14px 16px 0;
  align-items: center;
}

.tabs {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
  min-width: 0;
  padding: 4px;
  border-radius: 999px;
  background: var(--surface-2);
}

.tab {
  padding: 6px 14px;
  border: 0;
  border-radius: 999px;
  background: transparent;
  color: var(--faint);
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
  cursor: pointer;
  transition:
    color 0.12s ease,
    background-color 0.12s ease,
    box-shadow 0.12s ease;
}

.tab:hover {
  color: var(--muted);
}

.tab.on {
  color: var(--ink);
  background: var(--surface);
  box-shadow: var(--shadow-1);
}

.count {
  align-self: center;
  flex: none;
}

@media (prefers-reduced-motion: reduce) {
  .tab {
    transition: none;
  }
}
</style>
