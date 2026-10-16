# Log evidence capability matrix

This matrix is a hard constraint for the Logs feature. A Logs table must represent what local session artifacts actually persist, not what a proxy/request logger would ideally expose.

Legend:

- **YES** — persisted evidence is strong enough to expose the field honestly.
- **PARTIAL** — the source has relevant evidence, but coverage/granularity is incomplete or the current adapter does not yet normalize it.
- **NO** — no reliable local-session evidence has been verified; do not synthesize it.

## Common fields

| Field | Codex | Claude Code/Desktop | Pi / OMP | OpenCode | Grok Build (new `turn_completed`) | Grok legacy |
| --- | --- | --- | --- | --- | --- | --- |
| timestamp | YES | YES | YES | YES | YES | PARTIAL (session/stream timestamps) |
| harness | YES | YES | YES | YES | YES | YES |
| client/frontend | YES (`originator`) | YES (`entrypoint`) | PARTIAL (source identity only) | PARTIAL | PARTIAL | PARTIAL |
| provider | YES (`model_provider`) | PARTIAL (current adapter assumes Anthropic) | YES | YES (`providerID`) | YES/derivable as xAI/model evidence | YES/derivable |
| model | YES | YES | YES | YES (`modelID`) | YES (`modelUsage`/summary) | YES (summary/signals) |
| reasoning effort | YES | YES | YES | NO until a persisted OpenCode field with equivalent semantics is verified | NO verified equivalent | NO |
| project / cwd | YES | YES | YES | YES (`path`/session directory) | YES (summary cwd) | YES |
| session id | YES | YES | YES | YES | YES | YES |
| turn/message id | YES/PARTIAL (`turn_id`, record identity) | YES via assistant message id/record id | YES/PARTIAL via parent/record ids | YES via message id | YES via `prompt_id` | NO reliable per-turn authoritative id |
| event kind | YES | YES | YES (`assistant`, `tool-llm`, compaction, branch summary) | YES/PARTIAL | YES (`turn_completed`; structured tool events separately) | only a clearly labeled estimated/session aggregate if emitted |
| source evidence location | YES | YES | YES | YES (file path or DB/message identity) | YES | YES |

## Usage/accounting fields

| Field | Codex | Claude | Pi / OMP | OpenCode | Grok new | Grok legacy |
| --- | --- | --- | --- | --- | --- | --- |
| input tokens | YES | YES | YES | YES | YES | estimated only |
| cache-read tokens | YES | YES | YES | YES | YES | estimated only if defensible |
| cache-write tokens | YES (observed locally, usually 0) | YES | YES | YES when provider mapping exposes it | YES | NO/estimated only |
| output tokens | YES | YES | YES | YES | YES | estimated only |
| reasoning tokens | YES when reported | YES when reported | YES when reported | YES | YES | NO |
| source-reported cost | NO verified field | NO verified field | YES (`usage.cost.total`) | YES (`AssistantMessage.cost`) | NO: `costUsdTicks` scale is undocumented and must not be called USD | NO |
| Est. API cost | YES at query time when priced | YES | YES | YES | YES | only if estimated usage is explicitly included/labeled |

## Tool/error fields

| Field | Codex | Claude | Pi / OMP | OpenCode | Grok new | Grok legacy |
| --- | --- | --- | --- | --- | --- | --- |
| tool call count | YES (structured call events) | YES (structured content blocks) | YES | YES (parts) | PARTIAL: structured tool events exist, but per-turn correlation must be verified | session-level/partial |
| tool names | persisted, current normalized schema keeps only count | persisted, current schema keeps only count | persisted, current schema keeps only count | YES in parts | YES in structured updates/signals | session-level |
| tool error count | **NO in current normalized evidence** | PARTIAL: transcript tool-result error evidence exists but current adapter does not correlate/normalize it | YES where `toolResult.isError` is persisted | PARTIAL: tool state/error can be source evidence; verify exact current schema | NO verified per-turn error mapping yet | NO |
| model/request error | NO generic request outcome | NO generic request outcome currently normalized | PARTIAL (`isError` is tool-result scoped) | YES: assistant message has structured optional `error` | PARTIAL: only expose if an explicit structured failure update is verified | NO |

### Important `tool_errors` caveat

The current `model.Turn.ToolErrors` is an `int`, so a stored `0` cannot by itself distinguish:

```text
observed zero errors
vs
this adapter does not observe tool-result failures
```

Do **not** render every existing `tool_errors=0` as evidence of success. The Logs API needs an availability/provenance rule (nullable DTO, explicit capability/evidence flag, or a normalized schema improvement) before `tool_errors` is presented as a trustworthy cross-harness metric.

The same principle applies to any newly added optional diagnostic metric: absence is not zero.

## Performance/request diagnostics

| Field | Codex | Claude | Pi / OMP | OpenCode | Grok new | Grok legacy |
| --- | --- | --- | --- | --- | --- | --- |
| duration | NO reliable model-call duration verified | NO reliable model-call duration verified | NO verified | **YES**: `time.completed - time.created` when completed exists | **YES**: `turn_completed.usage.apiDurationMs` when persisted | NO |
| output tok/s (est.) | ESTIMATED | ESTIMATED | ESTIMATED | ESTIMATED | ESTIMATED | NO |
| TTFT | **NO** | **NO** | **NO** | NO verified persisted first-token timestamp | NO verified | NO |
| HTTP status code | **NO** | **NO** | **NO** | NO | NO | NO |
| upstream HTTP/provider error text | NO | NO | NO | only structured assistant error type/detail with content-safety review; never invent HTTP status | only if explicitly persisted in a structured failure record | NO |
| retries/attempts | NO common verified metric | NO common verified metric | NO | not verified as durable per-call evidence | not verified | NO |

**Estimated tok/s.** The Logs surface reports `est_output_tokens_per_second` = output tokens / `interval_ms`, where `interval_ms` is the gap to the previous stored turn of the same session (by harness + session id, measured before any filter, millisecond precision from `ts`). It is a deliberate, owner-approved exception to the no-adjacent-timestamp rule below, chosen because Claude Code and Codex persist no per-turn duration. The interval includes tool execution and user wait time, so the figure usually understates real decode speed; it is null for a session's first turn and for turns with no output, and every surface labels it as an estimate. It is derived per query and never stored.

OpenCode's current AssistantMessage schema explicitly carries `time.created`, optional `time.completed`, tokens/cache, cost, provider/model/session ids and optional structured `error`. An unfinished/orphaned message may have no `time.completed`, so duration must remain NULL for it rather than becoming zero.

Newer Grok `turn_completed.usage` examples carry per-turn input/output/cache/reasoning usage and `apiDurationMs`, with `prompt_id` and a JSONL timestamp. `costUsdTicks` remains intentionally ignored because its unit/scale is not documented.

## Logs product rules derived from this matrix

### Safe common-denominator table columns

The default table can reliably center on:

```text
Time
Harness
Model
Effort (nullable)
Project
Event
Input
Cache read
Cache write
Output
Tools
Est. API cost
Session
```

`Effort` is naturally NULL for harnesses that do not persist an equivalent setting.

### Optional/capability-aware fields

Keep these out of the assumption that every row has them. Show them in detail or as optional columns only when evidence exists:

```text
Reasoning
Reported cost
Tool errors
Duration
Output tok/s
Structured error type
Provider/client details
```

Never add generic `HTTP status` or `TTFT` columns until a future source actually persists them.

### Grok legacy rows

Do not make a legacy Grok context-curve estimate look like an ordinary authoritative request/turn log row.

If retained, it must be clearly typed, for example:

```text
event_kind = session-estimate
usage_basis = estimated
```

with no fake turn id, duration, TTFT, status, or per-turn tool/error attribution. Prefer omitting it from strict/default Logs views if that is clearer.

### Capability-aware DTO

The Logs API should use nullable/optional fields for diagnostics whose presence is not universal. A consumer must be able to distinguish unavailable from observed zero.

Apart from the labelled tok/s estimate above, do not infer a missing metric from adjacent timestamps, token volume, model names, prose, or global configuration merely to fill the table.
