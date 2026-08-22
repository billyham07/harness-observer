<script setup lang="ts">
import { computed } from 'vue'
import { FACET_DIMS, type FacetDim } from '@/api/types'
import { t } from '@/i18n'
import { setFacet, setLogsSession, setSelection, state } from '@/composables/useQueryState'

interface Chip {
  key: string
  dim: string
  value: string
  remove: () => void
}

/**
 * A filter the user cannot see is a number they cannot explain. Every active
 * narrowing is spelled out here and removable in one click, including the
 * session id typed on the Logs page.
 */
const chips = computed<Chip[]>(() => {
  const out: Chip[] = []
  for (const path of state.sel) {
    out.push({
      key: `sel:${path}`,
      dim: t('chip.combination'),
      value: path.replace(/\|/g, ' → '),
      remove: () => setSelection(state.sel.filter((s) => s !== path)),
    })
  }
  for (const dim of FACET_DIMS) {
    for (const value of state.facets[dim]) {
      out.push({
        key: `${dim}:${value}`,
        dim: t(`facet.${dim}` as 'facet.client'),
        value: value === '' ? t('none') : value,
        remove: () => setFacet(dim as FacetDim, state.facets[dim].filter((v) => v !== value)),
      })
    }
  }
  if (state.view === 'logs' && state.logsSession) {
    out.push({
      key: 'session',
      dim: t('logs.id'),
      value: state.logsSession,
      remove: () => setLogsSession(''),
    })
  }
  return out
})
</script>

<template>
  <div v-if="chips.length" class="chips">
    <span v-for="chip in chips" :key="chip.key" class="chip">
      <span class="dim">{{ chip.dim }}</span>
      <span class="val">{{ chip.value }}</span>
      <button type="button" :title="t('chip.remove')" @click="chip.remove()">×</button>
    </span>
  </div>
</template>

<style scoped>
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 10px var(--gutter);
  border-bottom: 1px solid var(--line);
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 3px 4px 3px 8px;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: var(--surface);
  font-size: 11.5px;
  max-width: 100%;
}

.dim {
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--faint);
}

.val {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--ink);
}

.chip button {
  flex: none;
  width: 18px;
  height: 18px;
  padding: 0;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: var(--faint);
  font-size: 13px;
  line-height: 1;
  cursor: pointer;
}

.chip button:hover {
  background: var(--surface-3);
  color: var(--ink);
}
</style>
