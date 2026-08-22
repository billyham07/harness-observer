package query

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/billyham07/harness-observer/internal/pricing"
)

// Pricer is the read side's view of the pricing catalog. Only the query layer
// depends on it, and only at query time.
type Pricer interface {
	Cost(model string, buckets pricing.Buckets) pricing.Cost
	Resolve(model string) pricing.Match
}

// Cost estimation is deliberately computed here rather than stored on
// agent_turns.
//
// Token counts are evidence: a session file said them, and they do not change.
// A published price is not evidence about this machine at all, it is a third
// party's price list that gets corrected, discounted and superseded. Stamping
// today's rate onto every stored row would make a pricing fix require
// re-reading session files, and would quietly turn "what OpenAI charged in
// April" into "what OpenAI charges now" for historical rows. So the store keeps
// tokens, the catalog keeps prices, and the two meet per query.
//
// The cost of that choice is one extra aggregation per request: the same
// filter, the same grouping, plus model, because a price only exists per model.
// The rows are then folded back onto the requested grouping.

// AggregateCost runs Aggregate and attaches API-equivalent cost estimates.
//
// It is a superset of Aggregate: with a nil pricer the result is identical.
func AggregateCost(ctx context.Context, db *sql.DB, req Request, pricer Pricer) ([]Row, error) {
	rows, err := Aggregate(ctx, db, req)
	if err != nil || pricer == nil || len(rows) == 0 {
		return rows, err
	}

	costReq := req
	costReq.GroupBy = withModel(req.GroupBy)
	// Limit and ordering belong to the presented rows, not to the pricing pass:
	// a top-100 table must still be priced from every model inside those 100
	// groups.
	costReq.Limit = 0
	costReq.OrderByTokens = false

	priced, err := Aggregate(ctx, db, costReq)
	if err != nil {
		return nil, err
	}

	// Key on the requested grouping only, so the extra model dimension folds
	// away again and a group that spans several models sums their costs.
	totals := make(map[string]*CostMetrics, len(rows))
	for _, r := range priced {
		key := groupKey(req.GroupBy, r)
		metrics := totals[key]
		if metrics == nil {
			metrics = &CostMetrics{}
			totals[key] = metrics
		}
		metrics.add(pricer.Cost(r.Group["model"], pricing.Buckets{
			Input:      r.InputTokens,
			CacheRead:  r.CacheRead,
			CacheWrite: r.CacheWrite,
			Output:     r.OutputTokens,
		}))
	}

	out := make([]Row, len(rows))
	for i, r := range rows {
		out[i] = r
		if metrics, ok := totals[groupKey(req.GroupBy, r)]; ok {
			out[i].CostMetrics = metrics.finish()
		}
	}
	return out, nil
}

func withModel(groupBy []string) []string {
	for _, g := range groupBy {
		if strings.EqualFold(strings.TrimSpace(g), "model") {
			return groupBy
		}
	}
	return append(append([]string{}, groupBy...), "model")
}

// groupKey identifies one presented row. The bucket is part of the key because
// a timeline row is a distinct group even when its dimensions repeat.
func groupKey(groupBy []string, r Row) string {
	parts := make([]string, 0, len(groupBy)+1)
	for _, g := range groupBy {
		parts = append(parts, r.Group[strings.ToLower(strings.TrimSpace(g))])
	}
	parts = append(parts, r.Bucket)
	// The separator is a rune that cannot appear in a dimension value read from
	// a session file, so two different groups cannot collide into one key.
	return strings.Join(parts, "\x00")
}

// CostMetrics is the API-equivalent cost estimate for one aggregated row.
//
// It is an estimate at published rates, not a bill: subscription and OAuth
// usage is not metered spend. ReportedCostUSD on Row stays separate, because a
// number a harness persisted and a number this catalog derived are different
// kinds of claim and must never be added together or substituted for each
// other.
type CostMetrics struct {
	EstimatedCostUSD float64 `json:"estimated_api_cost_usd"`
	// Composition of the estimate, in the same canonical buckets the tokens use.
	CostInputUSD      float64 `json:"cost_input_usd"`
	CostCacheReadUSD  float64 `json:"cost_cache_read_usd"`
	CostCacheWriteUSD float64 `json:"cost_cache_write_usd"`
	CostOutputUSD     float64 `json:"cost_output_usd"`
	// Coverage is token weighted on purpose. Resolving 99% of model names means
	// nothing if the unresolved 1% carries most of the tokens.
	PricedTokens    int64    `json:"priced_tokens"`
	UnpricedTokens  int64    `json:"unpriced_tokens"`
	PricingCoverage float64  `json:"pricing_coverage_ratio"`
	PricingBasis    []string `json:"pricing_basis,omitempty"`

	bases map[string]bool
}

func (m *CostMetrics) add(c pricing.Cost) {
	m.EstimatedCostUSD += c.Total
	m.CostInputUSD += c.Input
	m.CostCacheReadUSD += c.CacheRead
	m.CostCacheWriteUSD += c.CacheWrite
	m.CostOutputUSD += c.Output
	m.PricedTokens += c.PricedTokens
	m.UnpricedTokens += c.UnpricedTokens
	if m.bases == nil {
		m.bases = map[string]bool{}
	}
	// Both bases are recorded: a row priced from the catalog whose cache rates
	// were inferred is not the same claim as a fully published one.
	if c.PricedTokens > 0 {
		m.bases[string(c.Match.Basis)] = true
		if c.Heuristic {
			m.bases[string(pricing.BasisHeuristic)] = true
		}
	}
	if c.UnpricedTokens > 0 {
		m.bases[string(pricing.BasisUnpriced)] = true
	}
}

func (m *CostMetrics) finish() *CostMetrics {
	total := m.PricedTokens + m.UnpricedTokens
	if total > 0 {
		m.PricingCoverage = float64(m.PricedTokens) / float64(total)
	}
	m.PricingBasis = make([]string, 0, len(m.bases))
	for basis := range m.bases {
		m.PricingBasis = append(m.PricingBasis, basis)
	}
	sort.Strings(m.PricingBasis)
	return m
}

// Unpriced is one model whose rate could not be resolved, with the token volume
// that makes it worth caring about.
type Unpriced struct {
	Model    string `json:"observed_model"`
	Provider string `json:"provider,omitempty"`
	Harness  string `json:"harness,omitempty"`
	Tokens   int64  `json:"observed_tokens"`
	Turns    int64  `json:"turns"`
	Reason   string `json:"reason,omitempty"`
}

// UnpricedModels ranks the models the catalog could not price by how many
// tokens they account for, so the coverage gap that actually matters is the one
// at the top. It reports names and counters only, never content.
func UnpricedModels(ctx context.Context, db *sql.DB, filter Filter, pricer Pricer, limit int) ([]Unpriced, error) {
	if pricer == nil {
		return nil, nil
	}
	rows, err := Aggregate(ctx, db, Request{
		Filter:        filter,
		GroupBy:       []string{"harness", "provider", "model"},
		OrderByTokens: true,
	})
	if err != nil {
		return nil, err
	}
	var out []Unpriced
	for _, r := range rows {
		model := r.Group["model"]
		match := pricer.Resolve(model)
		if match.Priced() {
			continue
		}
		out = append(out, Unpriced{
			Model:    model,
			Provider: r.Group["provider"],
			Harness:  r.Group["harness"],
			Tokens:   r.ObservedTokens,
			Turns:    r.Turns,
			Reason:   match.Reason,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tokens > out[j].Tokens })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
