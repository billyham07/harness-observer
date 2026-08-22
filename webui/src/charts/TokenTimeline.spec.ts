import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { QueryRow } from '@/api/types'
import { setLang } from '@/i18n'
import { useTheme } from '@/composables/useTheme'
import TokenTimeline from './TokenTimeline.vue'
import { chartFactoryKey, type ChartFactory, type ChartHandle } from './useG2Chart'

/** A stand-in for G2 that records the lifecycle instead of drawing it. */
function recorder() {
  const specs: Record<string, unknown>[] = []
  let created = 0
  let renders = 0
  let destroys = 0
  const factory: ChartFactory = (): ChartHandle => {
    created++
    return {
      options: (spec) => specs.push(spec),
      render: () => renders++,
      destroy: () => destroys++,
      forceFit: () => {},
    }
  }
  return {
    factory,
    specs,
    counts: () => ({ created, renders, destroys }),
  }
}

const row = (bucket: string, input: number): QueryRow =>
  ({
    bucket,
    turns: 1,
    sessions: 1,
    input_tokens: input,
    cache_read_tokens: input * 10,
    cache_write_tokens: 1,
    prompt_tokens: input * 11,
    output_tokens: 2,
    reasoning_tokens: 1,
    observed_tokens: input * 11 + 2,
    tool_calls: 0,
    tool_errors: 0,
    cost_usd: 0,
    reported_cost_usd: 0,
    cache_hit_ratio: 0,
    first_seen: '',
    last_seen: '',
  }) as QueryRow

const rows = [row('2026-08-01', 10), row('2026-08-02', 20), row('2026-08-03', 30)]

function mountChart(chart: ReturnType<typeof recorder>, props: Record<string, unknown> = {}) {
  return mount(TokenTimeline, {
    props: { rows, bucket: 'day', ...props },
    global: { provide: { [chartFactoryKey as symbol]: chart.factory } },
  })
}

describe('token timeline', () => {
  it('creates one chart on mount and destroys it on unmount', async () => {
    const chart = recorder()
    const wrapper = mountChart(chart)
    await flushPromises()

    expect(chart.counts().created).toBe(1)
    expect(chart.counts().renders).toBe(1)
    expect(chart.counts().destroys).toBe(0)

    wrapper.unmount()
    expect(chart.counts().destroys).toBe(1)
  })

  it('updates in place when the data changes', async () => {
    const chart = recorder()
    const wrapper = mountChart(chart)
    await flushPromises()

    await wrapper.setProps({ rows: [...rows, row('2026-08-04', 40)] })
    await flushPromises()

    // One instance, two option pushes: a fresh chart per update is what leaves
    // an orphaned canvas behind.
    expect(chart.counts().created).toBe(1)
    expect(chart.specs).toHaveLength(2)
    expect(chart.counts().destroys).toBe(0)
    expect(wrapper.findAll('.canvas')).toHaveLength(1)
  })

  it('survives a theme switch and a language switch without duplicating the chart', async () => {
    const chart = recorder()
    const wrapper = mountChart(chart)
    await flushPromises()

    useTheme().set('dark')
    await flushPromises()
    setLang('en')
    await flushPromises()
    setLang('zh')
    await flushPromises()

    expect(chart.counts().created).toBe(1)
    expect(chart.counts().destroys).toBe(0)
    expect(wrapper.findAll('.canvas')).toHaveLength(1)
    useTheme().set('light')
    wrapper.unmount()
  })

  it('keeps the canonical bucket order stable across languages', async () => {
    setLang('en')
    const chart = recorder()
    const wrapper = mountChart(chart)
    await flushPromises()

    const scale = chart.specs[0].scale as { color: { domain: string[]; range: string[] } }
    expect(scale.color.domain).toEqual(['fresh input', 'cache read', 'cache write', 'output'])
    expect(scale.color.range).toHaveLength(4)

    setLang('zh')
    await flushPromises()
    const next = chart.specs[chart.specs.length - 1].scale as { color: { domain: string[] } }
    // Translated labels, same order: colour follows the bucket, never its rank.
    expect(next.color.domain).toEqual(['新鲜输入', '缓存读取', '缓存写入', '输出'])
    wrapper.unmount()
    setLang('en')
  })

  it('hands the container to the empty state when there is nothing to draw', async () => {
    const chart = recorder()
    const wrapper = mountChart(chart, { rows: [] })
    await flushPromises()

    expect(chart.counts().created).toBe(0)
    expect(wrapper.find('.canvas').exists()).toBe(false)
    expect(wrapper.find('.empty').exists()).toBe(true)

    await wrapper.setProps({ rows })
    await flushPromises()
    expect(chart.counts().created).toBe(1)
    wrapper.unmount()
  })
})
