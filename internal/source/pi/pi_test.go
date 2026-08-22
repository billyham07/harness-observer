package pi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

func TestOMPCollectsNormalizedUsageAndDeduplicatesResponse(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session","id":"s1","cwd":"C:/work/demo"}
{"type":"message","id":"m1","parentId":"u1","timestamp":"2026-08-20T10:00:00Z","message":{"role":"assistant","model":"claude-opus-4-1","provider":"anthropic","responseId":"r1","usage":{"input":100,"output":20,"cacheRead":80,"cacheWrite":5,"totalTokens":205,"cost":{"total":0.0123}},"content":[{"type":"toolCall","name":"bash"}]}}
{"type":"message","id":"m2","parentId":"u1","timestamp":"2026-08-20T10:00:01Z","message":{"role":"assistant","model":"claude-opus-4-1","provider":"anthropic","responseId":"r1","usage":{"input":100,"output":20,"cacheRead":80,"cacheWrite":5,"totalTokens":205,"cost":{"total":0.0123}},"content":[]}}
`
	if err := os.WriteFile(filepath.Join(project, "s1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := New("omp", root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Harness != "omp" || r.Provider != "anthropic" || r.Model != "claude-opus-4-1" {
		t.Fatalf("unexpected identity: %+v", r)
	}
	if r.Usage.InputTokens != 100 || r.Usage.OutputTokens != 20 || r.Usage.CacheReadTokens != 80 || r.Usage.CacheWriteTokens != 5 || r.Usage.PromptTokens != 185 {
		t.Fatalf("unexpected usage: %+v", r.Usage)
	}
	if r.Usage.Semantics != model.UsageSplitCache || r.Usage.NativeTotalTokens == nil || *r.Usage.NativeTotalTokens != 205 {
		t.Fatalf("native semantics missing: %+v", r.Usage)
	}
	if r.ToolCalls != 1 || r.EventKind != "assistant" {
		t.Fatalf("unexpected event: %+v", r)
	}
	if r.CostUSD == nil || *r.CostUSD != 0.0123 {
		t.Fatalf("native cost missing: %+v", r)
	}
}

func TestPiIncludesNestedToolAndCompactionUsage(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session","id":"s1","cwd":"/work/demo"}
{"type":"model_change","id":"mc1","timestamp":"2026-08-20T09:59:00Z","provider":"anthropic","modelId":"claude-opus-4-1"}
{"type":"message","id":"tool1","parentId":"a1","timestamp":"2026-08-20T10:00:00Z","message":{"role":"toolResult","isError":false,"usage":{"input":10,"output":2,"cacheRead":8,"cacheWrite":0,"totalTokens":20,"cost":{"total":0.001}}}}
{"type":"compaction","id":"c1","parentId":"tool1","timestamp":"2026-08-20T10:01:00Z","usage":{"input":20,"output":4,"cacheRead":15,"cacheWrite":1,"totalTokens":40,"cost":{"total":0.002}}}
{"type":"branch_summary","id":"b1","parentId":"c1","timestamp":"2026-08-20T10:02:00Z","usage":{"input":30,"output":6,"cacheRead":20,"cacheWrite":2,"totalTokens":58,"cost":{"total":0.003}}}
`
	if err := os.WriteFile(filepath.Join(project, "s1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := New("pi", root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want nested tool + compaction + branch summary", len(rows))
	}
	wantKinds := []string{"tool-llm", "compaction", "branch-summary"}
	for i, want := range wantKinds {
		if rows[i].EventKind != want {
			t.Fatalf("row %d kind=%q want=%q", i, rows[i].EventKind, want)
		}
		if rows[i].Provider != "anthropic" || rows[i].Model != "claude-opus-4-1" {
			t.Fatalf("row %d lost current model attribution: %+v", i, rows[i])
		}
	}
}
