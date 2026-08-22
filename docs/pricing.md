# API-equivalent cost estimation

The contract this implements is [`docs/cost-estimation.md`](cost-estimation.md).
This file is how it actually works.

## Two costs, never one

| Field | What it is |
| --- | --- |
| `reported_cost_usd` | A cost the source itself persisted (Pi/OMP `usage.cost.total`). Evidence. Sparse: most harnesses record nothing. Never synthesized. |
| `estimated_api_cost_usd` | What the observed tokens would cost at published API rates. Derived. Comparable across harnesses. Not a bill. |

`cost_usd` is kept as the older spelling of `reported_cost_usd` so existing
consumers keep working.

Subscription and OAuth usage is not metered spend, so the estimate is labelled
**Est. API cost** / **API-equivalent** everywhere it appears, and never Actual
Spend, Billed or Charged. The two numbers are never added together and one is
never substituted for the other: a comparison that estimates some harnesses and
reads reported cost for others is not a comparison.

## Query time, not ingest time

Tokens are evidence. A published price is a third party's price list that gets
corrected, discounted and superseded, so it is applied when the question is
asked, not stamped onto stored rows.

Consequences, all deliberate:

- A pricing correction changes historical answers immediately, without
  re-reading a single session file.
- `agent_turns` needs no schema change and no rebuild for pricing.
- The cost is one extra aggregation per query: the same filter and grouping plus
  `model`, folded back onto the requested grouping afterwards. Correlated tuple
  selections, arbitrary time ranges, facets and buckets all apply unchanged,
  because it is the same request.

`query.AggregateCost` is a superset of `query.Aggregate`: with no catalog it
returns exactly the same rows.

## The catalog

`internal/pricing` holds a filtered snapshot of LiteLLM's public model pricing
table, embedded in the binary:

```bash
go run ./scripts/genpricing            # rewrites internal/pricing/catalog.json
```

The generator keeps chat-shaped models and four rates — input, output, cache
read, cache write — which turns a ~1.8 MB upstream file into ~190 KB. It runs by
hand: a build that reaches the network fails on a train, and a price list that
moves under a release makes a number nobody can reproduce.

Precedence at runtime:

1. explicit local overrides
2. the cached public catalog, if one has been downloaded
3. the bundled snapshot

The refresh is best effort. It downloads a price list and nothing else — no
model, session or usage data leaves the machine — with a bounded timeout, at
most once per TTL (24h default), from the background scan rather than from a
page load. A failure, a malformed payload or a suspiciously small table leaves
the working catalog in place; the dashboard degrades to yesterday's prices,
never to no prices. Offline is a supported mode, not a degraded one.

```text
--pricing-refresh=false                     never touch the network
--pricing-overrides <file>                  explicit per-model rates
--pricing-heuristic-cache                   infer missing cache rates (off by default)

HARNESS_OBSERVER_PRICING_REFRESH            1/0
HARNESS_OBSERVER_PRICING_URL                catalog URL
HARNESS_OBSERVER_PRICING_CACHE              cached catalog path
HARNESS_OBSERVER_PRICING_TTL                Go duration, e.g. 24h
HARNESS_OBSERVER_PRICING_OVERRIDES          override file
HARNESS_OBSERVER_PRICING_HEURISTIC_CACHE    1/0
```

An override file is rates only, and an invalid one is an error rather than a
dropped field:

```json
{"models": {"internal-gateway/some-model": {"input": 1e-6, "output": 3e-6}}}
```

## Model resolution

Conservative and explainable. Every rule is an exact lookup of a
deterministically rewritten name — there is no fuzzy, prefix or substring
matching, so a future `gpt-5.7-sol` can never be priced as `gpt-5.6-sol`.

| Rule | Example |
| --- | --- |
| `exact` | `gpt-5.6-sol` |
| `separator-normalized` | `anthropic--claude-…`, `openai-codex:gpt-5.5` |
| `provider-prefix` | `aigw_local/deepseek-v4-flash` → `deepseek-v4-flash` |
| `canonical-alias` | `grok-4.6-build` → `xai/grok-4.6` |
| `vendor-name-reorder` | `claude-4.6-opus` → `claude-opus-4-6` |
| `date-suffix` | `claude-opus-4-6-20260205` → `claude-opus-4-6` |
| `case-insensitive` | `GPT-5.5` → `gpt-5.5` |
| `unique-provider-suffix` | `grok-4.5` → `xai/grok-4.5`, because only one provider publishes it |
| `agreed-provider-suffix` | several providers publish it at identical rates, so the choice cannot change the number |

Anything else is **unpriced**, with a reason. `glm-5.2` is unpriced because
three providers publish it at three different prices, and guessing which one was
meant would be inventing evidence. A reasoning-effort suffix is never stripped:
effort does not appear in a published rate.

`harness-observer pricing status --model <name>` explains one resolution:
observed name → canonical key → rule → catalog source and version → rates.

## The formula

```text
estimated_api_cost_usd =
    input_tokens       × input_rate
  + cache_read_tokens  × cache_read_rate
  + cache_write_tokens × cache_write_rate
  + output_tokens      × output_rate
```

`reasoning_tokens` is absent, and there is no bucket for it. In every source
format verified so far reasoning is a subset of the output counter (see
[`usage-semantics.md`](usage-semantics.md)), so charging it again would double
count it. If a provider ever publishes a separate, non-overlapping reasoning
price, that becomes a new bucket rather than an addition to output.

Rates are validated on load: negative, NaN, infinite and implausible values
(> $1 per token, which is a units bug) are rejected rather than clamped to zero.

## Unknown is not free

A bucket whose rate is unknown contributes nothing to the total and all of its
tokens to `unpriced_tokens`:

```text
priced_tokens
unpriced_tokens
pricing_coverage_ratio    = priced / (priced + unpriced)
pricing_basis             catalog | explicit-override | heuristic | unpriced
```

Coverage is **token weighted**, not model-count weighted. Resolving 99% of model
names means nothing if the unresolved 1% carries most of the tokens. On a corpus
where two of three model names resolve but 81% of the tokens do, coverage
reports 81%.

The UI follows the same rule: a row with no priced tokens shows `–`, never
`$0.00`, and a total below full coverage is marked partial rather than presented
as an authoritative figure.

## Cache rates

Published cache rates are used when the catalog has them. When it does not, the
tokens stay unpriced by default.

`--pricing-heuristic-cache` turns on the CodeBurn-style fallback
(`cache_write = input × 1.25`, `cache_read = input × 0.10`). Everything derived
that way carries `cache_basis=heuristic`, the aggregate's `pricing_basis`
includes `heuristic`, and the dashboard says so. Catalog and inferred coverage
are never merged into one unexplained number.

## Surfaces

```bash
harness-observer pricing status               # catalog, version, freshness, refresh outcome
harness-observer pricing status --model M     # explain one model's resolution
harness-observer pricing refresh              # download now, ignoring the TTL
harness-observer pricing unpriced             # unresolved models ranked by token volume
harness-observer query --cost                 # cost columns in the query table
```

`query --json` always carries the cost fields; the table only does with
`--cost`, so the default output stays about token evidence.

HTTP:

```text
GET  /api/query              rows carry the cost fields
GET  /api/pricing            catalog source, version, freshness, overrides, heuristics
GET  /api/pricing/unpriced   unresolved models ranked by observed tokens
POST /api/pricing/refresh    force a download (dashboard process only)
```

Prometheus gains `harness_observer_estimated_api_cost_usd_total` and
`harness_observer_unpriced_tokens_total`, exported together so the estimate is
never available without the coverage that qualifies it.

Diagnostics report model names, providers, harnesses and counters. Never
prompts, responses or content.

## Dashboard

- **Est. API cost** and **Pricing coverage** KPI tiles, dashed-bordered to mark
  them as estimates, and marked partial below 99.5% coverage.
- An **Estimated cost composition** ribbon in the same four buckets as the token
  ribbon. It is a separate strip because cache reads dominate the token ribbon
  and are nearly free, so the two shapes tell genuinely different stories.
- Sortable `est. API $`, `$/session`, `$/turn`, `$/1M observed` and `coverage`
  columns on every comparison table, separated from the token columns by a rule.
  `$/1M observed` is a derived comparison metric of ours, not a provider rate:
  observed tokens include cache reads.
- A provenance line under the cost ribbon naming the catalog, its version, its
  staleness, any overrides or heuristics in play, and how many tokens are
  excluded from the estimate.
