import type { BucketName, QueryRow } from '@/api/types'

/**
 * Time buckets on a real calendar.
 *
 * The query API returns only buckets that hold evidence, so a run of idle days
 * is simply absent. Plotted on a band scale those days vanish and the axis
 * stops being time: a quiet fortnight and a busy afternoon get the same width.
 * These helpers turn bucket keys back into instants and walk the calendar
 * between the first and last bucket, so an empty period is drawn as an
 * explicit zero instead of being squeezed out.
 *
 * Keys are produced in UTC by the store (internal/query bucketExpr):
 *   hour  2026-09-20T13:00:00Z
 *   day   2026-09-20T00:00:00Z
 *   week  2026-W38      (strftime %W of the Monday date_trunc('week') returns)
 *   month 2026-09
 */

const DAY = 86_400_000

/** Monday-first weekday, 0..6. */
const mondayIndex = (d: Date) => (d.getUTCDay() + 6) % 7

function weekStart(year: number, week: number): number {
  const jan1 = Date.UTC(year, 0, 1)
  const firstMonday = jan1 + ((7 - mondayIndex(new Date(jan1))) % 7) * DAY
  return firstMonday + (week - 1) * 7 * DAY
}

/** The instant a bucket starts, or null for a key this file does not know. */
export function bucketStart(key: string, bucket: BucketName): number | null {
  if (bucket === 'week') {
    const m = /^(\d{4})-W(\d{2})$/.exec(key)
    if (!m) return null
    const start = weekStart(Number(m[1]), Number(m[2]))
    // date_trunc('week') returns a Monday; W00 and weeks outside that year
    // cannot be keys from the store.
    return bucketKey(start, 'week') === key ? start : null
  }
  if (bucket === 'month') {
    const m = /^(\d{4})-(\d{2})$/.exec(key)
    return m ? Date.UTC(Number(m[1]), Number(m[2]) - 1, 1) : null
  }
  const ms = Date.parse(key)
  return Number.isNaN(ms) ? null : ms
}

const pad = (n: number) => String(n).padStart(2, '0')

/** The key the store would produce for the bucket starting at `ms`. */
export function bucketKey(ms: number, bucket: BucketName): string {
  const d = new Date(ms)
  const date = `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
  switch (bucket) {
    case 'hour':
      return `${date}T${pad(d.getUTCHours())}:00:00Z`
    case 'day':
      return `${date}T00:00:00Z`
    case 'month':
      return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}`
    case 'week': {
      // Axis ticks can fall anywhere within a week. Match the store's
      // Monday truncation before choosing the year (Jan 1 may belong to a
      // bucket whose Monday is in December of the preceding year).
      const monday = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()) - mondayIndex(d) * DAY)
      const jan1 = Date.UTC(monday.getUTCFullYear(), 0, 1)
      const yday = Math.round((monday.getTime() - jan1) / DAY)
      return `${monday.getUTCFullYear()}-W${pad(Math.floor((yday + 7) / 7))}`
    }
  }
}

export function nextBucket(ms: number, bucket: BucketName): number {
  const d = new Date(ms)
  switch (bucket) {
    case 'hour':
      return ms + 3_600_000
    case 'day':
      return ms + DAY
    case 'week':
      return ms + 7 * DAY
    case 'month':
      return Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 1)
  }
}

export interface TimePoint {
  /** Bucket start, epoch milliseconds (UTC). */
  t: number
  key: string
  /** The evidence row, or null for a bucket with no records. */
  row: QueryRow | null
}

/** Past this many buckets the calendar is not walked; rows are returned as-is. */
const FILL_LIMIT = 20_000

/**
 * Every bucket from the first row's to the last row's, in order. A bucket the
 * API did not return carries `row: null` and is plotted as zero. If any key
 * cannot be placed on the calendar, nothing is filled: the placeable rows are
 * returned as they are rather than padded around a gap of unknown size.
 */
export function fillBuckets(rows: QueryRow[], bucket: BucketName): TimePoint[] {
  const known = rows
    .map((row) => ({ row, t: bucketStart(row.bucket ?? '', bucket) }))
    .filter((p): p is { row: QueryRow; t: number } => p.t !== null)
    .sort((a, b) => a.t - b.t)
  if (!known.length) return []
  if (known.length !== rows.length) {
    return known.map((p) => ({ t: p.t, key: p.row.bucket ?? '', row: p.row }))
  }
  const byKey = new Map(known.map((p) => [bucketKey(p.t, bucket), p.row]))
  const out: TimePoint[] = []
  const last = known[known.length - 1].t
  for (let t = known[0].t; t <= last; t = nextBucket(t, bucket)) {
    if (out.length > FILL_LIMIT) {
      return known.map((p) => ({ t: p.t, key: p.row.bucket ?? '', row: p.row }))
    }
    const key = bucketKey(t, bucket)
    out.push({ t, key, row: byKey.get(key) ?? null })
  }
  return out
}
