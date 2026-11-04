import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { QueryRow } from '@/api/types'
import { setLang } from '@/i18n'
import { useTheme } from '@/composables/useTheme'
import { BUCKET_COLORS } from '@/lib/palette'
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

const rows = [row('2026-08-01T00:00:00Z', 10), row('2026-08-02T00:00:00Z', 20), row('2026-08-03T00:00:00Z', 30)]

function mountChart(chart: ReturnType<typeof recorder>, props: Record<string, unknown> = {}) {
  return mount(TokenTimeline, {
    props: { rows, bucket: 'day', ...props },
    global: { provide: { [chartFactoryKey as symbol]: chart.factory } },
  })
}

describe('token timeline', () => {
  it('creates one chart per bucket on mount and destroys each on unmount', async () => {
    const chart = recorder()
    const wrapper = mountChart(chart)
    await flushPromises()

    expect(chart.counts().created).toBe(4)
    expect(chart.counts().renders).toBe(4)
    expect(chart.counts().destroys).toBe(0)

    wrapper.unmount()
    expect(chart.counts().destroys).toBe(4)
  })

  it('updates every row in place when the data changes', async () => {
    const chart = recorder()
    const wrapper = mountChart(chart)
    await flushPromises()

    await wrapper.setProps({ rows: [...rows, row('2026-08-04T00:00:00Z', 40)] })
    await flushPromises()

    // Four instances, two option pushes each: a fresh chart per update is what
    // leaves an orphaned canvas behind.
    expect(chart.counts().created).toBe(4)
    expect(chart.specs).toHaveLength(8)
    expect(chart.counts().destroys).toBe(0)
    expect(wrapper.findAll('.canvas')).toHaveLength(4)
  })

  it('survives a theme switch and a language switch without duplicating a chart', async () => {
    const chart = recorder()
    const wrapper = mountChart(chart)
    await flushPromises()

    useTheme().set('dark')
    await flushPromises()
    setLang('en')
    await flushPromises()
    setLang('zh')
    await flushPromises()

    expect(chart.counts().created).toBe(4)
    expect(chart.counts().destroys).toBe(0)
    expect(wrapper.findAll('.canvas')).toHaveLength(4)
    useTheme().set('light')
    wrapper.unmount()
  })

  it('keeps the canonical bucket order and colours stable across languages', async () => {
    setLang('en')
    const chart = recorder()
    const wrapper = mountChart(chart)
    await flushPromises()

    const names = () => wrapper.findAll('.trend-name').map((n) => n.text())
    expect(names()).toEqual(['fresh input', 'cache read', 'cache write', 'output'])
    const strokes = chart.specs
      .slice(0, 4)
      .map((spec) => ((spec.children as { style: { stroke?: string } }[])[1].style.stroke ?? ''))
    expect(strokes).toEqual([
      BUCKET_COLORS.light.input,
      BUCKET_COLORS.light.cache_read,
      BUCKET_COLORS.light.cache_write,
      BUCKET_COLORS.light.output,
    ])

    setLang('zh')
    await flushPromises()
    // Translated labels, same order: colour follows the bucket, never its rank.
    expect(names()).toEqual(['新鲜输入', '缓存读取', '缓存写入', '输出'])
    wrapper.unmount()
    setLang('en')
  })

  it('plots a bucket the API omitted as an explicit zero on the calendar', async () => {
    const chart = recorder()
    const wrapper = mountChart(chart, { rows: [row('2026-08-01T00:00:00Z', 10), row('2026-08-03T00:00:00Z', 30)] })
    await flushPromises()

    const data = chart.specs[0].data as { key: string; value: number }[]
    expect(data.map((d) => d.key)).toEqual(['2026-08-01T00:00:00Z', '2026-08-02T00:00:00Z', '2026-08-03T00:00:00Z'])
    expect(data.map((d) => d.value)).toEqual([10, 0, 30])
    wrapper.unmount()
  })

  it('does not let the chart theme fade explicitly chosen axis text colours', async () => {
    const chart = recorder()
    const wrapper = mountChart(chart)
    await flushPromises()
    for (const spec of chart.specs.slice(0, 4)) {
      const axis = spec.axis as { x: false | { labelOpacity: number }; y: { labelOpacity: number } }
      expect(axis.y.labelOpacity).toBe(1)
      if (axis.x) expect(axis.x.labelOpacity).toBe(1)
    }
    wrapper.unmount()
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
    expect(chart.counts().created).toBe(4)
    wrapper.unmount()
  })
})
