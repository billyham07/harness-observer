package grok

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/source"
)

func TestRealLocalGrokIfPresent(t *testing.T) {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".grok", "sessions")
	if _, err := os.Stat(root); err != nil {
		t.Skip("no local Grok sessions")
	}
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("local Grok sessions with turn_completed parsed to zero turns")
	}
	for _, r := range rows {
		if r.Harness != "grok" {
			t.Fatalf("harness %s", r.Harness)
		}
		if r.Usage.Basis != model.UsageObserved {
			t.Fatalf("legacy estimate leaked: %+v", r)
		}
		if r.CostUSD != nil {
			t.Fatalf("costUsdTicks leaked into cost_usd: %+v", r.CostUSD)
		}
		if r.Usage.Semantics != model.UsageInputIncludesCacheRead {
			t.Fatalf("semantics %+v", r.Usage)
		}
	}
}

func TestGrokHomeDiscovery(t *testing.T) {
	t.Setenv("GROK_HOME", `C:\custom\grok`)
	if dir := defaultSessionsDir(); dir != filepath.Join(`C:\custom\grok`, "sessions") {
		t.Fatalf("GROK_HOME: %s", dir)
	}
}

func TestGrokAbsentInstallIsHarmless(t *testing.T) {
	rows, err := New(filepath.Join(t.TempDir(), "missing")).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("got %d", len(rows))
	}
}

func TestGrokAuthoritativeUsageAndDedup(t *testing.T) {
	root := t.TempDir()
	dir := writeGrokSession(t, root, "p1", []map[string]any{
		authoritativeUsage("p1", 1000, 100, 600, 50, 10, 125117780000),
		authoritativeUsage("p1", 1000, 100, 600, 50, 10, 125117780000), // duplicate prompt id
		authoritativeUsage("p2", 2000, 200, 1000, 100, 20, 1),
	}, nil)
	_ = dir
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 deduped turns", len(rows))
	}
	byID := map[string]model.Turn{}
	for _, r := range rows {
		byID[r.TurnID] = r
		if r.CostUSD != nil {
			t.Fatalf("costUsdTicks must not become cost_usd: %+v", r.CostUSD)
		}
		if r.Harness != "grok" || r.Client != "grok-build-plan" || r.ReasoningEffort != "medium" {
			t.Fatalf("attribution: %+v", r)
		}
		if r.Model != "grok-4.6-build" && r.Model != "grok-4.6" {
			t.Fatalf("model %s", r.Model)
		}
	}
	p1 := byID["p1"]
	if p1.Usage.InputTokens != 350 || p1.Usage.CacheReadTokens != 600 || p1.Usage.CacheWriteTokens != 50 {
		t.Fatalf("p1 cache split: %+v", p1.Usage)
	}
	if p1.Usage.OutputTokens != 100 || p1.Usage.ReasoningTokens != 10 {
		t.Fatalf("reasoning must stay a subset of output, not exclusive: %+v", p1.Usage)
	}
	if p1.ObservedTokens() != p1.Usage.PromptTokens+p1.Usage.OutputTokens {
		t.Fatalf("observed double-count: %d vs %+v", p1.ObservedTokens(), p1.Usage)
	}
	if p1.Usage.Semantics != model.UsageInputIncludesCacheRead {
		t.Fatalf("semantics %s", p1.Usage.Semantics)
	}
}

func TestGrokCacheSubsetClamp(t *testing.T) {
	root := t.TempDir()
	writeGrokSession(t, root, "p1", []map[string]any{
		authoritativeUsage("p1", 100, 10, 80, 50, 3, 0), // 80+50 > 100
	}, nil)
	rows, err := New(root).Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d", len(rows))
	}
	if rows[0].Usage.InputTokens != 0 {
		t.Fatalf("fresh input should clamp at 0, got %d", rows[0].Usage.InputTokens)
	}
	if rows[0].Usage.CacheReadTokens != 80 || rows[0].Usage.CacheWriteTokens != 50 {
		t.Fatalf("%+v", rows[0].Usage)
	}
}

func TestGrokLegacyUncoveredAndMixedNotBlended(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "%2Flegacy", "sess-legacy")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(legacy, "summary.json"), map[string]any{
		"info": map[string]any{"id": "sess-legacy", "cwd": "/legacy"}, "current_model_id": "grok-build",
	})
	writeJSON(t, filepath.Join(legacy, "signals.json"), map[string]any{"primaryModelId": "grok-build"})
	writeFile(t, filepath.Join(legacy, "updates.jsonl"),
		`{"params":{"_meta":{"totalTokens":40000,"promptId":"old"},"update":{"sessionUpdate":"agent_message_chunk"}}}`+"\n")

	writeGrokSession(t, root, "real", []map[string]any{
		authoritativeUsage("real", 500, 20, 100, 0, 5, 0),
	}, []string{
		`{"params":{"_meta":{"totalTokens":9000,"promptId":"uncovered"},"update":{"sessionUpdate":"agent_message_chunk"}}}`,
	})

	src := New(root)
	rows, err := src.Collect(context.Background(), source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("legacy/uncovered must not be estimated, got %d rows", len(rows))
	}
	if rows[0].TurnID != "real" || rows[0].Usage.Basis != model.UsageObserved {
		t.Fatalf("mixed session blended heuristic: %+v", rows[0])
	}
	files, err := src.Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("discovered %d", len(files))
	}
	notes := src.Report()
	joined := notes[0] + notes[1]
	if !contains(joined, "1") {
		t.Fatalf("doctor coverage notes: %v", notes)
	}
}

func TestGrokStructuredToolsAndIncrementalJSONL(t *testing.T) {
	root := t.TempDir()
	dir := writeGrokSession(t, root, "p1", nil, []string{
		`{"timestamp":"2026-06-19T11:30:00Z","params":{"update":{"sessionUpdate":"tool_call","title":"read_file"}}}`,
		`{"timestamp":"2026-06-19T11:30:01Z","params":{"update":{"sessionUpdate":"turn_completed","prompt_id":"p1","usage":{"inputTokens":10,"outputTokens":2,"cachedReadTokens":0,"cacheCreationTokens":0,"reasoningTokens":0}}}}`,
	})
	src := New(root)
	if err := src.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "updates.jsonl")
	first, next, err := src.ParseFile(path, source.FileState{}, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ToolCalls != 1 {
		t.Fatalf("tools %+v", first)
	}
	second, _, err := src.ParseFile(path, next, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("incremental resume re-parsed %d", len(second))
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"timestamp":"2026-06-19T11:31:00Z","params":{"update":{"sessionUpdate":"turn_completed","prompt_id":"p2","usage":{"inputTokens":3,"outputTokens":1}}}}` + "\n")
	_ = f.Close()
	third, _, err := src.ParseFile(path, next, source.Window{})
	if err != nil {
		t.Fatal(err)
	}
	if len(third) != 1 || third[0].TurnID != "p2" {
		t.Fatalf("tail turn: %+v", third)
	}
}

func writeGrokSession(t *testing.T, root, _ string, completed []map[string]any, extra []string) string {
	t.Helper()
	dir := filepath.Join(root, "%2FUsers%2Ftest%2Fmyproject", "019edf9c-0000-7000-8000-000000000001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "summary.json"), map[string]any{
		"info":             map[string]any{"id": "019edf9c-0000-7000-8000-000000000001", "cwd": "/Users/test/myproject"},
		"current_model_id": "grok-4.6",
		"agent_name":       "grok-build-plan",
		"reasoning_effort": "medium",
		"updated_at":       "2026-06-19T11:31:12Z",
	})
	writeJSON(t, filepath.Join(dir, "signals.json"), map[string]any{
		"primaryModelId": "grok-4.6",
		"modelsUsed":     []string{"grok-4.6"},
		"toolsUsed":      []string{"read_file"},
	})
	var lines []string
	lines = append(lines, extra...)
	for _, u := range completed {
		raw, _ := json.Marshal(map[string]any{
			"timestamp": "2026-06-19T11:30:00Z",
			"params": map[string]any{
				"update": map[string]any{
					"sessionUpdate": "turn_completed",
					"prompt_id":     u["prompt_id"],
					"usage":         u,
				},
			},
		})
		lines = append(lines, string(raw))
	}
	body := ""
	for i, line := range lines {
		if i > 0 {
			body += "\n"
		}
		body += line
	}
	if body != "" {
		body += "\n"
	}
	writeFile(t, filepath.Join(dir, "updates.jsonl"), body)
	return dir
}

func authoritativeUsage(promptID string, input, output, cacheRead, cacheWrite, reasoning int64, ticks int64) map[string]any {
	return map[string]any{
		"prompt_id":           promptID,
		"inputTokens":         input,
		"outputTokens":        output,
		"totalTokens":         input + output,
		"cachedReadTokens":    cacheRead,
		"cacheCreationTokens": cacheWrite,
		"reasoningTokens":     reasoning,
		"costUsdTicks":        ticks,
		"apiDurationMs":       1000,
		"modelUsage": map[string]any{
			"grok-4.6-build": map[string]any{
				"inputTokens": input, "outputTokens": output,
			},
		},
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(raw))
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()))
}
