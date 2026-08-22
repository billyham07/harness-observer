# Turn / event logs implementation contract

## Goal

Add a first-class **Logs** data surface for inspecting individual normalized coding-agent events. This is not application stdout logging and not an HTTP proxy request log. It is a searchable, paginated view over the same local session evidence already normalized into `agent_turns`.

Reference ideas:

- `lidge-jun/opencodex`: useful request-log shape and diagnostics mindset (model/provider/status/duration/TTFT/usage/attempt/error), but OpenCodex is a proxy and therefore sees wire-level facts harness-observer does not. Do not invent those fields here.
- `billyham07/higress-ai-key-admin`: preferred product pattern: server-side time/filter query, stable pagination/sorting, clear empty states, dense table, row detail surface, explicit timezone/time range, and filters that reflect valid data rather than decorative UI.

## Non-negotiable evidence boundary

The observer remains passive and content-minimizing.

Do not add collection of:

- prompts or model response text
- reasoning text
- tool arguments or tool output
- OAuth/access/API keys or environment secrets
- raw session JSON blobs
- network interception / wrappers / proxy hooks

Do not fabricate proxy-only data such as HTTP status, TTFT, upstream error, service tier, retries, or transport phase when a harness transcript does not persist them.

A missing field is `null`/absent, never a guessed zero or success value.

## Data model

The existing normalized row is the source of truth. The log API should expose a DTO with at least:

```text
timestamp
harness
client
provider
model
reasoning_effort
project
session_id
turn_id
record_id
event_kind
usage_basis
usage_semantics
input_tokens
cache_read_tokens
cache_write_tokens
output_tokens
reasoning_tokens
observed_tokens
tool_calls
tool_errors
reported_cost_usd
source_file
source_line
```

Also expose query-time API-equivalent estimated cost and pricing coverage/provenance when a pricer is configured, using the already-merged pricing subsystem. Do not stamp estimated pricing into `agent_turns`.

`source_file` is diagnostic metadata already stored by the project. It may be returned by the local-only API/detail view, but never read arbitrary file contents from a path supplied by the client.

## Store/query API

Implement a dedicated log query path, not a client-side download of all turns.

Recommended shape:

```text
GET /api/logs
GET /api/logs/facets   (or reuse/extend existing facet machinery cleanly)
```

`/api/logs` must support:

- arbitrary `[from,to)` range using the same time parser/semantics as the overview
- harness/model/reasoning-effort correlated tuple selection
- client/provider/project/event-kind/usage-basis filters
- optional session id / turn id / record id exact search
- optional model/project text search if it can remain indexed/cheap
- page size 20/50/100 with a hard upper bound
- newest-first default order
- deterministic tie-breaker so paging cannot duplicate/skip rows when timestamps are equal
- total row count (or a clearly documented cursor contract); for current local DuckDB volume, count + offset pagination is acceptable
- stable sort allow-list only; never interpolate arbitrary client SQL

Do not load all matching rows into Go and paginate afterward. Push filtering/order/limit into DuckDB.

### SQL safety/correctness

- parameterize values
- allow-list sort keys/directions
- preserve correlated `harness|model|effort` tuple semantics exactly as overview query does
- exact half-open time range `[from,to)`
- null values remain null/unknown; do not coerce missing dimensions into misleading labels in storage

## Log detail

A selected row needs enough metadata for diagnosis without exposing conversation content. Detail can include all normalized/evidence fields plus:

- canonical prompt-token composition
- native input/total counters when persisted
- source evidence location (`source_file`, `source_line`)
- reported cost if source supplied it
- API-equivalent cost estimate + pricing match rule/catalog key/source/version when resolvable

If later adapters add authoritative duration or other diagnostic fields, evolve the schema explicitly. Do not scrape them from text heuristically for this PR.

## Facets and empty states

The data API must make these UI states distinguishable:

```text
no data in selected time range
filters matched no rows
source/harness not yet ingested
```

Reuse the existing facet/correlated-selector rules where possible rather than introducing an independent filtering semantics for Logs.

## CLI surface

Add a compact diagnostic CLI surface if useful, e.g.:

```text
harness-observer logs --since ... --until ... --harness ... --limit 50
harness-observer logs --session <id> --json
```

Exact naming is flexible. It should query the local store and use the same backend semantics as `/api/logs`; do not build a second parser/query implementation.

## Application logging (small but useful)

This feature is about event logs, but add minimal structured **program diagnostics** where the current code emits ad-hoc errors/warnings if a common logger abstraction is genuinely useful. Keep it small:

- levels: debug/info/warn/error
- timestamp + component + message
- stderr by default
- no secrets/session content
- optional debug verbosity flag/env

Do not turn this PR into a logging-framework migration. The product Logs page must not simply tail process stdout.

## Tests

Required regression coverage:

- exact `[from,to)` boundaries
- newest-first deterministic pagination with equal timestamps
- page 1/page 2 have no duplicates or gaps
- hard page-size cap
- correlated harness/model/effort selectors remain correlated
- independent facets compose correctly
- exact session/turn/record id search
- null dimensions are represented honestly
- source file/line returned without reading source content
- pricing estimate remains query-time and unknown pricing is not `$0`
- invalid sort key cannot become SQL
- API returns distinct empty states or enough metadata for UI to derive them
- no prompt/response/tool payload columns are added to schema/DTO

## Agent execution contract

Implement the feature, not another design/PoC.

1. Inspect current `main` query/store/serve code first.
2. Reuse existing request filter structs/helpers where semantically correct.
3. Keep one query semantics across CLI/API/UI.
4. Do not regress cost, pricing coverage, time ranges, hierarchy filters, background ingest, or DuckDB one-writer constraints.
5. Do not add npm/bundler dependencies; this PR is primarily backend/query contract.
6. Any real-corpus mismatch becomes a minimized regression fixture/test where applicable.
7. `gofmt`, `go vet`, and `go test ./...` must remain green.

## Definition of done

Delivered in this branch; `docs/logs.md` is the resulting contract.

- [x] paginated/filterable event-log query against DuckDB
- [x] `/api/logs` local API
- [x] correlated selector semantics reused
- [x] exact session/turn/record lookup
- [x] stable deterministic ordering/pagination
- [x] normalized token/tool/evidence fields exposed
- [x] query-time estimated API cost exposed where available
- [x] no content/secrets/network interception introduced
- [x] optional compact CLI logs query uses same backend
- [x] regression tests cover paging/filter/time/security semantics
- [x] documentation updated
- [x] `go test ./...` passes
