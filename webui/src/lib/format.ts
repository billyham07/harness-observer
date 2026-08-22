import { locale } from '@/i18n'

/** What an absent value looks like. Never a zero: the source did not say. */
export const UNKNOWN = '—'

/** Compact magnitude for chart ticks, tree counts and dense cells. */
export function fmt(n: unknown): string {
  const v = Number(n)
  if (!Number.isFinite(v)) return ''
  const abs = Math.abs(v)
  if (abs >= 1e9) return `${(v / 1e9).toFixed(2)}B`
  if (abs >= 1e6) return `${(v / 1e6).toFixed(2)}M`
  if (abs >= 1e3) return `${(v / 1e3).toFixed(1)}k`
  return String(v)
}

/** The full count, grouped for the active locale. */
export const exact = (n: unknown) => Number(n || 0).toLocaleString(locale.value)

export const pct = (part: unknown, whole: unknown) =>
  Number(whole) ? `${((100 * Number(part)) / Number(whole)).toFixed(1)}%` : '–'

export const ratioPct = (v: unknown) => `${(100 * Number(v || 0)).toFixed(1)}%`

/**
 * money always shows the currency, because the number next to it in the same
 * row is a token count and the two must never be confused.
 */
export function money(n: unknown, digits = 2): string {
  const v = Number(n || 0)
  if (!Number.isFinite(v)) return '–'
  const floor = Math.pow(10, -digits)
  if (v !== 0 && Math.abs(v) < floor) return `<$${floor.toFixed(digits)}`
  return `$${v.toLocaleString(locale.value, { minimumFractionDigits: digits, maximumFractionDigits: digits })}`
}

export function shortDate(value: string): string {
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return d.toLocaleString(locale.value, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function fmtTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return UNKNOWN
  return d.toLocaleString(locale.value, { hour12: false })
}

/** dash keeps "not recorded" visible instead of folding it into a zero. */
export const dash = (v: unknown) => (v === null || v === undefined || v === '' ? UNKNOWN : String(v))

export const shortId = (v: string | null | undefined) =>
  !v ? UNKNOWN : v.length > 14 ? `${v.slice(0, 8)}…` : v

export function bucketLabel(value: string | undefined, bucket: string): string {
  if (!value) return ''
  if (bucket === 'week' || bucket === 'month') return value
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return bucket === 'hour'
    ? d.toLocaleString(locale.value, { month: 'short', day: 'numeric', hour: '2-digit' })
    : d.toLocaleDateString(locale.value, { month: 'short', day: 'numeric' })
}

export const ratio = (value: unknown, per: unknown) =>
  Number(per || 0) > 0 ? Number(value || 0) / Number(per) : 0
