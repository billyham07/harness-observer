import { computed, reactive, watch } from 'vue'
import type { LocationQuery, Router } from 'vue-router'
import { FACET_DIMS, type BucketName, type FacetDim } from '@/api/types'
import { isPresetKey, presetRange } from '@/lib/time'

export type View = 'overview' | 'logs'

export interface QueryState {
  view: View
  /** Local wall-clock instants, or '' for an open end. */
  from: string
  to: string
  /** A named preset key, 'custom' for a hand-picked range, or 'all'. */
  preset: string
  bucket: '' | BucketName
  /** Correlated harness|model|effort tuples. Never three independent lists. */
  sel: string[]
  facets: Record<FacetDim, string[]>
  auto: boolean
  interval: number
  logsPage: number
  logsPageSize: number
  logsSession: string
}

const emptyFacets = () =>
  Object.fromEntries(FACET_DIMS.map((d) => [d, [] as string[]])) as Record<FacetDim, string[]>

export const state = reactive<QueryState>({
  view: 'overview',
  from: '',
  to: '',
  preset: 'all',
  bucket: '',
  sel: [],
  facets: emptyFacets(),
  auto: false,
  interval: 30,
  logsPage: 1,
  logsPageSize: 50,
  logsSession: '',
})

const pathFor = (view: View) => (view === 'logs' ? '/logs' : '/')

const asArray = (v: unknown): string[] =>
  (Array.isArray(v) ? v : v === null || v === undefined ? [] : [v]).filter(
    (x): x is string => typeof x === 'string' && x !== '',
  )

const first = (v: unknown): string => {
  const list = asArray(v)
  return list.length ? list[0] : ''
}

/**
 * filterParams is the one serialization of "which evidence are we looking at".
 * Every endpoint gets exactly this, so the chart, the tables and the Logs page
 * can never end up describing three different windows.
 */
export function filterParams(): URLSearchParams {
  const p = new URLSearchParams()
  if (state.from) p.set('from', state.from)
  if (state.to) p.set('to', state.to)
  for (const s of state.sel) p.append('sel', s)
  for (const dim of FACET_DIMS) for (const v of state.facets[dim]) p.append(dim, v)
  if (state.view === 'logs' && state.logsSession) p.set('session', state.logsSession)
  return p
}

/** The address-bar form: the filter plus the view-local reading position. */
export function queryObject(): Record<string, string | string[]> {
  const query: Record<string, string | string[]> = {}
  if (state.from) query.from = state.from
  if (state.to) query.to = state.to
  if (state.sel.length) query.sel = [...state.sel]
  for (const dim of FACET_DIMS) if (state.facets[dim].length) query[dim] = [...state.facets[dim]]
  if (state.view === 'logs' && state.logsSession) query.session = state.logsSession
  if (state.preset && state.preset !== 'custom' && state.preset !== 'all') query.preset = state.preset
  if (state.bucket) query.bucket = state.bucket
  if (state.auto) query.auto = '1'
  query.interval = String(state.interval)
  if (state.view === 'logs') {
    if (state.logsPage > 1) query.page = String(state.logsPage)
    if (state.logsPageSize !== 50) query.page_size = String(state.logsPageSize)
  }
  return query
}

/**
 * readQuery parses a location back into state.
 *
 * A named preset is relative, so it is re-evaluated here: a shared "today" link
 * means today for whoever opens it, not the day it was copied. Only a
 * hand-picked range is carried over literally.
 */
export function readQuery(path: string, query: LocationQuery, now: Date = new Date()) {
  state.view = path.replace(/\/+$/, '') === '/logs' ? 'logs' : 'overview'
  state.from = first(query.from)
  state.to = first(query.to)

  const preset = first(query.preset)
  if (preset && isPresetKey(preset)) {
    state.preset = preset
    const span = presetRange(preset, now)
    state.from = span.from
    state.to = span.to
  } else {
    state.preset = state.from || state.to ? 'custom' : 'all'
  }

  const bucket = first(query.bucket)
  state.bucket = (['hour', 'day', 'week', 'month'] as const).includes(bucket as BucketName)
    ? (bucket as BucketName)
    : ''
  state.sel = asArray(query.sel)
  for (const dim of FACET_DIMS) state.facets[dim] = asArray(query[dim])

  state.auto = first(query.auto) === '1'
  const interval = parseInt(first(query.interval), 10)
  if (Number.isFinite(interval) && interval > 0) state.interval = interval

  const page = parseInt(first(query.page), 10)
  state.logsPage = Number.isFinite(page) && page > 0 ? page : 1
  const size = parseInt(first(query.page_size), 10)
  state.logsPageSize = size === 20 || size === 100 ? size : 50
  state.logsSession = first(query.session)
}

/**
 * autoBucket keeps a custom range readable without ever replacing it: the range
 * is whatever the user picked, only the grouping adapts.
 */
export function autoBucket(): BucketName {
  if (state.bucket) return state.bucket
  if (!state.from || !state.to) return 'day'
  const hours = (new Date(state.to).getTime() - new Date(state.from).getTime()) / 36e5
  if (!Number.isFinite(hours) || hours <= 0) return 'day'
  if (hours <= 72) return 'hour'
  if (hours <= 24 * 120) return 'day'
  return 'month'
}

/** Changes here mean "re-query"; page-local reading position is not included. */
export const filterSignature = computed(() => filterParams().toString())

/** True when anything at all is narrowing the numbers. */
export const hasFilters = computed(
  () =>
    !!state.from ||
    !!state.to ||
    state.sel.length > 0 ||
    FACET_DIMS.some((d) => state.facets[d].length > 0) ||
    !!state.logsSession,
)

/* ------------------------------------------------------------ router sync */

let router: Router | null = null
let lastWritten = ''

const intended = () => (router ? router.resolve({ path: pathFor(state.view), query: queryObject() }).fullPath : '')

/**
 * commit publishes state to the address bar. It is called by the mutators
 * below rather than by a watcher: one explicit owner of the URL is what keeps
 * a replace() from echoing back in as a parse and undoing a pending edit.
 */
export function commit() {
  if (!router) return
  const next = intended()
  if (next === lastWritten) return
  lastWritten = next
  void router.replace(next)
}

/** Navigate between pages, keeping the filter and adding a history entry. */
export function navigate(view: View) {
  if (!router) return
  if (state.view === view) return
  state.view = view
  state.logsPage = 1
  lastWritten = intended()
  void router.push(lastWritten)
}

export function installQueryState(instance: Router) {
  router = instance
  const route = instance.currentRoute
  readQuery(route.value.path, route.value.query)
  // A named preset was just re-evaluated against this moment, so the from/to in
  // the address bar is rewritten to the window actually being queried.
  lastWritten = intended()
  void instance.replace(lastWritten)

  // Back/forward and hand-edited URLs are the only inbound direction left.
  watch(
    () => route.value.fullPath,
    (now) => {
      if (now === lastWritten) return
      readQuery(route.value.path, route.value.query)
      lastWritten = now
      const rewritten = intended()
      if (rewritten !== now) {
        lastWritten = rewritten
        void instance.replace(rewritten)
      }
    },
  )
}

/* --------------------------------------------------------------- mutators */

export function setRange(from: string, to: string, preset: string) {
  state.from = from
  state.to = to
  state.preset = preset
  commit()
}

export function setBucket(bucket: '' | BucketName) {
  state.bucket = bucket
  commit()
}

export function setSelection(sel: string[]) {
  state.sel = sel
  commit()
}

export function setFacet(dim: FacetDim, values: string[]) {
  state.facets[dim] = values
  commit()
}

export function toggleFacetValue(dim: FacetDim, value: string, on: boolean) {
  const current = new Set(state.facets[dim])
  if (on) current.add(value)
  else current.delete(value)
  setFacet(dim, [...current])
}

export function setAuto(auto: boolean, interval?: number) {
  state.auto = auto
  if (interval !== undefined) state.interval = interval
  commit()
}

export function setLogsPage(page: number) {
  state.logsPage = Math.max(1, page)
  commit()
}

export function setLogsPageSize(size: number) {
  state.logsPageSize = size
  state.logsPage = 1
  commit()
}

export function setLogsSession(session: string) {
  state.logsSession = session.trim()
  state.logsPage = 1
  commit()
}

/** Reset clears every filter, and nothing else: theme, language and the open
 * page are the user's, not the query's. */
export function resetFilters() {
  state.from = ''
  state.to = ''
  state.preset = 'all'
  state.bucket = ''
  state.sel = []
  state.facets = emptyFacets()
  state.logsPage = 1
  state.logsSession = ''
  commit()
}
