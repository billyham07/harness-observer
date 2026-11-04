import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { QueryRow } from '@/api/types'
import { setLang } from '@/i18n'
import TurnAnatomy from './TurnAnatomy.vue'

const freeRow = {
  input_tokens: 100,
  cache_read_tokens: 200,
  cache_write_tokens: 0,
  output_tokens: 50,
  estimated_api_cost_usd: 0,
  cost_input_usd: 0,
  cost_cache_read_usd: 0,
  cost_cache_write_usd: 0,
  cost_output_usd: 0,
  priced_tokens: 350,
  unpriced_tokens: 0,
  pricing_coverage_ratio: 1,
} as QueryRow

describe('itemised API-equivalent cost', () => {
  it('keeps a published zero distinct from an unknown price', () => {
    setLang('en')
    const wrapper = mount(TurnAnatomy, { props: { row: freeRow } })
    const first = wrapper.find('tbody tr').findAll('td')
    expect(first[3].text()).toBe('$0.00')
    expect(first[4].text()).toBe('$0.00')
    // Zero cost has no meaningful relative-cost denominator.
    expect(first[5].text()).toBe('–')
    const total = wrapper.find('tfoot tr').findAll('td')
    expect(total[3].text()).toBe('$0.00')
    expect(total[5].text()).toBe('–')
    wrapper.unmount()
  })

  it('does not render an unresolved price as free', () => {
    const wrapper = mount(TurnAnatomy, {
      props: { row: { ...freeRow, priced_tokens: 0, unpriced_tokens: 350, pricing_coverage_ratio: 0 } },
    })
    const first = wrapper.find('tbody tr').findAll('td')
    expect(first[3].text()).toBe('–')
    expect(first[4].text()).toBe('–')
    wrapper.unmount()
  })

  it('labels the effective rate as a derived comparison, not a catalog price', () => {
    setLang('en')
    const wrapper = mount(TurnAnatomy, { props: { row: freeRow } })
    const header = wrapper.find('th.rate')
    expect(header.text()).toBe('Est. $/1M observed')
    expect(header.attributes('title')).toContain('not a published model rate')
    wrapper.unmount()
  })
})
