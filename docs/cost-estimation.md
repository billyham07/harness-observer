# API-equivalent cost estimation

This document defines the cost-estimation contract for harness-observer.
[`pricing.md`](pricing.md) describes the implementation that satisfies it.

## Goal

Add a useful `$` view for comparing coding-agent harnesses without pretending subscription/OAuth usage is an actual metered bill.

The primary question is:

```text
For the observed token/cache usage, what would the same model usage cost at its published API-equivalent rates?
```

This is primarily a **comparison metric** across:

```text
harness × model × reasoning_effort × project × time
```

It is not billing, invoice reconstruction, subscription allocation, or provider account reconciliation.

## Keep two cost meanings separate

Do not overload the existing `cost_usd` meaning.

Canonical concepts:

```text
reported_cost_usd
estimated_api_cost_usd
```

### `reported_cost_usd`

A cost explicitly persisted by the source session/provider, for example Pi/OMP `usage.cost.total`.

Properties:

- evidence-backed
- source-native accounting semantics
- may be unavailable for most harnesses
- must not be synthesized from a pricing table

### `estimated_api_cost_usd`

A derived API-equivalent estimate from normalized token buckets and a pricing catalog.

Properties:

- derived, not actual spend
- suitable for cross-harness/model comparisons
- calculated using the canonical normalized token buckets
- pricing provenance must be visible
- unknown/unpriced usage remains unknown, never silently `$0`

Dashboard wording must use language such as **API-equivalent cost**, **estimated cost**, or **est. API cost**. Do not label it Actual Spend / Billed / Charged.

## Reference implementation pattern

CodeBurn is a useful reference for the pricing subsystem. Its implementation includes:

- bundled LiteLLM price snapshot
- best-effort refresh from LiteLLM's model pricing JSON
- 24-hour cache TTL
- model aliases/canonicalization
- provider-prefix stripping
- exact/canonical/prefix lookup
- fallback pricing data
- unknown/unpriced-model reporting
- separate input/output/cache-write/cache-read rates

Use those ideas, but preserve harness-observer's stricter evidence semantics.

## Pricing catalog

Create a dedicated package, e.g.:

```text
internal/pricing
```

Suggested model:

```go
type Rates struct {
    InputPerToken      *float64
    OutputPerToken     *float64
    CacheReadPerToken  *float64
    CacheWritePerToken *float64
}

type Match struct {
    ObservedModel string
    CanonicalModel string
    Source string
    SourceVersion string
    Rates Rates
    RateBasis RateBasis
}
```

`RateBasis` should distinguish at least:

```text
catalog
explicit-override
heuristic
unpriced
```

Keep the model/rate value space open.

### Data source order

Recommended precedence:

1. explicit harness-observer overrides for known bad/missing catalog entries
2. fresh cached remote catalog
3. bundled catalog snapshot
4. optional fallback catalog
5. unpriced

A refresh failure must never make the dashboard unusable; bundled data is the offline baseline.

### Network behavior

The observer itself remains local-first.

- no network request is required to show costs
- bundled pricing snapshot must work offline
- remote pricing refresh is best-effort
- cache refreshed no more frequently than approximately 24 hours by default
- bounded timeout
- malformed remote data is rejected without destroying the last good catalog
- expose a CLI option / environment variable to disable pricing refresh entirely if practical

Do not send session/model usage data to the pricing endpoint. The refresh downloads only a public pricing catalog.

## Model-name resolution

This must be conservative and debuggable.

Support:

- exact model id
- provider-prefixed id
- canonical aliases
- known harness naming variants
- date suffix normalization where safe
- case-insensitive exact fallback where unambiguous

Examples that should be considered by tests include current observed forms such as:

```text
gpt-5.6-sol
gpt-5.6-luna
openai-codex:gpt-5.5
anthropic--claude-4.6-opus
claude-opus-4-6
grok-4.6-build
```

Do not use broad fuzzy matching that can silently price a future model as an older one.

Do not introduce a generic `gpt-5.6` prefix alias if it could swallow distinct future variants.

Return enough match metadata to explain which catalog key/rate was used.

## Cost formula

Calculate against the canonical normalized token buckets, never raw provider-native counters.

Base formula:

```text
estimated_api_cost_usd =
    input_tokens       × input_rate
  + cache_read_tokens  × cache_read_rate
  + cache_write_tokens × cache_write_rate
  + output_tokens      × output_rate
```

### Reasoning tokens

`reasoning_tokens` is generally a subset/detail of provider output accounting in the current normalized contract. Do **not** add reasoning tokens again on top of `output_tokens` unless a future pricing source explicitly establishes a separate non-overlapping billed bucket.

### Cache rates

Prefer explicit published/catalog cache rates.

If the pricing catalog omits cache rates, do not silently pretend the cost is known.

A CodeBurn-style heuristic may be supported as an optional fallback:

```text
cache_write_rate = input_rate × 1.25
cache_read_rate  = input_rate × 0.10
```

but any row/aggregate using such inferred rates must carry `rate_basis=heuristic` (or equivalent) and the UI must be able to distinguish it from catalog pricing.

Do not merge heuristic and catalog coverage into one unexplained number.

## When some buckets are unpriced

Avoid all-or-nothing `$0` behavior.

For each estimated row/aggregate track enough metadata to distinguish:

```text
fully priced
partially priced
unpriced
```

Suggested measures:

```text
priced_tokens
unpriced_tokens
pricing_coverage_ratio
estimated_api_cost_usd
```

Coverage should be token-weighted, not model-count weighted.

Example:

```text
99% of rows are local/free but the remaining 1% contains most billable tokens
```

must not report misleadingly high pricing coverage simply because most model IDs resolved.

Expected-free local models may be treated separately if a reliable rule exists. Do not classify unknown remote models as free just because no price was found.

## Storage vs query-time calculation

Prefer **query-time cost calculation** from token evidence plus the active pricing catalog.

Reasons:

- pricing changes over time
- catalog corrections should not require rebuilding session evidence
- token evidence remains authoritative and stable
- different pricing snapshots can be compared later

Do not permanently stamp today's public price onto every `agent_turns` row as though it were source evidence.

If performance requires materialization later, keep it clearly separated from `agent_turns` and version it by pricing snapshot/catalog version.

`reported_cost_usd` remains stored source evidence where available.

## Query/API surface

Extend aggregate results with at least:

```text
reported_cost_usd
estimated_api_cost_usd
priced_tokens
unpriced_tokens
pricing_coverage_ratio
pricing_basis breakdown
```

The estimate must obey all existing filters, especially correlated:

```text
(harness, model, reasoning_effort)
```

and arbitrary time ranges.

Add a diagnostic endpoint/query operation for unresolved pricing, for example:

```text
/api/pricing/unpriced
```

or equivalent, returning model + token volume so the largest missing coverage is obvious.

Suggested diagnostic fields:

```text
observed_model
provider
harness
observed_tokens
canonical_candidate?
reason
```

Do not expose prompts/content.

## Dashboard

Add estimated cost without turning the page into a finance dashboard.

Recommended additions:

### KPI

```text
Est. API cost     $123.45
Pricing coverage  96.8%
```

Tooltip/subtext:

```text
API-equivalent estimate from public model rates; not actual subscription spend.
```

If coverage is materially incomplete, visually mark the cost as partial rather than displaying an authoritative-looking total.

### Tables

Add a sortable `$` column to the main comparison tables, especially:

```text
harness → model → effort
model
project
```

Useful comparison fields:

```text
Est. API cost
$/session
$/turn
$/1M observed tokens
pricing coverage
```

`$/1M observed tokens` is a derived comparison metric and should be named clearly; it is not a provider rate.

### Cost composition

Where useful, expose components:

```text
fresh input $
cache read $
cache write $
output $
```

Do not need a separate large chart in MVP if table/KPI presentation is clearer.

### Reported vs estimated

If source-reported cost exists, expose it separately (possibly in detail/table views). Never silently substitute source-reported cost for estimated API cost for some harnesses while estimating others; that destroys cross-harness comparability.

## CLI

Extend `query` output with cost fields or provide opt-in columns.

Useful commands/options may include:

```text
harness-observer pricing status
harness-observer pricing refresh
harness-observer pricing unpriced
```

Exact CLI shape is implementation-defined; do not add commands just to satisfy naming if the same diagnostics fit cleanly elsewhere.

At minimum users/agents need a way to see:

- catalog source/version/time
- whether remote refresh succeeded
- unresolved models ranked by token volume
- explicit overrides/heuristic usage

## Tests

Required regression coverage:

- exact model resolution
- alias/canonical model resolution
- provider-prefixed model resolution
- variant names (`gpt-5.6-sol` vs `gpt-5.6-luna`) stay distinct
- unknown model does not become `$0`
- negative/invalid catalog rates are rejected
- input/output/cache-read/cache-write formula
- reasoning tokens are not double charged
- heuristic cache rates are marked heuristic
- partial coverage calculation
- pricing refresh failure falls back to bundled snapshot
- stale cached pricing refresh behavior
- correlated harness/model/effort selections preserve cost semantics
- dashboard auto refresh does not reset cost/filter state

## Agent execution contract

Implement this as a production feature, not a pricing PoC.

- Inspect CodeBurn's pricing implementation for mature edge cases, but do not copy its semantics blindly.
- Keep token evidence and price estimates separate.
- Never call estimated API-equivalent cost actual spend.
- Do not map unknown models to `$0`.
- Do not silently guess missing cache prices.
- Do not infer model pricing from reasoning effort.
- Do not mutate/re-ingest session files because public pricing changed.
- Keep the dashboard functional offline using bundled pricing data.
- Keep model matching conservative and testable.
- Preserve existing arbitrary time, hierarchy tuple filtering, and faceted filtering.
- Keep `go test ./...` green.

## Definition of done

- [ ] bundled offline pricing catalog exists
- [ ] optional/best-effort refreshed catalog with bounded cache/timeout exists
- [ ] conservative model alias/canonicalization works
- [ ] input/output/cache-read/cache-write priced separately
- [ ] reasoning output is not double charged
- [ ] source-reported cost and estimated API cost are separate semantics
- [ ] API-equivalent estimate works under all existing filters/groups
- [ ] pricing coverage is calculated and visible
- [ ] unknown/unpriced models are surfaced and ranked by token impact
- [ ] heuristic rates, if supported, are visibly marked
- [ ] dashboard shows Est. API cost + coverage
- [ ] model/harness/effort and project tables expose sortable cost metrics
- [ ] UI clearly states estimate is not actual subscription spend
- [ ] offline dashboard remains fully functional
- [ ] pricing/model regression tests are present
- [ ] `go test ./...` passes
