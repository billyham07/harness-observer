<script setup lang="ts">
import { computed, ref } from 'vue'
import { NCheckbox, NInput, NPopover } from 'naive-ui'
import type { FacetDim } from '@/api/types'
import { t } from '@/i18n'
import { fmt } from '@/lib/format'
import { useFilterOptions } from '@/composables/useFilterOptions'
import { setFacet, state, toggleFacetValue } from '@/composables/useQueryState'

const props = defineProps<{ dim: FacetDim }>()

const { facetFor } = useFilterOptions()
const facet = facetFor(props.dim)
const search = ref('')

const label = computed(() => t(`facet.${props.dim}` as 'facet.client'))
const picked = computed(() => state.facets[props.dim])

const values = computed(() => {
  const q = search.value.trim().toLowerCase()
  return (facet.value.values ?? []).filter((v) => !q || (v.value || '').toLowerCase().includes(q))
})

const summary = computed(() => {
  if (!picked.value.length) return t('all')
  if (picked.value.length === 1) return picked.value[0] === '' ? t('none') : picked.value[0]
  return t('selected', { n: picked.value.length })
})
</script>

<template>
  <NPopover trigger="click" placement="bottom-start" :show-arrow="false">
    <template #trigger>
      <button type="button" class="control" :class="{ active: picked.length > 0 }">
        <span class="control-label">{{ label }}</span>
        <span class="control-value">{{ summary }}</span>
        <span class="caret" aria-hidden="true" />
      </button>
    </template>

    <div class="facet-pop">
      <div class="pop-head">
        <span>{{ label }}</span>
        <button type="button" class="btn btn-ghost" @click="setFacet(dim, [])">{{ t('clear') }}</button>
      </div>
      <div class="pop-body">
        <NInput
          :value="search"
          size="small"
          clearable
          :placeholder="t('facet.filter', { label })"
          @update:value="search = $event"
        />
      </div>
      <div class="options list">
        <div v-if="!values.length" class="empty">
          {{ search.trim() ? t('facet.noMatch') : t('facet.nothing') }}
        </div>
        <label v-for="v in values" :key="v.value" class="option" :class="{ stale: v.stale }">
          <NCheckbox
            :checked="!!v.selected"
            @update:checked="toggleFacetValue(dim, v.value, $event)"
          >
            <span class="name" :class="{ unknown: v.value === '' }" :title="v.value">{{
              v.value === '' ? t('none') : v.value
            }}</span>
          </NCheckbox>
          <!-- A stale value has no count under the current context, and saying
               "0" would read as evidence that it was observed and empty. -->
          <span class="count">{{ v.stale ? '–' : fmt(v.observed_tokens) }}</span>
        </label>
      </div>
    </div>
  </NPopover>
</template>

<style scoped>
.facet-pop {
  width: 280px;
  max-width: 92vw;
}

.list {
  padding: 0 12px 12px;
}

.option :deep(.n-checkbox) {
  flex: 1;
  min-width: 0;
  align-items: center;
}

.option :deep(.n-checkbox__label) {
  min-width: 0;
  overflow: hidden;
  padding-right: 6px;
}
</style>
