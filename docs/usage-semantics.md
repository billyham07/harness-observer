# Token accounting contract

`harness-observer` does not assume that a field named `input` means the same thing in every harness/provider trace. Adapters normalize authoritative local usage evidence into disjoint buckets while retaining native counters when they carry different semantics.

## Canonical buckets

| Field | Meaning |
| --- | --- |
| `input_tokens` | Fresh / uncached prompt input. Cache reads are excluded. |
| `cache_read_tokens` | Prompt tokens served from prompt cache. |
| `cache_write_tokens` | Prompt tokens written into prompt cache when exposed. |
| `prompt_tokens` | `input_tokens + cache_read_tokens + cache_write_tokens`; preferred cross-harness prompt-volume metric. |
| `output_tokens` | Output tokens reported by the source. |
| `reasoning_tokens` | Reasoning/thinking tokens when explicitly exposed. Never inferred from text. In every source verified so far it is a **subset of `output_tokens`**, so it is reported for visibility and never added on top. |
| `native_input_tokens` | Raw source input counter when available. |
| `native_total_tokens` | Authoritative source total when available; never synthesized into this field. |

`basis` is `observed`, `derived-delta`, or `estimated`. Strict comparisons should normally exclude `estimated`.

The mechanical cross-harness total is therefore:

```text
observed_tokens = prompt_tokens + output_tokens
```

Reasoning is deliberately absent from that sum. Verified on real evidence: Codex rollouts satisfy `total_tokens == input_tokens + output_tokens` while `reasoning_output_tokens < output_tokens`, and Anthropic reports `output_tokens_details.thinking_tokens` inside `output_tokens`.

## Codex CLI / Codex Desktop

Evidence: `~/.codex/sessions/**/rollout-*.jsonl` and `~/.codex/archived_sessions/*.jsonl`, especially `event_msg/token_count`.

Codex/OpenAI reports `input_tokens`, `cached_input_tokens`, `output_tokens`, `reasoning_output_tokens`, and optional `total_tokens`. Cached input is a subset of native input, therefore:

```text
canonical input_tokens = max(0, native input_tokens - cached_input_tokens)
cache_read_tokens      = cached_input_tokens
cache_write_tokens     = cache_write_input_tokens
prompt_tokens          = native input_tokens
reasoning_tokens       = reasoning_output_tokens
```

`cache_write_input_tokens` is present in real `token_count` payloads (observed value: `0` in every local rollout inspected so far). Because those same payloads keep `total_tokens == input_tokens + output_tokens`, cache writes are treated as a **subset of** the native input counter, not as an extra bucket added to it. Fresh input is therefore `native input - cached - cache_write`, clamped at zero, and `prompt_tokens` stays equal to the native input counter. If a source ever reports cache buckets larger than its own input counter, fresh input clamps to zero and the invariant that survives is `prompt = input + cache_read + cache_write`.

### Resumed and forked rollouts

Resuming or forking a Codex conversation writes a **new** rollout file that shares `session_id` with the original and replays the earlier `token_count` events. Verified against real sessions (up to 11 files for one `session_id`): the replayed events keep their original usage numbers but receive a **new timestamp** (the resume time) and land on **different line numbers**.

Consequences for the record identity:

- The timestamp cannot be part of it (replays would look new).
- The line number cannot be part of it (a fork shifts every line).
- What is stable is the ordinal position of the usage event within the session plus the usage numbers themselves, so the record id is `session_id:token_count:<ordinal>:<usage fingerprint>`.

Two forks that diverge at the same ordinal report different numbers and stay separate records. When the same identity is seen more than once, the store keeps the **earliest** timestamp, so a time series does not shift just because a session was resumed later.

When `last_token_usage` is absent but `total_token_usage` is present, reconstruct the per-event values from cumulative deltas and mark `basis=derived-delta`. Keep `session_meta.payload.originator` as `client` so Codex Desktop, CLI/TUI, and other app-server clients remain distinguishable without treating originator as a format discriminator.

## Claude Code / Claude Desktop Agent / Cowork

Evidence roots include `%APPDATA%/Claude/{local-agent-mode-sessions,claude-code-sessions}` on Windows and `~/Library/Application Support/Claude/{local-agent-mode-sessions,claude-code-sessions}` on macOS, plus corresponding `Claude-3p` roots when present.

Join Desktop metadata `cliSessionId` to authoritative Claude-Code-shaped transcripts under `~/.claude/projects` when needed. Exclude `audit.jsonl`, which is a partial/streaming snapshot.

Both frontends write into `~/.claude/projects`, so the frontend is read per row from `entrypoint` (`cli` -> `claude-code`, `claude-desktop` -> `claude-desktop`) rather than from the directory a transcript happens to sit in. A session started in one frontend and resumed in the other splits at the turn where the evidence changes. A row without an `entrypoint` falls back to the Desktop claim (`cliSessionId`, or a transcript nested under a Desktop session root) and is otherwise CLI. An unrecognized entrypoint keeps the generic `claude` harness and reports its raw value as `client`, so a new frontend shows up as itself instead of being folded into an existing one.

Claude maps directly from disjoint buckets:

```text
input_tokens       = message.usage.input_tokens
cache_read_tokens  = message.usage.cache_read_input_tokens
cache_write_tokens = message.usage.cache_creation_input_tokens
prompt_tokens      = input + cache_read + cache_write
output_tokens      = message.usage.output_tokens
reasoning_tokens   = message.usage.output_tokens_details.thinking_tokens (subset of output)
```

Use Claude message IDs for evidence deduplication across Desktop/global transcript copies: resuming or forking a session copies earlier assistant messages verbatim into the new transcript, and the message id is what stops them from being counted twice.

Assistant messages whose usage object is entirely zero (`<synthetic>` model, injected messages) are not model calls and are not recorded.

## Pi / Oh My Pi

Evidence: `~/.pi/agent/sessions/**/*.jsonl` and `~/.omp/agent/sessions/**/*.jsonl`.

Persisted Pi `Usage` contains `input`, `output`, `cacheRead`, `cacheWrite`, `totalTokens`, and `cost.total`. The four token buckets are already disjoint; retain `totalTokens` as `native_total_tokens` and `cost.total` as `cost_usd`.

Pi session totals are not assistant-message-only: nested `toolResult` LLM work and `compaction` / `branch_summary` entries may also carry usage and count toward session totals. Emit these as separate `event_kind` values (`tool-llm`, `compaction`, `branch-summary`) rather than silently undercounting Pi/OMP.

Evidence gap observed on real local OMP sessions: `compaction` entries there carry `tokensBefore` (the context size at compaction time) and **no** `usage` object, and no `toolResult` message carried usage either. `tokensBefore` is context volume, not billed usage, so nothing is emitted for those records — the buckets stay missing rather than being synthesized. The adapter still handles `usage` on these entries because the format allows it and other installs may produce it.

## Reasoning effort / thinking level

`reasoning_effort` is a nullable canonical dimension recording the thinking/effort
level a harness was configured with for one model call. It is **never** inferred
from reasoning-token volume or from a model name: a model that emits many
reasoning tokens is not evidence that a high effort was requested, and NULL is a
real answer that stays visible in the hierarchy as "(no effort recorded)".

The value space is deliberately open. Values seen locally are `low`, `medium`,
`high`, `xhigh`, `max`, `ultra` and `off`. `ultra` appears in real Codex rollouts
and is absent from every list published so far, which is the reason nothing here
validates against a closed enum.

`off` and NULL are different answers and must stay different: `off` means the
source recorded that thinking was disabled, NULL means it recorded nothing.

### Codex

Evidence: `turn_context.payload.effort`, with
`turn_context.payload.collaboration_mode.settings.reasoning_effort` as the
fallback for rollouts that only carry the nested form. Both agree in every local
rollout. A turn_context declares the effort for the turn it opens, so the value
is carried forward to the `token_count` events that follow it, and across an
incremental resume through the checkpoint state.

Counter-evidence that must not be used: `session_meta` also contains the string
`reasoningEffort`, but only inside the input schema of an advertised cron tool
(`"One of none, minimal, low, medium, high, xhigh"`). That is a tool definition,
not a session setting. All 3744 turn_context records across 356 local rollouts
carry `effort`; none of them omit it.

### Pi / Oh My Pi

Evidence: `thinking_level_change.thinkingLevel`. The event is emitted when the
level changes, so the value applies to every later usage event until the next
change and is carried across an incremental resume.

`thinkingLevel: null` is emitted in real sessions (with `configured: null`) and
clears the level back to unknown rather than leaving a stale level attached to
calls that no longer used it. The sibling `configured` field is deliberately not
used: it records what the user asked for, including `auto`, while
`thinkingLevel` is the level the call actually ran at.

84 of the 89 local Pi/OMP sessions declare a level before their first usage
event; the remaining 5 never declare one and stay NULL.

### Claude Desktop / Claude Code

Evidence: a flat `effort` field on the assistant transcript entry itself
(`{"type":"assistant", ..., "effort":"max", "message":{...}}`).

This is read per entry and never inherited from a neighbouring entry: an entry
that does not declare an effort stays unknown. On this machine 14085 of 14094
assistant entries carry it (`medium`, `high`, `xhigh`, `max`); the 9 without it
are all `<synthetic>` messages, which are already excluded as non-calls, so
every real Claude model call in the corpus is attributed.

### OpenCode

Evidence: assistant-message `variant` when present, otherwise session `model.variant`.

### Grok Build

Evidence: `summary.json` `reasoning_effort` (session-level, carried to each
authoritative turn). NULL when the summary does not record one.

## OpenCode

Evidence: `$OPENCODE_DATA_DIR` / `$XDG_DATA_HOME/opencode` / `~/.local/share/opencode`,
and on Windows also `%LOCALAPPDATA%\opencode` if present. Current releases store
JSON under `storage/{session,message,part}/`; older installs use `opencode*.db`
(read-only). Required SQLite tables are `session`, `message`, `part`; a missing
table is a loud error, not a silent zero.

Message `tokens` are disjoint Anthropic-style buckets:

```text
input_tokens       = tokens.input
cache_read_tokens  = tokens.cache.read
cache_write_tokens = tokens.cache.write
output_tokens      = tokens.output
reasoning_tokens   = tokens.reasoning
native_total_tokens= tokens.total when persisted
```

`cost` on the assistant message is source-reported spend. Child sessions
(`parentID` / `parent_id`) still emit one row per assistant message; `session_id`
is the root so they do not appear as a second conversation. Record identity is
`opencode:<message-session-id>:<message-id>`.

Zero-usage assistant messages with text or tool parts are kept. Empty
placeholders with no tokens, cost, or activity are dropped.

## Grok Build

Evidence: `$GROK_HOME/sessions` or `~/.grok/sessions/<url-encoded-cwd>/<uuid>/`
with `summary.json`, `signals.json`, and `updates.jsonl`.

Authoritative path: `params.update.sessionUpdate == "turn_completed"` with a
`usage` object. Native `inputTokens` includes cache, matching Codex:

```text
canonical input = max(0, inputTokens - cachedReadTokens - cacheCreationTokens)
cache_read      = cachedReadTokens
cache_write     = cacheCreationTokens
output          = outputTokens
reasoning       = min(reasoningTokens, outputTokens)  // subset of output
```

`costUsdTicks` is undocumented and is never treated as USD. `apiDurationMs` is
not stored (no duration column). TTFT and HTTP status are not invented.

Legacy sessions that only have `_meta.totalTokens` / `_meta.promptId` context
curves are **uncovered**, not estimated. A mixed session emits only the
authoritative turns; the rest is reported by `doctor`, never blended in.

Record identity is `grok:<session-id>:<prompt_id>`. Duplicate `turn_completed`
rows for the same prompt id collapse to one record.

## Rules for future adapters


1. Prefer authoritative persisted counters over text-length estimates.
2. Never silently coerce an unavailable metric into an estimate.
3. Keep cache reads and writes separate from fresh input.
4. Preserve native counters when normalization changes their meaning.
5. Record `basis` and source accounting `semantics`.
6. Deduplicate logical API responses before aggregation; source files are evidence, not necessarily one-to-one with requests.
7. Check what a harness does on resume/fork before trusting a record identity. Replaying history into a new file is common, and it is the main way a collector silently doubles its numbers.
8. A dimension is only populated from explicit persisted evidence. If a harness does not record it, the column stays NULL and the dashboard says so; a plausible guess is worse than a visible gap.
