# AGENTS.md

## Mission

Build a local, read-only observability layer for coding-agent harnesses. The product is the normalized evidence pipeline, not a decorative dashboard.

## Non-negotiable boundaries

- Never proxy model requests.
- Never read, copy, export, or rewrite OAuth/API credentials.
- Never wrap or modify an agent runtime just to observe it.
- Treat local session files as evidence; collectors are read-only.
- Keep `model`, `provider`, and `harness/client` as separate dimensions.
- Arbitrary `since`/`until` ranges are a data-layer requirement. Do not reduce them to preset UI ranges.
- Preserve source provenance (`source_file`, `source_line`) for every normalized turn.
- Normalize token accounting to fresh input + cache read + cache write; preserve native counters and usage basis when source semantics differ.

## Delivery behavior

- Prefer working code and tests over plans, mock dashboards, or PoCs that narrow the final implementation.
- Test fixtures may be synthetic, but production adapters must be based on verified on-disk formats.
- When a source field is unavailable, keep it unknown/zero and document the evidence gap; do not invent estimates silently.
- Avoid frontend work until collector correctness, deduplication, incremental ingestion, and storage/query semantics are stable.
- Keep dependencies small. New dependencies need a concrete runtime benefit. Store/query uses DuckDB (`go-duckdb`); OpenCode's on-disk session DB is read through `modernc.org/sqlite`. The dashboard is Vue 3 + TypeScript built by Vite (`webui/`); its dependency list is deliberately short and every addition needs a reason in the commit that adds it.

## Initial source priority

1. Codex CLI / Codex Desktop
2. Claude Code / Claude Desktop Agent/Cowork (one adapter, split per turn by `entrypoint`)
3. Oh My Pi / Pi
4. OpenCode
5. Grok Build

## Ingestion invariants

- Session files are opened read-only and never rewritten, moved or deleted.
- Ingestion is incremental: unchanged files are not re-read, appended files resume at a checkpoint offset, replaced/truncated files are detected by a head fingerprint and re-read from zero.
- A half-written trailing record is never committed to the checkpoint offset.
- Persistence is idempotent: a record identity that repeats is stored once, keeping the earliest observed timestamp. Re-parsing the same evidence (equal timestamp) refreshes the normalized attribution columns, so a parser upgrade reaches rows that are already stored; a later replay of the same event does not.
- Before trusting a record identity, check what the harness does on resume/fork. Replaying history into a new file with fresh timestamps is common and is the main way a collector silently doubles its numbers.
- Bump `ingest.ParserVersion` when normalization changes; affected files are then rebuilt automatically.
- A file's turns and its checkpoint commit in one transaction. A checkpoint that outlived a rolled-back insert would skip that evidence forever.
- Changing the store engine does not change any of the above. Normalized data is rebuilt from session evidence rather than migrated row by row, and a previous engine's database file is left untouched, never deleted.

## Dimension invariants

- A dimension is populated from explicit persisted evidence or left NULL. Nothing is inferred from token volume, model names or plausibility, and NULL stays visible in the UI instead of being folded into a neighbouring value.
- Value spaces stay open. `reasoning_effort` is not validated against an enum: real local rollouts already contain `ultra`, which no published list mentions.
- `harness`, `model` and `reasoning_effort` are correlated. They are filtered as tuples, never as three independent `IN` lists, because the flat form silently admits combinations the evidence never contained.
- Filter options are recomputed under the active context on every refresh, with each facet excluding its own selection. A selected value that stops matching is reported as stale, not dropped and not quietly left working.

## Cost invariants

- Source-reported cost and API-equivalent estimated cost are separate concepts. Neither is ever synthesized from the other, added to the other, or substituted for the other inside one comparison.
- An API-equivalent estimate is never called actual spend. Subscription and OAuth usage is not metered, and a dashboard that implies otherwise is wrong even when the arithmetic is right.
- Prices are applied at query time from the active catalog. Public rates get corrected; stamping one onto stored evidence would make a pricing fix a re-ingestion, and would rewrite what a historical row means.
- A model with no published rate is unpriced, not free. Coverage is reported token-weighted, because resolving 99% of model names says nothing when the remaining 1% carries the tokens.
- Model matching is exact lookups of deterministic rewrites only. No prefix or fuzzy matching, and nothing inferred from reasoning effort: a future variant must fail to resolve rather than inherit an older variant's price.
- Inferred cache rates, if enabled at all, are labelled heuristic everywhere they surface. Catalog and inferred coverage are never merged into one number.
- The bundled price snapshot works offline. A refresh downloads a public price list and nothing else, and a failed or malformed one leaves the working catalog untouched.

## Definition of done for a new adapter

- Discovers the correct local roots cross-platform where applicable.
- Parses authoritative session records rather than secondary/audit snapshots.
- Has a stable deduplication key.
- Handles missing/corrupt lines without aborting unrelated sessions.
- Honors arbitrary time windows.
- Emits normalized usage and provenance.
- Includes fixture-based tests for identity, tokens, deduplication, and one malformed/edge case.
- Implements `source.FileSource` (Prepare/Files/ParseFile) so discovery, checkpointing and storage stay outside the adapter.
- Any parser mismatch found against real local sessions becomes a minimized regression fixture before it is called fixed.

## Releases

- A tag is the version. `scripts/package-release.sh <tag>` stamps it into the binary and produces the archive; the workflow runs that same script, so a release is reproducible on a laptop.
- Every build answers `harness-observer version`, not only tagged ones. A stamped tag, a `go install` module version, or `dev+<commit>` — and `.dirty` when the tree was not clean.
- The archive ships the DuckDB client library next to the binary, because the binary links it dynamically and will not start without it.
- Nothing is published that has not been unpacked and run. The release job extracts its own archive and asks the binary which version it is before creating the release.

## Frontend

The dashboard source is a Vue 3 + TypeScript app in `webui/`, built by Vite into `internal/web/assets/dist` and embedded with `go:embed`. Vue Router owns page navigation (`/` and `/logs`), AntV G2 owns chart mechanics, Naive UI owns commodity controls. On Windows, a bare `harness-observer` hosts that same UI in a native WebView2 window (`gpui`); `web` still serves it in a browser. Do not add a second frontend toolkit to replace Vue.

### Runtime boundary

- Node and npm are build-time only. Nothing the end user runs may need them, and nothing may be fetched at runtime: no CDN, no external fonts, no telemetry.
- `internal/web/assets/dist` is generated. It is gitignored, never hand-edited, and rebuilt by `scripts/build-web.*` before `go build` or `go test`.
- A binary without those assets says so on `/`. Do not replace that with a silent 404.

### State

- One owner per concern: URL serialization, the committed range, the correlated selection, facets, refresh, and each page's data. A second writer for any of them is the bug, not the fix.
- Pending picker state is separate from committed query state. Dragging across a calendar must not reach the address bar.
- Every load is version-checked and abortable. A response that arrives after the user moved on is dropped, never painted.
- A refresh replaces data. Filters, range, scroll position and expanded nodes are user state and survive it.
- The correlated `harness → model → effort` selector is ours, not a library tree. A component that cascades selections its own way will quietly admit combinations the evidence never contained.

### Styling

- Component-owned rules live in that component's `<style scoped>`. Only genuinely shared primitives go in `webui/src/styles/base.css`; theme tokens go in `tokens.css` and nowhere else.
- Never use `!important`. If a rule seems to need it, the earlier selector is too broad.
- `prefers-reduced-motion: reduce` repeats the same selectors that set `transition`, with `transition: none`. Do not blanket `*`.
- Colour is reserved for the four token buckets. Chrome stays neutral, and a state that needs marking (a partial estimate, a stale value) is marked typographically before it is marked in colour.
- Bucket hues are defined once in `lib/palette.ts` and mirrored in `tokens.css`; a unit test asserts the two agree. Any change to the set is re-validated for colour-vision separation before it ships.
- Interface strings go through `t()` (`src/i18n`); observed values never do.

### Charts

- One G2 instance per chart component: create on mount, update in place, destroy on unmount. A fresh instance per reactive update leaves an orphaned canvas that looks like a CSS bug for a long time.
- The chart engine is injected, so the lifecycle is testable without a canvas.
- Canonical bucket order and colour assignment do not change when a bucket is empty or the language changes.

### Change discipline

- `npm run typecheck`, `npm test -- --run` and `npm run build` all pass before `go test ./...` is meaningful.
- When fixing a UI regression, reproduce it on the running dashboard, then put the fix on the rule or the owner that caused it.
- Prefer existing composables and components over new abstractions.
- Remove temporary screenshots and local verification artifacts before committing.
