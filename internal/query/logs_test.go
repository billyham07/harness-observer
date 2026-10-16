package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/store"
)

// Four of these rows share one timestamp on purpose: equal timestamps are the
// case where a naive newest-first pagination starts duplicating and dropping
// rows between pages.
var (
	logT0 = time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	logT1 = time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	logT2 = time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	logT3 = time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC)
)

func cost(v float64) *float64 { return &v }

func seedLogs(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	turns := []model.Turn{
		// A pi/omp row: the only adapter family that observes tool-result
		// failures, and the only source that persists its own cost. Client,
		// effort, project and turn id are deliberately absent.
		{Timestamp: logT3, Harness: "omp", Provider: "xai", Model: "unpriced-model",
			SessionID: "o1", RecordID: "o1:1", EventKind: "compaction", ToolCalls: 1, ToolErrors: 0,
			CostUSD:    cost(0.42),
			SourceFile: "/evidence/omp/o1.jsonl", SourceLine: 12,
			Usage: model.NewSplitUsage(50, 5, 10, 0, 0, nil, model.UsageEstimated)},

		{Timestamp: logT2, Harness: "claude-code", Client: "Claude Code CLI", Provider: "anthropic",
			Model: "priced-b", ReasoningEffort: "high", Project: "demo",
			SessionID: "a1", RecordID: "a1:1", TurnID: "at1", EventKind: "assistant", ToolCalls: 3, ToolErrors: 2,
			SourceFile: "/evidence/claude/a1.jsonl", SourceLine: 1,
			Usage: model.NewSplitUsage(10, 500, 4000, 100, 80, nil, model.UsageObserved)},
		{Timestamp: logT2, Harness: "claude-code", Client: "Claude Code CLI", Provider: "anthropic",
			Model: "priced-b", ReasoningEffort: "high", Project: "demo",
			SessionID: "a1", RecordID: "a1:2", TurnID: "at2", EventKind: "assistant",
			SourceFile: "/evidence/claude/a1.jsonl", SourceLine: 2,
			Usage: model.NewSplitUsage(10, 400, 4000, 0, 0, nil, model.UsageObserved)},
		{Timestamp: logT2, Harness: "claude-code", Client: "Claude Code CLI", Provider: "anthropic",
			Model: "priced-b", ReasoningEffort: "high", Project: "demo",
			SessionID: "a1", RecordID: "a1:3", TurnID: "at3", EventKind: "assistant",
			SourceFile: "/evidence/claude/a1.jsonl", SourceLine: 3,
			Usage: model.NewSplitUsage(10, 300, 4000, 0, 0, nil, model.UsageObserved)},
		{Timestamp: logT2, Harness: "claude-code", Client: "Claude Code CLI", Provider: "anthropic",
			Model: "priced-b", ReasoningEffort: "high", Project: "demo",
			SessionID: "a1", RecordID: "a1:4", TurnID: "at4", EventKind: "tool-llm",
			SourceFile: "/evidence/claude/a1.jsonl", SourceLine: 4,
			Usage: model.NewSplitUsage(10, 200, 4000, 0, 0, nil, model.UsageObserved)},

		{Timestamp: logT1, Harness: "codex-desktop", Client: "Codex Desktop", Provider: "openai",
			Model: "priced-a", ReasoningEffort: "high", Project: "demo",
			SessionID: "c1", RecordID: "c1:1", TurnID: "ct1", EventKind: "assistant", ToolCalls: 2,
			SourceFile: "/evidence/codex/c1.jsonl", SourceLine: 7,
			Usage: model.NewInputIncludesCacheReadUsage(1000, 100, 900, 0, 40, nil, model.UsageObserved)},
		{Timestamp: logT0, Harness: "codex-cli", Client: "Codex CLI", Provider: "openai",
			Model: "priced-a", ReasoningEffort: "low", Project: "other",
			SessionID: "c2", RecordID: "c2:1", TurnID: "ct2", EventKind: "assistant",
			SourceFile: "/evidence/codex/c2.jsonl", SourceLine: 3,
			Usage: model.NewInputIncludesCacheReadUsage(2000, 200, 1800, 0, 0, nil, model.UsageObserved)},
	}
	if _, err := db.InsertTurns(context.Background(), turns); err != nil {
		t.Fatal(err)
	}
	return db.SQL()
}

func mustLogs(t *testing.T, db *sql.DB, q LogQuery) LogPage {
	t.Helper()
	page, err := QueryLogs(context.Background(), db, q, nil)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func recordIDs(page LogPage) []string {
	out := make([]string, 0, len(page.Rows))
	for _, r := range page.Rows {
		out = append(out, r.RecordID)
	}
	return out
}

// The upper bound is exclusive: [from,to). A window that ends exactly at an
// event's timestamp must not contain that event, or two consecutive windows
// would both claim it.
func TestQueryLogsTimeRangeIsHalfOpen(t *testing.T) {
	db := seedLogs(t)
	from, to := logT1, logT2
	page := mustLogs(t, db, LogQuery{Filter: Filter{From: &from, To: &to, ToExclusive: true}})
	if got := recordIDs(page); len(got) != 1 || got[0] != "c1:1" {
		t.Fatalf("[from,to) must include the lower bound and exclude the upper: %v", got)
	}
	if page.Total != 1 {
		t.Fatalf("total = %d, want 1", page.Total)
	}

	// The same window one second wider picks the boundary events back up, which
	// is what proves the exclusion above was the bound and not a lost row.
	wider := logT2.Add(time.Second)
	page = mustLogs(t, db, LogQuery{Filter: Filter{From: &from, To: &wider, ToExclusive: true}})
	if page.Total != 5 {
		t.Fatalf("total = %d, want the 4 equal-timestamp rows plus c1:1", page.Total)
	}

	// The aggregations keep an inclusive upper bound, and that difference is a
	// documented choice rather than an accident. Guard it so nobody "fixes"
	// half of it.
	inclusive := mustLogs(t, db, LogQuery{Filter: Filter{From: &from, To: &to}})
	if inclusive.Total != 5 {
		t.Fatalf("inclusive bound total = %d, want 5", inclusive.Total)
	}
}

// Newest first, with a total order, so paging cannot duplicate or skip rows
// when four events share one timestamp.
func TestQueryLogsPaginationIsDeterministic(t *testing.T) {
	db := seedLogs(t)
	seen := map[string]int{}
	var order []string
	var last time.Time
	for page := 1; page <= 4; page++ {
		got := mustLogs(t, db, LogQuery{Page: page, PageSize: 2})
		if got.Total != 7 {
			t.Fatalf("page %d: total = %d, want 7", page, got.Total)
		}
		if got.Pages != 4 {
			t.Fatalf("page %d: pages = %d, want 4", page, got.Pages)
		}
		if want := page < 4; got.HasMore != want {
			t.Fatalf("page %d: has_more = %v, want %v", page, got.HasMore, want)
		}
		for _, r := range got.Rows {
			seen[r.RecordID]++
			order = append(order, r.RecordID)
			if !last.IsZero() && r.Timestamp.After(last) {
				t.Fatalf("page %d: %s at %s is newer than the previous row at %s", page, r.RecordID, r.Timestamp, last)
			}
			last = r.Timestamp
		}
	}
	if len(order) != 7 {
		t.Fatalf("walked %d rows across 4 pages, want 7", len(order))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("record %s appeared %d times across pages", id, n)
		}
	}

	// Same walk again: a stable order is only stable if it repeats.
	var again []string
	for page := 1; page <= 4; page++ {
		again = append(again, recordIDs(mustLogs(t, db, LogQuery{Page: page, PageSize: 2}))...)
	}
	if strings.Join(order, ",") != strings.Join(again, ",") {
		t.Fatalf("order changed between identical walks:\n%v\n%v", order, again)
	}

	// A page past the end is empty but is not an empty state: the data exists.
	beyond := mustLogs(t, db, LogQuery{Page: 99, PageSize: 2})
	if len(beyond.Rows) != 0 || beyond.Total != 7 || beyond.Empty != nil {
		t.Fatalf("page past the end: %d rows, total %d, empty %+v", len(beyond.Rows), beyond.Total, beyond.Empty)
	}
}

func TestQueryLogsPageSizeIsCapped(t *testing.T) {
	db := seedLogs(t)
	page := mustLogs(t, db, LogQuery{PageSize: 100000})
	if page.PageSize != MaxLogPageSize {
		t.Fatalf("page_size = %d, want the hard cap %d", page.PageSize, MaxLogPageSize)
	}
	if got := mustLogs(t, db, LogQuery{PageSize: 0}).PageSize; got != DefaultLogPageSize {
		t.Fatalf("default page_size = %d, want %d", got, DefaultLogPageSize)
	}
	if got := mustLogs(t, db, LogQuery{PageSize: -5}).PageSize; got != DefaultLogPageSize {
		t.Fatalf("negative page_size = %d, want the default %d", got, DefaultLogPageSize)
	}
}

// The harness/model/effort selector stays correlated here exactly as it does in
// the overview: two selected tuples are two tuples, not the cross product of
// their levels.
func TestQueryLogsSelectionStaysCorrelated(t *testing.T) {
	db := seedLogs(t)
	pricedA, pricedB := "priced-a", "priced-b"
	page := mustLogs(t, db, LogQuery{Filter: Filter{Selection: []Tuple{
		{Harness: "codex-desktop", Model: &pricedA},
		{Harness: "codex-cli", Model: &pricedB},
	}}})
	if got := recordIDs(page); len(got) != 1 || got[0] != "c1:1" {
		t.Fatalf("flattened selection leaked codex-cli/priced-a into the result: %v", got)
	}

	effort := "high"
	page = mustLogs(t, db, LogQuery{Filter: Filter{Selection: []Tuple{
		{Harness: "codex-cli", Model: &pricedA, Effort: &effort},
	}}})
	if page.Total != 0 {
		t.Fatalf("codex-cli only ran priced-a at effort=low; got %d rows", page.Total)
	}
}

// Flat dimensions compose as AND across dimensions and OR within one.
func TestQueryLogsIndependentFiltersCompose(t *testing.T) {
	db := seedLogs(t)
	page := mustLogs(t, db, LogQuery{Filter: Filter{
		Client:    []string{"Claude Code CLI"},
		EventKind: []string{"assistant"},
	}})
	if page.Total != 3 {
		t.Fatalf("client AND event_kind = %d rows, want 3", page.Total)
	}
	page = mustLogs(t, db, LogQuery{Filter: Filter{EventKind: []string{"assistant", "compaction"}}})
	if page.Total != 6 {
		t.Fatalf("event_kind OR within the dimension = %d rows, want 6", page.Total)
	}
	page = mustLogs(t, db, LogQuery{Filter: Filter{Basis: []string{"estimated"}}})
	if got := recordIDs(page); len(got) != 1 || got[0] != "o1:1" {
		t.Fatalf("usage_basis filter: %v", got)
	}
}

func TestQueryLogsExactIDLookup(t *testing.T) {
	db := seedLogs(t)
	if got := mustLogs(t, db, LogQuery{Filter: Filter{Session: []string{"a1"}}}).Total; got != 4 {
		t.Fatalf("session lookup = %d rows, want 4", got)
	}
	if got := recordIDs(mustLogs(t, db, LogQuery{Filter: Filter{Record: []string{"c1:1"}}})); len(got) != 1 || got[0] != "c1:1" {
		t.Fatalf("record lookup: %v", got)
	}
	if got := recordIDs(mustLogs(t, db, LogQuery{Filter: Filter{Turn: []string{"at3"}}})); len(got) != 1 || got[0] != "a1:3" {
		t.Fatalf("turn lookup: %v", got)
	}
	// Exact, not prefix: an id search must not widen into a substring match.
	if got := mustLogs(t, db, LogQuery{Filter: Filter{Session: []string{"a"}}}).Total; got != 0 {
		t.Fatalf("session id lookup matched a prefix: %d rows", got)
	}
}

// Search covers model and project only, because those are the only free-text
// dimensions in the store. There is no content to search.
func TestQueryLogsSearchIsScopedAndEscaped(t *testing.T) {
	db := seedLogs(t)
	if got := mustLogs(t, db, LogQuery{Filter: Filter{Search: "PRICED-A"}}).Total; got != 2 {
		t.Fatalf("case-insensitive model search = %d rows, want 2", got)
	}
	if got := mustLogs(t, db, LogQuery{Filter: Filter{Search: "other"}}).Total; got != 1 {
		t.Fatalf("project search = %d rows, want 1", got)
	}
	// A wildcard typed by the user is a literal character, not "match
	// everything".
	if got := mustLogs(t, db, LogQuery{Filter: Filter{Search: "%"}}).Total; got != 0 {
		t.Fatalf("unescaped LIKE wildcard matched %d rows, want 0", got)
	}
	if got := mustLogs(t, db, LogQuery{Filter: Filter{Search: "_riced-a"}}).Total; got != 0 {
		t.Fatalf("unescaped LIKE single-char wildcard matched %d rows, want 0", got)
	}
	// The session id is not free text and is not searched: it has an exact
	// lookup instead.
	if got := mustLogs(t, db, LogQuery{Filter: Filter{Search: "a1"}}).Total; got != 0 {
		t.Fatalf("search leaked into id columns: %d rows", got)
	}
}

func TestQueryLogsNullDimensionsStayNull(t *testing.T) {
	db := seedLogs(t)
	page := mustLogs(t, db, LogQuery{Filter: Filter{Record: []string{"o1:1"}}})
	if len(page.Rows) != 1 {
		t.Fatalf("want the omp row, got %d rows", len(page.Rows))
	}
	row := page.Rows[0]
	for name, v := range map[string]*string{
		"client":           row.Client,
		"reasoning_effort": row.ReasoningEffort,
		"project":          row.Project,
		"turn_id":          row.TurnID,
	} {
		if v != nil {
			t.Fatalf("%s = %q, want null: an absent dimension must not become an empty string", name, *v)
		}
	}
	if row.NativeTotalTokens != nil {
		t.Fatalf("native_total_tokens = %d, want null", *row.NativeTotalTokens)
	}

	// And a row that does have them keeps them.
	page = mustLogs(t, db, LogQuery{Filter: Filter{Record: []string{"c1:1"}}})
	row = page.Rows[0]
	if row.ReasoningEffort == nil || *row.ReasoningEffort != "high" || row.Project == nil || *row.Project != "demo" {
		t.Fatalf("populated dimensions lost: %+v", row)
	}
	if row.NativeInputTokens == nil || *row.NativeInputTokens != 1000 {
		t.Fatalf("native input counter not preserved: %+v", row.NativeInputTokens)
	}
}

// A stored tool_errors=0 from an adapter that never looks for tool failures is
// not evidence of success, and must not render as one.
func TestQueryLogsToolErrorsCarryEvidence(t *testing.T) {
	db := seedLogs(t)
	byID := map[string]LogRow{}
	for _, r := range mustLogs(t, db, LogQuery{PageSize: 50}).Rows {
		byID[r.RecordID] = r
	}

	codex := byID["c1:1"]
	if codex.ToolErrors != nil {
		t.Fatalf("codex has no tool-result error evidence; tool_errors = %d, want null", *codex.ToolErrors)
	}
	if codex.ToolErrorEvidence != ToolErrorsUnavailable {
		t.Fatalf("codex tool_error_evidence = %q, want %q", codex.ToolErrorEvidence, ToolErrorsUnavailable)
	}
	if codex.ToolCalls != 2 {
		t.Fatalf("tool call count is observed and must survive: %d", codex.ToolCalls)
	}

	omp := byID["o1:1"]
	if omp.ToolErrors == nil || *omp.ToolErrors != 0 || omp.ToolErrorEvidence != ToolErrorsObserved {
		t.Fatalf("omp observes tool failures, so a real zero must be a zero: %+v (%s)", omp.ToolErrors, omp.ToolErrorEvidence)
	}

	// A positive count is evidence on its own, whatever the capability table
	// says about the family.
	claude := byID["a1:1"]
	if claude.ToolErrors == nil || *claude.ToolErrors != 2 || claude.ToolErrorEvidence != ToolErrorsObserved {
		t.Fatalf("an observed non-zero error count must be reported: %+v (%s)", claude.ToolErrors, claude.ToolErrorEvidence)
	}
}

// The reported cost is the source's own number. It stays null when the source
// reports none, and is never filled in from the price catalog.
func TestQueryLogsReportedCostIsSourceEvidence(t *testing.T) {
	db := seedLogs(t)
	page, err := QueryLogs(context.Background(), db, LogQuery{PageSize: 50}, testPricer(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range page.Rows {
		switch r.RecordID {
		case "o1:1":
			if r.ReportedCostUSD == nil || *r.ReportedCostUSD != 0.42 {
				t.Fatalf("omp reported cost lost: %+v", r.ReportedCostUSD)
			}
		default:
			if r.ReportedCostUSD != nil {
				t.Fatalf("%s: reported cost = %v, want null; no catalog price may leak into it", r.RecordID, *r.ReportedCostUSD)
			}
		}
	}
}

func TestQueryLogsPricingIsQueryTimeAndUnknownIsNotZero(t *testing.T) {
	db := seedLogs(t)

	plain := mustLogs(t, db, LogQuery{PageSize: 50})
	if plain.PricingAvailable {
		t.Fatal("pricing_available must be false without a pricer")
	}
	for _, r := range plain.Rows {
		if r.EstimatedCost != nil {
			t.Fatalf("%s: estimate present without a pricer", r.RecordID)
		}
	}

	page, err := QueryLogs(context.Background(), db, LogQuery{PageSize: 50}, testPricer(t))
	if err != nil {
		t.Fatal(err)
	}
	if !page.PricingAvailable {
		t.Fatal("pricing_available must be true with a pricer")
	}
	byID := map[string]LogRow{}
	for _, r := range page.Rows {
		byID[r.RecordID] = r
	}

	priced := byID["c1:1"].EstimatedCost
	if priced == nil || priced.EstimatedCostUSD == nil || *priced.EstimatedCostUSD <= 0 {
		t.Fatalf("priced row has no estimate: %+v", priced)
	}
	if priced.UnpricedTokens != 0 || priced.PricingCoverage != 1 {
		t.Fatalf("fully priced row reports a coverage gap: %+v", priced)
	}
	if priced.Rule == "" || priced.CanonicalModel == "" {
		t.Fatalf("estimate carries no provenance: %+v", priced)
	}

	unpriced := byID["o1:1"].EstimatedCost
	if unpriced == nil {
		t.Fatal("an unpriced row still needs a coverage report")
	}
	if unpriced.EstimatedCostUSD != nil {
		t.Fatalf("unknown pricing rendered as %v; it must stay null, not $0", *unpriced.EstimatedCostUSD)
	}
	if unpriced.UnpricedTokens == 0 || unpriced.Reason == "" {
		t.Fatalf("unpriced row must say what it could not price: %+v", unpriced)
	}

	// Query time means query time: no price column appeared in the store.
	rows, err := db.Query(`SELECT column_name FROM information_schema.columns WHERE table_name = 'agent_turns'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(column, "estimated") || strings.Contains(column, "price") || strings.Contains(column, "rate") {
			t.Fatalf("pricing leaked into the schema as column %q", column)
		}
	}
}

// Source location is metadata about where the evidence lives. The API returns
// the path and the line, and never the bytes at that path.
func TestQueryLogsReturnEvidenceLocationWithoutContent(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	const secret = "TOP-SECRET-PROMPT-TEXT"
	if err := os.WriteFile(transcript, []byte(`{"prompt":"`+secret+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.InsertTurns(context.Background(), []model.Turn{{
		Timestamp: logT1, Harness: "codex-cli", Model: "priced-a", SessionID: "s", RecordID: "s:1",
		EventKind: "assistant", SourceFile: transcript, SourceLine: 1,
		Usage: model.NewSplitUsage(1, 1, 0, 0, 0, nil, model.UsageObserved),
	}}); err != nil {
		t.Fatal(err)
	}

	page := mustLogs(t, db.SQL(), LogQuery{})
	if len(page.Rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(page.Rows))
	}
	if page.Rows[0].SourceFile != transcript || page.Rows[0].SourceLine != 1 {
		t.Fatalf("evidence location lost: %q:%d", page.Rows[0].SourceFile, page.Rows[0].SourceLine)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatal("the API read the evidence file and returned its content")
	}
}

// The DTO is an allow-list. If a column that carries conversation content ever
// reaches the schema, this test fails before it reaches an HTTP response.
func TestQueryLogsDTOCarriesNoContentFields(t *testing.T) {
	db := seedLogs(t)
	page, err := QueryLogs(context.Background(), db, LogQuery{PageSize: 1}, testPricer(t))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(page.Rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"timestamp": true, "harness": true, "client": true, "provider": true, "model": true,
		"reasoning_effort": true, "project": true, "session_id": true, "turn_id": true,
		"record_id": true, "event_kind": true, "usage_basis": true, "usage_semantics": true,
		"input_tokens": true, "cache_read_tokens": true, "cache_write_tokens": true,
		"prompt_tokens": true, "output_tokens": true, "reasoning_tokens": true,
		"observed_tokens": true, "native_input_tokens": true, "native_total_tokens": true,
		"tool_calls": true, "tool_errors": true, "tool_error_evidence": true,
		"reported_cost_usd": true, "source_file": true, "source_line": true, "estimated_cost": true,
	}
	for name := range fields {
		if !allowed[name] {
			t.Fatalf("unexpected log field %q: the Logs DTO is an allow-list, and content never joins it", name)
		}
	}
	for _, banned := range []string{"prompt", "response", "message", "content", "text", "tool_args", "tool_output", "api_key", "token_value"} {
		if _, found := fields[banned]; found {
			t.Fatalf("content field %q must never be exposed", banned)
		}
	}
}

func TestQueryLogsRejectsUnknownSort(t *testing.T) {
	db := seedLogs(t)
	for _, sortKey := range []string{"ts_unix", "ts_unix DESC; DROP TABLE agent_turns", "1"} {
		if _, err := QueryLogs(context.Background(), db, LogQuery{Sort: LogSort(sortKey)}, nil); err == nil {
			t.Fatalf("sort %q must be rejected, not interpolated into SQL", sortKey)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_turns`).Scan(&count); err != nil || count != 7 {
		t.Fatalf("table state after rejected sorts: %d (%v)", count, err)
	}
	// The allow-listed keys do work, in both directions.
	for _, sortKey := range LogSorts() {
		for _, ascending := range []bool{true, false} {
			page, err := QueryLogs(context.Background(), db, LogQuery{Sort: LogSort(sortKey), Ascending: ascending, PageSize: 50}, nil)
			if err != nil {
				t.Fatalf("sort %q: %v", sortKey, err)
			}
			if len(page.Rows) != 7 {
				t.Fatalf("sort %q returned %d rows", sortKey, len(page.Rows))
			}
		}
	}
	if page := mustLogs(t, db, LogQuery{Sort: LogSortTokens, PageSize: 1}); page.Rows[0].RecordID != "a1:1" {
		t.Fatalf("tokens sort picked %s, want the largest row a1:1", page.Rows[0].RecordID)
	}
	if page := mustLogs(t, db, LogQuery{Ascending: true, PageSize: 1}); page.Rows[0].RecordID != "c2:1" {
		t.Fatalf("ascending time sort picked %s, want the oldest row c2:1", page.Rows[0].RecordID)
	}
}

// The three ways a Logs page can be empty are three different things to tell a
// user, so the API distinguishes them instead of returning one blank table.
func TestQueryLogsEmptyStatesAreDistinguishable(t *testing.T) {
	fresh, err := store.Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	page := mustLogs(t, fresh.SQL(), LogQuery{})
	if page.Empty == nil || page.Empty.Reason != EmptyNotIngested {
		t.Fatalf("nothing ingested yet: %+v", page.Empty)
	}
	if page.Empty.StoredTurns != 0 {
		t.Fatalf("stored_turns = %d, want 0", page.Empty.StoredTurns)
	}

	db := seedLogs(t)
	from := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2001, 1, 2, 0, 0, 0, 0, time.UTC)
	page = mustLogs(t, db, LogQuery{Filter: Filter{From: &from, To: &to, ToExclusive: true}})
	if page.Empty == nil || page.Empty.Reason != EmptyRange {
		t.Fatalf("empty time range: %+v", page.Empty)
	}
	if page.Empty.StoredTurns != 7 || page.Empty.TurnsInRange != 0 {
		t.Fatalf("empty range counts: %+v", page.Empty)
	}

	page = mustLogs(t, db, LogQuery{Filter: Filter{Harness: []string{"not-a-harness"}}})
	if page.Empty == nil || page.Empty.Reason != EmptyFiltered {
		t.Fatalf("filters matched nothing: %+v", page.Empty)
	}
	if page.Empty.TurnsInRange != 7 {
		t.Fatalf("turns_in_range = %d, want 7: the window itself is not empty", page.Empty.TurnsInRange)
	}

	if got := mustLogs(t, db, LogQuery{}); got.Empty != nil {
		t.Fatalf("a page with rows must not carry an empty state: %+v", got.Empty)
	}
}

func TestQueryLogsEstimatesThroughputFromPreviousTurnInSession(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "rate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	turn := func(at time.Duration, session, record string, output int64) model.Turn {
		return model.Turn{Timestamp: base.Add(at), Harness: "claude-code", SessionID: session,
			RecordID: record, EventKind: "assistant", SourceFile: "/evidence/" + session + ".jsonl", SourceLine: 1,
			Usage: model.NewSplitUsage(10, output, 0, 0, 0, nil, model.UsageObserved)}
	}
	turns := []model.Turn{
		turn(0, "s1", "s1:1", 100),
		// Half a second later, inside the same wall-clock second: ts_unix alone
		// would see no gap at all.
		turn(500*time.Millisecond, "s1", "s1:2", 50),
		turn(2500*time.Millisecond, "s1", "s1:3", 0),
		// Another session in between must not borrow s1's history.
		turn(time.Second, "s2", "s2:1", 40),
	}
	if _, err := db.InsertTurns(context.Background(), turns); err != nil {
		t.Fatal(err)
	}

	byID := map[string]LogRow{}
	for _, r := range mustLogs(t, db.SQL(), LogQuery{}).Rows {
		byID[r.RecordID] = r
	}
	if r := byID["s1:1"]; r.IntervalMS != nil || r.EstOutputTokensPerSecond != nil {
		t.Fatalf("a session's first turn has no previous turn to measure from: %+v", r)
	}
	if r := byID["s1:2"]; r.IntervalMS == nil || *r.IntervalMS != 500 ||
		r.EstOutputTokensPerSecond == nil || *r.EstOutputTokensPerSecond != 100 {
		t.Fatalf("want 500 ms and 100 tok/s, got %+v", r)
	}
	if r := byID["s1:3"]; r.IntervalMS == nil || *r.IntervalMS != 2000 || r.EstOutputTokensPerSecond != nil {
		t.Fatalf("no output means no throughput, not 0 tok/s: %+v", r)
	}
	if r := byID["s2:1"]; r.IntervalMS != nil {
		t.Fatalf("interval crossed a session boundary: %+v", r)
	}

	// A filter narrows the rows, not the history an interval is measured over.
	only := mustLogs(t, db.SQL(), LogQuery{Filter: Filter{Record: []string{"s1:2"}}}).Rows
	if len(only) != 1 || only[0].IntervalMS == nil || *only[0].IntervalMS != 500 {
		t.Fatalf("filtering changed the interval: %+v", only)
	}
}
