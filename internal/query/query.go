// Package query is the read side over the local store. Arbitrary time ranges
// are a first-class filter here, not a preset list of dashboard ranges.
package query

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Filter is the full filter surface. Every slice field is an OR within itself
// and an AND against the other fields. Empty slices mean "no restriction".
type Filter struct {
	From      *time.Time
	To        *time.Time
	Harness   []string
	Client    []string
	Provider  []string
	Model     []string
	Effort    []string
	Project   []string
	Basis     []string
	Semantics []string
	EventKind []string

	// ToExclusive makes the upper bound half-open, [From,To). The row-level
	// Logs surface sets it so consecutive windows tile without overlapping or
	// dropping a second. The aggregations keep the inclusive bound they were
	// built with: their callers pass human end-of-day bounds like 23:59:59,
	// and silently excluding that second would change published numbers.
	ToExclusive bool

	// Exact identity lookups. These are equality matches, not searches: they
	// are how a specific event is pulled out of the store by an id that
	// something else (a table row, a bug report, another tool) already knows.
	Session []string
	Record  []string
	Turn    []string

	// Search is a case-insensitive substring match over model and project only.
	// Those are the only two free-text dimensions the store holds; there is no
	// prompt, response or tool payload to search, by design.
	Search string

	// Selection is the hierarchical harness -> model -> reasoning_effort
	// selector. It is deliberately NOT three independent lists: each entry is
	// one observed tuple and the entries are OR'd, so selecting sol/high and
	// luna/max cannot be widened into sol/max by the query layer.
	Selection []Tuple
}

// Tuple is one node of the harness -> model -> reasoning_effort hierarchy.
//
// A nil Model means "any model under this harness" (the harness node itself was
// selected); a non-nil Model pointing at "" means the rows whose model is
// unknown, which is a different thing. Effort follows the same rule. That
// distinction is what lets a parent node and an explicit unknown child both be
// expressed without a magic sentinel value.
type Tuple struct {
	Harness string  `json:"harness"`
	Model   *string `json:"model,omitempty"`
	Effort  *string `json:"effort,omitempty"`
}

// String renders a tuple in the wire/CLI form harness[|model[|effort]].
// A present-but-empty segment is the unknown value; an absent segment is "any".
func (t Tuple) String() string {
	parts := []string{escapeSegment(t.Harness)}
	if t.Model != nil {
		parts = append(parts, escapeSegment(*t.Model))
		if t.Effort != nil {
			parts = append(parts, escapeSegment(*t.Effort))
		}
	}
	return strings.Join(parts, "|")
}

// Depth is how specific the tuple is: 1 harness, 2 harness+model, 3 full.
func (t Tuple) Depth() int {
	switch {
	case t.Model == nil:
		return 1
	case t.Effort == nil:
		return 2
	default:
		return 3
	}
}

// ParseTuple reads harness[|model[|effort]]. Backslash escapes a literal pipe
// so a value containing one is still expressible.
func ParseTuple(v string) (Tuple, error) {
	segments, err := splitSegments(v)
	if err != nil {
		return Tuple{}, err
	}
	if len(segments) == 0 || strings.TrimSpace(segments[0]) == "" {
		return Tuple{}, fmt.Errorf("selection %q has no harness", v)
	}
	if len(segments) > 3 {
		return Tuple{}, fmt.Errorf("selection %q has %d levels, want harness[|model[|effort]]", v, len(segments))
	}
	t := Tuple{Harness: segments[0]}
	if len(segments) > 1 {
		model := segments[1]
		t.Model = &model
	}
	if len(segments) > 2 {
		effort := segments[2]
		t.Effort = &effort
	}
	return t, nil
}

func splitSegments(v string) ([]string, error) {
	var (
		out     []string
		current strings.Builder
		escaped bool
	)
	for _, r := range v {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '|':
			out = append(out, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if escaped {
		return nil, fmt.Errorf("selection %q ends with a dangling escape", v)
	}
	out = append(out, current.String())
	return out, nil
}

func escapeSegment(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	return strings.ReplaceAll(v, "|", "\\|")
}

// Bucket is an optional time grouping. None keeps the result flat.
type Bucket string

const (
	BucketNone  Bucket = ""
	BucketHour  Bucket = "hour"
	BucketDay   Bucket = "day"
	BucketWeek  Bucket = "week"
	BucketMonth Bucket = "month"
)

// bucketExpr truncates in UTC. The connection pins TimeZone='UTC', so a stored
// series does not shift when the host's offset changes.
var bucketExpr = map[Bucket]string{
	BucketHour:  `strftime(date_trunc('hour', to_timestamp(ts_unix)), '%Y-%m-%dT%H:00:00Z')`,
	BucketDay:   `strftime(date_trunc('day', to_timestamp(ts_unix)), '%Y-%m-%dT00:00:00Z')`,
	BucketWeek:  `strftime(date_trunc('week', to_timestamp(ts_unix)), '%Y-W%W')`,
	BucketMonth: `strftime(date_trunc('month', to_timestamp(ts_unix)), '%Y-%m')`,
}

// groupColumns are the only groupable dimensions; the map also prevents SQL
// injection through a user-supplied --group-by value.
var groupColumns = map[string]string{
	"harness":    "harness",
	"client":     "client",
	"provider":   "provider",
	"model":      "model",
	"effort":     "reasoning_effort",
	"project":    "project",
	"event_kind": "event_kind",
	"basis":      "usage_basis",
	"semantics":  "usage_semantics",
	"session":    "session_id",
}

func GroupDimensions() []string {
	out := make([]string, 0, len(groupColumns))
	for k := range groupColumns {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type Request struct {
	Filter  Filter
	GroupBy []string
	Bucket  Bucket
	Limit   int
	// OrderByTokens sorts by total observed tokens instead of the group key.
	OrderByTokens bool
}

type Row struct {
	Group          map[string]string `json:"group,omitempty"`
	Bucket         string            `json:"bucket,omitempty"`
	Turns          int64             `json:"turns"`
	Sessions       int64             `json:"sessions"`
	InputTokens    int64             `json:"input_tokens"`
	CacheRead      int64             `json:"cache_read_tokens"`
	CacheWrite     int64             `json:"cache_write_tokens"`
	PromptTokens   int64             `json:"prompt_tokens"`
	OutputTokens   int64             `json:"output_tokens"`
	Reasoning      int64             `json:"reasoning_tokens"`
	ObservedTokens int64             `json:"observed_tokens"`
	ToolCalls      int64             `json:"tool_calls"`
	ToolErrors     int64             `json:"tool_errors"`
	// CostUSD is cost the source itself persisted, summed. It is evidence, not
	// an estimate: only some harnesses record it, and it is never synthesized
	// from a price list. reported_cost_usd is the same number under the name
	// that says what it is; cost_usd stays for existing consumers.
	CostUSD         float64 `json:"cost_usd"`
	ReportedCostUSD float64 `json:"reported_cost_usd"`
	// CostMetrics is the API-equivalent estimate, present only when the query
	// ran with a pricing catalog. It is deliberately a separate concept from
	// the reported cost above.
	*CostMetrics
	CacheHitRatio float64   `json:"cache_hit_ratio"`
	FirstSeen     time.Time `json:"first_seen"`
	LastSeen      time.Time `json:"last_seen"`
}

func (f Filter) where() (string, []any) {
	var clauses []string
	var args []any
	if f.From != nil {
		clauses = append(clauses, "ts_unix >= ?")
		args = append(args, f.From.UTC().Unix())
	}
	if f.To != nil {
		bound := "ts_unix <= ?"
		if f.ToExclusive {
			bound = "ts_unix < ?"
		}
		clauses = append(clauses, bound)
		args = append(args, f.To.UTC().Unix())
	}
	add := func(column string, values []string) {
		if len(values) == 0 {
			return
		}
		placeholders := make([]string, len(values))
		for i, v := range values {
			placeholders[i] = "?"
			args = append(args, v)
		}
		clauses = append(clauses, fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ",")))
	}
	add("harness", f.Harness)
	add("client", f.Client)
	add("provider", f.Provider)
	add("model", f.Model)
	add("reasoning_effort", f.Effort)
	add("project", f.Project)
	add("usage_basis", f.Basis)
	add("usage_semantics", f.Semantics)
	add("event_kind", f.EventKind)
	add("session_id", f.Session)
	add("record_id", f.Record)
	add("turn_id", f.Turn)
	if term := strings.TrimSpace(f.Search); term != "" {
		// Parameterized on both sides, with LIKE's own wildcards escaped so a
		// value containing % or _ searches for those characters instead of
		// quietly matching everything.
		pattern := "%" + escapeLike(term) + "%"
		clauses = append(clauses, `(COALESCE(model, '') ILIKE ? ESCAPE '\' OR COALESCE(project, '') ILIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if clause, tupleArgs := selectionClause(f.Selection); clause != "" {
		clauses = append(clauses, clause)
		args = append(args, tupleArgs...)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// escapeLike neutralizes LIKE metacharacters in a user-supplied search term.
// The escape character itself goes first, or escaping the wildcards would then
// escape those escapes.
func escapeLike(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "%", `\%`)
	return strings.ReplaceAll(v, "_", `\_`)
}

// selectionClause turns the hierarchy selection into one OR of AND groups.
//
// The correlation between the levels is the whole point:
//
//	(harness='codex-desktop' AND model='sol' AND effort='high')
//	OR (harness='codex-desktop' AND model='luna' AND effort='max')
//
// Flattening this into harness IN (...) AND model IN (...) AND effort IN (...)
// would also match sol/max and luna/high, which is a different question.
func selectionClause(tuples []Tuple) (string, []any) {
	if len(tuples) == 0 {
		return "", nil
	}
	var (
		groups []string
		args   []any
	)
	for _, t := range tuples {
		parts := []string{"harness = ?"}
		args = append(args, t.Harness)
		if t.Model != nil {
			parts = append(parts, "COALESCE(model, '') = ?")
			args = append(args, *t.Model)
		}
		if t.Effort != nil {
			parts = append(parts, "COALESCE(reasoning_effort, '') = ?")
			args = append(args, *t.Effort)
		}
		groups = append(groups, "("+strings.Join(parts, " AND ")+")")
	}
	return "(" + strings.Join(groups, " OR ") + ")", args
}

// Aggregate runs one grouped aggregation. Unknown group dimensions are an
// error rather than a silently ignored filter.
func Aggregate(ctx context.Context, db *sql.DB, req Request) ([]Row, error) {
	var selects, groups []string
	for _, g := range req.GroupBy {
		col, ok := groupColumns[strings.ToLower(strings.TrimSpace(g))]
		if !ok {
			return nil, fmt.Errorf("unknown group dimension %q (known: %s)", g, strings.Join(GroupDimensions(), ", "))
		}
		selects = append(selects, fmt.Sprintf("COALESCE(%s, '') AS g_%s", col, col))
		groups = append(groups, col)
	}
	if req.Bucket != BucketNone {
		expr, ok := bucketExpr[req.Bucket]
		if !ok {
			return nil, fmt.Errorf("unknown time bucket %q", req.Bucket)
		}
		selects = append(selects, expr+" AS bucket")
		groups = append(groups, expr)
	}

	metrics := `
  COUNT(*) AS turns,
  COUNT(DISTINCT harness || ':' || session_id) AS sessions,
  COALESCE(SUM(input_tokens),0), COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_write_tokens),0),
  COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(reasoning_tokens),0),
  COALESCE(SUM(tool_calls),0), COALESCE(SUM(tool_errors),0), COALESCE(SUM(cost_usd),0),
  COALESCE(MIN(ts_unix),0), COALESCE(MAX(ts_unix),0)`

	where, args := req.Filter.where()
	sb := &strings.Builder{}
	sb.WriteString("SELECT ")
	if len(selects) > 0 {
		sb.WriteString(strings.Join(selects, ", ") + ",")
	}
	sb.WriteString(metrics)
	sb.WriteString(" FROM agent_turns")
	sb.WriteString(where)
	if len(groups) > 0 {
		sb.WriteString(" GROUP BY " + strings.Join(groups, ", "))
	}
	switch {
	case req.OrderByTokens:
		sb.WriteString(" ORDER BY (COALESCE(SUM(prompt_tokens),0) + COALESCE(SUM(output_tokens),0)) DESC")
	case len(groups) > 0:
		sb.WriteString(" ORDER BY " + strings.Join(groups, ", "))
	}
	if req.Limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", req.Limit))
	}

	rows, err := db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		groupValues := make([]string, len(req.GroupBy))
		scan := make([]any, 0, len(groupValues)+14)
		for i := range groupValues {
			scan = append(scan, &groupValues[i])
		}
		var bucket string
		if req.Bucket != BucketNone {
			scan = append(scan, &bucket)
		}
		var r Row
		var firstUnix, lastUnix int64
		scan = append(scan, &r.Turns, &r.Sessions, &r.InputTokens, &r.CacheRead, &r.CacheWrite,
			&r.PromptTokens, &r.OutputTokens, &r.Reasoning, &r.ToolCalls, &r.ToolErrors, &r.CostUSD,
			&firstUnix, &lastUnix)
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		if len(req.GroupBy) > 0 {
			r.Group = make(map[string]string, len(req.GroupBy))
			for i, g := range req.GroupBy {
				r.Group[strings.ToLower(strings.TrimSpace(g))] = groupValues[i]
			}
		}
		r.Bucket = bucket
		r.ObservedTokens = r.PromptTokens + r.OutputTokens
		r.ReportedCostUSD = r.CostUSD
		if r.PromptTokens > 0 {
			r.CacheHitRatio = float64(r.CacheRead) / float64(r.PromptTokens)
		}
		if firstUnix > 0 {
			r.FirstSeen = time.Unix(firstUnix, 0).UTC()
		}
		if lastUnix > 0 {
			r.LastSeen = time.Unix(lastUnix, 0).UTC()
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
