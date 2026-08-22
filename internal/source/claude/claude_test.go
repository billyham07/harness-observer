package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

func TestDesktopMetadataJoinsGlobalClaudeTranscript(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "desktop", "local-agent-mode-sessions")
	projects := filepath.Join(root, ".claude", "projects")
	if err := os.MkdirAll(filepath.Join(desktop, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projects, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"cliSessionId":"cli-1","sessionId":"desktop-1","title":"demo","model":"claude-opus-4-1","originCwd":"C:/work/demo"}`
	if err := os.WriteFile(filepath.Join(desktop, "workspace", "local_1.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	trace := `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","message":{"id":"msg-1","role":"assistant","model":"claude-opus-4-1","usage":{"input_tokens":500,"output_tokens":50,"cache_read_input_tokens":400,"cache_creation_input_tokens":10},"content":[{"type":"tool_use","name":"Bash"}]}}
`
	if err := os.WriteFile(filepath.Join(projects, "demo", "cli-1.jsonl"), []byte(trace), 0o644); err != nil {
		t.Fatal(err)
	}
	cliTrace := `{"type":"assistant","timestamp":"2026-08-20T11:01:00Z","sessionId":"cli-only","entrypoint":"cli","message":{"id":"msg-2","role":"assistant","model":"claude-opus-4-1","usage":{"input_tokens":5,"output_tokens":7},"content":[]}}
`
	if err := os.WriteFile(filepath.Join(projects, "demo", "claude-code-only.jsonl"), []byte(cliTrace), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(desktop)
	s.ProjectsOverride = projects
	rows, err := s.Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (one Desktop, one CLI)", len(rows))
	}
	byHarness := map[string]model.Turn{}
	for _, row := range rows {
		byHarness[row.Harness] = row
	}
	cli, ok := byHarness["claude-code"]
	if !ok || cli.Client != "Claude Code CLI" {
		t.Fatalf("unattached CLI transcript not attributed to claude-code: %+v", rows)
	}
	r, ok := byHarness["claude-desktop"]
	if !ok {
		t.Fatalf("no Desktop-attributed row: %+v", rows)
	}
	if r.SessionID != "desktop-1" || r.Project != "demo" {
		t.Fatalf("unexpected attribution: %+v", r)
	}
	if r.Usage.InputTokens != 500 || r.Usage.CacheReadTokens != 400 || r.Usage.CacheWriteTokens != 10 || r.Usage.PromptTokens != 910 || r.ToolCalls != 1 {
		t.Fatalf("unexpected usage: %+v", r)
	}
	if r.RecordID != "claude:msg-1" || r.EventKind != "assistant" || r.Client != "Claude Desktop" {
		t.Fatalf("unexpected evidence identity: %+v", r)
	}
}

func TestDesktopExcludesAuditJSONL(t *testing.T) {
	root := t.TempDir()
	traceDir := filepath.Join(root, "local-agent-mode-sessions", "w", "g", "local_1", ".claude", "projects", "demo")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	row := `{"type":"assistant","timestamp":"2026-08-20T11:00:00Z","message":{"id":"msg-1","role":"assistant","model":"claude-opus-4-1","usage":{"input_tokens":1,"output_tokens":1},"content":[]}}`
	if err := os.WriteFile(filepath.Join(traceDir, "audit.jsonl"), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(traceDir, "real.jsonl"), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := New(filepath.Join(root, "local-agent-mode-sessions")).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
}
