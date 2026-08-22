import type { HierarchyNode, StaleSelection } from '@/api/types'

export type Selection = '' | 'partial' | 'full'

/**
 * splitTuple mirrors the server's wire form: an unescaped "|" separates levels,
 * a present-but-empty level is the unknown value, an absent level means "any".
 */
export function splitTuple(path: string): string[] {
  const segments: string[] = []
  let current = ''
  let escaped = false
  for (const ch of path) {
    if (escaped) {
      current += ch
      escaped = false
    } else if (ch === '\\') escaped = true
    else if (ch === '|') {
      segments.push(current)
      current = ''
    } else current += ch
  }
  segments.push(current)
  return segments
}

/** Every leaf (effort level) under a node at the given depth. */
export function leavesOf(node: HierarchyNode, depth: number): HierarchyNode[] {
  if (depth === 2) return [node]
  const out: HierarchyNode[] = []
  for (const child of node.children ?? []) out.push(...leavesOf(child, depth + 1))
  return out
}

export function allLeaves(nodes: HierarchyNode[]): HierarchyNode[] {
  const out: HierarchyNode[] = []
  for (const h of nodes ?? []) out.push(...leavesOf(h, 0))
  return out
}

function rollUp(children: Selection[]): Selection {
  if (!children.length) return ''
  let full = 0
  for (const c of children) {
    if (c === 'partial') return 'partial'
    if (c === 'full') full++
  }
  if (full === 0) return ''
  return full === children.length ? 'full' : 'partial'
}

/**
 * selectionMap recomputes every node's tri-state from the committed selection.
 *
 * The client, not the last server response, is the authority between renders.
 * Deriving it from the previous response instead would drop a selection made
 * while a reload was still in flight.
 */
export function selectionMap(nodes: HierarchyNode[], sel: string[]): Map<string, Selection> {
  const marks = new Map<string, Selection>()
  const tuples = sel.map(splitTuple)
  for (const h of nodes ?? []) {
    const modelMarks: Selection[] = []
    for (const m of h.children ?? []) {
      const effortMarks: Selection[] = []
      for (const e of m.children ?? []) {
        const hit = tuples.some(
          (tuple) =>
            tuple[0] === h.value &&
            (tuple.length < 2 || tuple[1] === m.value) &&
            (tuple.length < 3 || tuple[2] === e.value),
        )
        const mark: Selection = hit ? 'full' : ''
        marks.set(e.path, mark)
        effortMarks.push(mark)
      }
      const mark = rollUp(effortMarks)
      marks.set(m.path, mark)
      modelMarks.push(mark)
    }
    marks.set(h.path, rollUp(modelMarks))
  }
  return marks
}

/**
 * compress turns an explicit leaf set back into the shortest equivalent tuple
 * list: a fully selected harness collapses to one entry, a fully selected model
 * to one entry, everything else stays a leaf. Selections that match nothing in
 * the current context are kept untouched so they stay visible and undoable.
 */
export function compress(
  nodes: HierarchyNode[],
  selectedLeaves: Set<string>,
  stale: StaleSelection[] = [],
): string[] {
  const out: string[] = []
  for (const harness of nodes ?? []) {
    const hLeaves = leavesOf(harness, 0)
    if (hLeaves.length && hLeaves.every((l) => selectedLeaves.has(l.path))) {
      out.push(harness.path)
      continue
    }
    for (const model of harness.children ?? []) {
      const mLeaves = model.children ?? []
      if (mLeaves.length && mLeaves.every((l) => selectedLeaves.has(l.path))) {
        out.push(model.path)
        continue
      }
      for (const leaf of mLeaves) if (selectedLeaves.has(leaf.path)) out.push(leaf.path)
    }
  }
  for (const s of stale ?? []) out.push(s.path)
  return [...new Set(out)]
}

/**
 * toggle flips one node and returns the new committed selection. A parent is
 * one click, and every child underneath stays independently editable, because
 * the edit happens on the explicit leaf set and is re-compressed afterwards.
 */
export function toggle(
  nodes: HierarchyNode[],
  sel: string[],
  node: HierarchyNode,
  depth: number,
  stale: StaleSelection[] = [],
): string[] {
  const marks = selectionMap(nodes, sel)
  const selected = new Set(
    allLeaves(nodes)
      .filter((l) => marks.get(l.path) === 'full')
      .map((l) => l.path),
  )
  const affected = leavesOf(node, depth).map((l) => l.path)
  const turnOn = !affected.every((p) => selected.has(p))
  for (const p of affected) {
    if (turnOn) selected.add(p)
    else selected.delete(p)
  }
  return compress(nodes, selected, stale)
}

/**
 * visibleTree applies the search box. Filtering is done on a copy so the
 * selection logic keeps working against the full observed tree.
 */
export function visibleTree(nodes: HierarchyNode[], query: string): HierarchyNode[] {
  const q = query.trim().toLowerCase()
  if (!q) return nodes ?? []
  const out: HierarchyNode[] = []
  for (const h of nodes ?? []) {
    const models: HierarchyNode[] = []
    for (const m of h.children ?? []) {
      const efforts = (m.children ?? []).filter((e) =>
        `${h.value} ${m.value} ${e.value}`.toLowerCase().includes(q),
      )
      if (efforts.length || `${h.value} ${m.value}`.toLowerCase().includes(q)) {
        models.push({ ...m, children: efforts.length ? efforts : m.children })
      }
    }
    if (models.length || h.value.toLowerCase().includes(q)) {
      out.push({ ...h, children: models.length ? models : h.children })
    }
  }
  return out
}
