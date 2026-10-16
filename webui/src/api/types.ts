// Typed mirrors of the Go read API. These are transcriptions of the `json:`
// tags in internal/query, internal/pricing and internal/web, not a convenient
// re-shaping of them: a field that is nullable in Go is nullable here, because
// "the source did not record this" and "the source recorded zero" are different
// answers and the UI has to be able to tell them apart.

/** Canonical token buckets. The order is the stacking order, everywhere. */
export const BUCKETS = ['input', 'cache_read', 'cache_write', 'output'] as const
export type Bucket = (typeof BUCKETS)[number]

/** Flat dimensions offered as independent facets. */
export const FACET_DIMS = ['client', 'provider', 'project', 'event_kind', 'basis', 'semantics'] as const
export type FacetDim = (typeof FACET_DIMS)[number]

export type BucketName = 'hour' | 'day' | 'week' | 'month'

/** One aggregate row from /api/query. */
export interface QueryRow {
  group?: Record<string, string>
  bucket?: string
  turns: number
  sessions: number
  input_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  prompt_tokens: number
  output_tokens: number
  reasoning_tokens: number
  observed_tokens: number
  tool_calls: number
  tool_errors: number
  cost_usd: number
  reported_cost_usd: number
  cache_hit_ratio: number
  first_seen: string
  last_seen: string

  // Cost fields are present only when a pricing catalog is configured. Their
  // absence is the difference between "no rate was found" and "nobody asked
  // for a rate", and the UI renders those two states differently.
  estimated_api_cost_usd?: number
  cost_input_usd?: number
  cost_cache_read_usd?: number
  cost_cache_write_usd?: number
  cost_output_usd?: number
  priced_tokens?: number
  unpriced_tokens?: number
  pricing_coverage_ratio?: number
  pricing_basis?: string[]
}

export interface QueryResponse {
  rows: QueryRow[]
  count: number
}

export interface HierarchyNode {
  value: string
  path: string
  turns: number
  observed_tokens: number
  selection?: '' | 'partial' | 'full'
  children?: HierarchyNode[]
}

export interface StaleSelection {
  path: string
  tuple: { harness: string; model?: string | null; effort?: string | null }
}

export interface Hierarchy {
  levels: string[]
  nodes: HierarchyNode[]
  stale_selections?: StaleSelection[]
}

export interface FacetValue {
  value: string
  turns: number
  observed_tokens: number
  selected?: boolean
  stale?: boolean
}

export interface Facet {
  dimension: string
  values: FacetValue[]
}

export interface FacetsResponse {
  facets: Facet[]
}

export interface LogCost {
  estimated_api_cost_usd: number | null
  cost_input_usd: number
  cost_cache_read_usd: number
  cost_cache_write_usd: number
  cost_output_usd: number
  priced_tokens: number
  unpriced_tokens: number
  pricing_coverage_ratio: number
  pricing_basis: string
  cache_pricing_basis?: string
  heuristic_rates?: boolean
  match_rule?: string
  canonical_model?: string
  catalog_source?: string
  catalog_version?: string
  unpriced_reason?: string
}

export interface LogRow {
  timestamp: string
  harness: string
  client: string | null
  provider: string | null
  model: string | null
  reasoning_effort: string | null
  project: string | null
  session_id: string
  turn_id: string | null
  record_id: string
  event_kind: string
  usage_basis: string
  usage_semantics: string
  input_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  prompt_tokens: number
  output_tokens: number
  reasoning_tokens: number
  observed_tokens: number
  native_input_tokens: number | null
  native_total_tokens: number | null
  tool_calls: number
  tool_errors: number | null
  tool_error_evidence: string
  reported_cost_usd: number | null
  source_file: string
  source_line: number
  /** Time since the previous turn of the same session; null for the first turn. */
  interval_ms: number | null
  /** Output over interval_ms. An estimate: the interval includes tool and wait time. */
  est_output_tokens_per_second: number | null
  estimated_cost?: LogCost | null
}

export interface LogEmptyState {
  reason: 'not-ingested' | 'empty-range' | 'no-match' | string
  stored_turns?: number
  turns_in_range?: number
}

export interface LogPage {
  rows: LogRow[]
  page: number
  page_size: number
  total: number
  pages: number
  has_more: boolean
  sort: string
  order: string
  pricing_available?: boolean
  empty_state?: LogEmptyState | null
}

export interface PricingStatus {
  source: string
  version: string
  models: number
  fetched_at?: string
  stale: boolean
  bundled_version: string
  bundled_models: number
  refresh_enabled: boolean
  url: string
  cache_path: string
  ttl_seconds: number
  last_refresh_attempt?: string
  last_refresh_error?: string
  heuristic_cache: boolean
  overrides: number
  overrides_path?: string
}

export interface PricingResponse {
  available: boolean
  status?: PricingStatus
}

export interface IngestStats {
  source: string
  roots: string[]
  files_discovered: number
  files_parsed: number
  files_skipped: number
  files_rewound: number
  turns_parsed: number
  turns_ingested: number
  newest_activity?: string
  warnings?: string[]
}

export interface IngestStatus {
  running: boolean
  runs: number
  interval_seconds: number
  last_start?: string
  last_finish?: string
  last_duration_ms: number
  last_error?: string
  last_stats?: IngestStats[]
  schema_version?: number
  db?: string
  version?: string
}
