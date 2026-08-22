import type { QueryRow } from '@/api/types'
import type { StringKey } from '@/i18n'
import { ratio } from './format'

export interface Column {
  label: StringKey
  key: string
  /** Derived comparison metrics compute their value instead of reading one. */
  value?: (row: QueryRow) => number
  money?: boolean
  digits?: number
  /** Cost columns are estimates, and are separated from token evidence. */
  cost?: boolean
  format?: (value: number, row: QueryRow) => string
}

export const METRIC_COLUMNS: Column[] = [
  { label: 'col.observed', key: 'observed_tokens' },
  { label: 'col.freshIn', key: 'input_tokens' },
  { label: 'col.cacheRead', key: 'cache_read_tokens' },
  { label: 'col.cacheWrite', key: 'cache_write_tokens' },
  { label: 'col.output', key: 'output_tokens' },
  { label: 'col.reasoning', key: 'reasoning_tokens' },
  { label: 'col.turns', key: 'turns' },
  { label: 'col.sessions', key: 'sessions' },
]

/**
 * Cost columns are estimates at published API rates, never billed spend.
 * $/1M observed tokens is a derived comparison metric of ours, not a provider
 * rate: observed tokens include cache reads, which are cheap.
 */
export const COST_COLUMNS: Column[] = [
  { label: 'col.estCost', key: 'estimated_api_cost_usd', money: true, cost: true },
  {
    label: 'col.perSession',
    key: 'usd_per_session',
    money: true,
    cost: true,
    value: (r) => ratio(r.estimated_api_cost_usd, r.sessions),
  },
  {
    label: 'col.perTurn',
    key: 'usd_per_turn',
    money: true,
    cost: true,
    digits: 4,
    value: (r) => ratio(r.estimated_api_cost_usd, r.turns),
  },
  {
    label: 'col.perMTokens',
    key: 'usd_per_m_tokens',
    money: true,
    cost: true,
    value: (r) => ratio(r.estimated_api_cost_usd, Number(r.observed_tokens || 0) / 1e6),
  },
  {
    label: 'col.coverage',
    key: 'pricing_coverage_ratio',
    cost: true,
    format: (value, row) =>
      row.pricing_coverage_ratio === undefined ? '–' : `${(100 * Number(value || 0)).toFixed(1)}%`,
  },
]

export const ALL_COLUMNS: Column[] = [...METRIC_COLUMNS, ...COST_COLUMNS]

export const columnValue = (col: Column, row: QueryRow): number =>
  col.value ? col.value(row) : Number((row as unknown as Record<string, unknown>)[col.key] || 0)
