import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { defineComponent, h } from 'vue'
import type { LogPage, LogRow } from '@/api/types'
import { setLang } from '@/i18n'
import { UNKNOWN } from '@/lib/format'
import { installQueryState, resetFilters, setLogsPage, setLogsPageSize } from '@/composables/useQueryState'
import LogsPage from './LogsPage.vue'

const Blank = defineComponent({ render: () => h('div') })

const logRow = (over: Partial<LogRow> = {}): LogRow => ({
  timestamp: '2026-08-21T10:00:00Z',
  harness: 'claude-code',
  client: 'desktop',
  provider: 'anthropic',
  model: 'claude-opus-5',
  reasoning_effort: 'max',
  project: 'alpha',
  session_id: '019a-session-id-long-enough',
  turn_id: 'turn-1',
  record_id: 'record-1',
  event_kind: 'assistant',
  usage_basis: 'observed',
  usage_semantics: 'input-includes-cache-read',
  input_tokens: 12,
  cache_read_tokens: 340,
  cache_write_tokens: 5,
  prompt_tokens: 357,
  output_tokens: 9,
  reasoning_tokens: 2,
  observed_tokens: 366,
  native_input_tokens: null,
  native_total_tokens: null,
  tool_calls: 3,
  tool_errors: null,
  tool_error_evidence: '',
  reported_cost_usd: null,
  source_file: 'C:/sessions/a.jsonl',
  source_line: 42,
  interval_ms: 1500,
  est_output_tokens_per_second: 6,
  estimated_cost: null,
  ...over,
})

/**
 * A page that is one screen out of a much larger result. If anything in the
 * client ever paginates locally, this fixture is what catches it: the server
 * says 500 rows exist and hands back two.
 */
const page: LogPage = {
  rows: [logRow(), logRow({
      record_id: 'record-2',
      model: null,
      tool_errors: 2,
      interval_ms: null,
      est_output_tokens_per_second: null,
    })],
  page: 1,
  page_size: 50,
  total: 500,
  pages: 10,
  has_more: true,
  sort: 'time',
  order: 'desc',
  pricing_available: true,
}

let requests: string[] = []

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

async function mountLogs() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Blank },
      { path: '/logs', component: LogsPage },
    ],
  })
  await router.push('/logs')
  await router.isReady()
  installQueryState(router)
  const wrapper = mount(LogsPage, { global: { plugins: [router] } })
  await flushPromises()
  return wrapper
}

const logsRequests = () => requests.filter((r) => r.startsWith('/api/logs'))

describe('logs page', () => {
  beforeEach(async () => {
    setLang('en')
    resetFilters()
    requests = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        requests.push(url)
        if (url.startsWith('/api/logs')) return jsonResponse(page)
        if (url.startsWith('/api/hierarchy')) return jsonResponse({ levels: [], nodes: [] })
        if (url.startsWith('/api/facets')) return jsonResponse({ facets: [] })
        return jsonResponse({})
      }),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('asks the server for one page and renders exactly that page', async () => {
    const wrapper = await mountLogs()

    expect(logsRequests()).toHaveLength(1)
    expect(logsRequests()[0]).toContain('page=1')
    expect(logsRequests()[0]).toContain('page_size=50')
    // 500 rows exist; two came back and two are on screen.
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    wrapper.unmount()
  })

  it('turns a page on the server, not in the browser', async () => {
    const wrapper = await mountLogs()
    setLogsPage(3)
    await flushPromises()

    expect(logsRequests()).toHaveLength(2)
    expect(logsRequests()[1]).toContain('page=3')
    wrapper.unmount()
  })

  it('changes the page size on the server and returns to the first page', async () => {
    const wrapper = await mountLogs()
    setLogsPageSize(100)
    await flushPromises()

    const last = logsRequests()[logsRequests().length - 1]
    expect(last).toContain('page_size=100')
    expect(last).toContain('page=1')
    wrapper.unmount()
  })

  it('renders a field the source never recorded as unknown, not zero', async () => {
    const wrapper = await mountLogs()
    const rows = wrapper.findAll('tbody tr')

    // tool_errors is null on both fixture rows' first entry and 2 on the second.
    expect(rows[0].text()).toContain(UNKNOWN)
    expect(rows[0].text()).not.toMatch(/\b0\b/)
    // The second row has no model recorded.
    expect(rows[1].findAll('.unknown').length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('shows estimated tok/s, and unknown for the first turn of a session', async () => {
    const wrapper = await mountLogs()
    const rows = wrapper.findAll('tbody tr')

    expect(rows[0].findAll('td')[10].text()).toBe('6.0')
    expect(rows[1].findAll('td')[10].text()).toBe(UNKNOWN)
    wrapper.unmount()
  })

  it('opens the detail drawer on the row that was clicked', async () => {
    const wrapper = await mountLogs()
    await wrapper.findAll('tbody tr')[1].trigger('click')
    await flushPromises()

    const drawer = document.body.textContent ?? ''
    expect(drawer).toContain('record-2')
    expect(drawer).toContain('C:/sessions/a.jsonl')
    expect(drawer).not.toContain('record-1')
    wrapper.unmount()
  })
})
