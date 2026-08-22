package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/source"
	"github.com/billyham07/harness-observer/internal/source/codex"
	"github.com/billyham07/harness-observer/internal/store"
)

const sessionHeader = `{"type":"session_meta","timestamp":"2026-08-20T09:59:00Z","payload":{"id":"s1","cwd":"/work/demo","originator":"Codex Desktop"}}` + "\n"

func tokenEvent(ts string, input, output int) string {
	return `{"type":"event_msg","timestamp":"` + ts + `","payload":{"type":"token_count","info":{"model":"gpt-5.6","last_token_usage":{"input_tokens":` +
		itoa(input) + `,"output_tokens":` + itoa(output) + `,"total_tokens":` + itoa(input+output) + `}}}}` + "\n"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}

func setup(t *testing.T) (*store.DB, string, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "rollout-s1.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	return db, root, path
}

func run(t *testing.T, db *store.DB, root string) Stats {
	t.Helper()
	stats, err := Ingestor{DB: db, Sources: []source.FileSource{codex.New(root)}}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected one source result, got %d", len(stats))
	}
	return stats[0]
}

func storedRows(t *testing.T, db *store.DB) int {
	t.Helper()
	var n int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM agent_turns`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAppendOnlySessionIsNotRescanned(t *testing.T) {
	db, root, path := setup(t)
	if err := os.WriteFile(path, []byte(sessionHeader+tokenEvent("2026-08-20T10:00:00Z", 100, 10)), 0o644); err != nil {
		t.Fatal(err)
	}

	first := run(t, db, root)
	if first.FilesParsed != 1 || first.TurnsIngested != 1 {
		t.Fatalf("first run: %+v", first)
	}

	// Nothing changed: the file must not be read again at all.
	second := run(t, db, root)
	if second.FilesSkipped != 1 || second.FilesParsed != 0 || second.TurnsParsed != 0 {
		t.Fatalf("unchanged file was re-read: %+v", second)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(tokenEvent("2026-08-20T10:05:00Z", 200, 20)); err != nil {
		t.Fatal(err)
	}
	f.Close()

	third := run(t, db, root)
	if third.FilesParsed != 1 {
		t.Fatalf("appended file was not re-visited: %+v", third)
	}
	if third.TurnsParsed != 1 {
		t.Fatalf("resume parsed %d turns, want only the appended one", third.TurnsParsed)
	}
	if third.TurnsIngested != 1 {
		t.Fatalf("appended run stored %d new rows, want 1", third.TurnsIngested)
	}
	if got := storedRows(t, db); got != 2 {
		t.Fatalf("stored %d rows total, want 2", got)
	}
}

// Truncation or in-place rewriting invalidates the offset. The head
// fingerprint catches it and the file is re-read from byte zero.
func TestTruncatedSessionIsRewound(t *testing.T) {
	db, root, path := setup(t)
	if err := os.WriteFile(path, []byte(sessionHeader+tokenEvent("2026-08-20T10:00:00Z", 100, 10)), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, db, root)

	replacement := `{"type":"session_meta","timestamp":"2026-08-20T11:00:00Z","payload":{"id":"s2","cwd":"/work/other","originator":"Codex Desktop"}}` + "\n" +
		tokenEvent("2026-08-20T11:00:01Z", 300, 30)
	if err := os.WriteFile(path, []byte(replacement), 0o644); err != nil {
		t.Fatal(err)
	}

	stats := run(t, db, root)
	if stats.FilesRewound != 1 || stats.FilesParsed != 1 {
		t.Fatalf("replaced file was not rewound: %+v", stats)
	}
	if stats.TurnsIngested != 1 {
		t.Fatalf("rewound run stored %d new rows, want 1", stats.TurnsIngested)
	}
	if got := storedRows(t, db); got != 2 {
		t.Fatalf("stored %d rows, want 2 (old evidence is kept, new evidence added)", got)
	}
}

// A record that is still being written must not be ingested as a fragment, and
// must be ingested exactly once after it is complete.
func TestPartiallyWrittenLineIsIngestedOnceWhenComplete(t *testing.T) {
	db, root, path := setup(t)
	complete := tokenEvent("2026-08-20T10:00:00Z", 100, 10)
	partial := complete[:len(complete)/2]
	if err := os.WriteFile(path, []byte(sessionHeader+partial), 0o644); err != nil {
		t.Fatal(err)
	}
	if stats := run(t, db, root); stats.TurnsIngested != 0 {
		t.Fatalf("partial record was ingested: %+v", stats)
	}

	if err := os.WriteFile(path, []byte(sessionHeader+complete), 0o644); err != nil {
		t.Fatal(err)
	}
	if stats := run(t, db, root); stats.TurnsIngested != 1 {
		t.Fatalf("completed record was not ingested: %+v", stats)
	}
	if got := storedRows(t, db); got != 1 {
		t.Fatalf("stored %d rows, want exactly 1", got)
	}
}

func TestParserVersionBumpRewindsCheckpoints(t *testing.T) {
	db, root, path := setup(t)
	if err := os.WriteFile(path, []byte(sessionHeader+tokenEvent("2026-08-20T10:00:00Z", 100, 10)), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, db, root)

	// Simulate a database written by a binary with an older parser.
	if _, err := db.SQL().Exec(`UPDATE ingest_checkpoints SET parser_version = ?`, ParserVersion-1); err != nil {
		t.Fatal(err)
	}
	stats := run(t, db, root)
	if stats.FilesRewound != 1 || stats.FilesParsed != 1 {
		t.Fatalf("stale parser version did not force a re-read: %+v", stats)
	}
	if got := storedRows(t, db); got != 1 {
		t.Fatalf("re-read duplicated evidence: %d rows", got)
	}
}
