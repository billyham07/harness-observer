<script setup lang="ts">
import { computed } from 'vue'
import { NCheckbox, NInput } from 'naive-ui'
import type { HierarchyNode } from '@/api/types'
import { t } from '@/i18n'
import { fmt } from '@/lib/format'
import { visibleTree } from '@/lib/hierarchy'
import { useFilterOptions } from '@/composables/useFilterOptions'

const { nodes, stale, marks, search, collapsed, toggleNode, clearSelection, dropStale, toggleCollapsed } =
  useFilterOptions()

interface Row {
  node: HierarchyNode
  depth: number
  expandable: boolean
  expanded: boolean
}

/**
 * The tree is rendered from an explicit row list rather than nested
 * components. Depth, collapse and the search filter then have one place to
 * agree, and a search hit deep in the tree can open its ancestors without a
 * second traversal.
 */
const rows = computed<Row[]>(() => {
  const out: Row[] = []
  const searching = search.value.trim().length > 0
  const walk = (list: HierarchyNode[], depth: number) => {
    for (const node of list) {
      const children = node.children ?? []
      const expandable = depth < 2 && children.length > 0
      const expanded = expandable && (!collapsed.value.has(node.path) || searching)
      out.push({ node, depth, expandable, expanded })
      if (expanded) walk(children, depth + 1)
    }
  }
  walk(visibleTree(nodes.value, search.value), 0)
  return out
})

const staleList = computed(() => stale.value.map((s) => s.path).join(', '))

function labelFor(node: HierarchyNode, depth: number) {
  if (node.value !== '') return node.value
  return depth === 1 ? t('hierarchy.noModel') : t('hierarchy.noEffort')
}
</script>

<template>
  <div class="tree-pop">
    <div class="pop-head">
      <span>{{ t('hierarchy.head') }}</span>
      <button type="button" class="btn btn-ghost" @click="clearSelection">{{ t('clear') }}</button>
    </div>

    <div class="pop-body">
      <p class="pop-hint">{{ t('hierarchy.hint') }}</p>
      <NInput
        :value="search"
        size="small"
        clearable
        :placeholder="t('hierarchy.search')"
        @update:value="search = $event"
      />
    </div>

    <!-- A selection that stops matching is reported, not dropped and not
         quietly left working. -->
    <div v-if="stale.length" class="stale">
      <span>{{
        stale.length === 1
          ? t('stale.one', { list: staleList })
          : t('stale.many', { n: stale.length, list: staleList })
      }}</span>
      <button type="button" class="btn btn-ghost" @click="dropStale">{{ t('stale.drop') }}</button>
    </div>

    <div class="options tree">
      <div v-if="!rows.length" class="empty">
        {{ search.trim() ? t('empty.search') : t('empty.range') }}
      </div>
      <div
        v-for="row in rows"
        :key="row.node.path"
        class="option"
        :style="{ paddingLeft: 6 + row.depth * 16 + 'px' }"
      >
        <button
          v-if="row.expandable"
          type="button"
          class="twisty"
          :title="row.expanded ? t('hierarchy.collapse') : t('hierarchy.expand')"
          @click="toggleCollapsed(row.node.path)"
        >
          {{ row.expanded ? '▾' : '▸' }}
        </button>
        <span v-else class="twisty-spacer" />
        <NCheckbox
          :checked="marks.get(row.node.path) === 'full'"
          :indeterminate="marks.get(row.node.path) === 'partial'"
          @update:checked="toggleNode(row.node, row.depth)"
        >
          <span
            class="name"
            :class="{ unknown: row.node.value === '' }"
            :title="row.node.value || t('hierarchy.notRecorded')"
            >{{ labelFor(row.node, row.depth) }}</span
          >
        </NCheckbox>
        <span class="count">{{ fmt(row.node.observed_tokens) }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tree-pop {
  width: 380px;
  max-width: 92vw;
}

.pop-body {
  padding-bottom: 8px;
}

.stale {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin: 0 12px 8px;
  padding: 8px 10px;
  border: 1px solid var(--warn-soft);
  border-radius: var(--r-2);
  background: var(--warn-soft);
  color: var(--warn);
  font-size: 11.5px;
  line-height: 1.45;
}

.stale button {
  flex: none;
  color: var(--warn);
}

.tree {
  padding: 0 12px 12px;
}

.option {
  gap: 6px;
}

.twisty,
.twisty-spacer {
  flex: none;
  width: 16px;
  height: 16px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--faint);
  font-size: 10px;
  line-height: 16px;
  cursor: pointer;
}

.twisty:hover {
  color: var(--ink);
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
