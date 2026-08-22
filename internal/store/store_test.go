package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func turn(recordID string, ts time.Time) model.Turn {
	return model.Turn{
		Timestamp: ts,
		Harness:   "codex-desktop",
		Client:    "Codex Desktop",
		Provider:  "openai",
		Model:     "gpt-5.6",
		Project:   "demo",
		SessionID: "s1",
		RecordID:  recordID,
		EventKind: "assistant",
		Usage:     model.NewInputIncludesCacheReadUsage(1000, 100, 900, 0, 40, nil, model.UsageObserved),
	}
}

func TestMigrationsAreIdempotentAndVersioned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := db.SchemaVersion()
	if err != nil || v != SchemaVersion {
		t.Fatalf("schema version = %d (%v), want %d", v, err, SchemaVersion)
	}
	db.Close()

	again, err := Open(path)
	if err != nil {
		t.Fatalf("reopening a migrated database must succeed: %v", err)
	}
	defer again.Close()
	if v, _ := again.SchemaVersion(); v != SchemaVersion {
		t.Fatalf("schema version drifted to %d", v)
	}
}

func TestInsertTurnsIsIdempotent(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	ts := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	n, err := db.InsertTurns(ctx, []model.Turn{turn("r1", ts), turn("r2", ts)})
	if err != nil || n != 2 {
		t.Fatalf("first insert = %d (%v), want 2", n, err)
	}
	n, err = db.InsertTurns(ctx, []model.Turn{turn("r1", ts), turn("r2", ts)})
	if err != nil || n != 0 {
		t.Fatalf("re-insert = %d (%v), want 0 new rows", n, err)
	}
	var count int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM agent_turns`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("stored %d rows, want 2", count)
	}
}

// A replayed record arrives with the resume timestamp. Whichever order the
// files are scanned in, the stored row must keep the earliest observation.
func TestConflictKeepsEarliestTimestamp(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	early := time.Date(2026, 8, 13, 1, 11, 15, 0, time.UTC)
	late := time.Date(2026, 8, 13, 1, 48, 40, 0, time.UTC)

	for _, order := range [][]time.Time{{early, late}, {late, early}} {
		if _, err := db.SQL().Exec(`DELETE FROM agent_turns`); err != nil {
			t.Fatal(err)
		}
		for _, ts := range order {
			if _, err := db.InsertTurns(ctx, []model.Turn{turn("r1", ts)}); err != nil {
				t.Fatal(err)
			}
		}
		var stored int64
		if err := db.SQL().QueryRow(`SELECT ts_unix FROM agent_turns WHERE record_id = 'r1'`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != early.Unix() {
			t.Fatalf("stored ts = %d, want the earliest %d (insert order %v)", stored, early.Unix(), order)
		}
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	cp := Checkpoint{
		Source:          "codex",
		Path:            `C:\Users\demo\.codex\sessions\rollout.jsonl`,
		Size:            2048,
		MTimeUnixMs:     1786605173024,
		HeadFingerprint: "abc123",
		State: source.FileState{
			Offset: 1024,
			Line:   12,
			Meta:   json.RawMessage(`{"session_id":"s1"}`),
		},
		ParserVersion: 3,
		TurnsIngested: 7,
	}
	if err := db.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}
	got, found, err := db.Checkpoint(ctx, cp.Source, cp.Path)
	if err != nil || !found {
		t.Fatalf("checkpoint not found (%v)", err)
	}
	if got.State.Offset != 1024 || got.State.Line != 12 || string(got.State.Meta) != `{"session_id":"s1"}` {
		t.Fatalf("checkpoint state round trip failed: %+v", got.State)
	}
	if got.HeadFingerprint != "abc123" || got.ParserVersion != 3 || got.TurnsIngested != 7 {
		t.Fatalf("checkpoint metadata round trip failed: %+v", got)
	}

	cp.State.Offset = 4096
	if err := db.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}
	got, _, err = db.Checkpoint(ctx, cp.Source, cp.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.State.Offset != 4096 {
		t.Fatalf("checkpoint update did not stick: %+v", got.State)
	}

	if err := db.ResetSource(ctx, "codex"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := db.Checkpoint(ctx, cp.Source, cp.Path); found {
		t.Fatal("ResetSource must drop the source's checkpoints")
	}
}

// The schema is observability metadata only. This test is a tripwire against
// someone adding a prompt/response/tool-output column later.
func TestSchemaStoresNoConversationContent(t *testing.T) {
	db := open(t)
	rows, err := db.SQL().Query(`SELECT name FROM pragma_table_info('agent_turns')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	forbidden := map[string]bool{
		"prompt": true, "content": true, "message": true, "text": true,
		"response": true, "tool_input": true, "tool_output": true, "raw": true,
		"arguments": true, "token": true, "api_key": true,
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if forbidden[name] {
			t.Fatalf("column %q stores conversation content or credentials", name)
		}
	}
}

// A parser upgrade re-reads the same bytes and produces the same timestamp for
// an already stored event. Its refreshed normalization -- a newly extracted
// dimension, a corrected attribution -- has to land on the existing row, or the
// upgrade would only ever apply to sessions written after it.
func TestReingestingTheSameEventRefreshesNormalizedAttribution(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	ts := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	before := turn("r1", ts)
	if _, err := db.InsertTurns(ctx, []model.Turn{before}); err != nil {
		t.Fatal(err)
	}
	upgraded := turn("r1", ts)
	upgraded.ReasoningEffort = "xhigh"
	inserted, err := db.InsertTurns(ctx, []model.Turn{upgraded})
	if err != nil {
		t.Fatal(err)
	}
	if inserted != 0 {
		t.Fatalf("re-ingesting the same event added %d rows", inserted)
	}
	var effort string
	if err := db.SQL().QueryRow(`SELECT COALESCE(reasoning_effort, '') FROM agent_turns WHERE record_id = 'r1'`).Scan(&effort); err != nil {
		t.Fatal(err)
	}
	if effort != "xhigh" {
		t.Fatalf("reasoning_effort = %q, want xhigh: a parser upgrade never reached stored rows", effort)
	}
}

// A resumed session replays earlier events with the resume timestamp. That
// replay must not overwrite the original row's attribution, which is why the
// refresh above is gated on the timestamp rather than applied unconditionally.
func TestLaterReplayDoesNotOverwriteTheOriginalRow(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	original := turn("r1", time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC))
	original.ReasoningEffort = "high"
	if _, err := db.InsertTurns(ctx, []model.Turn{original}); err != nil {
		t.Fatal(err)
	}
	replay := turn("r1", time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC))
	replay.ReasoningEffort = "low"
	if _, err := db.InsertTurns(ctx, []model.Turn{replay}); err != nil {
		t.Fatal(err)
	}
	var effort string
	var tsUnix int64
	if err := db.SQL().QueryRow(`SELECT COALESCE(reasoning_effort, ''), ts_unix FROM agent_turns WHERE record_id = 'r1'`).Scan(&effort, &tsUnix); err != nil {
		t.Fatal(err)
	}
	if effort != "high" {
		t.Fatalf("a later replay rewrote the effort to %q", effort)
	}
	if tsUnix != original.Timestamp.Unix() {
		t.Fatalf("a later replay moved the timestamp to %d", tsUnix)
	}
}
