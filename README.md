# Harness Observer

**Local, read-only observability for coding-agent harnesses.**

Harness Observer reads the session evidence that coding agents already persist locally and turns it into one comparable view across **harness × model × reasoning effort × project × time**.

It is built for a question that ordinary provider dashboards do not answer well:

> How does the same model actually behave when used through different coding-agent harnesses?

Harness Observer is not an API gateway and does not sit in the model request path. It does **not** proxy model traffic, extract OAuth/API credentials, modify agent configuration, wrap the agent runtime, or persist prompt/response/tool-output content.

## What it shows

- fresh input, cache read, cache write, output and reasoning tokens
- cache behavior and prompt composition
- sessions, turns and structured tool activity where the source records it
- model × harness × reasoning-effort comparisons
- project/client/provider breakdowns
- individual normalized turn/event logs
- source-reported cost where available
- **API-equivalent estimated cost** from published model rates, with visible pricing coverage and provenance
- exact arbitrary time windows, not only fixed 24h/7d presets

The built-in dashboard is served locally and embedded in the binary. The frontend is Vue 3 + TypeScript with AntV G2 charts; Node is a build-time dependency only.

## Supported harnesses

| Harness | Local evidence | Notes |
| --- | --- | --- |
| Codex CLI / Desktop | `~/.codex/sessions`, archived sessions | preserves originator/client, model and persisted effort |
| Claude Code / Desktop | `~/.claude/projects/**/*.jsonl` plus Desktop roots | turn-level frontend attribution; shared transcripts are not double counted |
| Oh My Pi | `~/.omp/agent/sessions/**/*.jsonl` | usage, thinking level, tool/compaction events where persisted |
| Pi | `~/.pi/agent/sessions/**/*.jsonl` | same normalized Pi-family evidence model |
| OpenCode | `$OPENCODE_DATA_DIR` or platform data dir | JSON storage and read-only SQLite backends |
| Grok Build | `$GROK_HOME/sessions` or `~/.grok/sessions` | authoritative `turn_completed.usage`; legacy estimates stay clearly separated |

Adapters are evidence-driven: a field that a harness does not persist remains unknown instead of being guessed.

## Privacy boundary

Harness Observer is intentionally passive.

**It reads:**

- timestamps and stable session/turn identities
- model/provider/client/project metadata when persisted
- token/cache/cost counters
- structured tool counts/errors where the source exposes them
- local evidence location needed for checkpointing/debugging

**It does not persist:**

- prompts or model responses
- reasoning text
- tool arguments or command output
- raw session JSON
- OAuth tokens, API keys or credentials

The dashboard/API bind to `127.0.0.1` by default. No model traffic or usage data is uploaded anywhere. Pricing works from an embedded public catalog snapshot; an optional best-effort refresh downloads only public pricing metadata and falls back to the bundled snapshot on failure.

## Quick start

Download a prebuilt archive from the [GitHub Releases](https://github.com/billyham07/harness-observer/releases) page, extract it, and keep the DuckDB client library beside the executable.

### Windows

```powershell
.\harness-observer.exe
```

A bare invocation opens the dashboard in a native WebView2 window (`gpui`). Use `web` to open it in a browser instead.

### Linux

```bash
./harness-observer web
```

Then open:

```text
http://127.0.0.1:9464
```

`gpui` and `web` both own incremental background ingestion and the local dashboard, so no cron job, sidecar service, Prometheus stack or Node runtime is required. On Windows, `gpui` is the default command.

Check the build and local store schema with:

```text
harness-observer version
```

To build from source, see [`docs/build.md`](docs/build.md).

## Dashboard

The Overview page is designed for comparisons rather than provider totals:

- correlated `harness → model → reasoning_effort` selection
- arbitrary date/time range with relative presets
- cross-filtered client/provider/project/evidence facets
- token and cost composition
- usage over time
- sortable breakdowns by harness, model, project, client and evidence basis
- visible pricing coverage so unknown rates never appear as `$0`

The Logs page exposes individual normalized events with server-side filtering and pagination. Missing capabilities remain `—`/null rather than being rendered as successful zero values.

See [`docs/dashboard.md`](docs/dashboard.md) and [`docs/logs.md`](docs/logs.md).

## Why the hierarchy is correlated

Harness, model and reasoning effort are not three independent dropdowns.

If the evidence contains:

```text
codex-desktop
├─ gpt-5.6-sol
│  ├─ high
│  └─ xhigh
└─ gpt-5.6-luna
   └─ max
```

selecting `sol/high + luna/max` means exactly those two observed tuples. It must not silently widen into `sol/max` or `luna/high`.

The same tuple semantics are preserved in the CLI and HTTP API.

## Token semantics

Harnesses name usage fields differently, so Harness Observer normalizes them before comparison.

Canonical buckets are:

```text
input_tokens        fresh / uncached prompt input
cache_read_tokens   prompt tokens served from cache
cache_write_tokens  prompt tokens written to cache
prompt_tokens       input + cache read + cache write
output_tokens       generated output
reasoning_tokens    reasoning detail/subset when explicitly exposed
```

For OpenAI/Codex-style counters where cached input is a subset of native input, Harness Observer subtracts the cached portion before assigning canonical fresh input. It never double counts cached input or reasoning output simply because both counters exist.

Every usage row also carries evidence semantics/basis so observed, derived-delta and estimated values remain distinguishable.

Full source-specific behavior is documented in [`docs/usage-semantics.md`](docs/usage-semantics.md).

## API-equivalent cost

Harness Observer keeps two cost concepts separate:

- `reported_cost_usd` — a cost the source itself persisted
- `estimated_api_cost_usd` — what the normalized observed usage would cost at published API rates

The estimate is useful for comparing harness/model workloads; it is **not actual subscription spend or a bill**.

Pricing is applied at query time from the canonical token buckets. Unknown models remain unpriced, and pricing coverage is reported explicitly instead of silently treating unresolved usage as free.

See [`docs/pricing.md`](docs/pricing.md).

## Commands

```text
harness-observer gpui      native desktop window + background incremental ingest
harness-observer web       local dashboard in a browser + background incremental ingest
harness-observer collect   stream normalized turns as NDJSON without storage
harness-observer ingest    incrementally persist normalized turns to DuckDB
harness-observer doctor    inspect source discovery/checkpoint health
harness-observer query     aggregate the local store over arbitrary time ranges
harness-observer logs      list/filter/page normalized turn events
harness-observer serve     local HTTP/JSON API + Prometheus metrics
harness-observer pricing   inspect/refresh pricing catalog and unpriced models
harness-observer version   print build, Go and store/parser versions
```

Run `harness-observer --help` for the command list, and
`harness-observer <command> -h` for that command's flags.

## Query examples

Aggregate an exact time window:

```powershell
harness-observer query `
  --since 2026-08-20T09:30:00 `
  --until 2026-08-20T17:15:00 `
  --group harness,model `
  --bucket hour
```

Preserve correlated harness/model/effort selection:

```powershell
harness-observer query `
  --select "codex-desktop|gpt-5.6-sol|high" `
  --select "codex-desktop|gpt-5.6-luna|max" `
  --group model,effort `
  --cost
```

Inspect individual events:

```powershell
harness-observer logs `
  --since 2026-08-20T09:00:00 `
  --until 2026-08-21T00:00:00 `
  --harness codex-cli `
  --limit 20 `
  --cost
```

## Local storage

The primary store is DuckDB under the platform user-data directory, for example:

```text
Windows  %LOCALAPPDATA%\harness-observer\harness-observer.duckdb
Linux    ~/.local/share/harness-observer/harness-observer.duckdb
macOS    ~/Library/Application Support/harness-observer/harness-observer.duckdb
```

Override it with `--db` or `HARNESS_OBSERVER_DB`.

DuckDB takes an exclusive writer lock for the process. `web` and `gpui` already
ingest on a timer, so a second `ingest` against the same database file will fail
to open it.

Ingestion is incremental and idempotent. Append-only files resume from checkpoints; truncation/replacement is detected; partially written JSONL records are retried when complete; parser-version changes can rebuild affected evidence without mutating the agent-owned source files.

## HTTP / Prometheus

`harness-observer serve` exposes the read-only query service without the
dashboard or background scanner. It binds to loopback by default:

```text
/api/health
/api/query
/api/logs
/api/hierarchy
/api/facets
/api/pricing
/api/pricing/unpriced
/metrics
```

`harness-observer web` and `harness-observer gpui` mount the same read API and
add ingest status/refresh routes (`/api/status`, `/api/refresh`,
`/api/pricing/refresh`) plus the embedded dashboard.

Prometheus/Grafana remain supported for aggregate observability, but the built-in dashboard is the primary UI. See [`docs/grafana.md`](docs/grafana.md).

## Architecture

```text
Codex / Claude / OMP / Pi / OpenCode / Grok Build
                    │
                    │ read-only local session evidence
                    ▼
               source adapters
                    │
                    ▼
             normalized turns
                    │
          incremental checkpoints
                    │
                    ▼
                  DuckDB
             ┌──────┴──────┐
             ▼             ▼
    native window / Vue UI    CLI / HTTP API
             │                     │
             └──────────┬──────────┘
                    ▼
        comparison / logs / cost
```

The model request path remains untouched.

## Development

The backend is Go; the frontend source lives in `webui/` and builds into embedded assets.

Typical checks:

```bash
cd webui
npm ci
npm run typecheck
npm test -- --run
npm run build
cd ..

go vet ./...
go test ./...
```

DuckDB is linked dynamically in release builds. Platform-specific setup, CGO details and release packaging are documented in [`docs/build.md`](docs/build.md).

## Contributing

Contributions are welcome, especially new harness adapters and real-world format regression fixes. Please read [`CONTRIBUTING.md`](CONTRIBUTING.md) before opening a pull request.

A new adapter should define its authoritative local evidence, token semantics, dedup/checkpoint strategy, identity attribution, unsupported fields and regression fixtures. Do not add guessed precision merely to make cross-harness tables look complete.

## Security

Please do not report credential or privacy issues in a public issue. See [`SECURITY.md`](SECURITY.md) for the private reporting path and the project's security/privacy boundaries.

## License

MIT — see [`LICENSE`](LICENSE).
