import { beforeEach, describe, expect, it } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { defineComponent, h } from 'vue'
import {
  autoBucket,
  filterParams,
  installQueryState,
  navigate,
  queryObject,
  readQuery,
  resetFilters,
  setFacet,
  setLogsPage,
  setLogsPageSize,
  setLogsSession,
  setRange,
  setSelection,
  state,
} from './useQueryState'
import { presetRange } from '@/lib/time'

const Blank = defineComponent({ render: () => h('div') })

async function boot(path: string): Promise<Router> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Blank },
      { path: '/logs', component: Blank },
    ],
  })
  await router.push(path)
  await router.isReady()
  installQueryState(router)
  await flushPromises()
  return router
}

const url = (router: Router) => decodeURIComponent(router.currentRoute.value.fullPath)

describe('URL state', () => {
  beforeEach(() => resetFilters())

  it('parses a bookmarked dashboard link', async () => {
    await boot(
      '/?from=2026-08-01T00:00:00&to=2026-08-02T23:59:59&sel=claude-code%7Copus&sel=codex-cli' +
        '&project=alpha&project=beta&client=desktop&bucket=hour&auto=1&interval=15',
    )
    expect(state.view).toBe('overview')
    expect(state.from).toBe('2026-08-01T00:00:00')
    expect(state.to).toBe('2026-08-02T23:59:59')
    expect(state.preset).toBe('custom')
    expect(state.sel).toEqual(['claude-code|opus', 'codex-cli'])
    expect(state.facets.project).toEqual(['alpha', 'beta'])
    expect(state.facets.client).toEqual(['desktop'])
    expect(state.bucket).toBe('hour')
    expect(state.auto).toBe(true)
    expect(state.interval).toBe(15)
  })

  it('re-evaluates a named preset on load instead of trusting stale instants', async () => {
    const router = await boot('/?preset=today&from=2020-01-01T00:00:00&to=2020-01-02T00:00:00')
    const today = presetRange('today')
    expect(state.preset).toBe('today')
    expect(state.from).toBe(today.from)
    expect(state.to).toBe(today.to)
    // A shared "today" link means today for whoever opens it, so the address
    // bar is rewritten to the window actually being queried.
    expect(url(router)).toContain(`from=${today.from}`)
    expect(url(router)).not.toContain('2020-01-01')
  })

  it('sends correlated tuples, never three independent lists', async () => {
    await boot('/?sel=claude-code%7Copus%7Cmax&sel=codex-cli%7Cgpt%7Chigh')
    const params = filterParams()
    expect(params.getAll('sel')).toEqual(['claude-code|opus|max', 'codex-cli|gpt|high'])
    expect(params.getAll('harness')).toEqual([])
    expect(params.getAll('model')).toEqual([])
    expect(params.getAll('effort')).toEqual([])
  })

  it('round-trips repeated facet values through the address bar', async () => {
    const router = await boot('/')
    setFacet('project', ['alpha', 'beta'])
    setFacet('provider', ['anthropic'])
    await flushPromises()
    expect(url(router)).toContain('project=alpha&project=beta')

    readQuery('/', router.currentRoute.value.query)
    expect(state.facets.project).toEqual(['alpha', 'beta'])
    expect(state.facets.provider).toEqual(['anthropic'])
  })

  it('keeps an exact custom range to the second', async () => {
    const router = await boot('/')
    setRange('2026-08-03T09:15:07', '2026-08-03T09:45:59', 'custom')
    await flushPromises()
    expect(url(router)).toContain('from=2026-08-03T09:15:07')
    expect(url(router)).toContain('to=2026-08-03T09:45:59')
    // A hand-picked range is carried over literally: no preset key is written.
    expect(url(router)).not.toContain('preset=')

    readQuery('/', router.currentRoute.value.query)
    expect(state.from).toBe('2026-08-03T09:15:07')
    expect(state.to).toBe('2026-08-03T09:45:59')
    expect(state.preset).toBe('custom')
  })

  it('carries the filter across Overview and Logs and resets the reading position', async () => {
    const router = await boot('/')
    setSelection(['claude-code|opus'])
    setFacet('project', ['alpha'])
    setLogsPage(4)
    await flushPromises()
    const before = filterParams().toString()

    navigate('logs')
    await flushPromises()
    expect(state.view).toBe('logs')
    expect(state.logsPage).toBe(1)
    expect(filterParams().toString()).toBe(before)
    expect(url(router)).toContain('/logs')
  })

  it('serves /logs as a direct destination', async () => {
    await boot('/logs?page=3&page_size=100&session=abc')
    expect(state.view).toBe('logs')
    expect(state.logsPage).toBe(3)
    expect(state.logsPageSize).toBe(100)
    expect(state.logsSession).toBe('abc')
    expect(filterParams().get('session')).toBe('abc')
  })

  it('does not leak the Logs reading position into the Overview URL', async () => {
    const router = await boot('/logs')
    setLogsPageSize(100)
    setLogsPage(2)
    await flushPromises()
    expect(url(router)).toContain('page=2')

    navigate('overview')
    await flushPromises()
    expect(url(router)).not.toContain('page=')
    expect(url(router)).not.toContain('page_size=')
    // A session id is a Logs-page filter and must not narrow Overview.
    setLogsSession('abc')
    expect(filterParams().get('session')).toBeNull()
  })

  it('adapts the bucket to the range without replacing the range', async () => {
    await boot('/')
    setRange('2026-08-03T00:00:00', '2026-08-03T12:00:00', 'custom')
    expect(autoBucket()).toBe('hour')
    setRange('2026-01-01T00:00:00', '2026-03-01T00:00:00', 'custom')
    expect(autoBucket()).toBe('day')
    setRange('2020-01-01T00:00:00', '2026-08-01T00:00:00', 'custom')
    expect(autoBucket()).toBe('month')
    // The range itself is untouched by any of that.
    expect(state.from).toBe('2020-01-01T00:00:00')
  })

  it('resets every filter and nothing else', async () => {
    await boot('/?interval=60&auto=1')
    setSelection(['claude-code'])
    setFacet('project', ['alpha'])
    setRange('2026-08-01T00:00:00', '2026-08-02T00:00:00', 'custom')
    resetFilters()

    expect(state.sel).toEqual([])
    expect(state.facets.project).toEqual([])
    expect(state.from).toBe('')
    expect(state.preset).toBe('all')
    // Refresh settings are the user's, not the query's.
    expect(state.auto).toBe(true)
    expect(state.interval).toBe(60)
  })

  it('omits empty values from the address bar', async () => {
    await boot('/?interval=30')
    const query = queryObject()
    expect(query.from).toBeUndefined()
    expect(query.sel).toBeUndefined()
    expect(query.preset).toBeUndefined()
    expect(query.interval).toBe('30')
  })
})
