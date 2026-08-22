# Turn / event logs

The Logs surface answers "what exactly happened, in order", where `query`
answers "how much in total". Both read the same normalized rows in
`agent_turns`; Logs just does not aggregate them.

Three things it is **not**:

- not this program's stdout (that is [program diagnostics](#program-diagnostics)
  below, a different thing with a different name),
- not a proxy or wire log — the observer never sits in the request path, so
  HTTP status, TTFT, retries and upstream error bodies do not exist here and are
  not invented,
- not a content viewer. No prompt, response, reasoning text, tool argument or
  tool output is stored, so none can be listed.

What each source can honestly supply is tabulated in
[`log-evidence-capabilities.md`](log-evidence-capabilities.md). That matrix is
the constraint the DTO below follows.

## `harness-observer logs`

```powershell
go run ./cmd/harness-observer logs `
  --since 2026-08-20T09:00:00 --until 2026-08-21T00:00:00 `
  --harness codex-cli --limit 20 --cost
```

```text
time                       harness    model    effort  project  event      input  cache_read  ...  tool_err  est_usd
2026-08-20T17:00:00+08:00  codex-cli  gpt-5.6  high    demo     assistant  2000   118000      ...  -         0.159000

page 1/3  47 events  page size 20  sorted by time desc
```

| Flag | Meaning |
| --- | --- |
| `--since` / `--until` | arbitrary window; `[since,until)` by default |
| `--bound` | `half-open` (default) or `inclusive`, matching the aggregations |
| `--harness --client --provider --model --effort --project --basis --semantics --event-kind` | comma separated, multi-value |
| `--select` | correlated `harness[\|model[\|effort]]` tuple, repeatable |
| `--session --turn --record` | exact id lookup, comma separated |
| `--search` | case-insensitive substring of **model or project** only |
| `--sort` `--dir` | `time` (default), `tokens`, `output`; `desc` (default) or `asc` |
| `--page` `--limit` | 1-based page, rows per page (hard cap 200) |
| `--strict` | exclude `usage_basis=estimated` |
| `--cost` | add the API-equivalent estimate column |
| `--json` | full DTO, including the estimate and its provenance |

A `-` in the table means **unknown**, never zero: an effort the harness did not
record, an adapter that does not observe tool failures, a model with no
published rate.

## `GET /api/logs`

Same semantics, same parser, same code path — the CLI builds its request through
`serve.LogQueryFromValues` so there is one implementation to be right or wrong.

```text
GET /api/logs?from=2026-08-20&to=2026-08-21&harness=codex-cli&page=2&page_size=50
GET /api/logs?session=019a4c...&sort=tokens&dir=desc
GET /api/logs?sel=codex-cli%7Cgpt-5.6%7Chigh&q=harness-observer
```

| Parameter | Notes |
| --- | --- |
| `from`, `to` | RFC3339 or `YYYY-MM-DD[THH:MM[:SS]]` in local time |
| `bound` | `half-open` (default here) or `inclusive` |
| `harness`, `client`, `provider`, `model`, `effort`, `project`, `basis`, `semantics`, `event_kind` | repeatable or comma separated |
| `sel` | correlated tuple, repeatable, never comma-split |
| `session`, `record`, `turn` | exact match |
| `q` | substring of model or project |
| `sort`, `dir` | allow-list only; anything else is `400` |
| `page`, `page_size` (alias `limit`) | 1-based; page size is clamped to 200 and the effective value is echoed back |

`session`, `record`, `turn`, `q` and `bound` are on the shared filter, so
`/api/query`, `/api/hierarchy` and `/api/facets` accept them too. A Logs page and
the facets beside it therefore describe the same rows.

### Response

```jsonc
{
  "rows": [ /* see the DTO below */ ],
  "page": 1, "page_size": 50, "total": 47, "pages": 1, "has_more": false,
  "sort": "time", "order": "desc",
  "pricing_available": true,
  "empty_state": null
}
```

Per row:

```jsonc
{
  "timestamp": "2026-08-20T10:00:00Z",
  "harness": "claude-code", "client": "Claude Code CLI", "provider": "anthropic",
  "model": "claude-opus-4-6", "reasoning_effort": null, "project": "harness-observer",
  "session_id": "a1", "turn_id": null, "record_id": "a1:1",
  "event_kind": "assistant", "usage_basis": "observed", "usage_semantics": "split-cache",
  "input_tokens": 500, "cache_read_tokens": 40000, "cache_write_tokens": 1200,
  "prompt_tokens": 41700, "output_tokens": 900, "reasoning_tokens": 300,
  "observed_tokens": 42600, "native_input_tokens": 500, "native_total_tokens": null,
  "tool_calls": 1, "tool_errors": null, "tool_error_evidence": "unavailable",
  "reported_cost_usd": null,
  "source_file": "C:/sessions/a1.jsonl", "source_line": 7,
  "estimated_cost": {
    "estimated_api_cost_usd": 0.0525, "priced_tokens": 42600, "unpriced_tokens": 0,
    "pricing_coverage_ratio": 1, "pricing_basis": "catalog", "match_rule": "exact",
    "canonical_model": "claude-opus-4-6", "catalog_source": "cached", "catalog_version": "0e8d2323"
  }
}
```

## The rules this surface keeps

**Null is null.** A dimension the source never wrote stays `null`. It does not
become `""`, `"unknown"` or `0`, in the store or in the DTO.

**`tool_errors` carries its own evidence flag.** `model.Turn.ToolErrors` is an
`int`, so a stored `0` cannot distinguish "no failures happened" from "this
adapter never looks at tool results". Only Pi/OMP currently correlates
tool-result failures, so every other family reports `tool_errors: null` with
`tool_error_evidence: "unavailable"`. A non-zero count is evidence in itself and
is always reported. Add a family to `toolErrorEvidence` in
`internal/query/logs.go` in the same change that teaches its adapter to observe
them — not before.

**Unpriced is not free.** `estimated_api_cost_usd` is `null` when no published
rate resolved, with `unpriced_tokens` and `unpriced_reason` saying why. Prices
are applied per query from the active catalog and are never written into
`agent_turns`; see [`pricing.md`](pricing.md).

**Reported cost and estimated cost stay apart.** `reported_cost_usd` is a number
the source itself persisted. It is never filled in from the catalog, and the
estimate is never called spend.

**Source location, not source content.** `source_file` and `source_line` say
where the evidence lives. No endpoint reads a file path, and none accepts one
from a client.

**Ordering is total.** `ORDER BY <allow-listed key>, harness, record_id` — the
tie-breaker is the table's primary key, so events sharing a timestamp cannot
duplicate or vanish between pages. Sort keys and directions are allow-lists;
values are parameters. Nothing from a client reaches the SQL text.

**Filtering, ordering and paging happen in DuckDB.** No endpoint downloads the
matching rows and slices them in Go, and no client is expected to.

### Half-open windows, and why only here

`/api/logs` and `harness-observer logs` use `[from,to)`. Reading a list window by
window is exactly the case where an inclusive upper bound shows one event twice.

The aggregations (`/api/query`, `/metrics`, `harness-observer query`) keep the
inclusive `to` they were built with: their callers pass human end-of-day bounds
like `23:59:59`, and quietly dropping that second would change numbers people
already read. Pass `bound=inclusive` or `bound=half-open` to pick either
semantics explicitly on any endpoint.

### Empty states

An empty table has three quite different causes, so the API names which one it
is instead of leaving a UI to guess:

```jsonc
"empty_state": { "reason": "not-ingested", "stored_turns": 0, "turns_in_range": 0 }
```

| `reason` | Meaning |
| --- | --- |
| `not-ingested` | the store holds nothing at all — run `ingest`, or `web` |
| `empty-range` | rows exist, none in this window |
| `filtered-out` | rows exist in this window, the filters excluded them |

`empty_state` is present only when `total` is 0. A page number past the end
returns no rows and no empty state, because the data is there.

## Program diagnostics

Separate, small, and about this process rather than about agent activity:
`internal/logx` writes `timestamp LEVEL [component] message key=value` lines to
stderr.

```powershell
harness-observer web --debug          # or: $env:HARNESS_OBSERVER_LOG = "debug"
```

Levels are `debug`, `info`, `warn`, `error`, `off`; the default is `info`. It
exists because background ingestion in `web` has no console of its own — a
failing scan or a failed pricing refresh used to be visible only to whoever
thought to poll `/api/status`. Counters, paths, durations and our own error
strings only: never session content, never credentials.
