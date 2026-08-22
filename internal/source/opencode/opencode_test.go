package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
	_ "modernc.org/sqlite"
)

func TestRealLocalOpenCodeIfPresent(t *testing.T) {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".local", "share", "opencode")
	if _, err := os.Stat(filepath.Join(root, "opencode.db")); err != nil {
		t.Skip("no local OpenCode DB")
	}
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("local OpenCode DB parsed to zero turns")
	}
	for _, r := range rows {
		if r.Harness != "opencode" {
			t.Fatalf("harness %s", r.Harness)
		}
		if r.Usage.Semantics != model.UsageSplitCache {
			t.Fatalf("semantics %+v", r.Usage)
		}
		if r.RecordID == "" || r.SessionID == "" {
			t.Fatalf("identity %+v", r)
		}
	}
}

func TestOpenCodeDataDirOverride(t *testing.T) {
	t.Setenv("OPENCODE_DATA_DIR", `C:\custom\opencode-data`)
	dirs := defaultDataDirs()
	if len(dirs) != 1 || dirs[0] != filepath.Clean(`C:\custom\opencode-data`) {
		t.Fatalf("OPENCODE_DATA_DIR should win: %v", dirs)
	}
}

func TestOpenCodeAbsentInstallIsHarmless(t *testing.T) {
	rows, err := New(filepath.Join(t.TempDir(), "missing")).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want 0", len(rows))
	}
}

func TestOpenCodeFileStorageNormalizesUsageAndParts(t *testing.T) {
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "storage", "session", "proj", "ses_root.json"), map[string]any{
		"id": "ses_root", "directory": `C:\work\demo`, "title": "root",
	})
	writeJSON(t, filepath.Join(root, "storage", "message", "ses_root", "msg_a.json"), map[string]any{
		"id": "msg_a", "sessionID": "ses_root", "role": "assistant",
		"modelID": "claude-opus-4-1", "providerID": "anthropic", "cost": 0.02,
		"tokens": map[string]any{"total": 205, "input": 100, "output": 20, "reasoning": 5, "cache": map[string]any{"read": 80, "write": 5}},
		"time":   map[string]any{"created": 1780000000000, "completed": 1780000001000},
	})
	writeJSON(t, filepath.Join(root, "storage", "part", "msg_a", "prt_b.json"), map[string]any{
		"id": "prt_b", "type": "tool", "tool": "bash", "state": map[string]any{"status": "error"},
	})
	writeJSON(t, filepath.Join(root, "storage", "part", "msg_a", "prt_a.json"), map[string]any{
		"id": "prt_a", "type": "text", "text": "hello",
	})
	// Empty placeholder: no tokens, no parts.
	writeJSON(t, filepath.Join(root, "storage", "message", "ses_root", "msg_empty.json"), map[string]any{
		"id": "msg_empty", "sessionID": "ses_root", "role": "assistant",
		"time": map[string]any{"created": 1780000002000},
	})
	// Zero-usage but real tool activity.
	writeJSON(t, filepath.Join(root, "storage", "message", "ses_root", "msg_zero.json"), map[string]any{
		"id": "msg_zero", "sessionID": "ses_root", "role": "assistant",
		"modelID": "claude-opus-4-1", "providerID": "anthropic",
		"time": map[string]any{"created": 1780000003000},
	})
	writeJSON(t, filepath.Join(root, "storage", "part", "msg_zero", "prt_t.json"), map[string]any{
		"id": "prt_t", "type": "tool", "tool": "read", "state": map[string]any{"status": "completed"},
	})

	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want usage turn + zero-usage tool turn", len(rows))
	}
	r := rows[0]
	if r.RecordID != "opencode:ses_root:msg_a" {
		t.Fatalf("record id: %s", r.RecordID)
	}
	if r.Harness != "opencode" || r.Provider != "anthropic" || r.Model != "claude-opus-4-1" {
		t.Fatalf("identity: %+v", r)
	}
	if r.Usage.InputTokens != 100 || r.Usage.CacheReadTokens != 80 || r.Usage.CacheWriteTokens != 5 || r.Usage.OutputTokens != 20 || r.Usage.ReasoningTokens != 5 {
		t.Fatalf("usage: %+v", r.Usage)
	}
	if r.Usage.PromptTokens != 185 || r.Usage.Semantics != model.UsageSplitCache {
		t.Fatalf("semantics: %+v", r.Usage)
	}
	if r.Usage.NativeTotalTokens == nil || *r.Usage.NativeTotalTokens != 205 {
		t.Fatalf("native total: %+v", r.Usage)
	}
	if r.ObservedTokens() != 205 {
		t.Fatalf("reasoning must not add to observed: %d", r.ObservedTokens())
	}
	if r.ToolCalls != 1 || r.ToolErrors != 1 {
		t.Fatalf("tools: calls=%d errors=%d", r.ToolCalls, r.ToolErrors)
	}
	if r.CostUSD == nil || *r.CostUSD != 0.02 {
		t.Fatalf("cost: %+v", r.CostUSD)
	}
	if r.Project != "demo" {
		t.Fatalf("project: %s", r.Project)
	}
	zero := rows[1]
	if zero.RecordID != "opencode:ses_root:msg_zero" || zero.ToolCalls != 1 {
		t.Fatalf("zero-usage tool turn: %+v", zero)
	}
}

func TestOpenCodeChildSessionDoesNotDoubleCount(t *testing.T) {
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "storage", "session", "proj", "ses_root.json"), map[string]any{
		"id": "ses_root", "directory": "/work/demo",
	})
	writeJSON(t, filepath.Join(root, "storage", "session", "proj", "ses_child.json"), map[string]any{
		"id": "ses_child", "parentID": "ses_root", "directory": "/work/demo",
	})
	writeJSON(t, filepath.Join(root, "storage", "message", "ses_root", "msg_r.json"), map[string]any{
		"id": "msg_r", "sessionID": "ses_root", "role": "assistant",
		"tokens": map[string]any{"input": 10, "output": 2},
		"time":   map[string]any{"created": 1780000000000},
	})
	writeJSON(t, filepath.Join(root, "storage", "message", "ses_child", "msg_c.json"), map[string]any{
		"id": "msg_c", "sessionID": "ses_child", "role": "assistant",
		"tokens": map[string]any{"input": 7, "output": 1},
		"time":   map[string]any{"created": 1780000001000},
	})
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	ids := map[string]string{}
	var input int64
	for _, r := range rows {
		ids[r.RecordID] = r.SessionID
		input += r.Usage.InputTokens
		if r.SessionID != "ses_root" {
			t.Fatalf("child usage must attribute to root session, got %s", r.SessionID)
		}
	}
	if input != 17 {
		t.Fatalf("tokens double-counted or dropped: %d", input)
	}
	if _, ok := ids["opencode:ses_child:msg_c"]; !ok {
		t.Fatalf("child message identity missing: %v", ids)
	}
}

func TestOpenCodeSQLiteAndIncompatibleSchema(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "opencode.db")
	createOpenCodeDB(t, dbPath, true)
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d sqlite rows", len(rows))
	}
	if rows[0].RecordID != "opencode:ses_sql:msg_sql" {
		t.Fatalf("id %s", rows[0].RecordID)
	}
	if rows[0].Usage.InputTokens != 40 || rows[0].Usage.CacheReadTokens != 10 {
		t.Fatalf("sqlite usage %+v", rows[0].Usage)
	}

	bad := filepath.Join(t.TempDir(), "opencode.db")
	createOpenCodeDB(t, bad, false)
	_, _, parseErr := New(filepath.Dir(bad)).ParseFile(bad, source.FileState{}, source.Window{})
	if parseErr == nil {
		t.Fatal("incompatible schema must error instead of returning zero turns")
	}
	if want := "missing tables"; !contains(parseErr.Error(), want) {
		t.Fatalf("error %v should mention %q", parseErr, want)
	}
}

func TestOpenCodeSQLiteIncrementalCursor(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "opencode.db")
	createOpenCodeDB(t, dbPath, true)
	src := New(root)
	if err := src.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, next, err := src.ParseFile(dbPath, source.FileState{}, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("first pass %d", len(first))
	}
	second, _, err := src.ParseFile(dbPath, next, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("cursor should skip already-seen messages, got %d", len(second))
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func createOpenCodeDB(t *testing.T, path string, withRequiredTables bool) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE session (
		id TEXT PRIMARY KEY, parent_id TEXT, directory TEXT, title TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if !withRequiredTables {
		return
	}
	if _, err := db.Exec(`CREATE TABLE message (
		id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, data TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE part (
		id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, data TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session(id, directory, title) VALUES ('ses_sql', '/work/sql', 'sql')`); err != nil {
		t.Fatal(err)
	}
	msg, _ := json.Marshal(map[string]any{
		"role": "assistant", "modelID": "gpt-5", "providerID": "openai",
		"tokens": map[string]any{"input": 40, "output": 4, "cache": map[string]any{"read": 10, "write": 0}},
		"time":   map[string]any{"created": 1780000000000},
	})
	if _, err := db.Exec(`INSERT INTO message(id, session_id, time_created, data) VALUES ('msg_sql', 'ses_sql', 1780000000000, ?)`, string(msg)); err != nil {
		t.Fatal(err)
	}
	part, _ := json.Marshal(map[string]any{"type": "text", "text": "ok"})
	if _, err := db.Exec(`INSERT INTO part(id, message_id, session_id, data) VALUES ('prt_sql', 'msg_sql', 'ses_sql', ?)`, string(part)); err != nil {
		t.Fatal(err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()))
}
