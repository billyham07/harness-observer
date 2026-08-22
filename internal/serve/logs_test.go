package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/query"
	"github.com/billyham07/harness-observer/internal/store"
)

func logServer(t *testing.T) http.Handler {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "serve.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	at := func(day, hour int) time.Time { return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC) }
	turns := []model.Turn{
		{Timestamp: at(18, 9), Harness: "codex-cli", Client: "Codex CLI", Provider: "openai", Model: "gpt-5.6",
			ReasoningEffort: "high", Project: "demo", SessionID: "c1", RecordID: "c1:1", TurnID: "t1",
			EventKind: "assistant", SourceFile: "/evidence/c1.jsonl", SourceLine: 4,
			Usage: model.NewInputIncludesCacheReadUsage(1000, 100, 900, 0, 40, nil, model.UsageObserved)},
		{Timestamp: at(19, 10), Harness: "claude-code", Client: "Claude Code CLI", Provider: "anthropic",
			Model: "claude-opus-5", Project: "other", SessionID: "a1", RecordID: "a1:1",
			EventKind: "assistant", SourceFile: "/evidence/a1.jsonl", SourceLine: 1,
			Usage: model.NewSplitUsage(10, 500, 4000, 100, 80, nil, model.UsageObserved)},
		{Timestamp: at(19, 10), Harness: "claude-code", Client: "Claude Code CLI", Provider: "anthropic",
			Model: "claude-opus-5", Project: "other", SessionID: "a1", RecordID: "a1:2",
			EventKind: "tool-llm", SourceFile: "/evidence/a1.jsonl", SourceLine: 2,
			Usage: model.NewSplitUsage(10, 400, 4000, 0, 0, nil, model.UsageObserved)},
	}
	if _, err := db.InsertTurns(context.Background(), turns); err != nil {
		t.Fatal(err)
	}
	return Server{DB: db}.Handler()
}

func getLogs(t *testing.T, h http.Handler, rawQuery string) query.LogPage {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/logs?"+rawQuery, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/logs?%s = %d: %s", rawQuery, rec.Code, rec.Body.String())
	}
	var page query.LogPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	return page
}

func TestLogsEndpointFiltersAndPages(t *testing.T) {
	h := logServer(t)

	page := getLogs(t, h, "")
	if page.Total != 3 || len(page.Rows) != 3 {
		t.Fatalf("unfiltered: total %d, %d rows", page.Total, len(page.Rows))
	}
	if page.Rows[0].RecordID != "a1:1" && page.Rows[0].RecordID != "a1:2" {
		t.Fatalf("default order must be newest first, got %s", page.Rows[0].RecordID)
	}
	if page.Sort != string(query.LogSortTime) || page.Order != "desc" {
		t.Fatalf("default sort/order = %s/%s", page.Sort, page.Order)
	}

	if got := getLogs(t, h, "harness=claude-code&event_kind=assistant").Total; got != 1 {
		t.Fatalf("filtered total = %d, want 1", got)
	}
	if got := getLogs(t, h, "session=a1").Total; got != 2 {
		t.Fatalf("session lookup = %d, want 2", got)
	}
	if got := getLogs(t, h, "record=c1:1").Rows; len(got) != 1 || got[0].RecordID != "c1:1" {
		t.Fatalf("record lookup: %+v", got)
	}
	if got := getLogs(t, h, "turn=t1").Rows; len(got) != 1 || got[0].RecordID != "c1:1" {
		t.Fatalf("turn lookup: %+v", got)
	}
	if got := getLogs(t, h, "q=other").Total; got != 2 {
		t.Fatalf("project search = %d, want 2", got)
	}

	first := getLogs(t, h, "page=1&page_size=2")
	second := getLogs(t, h, "page=2&page_size=2")
	if len(first.Rows) != 2 || len(second.Rows) != 1 {
		t.Fatalf("paging: %d + %d rows", len(first.Rows), len(second.Rows))
	}
	if !first.HasMore || second.HasMore {
		t.Fatalf("has_more: page1=%v page2=%v", first.HasMore, second.HasMore)
	}
	for _, a := range first.Rows {
		for _, b := range second.Rows {
			if a.RecordID == b.RecordID {
				t.Fatalf("record %s appears on both pages", a.RecordID)
			}
		}
	}
	if got := getLogs(t, h, "limit=1").PageSize; got != 1 {
		t.Fatalf("limit alias for page_size = %d", got)
	}
	if got := getLogs(t, h, "page_size=9999").PageSize; got != query.MaxLogPageSize {
		t.Fatalf("page_size cap = %d, want %d", got, query.MaxLogPageSize)
	}
}

// The correlated selector reaches the endpoint intact: sel is one tuple per
// value and is never split on a comma.
func TestLogsEndpointKeepsSelectionCorrelated(t *testing.T) {
	h := logServer(t)
	if got := getLogs(t, h, "sel=codex-cli%7Cgpt-5.6").Total; got != 1 {
		t.Fatalf("harness|model selection = %d rows, want 1", got)
	}
	if got := getLogs(t, h, "sel=codex-cli%7Cclaude-opus-5").Total; got != 0 {
		t.Fatalf("a tuple that never occurred must match nothing, got %d rows", got)
	}
	if got := getLogs(t, h, "sel=codex-cli%7Cgpt-5.6%7Chigh&sel=claude-code%7Cclaude-opus-5").Total; got != 3 {
		t.Fatalf("two tuples = %d rows, want 3", got)
	}
	// Effort is correlated, not an independent list: codex ran gpt-5.6 at
	// effort=high, so the same model at effort=low is not in the evidence.
	if got := getLogs(t, h, "sel=codex-cli%7Cgpt-5.6%7Clow").Total; got != 0 {
		t.Fatalf("effort correlation lost: %d rows", got)
	}
}

// [from,to) on the Logs endpoint, and the aggregation's inclusive bound is
// still reachable explicitly.
func TestLogsEndpointTimeBounds(t *testing.T) {
	h := logServer(t)
	window := "from=2026-08-18T09:00:00Z&to=2026-08-19T10:00:00Z"
	if got := getLogs(t, h, window).Total; got != 1 {
		t.Fatalf("half-open window = %d rows, want 1", got)
	}
	if got := getLogs(t, h, window+"&bound=inclusive").Total; got != 3 {
		t.Fatalf("inclusive window = %d rows, want 3", got)
	}
}

func TestLogsEndpointRejectsBadInput(t *testing.T) {
	h := logServer(t)
	for _, rawQuery := range []string{
		"sort=ts_unix",
		"sort=ts_unix%3BDROP+TABLE+agent_turns",
		"sort=ts_unix+DESC%2C+1",
		"dir=sideways",
		"page=abc",
		"page_size=-1",
		"from=yesterday",
		"bound=whatever",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/logs?"+rawQuery, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("GET /api/logs?%s = %d, want 400", rawQuery, rec.Code)
		}
	}
	// The store is still there after every rejected request.
	if got := getLogs(t, h, "").Total; got != 3 {
		t.Fatalf("store after rejected requests: %d rows", got)
	}
}

// A UI must be able to tell "nothing ingested" from "your filters excluded
// everything" without guessing from a blank table.
func TestLogsEndpointReportsEmptyStates(t *testing.T) {
	h := logServer(t)
	page := getLogs(t, h, "from=2001-01-01&to=2001-01-02")
	if page.Empty == nil || page.Empty.Reason != query.EmptyRange {
		t.Fatalf("empty range: %+v", page.Empty)
	}
	page = getLogs(t, h, "harness=nope")
	if page.Empty == nil || page.Empty.Reason != query.EmptyFiltered {
		t.Fatalf("filtered out: %+v", page.Empty)
	}
	if page.Empty.StoredTurns != 3 {
		t.Fatalf("stored_turns = %d, want 3", page.Empty.StoredTurns)
	}

	empty, err := store.Open(filepath.Join(t.TempDir(), "blank.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	page = getLogs(t, Server{DB: empty}.Handler(), "")
	if page.Empty == nil || page.Empty.Reason != query.EmptyNotIngested {
		t.Fatalf("not ingested: %+v", page.Empty)
	}
}

// Without a pricing catalog the response says so, instead of shipping zeros
// that read as free.
func TestLogsEndpointOmitsCostWithoutPricing(t *testing.T) {
	page := getLogs(t, logServer(t), "")
	if page.PricingAvailable {
		t.Fatal("pricing_available must be false when no catalog is configured")
	}
	for _, row := range page.Rows {
		if row.EstimatedCost != nil {
			t.Fatalf("%s carries an estimate without a catalog", row.RecordID)
		}
	}
}

// The response carries evidence locations, never evidence contents, and no
// endpoint accepts a path from the client.
func TestLogsEndpointExposesNoFileContent(t *testing.T) {
	h := logServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/logs?record=c1:1", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "/evidence/c1.jsonl") || !strings.Contains(body, `"source_line": 4`) {
		t.Fatalf("evidence location missing: %s", body)
	}
	// There is no source-content route to ask for the file itself.
	for _, path := range []string{"/api/logs/source?file=/evidence/c1.jsonl", "/api/source?file=/etc/passwd"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404: the API never serves file contents", path, rec.Code)
		}
	}
}
