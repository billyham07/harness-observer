import { describe, expect, it } from 'vitest'
import type { QueryRow } from '@/api/types'
import { bucketKey, bucketStart, fillBuckets } from './timeline'

const row = (bucket: string, input = 1) => ({ bucket, input_tokens: input }) as QueryRow

describe('time buckets', () => {
  it('round-trips every bucket key the store produces', () => {
    const cases: [string, 'hour' | 'day' | 'week' | 'month'][] = [
      ['2026-09-20T13:00:00Z', 'hour'],
      ['2026-09-20T00:00:00Z', 'day'],
      ['2026-W38', 'week'],
      ['2026-W01', 'week'],
      ['2025-W52', 'week'],
      ['2026-09', 'month'],
    ]
    for (const [key, bucket] of cases) {
      expect(bucketKey(bucketStart(key, bucket)!, bucket)).toBe(key)
    }
  })

  it('starts a %W week on its Monday', () => {
    // 2026-01-01 is a Thursday; the first Monday, 2026-01-05, opens W01.
    expect(new Date(bucketStart('2026-W01', 'week')!).toISOString()).toBe('2026-01-05T00:00:00.000Z')
  })

  it('labels ticks at New Year with the week of their Monday, as the store does', () => {
    expect(bucketKey(Date.parse('2026-01-01T12:00:00Z'), 'week')).toBe('2025-W52')
    expect(bucketKey(Date.parse('2026-01-05T23:00:00Z'), 'week')).toBe('2026-W01')
    // date_trunc('week') always returns a Monday, so the store cannot emit W00.
    expect(bucketStart('2026-W00', 'week')).toBeNull()
  })

  it('draws idle days as explicit zero buckets instead of squeezing them out', () => {
    const points = fillBuckets([row('2026-04-20T00:00:00Z', 5), row('2026-04-23T00:00:00Z', 7)], 'day')
    expect(points.map((p) => p.key)).toEqual([
      '2026-04-20T00:00:00Z',
      '2026-04-21T00:00:00Z',
      '2026-04-22T00:00:00Z',
      '2026-04-23T00:00:00Z',
    ])
    expect(points.map((p) => p.row?.input_tokens ?? null)).toEqual([5, null, null, 7])
  })

  it('walks weeks across a year boundary', () => {
    const points = fillBuckets([row('2025-W52'), row('2026-W02')], 'week')
    expect(points.map((p) => p.key)).toEqual(['2025-W52', '2026-W01', '2026-W02'])
  })

  it('walks months, sorting rows that arrive out of order', () => {
    const points = fillBuckets([row('2026-03'), row('2025-12')], 'month')
    expect(points.map((p) => p.key)).toEqual(['2025-12', '2026-01', '2026-02', '2026-03'])
  })

  it('does not pad a calendar around a key it cannot place', () => {
    const points = fillBuckets([row('2026-04-20T00:00:00Z'), row('not-a-bucket')], 'day')
    expect(points).toHaveLength(1)
    expect(points[0].row).not.toBeNull()
  })
})
