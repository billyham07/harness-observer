# Sidebar + Logs UI implementation contract

## Dependency

This PR is intentionally stacked on `feat/turn-event-logs` and should use the `/api/logs` contract implemented there. Do not duplicate log querying in the browser.

## Goal

Turn the current single-page dashboard into a small two-section local application with a left navigation rail/sidebar:

```text
Harness Observer

▣ Overview
≡ Logs
```

- **Overview** = the current dashboard, functionally unchanged.
- **Logs** = a dense searchable/paginated event-log table built on the backend log API.

The app remains embedded vanilla HTML/CSS/JS (`go:embed`, no npm, bundler or CDN).

## Navigation shell

Add a persistent left sidebar on desktop.

Requirements:

- current page clearly highlighted
- text labels + simple inline/icon glyphs; no external asset dependency
- existing EN/中 and light/dark/auto controls stay accessible
- app title/identity remains visible
- main content gets the remaining viewport width; do not squeeze Overview charts into a narrow fixed canvas
- responsive behavior: collapse to a compact rail/drawer/header navigation on small screens rather than forcing a permanent wide sidebar
- keyboard/focus states are visible

Use URL state so refresh/back/forward/direct links work. Exact route mechanism may be either paths (`/`, `/logs`) or a stable query/hash route, but prefer real paths if the current embedded server can support SPA fallback cleanly.

Do not reset Overview filters merely because the user visits Logs and returns. Preserve current URL-driven Overview state semantics.

## Overview

The current page becomes the **Overview** tab/page.

Do not regress:

- rebuilt time range picker/calendar
- exact custom from/to time fields
- named relative presets
- harness → model → reasoning_effort correlated tri-state selector
- faceted filters and stale-selection behavior
- removable filter chips
- token/cost KPIs
- pricing coverage/provenance
- charts and sortable comparison/project tables
- EN/中
- light/dark/auto
- background/manual refresh behavior

This PR is a shell/navigation refactor, not a redesign of Overview.

## Logs page product pattern

Use `higress-ai-key-admin`'s logs page as the stronger UX reference: filter tools above a dense table, explicit time range, server pagination, useful empty states, and click-row detail. OpenCodex is useful for the idea of a request/event diagnostic row, but its GUI fields are proxy-specific and must not be copied blindly.

### Toolbar

Logs needs:

- time range picker using the same date/time control and `[from,to)` semantics as Overview
- harness/model/effort correlated selector (reuse component/logic rather than fork it)
- client/provider/project/event-kind/basis filters when the backend exposes them
- exact ID search for session/turn/record
- refresh button
- active filters visible/removable

Avoid the old CodeBurn-style scattered dropdown behavior. One filter must never silently invalidate another while still appearing selected; reuse current facet/stale-selection semantics.

### Table

Default newest first. Suggested visible columns:

```text
Time
Harness
Model
Effort
Project
Event
Input
Cache read
Cache write
Output
Tools
Tool errors
Est. API cost
Session
```

Keep the default table useful at ordinary desktop width; put lower-priority identifiers/evidence in the detail drawer rather than creating a permanently 2500px-wide table.

Rules:

- server-side pagination, 20/50/100 rows
- stable sort if backend supports allow-listed sorting
- sticky header when useful
- compact numeric formatting but full value in title/detail
- local timezone clearly indicated for rendered timestamps
- `null`/unknown renders as `—`, never `0`, success, or guessed text
- tool errors should be visually distinguishable without turning the table into a red dashboard
- row is clickable and keyboard reachable

### Detail drawer/panel

Clicking a row opens a side drawer or equivalent detail panel with sections:

```text
Identity
  timestamp / harness / client / provider / model / effort / project
  session_id / turn_id / record_id / event_kind

Usage
  fresh input / cache read / cache write / output / reasoning
  prompt total / observed total
  usage semantics / usage basis

Tools
  tool calls / tool errors

Cost
  source-reported cost (only if persisted)
  Est. API cost
  pricing coverage / catalog match provenance

Evidence
  source file
  source line
```

Do not display prompt, response, reasoning text, tool args/output, auth data, or arbitrary source-file contents.

Long IDs should be copyable, not fully expanded in the table.

### Empty/error/loading states

Explicitly distinguish:

- loading
- no ingested data for selected time range
- no rows matched active filters/search
- backend/query error

Do not leave a blank table with no explanation.

## Shared state/components

The main technical objective besides UI is avoiding duplicated filter/date logic.

Refactor current vanilla JS into small reusable modules/functions if needed, e.g.:

```text
range state/control
hierarchy selector
facet multiselect
filter chips
API fetch helpers
formatters
page router
```

Do not rewrite the frontend in a framework just to get components. Keep changes incremental and production-shaped.

Overview and Logs may have independent active filter state, but each page's state must survive navigation/back-forward. If sharing the time range improves UX, do it explicitly and test it; do not accidentally couple state through global variables.

## Accessibility / interaction correctness

- tab order works through sidebar, filters, table rows and drawer
- Escape closes popovers/drawer where appropriate
- click-outside behavior does not lose unfinished range state unexpectedly
- focus returns sensibly after drawer close
- selected nav item exposes current-page semantics
- buttons have labels/tooltips where icon-only

## Tests

Add browser/UI unit tests only in the style already used by this repo; do not introduce a heavyweight browser framework solely for this PR.

Required regression coverage where practical:

- navigation Overview ↔ Logs
- refresh/direct-link keeps selected page
- Overview URL/filter state survives round-trip through Logs
- Logs query serialization preserves correlated hierarchy tuples
- Logs pagination changes server request, not client slicing
- page-size switch resets/normalizes page correctly
- row click opens matching detail
- unknown/null values render as unknown, not zero
- empty range vs no-match states are different
- current range picker/calendar behavior is unchanged
- theme/language remain functional on both pages
- mobile sidebar/collapse does not cover the page permanently

## Agent execution contract

Implement the UI, not a screenshot/mock.

1. Inspect current embedded web assets and PR #4/#5 behavior before refactoring.
2. Use the backend `/api/logs`; no full table dump or browser-side SQL-like filtering.
3. Reuse filter/time semantics. Do not create a second incompatible hierarchy selector.
4. Do not add npm, CDN or a frontend framework.
5. Preserve offline operation.
6. Do not regress Overview features for the sake of shell cleanup.
7. Do not render sensitive conversation/session contents.
8. Keep CSS/layout restrained and information-dense; this is an observability tool, not a landing page.
9. `gofmt`, `go vet`, and `go test ./...` remain green.

## Definition of done

- [x] persistent responsive left navigation
- [x] Overview is current dashboard with no functional regressions
- [x] Logs is second navigation destination
- [x] Logs uses backend server-side time/filter/pagination
- [x] shared range/hierarchy/filter behavior where appropriate
- [x] dense useful log table
- [x] row detail drawer/panel
- [x] explicit loading/no-data/no-match/error states
- [x] page/navigation state survives refresh/back-forward
- [x] EN/中 and theme controls work on both pages
- [x] mobile/keyboard behavior is usable
- [x] no sensitive content added
- [x] tests and docs updated
- [x] `go test ./...` passes
