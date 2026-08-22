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
  padding: 0 16px;
  align-items: stretch;
}

.tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 2px;
  min-width: 0;
}

/* The tabs are the panel's title, so they wear the panel title's voice: small,
 * uppercase, widely tracked. The active one is marked by a rule under it, the
 * same position indicator the left rail uses for the active page. */
.tab {
  position: relative;
  padding: 13px 10px;
  border: 0;
  background: transparent;
  color: var(--faint);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  white-space: nowrap;
  cursor: pointer;
  transition: color 0.12s ease;
}

.tab:hover {
  color: var(--muted);
}

.tab.on {
  color: var(--ink);
}

.tab.on::after {
  content: '';
  position: absolute;
  left: 6px;
  right: 6px;
  bottom: -1px;
  height: 2px;
  border-radius: 1px;
  background: var(--accent);
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
