import { beforeEach, describe, expect, it } from 'vitest'
import type { Facet, Hierarchy } from '@/api/types'
import { setFacets, setHierarchy, useFilterOptions } from './useFilterOptions'
import { resetFilters, setFacet, setRange, setSelection, state } from './useQueryState'

const facet = (dimension: string, values: Facet['values']): Facet => ({ dimension, values })
const value = (v: string, tokens: number, extra: Partial<Facet['values'][number]> = {}) => ({
  value: v,
  turns: 1,
  observed_tokens: tokens,
  ...extra,
})

const hierarchy: Hierarchy = {
  levels: ['harness', 'model', 'effort'],
  nodes: [
    {
      value: 'claude-code',
      path: 'claude-code',
      turns: 1,
      observed_tokens: 10,
      children: [
        {
          value: 'opus',
          path: 'claude-code|opus',
          turns: 1,
          observed_tokens: 10,
          children: [{ value: 'max', path: 'claude-code|opus|max', turns: 1, observed_tokens: 10 }],
        },
      ],
    },
  ],
  stale_selections: [{ path: 'omp|grok', tuple: { harness: 'omp', model: 'grok' } }],
}

describe('filter options', () => {
  beforeEach(() => {
    resetFilters()
    setFacets([])
    setHierarchy({ levels: [], nodes: [], stale_selections: [] })
  })

  it('recomputing one facet leaves another dimension’s selection alone', () => {
    const { facetFor } = useFilterOptions()
    setFacet('client', ['desktop'])
    setFacet('project', ['alpha'])

    // A later response narrows the project options; client is a different
    // dimension and its selection has nothing to do with it.
    setFacets([
      facet('project', [value('beta', 5)]),
      facet('client', [value('desktop', 3, { selected: true })]),
    ])

    expect(state.facets.client).toEqual(['desktop'])
    expect(state.facets.project).toEqual(['alpha'])
    expect(facetFor('project').value.values.map((v) => v.value)).toEqual(['beta'])
  })

  it('keeps a selected value that stopped matching visible instead of dropping it', () => {
    const { facetFor } = useFilterOptions()
    setFacet('project', ['alpha'])
    setFacets([
      facet('project', [value('alpha', 0, { selected: true, stale: true }), value('beta', 5)]),
    ])

    const values = facetFor('project').value.values
    expect(values.map((v) => v.value)).toContain('alpha')
    expect(values.find((v) => v.value === 'alpha')?.stale).toBe(true)
    expect(state.facets.project).toEqual(['alpha'])
  })

  it('reports stale hierarchy selections and drops only those', () => {
    const { stale, dropStale } = useFilterOptions()
    setSelection(['claude-code', 'omp|grok'])
    setHierarchy(hierarchy)

    expect(stale.value.map((s) => s.path)).toEqual(['omp|grok'])
    dropStale()
    expect(state.sel).toEqual(['claude-code'])
  })

  it('a data refresh replaces data, not the user’s filters', () => {
    setRange('2026-08-01T00:00:00', '2026-08-02T23:59:59', 'custom')
    setSelection(['claude-code|opus|max'])
    setFacet('project', ['alpha'])

    // What a background refresh does: new options, new counts, same question.
    setHierarchy(hierarchy)
    setFacets([facet('project', [value('alpha', 9, { selected: true })])])

    expect(state.from).toBe('2026-08-01T00:00:00')
    expect(state.to).toBe('2026-08-02T23:59:59')
    expect(state.preset).toBe('custom')
    expect(state.sel).toEqual(['claude-code|opus|max'])
    expect(state.facets.project).toEqual(['alpha'])
  })

  it('derives the tri-state from the committed selection, not the last response', () => {
    const { marks, toggleNode, nodes } = useFilterOptions()
    setHierarchy({ ...hierarchy, stale_selections: [] })
    setSelection([])
    expect(marks.value.get('claude-code')).toBe('')

    toggleNode(nodes.value[0], 0)
    expect(state.sel).toEqual(['claude-code'])
    expect(marks.value.get('claude-code|opus|max')).toBe('full')
  })
})
