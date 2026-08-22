# Frontend rearchitecture: Vue 3 + Vite + AntV G2

## Status: implemented

The migration in this document has landed. `webui/` is the production frontend,
`internal/web/assets/dist` is what the binary embeds, and the previous
`app.js`/`app.css`/`i18n.js` page has been removed rather than kept behind a
flag. The rest of this document is retained as the contract it was written as;
what follows is where the implementation deliberately differs from it.

**Naive UI, not Ant Design Vue.** The document allows another well-supported
Vue 3 library if the choice is documented. Naive UI was chosen because its
theming is a plain object of tokens, which lets the app feed it values read back
out of its own stylesheet at runtime. The component library therefore supplies
interaction correctness — a date/time range panel with seconds, popovers, a
drawer, a remote-paginated table — without supplying a visual identity that
would have to be fought back.

**The two composition views are one panel.** Parity asked for token composition
and estimated cost composition. Both are present, in a single *Anatomy of a
turn* panel: the same four buckets on two bars of identical geometry, joined
bucket-to-bucket by ribbons. The comparison the two separate strips existed to
support — a bucket that is nearly all of the tokens and a fraction of the cost,
or the reverse — is the shape of that panel rather than something the reader
has to do in their head.

**Two headline readouts, not ten equal tiles.** Observed tokens and the API-
equivalent estimate lead; the remaining eight figures are a quiet strip. Every
KPI from the previous dashboard is still on screen.

**A partial estimate is footnoted, not coloured.** Colour is reserved for the
four token buckets, so a coverage figure below the floor is marked with an
asterisk and a dotted rule instead of a warning hue — which, in the previous
palette, was also the output bucket's hue.

**`dist` is generated, not committed.** CI builds the frontend before the Go
steps, `scripts/build-web.*` does it locally, and a binary compiled without it
serves a page naming the missing step instead of a stack of 404s.

**Frontend invariants moved to frontend tests.** The Go suite used to assert on
`app.js` source text. Those assertions are now behavioural tests in
`webui/src/**/*.spec.ts`; the Go side asserts what the Go side owns — that the
shell is served on both routes, that it loads nothing remote, and that the
bundle talks to the server-paginated endpoints.

**Bundle size.** One script and one stylesheet, about 2.0 MB minified (590 kB
gzipped), dominated by G2 and Naive UI. G2 is assembled from `corelib` rather
than the full standard library for that reason. This is embedded in a binary
that is already tens of megabytes of DuckDB and is served over loopback, but it
is a real increase over the previous hand-written page and should not grow
without a reason.

## Goal

Replace the current monolithic embedded vanilla frontend with a maintainable Vue 3 + Vite application while preserving the existing local/offline single-binary product shape.

This is a frontend infrastructure migration first. It is **not** permission to redesign product semantics, rewrite the Go backend, or change API/filter behavior.

Current `main` already behaves like a small SPA:

- Overview + Logs routes
- URL-backed query state
- arbitrary/custom time range and calendar
- correlated harness -> model -> reasoning_effort selector
- faceted filters and stale selections
- removable chips
- background/manual refresh
- pricing provenance and cost metrics
- multiple aggregate tables
- event logs table + pagination + drawer
- theme and i18n
- custom SVG chart rendering

The imperative frontend has crossed the point where continuing to add handwritten DOM/state logic is the simpler option.

## Target stack

Required:

```text
Vue 3
Vite
TypeScript
Vue Router
AntV G2
```

Recommended but not mandatory if the agent finds a better, well-supported Vue 3 alternative:

```text
mature Vue UI component library
```

Ant Design Vue is a reasonable default because this app needs mature implementations of date/range pickers, dropdown/select/tree/table/pagination/drawer/tooltip/empty states. If another Vue 3 library is chosen, document why and keep the visual result restrained and observability-oriented.

Do not introduce Pinia by default. Start with composables/reactive state; add a store only if cross-page state becomes materially cleaner with it.

## Product/runtime boundary

Node/npm are **build-time only**.

The end-user runtime remains:

```text
harness-observer(.exe)
    -> embedded static assets
    -> 127.0.0.1:9464
    -> no Node runtime
    -> no CDN
    -> fully offline UI
```

Vite output must be embedded by Go (`go:embed`) just like the current assets.

No runtime dependency on npm, a dev server, external fonts, external JS, analytics, telemetry, or third-party CDNs.

## Suggested repository layout

Exact names may vary, but keep source and generated assets clearly separated:

```text
webui/
  package.json
  package-lock.json (or another single committed lockfile)
  tsconfig.json
  vite.config.ts
  src/
    main.ts
    App.vue
    router/
    pages/
      OverviewPage.vue
      LogsPage.vue
    components/
      AppShell.vue
      TimeRangePicker.vue
      HarnessSelector.vue
      FacetFilter.vue
      FilterChips.vue
      LogDetailDrawer.vue
    charts/
      TokenTimeline.vue
      TokenComposition.vue
      CostComposition.vue
    composables/
      useQueryState.ts
      useTimeRange.ts
      useHierarchy.ts
      useFacets.ts
      useRefresh.ts
      useLogs.ts
    api/
      observer.ts
      types.ts
    i18n/
      ...

internal/web/assets/
  dist/  # generated Vite output embedded by Go
```

Do not hand-edit generated `dist` assets.

## Build integration

Add explicit reproducible commands, for example:

```text
npm ci
npm run build
```

and a repo-level helper such as:

```text
scripts/build-web.ps1
scripts/build-web.sh
```

The Go build should have a clear failure mode when required generated assets are absent. CI must build the frontend before Go packaging/tests that require embedded assets.

Avoid magical postinstall steps.

Keep the lockfile committed and dependency versions intentional.

## Backend/API invariants

Do not redesign existing Go APIs merely to simplify Vue code.

Preserve the current semantics of at least:

```text
/api/query
/api/hierarchy
/api/facets
/api/logs
/api/status
/api/refresh
/api/pricing
/api/pricing/unpriced
```

Keep:

- exact `[from,to)` backend semantics
- correlated harness/model/effort tuple selection
- stale-selection behavior
- query-time pricing estimates
- source-reported cost vs Est. API cost separation
- Logs server-side pagination/filtering
- capability-aware/null log fields
- local-only defaults

Create typed frontend API DTOs that reflect backend JSON rather than weakening backend types into generic `any` objects.

## URL/state contract

Existing bookmarked/shared dashboard links are user-facing behavior.

Preserve current URL semantics for:

```text
/
/logs
from
to
preset
bucket
sel (repeatable correlated tuples)
facets
refresh options where currently persisted
logs page/page_size/session search
```

If URL keys are changed, support backward-compatible parsing for old links and document the migration.

Browser refresh, back/forward, and direct links to `/logs` must work through the embedded Go server.

Use Vue Router for page navigation; do not reimplement route state manually.

## Feature parity: Overview

Before adding new visualization features, the Vue version must reproduce current Overview behavior:

- responsive left navigation
- Overview route
- time range presets
- custom arbitrary date + exact time input
- two-date range selection
- month navigation
- future date constraints
- correlated harness -> model -> effort selector
- search inside hierarchy
- tri-state parent/child selection behavior
- stale selections visible and removable
- all existing facets
- filter chips
- reset
- manual Scan now
- background query refresh settings/status
- token KPIs
- Est. API cost + pricing coverage/provenance
- token composition
- cost composition
- tokens-over-time visualization
- Harness x Model x Effort table
- By Harness / Model / Project / Evidence tables
- Last scan diagnostics
- theme
- English/Chinese UI

Do not use migration as an excuse to drop the less convenient filters or exact custom time input.

## Feature parity: Logs

Preserve:

- `/logs` direct route
- same/shared time and hierarchy semantics
- facets that Logs currently supports
- exact session search
- server-side page sizes 20/50/100
- deterministic newest-first backend pagination
- unknown values rendered as unknown, not zero
- row click/detail drawer
- evidence location metadata
- source-reported vs estimated cost distinction
- capability-aware optional fields
- explicit loading/no-data/no-match/error states

Do not load all log rows and paginate/filter client-side.

## Range picker

This is specifically an area where the old implementation accumulated event-lifecycle bugs. Do not port the old imperative implementation line-for-line.

Prefer a mature range/date picker component, but retain product semantics:

- named relative presets
- arbitrary start/end dates
- exact time editing to seconds
- local-time display/input
- custom range committed to the same canonical query state
- reverse-order and same-day selection
- selected range survives navigation and refresh
- correct URL serialization

If the UI library's range picker cannot express exact second-level times cleanly, compose date-range + time inputs rather than deleting precision.

## AntV G2 migration

Replace the manually constructed SVG timeline and related low-level chart primitives with AntV G2.

The frontend should no longer own generic chart mechanics such as:

```text
nice tick calculation
SVG axis construction
manual bar geometry
viewBox scaling
manual stacked rectangle layout
custom primitive tooltip positioning
```

### Initial charts

For this PR, feature parity is enough:

1. Tokens over time: stacked canonical token buckets
2. Token composition
3. Estimated cost composition

Composition views may remain compact bars if that is clearer; the key requirement is to remove bespoke low-level visualization plumbing where G2 provides the primitive.

AntV lifecycle must be Vue-safe:

- create chart on mount
- update from reactive data intentionally
- destroy chart on unmount
- respond to container resize
- theme switch without leaking chart instances
- i18n label switch without duplicate canvas/SVG nodes

Do not instantiate a new chart on every incidental reactive update without destroying/reusing it.

## UI component policy

The migration should intentionally stop reimplementing commodity UI controls.

Use mature components where appropriate for:

```text
Date/Range picker
Time picker
Popover/dropdown
Select/multi-select
Tree
Table
Pagination
Drawer
Tooltip
Empty/loading/error states
```

But do **not** let a component library flatten the correlated hierarchy semantics into three independent dropdowns.

The harness/model/effort selector is domain-specific and must retain exact correlated tuple behavior, even if built from a library Tree/TreeSelect primitive.

## Styling

Do not turn this into a generic Ant Design admin template or a flashy dashboard.

Retain the current product personality:

- dense
- restrained
- observability-first
- readable numeric tables
- clear unknown/evidence states
- dark/light support
- good desktop use of width
- usable mobile layout

A component library should provide interaction correctness, not dictate the entire visual identity.

Avoid decorative gradients, oversized cards, marketing-style whitespace, or motion that reduces information density.

## State architecture

Eliminate the single global imperative `state` object plus scattered companion globals as the primary architecture.

Recommended composables:

```text
useRouteQueryState
useTimeRange
useCorrelatedHierarchy
useFacets
useRefresh
useOverviewData
useLogs
usePricing
```

Rules:

- API data state and UI transient state are separate
- committed range and pending picker state are separate
- URL serialization has one owner
- correlated selection encoding/decoding has one owner
- page-specific Logs state does not accidentally mutate Overview aggregation state
- refresh replaces data, not user filter state
- concurrent loads use cancellation/versioning so stale responses cannot overwrite newer state

## TypeScript

Use TypeScript for the new frontend.

Define explicit types for:

- query filters
- hierarchy nodes/tuple selections
- aggregate rows
- cost/pricing metadata
- logs rows/page response
- ingest/status response

Avoid pervasive `any` and unchecked property bags that simply recreate current runtime ambiguity in TS syntax.

## Testing

This migration must add a practical frontend test layer.

Recommended baseline:

```text
Vitest
Vue Test Utils
jsdom/happy-dom
```

Use a browser/E2E runner only if justified; do not introduce a huge E2E stack merely to satisfy a checkbox.

Required regression coverage:

### State/URL

- old/current URL query parses correctly
- correlated tuples remain correlated
- repeated facets serialize/deserialize
- Overview <-> Logs navigation preserves intended state
- direct `/logs` route works
- relative preset re-evaluates on load
- exact custom range round-trips

### Range

- first/second range selection behavior
- same-day and reverse-order ranges
- exact time changes
- range does not reset on unrelated reactive refresh

### Hierarchy/facets

- tri-state parent selection
- leaf selection compression
- stale selections remain visible
- one facet recomputation does not silently clear another selected value

### Logs

- pagination requests correct backend page
- page size updates backend request
- null optional fields render as unknown
- drawer displays matching row
- no client-side full-data pagination

### Charts

- G2 instance lifecycle mount/update/unmount
- no duplicate chart DOM after theme/i18n/data refresh
- canonical bucket ordering remains stable

## CI

Update CI to include frontend validation before the Go test/build path, at minimum:

```text
npm ci
npm run typecheck
npm test -- --run   # or equivalent
npm run build
gofmt / go vet / go test ./...
```

Cache npm downloads if desired, but correctness must not depend on cache.

Windows build/release flow must still produce the same local executable experience; Node is not required on the end user's machine.

## Migration strategy

This should be a coherent replacement, not months of maintaining two frontends.

Suggested implementation order inside the PR:

1. scaffold Vue/Vite/TS build + Go embedding
2. typed API layer and router
3. app shell/theme/i18n
4. shared URL/query/range state
5. Overview filters and tables
6. AntV charts
7. Logs table/drawer
8. parity/regression tests
9. remove old imperative frontend source once parity is verified

Do not leave both UIs indefinitely behind flags unless a very short-lived comparison mode is necessary during implementation.

## Non-goals

- no backend storage/query rewrite
- no new analytics metrics solely because AntV makes charts easy
- no provider/harness parser changes
- no pricing semantic changes
- no Electron/Tauri desktop shell
- no separate Node server
- no CDN assets
- no user tracking/telemetry

## Definition of done

- [ ] Vue 3 + TypeScript + Vite frontend is the production embedded UI
- [ ] AntV G2 replaces bespoke timeline SVG mechanics
- [ ] mature UI primitives replace fragile handwritten commodity controls where useful
- [ ] Overview feature parity
- [ ] Logs feature parity
- [ ] existing URL/bookmark semantics preserved or backward-compatible
- [ ] exact custom date/time selection works
- [ ] correlated hierarchy semantics unchanged
- [ ] facets/stale selections unchanged
- [ ] pricing semantics/provenance unchanged
- [ ] Logs remain server-side paginated and capability-aware
- [ ] theme + EN/Chinese parity
- [ ] responsive desktop/mobile behavior
- [ ] frontend typecheck/tests/build in CI
- [ ] generated Vite assets embedded into the Go binary
- [ ] end-user runtime requires no Node/npm/network/CDN
- [ ] old monolithic imperative frontend removed after parity
- [ ] `go vet` and `go test ./...` remain green

## Agent execution contract

Implement the migration end-to-end. Do not stop at scaffolding or a visual mock.

Treat current `main` as the functional specification. Compare both Overview and Logs before removing the old frontend.

The objective is to remove the current frontend architecture bottleneck while preserving the evidence/query semantics that make the dashboard trustworthy.
