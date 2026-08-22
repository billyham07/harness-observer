package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/source"
)

func writeTranscript(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "local-agent-mode-sessions", "w", "g", "local_1", ".claude", "projects", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "local-agent-mode-sessions")
}

// Real ~/.claude/projects transcripts report thinking inside
// output_tokens_details. Dropping it loses the reasoning dimension entirely;
// adding it to the output total would double count it.
func TestClaudeThinkingTokensBecomeReasoning(t *testing.T) {
	root := writeTranscript(t, `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","cwd":"C:/work/demo","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":2,"cache_creation_input_tokens":1889,"cache_read_input_tokens":493415,"output_tokens":1736,"output_tokens_details":{"thinking_tokens":80}},"content":[]}}
`)
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	u := rows[0].Usage
	if u.ReasoningTokens != 80 {
		t.Fatalf("thinking tokens not mapped: %+v", u)
	}
	if u.OutputTokens != 1736 {
		t.Fatalf("output must stay the native counter: %+v", u)
	}
	if got := rows[0].ObservedTokens(); got != 2+1889+493415+1736 {
		t.Fatalf("observed tokens double counted reasoning: %d", got)
	}
	if rows[0].SessionID != "s1" {
		t.Fatalf("session identity should come from the transcript: %+v", rows[0])
	}
}

// Synthetic assistant messages carry an all-zero usage object. They are not
// model calls, so they must not become zero-token rows that pollute grouping.
func TestClaudeSyntheticMessagesAreNotEvidence(t *testing.T) {
	root := writeTranscript(t, `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","message":{"id":"msg-0","role":"assistant","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"content":[]}}
{"type":"assistant","timestamp":"2026-08-20T11:00:05Z","sessionId":"s1","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}
`)
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Model != "claude-opus-5" {
		t.Fatalf("synthetic message was ingested: %+v", rows)
	}
}

// Resuming or forking a Claude session copies earlier assistant messages into a
// new transcript. The message id is what keeps them from being counted twice.
func TestClaudeCopiedMessagesDeduplicateByMessageID(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "local-agent-mode-sessions", "w", "g", "local_1", ".claude", "projects", "demo")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	row := `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}` + "\n"
	for _, name := range []string{"original.jsonl", "resumed.jsonl"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(row), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := New(filepath.Join(root, "local-agent-mode-sessions")).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("copied message counted %d times", len(rows))
	}
}

// The Desktop attachment map is keyed by a lowercased path for comparison. The
// path handed back to the caller must stay the real one: lowercasing it works
// on Windows and silently finds nothing on a case-sensitive filesystem.
func TestAttachedTranscriptPathKeepsOriginalCase(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "Desktop", "local-agent-mode-sessions", "Workspace")
	projects := filepath.Join(root, "Projects", "MyRepo")
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"cliSessionId":"CLI-1","sessionId":"desktop-1","model":"claude-opus-5","originCwd":"C:/work/demo"}`
	if err := os.WriteFile(filepath.Join(desktop, "local_1.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(projects, "CLI-1.jsonl")
	body := `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}` + "\n"
	if err := os.WriteFile(transcript, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(filepath.Join(root, "Desktop", "local-agent-mode-sessions"))
	s.ProjectsOverride = filepath.Join(root, "Projects")
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	files, err := s.Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d discovered files, want 1", len(files))
	}
	if files[0] != transcript {
		t.Fatalf("discovered path %q, want the original %q", files[0], transcript)
	}
	if _, err := os.Stat(files[0]); err != nil {
		t.Fatalf("discovered path is not openable: %v", err)
	}
	rows, err := s.Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("attached transcript produced %d rows, want 1", len(rows))
	}
}

// Claude transcripts persist the effort on the assistant entry itself. All
// 14085 real assistant rows with token counts on this machine carry it; the
// only rows without it are <synthetic> messages, which are already excluded.
func TestClaudeEffortIsReadPerAssistantEntry(t *testing.T) {
	root := writeTranscript(t, `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","effort":"medium","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}
{"type":"assistant","timestamp":"2026-08-20T11:05:00Z","sessionId":"s1","effort":"xhigh","message":{"id":"msg-2","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":6,"output_tokens":8},"content":[]}}
`)
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].ReasoningEffort != "medium" || rows[1].ReasoningEffort != "xhigh" {
		t.Fatalf("per-entry effort not attributed: %q, %q", rows[0].ReasoningEffort, rows[1].ReasoningEffort)
	}
}

// An entry that declares no effort stays unknown. Inheriting the previous
// entry's value would attribute a setting to a call that never recorded one.
func TestClaudeEffortIsNotInheritedFromAPreviousEntry(t *testing.T) {
	root := writeTranscript(t, `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","sessionId":"s1","effort":"max","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}
{"type":"assistant","timestamp":"2026-08-20T11:05:00Z","sessionId":"s1","message":{"id":"msg-2","role":"assistant","model":"claude-opus-5","usage":{"input_tokens":6,"output_tokens":8},"content":[]}}
`)
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].ReasoningEffort != "max" {
		t.Fatalf("row 0 effort = %q, want max", rows[0].ReasoningEffort)
	}
	if rows[1].ReasoningEffort != "" {
		t.Fatalf("row 1 inherited effort %q", rows[1].ReasoningEffort)
	}
}
