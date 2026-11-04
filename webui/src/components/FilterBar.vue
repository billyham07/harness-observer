<script setup lang="ts">
import { computed } from 'vue'
import { NPopover } from 'naive-ui'
import { FACET_DIMS } from '@/api/types'
import { t } from '@/i18n'
import { hasFilters, resetFilters, state } from '@/composables/useQueryState'
import FacetFilter from './FacetFilter.vue'
import HierarchySelector from './HierarchySelector.vue'
import Icon from './Icon.vue'

/** Whatever is narrowing the numbers is reachable from this one row. */
const hierarchyLabel = computed(() => {
  if (!state.sel.length) return t('hierarchy.everything')
  if (state.sel.length === 1) return state.sel[0].replace(/\|/g, ' → ')
  return t('hierarchy.combinations', { n: state.sel.length })
})
</script>

<template>
  <nav class="filterbar" :aria-label="t('filters.aria')">
    <NPopover trigger="click" placement="bottom-start" :show-arrow="false">
      <template #trigger>
        <button type="button" class="control wide" :class="{ active: state.sel.length > 0 }">
          <span class="control-label">{{ t('hierarchy.label') }}</span>
          <span class="control-value">{{ hierarchyLabel }}</span>
          <Icon name="chevron-down" class="caret" />
        </button>
      </template>
      <HierarchySelector />
    </NPopover>

    <FacetFilter v-for="dim in FACET_DIMS" :key="dim" :dim="dim" />

    <button type="button" class="btn btn-ghost reset" :disabled="!hasFilters" @click="resetFilters()">
      {{ t('filters.reset') }}
    </button>
  </nav>
</template>

<style scoped>
.filterbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  padding: 10px var(--gutter) 12px;
}

/* On a narrow window the filters scroll sideways in one row instead of
 * stacking four rows of chrome above the numbers they narrow. */
@media (max-width: 720px) {
  .filterbar {
    flex-wrap: nowrap;
    overflow-x: auto;
  }
}

.control.wide {
  max-width: 360px;
}

.reset {
  margin-left: auto;
}
</style>
