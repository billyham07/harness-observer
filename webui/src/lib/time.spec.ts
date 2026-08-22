import { describe, expect, it } from 'vitest'
import { dayEnd, dayStart, localISO, presetRange, rangeFromInstants, weekStart } from './time'

const at = (y: number, m: number, d: number, h = 0, min = 0, s = 0) => new Date(y, m - 1, d, h, min, s)

describe('time range', () => {
  it('serialises local wall-clock instants to the second', () => {
    expect(localISO(at(2026, 8, 3, 9, 15, 7))).toBe('2026-08-03T09:15:07')
    expect(localISO(at(2026, 12, 31, 23, 59, 59))).toBe('2026-12-31T23:59:59')
  })

  it('normalises a range picked end-first', () => {
    const early = at(2026, 8, 3, 9, 0, 0).getTime()
    const late = at(2026, 8, 5, 18, 30, 45).getTime()
    expect(rangeFromInstants([late, early])).toEqual({
      from: '2026-08-03T09:00:00',
      to: '2026-08-05T18:30:45',
      preset: 'custom',
    })
  })

  it('keeps a same-day range as a real window rather than collapsing it', () => {
    const from = at(2026, 8, 3, 0, 0, 0).getTime()
    const to = at(2026, 8, 3, 23, 59, 59).getTime()
    const range = rangeFromInstants([from, to])
    expect(range.from).toBe('2026-08-03T00:00:00')
    expect(range.to).toBe('2026-08-03T23:59:59')
  })

  it('preserves an exact time edit down to the second', () => {
    const from = at(2026, 8, 3, 9, 15, 7).getTime()
    const to = at(2026, 8, 3, 9, 15, 8).getTime()
    expect(rangeFromInstants([from, to])).toEqual({
      from: '2026-08-03T09:15:07',
      to: '2026-08-03T09:15:08',
      preset: 'custom',
    })
  })

  it('clears to "all time" rather than to an empty window', () => {
    expect(rangeFromInstants(null)).toEqual({ from: '', to: '', preset: 'all' })
  })

  it('evaluates a named preset against the moment it is asked, not when it was written', () => {
    const monday = at(2026, 8, 3, 14, 22, 9) // a Monday
    expect(presetRange('today', monday)).toEqual({
      from: localISO(dayStart(monday)),
      to: localISO(dayEnd(monday)),
    })
    expect(presetRange('thisWeek', monday).from).toBe(localISO(weekStart(monday)))
    expect(presetRange('lastMonth', monday)).toEqual({
      from: '2026-07-01T00:00:00',
      to: '2026-07-31T23:59:59',
    })
    expect(presetRange('all', monday)).toEqual({ from: '', to: '' })
  })

  it('starts weeks on Monday, matching the week bucket the query side groups by', () => {
    expect(weekStart(at(2026, 8, 9)).getDay()).toBe(1) // a Sunday rolls back
    expect(weekStart(at(2026, 8, 3)).getDate()).toBe(3) // a Monday stays put
  })
})
