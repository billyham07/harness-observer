package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/store"
)

func seed(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	at := func(day, hour int) time.Time {
		return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
	}
	turns := []model.Turn{
		{Timestamp: at(18, 9), Harness: "codex-desktop", Client: "Codex Desktop", Provider: "openai", Model: "gpt-5.6",
			Project: "demo", SessionID: "c1", RecordID: "c1:1", EventKind: "assistant", ToolCalls: 2,
			Usage: model.NewInputIncludesCacheReadUsage(1000, 100, 900, 0, 40, nil, model.UsageObserved)},
		{Timestamp: at(19, 9), Harness: "codex-desktop", Client: "Codex Desktop", Provider: "openai", Model: "gpt-5.6",
			Project: "demo", SessionID: "c1", RecordID: "c1:2", EventKind: "assistant",
			Usage: model.NewInputIncludesCacheReadUsage(2000, 200, 1800, 0, 0, nil, model.UsageDerivedDelta)},
		{Timestamp: at(19, 10), Harness: "claude-desktop", Client: "Claude Desktop", Provider: "anthropic", Model: "claude-opus-5",
			Project: "other", SessionID: "a1", RecordID: "a1:1", EventKind: "assistant", ToolErrors: 1,
			Usage: model.NewSplitUsage(10, 500, 4000, 100, 80, nil, model.UsageObserved)},
		{Timestamp: at(20, 10), Harness: "omp", Client: "omp", Provider: "xai", Model: "grok-4.6",
			Project: "demo", SessionID: "o1", RecordID: "o1:1", EventKind: "compaction",
			Usage: model.NewSplitUsage(50, 5, 10, 0, 0, nil, model.UsageEstimated)},
	}
	if _, err := db.InsertTurns(context.Background(), turns); err != nil {
		t.Fatal(err)
	}
	return db.SQL()
}

func TestAggregateGroupsByHarnessAndModel(t *testing.T) {
	db := seed(t)
	rows, err := Aggregate(context.Background(), db, Request{GroupBy: []string{"harness", "model"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d groups, want 3", len(rows))
	}
	for _, r := range rows {
		if r.Group["harness"] != "codex-desktop" {
			continue
		}
		if r.Turns != 2 || r.Sessions != 1 {
			t.Fatalf("codex group: %+v", r)
		}
		if r.InputTokens != 300 || r.CacheRead != 2700 || r.PromptTokens != 3000 {
			t.Fatalf("codex tokens: %+v", r)
		}
		if r.ObservedTokens != r.PromptTokens+r.OutputTokens {
			t.Fatalf("observed tokens must exclude reasoning: %+v", r)
		}
		if got := r.CacheHitRatio; got < 0.89 || got > 0.91 {
			t.Fatalf("cache hit ratio = %v, want ~0.9", got)
		}
	}
}

// Arbitrary ranges are the point: this is not a preset 24h/7d/30d filter.
func TestAggregateHonorsArbitraryTimeRange(t *testing.T) {
	db := seed(t)
	from := time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)
	to := time.Date(2026, 8, 20, 9, 30, 0, 0, time.UTC)
	rows, err := Aggregate(context.Background(), db, Request{Filter: Filter{From: &from, To: &to}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Turns != 1 {
		t.Fatalf("expected exactly the 19th 10:00 record, got %+v", rows)
	}
	if rows[0].OutputTokens != 500 {
		t.Fatalf("wrong record selected: %+v", rows[0])
	}
}

func TestAggregateFiltersByBasisAndEventKind(t *testing.T) {
	db := seed(t)
	rows, err := Aggregate(context.Background(), db, Request{Filter: Filter{Basis: []string{"observed", "derived-delta"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Turns != 3 {
		t.Fatalf("strict mode should exclude the estimated record: %+v", rows)
	}

	rows, err = Aggregate(context.Background(), db, Request{Filter: Filter{EventKind: []string{"compaction"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Turns != 1 {
		t.Fatalf("compaction usage must stay queryable on its own: %+v", rows)
	}
}

func TestAggregateTimeBuckets(t *testing.T) {
	db := seed(t)
	rows, err := Aggregate(context.Background(), db, Request{GroupBy: []string{"harness"}, Bucket: BucketDay})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("got %d daily rows, want 4", len(rows))
	}
	for _, r := range rows {
		if r.Bucket == "" {
			t.Fatalf("missing bucket label: %+v", r)
		}
	}
}

func TestAggregateRejectsUnknownGroupDimension(t *testing.T) {
	db := seed(t)
	if _, err := Aggregate(context.Background(), db, Request{GroupBy: []string{"cwd; DROP TABLE agent_turns"}}); err == nil {
		t.Fatal("unknown group dimension must be rejected, not interpolated into SQL")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_turns`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("table state after rejected query: %d (%v)", count, err)
	}
}
