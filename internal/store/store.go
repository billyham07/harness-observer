// Package store owns durable local persistence: a versioned DuckDB schema for
// normalized agent turns plus the per-file ingestion checkpoints.
//
// Deliberately absent from this schema: prompts, model responses, tool
// arguments, tool output, raw session JSON, credentials. Only observability
// metadata is persisted.
package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"

	"github.com/marcboeker/go-duckdb/v2"
)

// SchemaVersion is the current migration level. Bump it and append to
// migrations when the persisted shape changes.
const SchemaVersion = 3

// maxConns bounds the shared instance's connection pool. Reads are concurrent;
// writes come from one ingestion goroutine, so there is no writer contention to
// tune for.
const maxConns = 8

type DB struct {
	sql  *sql.DB
	path string
}

// DefaultPath returns the per-user database location. Windows uses LOCALAPPDATA
// so the file never lands in a roaming profile.
func DefaultPath() string {
	if v := os.Getenv("HARNESS_OBSERVER_DB"); v != "" {
		return v
	}
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = os.Getenv("APPDATA")
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, "Library", "Application Support")
		}
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			if home, err := os.UserHomeDir(); err == nil {
				base = filepath.Join(home, ".local", "share")
			}
		}
	}
	if base == "" {
		base = "."
	}
	return filepath.Join(base, "harness-observer", "harness-observer.duckdb")
}

// LegacySQLitePath is where releases before the DuckDB switch kept their store.
// The file is never opened, migrated or deleted: the normalized data is cheaper
// and safer to rebuild from the session evidence than to migrate row by row,
// and deleting somebody's old database to save a few seconds is not a trade the
// collector gets to make.
func LegacySQLitePath() string {
	current := DefaultPath()
	return filepath.Join(filepath.Dir(current), "harness-observer.db")
}

// LegacySQLiteStore returns the legacy path if it still exists, so the CLI can
// mention it once instead of leaving an orphan file unexplained.
func LegacySQLiteStore() (string, bool) {
	path := LegacySQLitePath()
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, true
	}
	return "", false
}

func Open(path string) (*DB, error) {
	if path == "" {
		path = DefaultPath()
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	// One connector, many connections: DuckDB takes an exclusive file lock per
	// process, so every connection has to share a single database instance. It
	// also means the dashboard can serve queries while an ingest transaction is
	// open, instead of serializing every page load behind the scan.
	connector, err := duckdb.NewConnector(filepath.ToSlash(path), func(execer driver.ExecerContext) error {
		// Bucketing uses date_trunc on a timestamp derived from ts_unix. Without
		// a pinned zone that truncation would follow whatever the machine's
		// local time happens to be, and a stored time series would shift when
		// the user travels or the host changes its offset.
		_, err := execer.ExecContext(context.Background(), "SET TimeZone='UTC'", nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	handle := sql.OpenDB(connector)
	handle.SetMaxOpenConns(maxConns)
	db := &DB{sql: handle, path: path}
	if err := db.migrate(); err != nil {
		handle.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) Close() error { return d.sql.Close() }
func (d *DB) Path() string { return d.path }
func (d *DB) SQL() *sql.DB { return d.sql }

// migrations are applied in order, one statement at a time: the DuckDB driver
// prepares a single statement per Exec, so a migration is a list rather than a
// script.
//
// There are deliberately no secondary indexes. DuckDB's columnar scans and zone
// maps already answer the range and equality filters a row store would need an
// index for, and every ART index is also stored in the database file. Measured
// on this machine's corpus, the eleven indexes a SQLite schema wants left query
// latency unchanged and inflated the database from 12.9 MB to 34.4 MB. The
// composite primary key stays: it is the deduplication identity and the
// ON CONFLICT target.
var migrations = [][]string{
	// v1: normalized turns + per-file ingestion checkpoints.
	{
		`
CREATE TABLE IF NOT EXISTS agent_turns (
  ts TEXT NOT NULL,
  ts_unix BIGINT NOT NULL,
  harness TEXT NOT NULL,
  client TEXT,
  provider TEXT,
  model TEXT,
  project TEXT,
  cwd TEXT,
  session_id TEXT NOT NULL,
  record_id TEXT NOT NULL,
  turn_id TEXT,
  event_kind TEXT NOT NULL,
  input_tokens BIGINT NOT NULL DEFAULT 0,
  cache_read_tokens BIGINT NOT NULL DEFAULT 0,
  cache_write_tokens BIGINT NOT NULL DEFAULT 0,
  prompt_tokens BIGINT NOT NULL DEFAULT 0,
  output_tokens BIGINT NOT NULL DEFAULT 0,
  reasoning_tokens BIGINT NOT NULL DEFAULT 0,
  native_input_tokens BIGINT,
  native_total_tokens BIGINT,
  usage_semantics TEXT NOT NULL,
  usage_basis TEXT NOT NULL,
  tool_calls BIGINT NOT NULL DEFAULT 0,
  tool_errors BIGINT NOT NULL DEFAULT 0,
  cost_usd DOUBLE,
  source_file TEXT NOT NULL,
  source_line BIGINT NOT NULL,
  PRIMARY KEY (harness, record_id)
)`,
		`
CREATE TABLE IF NOT EXISTS ingest_checkpoints (
  source TEXT NOT NULL,
  path TEXT NOT NULL,
  size BIGINT NOT NULL,
  mtime_unix_ms BIGINT NOT NULL,
  head_fingerprint TEXT NOT NULL,
  offset_bytes BIGINT NOT NULL,
  line_no BIGINT NOT NULL,
  parser_version BIGINT NOT NULL,
  state_json TEXT,
  turns_ingested BIGINT NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (source, path)
)`,
	},
	// v2: reasoning effort / thinking level as a first-class nullable dimension.
	// NULL means the source did not persist one for that call; it is never
	// inferred, so NULL and "off" are different answers and stay different.
	{
		`ALTER TABLE agent_turns ADD COLUMN reasoning_effort TEXT`,
	},
	// v3: Claude attribution split into claude-code / claude-desktop.
	//
	// Rows collected before the split were all stamped harness='claude-desktop'
	// even when the transcript's own entrypoint said "cli". They are dropped
	// rather than rewritten: the harness is half of the primary key, so an
	// in-place update would collide with the correctly attributed row the next
	// scan produces, and every dropped row is re-derived from the session files
	// that are still the only source of truth. Renaming the checkpoints keeps
	// that rescan incremental instead of re-reading every byte twice.
	{
		`DELETE FROM agent_turns WHERE harness = 'claude-desktop'`,
		`UPDATE ingest_checkpoints SET source = 'claude' WHERE source = 'claude-desktop'`,
	},
}

func (d *DB) migrate() error {
	if _, err := d.sql.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	var current int
	row := d.sql.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&current); err != nil {
		return err
	}
	if current > len(migrations) {
		return fmt.Errorf("database at %s has schema version %d, this binary only knows %d", d.path, current, SchemaVersion)
	}
	for v := current; v < len(migrations); v++ {
		tx, err := d.sql.Begin()
		if err != nil {
			return err
		}
		for _, statement := range migrations[v] {
			if _, err := tx.Exec(statement); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %d: %w", v+1, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_version(version, applied_at) VALUES (?, ?)`, v+1, time.Now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) SchemaVersion() (int, error) {
	var v int
	err := d.sql.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v)
	return v, err
}

// insertColumns is the persisted column order. Nothing here is conversation
// content: no prompts, responses, tool arguments, tool output or raw session
// JSON ever reaches the store.
var insertColumns = []string{
	"ts", "ts_unix", "harness", "client", "provider", "model", "reasoning_effort", "project", "cwd",
	"session_id", "record_id", "turn_id", "event_kind",
	"input_tokens", "cache_read_tokens", "cache_write_tokens", "prompt_tokens",
	"output_tokens", "reasoning_tokens", "native_input_tokens", "native_total_tokens",
	"usage_semantics", "usage_basis", "tool_calls", "tool_errors", "cost_usd",
	"source_file", "source_line",
}

// insertRowsPerStatement is how many turns share one INSERT.
//
// Both extremes are slow for different reasons, and the curve is not intuitive.
// A statement per row pays DuckDB's per-statement overhead 33k times; a very
// wide multi-row VALUES pays a parse cost that grows faster than linearly in
// the number of placeholders. Measured on this machine's 33k-row Codex corpus:
//
//	rows/statement    1     10    20    40    64    200    1000
//	seconds         ~300    82    56    52    51     68     140
//
// The floor is broad between 40 and 80, so this sits in the middle of it rather
// than on an edge that a different corpus might move.
const insertRowsPerStatement = 64

// insertConflictClause keeps re-ingestion idempotent.
//
// On conflict the earliest observation wins. A harness that replays historical
// usage into a resumed session stamps the replay with the resume time; keeping
// the earliest observation makes the stored time series independent of the
// order files happened to be scanned in.
//
// The gate is <= rather than <, which is what makes a parser upgrade land: the
// same file re-parsed by a newer binary produces the same timestamp, so its
// normalized attribution (a newly extracted dimension, a corrected model name)
// refreshes in place. A later replay of the same event is still ignored.
const insertConflictClause = `
ON CONFLICT (harness, record_id) DO UPDATE SET
  ts = excluded.ts,
  ts_unix = excluded.ts_unix,
  client = excluded.client,
  provider = excluded.provider,
  model = excluded.model,
  reasoning_effort = excluded.reasoning_effort,
  project = excluded.project,
  cwd = excluded.cwd,
  event_kind = excluded.event_kind,
  tool_calls = excluded.tool_calls,
  tool_errors = excluded.tool_errors,
  source_file = excluded.source_file,
  source_line = excluded.source_line
WHERE excluded.ts_unix <= agent_turns.ts_unix`

func insertStatement(rows int) string {
	placeholders := "(" + strings.TrimSuffix(strings.Repeat("?,", len(insertColumns)), ",") + ")"
	values := make([]string, rows)
	for i := range values {
		values[i] = placeholders
	}
	return "INSERT INTO agent_turns (" + strings.Join(insertColumns, ", ") + ") VALUES " +
		strings.Join(values, ",") + insertConflictClause
}

func turnArgs(t model.Turn) []any {
	return []any{
		t.Timestamp.UTC().Format(time.RFC3339Nano), t.Timestamp.UTC().Unix(),
		t.Harness, nullable(t.Client), nullable(t.Provider), nullable(t.Model), nullable(t.ReasoningEffort), nullable(t.Project), nullable(t.CWD),
		t.SessionID, t.RecordID, nullable(t.TurnID), t.EventKind,
		t.Usage.InputTokens, t.Usage.CacheReadTokens, t.Usage.CacheWriteTokens, t.Usage.PromptTokens,
		t.Usage.OutputTokens, t.Usage.ReasoningTokens, t.Usage.NativeInputTokens, t.Usage.NativeTotalTokens,
		string(t.Usage.Semantics), string(t.Usage.Basis), t.ToolCalls, t.ToolErrors, t.CostUSD,
		t.SourceFile, t.SourceLine,
	}
}

// dedupeBatch collapses repeated identities inside one batch, keeping the
// earliest timestamp. Two reasons: it is the same rule the conflict clause
// applies, and a multi-row INSERT cannot resolve a conflict against a row the
// same statement is inserting.
func dedupeBatch(turns []model.Turn) []model.Turn {
	seen := make(map[string]int, len(turns))
	out := make([]model.Turn, 0, len(turns))
	for _, t := range turns {
		key := t.Harness + "\x00" + t.RecordID
		if i, dup := seen[key]; dup {
			if t.Timestamp.Before(out[i].Timestamp) {
				out[i] = t
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, t)
	}
	return out
}

// InsertTurns writes a batch in one transaction and returns how many rows were
// new. Re-ingesting the same evidence is a no-op, which is what makes repeated
// scans and interrupted runs safe.
func (d *DB) InsertTurns(ctx context.Context, turns []model.Turn) (int, error) {
	return d.CommitFile(ctx, turns, nil)
}

// CommitFile stores one file's turns and its checkpoint in a single
// transaction, returning how many rows were new. Atomicity is the point: a
// checkpoint that outlived a rolled-back insert would skip evidence forever,
// and one commit per file instead of two roughly halves the cost of building a
// store from scratch. Pass a nil checkpoint to write turns only.
func (d *DB) CommitFile(ctx context.Context, turns []model.Turn, cp *Checkpoint) (int, error) {
	turns = dedupeBatch(turns)
	if len(turns) == 0 && cp == nil {
		return 0, nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var before, after int64
	if len(turns) > 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_turns`).Scan(&before); err != nil {
			return 0, err
		}
		for start := 0; start < len(turns); start += insertRowsPerStatement {
			end := start + insertRowsPerStatement
			if end > len(turns) {
				end = len(turns)
			}
			chunk := turns[start:end]
			args := make([]any, 0, len(chunk)*len(insertColumns))
			for _, t := range chunk {
				args = append(args, turnArgs(t)...)
			}
			if _, err := tx.ExecContext(ctx, insertStatement(len(chunk)), args...); err != nil {
				return 0, err
			}
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_turns`).Scan(&after); err != nil {
			return 0, err
		}
	}
	if cp != nil {
		if err := saveCheckpointTx(ctx, tx, *cp); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(after - before), nil
}

// Checkpoint records where a scan stopped inside one session file.
type Checkpoint struct {
	Source          string
	Path            string
	Size            int64
	MTimeUnixMs     int64
	HeadFingerprint string
	State           source.FileState
	ParserVersion   int
	TurnsIngested   int64
	UpdatedAt       time.Time
}

func (d *DB) Checkpoint(ctx context.Context, sourceName, path string) (Checkpoint, bool, error) {
	var (
		cp        Checkpoint
		stateJSON sql.NullString
		updated   string
	)
	row := d.sql.QueryRowContext(ctx,
		`SELECT size, mtime_unix_ms, head_fingerprint, offset_bytes, line_no, parser_version, state_json, turns_ingested, updated_at
		 FROM ingest_checkpoints WHERE source = ? AND path = ?`, sourceName, path)
	err := row.Scan(&cp.Size, &cp.MTimeUnixMs, &cp.HeadFingerprint, &cp.State.Offset, &cp.State.Line,
		&cp.ParserVersion, &stateJSON, &cp.TurnsIngested, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, false, nil
	}
	if err != nil {
		return Checkpoint{}, false, err
	}
	cp.Source, cp.Path = sourceName, path
	if stateJSON.Valid && stateJSON.String != "" {
		cp.State.Meta = json.RawMessage(stateJSON.String)
	}
	cp.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return cp, true, nil
}

func (d *DB) SaveCheckpoint(ctx context.Context, cp Checkpoint) error {
	return saveCheckpointTx(ctx, d.sql, cp)
}

// execer is satisfied by both *sql.DB and *sql.Tx, so a checkpoint can be
// written standalone or inside the transaction that stores its turns.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func saveCheckpointTx(ctx context.Context, db execer, cp Checkpoint) error {
	var stateJSON any
	if len(cp.State.Meta) > 0 {
		stateJSON = string(cp.State.Meta)
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO ingest_checkpoints (source, path, size, mtime_unix_ms, head_fingerprint, offset_bytes, line_no, parser_version, state_json, turns_ingested, updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT (source, path) DO UPDATE SET
  size = excluded.size,
  mtime_unix_ms = excluded.mtime_unix_ms,
  head_fingerprint = excluded.head_fingerprint,
  offset_bytes = excluded.offset_bytes,
  line_no = excluded.line_no,
  parser_version = excluded.parser_version,
  state_json = excluded.state_json,
  turns_ingested = excluded.turns_ingested,
  updated_at = excluded.updated_at`,
		cp.Source, cp.Path, cp.Size, cp.MTimeUnixMs, cp.HeadFingerprint,
		cp.State.Offset, cp.State.Line, cp.ParserVersion, stateJSON, cp.TurnsIngested,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// ResetSource drops the checkpoints for one source so a parser upgrade can
// rebuild only the affected evidence. Stored turns are left alone; re-ingestion
// is idempotent and will re-add anything that is missing.
func (d *DB) ResetSource(ctx context.Context, sourceName string) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM ingest_checkpoints WHERE source = ?`, sourceName)
	return err
}

// CheckpointStats summarizes local ingestion state for the doctor command.
type CheckpointStats struct {
	Files          int64
	Turns          int64
	NewestActivity time.Time
}

func (d *DB) CheckpointStats(ctx context.Context, sourceName string) (CheckpointStats, error) {
	var (
		st      CheckpointStats
		updated sql.NullString
	)
	err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(turns_ingested), 0), MAX(updated_at) FROM ingest_checkpoints WHERE source = ?`,
		sourceName).Scan(&st.Files, &st.Turns, &updated)
	if err != nil {
		return st, err
	}
	if updated.Valid {
		st.NewestActivity, _ = time.Parse(time.RFC3339Nano, updated.String)
	}
	return st, nil
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
