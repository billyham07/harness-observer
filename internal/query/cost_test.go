package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/billyham07/harness-observer/internal/model"
	"github.com/billyham07/harness-observer/internal/pricing"
	"github.com/billyham07/harness-observer/internal/store"
)

func rate(v float64) *float64 { return &v }

// testPricer prices two synthetic models through the real resolver, using rate
// overrides so the assertions do not move when the bundled catalog is
// regenerated. "unpriced-model" is deliberately absent.
func testPricer(t *testing.T) *pricing.Resolver {
	t.Helper()
	r, err := pricing.New(pricing.Options{
		CachePath: filepath.Join(t.TempDir(), "pricing-catalog.json"),
		Overrides: map[string]pricing.Rates{
			// $1 per million input, $10 per million output, $0.10 per million
			// cache read, $1.25 per million cache write.
			"priced-a": {Input: rate(1e-6), Output: rate(10e-6), CacheRead: rate(0.1e-6), CacheWrite: rate(1.25e-6)},
			"priced-b": {Input: rate(2e-6), Output: rate(20e-6), CacheRead: rate(0.2e-6), CacheWrite: rate(2.5e-6)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// seedCost stores one harness whose turns span a priced model, a second priced
// model and a model with no published rate.
func seedCost(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "cost.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	at := func(day int) time.Time { return time.Date(2026, 8, day, 9, 0, 0, 0, time.UTC) }
	turns := []model.Turn{
		{Timestamp: at(18), Harness: "hx", Provider: "p", Model: "priced-a", ReasoningEffort: "high",
			SessionID: "s1", RecordID: "s1:1", EventKind: "assistant",
			// 1M fresh input, 2M cache read, 0 cache write, 100k output of
			// which 60k is reasoning.
			Usage: model.NewSplitUsage(1_000_000, 100_000, 2_000_000, 0, 60_000, nil, model.UsageObserved)},
		{Timestamp: at(19), Harness: "hx", Provider: "p", Model: "priced-b", ReasoningEffort: "max",
			SessionID: "s2", RecordID: "s2:1", EventKind: "assistant",
			Usage: model.NewSplitUsage(1_000_000, 100_000, 0, 0, 0, nil, model.UsageObserved)},
		{Timestamp: at(20), Harness: "hx", Provider: "p", Model: "unpriced-model", ReasoningEffort: "high",
			SessionID: "s3", RecordID: "s3:1", EventKind: "assistant",
			Usage: model.NewSplitUsage(500_000, 500_000, 0, 0, 0, nil, model.UsageObserved)},
	}
	if _, err := db.InsertTurns(context.Background(), turns); err != nil {
		t.Fatal(err)
	}
	return db.SQL()
}

func TestAggregateCostWithoutPricerMatchesAggregate(t *testing.T) {
	db := seedCost(t)
	req := Request{GroupBy: []string{"model"}}
	plain, err := Aggregate(context.Background(), db, req)
	if err != nil {
		t.Fatal(err)
	}
	priced, err := AggregateCost(context.Background(), db, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != len(priced) {
		t.Fatalf("row counts differ: %d vs %d", len(plain), len(priced))
	}
	for i := range plain {
		if priced[i].CostMetrics != nil {
			t.Errorf("row %d carries cost metrics without a pricing catalog", i)
		}
	}
}

func TestAggregateCostFormulaAndComposition(t *testing.T) {
	db := seedCost(t)
	rows, err := AggregateCost(context.Background(), db,
		Request{Filter: Filter{Model: []string{"priced-a"}}, GroupBy: []string{"model"}}, testPricer(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.CostMetrics == nil {
		t.Fatal("no cost metrics")
	}
	// 1M input * $1/M + 2M cache read * $0.10/M + 100k output * $10/M
	want := 1.0 + 0.2 + 1.0
	if !near(r.EstimatedCostUSD, want) {
		t.Errorf("estimated = %v, want %v", r.EstimatedCostUSD, want)
	}
	if !near(r.CostInputUSD, 1.0) || !near(r.CostCacheReadUSD, 0.2) || !near(r.CostOutputUSD, 1.0) {
		t.Errorf("composition = in %v read %v write %v out %v",
			r.CostInputUSD, r.CostCacheReadUSD, r.CostCacheWriteUSD, r.CostOutputUSD)
	}
	// The turn recorded 60k reasoning tokens inside its 100k output. Charging
	// them again would push the output component to $1.60.
	if r.Reasoning != 60_000 {
		t.Fatalf("fixture lost its reasoning tokens: %d", r.Reasoning)
	}
	if !near(r.CostOutputUSD, 1.0) {
		t.Errorf("output cost = %v, want 1.0: reasoning must not be charged on top of output", r.CostOutputUSD)
	}
	if r.PricingCoverage != 1 || r.UnpricedTokens != 0 {
		t.Errorf("coverage = %v with %d unpriced tokens, want full coverage", r.PricingCoverage, r.UnpricedTokens)
	}
}

// A group that spans several models sums their separately priced costs, and the
// extra model dimension the pricing pass needs does not leak into the result.
func TestAggregateCostFoldsModelsIntoTheRequestedGrouping(t *testing.T) {
	db := seedCost(t)
	rows, err := AggregateCost(context.Background(), db, Request{GroupBy: []string{"harness"}}, testPricer(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want one harness row", len(rows))
	}
	r := rows[0]
	if _, leaked := r.Group["model"]; leaked {
		t.Error("the pricing pass leaked its model dimension into the result")
	}
	// priced-a: 1 + 0.2 + 1 = 2.2   priced-b: 2 + 2 = 4   unpriced-model: 0
	if !near(r.EstimatedCostUSD, 6.2) {
		t.Errorf("estimated = %v, want 6.2", r.EstimatedCostUSD)
	}
	// The unpriced model contributes 1M tokens of coverage gap, never $0 of cost.
	if r.UnpricedTokens != 1_000_000 {
		t.Errorf("unpriced tokens = %d, want 1000000", r.UnpricedTokens)
	}
	if r.PricedTokens != 4_200_000 {
		t.Errorf("priced tokens = %d, want 4200000", r.PricedTokens)
	}
	wantCoverage := 4_200_000.0 / 5_200_000.0
	if !near(r.PricingCoverage, wantCoverage) {
		t.Errorf("coverage = %v, want %v", r.PricingCoverage, wantCoverage)
	}
	// Coverage is token weighted: two of three model names resolved, which is
	// 67%, while the tokens say 81%.
	if r.PricingCoverage < 0.7 {
		t.Errorf("coverage %v looks model-count weighted", r.PricingCoverage)
	}
	if !hasBasis(r.PricingBasis, string(pricing.BasisUnpriced)) {
		t.Errorf("pricing basis %v does not admit the unpriced part", r.PricingBasis)
	}
}

// The hierarchy selector sends correlated tuples. Cost must be computed over
// exactly the selected tuples, never over the cross product of their levels.
func TestAggregateCostHonorsCorrelatedSelection(t *testing.T) {
	db := seedCost(t)
	modelA, effortA := "priced-a", "high"
	modelB, effortB := "priced-b", "max"
	rows, err := AggregateCost(context.Background(), db, Request{
		Filter: Filter{Selection: []Tuple{
			{Harness: "hx", Model: &modelA, Effort: &effortA},
			{Harness: "hx", Model: &modelB, Effort: &effortB},
		}},
		GroupBy: []string{"harness"},
	}, testPricer(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	// Only the two selected tuples: the unpriced model's turn is out of scope,
	// so coverage is complete rather than diluted by rows nobody asked for.
	if !near(rows[0].EstimatedCostUSD, 6.2) {
		t.Errorf("estimated = %v, want 6.2", rows[0].EstimatedCostUSD)
	}
	if rows[0].UnpricedTokens != 0 || rows[0].PricingCoverage != 1 {
		t.Errorf("selection let unselected rows into the estimate: %+v", rows[0].CostMetrics)
	}

	// Swapping the efforts selects tuples the evidence never contained, so
	// there is nothing to price.
	crossed, err := AggregateCost(context.Background(), db, Request{
		Filter: Filter{Selection: []Tuple{
			{Harness: "hx", Model: &modelA, Effort: &effortB},
			{Harness: "hx", Model: &modelB, Effort: &effortA},
		}},
		GroupBy: []string{"harness"},
	}, testPricer(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(crossed) != 0 {
		t.Errorf("a tuple combination the evidence never contained produced %d priced rows", len(crossed))
	}
}

// A limited or reordered table still prices every model inside the rows it
// shows: the limit applies to presentation, not to the pricing pass.
func TestAggregateCostSurvivesLimitAndOrdering(t *testing.T) {
	db := seedCost(t)
	rows, err := AggregateCost(context.Background(), db,
		Request{GroupBy: []string{"model"}, OrderByTokens: true, Limit: 1}, testPricer(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].CostMetrics == nil {
		t.Fatal("the top row lost its cost metrics")
	}
	if rows[0].Group["model"] == "priced-a" && !near(rows[0].EstimatedCostUSD, 2.2) {
		t.Errorf("estimated = %v, want 2.2", rows[0].EstimatedCostUSD)
	}
}

func TestAggregateCostPerBucket(t *testing.T) {
	db := seedCost(t)
	rows, err := AggregateCost(context.Background(), db,
		Request{GroupBy: []string{"harness"}, Bucket: BucketDay}, testPricer(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d buckets, want 3", len(rows))
	}
	byBucket := map[string]float64{}
	for _, r := range rows {
		if r.CostMetrics == nil {
			t.Fatalf("bucket %q has no cost metrics", r.Bucket)
		}
		byBucket[r.Bucket] = r.EstimatedCostUSD
	}
	if !near(byBucket["2026-08-18T00:00:00Z"], 2.2) || !near(byBucket["2026-08-19T00:00:00Z"], 4.0) {
		t.Errorf("per-bucket costs = %v", byBucket)
	}
	if byBucket["2026-08-20T00:00:00Z"] != 0 {
		t.Errorf("the unpriced day = %v, want 0 cost with unpriced tokens", byBucket["2026-08-20T00:00:00Z"])
	}
}

func TestUnpricedModelsRankedByTokens(t *testing.T) {
	db := seedCost(t)
	models, err := UnpricedModels(context.Background(), db, Filter{}, testPricer(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d unpriced models, want 1: %+v", len(models), models)
	}
	if models[0].Model != "unpriced-model" {
		t.Errorf("unpriced model = %q", models[0].Model)
	}
	if models[0].Tokens != 1_000_000 {
		t.Errorf("tokens = %d, want 1000000", models[0].Tokens)
	}
	if models[0].Reason == "" {
		t.Error("an unpriced model must carry a reason")
	}
}

func hasBasis(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func near(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
