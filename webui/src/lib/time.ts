import type { StringKey } from '@/i18n'

export const DAY = 24 * 36e5

export const dayStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate())
export const dayEnd = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate(), 23, 59, 59)
export const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n)
export const monthStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), 1)
/** Weeks start on Monday, matching the week bucket the query side groups by. */
export const weekStart = (d: Date) => addDays(d, -((d.getDay() + 6) % 7))
export const sameDay = (a: Date | null, b: Date | null) =>
  !!a && !!b && dayStart(a).getTime() === dayStart(b).getTime()

/**
 * localISO is the wire form for a hand-picked instant: local wall-clock time
 * with no zone suffix. The Go side parses it with time.ParseInLocation, so a
 * bookmarked range means the same wall-clock window it was picked in.
 */
export function localISO(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  )
}

export function parseLocal(value: string): Date | null {
  if (!value) return null
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? null : d
}

export interface Preset {
  key: string
  label: StringKey
  span: (now: Date) => [Date, Date] | null
}

/**
 * One list drives the preset buttons, the ?preset= parameter, the label on the
 * control and which button reads as selected. A preset is a function of "now",
 * never a frozen pair of timestamps, so "Today" still means today tomorrow.
 */
export const PRESETS: Preset[] = [
  { key: 'today', label: 'range.today', span: (n) => [dayStart(n), dayEnd(n)] },
  {
    key: 'yesterday',
    label: 'range.yesterday',
    span: (n) => [dayStart(addDays(n, -1)), dayEnd(addDays(n, -1))],
  },
  { key: '24h', label: 'range.24h', span: (n) => [new Date(n.getTime() - DAY), n] },
  { key: '7d', label: 'range.7d', span: (n) => [new Date(n.getTime() - 7 * DAY), n] },
  { key: '30d', label: 'range.30d', span: (n) => [new Date(n.getTime() - 30 * DAY), n] },
  { key: '90d', label: 'range.90d', span: (n) => [new Date(n.getTime() - 90 * DAY), n] },
  { key: 'thisWeek', label: 'range.thisWeek', span: (n) => [weekStart(n), dayEnd(n)] },
  { key: 'thisMonth', label: 'range.month', span: (n) => [monthStart(n), dayEnd(n)] },
  {
    key: 'lastMonth',
    label: 'range.lastMonth',
    span: (n) => [
      new Date(n.getFullYear(), n.getMonth() - 1, 1),
      dayEnd(new Date(n.getFullYear(), n.getMonth(), 0)),
    ],
  },
  { key: 'all', label: 'range.all', span: () => null },
]

export const isPresetKey = (key: string) => PRESETS.some((p) => p.key === key)

export function presetRange(key: string, now: Date = new Date()): { from: string; to: string } {
  const preset = PRESETS.find((p) => p.key === key)
  const span = preset ? preset.span(now) : null
  if (!span) return { from: '', to: '' }
  return { from: localISO(span[0]), to: localISO(span[1]) }
}

/**
 * rangeFromInstants turns a picked pair of instants into the committed range.
 *
 * A range picked end-first is still a range, so it is normalised here rather
 * than in the picker: every caller downstream — the URL, the API, the label —
 * then gets a pair where the first instant is the earlier one, and none of
 * them has to ask.
 */
export function rangeFromInstants(
  value: [number, number] | null,
): { from: string; to: string; preset: string } {
  if (!value) return { from: '', to: '', preset: 'all' }
  const [a, b] = value[0] <= value[1] ? value : [value[1], value[0]]
  return { from: localISO(new Date(a)), to: localISO(new Date(b)), preset: 'custom' }
}
