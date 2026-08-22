package query

// The Logs surface is a row-level read over the same normalized evidence the
// aggregations use. It is not process/stdout logging and not a proxy request
// log: every field below already exists in agent_turns, and nothing that a
// harness transcript does not persist is invented here.
//
// Three rules shape this file:
//
//   - Filtering, ordering and paging are pushed into DuckDB. A page is a page,
//     not the first N rows of a full result set that Go downloaded first.
//   - Unknown stays unknown. A dimension the source never wrote is NULL in the
//     store and null in the DTO, never "" or "unknown" or 0.
//   - A price is applied per query, never stored. Same rule as the overview.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/billyham07/harness-observer/internal/pricing"
)

// Page size bounds. The upper bound is a hard cap rather than a suggestion: a
// client asking for a million rows gets MaxLogPageSize and is told so through
// the page_size the response reports back.
const (
	DefaultLogPageSize = 50
	MaxLogPageSize     = 200
)

// LogSort is the allow-list of orderings. A client-supplied value that is not
// in this map is an error, never a string that reaches the SQL text.
type LogSort string

const (
	LogSortTime   LogSort = "time"
	LogSortTokens LogSort = "tokens"
	LogSortOutput LogSort = "output"
)

var logSortExpr = map[LogSort]string{
	LogSortTime:   "ts_unix",
	LogSortTokens: "(prompt_tokens + output_tokens)",
	LogSortOutput: "output_tokens",
}

// LogSorts lists the allowed sort keys, for flag help and error messages.
func LogSorts() []string {
	return []string{string(LogSortTime), string(LogSortTokens), string(LogSortOutput)}
}

// LogQuery is one page request. Filter is the same struct the overview,
// hierarchy and facets use, so a Logs page and the chart above it cannot drift
// into two different definitions of the same window.
type LogQuery struct {
	Filter Filter
	Sort   LogSort
	// Ascending flips the default newest-first order. The tie-breaker stays
	// ascending either way; it only has to be total and stable.
	Ascending bool
	Page      int // 1-based; 0 means the first page
	PageSize  int
}

// LogRow is one normalized event.
//
// Pointer fields are the point of this DTO. `tool_errors: null` and
// `tool_errors: 0` are different claims, and so are a NULL model and a model
// recorded as an empty string.
type LogRow struct {
	Timestamp       time.Time `json:"timestamp"`
	Harness         string    `json:"harness"`
	Client          *string   `json:"client"`
	Provider        *string   `json:"provider"`
	Model           *string   `json:"model"`
	ReasoningEffort *string   `json:"reasoning_effort"`
	Project         *string   `json:"project"`
	SessionID       string    `json:"session_id"`
	TurnID          *string   `json:"turn_id"`
	RecordID        string    `json:"record_id"`
	EventKind       string    `json:"event_kind"`
	UsageBasis      string    `json:"usage_basis"`
	UsageSemantics  string    `json:"usage_semantics"`

	InputTokens      int64 `json:"input_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	PromptTokens     int64 `json:"prompt_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`
	// ObservedTokens excludes reasoning, which is a subset of output in every
	// verified source format. See model.Turn.ObservedTokens.
	ObservedTokens    int64  `json:"observed_tokens"`
	NativeInputTokens *int64 `json:"native_input_tokens"`
	NativeTotalTokens *int64 `json:"native_total_tokens"`

	ToolCalls int64 `json:"tool_calls"`
	// ToolErrors is null when this harness's adapter does not observe
	// tool-result failures at all. A stored 0 from such an adapter means "not
	// observed", and rendering it as a green zero would be a lie the table
	// tells on every row. See ToolErrorEvidence and docs/log-evidence-capabilities.md.
	ToolErrors        *int64 `json:"tool_errors"`
	ToolErrorEvidence string `json:"tool_error_evidence"`

	// ReportedCostUSD is a number the source itself persisted. Null means the
	// source reports none; it is never filled in from the price catalog.
	ReportedCostUSD *float64 `json:"reported_cost_usd"`

	// SourceFile/SourceLine are the evidence location this row was normalized
	// from. They are metadata about where to look, and the API never reads or
	// returns the contents of that path.
	SourceFile string `json:"source_file"`
	SourceLine int64  `json:"source_line"`

	// EstimatedCost is present only when the request ran with a pricing
	// catalog. It is an API-equivalent estimate at published rates, not spend.
	EstimatedCost *LogCost `json:"estimated_cost,omitempty"`
}

// Evidence states for ToolErrors.
const (
	ToolErrorsObserved    = "observed"
	ToolErrorsUnavailable = "unavailable"
)

// toolErrorEvidence lists the harness families whose adapter actually
// normalizes tool-result failures today. Everything else reports
// "unavailable", because model.Turn.ToolErrors is an int and a stored 0 cannot
// distinguish "no failures" from "this adapter never looked".
//
// Add a family here in the same change that teaches its adapter to correlate
// tool results, not before.
var toolErrorEvidence = map[string]bool{
	"omp": true,
	"pi":  true,
}

// harnessFamily strips the frontend suffix: codex-desktop and codex-cli are one
// adapter and share its evidence capabilities.
func harnessFamily(harness string) string {
	if i := strings.IndexByte(harness, '-'); i > 0 {
		return harness[:i]
	}
	return harness
}

// LogCost is the per-row API-equivalent estimate plus the provenance that
// qualifies it: which rule matched, which catalog key, which catalog version.
//
// EstimatedCostUSD is a pointer so that an unpriced model reports null instead
// of $0.00. A model with no published rate is unpriced, not free.
type LogCost struct {
	EstimatedCostUSD  *float64 `json:"estimated_api_cost_usd"`
	CostInputUSD      float64  `json:"cost_input_usd"`
	CostCacheReadUSD  float64  `json:"cost_cache_read_usd"`
	CostCacheWriteUSD float64  `json:"cost_cache_write_usd"`
	CostOutputUSD     float64  `json:"cost_output_usd"`
	PricedTokens      int64    `json:"priced_tokens"`
	UnpricedTokens    int64    `json:"unpriced_tokens"`
	PricingCoverage   float64  `json:"pricing_coverage_ratio"`
	Basis             string   `json:"pricing_basis"`
	CacheBasis        string   `json:"cache_pricing_basis,omitempty"`
	// Heuristic is true when any bucket was priced from an inferred rate. It is
	// reported separately so a partly inferred number is never presented as a
	// fully published one.
	Heuristic      bool   `json:"heuristic_rates,omitempty"`
	Rule           string `json:"match_rule,omitempty"`
	CanonicalModel string `json:"canonical_model,omitempty"`
	CatalogSource  string `json:"catalog_source,omitempty"`
	CatalogVersion string `json:"catalog_version,omitempty"`
	Reason         string `json:"unpriced_reason,omitempty"`
}

// LogPage is one answered page plus everything a UI needs to render its state
// without guessing.
type LogPage struct {
	Rows     []LogRow `json:"rows"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Total    int64    `json:"total"`
	Pages    int64    `json:"pages"`
	HasMore  bool     `json:"has_more"`
	Sort     string   `json:"sort"`
	Order    string   `json:"order"`
	// PricingAvailable says whether estimated_cost could be computed at all, so
	// a missing estimate is never read as a zero one.
	PricingAvailable bool `json:"pricing_available"`
	// Empty is set only when Total is 0, and says which of the three empty
	// states this is. A UI that has to tell "nothing ingested yet" from "your
	// filters excluded everything" should not have to infer it from counts.
	Empty *LogEmptyState `json:"empty_state,omitempty"`
}

// Empty-state reasons.
const (
	EmptyNotIngested = "not-ingested"
	EmptyRange       = "empty-range"
	EmptyFiltered    = "filtered-out"
)

type LogEmptyState struct {
	Reason string `json:"reason"`
	// StoredTurns is the whole store, ignoring every filter including time.
	StoredTurns int64 `json:"stored_turns"`
	// TurnsInRange applies the time window only.
	TurnsInRange int64 `json:"turns_in_range"`
}

// logColumns is the projection. It is written out rather than SELECT *, so a
// future column cannot silently join the API surface: the Logs DTO stays a
// deliberate list, and content columns are not on it because they do not exist
// in the schema.
const logColumns = `ts_unix, harness, client, provider, model, reasoning_effort, project,
	session_id, record_id, turn_id, event_kind, usage_basis, usage_semantics,
	input_tokens, cache_read_tokens, cache_write_tokens, prompt_tokens,
	output_tokens, reasoning_tokens, native_input_tokens, native_total_tokens,
	tool_calls, tool_errors, cost_usd, source_file, source_line`

// normalize validates the sort key and clamps the paging values. It returns the
// effective values rather than mutating in place, so the caller's request stays
// what the caller asked for.
func (q LogQuery) normalize() (sort LogSort, page, pageSize int, err error) {
	sort = LogSort(strings.ToLower(strings.TrimSpace(string(q.Sort))))
	if sort == "" {
		sort = LogSortTime
	}
	if _, ok := logSortExpr[sort]; !ok {
		return "", 0, 0, fmt.Errorf("unknown log sort %q (known: %s)", q.Sort, strings.Join(LogSorts(), ", "))
	}
	page = q.Page
	if page < 1 {
		page = 1
	}
	pageSize = q.PageSize
	if pageSize <= 0 {
		pageSize = DefaultLogPageSize
	}
	if pageSize > MaxLogPageSize {
		pageSize = MaxLogPageSize
	}
	return sort, page, pageSize, nil
}

// QueryLogs answers one page of normalized events.
//
// The pricer is optional and query-time only: with a nil pricer the rows are
// identical minus estimated_cost, and no price is ever written to the store.
func QueryLogs(ctx context.Context, db *sql.DB, q LogQuery, pricer Pricer) (LogPage, error) {
	sortKey, page, pageSize, err := q.normalize()
	if err != nil {
		return LogPage{}, err
	}
	direction := "DESC"
	if q.Ascending {
		direction = "ASC"
	}
	out := LogPage{
		Rows:             []LogRow{},
		Page:             page,
		PageSize:         pageSize,
		Sort:             string(sortKey),
		Order:            strings.ToLower(direction),
		PricingAvailable: pricer != nil,
	}

	where, args := q.Filter.where()
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_turns"+where, args...).Scan(&out.Total); err != nil {
		return LogPage{}, err
	}
	if out.Total == 0 {
		empty, emptyErr := emptyState(ctx, db, q.Filter)
		if emptyErr != nil {
			return LogPage{}, emptyErr
		}
		out.Empty = empty
		return out, nil
	}
	out.Pages = (out.Total + int64(pageSize) - 1) / int64(pageSize)
	out.HasMore = int64(page)*int64(pageSize) < out.Total

	// The tie-breaker is the table's primary key, so the order is total: two
	// rows sharing a timestamp cannot swap between page 1 and page 2 and make a
	// row appear twice or never.
	q2 := fmt.Sprintf("SELECT %s FROM agent_turns%s ORDER BY %s %s, harness ASC, record_id ASC LIMIT ? OFFSET ?",
		logColumns, where, logSortExpr[sortKey], direction)
	pageArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)

	rows, err := db.QueryContext(ctx, q2, pageArgs...)
	if err != nil {
		return LogPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		row, scanErr := scanLogRow(rows)
		if scanErr != nil {
			return LogPage{}, scanErr
		}
		if pricer != nil {
			row.EstimatedCost = priceRow(pricer, row)
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return LogPage{}, err
	}
	return out, nil
}

// emptyState distinguishes the three ways a Logs page can be empty. It runs
// only when the filtered count is already known to be zero, so the extra two
// counts never touch the normal request path.
func emptyState(ctx context.Context, db *sql.DB, filter Filter) (*LogEmptyState, error) {
	state := &LogEmptyState{Reason: EmptyFiltered}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_turns").Scan(&state.StoredTurns); err != nil {
		return nil, err
	}
	if state.StoredTurns == 0 {
		state.Reason = EmptyNotIngested
		return state, nil
	}
	// Only the window, none of the dimension filters: that is what separates
	// "nothing happened then" from "your filters excluded it".
	window := Filter{From: filter.From, To: filter.To, ToExclusive: filter.ToExclusive}
	where, args := window.where()
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_turns"+where, args...).Scan(&state.TurnsInRange); err != nil {
		return nil, err
	}
	if state.TurnsInRange == 0 {
		state.Reason = EmptyRange
	}
	return state, nil
}

func scanLogRow(rows *sql.Rows) (LogRow, error) {
	var (
		r                                                       LogRow
		tsUnix                                                  int64
		client, provider, model, effort, project, turnID        sql.NullString
		nativeInput, nativeTotal                                sql.NullInt64
		reportedCost                                            sql.NullFloat64
		toolCalls, toolErrors, sourceLine                       int64
		eventKind, usageBasis, usageSemantics, sourceFile       string
		input, cacheRead, cacheWrite, prompt, output, reasoning int64
	)
	if err := rows.Scan(&tsUnix, &r.Harness, &client, &provider, &model, &effort, &project,
		&r.SessionID, &r.RecordID, &turnID, &eventKind, &usageBasis, &usageSemantics,
		&input, &cacheRead, &cacheWrite, &prompt, &output, &reasoning, &nativeInput, &nativeTotal,
		&toolCalls, &toolErrors, &reportedCost, &sourceFile, &sourceLine); err != nil {
		return LogRow{}, err
	}
	r.Timestamp = time.Unix(tsUnix, 0).UTC()
	r.Client = nullString(client)
	r.Provider = nullString(provider)
	r.Model = nullString(model)
	r.ReasoningEffort = nullString(effort)
	r.Project = nullString(project)
	r.TurnID = nullString(turnID)
	r.EventKind = eventKind
	r.UsageBasis = usageBasis
	r.UsageSemantics = usageSemantics
	r.InputTokens = input
	r.CacheReadTokens = cacheRead
	r.CacheWriteTokens = cacheWrite
	r.PromptTokens = prompt
	r.OutputTokens = output
	r.ReasoningTokens = reasoning
	r.ObservedTokens = prompt + output
	if nativeInput.Valid {
		v := nativeInput.Int64
		r.NativeInputTokens = &v
	}
	if nativeTotal.Valid {
		v := nativeTotal.Int64
		r.NativeTotalTokens = &v
	}
	r.ToolCalls = toolCalls
	// A positive count is evidence in itself: some adapter observed those
	// failures, whatever the capability table says about its family.
	if toolErrors > 0 || toolErrorEvidence[harnessFamily(r.Harness)] {
		v := toolErrors
		r.ToolErrors = &v
		r.ToolErrorEvidence = ToolErrorsObserved
	} else {
		r.ToolErrorEvidence = ToolErrorsUnavailable
	}
	if reportedCost.Valid {
		v := reportedCost.Float64
		r.ReportedCostUSD = &v
	}
	r.SourceFile = sourceFile
	r.SourceLine = sourceLine
	return r, nil
}

func nullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// priceRow estimates one row at query time and keeps the provenance attached to
// the number. An unresolved model yields a nil amount with a reason, never a
// zero that reads as free.
func priceRow(pricer Pricer, row LogRow) *LogCost {
	model := ""
	if row.Model != nil {
		model = *row.Model
	}
	c := pricer.Cost(model, pricing.Buckets{
		Input:      row.InputTokens,
		CacheRead:  row.CacheReadTokens,
		CacheWrite: row.CacheWriteTokens,
		Output:     row.OutputTokens,
	})
	cost := &LogCost{
		CostInputUSD:      c.Input,
		CostCacheReadUSD:  c.CacheRead,
		CostCacheWriteUSD: c.CacheWrite,
		CostOutputUSD:     c.Output,
		PricedTokens:      c.PricedTokens,
		UnpricedTokens:    c.UnpricedTokens,
		Heuristic:         c.Heuristic,
		Basis:             string(c.Match.Basis),
		CacheBasis:        string(c.Match.CacheBasis),
		Rule:              c.Match.Rule,
		CanonicalModel:    c.Match.CanonicalModel,
		CatalogSource:     c.Match.Source,
		CatalogVersion:    c.Match.SourceVersion,
		Reason:            c.Match.Reason,
	}
	if total := c.PricedTokens + c.UnpricedTokens; total > 0 {
		cost.PricingCoverage = float64(c.PricedTokens) / float64(total)
	}
	if c.PricedTokens > 0 {
		total := c.Total
		cost.EstimatedCostUSD = &total
	}
	return cost
}
