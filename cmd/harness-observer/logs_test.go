package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/billyham07/harness-observer/internal/query"
)

func str(v string) *string   { return &v }
func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }

func renderLogs(t *testing.T, page query.LogPage, withCost bool) string {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := printLogTable(buf, page, withCost); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// "-" in this table means unknown, and it has to mean that consistently: an
// effort the harness never recorded, an adapter that does not observe tool
// failures, and a model with no published rate are all unknown, not zero.
func TestLogTableRendersUnknownAsDashNotZero(t *testing.T) {
	page := query.LogPage{
		Page: 1, Pages: 2, Total: 3, PageSize: 1, Sort: "time", Order: "desc",
		Rows: []query.LogRow{{
			Timestamp: time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC),
			Harness:   "codex-cli", Model: str("gpt-5.6"), SessionID: "c1", RecordID: "c1:1",
			EventKind: "assistant", ToolCalls: 2,
			// No effort, no project, no tool-error evidence, no rate.
			EstimatedCost: &query.LogCost{UnpricedTokens: 10, Reason: "no catalog entry for this model"},
		}},
	}
	out := renderLogs(t, page, true)
	fields := strings.Fields(strings.Split(out, "\n")[1])
	dashes := 0
	for _, f := range fields {
		if f == "-" {
			dashes++
		}
	}
	if dashes != 5 {
		t.Fatalf("want 5 unknown cells (effort, project, tool_err, est_usd, coverage), got %d in %q", dashes, fields)
	}
	if strings.Contains(out, "0.000000") {
		t.Fatalf("an unpriced row must not render a zero cost:\n%s", out)
	}
	if !strings.Contains(out, "page 1/2") || !strings.Contains(out, "3 events") {
		t.Fatalf("footer is missing the paging context:\n%s", out)
	}
	if !strings.Contains(out, "not actual subscription spend") {
		t.Fatalf("the cost column must stay labelled as an estimate:\n%s", out)
	}
}

func TestLogTableRendersObservedZeroAsZero(t *testing.T) {
	page := query.LogPage{
		Page: 1, Pages: 1, Total: 1, PageSize: 50, Sort: "time", Order: "desc",
		Rows: []query.LogRow{{
			Timestamp: time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC),
			Harness:   "omp", Model: str("grok-4.6"), ReasoningEffort: str("high"), Project: str("demo"),
			SessionID: "o1", RecordID: "o1:1", EventKind: "assistant",
			ToolCalls: 1, ToolErrors: i64(0), ToolErrorEvidence: query.ToolErrorsObserved,
			EstimatedCost: &query.LogCost{EstimatedCostUSD: f64(0.0125), PricingCoverage: 1},
		}},
	}
	out := renderLogs(t, page, true)
	row := strings.Fields(strings.Split(out, "\n")[1])
	if strings.Contains(strings.Join(row, " "), " - ") {
		t.Fatalf("an adapter that observes tool failures reports a real zero:\n%s", out)
	}
	if !strings.Contains(out, "0.012500") || !strings.Contains(out, "100%") {
		t.Fatalf("priced row lost its estimate:\n%s", out)
	}
}

// The three empty states are three different things to tell somebody.
func TestEmptyStateMessages(t *testing.T) {
	cases := map[string]struct {
		state query.LogEmptyState
		want  string
	}{
		"nothing ingested": {query.LogEmptyState{Reason: query.EmptyNotIngested}, "harness-observer ingest"},
		"empty range":      {query.LogEmptyState{Reason: query.EmptyRange, StoredTurns: 42}, "42 events outside it"},
		"filtered out":     {query.LogEmptyState{Reason: query.EmptyFiltered, StoredTurns: 42, TurnsInRange: 7}, "7 events are in the selected time range"},
	}
	for name, tc := range cases {
		got := emptyStateMessage(tc.state)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q does not mention %q", name, got, tc.want)
		}
	}
	// An empty page prints the state and no table header at all.
	out := renderLogs(t, query.LogPage{Empty: &query.LogEmptyState{Reason: query.EmptyNotIngested}}, false)
	if strings.Contains(out, "cache_read") || strings.Contains(out, "tool_err") {
		t.Fatalf("an empty result must not print an empty table:\n%s", out)
	}
}
