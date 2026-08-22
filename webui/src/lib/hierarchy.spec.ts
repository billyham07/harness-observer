import { describe, expect, it } from 'vitest'
import type { HierarchyNode, StaleSelection } from '@/api/types'
import { allLeaves, compress, selectionMap, splitTuple, toggle, visibleTree } from './hierarchy'

const leaf = (path: string, value: string): HierarchyNode => ({
  value,
  path,
  turns: 1,
  observed_tokens: 1,
})

/**
 * A fixture shaped like real evidence: one harness with two models, one of
 * which never recorded an effort, and a second harness that only ever ran one
 * combination. The empty-string level is the "not recorded" value and has to
 * behave like any other.
 */
const tree: HierarchyNode[] = [
  {
    ...leaf('claude-code', 'claude-code'),
    children: [
      {
        ...leaf('claude-code|opus', 'opus'),
        children: [leaf('claude-code|opus|max', 'max'), leaf('claude-code|opus|high', 'high')],
      },
      {
        ...leaf('claude-code|sonnet', 'sonnet'),
        children: [leaf('claude-code|sonnet|', '')],
      },
    ],
  },
  {
    ...leaf('codex-cli', 'codex-cli'),
    children: [
      { ...leaf('codex-cli|gpt', 'gpt'), children: [leaf('codex-cli|gpt|high', 'high')] },
    ],
  },
]

const stale: StaleSelection[] = [
  { path: 'omp|grok', tuple: { harness: 'omp', model: 'grok' } },
]

describe('correlated hierarchy', () => {
  it('reads a harness-level selection as full all the way down', () => {
    const marks = selectionMap(tree, ['claude-code'])
    expect(marks.get('claude-code')).toBe('full')
    expect(marks.get('claude-code|opus')).toBe('full')
    expect(marks.get('claude-code|opus|max')).toBe('full')
    // The other harness is untouched: a selection never widens sideways.
    expect(marks.get('codex-cli')).toBe('')
  })

  it('rolls one selected leaf up into a partial parent', () => {
    const marks = selectionMap(tree, ['claude-code|opus|max'])
    expect(marks.get('claude-code|opus|max')).toBe('full')
    expect(marks.get('claude-code|opus|high')).toBe('')
    expect(marks.get('claude-code|opus')).toBe('partial')
    expect(marks.get('claude-code')).toBe('partial')
  })

  it('compresses a fully selected subtree to its shortest form', () => {
    const everyLeaf = new Set(allLeaves(tree).map((l) => l.path))
    expect(compress(tree, everyLeaf)).toEqual(['claude-code', 'codex-cli'])

    const oneModel = new Set(['claude-code|opus|max', 'claude-code|opus|high'])
    expect(compress(tree, oneModel)).toEqual(['claude-code|opus'])
  })

  it('expands back into explicit tuples when one child is dropped', () => {
    const harness = toggle(tree, [], tree[0], 0)
    expect(harness).toEqual(['claude-code'])

    const opus = tree[0].children![0]
    const withoutOpus = toggle(tree, harness, opus, 1)
    // Never a flat harness list plus a model list: what survives is the model
    // that is still fully selected, so the query cannot admit a combination
    // nobody observed.
    expect(withoutOpus).toEqual(['claude-code|sonnet'])

    // And dropping one of two efforts leaves the surviving leaf spelled out.
    const max = tree[0].children![0].children![0]
    expect(toggle(tree, ['claude-code|opus'], max, 2)).toEqual(['claude-code|opus|high'])
  })

  it('treats a level the source never recorded as a value like any other', () => {
    const sonnet = tree[0].children![1]
    expect(toggle(tree, [], sonnet, 1)).toEqual(['claude-code|sonnet'])
    expect(selectionMap(tree, ['claude-code|sonnet|']).get('claude-code|sonnet')).toBe('full')
  })

  it('keeps a selection that matches nothing here visible and undoable', () => {
    const next = toggle(tree, ['omp|grok'], tree[1], 0, stale)
    expect(next).toContain('codex-cli')
    expect(next).toContain('omp|grok')
  })

  it('splits the wire form, honouring escaped separators', () => {
    expect(splitTuple('claude-code|opus|max')).toEqual(['claude-code', 'opus', 'max'])
    expect(splitTuple('claude-code|sonnet|')).toEqual(['claude-code', 'sonnet', ''])
    expect(splitTuple('harness|a\\|b')).toEqual(['harness', 'a|b'])
  })

  it('filters the visible tree without touching the selectable one', () => {
    const filtered = visibleTree(tree, 'sonnet')
    expect(filtered).toHaveLength(1)
    expect(filtered[0].children).toHaveLength(1)
    expect(filtered[0].children![0].value).toBe('sonnet')
    // Selection still resolves against the full tree behind the filter.
    expect(selectionMap(tree, ['codex-cli']).get('codex-cli|gpt|high')).toBe('full')
    expect(visibleTree(tree, 'nothing-here')).toEqual([])
  })
})
