package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/source"
)

func writeSession(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Minimized from real ~/.codex/sessions rollouts: forking a conversation writes
// a second rollout file that shares the session_id and replays the earlier
// token_count events with a NEW timestamp at a DIFFERENT line number. Counting
// the replay again would inflate every historical session.
func TestCodexForkReplayIsNotDoubleCounted(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions", "2026", "08", "13")

	original := `{"type":"session_meta","timestamp":"2026-08-13T01:09:40Z","payload":{"id":"019ff8aa","cwd":"/work/demo","originator":"Codex Desktop"}}
{"type":"response_item","timestamp":"2026-08-13T01:10:00Z","payload":{"type":"function_call","name":"shell"}}
{"type":"event_msg","timestamp":"2026-08-13T01:11:15Z","payload":{"type":"token_count","info":{"model":"gpt-5.6","last_token_usage":{"input_tokens":27468,"cached_input_tokens":11008,"output_tokens":258,"total_tokens":27726}}}}
{"type":"event_msg","timestamp":"2026-08-13T01:11:26Z","payload":{"type":"token_count","info":{"model":"gpt-5.6","last_token_usage":{"input_tokens":28231,"cached_input_tokens":26368,"output_tokens":382,"total_tokens":28613}}}}
`
	// The fork replays both events at earlier line numbers with the resume
	// timestamp, then adds one genuinely new event.
	fork := `{"type":"session_meta","timestamp":"2026-08-13T01:48:40Z","payload":{"id":"019ff8aa","cwd":"/work/demo","originator":"Codex Desktop"}}
{"type":"event_msg","timestamp":"2026-08-13T01:48:40Z","payload":{"type":"token_count","info":{"model":"gpt-5.6","last_token_usage":{"input_tokens":27468,"cached_input_tokens":11008,"output_tokens":258,"total_tokens":27726}}}}
{"type":"event_msg","timestamp":"2026-08-13T01:48:40Z","payload":{"type":"token_count","info":{"model":"gpt-5.6","last_token_usage":{"input_tokens":28231,"cached_input_tokens":26368,"output_tokens":382,"total_tokens":28613}}}}
{"type":"event_msg","timestamp":"2026-08-13T01:49:10Z","payload":{"type":"token_count","info":{"model":"gpt-5.6","last_token_usage":{"input_tokens":30000,"cached_input_tokens":28000,"output_tokens":500,"total_tokens":30500}}}}
`
	writeSession(t, filepath.Join(dir, "rollout-2026-08-13T09-09-40-019ff8aa.jsonl"), original)
	writeSession(t, filepath.Join(dir, "rollout-2026-08-13T09-48-39-019ff8ce.jsonl"), fork)

	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.RecordID)
		}
		t.Fatalf("got %d rows, want 3 (2 original + 1 new): %v", len(rows), ids)
	}
	var total int64
	for _, r := range rows {
		total += r.Usage.PromptTokens
	}
	if want := int64(27468 + 28231 + 30000); total != want {
		t.Fatalf("prompt tokens = %d, want %d", total, want)
	}
}

// Two forks that diverge at the same position produce different usage numbers,
// and both are real evidence. The ordinal alone would collapse them.
func TestCodexDivergentForksBothCount(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions")
	header := `{"type":"session_meta","timestamp":"2026-08-13T01:09:40Z","payload":{"id":"s1","cwd":"/work/demo"}}` + "\n"
	writeSession(t, filepath.Join(dir, "rollout-a.jsonl"), header+
		`{"type":"event_msg","timestamp":"2026-08-13T01:11:15Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"output_tokens":10,"total_tokens":110}}}}`+"\n")
	writeSession(t, filepath.Join(dir, "rollout-b.jsonl"), header+
		`{"type":"event_msg","timestamp":"2026-08-13T01:12:15Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":222,"output_tokens":22,"total_tokens":244}}}}`+"\n")

	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
}

// Codex token_count reports cache_write_input_tokens. Real rollouts keep
// total_tokens == input_tokens + output_tokens, so the write bucket is carved
// out of the native input counter rather than added on top of it.
func TestCodexCacheWriteIsCarvedOutOfNativeInput(t *testing.T) {
	root := t.TempDir()
	writeSession(t, filepath.Join(root, "sessions", "rollout-s1.jsonl"),
		`{"type":"session_meta","timestamp":"2026-08-20T09:59:00Z","payload":{"id":"s1","cwd":"/work/demo"}}`+"\n"+
			`{"type":"event_msg","timestamp":"2026-08-20T10:00:01Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":1000,"cached_input_tokens":700,"cache_write_input_tokens":200,"output_tokens":50,"total_tokens":1050}}}}`+"\n")

	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	u := rows[0].Usage
	if u.InputTokens != 100 || u.CacheReadTokens != 700 || u.CacheWriteTokens != 200 || u.PromptTokens != 1000 {
		t.Fatalf("unexpected usage: %+v", u)
	}
}

// An append-only session must resume from its checkpoint: the tail is parsed,
// the prefix is not re-read, and the session identity declared on line 1
// survives because it is carried in the checkpoint state.
func TestCodexIncrementalResumeKeepsSessionIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "rollout-s1.jsonl")
	writeSession(t, path,
		`{"type":"session_meta","timestamp":"2026-08-20T09:59:00Z","payload":{"id":"s1","cwd":"/work/demo","originator":"Codex Desktop"}}`+"\n"+
			`{"type":"event_msg","timestamp":"2026-08-20T10:00:01Z","payload":{"type":"token_count","info":{"model":"gpt-5.6","last_token_usage":{"input_tokens":100,"output_tokens":10,"total_tokens":110}}}}`+"\n")

	s := New(root)
	first, state, err := s.ParseFile(path, source.FileState{}, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("got %d rows on first pass, want 1", len(first))
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"event_msg","timestamp":"2026-08-20T10:05:01Z","payload":{"type":"token_count","info":{"model":"gpt-5.6","last_token_usage":{"input_tokens":200,"output_tokens":20,"total_tokens":220}}}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	second, _, err := s.ParseFile(path, state, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 {
		t.Fatalf("resume returned %d rows, want only the appended one", len(second))
	}
	r := second[0]
	if r.SessionID != "s1" || r.Model != "gpt-5.6" || r.Harness != "codex-desktop" || r.CWD != "/work/demo" {
		t.Fatalf("resumed row lost identity carried by the checkpoint: %+v", r)
	}
	if r.RecordID == first[0].RecordID {
		t.Fatalf("appended event reused the first record id: %s", r.RecordID)
	}
}
