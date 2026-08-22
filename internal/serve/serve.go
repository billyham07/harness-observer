// Package serve exposes the local store over a boring HTTP/JSON surface plus a
// Prometheus text endpoint. It is a read side only: nothing here writes to the
// store or touches agent-owned session files.
package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/billyham07/harness-observer/internal/buildinfo"
	"github.com/billyham07/harness-observer/internal/pricing"
	"github.com/billyham07/harness-observer/internal/query"
	"github.com/billyham07/harness-observer/internal/store"
)

type Server struct {
	DB *store.DB
	// Pricing turns stored token evidence into an API-equivalent cost estimate
	// at query time. Nil simply leaves the cost fields off every response; the
	// store is never asked to hold a price.
	Pricing *pricing.Resolver
}

// pricer adapts the resolver to the query layer's interface, and keeps a nil
// resolver from becoming a non-nil interface holding a nil pointer.
func (s Server) pricer() query.Pricer {
	if s.Pricing == nil {
		return nil
	}
	return s.Pricing
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

// Register mounts the read-side API on an existing mux, so a process that also
// serves a UI does not have to duplicate the routing table.
func (s Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/query", s.query)
	mux.HandleFunc("/api/logs", s.logs)
	mux.HandleFunc("/api/hierarchy", s.hierarchy)
	mux.HandleFunc("/api/facets", s.facets)
	mux.HandleFunc("/api/pricing", s.pricingStatus)
	mux.HandleFunc("/api/pricing/unpriced", s.pricingUnpriced)
	mux.HandleFunc("/metrics", s.metrics)
}

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	version, err := s.DB.SchemaVersion()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"status": "ok", "version": buildinfo.Version(), "schema_version": version, "db": s.DB.Path()})
}

// query answers arbitrary time-range aggregations:
//
//	/api/query?from=2026-08-01T00:00:00Z&to=2026-08-20T23:59:59Z
//	          &group=harness,model&bucket=day&harness=codex-desktop&basis=observed
func (s Server) query(w http.ResponseWriter, r *http.Request) {
	req, err := RequestFromValues(r.URL.Query().Get, multi(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := query.AggregateCost(r.Context(), s.DB.SQL(), req, s.pricer())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"rows": rows, "count": len(rows)})
}

// pricingStatus answers "where did these dollar numbers come from": catalog
// source, version, freshness, refresh outcome and whether any rate was
// overridden or inferred.
func (s Server) pricingStatus(w http.ResponseWriter, r *http.Request) {
	if s.Pricing == nil {
		writeJSON(w, map[string]any{"available": false})
		return
	}
	writeJSON(w, map[string]any{"available": true, "status": s.Pricing.Status()})
}

// pricingUnpriced ranks unresolved models by token volume, so the coverage gap
// worth fixing is the one at the top of the list. Model names and counters
// only: no prompts, no content.
func (s Server) pricingUnpriced(w http.ResponseWriter, r *http.Request) {
	req, err := RequestFromValues(r.URL.Query().Get, multi(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	models, err := query.UnpricedModels(r.Context(), s.DB.SQL(), req.Filter, s.pricer(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var tokens int64
	for _, m := range models {
		tokens += m.Tokens
	}
	writeJSON(w, map[string]any{"models": models, "count": len(models), "observed_tokens": tokens})
}

// hierarchy returns the observed harness -> model -> reasoning_effort tuples
// under the current filter. It is a separate endpoint from /api/facets because
// these three levels are correlated and must never be offered independently.
func (s Server) hierarchy(w http.ResponseWriter, r *http.Request) {
	req, err := RequestFromValues(r.URL.Query().Get, multi(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h, err := query.BuildHierarchy(r.Context(), s.DB.SQL(), req.Filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, h)
}

// facets returns the remaining flat dimensions, cross-filtered by everything
// except their own selection.
func (s Server) facets(w http.ResponseWriter, r *http.Request) {
	req, err := RequestFromValues(r.URL.Query().Get, multi(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	facets, err := query.BuildFacets(r.Context(), s.DB.SQL(), req.Filter, splitAll(r.URL.Query()["dimension"]))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"facets": facets})
}

func multi(r *http.Request) func(string) []string {
	return func(key string) []string {
		// Comma is a convenience separator for flat dimensions only. A
		// selection tuple is never split on it: a model name may contain a
		// comma, and splitting one would silently change which tuple is meant.
		if key == "sel" {
			return r.URL.Query()[key]
		}
		return splitAll(r.URL.Query()[key])
	}
}

// RequestFromValues builds a query request from a flat key/value source. It is
// shared by the HTTP handler and the CLI so both honor identical semantics.
func RequestFromValues(get func(string) string, getAll func(string) []string) (query.Request, error) {
	var req query.Request
	from, err := ParseTime(get("from"))
	if err != nil {
		return req, err
	}
	to, err := ParseTime(get("to"))
	if err != nil {
		return req, err
	}
	selection, err := ParseSelection(getAll("sel"))
	if err != nil {
		return req, err
	}
	req.Filter = query.Filter{
		From:      from,
		To:        to,
		Harness:   getAll("harness"),
		Client:    getAll("client"),
		Provider:  getAll("provider"),
		Model:     getAll("model"),
		Effort:    getAll("effort"),
		Project:   getAll("project"),
		Basis:     getAll("basis"),
		Semantics: getAll("semantics"),
		EventKind: getAll("event_kind"),
		Selection: selection,
		// Exact identity lookups and the model/project search live on the
		// shared filter, so the facet and hierarchy endpoints stay consistent
		// with a Logs page that has an id or a search term applied.
		Session: getAll("session"),
		Record:  getAll("record"),
		Turn:    getAll("turn"),
		Search:  strings.TrimSpace(get("q")),
	}
	// bound picks the upper-bound semantics. The aggregations default to the
	// inclusive bound they were built with; /api/logs defaults to half-open.
	switch bound := strings.ToLower(strings.TrimSpace(get("bound"))); bound {
	case "", "inclusive":
	case "half-open", "exclusive":
		req.Filter.ToExclusive = true
	default:
		return req, fmt.Errorf("invalid bound %q (use inclusive or half-open)", bound)
	}
	req.GroupBy = getAll("group")
	req.Bucket = query.Bucket(strings.TrimSpace(get("bucket")))
	if v := strings.TrimSpace(get("limit")); v != "" {
		n, convErr := strconv.Atoi(v)
		if convErr != nil {
			return req, fmt.Errorf("invalid limit %q", v)
		}
		req.Limit = n
	}
	req.OrderByTokens = strings.EqualFold(strings.TrimSpace(get("order")), "tokens")
	return req, nil
}

// ParseSelection reads the hierarchical selector from repeated values of the
// form harness[|model[|effort]].
func ParseSelection(values []string) ([]query.Tuple, error) {
	var out []query.Tuple
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			continue
		}
		t, err := query.ParseTuple(v)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// ParseTime accepts RFC3339 and a few local-time shorthands so a time range can
// be typed by hand without ceremony.
func ParseTime(v string) (*time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("invalid time %q (use RFC3339 or YYYY-MM-DD[THH:MM[:SS]])", v)
}

func splitAll(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// metrics renders the Prometheus text exposition format by hand: one small
// dependency-free endpoint beats pulling in a client library for six series.
//
// Cardinality is bounded on purpose. session_id, project and cwd are NOT
// labels; they stay queryable through SQLite where high cardinality is cheap.
func (s Server) metrics(w http.ResponseWriter, r *http.Request) {
	rows, err := query.AggregateCost(r.Context(), s.DB.SQL(), query.Request{
		GroupBy: []string{"harness", "client", "provider", "model", "basis"},
	}, s.pricer())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	version, _ := s.DB.SchemaVersion()

	b := &strings.Builder{}
	fmt.Fprintf(b, "# HELP harness_observer_schema_version Local store schema version.\n# TYPE harness_observer_schema_version gauge\nharness_observer_schema_version %d\n", version)

	writeMetric(b, "harness_observer_tokens_total", "counter", "Normalized tokens by canonical bucket.", rows, func(r query.Row) map[string]int64 {
		return map[string]int64{
			"input":       r.InputTokens,
			"cache_read":  r.CacheRead,
			"cache_write": r.CacheWrite,
			"output":      r.OutputTokens,
			"reasoning":   r.Reasoning,
		}
	})
	writeSimple(b, "harness_observer_turns_total", "counter", "Normalized usage records observed.", rows, func(r query.Row) float64 { return float64(r.Turns) })
	writeSimple(b, "harness_observer_sessions_total", "gauge", "Distinct sessions observed.", rows, func(r query.Row) float64 { return float64(r.Sessions) })
	writeSimple(b, "harness_observer_tool_calls_total", "counter", "Tool calls attributed to a usage record.", rows, func(r query.Row) float64 { return float64(r.ToolCalls) })
	writeSimple(b, "harness_observer_tool_errors_total", "counter", "Tool errors reported by the source format.", rows, func(r query.Row) float64 { return float64(r.ToolErrors) })
	writeSimple(b, "harness_observer_cost_usd_total", "counter", "Cost reported by the source, when the source reports one.", rows, func(r query.Row) float64 { return r.CostUSD })
	// An API-equivalent estimate at published rates, not billed spend. Exported
	// next to the unpriced token count so a dashboard can never present the
	// estimate without the coverage that qualifies it.
	writeSimple(b, "harness_observer_estimated_api_cost_usd_total", "counter", "API-equivalent cost estimated from public model rates. Not actual subscription spend.", rows, func(r query.Row) float64 {
		if r.CostMetrics == nil {
			return 0
		}
		return r.EstimatedCostUSD
	})
	writeSimple(b, "harness_observer_unpriced_tokens_total", "counter", "Observed tokens the pricing catalog could not price.", rows, func(r query.Row) float64 {
		if r.CostMetrics == nil {
			return 0
		}
		return float64(r.UnpricedTokens)
	})
	writeSimple(b, "harness_observer_last_activity_timestamp_seconds", "gauge", "Timestamp of the newest observed usage record.", rows, func(r query.Row) float64 {
		if r.LastSeen.IsZero() {
			return 0
		}
		return float64(r.LastSeen.Unix())
	})

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func writeSimple(b *strings.Builder, name, kind, help string, rows []query.Row, value func(query.Row) float64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	for _, r := range rows {
		fmt.Fprintf(b, "%s{%s} %g\n", name, labels(r, nil), value(r))
	}
}

func writeMetric(b *strings.Builder, name, kind, help string, rows []query.Row, buckets func(query.Row) map[string]int64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	for _, r := range rows {
		values := buckets(r)
		names := make([]string, 0, len(values))
		for k := range values {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, bucket := range names {
			fmt.Fprintf(b, "%s{%s} %d\n", name, labels(r, map[string]string{"bucket": bucket}), values[bucket])
		}
	}
}

func labels(r query.Row, extra map[string]string) string {
	pairs := map[string]string{
		"harness":     r.Group["harness"],
		"client":      r.Group["client"],
		"provider":    r.Group["provider"],
		"model":       r.Group["model"],
		"usage_basis": r.Group["basis"],
	}
	for k, v := range extra {
		pairs[k] = v
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, escapeLabel(pairs[k])))
	}
	return strings.Join(parts, ",")
}

func escapeLabel(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return strings.ReplaceAll(v, `"`, `\"`)
}

// ListenAndServe binds to addr and serves until ctx is done.
func (s Server) ListenAndServe(ctx context.Context, addr string) error {
	// One best-effort catalog refresh at startup, never on the request path: a
	// slow network must not be able to delay a page that already has prices.
	if s.Pricing != nil {
		s.Pricing.MaybeRefresh(ctx)
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
