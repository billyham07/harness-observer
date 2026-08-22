package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

func TestCodexDesktopTurnNormalizesInclusiveCachedInput(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions", "2026", "08", "20")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session_meta","timestamp":"2026-08-20T09:59:00Z","payload":{"id":"s1","cwd":"C:/work/demo","model_provider":"openai","originator":"Codex Desktop"}}
{"type":"response_item","timestamp":"2026-08-20T10:00:00Z","payload":{"type":"function_call","name":"exec_command"}}
{"type":"event_msg","timestamp":"2026-08-20T10:00:01Z","payload":{"type":"token_count","turn_id":"t1","info":{"model":"gpt-5.6","last_token_usage":{"input_tokens":1000,"cached_input_tokens":900,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1140}}}}
`
	if err := os.WriteFile(filepath.Join(dir, "rollout-s1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Harness != "codex-desktop" || r.Client != "Codex Desktop" {
		t.Fatalf("unexpected client attribution: %+v", r)
	}
	if r.Model != "gpt-5.6" || r.ToolCalls != 1 || r.Project != "demo" {
		t.Fatalf("unexpected row: %+v", r)
	}
	if r.Usage.InputTokens != 100 || r.Usage.CacheReadTokens != 900 || r.Usage.PromptTokens != 1000 {
		t.Fatalf("unexpected normalized usage: %+v", r.Usage)
	}
	if r.Usage.ReasoningTokens != 40 || r.Usage.Semantics != model.UsageInputIncludesCacheRead {
		t.Fatalf("unexpected usage semantics: %+v", r.Usage)
	}
	if r.Usage.NativeInputTokens == nil || *r.Usage.NativeInputTokens != 1000 {
		t.Fatalf("native input not preserved: %+v", r.Usage)
	}
}

func TestCodexFallsBackToCumulativeUsageDelta(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions", "2026", "08", "20")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session_meta","timestamp":"2026-08-20T09:59:00Z","payload":{"id":"s1","cwd":"/work/demo","originator":"codex-tui"}}
{"type":"event_msg","timestamp":"2026-08-20T10:00:01Z","payload":{"type":"token_count","turn_id":"t1","info":{"model":"gpt-5.6","total_token_usage":{"input_tokens":1000,"cached_input_tokens":900,"output_tokens":100,"reasoning_output_tokens":40,"total_tokens":1140}}}}
{"type":"event_msg","timestamp":"2026-08-20T10:01:01Z","payload":{"type":"token_count","turn_id":"t2","info":{"model":"gpt-5.6","total_token_usage":{"input_tokens":1200,"cached_input_tokens":1000,"output_tokens":130,"reasoning_output_tokens":50,"total_tokens":1380}}}}
`
	if err := os.WriteFile(filepath.Join(dir, "rollout-s1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	second := rows[1]
	if second.Harness != "codex-cli" || second.Usage.Basis != model.UsageDerivedDelta {
		t.Fatalf("unexpected fallback attribution: %+v", second)
	}
	if second.Usage.InputTokens != 100 || second.Usage.CacheReadTokens != 100 || second.Usage.OutputTokens != 30 || second.Usage.ReasoningTokens != 10 {
		t.Fatalf("bad cumulative delta: %+v", second.Usage)
	}
}

func TestCodexDoesNotDoubleCountArchivedCopy(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions", "2026", "08", "20")
	archive := filepath.Join(root, "archived_sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session_meta","timestamp":"2026-08-20T09:59:00Z","payload":{"id":"s1","cwd":"/work/demo"}}
{"type":"event_msg","timestamp":"2026-08-20T10:00:01Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"output_tokens":2}}}}
`
	for _, p := range []string{filepath.Join(sessions, "rollout-s1.jsonl"), filepath.Join(archive, "rollout-s1.jsonl")} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows from active+archive copy, want 1", len(rows))
	}
}
