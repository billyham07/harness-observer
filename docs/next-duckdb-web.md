# DuckDB + local web dashboard direction

This document defines the next implementation slice after the collector/storage/query foundation.

## Product direction changes

1. Replace SQLite as the primary store/query engine with DuckDB.
2. Stop treating Grafana/Prometheus as the primary dashboard path.
3. Add a built-in local web dashboard started by `harness-observer web`.
4. Bind to loopback by default (`127.0.0.1`).
5. The web process owns background incremental ingestion and automatic dashboard refresh.
6. Filtering is multi-dimensional and multi-select, with arbitrary custom time ranges.

## Canonical filter dimensions

The persisted/query model remains flat and composable:

- time range
- harness
- client
- provider
- model
- reasoning_effort / thinking_level
- project
- event_kind
- usage_basis

General query semantics:

- multiple selected values inside one dimension are OR
- dimensions combine with AND
- empty dimension means unrestricted

However, **the primary UI selector for harness/model/effort must be hierarchical, not three unrelated dropdowns**.

Required representation:

```text
codex-desktop
├─ gpt-5.6-sol
│  ├─ high
│  └─ xhigh
└─ gpt-5.6-luna
   └─ max
```

This hierarchy represents actual observed tuples:

```text
(harness, model, reasoning_effort)
```

The UI must never manufacture invalid Cartesian-product combinations such as `gpt-5.6-luna / xhigh` when that tuple does not exist in the filtered evidence.

Selection requirements:

- harness nodes are multi-select
- model nodes under each harness are multi-select
- effort nodes under each model are multi-select
- selecting a parent may select all currently visible children, but child selections must remain independently editable
- partial parent selection should be represented explicitly (tri-state or equivalent)
- counts should be shown per node where practical
- hierarchy is recalculated against the active time range and all other filters
- valid selections survive refresh
- selections that become impossible after another filter changes must be visibly removed/marked/cleared by one deterministic rule; never silently retained

Minimum selection that must be expressible exactly:

```text
codex-desktop
└─ gpt-5.6-sol
   └─ high
```

And multi-selection must support cases such as:

```text
codex-desktop
├─ gpt-5.6-sol
│  ├─ high
│  └─ xhigh
└─ gpt-5.6-luna
   └─ max
```

Other dimensions such as provider/project/event_kind/usage_basis may use ordinary faceted multi-select controls because they do not have the same parent-child dependency.

## Reasoning effort evidence

`reasoning_effort` is a nullable canonical dimension. Never infer it from reasoning-token volume.

- Codex: use persisted turn/session context effort when present.
- Pi/OMP: use `thinking_level_change.thinkingLevel` and carry it forward to subsequent usage events until changed.
- Claude Desktop: only populate when a persisted local artifact explicitly exposes an equivalent setting; otherwise leave NULL/unknown.
- Future adapters must document the exact persisted evidence used for this field.

Potential values must stay source-compatible and not be hard-coded to one provider. Typical values may include `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, but unknown/new strings should remain representable.

## DuckDB direction

DuckDB becomes the primary persistent database. Do not create a SQLite -> DuckDB dual-write/sync architecture.

Accepted tradeoff: DuckDB's official Go client uses CGO, so Windows builds may require an appropriate C/C++ toolchain. Update CI/build documentation accordingly.

Migration requirements:

- preserve current normalized `agent_turns` semantics
- preserve idempotent ingestion/checkpoints
- preserve arbitrary time range queries
- migrate/rebuild safely from existing SQLite installs where practical
- do not silently delete an existing SQLite database
- favor rebuilding normalized data from source session evidence when that is safer than complex row-level migration
- keep database schema versioned

## Local web dashboard

Command:

```text
harness-observer web
```

Default behavior:

```text
listen: 127.0.0.1:<documented default port>
open browser: yes unless --no-open
background incremental ingest: enabled
auto refresh: enabled
```

Prefer an embedded static frontend (`go:embed`) with minimal HTML/CSS/JavaScript. Do not introduce React/Vue/Vite/npm unless a concrete requirement cannot be met cleanly without them.

Dashboard must support:

- arbitrary `from` / `to` date-time selection
- quick presets may exist, but must never replace custom time selection
- hierarchical multi-select selector for `harness -> model -> reasoning_effort`
- faceted multi-select filters for remaining canonical dimensions
- stable clearing/reset behavior
- selected filter state encoded in URL query parameters when practical, so views are reproducible/shareable locally
- loading/error/empty states that do not corrupt previous selections

Initial views:

- total observed tokens
- fresh input / cache read / cache write / output / reasoning
- cache hit ratio
- sessions
- turns
- tool calls / tool errors where evidenced
- token usage over time
- usage by model x harness x reasoning effort
- usage by harness
- usage by model
- usage by project
- evidence-basis breakdown

The dashboard is for investigation/comparison, not decoration. Tables are acceptable where charts hide important dimensions.

## Automatic ingestion + refresh

The `web` process should coordinate freshness end-to-end.

Required behavior:

- periodically run incremental ingestion in-process
- do not require cron / Task Scheduler for normal desktop use
- ingestion interval configurable
- dashboard data refresh interval configurable separately if useful
- manual `Refresh now` action triggers ingestion followed by query refresh
- auto-refresh can be enabled/disabled in the UI
- show last successful ingest time and last ingest result/error
- never run overlapping ingestion jobs
- a slow ingest must not freeze HTTP serving
- dashboard refresh should not reset active filters/time range
- avoid unconditional full-page reloads; refresh data/state only

A simple polling design is acceptable. SSE/WebSocket is optional and should only be added if it materially simplifies correctness/UX.

## Query/API changes

The query layer must remain reusable by CLI and web.

For the primary selector, add a hierarchy/facet operation returning **observed tuples**, not independent global lists. A response shape may be equivalent to:

```json
{
  "harnesses": [
    {
      "value": "codex-desktop",
      "count": 240,
      "models": [
        {
          "value": "gpt-5.6-sol",
          "count": 180,
          "efforts": [
            {"value": "high", "count": 110},
            {"value": "xhigh", "count": 70}
          ]
        },
        {
          "value": "gpt-5.6-luna",
          "count": 60,
          "efforts": [
            {"value": "max", "count": 60}
          ]
        }
      ]
    }
  ]
}
```

This hierarchy must be computed under the current time range plus all other active filters.

For remaining dimensions, ordinary facet/filter-options operations may return distinct values/counts under current cross-filter context.

Do not fetch unfiltered global option lists once and keep them forever; that reproduces stale dropdown-selection bugs.

The backend query representation may remain flat (`harness[]`, `model[]`, `reasoning_effort[]`) only if it can exactly represent the selected hierarchy without losing tuple correlation. If flat independent arrays would broaden a selection incorrectly, introduce an explicit tuple filter such as repeated `(harness, model, effort)` selections instead of relying on Cartesian-product semantics.

Example: selecting `sol/high` plus `luna/max` must mean exactly:

```text
(codex-desktop AND gpt-5.6-sol AND high)
OR
(codex-desktop AND gpt-5.6-luna AND max)
```

It must **not** degrade into:

```text
harness = codex-desktop
AND model IN (gpt-5.6-sol, gpt-5.6-luna)
AND effort IN (high, max)
```

because that incorrectly also permits `sol/max` and `luna/high` if such rows appear.

## Optional future dimensions

Design schema/query code so these can be added without another major rewrite, but do not fabricate them in this PR:

- `agent_role` / subagent role
- `mode` / collaboration mode
- `service_tier`

Only populate them from explicit persisted evidence.

## Removed/de-emphasized direction

- Grafana is no longer the primary frontend.
- Prometheus is not required for the local desktop workflow.
- Existing `/metrics` support may remain if cheap to maintain, but do not let Prometheus/Grafana requirements shape the storage/query/UI design.
- Remove or clearly deprecate stale docs saying "Grafana first" or "no custom frontend planned".

## Agent execution contract

This is an implementation task, not a request for another design/PoC/estimate.

- Inspect current code before changing architecture.
- Implement production-shaped code and tests.
- Do not replace the web UI with screenshots/mock JSON.
- Do not stop after scaffolding a frontend with no real query wiring.
- Do not hard-code a fixed time-range list.
- Do not implement harness/model/effort as three independent global dropdowns.
- Do not flatten selected hierarchy into independent arrays if that changes tuple semantics.
- Do not infer reasoning effort from token counts/model names.
- Do not introduce SQLite/DuckDB synchronization unless a real blocking constraint is demonstrated.
- Keep ingestion read-only toward agent session files.
- Keep raw prompts/responses/tool outputs out of the database by default.
- Preserve existing token accounting semantics and evidence basis.
- Keep `go test ./...` green and add focused tests for new behavior.

## Definition of done

- [ ] DuckDB is the primary persistent/query store
- [ ] Windows/Linux CI/build path is documented and working with CGO requirements
- [ ] existing ingestion/dedup/checkpoint invariants still hold
- [ ] `reasoning_effort` is persisted and queryable where source evidence exists
- [ ] Codex effort attribution has regression tests
- [ ] Pi/OMP `thinking_level_change` attribution has regression tests
- [ ] `harness-observer web` starts a loopback-only embedded dashboard
- [ ] dashboard supports arbitrary custom date/time range
- [ ] primary selector renders `harness -> model -> reasoning_effort` hierarchy
- [ ] hierarchy supports true multi-select with partial parent selection
- [ ] tuple correlation is preserved in backend filtering
- [ ] `sol/high + luna/max` does not accidentally include `sol/max` or `luna/high`
- [ ] remaining canonical filters are faceted multi-select
- [ ] facet/hierarchy options update correctly under cross-filtering
- [ ] stale/invalid selected options are handled explicitly, not silently ignored
- [ ] background incremental ingestion runs without overlapping jobs
- [ ] auto-refresh works without resetting filters
- [ ] manual refresh performs ingest + data refresh
- [ ] last ingest status is visible
- [ ] model x harness x reasoning-effort comparison is directly visible/queryable
- [ ] obsolete Grafana-first documentation is removed/reworded
- [ ] `go test ./...` passes
