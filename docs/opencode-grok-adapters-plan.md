# OpenCode + Grok Build adapters implementation contract

## Goal

Finish the next two harnesses already called out by the roadmap:

```text
OpenCode
Grok Build
```

Both must plug into the same `source.FileSource` / normalized `model.Turn` pipeline as Codex, Claude, Pi and OMP. No proxying, wrappers or provider API calls.

Use public implementations such as CodeBurn as format references, but validate against the actual current local artifacts wherever available. Public parser behavior is evidence for discovery, not permission to invent fields.

## Shared adapter rules

For both adapters:

- passive, read-only local artifact ingestion
- no credentials/OAuth/API key access
- no prompts/responses/reasoning text/tool arguments/tool output persisted
- dimensions populated from persisted evidence or left empty
- use canonical token buckets correctly
- `reasoning_tokens` remains a subset/detail of output when source semantics say so; never double count observed tokens or estimated cost
- tool counts/errors only when reliably derivable
- source file/line or equivalent evidence location retained
- incremental ingestion/checkpointing must work with the existing parser-state mechanism
- every real format mismatch becomes a minimized fixture/regression test
- doctor must explain roots searched, files/dbs discovered and actionable parse/schema warnings

## OpenCode

Reference format behavior from CodeBurn:

- default data root: `$OPENCODE_DATA_DIR`, else `$XDG_DATA_HOME/opencode`, else `~/.local/share/opencode`
- current OpenCode 1.1+ may use file-based JSON under `storage/`
- older/current variants may also expose SQLite `opencode*.db`
- compatible forks can relocate/rename the data directory/database
- logical records involve sessions/messages/parts
- root sessions can own child/grandchild agent sessions; avoid double counting child sessions as independent full sessions while still including their usage
- token fields may include input, output, reasoning, cache read and cache write
- assistant messages with genuine text/tool activity but zero/missing router usage may be valid zero-usage calls; empty placeholders should not become fake turns
- part ordering matters

### OpenCode discovery

Implement cross-platform root resolution, including Windows. Do not assume the Linux XDG path is the only installation layout.

Support explicit overrides so users can point to compatible forks/custom installs. Exact flags/env names may follow the project style; `OPENCODE_DATA_DIR` compatibility is useful.

Discovery should detect and report which storage backend is in use:

```text
file-json
sqlite
```

Do not require both.

### OpenCode SQLite safety

OpenCode owns its DB. Observer access must be read-only and must not mutate schema/journal state.

- prefer read-only connection mode
- no migrations/PRAGMA that changes source DB state
- handle locked/busy source gracefully with an actionable warning rather than corrupting ingestion
- validate required tables/columns and report schema incompatibility loudly; do not silently return zero turns

### OpenCode attribution

Preserve when authoritative:

```text
harness = opencode
client
provider
model
reasoning_effort/thinking setting
project/cwd
session/root-session identity
event kind
tool activity
```

Do not attribute provider/model from global config when a message/session persisted a more specific value.

### OpenCode dedup/session tree

Define a stable record identity, preferably based on authoritative session/message IDs.

Child/subagent sessions must not make totals appear twice. If root-session aggregation is needed for discovery, each underlying assistant/message usage event should still produce one normalized event with stable identity and enough parent/root attribution to query later if the current schema supports it. Do not create a synthetic aggregate row in addition to the real rows unless existing architecture explicitly requires one.

## Grok Build

Reference current known layout:

```text
$GROK_HOME/sessions/
~/.grok/sessions/
  <url-encoded-cwd>/<session-uuid>/
    summary.json
    signals.json
    updates.jsonl
```

Known evidence roles:

- `summary.json`: session id/cwd/timestamps/current model
- `signals.json`: models/tools/context signals
- `updates.jsonl`: ACP stream; newer versions may emit `turn_completed` records with provider-recorded usage and prompt IDs

### Grok token evidence precedence

Prefer authoritative per-turn `turn_completed.usage` when present.

Map only fields whose semantics are verified. Expected shape may include:

```text
inputTokens
outputTokens
cachedReadTokens
cacheCreationTokens
reasoningTokens
```

Treat cache read/write as input subsets only if the observed format establishes that. Clamp impossible subset violations locally and warn/test; do not silently distort totals.

Do not use undocumented `costUsdTicks` as dollars.

### Grok legacy fallback

Older sessions may only expose running context values such as `_meta.totalTokens` grouped by `_meta.promptId`.

If an estimator is implemented:

- mark `usage_basis=estimated`
- keep it isolated from authoritative turns
- never blend estimated and authoritative token buckets for the same turn
- document exactly what is estimated
- prefer under-reporting with visible coverage/warning over a heuristic that claims false precision

If the current source evidence is insufficient for a defensible per-turn estimate, it is acceptable to emit only authoritative covered turns and report uncovered/legacy sessions in doctor diagnostics. Do not fabricate a polished total merely to match CodeBurn.

### Grok model attribution

Use persisted `turn_completed`/usage model identity when authoritative, otherwise summary/signals current model. Preserve raw observed model ID and let the existing pricing resolver handle aliases. Do not hardcode a fake model solely to obtain a price.

### Grok tools

Use structured tool signals/events when available. Do not parse shell command text or response prose to infer tool usage.

## Registration / UX

Update source registration so defaults become at least:

```text
codex,claude,omp,pi,opencode,grok
```

Use stable accepted names and useful aliases if needed (`grok-build`).

Update:

- `--sources` help
- `doctor`
- README support list
- relevant web/background ingestion source lists

A missing optional harness root is normal and should not make default ingestion fail. Existing sources already tolerate absent roots; new adapters must too.

## Incremental ingestion

Both adapters must fit current checkpoint rules.

### File-backed OpenCode/Grok

- unchanged files not re-read
- append-only JSONL resumes from byte offset
- partial final line not checkpointed as complete
- truncation/rewrite detected
- parser-version bump can rebuild affected source only

### OpenCode SQLite

A DB is not an append-only JSONL file. Do not fake byte offsets.

Introduce a backend-appropriate incremental cursor/checkpoint, e.g. stable message/event timestamp + id / monotonically ordered primary key where the schema guarantees it. If no safe monotonic cursor exists, use a bounded re-query window with stable dedup in the destination rather than scanning the entire DB every poll.

Checkpoint state must remain source-owned/adapter-specific and versioned.

## Real-session validation

Before calling either adapter done:

- run against real local OpenCode/Grok data if present
- compare discovered session/turn/token counts against source-native visible summaries or an independent parser where possible
- inspect several sessions manually for model/cache/reasoning/tool semantics
- add fixtures derived/minimized from real shape without conversation content

`doctor` should report coverage such as:

```text
OpenCode backend/storage roots and schema version
Grok sessions with authoritative turn_completed usage
Grok legacy/uncovered sessions
parse warnings by format/version
newest activity
```

## Tests: OpenCode

- root discovery + override precedence
- absent install is harmless
- file-based storage fixture
- SQLite storage fixture
- missing/incompatible SQLite schema warns/fails visibly instead of zero data
- child/subagent sessions do not double count
- stable message dedup
- part ordering
- input/output/cache-read/cache-write normalization
- reasoning not double counted
- zero-usage real assistant/tool event vs empty placeholder
- incremental SQLite/file behavior

## Tests: Grok Build

- `GROK_HOME` and default root discovery
- summary/signals/updates parsing
- duplicate `turn_completed` prompt ids deduped
- authoritative usage normalization
- cache subset violation handling
- reasoning bounded by/output semantics preserved
- legacy records clearly estimated or explicitly uncovered
- mixed authoritative/legacy session does not double count or silently blend
- undocumented cost ticks ignored
- stable record identity and incremental JSONL tailing
- tool signals counted only from structured evidence

## Agent execution contract

Implement both production adapters end-to-end; do not stop at format research.

1. Inspect existing Codex/Claude/Pi adapters and checkpoint abstractions first.
2. Inspect current OpenCode/Grok artifacts on the machine if present before finalizing mappings.
3. Use CodeBurn adapters/docs as a reference for edge cases, not as unquestioned truth.
4. Prefer authoritative usage over estimates.
5. Estimated evidence must carry `UsageEstimated`; derived cumulative deltas use the existing basis where appropriate.
6. Unknown/missing data stays unknown, not zero unless zero is actually reported.
7. Do not change existing source semantics to make the new adapters easier.
8. Do not regress cost resolver, logs work, dashboard or existing ingestion.
9. `gofmt`, `go vet`, and `go test ./...` remain green.

## Definition of done

- [x] OpenCode source registered and enabled by default
- [x] OpenCode file + SQLite formats handled where current releases require them
- [x] OpenCode child/subagent usage does not double count
- [x] Grok Build source registered and enabled by default
- [x] Grok authoritative completed-turn usage parsed when available
- [x] Grok legacy/uncovered evidence is honestly labeled
- [x] both adapters incremental/idempotent
- [x] doctor has actionable discovery/coverage diagnostics
- [x] model/provider/project/effort attribution uses source evidence
- [x] tool counts only from structured evidence
- [x] fixtures/tests cover format and dedup edge cases
- [x] real local data validated when available
- [x] README/help updated
- [x] `go test ./...` passes
