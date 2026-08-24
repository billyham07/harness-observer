# Local dashboard

```bash
harness-observer gpui     # native window (Windows default)
harness-observer web      # same UI in a browser
```

Both commands serve the embedded dashboard on `127.0.0.1:9464`. `gpui` hosts it
in a native window (WebView2 on Windows; elsewhere it falls back to a browser).
`web` always opens a browser. The same process runs incremental ingestion in
the background, so a desktop user needs neither cron nor Task Scheduler to keep
the numbers current. A left rail switches between **Overview** (`/`) and
**Logs** (`/logs`); Logs is a server-paginated event table over
[`/api/logs`](logs.md), not a second copy of the filters in the browser.

On Windows, a bare `harness-observer` with no arguments is `gpui`. Leading
flags still belong to `collect`, so `harness-observer --since ...` keeps
working. Double-clicking the `.exe` from Explorer hides the console window; a
terminal session keeps it so the URL is visible.

```text
--addr              listen address (loopback default; this is data about your machine)
--db                database path (default: per-user data dir, or $HARNESS_OBSERVER_DB)
--sources           codex,claude,omp,pi,opencode,grok
--ingest-interval   background scan interval, 0 disables it (manual refresh still works)
--no-open           web only: do not launch a browser
--debug             program diagnostics on stderr; gpui also enables WebView2 DevTools
--pricing-refresh   allow the best-effort public price-list download (default true)
--pricing-overrides explicit per-model rates, a JSON file
--pricing-heuristic-cache  infer missing cache rates from the input rate (default off)
```

## Frontend

The source is a Vue 3 + TypeScript application in [`webui/`](../webui), built by
Vite and compiled into the binary with `go:embed`:

```text
webui/                          Vue 3 · TypeScript · Vite · Vue Router · AntV G2 · Naive UI
  └─ npm run build ─▶ internal/web/assets/dist ─▶ go:embed ─▶ harness-observer(.exe)
```

Node and npm are **build-time only**. What ships is one script and one
stylesheet inside the binary: no Node runtime, no dev server, no runtime asset
directory and no CDN, because the dashboard has to work on a machine with no
network. Build it with [`scripts/build-web.sh`](../scripts/build-web.sh) (or
`scripts\build-web.ps1`); a binary compiled without it serves a page that says
so instead of a stack of 404s.

Charts are [AntV G2](https://g2.antv.antgroup.com/): the frontend no longer owns
tick calculation, axis construction, bar geometry or stack layout. Commodity
controls — the date/time range panel, popovers, the drawer, the paginated Logs
table — come from Naive UI, chosen over Ant Design Vue because its theme
overrides are read back out of our own stylesheet at runtime, so the component
library supplies interaction correctness without supplying a visual identity.

What is *not* delegated is the `harness → model → reasoning_effort` selector.
Its correlated tuple semantics are the product, so it is built from checkbox and
popover primitives rather than from a tree component that would want to cascade
selections its own way.

State lives in typed composables with single ownership — one owner for URL
serialization, one for the committed range, one for the correlated selection,
one for facets, one per page's data. Pending picker state is separate from
committed query state, and every load is both version-checked and abortable, so
a response that was already in flight when the user changed something cannot
overwrite the newer state on arrival.

`npm run dev` serves the same app against a running `harness-observer web` on
`127.0.0.1:9464`, so the Vue side can be iterated on without rebuilding Go.

## Filter surface

Every filter is a dropdown in one row under the header, and whatever is
currently narrowing the numbers is repeated as a removable chip. Time range,
grouping interval, all filters and the refresh settings live in the URL, so a
view can be bookmarked or pasted to explain a number.

| Control | What it does |
| --- | --- |
| Range | relative presets (Today, Yesterday, last 24h/7d/30d/90d, this week, this month, last month, all time), a two-month range calendar with date + time fields for anything else, and the time bucket. Bucket `Auto` picks hour/day/month from the span. A preset is stored in the URL by name and re-evaluated on load, so a shared "Today" link means today; a hand-picked range is stored literally |
| Harness → model → effort | the correlated selector below, with a search box for large trees |
| Client, Provider, Project, Event kind, Evidence basis, Usage semantics | flat multi-selects, cross-filtered by everything except themselves |
| Scan now | re-reads session files, then re-runs the query. The pill next to it reports the last scan |
| Auto / Light / Dark | theme; `Auto` follows the OS and re-resolves when it changes |
| EN / 中 | interface language, English or Chinese. It defaults to the browser's language and is remembered per browser |

Only interface chrome is translated. Observed values — harness, model,
reasoning effort, project, client — are evidence and are shown exactly as the
session files recorded them. Switching language re-renders from the last
response instead of re-querying, so the numbers on screen cannot move when the
language does. Strings live in [`webui/src/i18n`](../webui/src/i18n);
a missing key falls back to English rather than showing the key.

## The primary selector is a hierarchy, not three dropdowns

`harness`, `model` and `reasoning_effort` are correlated. The selector shows the
tuples that actually exist:

```text
codex-desktop
├─ gpt-5.6-sol
│  ├─ high
│  ├─ medium
│  └─ ultra
└─ gpt-5.6-luna
   └─ max
```

Selecting `gpt-5.6-sol / medium` plus `gpt-5.5 / xhigh` means exactly those two
tuples:

```text
(harness='codex-desktop' AND model='gpt-5.6-sol' AND effort='medium')
OR (harness='codex-desktop' AND model='gpt-5.5'     AND effort='xhigh')
```

Three independent lists would send `model IN (...) AND effort IN (...)`, which
on the local corpus also pulls in `gpt-5.5/medium` and `gpt-5.6-sol/xhigh` — an
extra 485M observed tokens attributed to a comparison nobody asked for. The
same selection is expressible from the CLI:

```bash
harness-observer query --select "codex-desktop|gpt-5.6-sol|medium" --select "codex-desktop|gpt-5.5|xhigh" --group model,effort
```

Wire form is `harness[|model[|effort]]`. A missing level means "everything below
this node"; a present but empty level means the rows where the source recorded
nothing (`omp|deepseek-v4-flash|` selects the calls with no effort recorded).
`\|` escapes a literal pipe.

Selecting a parent selects everything under it as one compact entry. Unchecking
one child expands that entry into its explicit siblings, so children stay
independently editable.

## Cross-filtering and stale selections

The hierarchy and every facet are recomputed against the active time range and
all other filters on each refresh. A facet is computed with **its own** selection
removed, otherwise picking one client would hide every other client and a second
one could never be added.

A selection that no longer matches anything is not silently dropped and not
silently kept working. It stays in the URL, is listed as stale in the selector,
contributes nothing to the aggregates, and there is one button to clear it. The
numbers on screen always correspond to what the selector shows.

## Cost is shown as an estimate, and says so

One of the two headline readouts carries the API-equivalent cost estimate with
its pricing coverage underneath it,
and every comparison table gains sortable `est. API $`, `$/session`, `$/turn`,
`$/1M observed` and `coverage` columns, separated from the token columns by a
rule. Cost tiles and cells are styled as a different kind of number on purpose:
the tokens are evidence, the dollars are derived from somebody else's price
list.

The rules that keep the number honest:

- it is labelled **Est. API cost** / API-equivalent, never Actual Spend
- a row whose model has no published rate shows `–`, never `$0.00`
- a total below 99.5% token-weighted coverage is marked partial rather than
  presented as an authoritative figure
- a provenance line under the composition panel names the catalog, its version,
  its staleness, any local overrides or inferred cache rates, and how many
  observed tokens are excluded from the estimate
- source-reported cost stays a separate field and is never substituted for the
  estimate on the harnesses that happen to record one

`/api/pricing` is fetched alongside the query it qualifies, so the numbers and
the statement of where they came from can never be one refresh apart. Full
semantics in [`pricing.md`](pricing.md).

## Freshness

- background incremental ingest on a timer (`--ingest-interval`)
- `Refresh now` = ingest, then reload the data
- auto refresh toggle + interval for the data reload alone
- last ingest time, duration, new-row count and any error are always visible
- ingestion runs on a single goroutine, so runs can never overlap; a burst of
  refresh requests collapses into one follow-up run
- HTTP handlers never ingest inline, so a slow scan cannot block a page load
- a refresh replaces data only: filters, time range, scroll position and which
  nodes are expanded all survive, and a response that arrives after the user has
  moved on is dropped rather than painted

## One writer per database

DuckDB locks the database file for the whole process, so `web`/`gpui` and a
separate `ingest` cannot share a store: the second process fails to open it.
The dashboard process ingests on its own timer, so a second scheduled job is
redundant anyway. See [`build.md`](build.md).

## Reproducible views

Filter state lives in the URL query string (`from`, `to`, `bucket`, repeated
`sel`, and repeated `client`/`provider`/`project`/`event_kind`/`basis`/
`semantics`), so a view can be bookmarked or pasted to yourself. The page
itself is the path: `/` is Overview and `/logs` is Logs. Switching pages keeps
the shared filters; Logs adds `page`, `page_size` and `session` when set.


## API

The dashboard is a client of the same endpoints the CLI uses:

| Endpoint | Purpose |
| --- | --- |
| `/api/query` | aggregation over any time range, grouping and bucketing |
| `/api/logs` | one page of individual normalized events; see [`logs.md`](logs.md) |
| `/api/hierarchy` | observed `(harness, model, reasoning_effort)` tuples with counts and tri-state |
| `/api/facets` | flat dimensions, cross-filtered, with stale values marked |
| `/api/status` | ingest state: running, last result, last error, interval |
| `/api/refresh` | `POST` — queue an ingestion run, returns immediately |
| `/api/pricing` | pricing catalog source, version, freshness, overrides, heuristics |
| `/api/pricing/unpriced` | models with no published rate, ranked by observed tokens |
| `/api/pricing/refresh` | `POST` — download the public price list now, ignoring the TTL |
| `/api/health` | schema version and database path |
| `/metrics` | Prometheus text exposition (optional, see [grafana.md](grafana.md)) |
