-- Reference copy of the schema harness-observer creates and migrates itself
-- (see internal/store/store.go, migrations v1 to v3). Keep the two in sync; the
-- Go migrations are authoritative at runtime.
--
-- Engine is DuckDB. Nothing here stores prompts, model responses, tool
-- arguments, tool output, raw session JSON or credentials. Observability
-- metadata only.

CREATE TABLE schema_version (
  version INTEGER NOT NULL,
  applied_at TEXT NOT NULL
);

CREATE TABLE agent_turns (
  ts TEXT NOT NULL,                 -- RFC3339 UTC, human/debug readable
  ts_unix BIGINT NOT NULL,          -- range filter
  harness TEXT NOT NULL,            -- codex-desktop, codex-cli, claude-code, claude-desktop, omp, pi
  client TEXT,                      -- originator/frontend identity when the source exposes one
  provider TEXT,
  model TEXT,
  reasoning_effort TEXT,            -- thinking/effort level, NULL when the source recorded none
  project TEXT,
  cwd TEXT,
  session_id TEXT NOT NULL,
  record_id TEXT NOT NULL,          -- stable identity of one usage event, survives replays
  turn_id TEXT,
  event_kind TEXT NOT NULL,         -- assistant, tool-llm, compaction, branch-summary
  input_tokens BIGINT NOT NULL DEFAULT 0,
  cache_read_tokens BIGINT NOT NULL DEFAULT 0,
  cache_write_tokens BIGINT NOT NULL DEFAULT 0,
  prompt_tokens BIGINT NOT NULL DEFAULT 0,
  output_tokens BIGINT NOT NULL DEFAULT 0,
  reasoning_tokens BIGINT NOT NULL DEFAULT 0,    -- subset of output_tokens, never additive
  native_input_tokens BIGINT,
  native_total_tokens BIGINT,
  usage_semantics TEXT NOT NULL,    -- split-cache | input-includes-cache-read
  usage_basis TEXT NOT NULL,        -- observed | derived-delta | estimated
  tool_calls BIGINT NOT NULL DEFAULT 0,
  tool_errors BIGINT NOT NULL DEFAULT 0,
  cost_usd DOUBLE,                  -- cost the SOURCE persisted, when it does. Evidence.
                                    -- The API-equivalent cost estimate is never stored here:
                                    -- it is derived per query from the pricing catalog, so a
                                    -- price correction never means re-reading session files.
                                    -- See docs/pricing.md.
  source_file TEXT NOT NULL,
  source_line BIGINT NOT NULL,
  PRIMARY KEY (harness, record_id)  -- deduplication identity and ON CONFLICT target
);

-- There are deliberately no secondary indexes. DuckDB's columnar scans and zone
-- maps answer the range and equality filters a row store would need indexes
-- for, and every ART index is also stored in the database file: on the local
-- corpus the eleven indexes a SQLite schema wants left query latency unchanged
-- and grew the database from 12.9 MB to 34.4 MB.

-- On conflict the row with the EARLIEST timestamp wins, and a re-parse of the
-- same evidence (equal timestamp) refreshes the normalized attribution columns
-- so a parser upgrade reaches rows that are already stored. See
-- internal/store/store.go: the gate is "excluded.ts_unix <= agent_turns.ts_unix".

-- Per-file incremental ingestion state. head_fingerprint is "<prefix bytes>:<sha256>"
-- of the file's leading bytes, so a rotated/truncated/rewritten file is detected
-- and re-read from zero while an appended one resumes at offset_bytes.
--
-- A file's turns and its checkpoint are committed in one transaction, so a
-- checkpoint can never outlive the rows it accounts for.
CREATE TABLE ingest_checkpoints (
  source TEXT NOT NULL,
  path TEXT NOT NULL,
  size BIGINT NOT NULL,
  mtime_unix_ms BIGINT NOT NULL,
  head_fingerprint TEXT NOT NULL,
  offset_bytes BIGINT NOT NULL,
  line_no BIGINT NOT NULL,
  parser_version BIGINT NOT NULL,   -- bumped by the binary to force a rebuild
  state_json TEXT,                  -- adapter parse state (session id, model, effort, ...)
  turns_ingested BIGINT NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (source, path)
);
