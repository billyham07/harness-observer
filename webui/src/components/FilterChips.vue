<script setup lang="ts">
import { computed } from 'vue'
import { FACET_DIMS, type FacetDim } from '@/api/types'
import { t } from '@/i18n'
import { setFacet, setLogsSession, setSelection, state } from '@/composables/useQueryState'
import Icon from './Icon.vue'

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
      <button type="button" :title="t('chip.remove')" :aria-label="`${t('chip.remove')}: ${chip.value}`" @click="chip.remove()">
        <Icon name="close" />
      </button>
    </span>
  </div>
</template>

<style scoped>
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 0 var(--gutter) 12px;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 26px;
  padding: 0 3px 0 8px;
  border: 1px solid var(--line);
  border-radius: var(--r-1);
  background: var(--surface-2);
  font-size: 12.5px;
  max-width: 100%;
}

.dim {
  font-size: 12px;
  font-weight: 500;
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
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  padding: 0;
  border: 0;
  border-radius: var(--r-1);
  background: transparent;
  color: var(--faint);
  cursor: pointer;
}

.chip button .icon {
  width: 12px;
  height: 12px;
}

.chip button:hover {
  background: var(--surface-3);
  color: var(--ink);
}
</style>
