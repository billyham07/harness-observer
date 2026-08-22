import { computed, ref } from 'vue'
import type { Facet, FacetDim, Hierarchy, HierarchyNode, StaleSelection } from '@/api/types'
import { compress, selectionMap, toggle, type Selection } from '@/lib/hierarchy'
import { setSelection, state } from './useQueryState'

/**
 * The filter options are shared by both pages: Overview and Logs narrow the
 * same evidence, so a selection made on one is already correct on the other.
 * They are recomputed under the active context on every load, with each facet
 * excluding its own selection.
 */
const hierarchy = ref<Hierarchy>({ levels: [], nodes: [], stale_selections: [] })
const facets = ref<Facet[]>([])

/** Search box inside the hierarchy popover. Transient UI, never a filter. */
const search = ref('')
/** Hierarchy nodes the user closed. Client state: a reload must not reopen them. */
const collapsed = ref(new Set<string>())

export function setHierarchy(next: Hierarchy) {
  hierarchy.value = next
}

export function setFacets(next: Facet[]) {
  facets.value = next
}

export function useFilterOptions() {
  const nodes = computed<HierarchyNode[]>(() => hierarchy.value.nodes ?? [])
  const stale = computed<StaleSelection[]>(() => hierarchy.value.stale_selections ?? [])

  /** Tri-state for every node, derived from the committed selection. */
  const marks = computed<Map<string, Selection>>(() => selectionMap(nodes.value, state.sel))

  const facetFor = (dim: FacetDim) =>
    computed<Facet>(() => facets.value.find((f) => f.dimension === dim) ?? { dimension: dim, values: [] })

  function toggleNode(node: HierarchyNode, depth: number) {
    setSelection(toggle(nodes.value, state.sel, node, depth, stale.value))
  }

  function clearSelection() {
    setSelection([])
  }

  /** Drop only the selections that match nothing here, keeping the rest. */
  function dropStale() {
    const drop = new Set(stale.value.map((s) => s.path))
    setSelection(state.sel.filter((s) => !drop.has(s)))
  }

  function toggleCollapsed(path: string) {
    const next = new Set(collapsed.value)
    if (next.has(path)) next.delete(path)
    else next.add(path)
    collapsed.value = next
  }

  return {
    hierarchy,
    facets,
    nodes,
    stale,
    marks,
    search,
    collapsed,
    facetFor,
    toggleNode,
    clearSelection,
    dropStale,
    toggleCollapsed,
    /** Exposed for tests: the compression that keeps ?sel= short. */
    compress: (selected: Set<string>) => compress(nodes.value, selected, stale.value),
  }
}
