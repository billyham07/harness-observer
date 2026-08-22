import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { QueryRow } from '@/api/types'
import { setLang } from '@/i18n'
import BreakdownTabs, { type Breakdown } from './BreakdownTabs.vue'

const row = (group: Record<string, string>, tokens: number): QueryRow =>
  ({
    group,
    turns: 1,
    sessions: 1,
    input_tokens: 1,
    cache_read_tokens: 1,
    cache_write_tokens: 0,
    prompt_tokens: 2,
    output_tokens: 1,
    reasoning_tokens: 0,
    observed_tokens: tokens,
    tool_calls: 0,
    tool_errors: 0,
    cost_usd: 0,
    reported_cost_usd: 0,
    cache_hit_ratio: 0,
    first_seen: '',
    last_seen: '',
  }) as QueryRow

const breakdowns: Breakdown[] = [
  {
    key: 'harness',
    label: 'panel.byHarness',
    dims: ['harness'],
    rows: [row({ harness: 'claude-code' }, 10), row({ harness: 'codex-cli' }, 5)],
  },
  { key: 'model', label: 'panel.byModel', dims: ['model'], rows: [row({ model: 'opus' }, 7)] },
  { key: 'client', label: 'panel.byClient', dims: ['client'], rows: [] },
]

const mountTabs = () => mount(BreakdownTabs, { props: { breakdowns } })

describe('breakdown tabs', () => {
  it('opens on the first cut and names every other one', () => {
    setLang('en')
    const wrapper = mountTabs()
    const tabs = wrapper.findAll('[role="tab"]')

    expect(tabs).toHaveLength(3)
    expect(tabs.map((t) => t.text())).toEqual(['By harness', 'By model', 'By client'])
    expect(tabs[0].attributes('aria-selected')).toBe('true')
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.text()).toContain('claude-code')
    wrapper.unmount()
  })

  it('switches the table without touching the others', async () => {
    const wrapper = mountTabs()
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[role="tab"]')[1].attributes('aria-selected')).toBe('true')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.text()).toContain('opus')
    expect(wrapper.text()).not.toContain('claude-code')
    wrapper.unmount()
  })

  it('shows the empty state for a cut with no rows', async () => {
    const wrapper = mountTabs()
    await wrapper.findAll('[role="tab"]')[2].trigger('click')
    await flushPromises()

    expect(wrapper.find('.empty').exists()).toBe(true)
    expect(wrapper.findAll('tbody tr')).toHaveLength(0)
    wrapper.unmount()
  })

  it('moves between tabs with the arrow keys', async () => {
    const wrapper = mountTabs()
    const tabs = wrapper.findAll('[role="tab"]')

    await tabs[0].trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.findAll('[role="tab"]')[1].attributes('aria-selected')).toBe('true')

    // And wraps around, so the tablist has no dead end.
    await wrapper.findAll('[role="tab"]')[1].trigger('keydown', { key: 'ArrowLeft' })
    await wrapper.findAll('[role="tab"]')[0].trigger('keydown', { key: 'ArrowLeft' })
    expect(wrapper.findAll('[role="tab"]')[2].attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('only the selected tab is reachable by tab key', () => {
    const wrapper = mountTabs()
    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs.map((t) => t.attributes('tabindex'))).toEqual(['0', '-1', '-1'])
    wrapper.unmount()
  })
})
