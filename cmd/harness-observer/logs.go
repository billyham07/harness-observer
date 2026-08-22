package main

// The logs command is the row-level counterpart to query. It answers "what
// exactly happened, in order" instead of "how much in total", and it goes
// through serve.LogQueryFromValues so the CLI, /api/logs and the dashboard
// cannot drift into three different filter semantics.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/billyham07/harness-observer/internal/query"
	"github.com/billyham07/harness-observer/internal/serve"
	"github.com/billyham07/harness-observer/internal/store"
)

func runLogs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	dbPath := fs.String("db", "", "DuckDB path (default: per-user data dir, or $HARNESS_OBSERVER_DB)")
	since, until := windowFlags(fs)
	bound := fs.String("bound", "half-open", "upper bound semantics: half-open for [since,until), or inclusive")
	harness := fs.String("harness", "", "filter, comma separated")
	client := fs.String("client", "", "filter, comma separated")
	provider := fs.String("provider", "", "filter, comma separated")
	modelFilter := fs.String("model", "", "filter, comma separated")
	effort := fs.String("effort", "", "filter by reasoning effort, comma separated")
	project := fs.String("project", "", "filter, comma separated")
	basis := fs.String("basis", "", "filter: observed, derived-delta, estimated")
	semantics := fs.String("semantics", "", "filter: split-cache, input-includes-cache-read")
	eventKind := fs.String("event-kind", "", "filter: assistant, tool-llm, compaction, branch-summary")
	strict := fs.Bool("strict", false, "exclude usage_basis=estimated")
	session := fs.String("session", "", "exact session id, comma separated")
	turn := fs.String("turn", "", "exact turn id, comma separated")
	record := fs.String("record", "", "exact record id, comma separated")
	search := fs.String("search", "", "case-insensitive substring of model or project (no content is stored or searched)")
	sortKey := fs.String("sort", string(query.LogSortTime), "sort key: "+strings.Join(query.LogSorts(), ", "))
	dir := fs.String("dir", "desc", "sort direction: desc or asc")
	page := fs.Int("page", 1, "1-based page number")
	limit := fs.Int("limit", query.DefaultLogPageSize, fmt.Sprintf("rows per page (hard cap %d)", query.MaxLogPageSize))
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	withCost := fs.Bool("cost", false, "add the API-equivalent cost column (an estimate at published rates, not billed spend)")
	buildPricing := pricingFlags(fs)
	var selection stringList
	fs.Var(&selection, "select", "hierarchy selection harness[|model[|effort]], repeatable; levels stay correlated")
	if err := fs.Parse(args); err != nil {
		return err
	}

	values := map[string]string{
		"from": *since, "to": *until, "bound": *bound,
		"harness": *harness, "client": *client, "provider": *provider, "model": *modelFilter,
		"effort": *effort, "project": *project, "basis": *basis, "semantics": *semantics,
		"event_kind": *eventKind, "session": *session, "turn": *turn, "record": *record,
		"q": *search, "sort": *sortKey, "dir": *dir,
		"page": fmt.Sprint(*page), "page_size": fmt.Sprint(*limit),
	}
	q, err := serve.LogQueryFromValues(
		func(k string) string { return values[k] },
		func(k string) []string {
			if k == "sel" {
				return selection
			}
			return splitCSV(values[k])
		},
	)
	if err != nil {
		return err
	}
	if *strict && len(q.Filter.Basis) == 0 {
		q.Filter.Basis = []string{"observed", "derived-delta"}
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	// JSON always carries the estimate; the table only when asked, so the
	// default output stays about token evidence.
	var pricer query.Pricer
	if *withCost || *asJSON {
		resolver, buildErr := buildPricing()
		if buildErr != nil {
			return buildErr
		}
		pricer = resolver
	}
	result, err := query.QueryLogs(ctx, db.SQL(), q, pricer)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	return printLogTable(os.Stdout, result, *withCost)
}

func printLogTable(w io.Writer, page query.LogPage, withCost bool) error {
	if page.Empty != nil {
		_, err := fmt.Fprintln(w, emptyStateMessage(*page.Empty))
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := []string{"time", "harness", "model", "effort", "project", "event",
		"input", "cache_read", "cache_write", "output", "tools", "tool_err", "session", "record"}
	if withCost {
		header = append(header, "est_usd", "coverage")
	}
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range page.Rows {
		cells := []string{
			r.Timestamp.Local().Format(time.RFC3339),
			r.Harness,
			orDash(r.Model),
			orDash(r.ReasoningEffort),
			orDash(r.Project),
			r.EventKind,
			fmt.Sprint(r.InputTokens), fmt.Sprint(r.CacheReadTokens), fmt.Sprint(r.CacheWriteTokens),
			fmt.Sprint(r.OutputTokens), fmt.Sprint(r.ToolCalls),
			// "-" is not zero: this adapter does not observe tool failures.
			intOrDash(r.ToolErrors),
			r.SessionID,
			r.RecordID,
		}
		if withCost {
			cells = append(cells, logCostCells(r)...)
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	fmt.Fprintln(tw)
	fmt.Fprintf(tw, "page %d/%d\t%d events\tpage size %d\tsorted by %s %s\n",
		page.Page, max64(page.Pages, 1), page.Total, page.PageSize, page.Sort, page.Order)
	if withCost {
		fmt.Fprintln(tw, "est_usd is an API-equivalent estimate at published rates, not actual subscription spend.")
		fmt.Fprintln(tw, "\"-\" means unknown: an unpriced model, or a metric this adapter does not observe. It is never a zero.")
	}
	return tw.Flush()
}

func emptyStateMessage(state query.LogEmptyState) string {
	switch state.Reason {
	case query.EmptyNotIngested:
		return "no events stored yet. Run \"harness-observer ingest\" (or \"harness-observer web\") to scan local session files."
	case query.EmptyRange:
		return fmt.Sprintf("no events in this time range. The store holds %d events outside it.", state.StoredTurns)
	default:
		return fmt.Sprintf("no events matched these filters. %d events are in the selected time range.", state.TurnsInRange)
	}
}

func logCostCells(r query.LogRow) []string {
	if r.EstimatedCost == nil || r.EstimatedCost.EstimatedCostUSD == nil {
		return []string{"-", "-"}
	}
	return []string{
		fmt.Sprintf("%.6f", *r.EstimatedCost.EstimatedCostUSD),
		fmt.Sprintf("%.0f%%", r.EstimatedCost.PricingCoverage*100),
	}
}

func orDash(v *string) string {
	if v == nil || *v == "" {
		return "-"
	}
	return *v
}

func intOrDash(v *int64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprint(*v)
}

func max64(v, floor int64) int64 {
	if v < floor {
		return floor
	}
	return v
}
