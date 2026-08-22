package pi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/source"
)

func writeOMPSession(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Pi/OMP record the thinking level as its own event, ahead of the usage it
// applies to, and only when it changes. The level therefore has to be carried
// forward across every later usage event until the next change.
func TestThinkingLevelChangeAppliesUntilChanged(t *testing.T) {
	root := t.TempDir()
	writeOMPSession(t, root, "s1.jsonl", `{"type":"session","version":3,"id":"s1","timestamp":"2026-08-20T09:00:00Z","cwd":"C:/work/demo"}
{"type":"model_change","id":"mc1","timestamp":"2026-08-20T09:00:01Z","provider":"anthropic","modelId":"claude-opus-5"}
{"type":"thinking_level_change","id":"tl1","timestamp":"2026-08-20T09:00:02Z","thinkingLevel":"high","configured":"high"}
{"type":"message","id":"m1","timestamp":"2026-08-20T09:00:10Z","message":{"role":"assistant","responseId":"r1","usage":{"input":10,"output":2}}}
{"type":"message","id":"m2","timestamp":"2026-08-20T09:00:20Z","message":{"role":"assistant","responseId":"r2","usage":{"input":11,"output":3}}}
{"type":"thinking_level_change","id":"tl2","timestamp":"2026-08-20T09:01:00Z","thinkingLevel":"max","configured":"auto"}
{"type":"message","id":"m3","timestamp":"2026-08-20T09:01:10Z","message":{"role":"assistant","responseId":"r3","usage":{"input":12,"output":4}}}
`)
	rows, err := New("omp", root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	want := []string{"high", "high", "max"}
	for i, r := range rows {
		if r.ReasoningEffort != want[i] {
			t.Fatalf("row %d effort = %q, want %q", i, r.ReasoningEffort, want[i])
		}
	}
}

// "off" is a real observed level and must stay representable: a model running
// with thinking disabled is a different comparison bucket from a model whose
// level was never recorded.
func TestThinkingLevelOffIsAValueAndNullClearsIt(t *testing.T) {
	root := t.TempDir()
	writeOMPSession(t, root, "s2.jsonl", `{"type":"session","version":3,"id":"s2","timestamp":"2026-08-20T09:00:00Z","cwd":"C:/work/demo"}
{"type":"thinking_level_change","id":"tl1","timestamp":"2026-08-20T09:00:02Z","thinkingLevel":"off","configured":"off"}
{"type":"message","id":"m1","timestamp":"2026-08-20T09:00:10Z","message":{"role":"assistant","responseId":"r1","usage":{"input":10,"output":2}}}
{"type":"thinking_level_change","id":"tl2","timestamp":"2026-08-20T09:00:30Z","thinkingLevel":null,"configured":null}
{"type":"message","id":"m2","timestamp":"2026-08-20T09:00:40Z","message":{"role":"assistant","responseId":"r2","usage":{"input":11,"output":3}}}
`)
	rows, err := New("omp", root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].ReasoningEffort != "off" {
		t.Fatalf("row 0 effort = %q, want off", rows[0].ReasoningEffort)
	}
	if rows[1].ReasoningEffort != "" {
		t.Fatalf("explicit null must clear the level, got %q", rows[1].ReasoningEffort)
	}
}

// Five of the local OMP/Pi sessions never emit thinking_level_change at all.
// Those turns stay unknown; nothing is inferred from reasoning-token volume.
func TestSessionWithoutThinkingLevelStaysUnknown(t *testing.T) {
	root := t.TempDir()
	writeOMPSession(t, root, "s3.jsonl", `{"type":"session","version":3,"id":"s3","timestamp":"2026-08-20T09:00:00Z","cwd":"C:/work/demo"}
{"type":"message","id":"m1","timestamp":"2026-08-20T09:00:10Z","message":{"role":"assistant","responseId":"r1","usage":{"input":10,"output":2,"reasoning":5000}}}
`)
	rows, err := New("omp", root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ReasoningEffort != "" {
		t.Fatalf("effort invented without evidence: %+v", rows)
	}
}

// The level is declared near the top of a session file; an incremental resume
// starts past it and must restore it from the checkpoint state.
func TestThinkingLevelSurvivesIncrementalResume(t *testing.T) {
	root := t.TempDir()
	head := `{"type":"session","version":3,"id":"s4","timestamp":"2026-08-20T09:00:00Z","cwd":"C:/work/demo"}
{"type":"thinking_level_change","id":"tl1","timestamp":"2026-08-20T09:00:02Z","thinkingLevel":"xhigh"}
{"type":"message","id":"m1","timestamp":"2026-08-20T09:00:10Z","message":{"role":"assistant","responseId":"r1","usage":{"input":10,"output":2}}}
`
	path := writeOMPSession(t, root, "s4.jsonl", head)
	s := New("omp", root)
	first, state, err := s.ParseFile(path, source.FileState{}, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ReasoningEffort != "xhigh" {
		t.Fatalf("first pass: %+v", first)
	}
	var carried parseState
	if err := json.Unmarshal(state.Meta, &carried); err != nil {
		t.Fatal(err)
	}
	if carried.Effort != "xhigh" {
		t.Fatalf("checkpoint state lost the thinking level: %+v", carried)
	}

	writeOMPSession(t, root, "s4.jsonl", head+`{"type":"message","id":"m2","timestamp":"2026-08-20T09:10:00Z","message":{"role":"assistant","responseId":"r2","usage":{"input":20,"output":4}}}
`)
	resumed, _, err := s.ParseFile(path, state, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed) != 1 {
		t.Fatalf("resume parsed %d turns, want 1", len(resumed))
	}
	if resumed[0].ReasoningEffort != "xhigh" {
		t.Fatalf("resumed turn effort = %q, want xhigh", resumed[0].ReasoningEffort)
	}
}
