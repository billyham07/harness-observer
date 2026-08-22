package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func newTestResolver(t *testing.T, opts Options) *Resolver {
	t.Helper()
	if opts.CachePath == "" {
		// Never touch the user's real cache from a test.
		opts.CachePath = filepath.Join(t.TempDir(), "pricing-catalog.json")
	}
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestBundledCatalogLoads(t *testing.T) {
	c, err := Bundled()
	if err != nil {
		t.Fatalf("Bundled: %v", err)
	}
	if c.Len() < minUsableModels {
		t.Fatalf("bundled catalog has %d models", c.Len())
	}
	if c.Version == "" {
		t.Error("bundled catalog has no version")
	}
	for name, e := range c.Models {
		for label, v := range map[string]*float64{
			"input": e.Rates.Input, "output": e.Rates.Output,
			"cache_read": e.Rates.CacheRead, "cache_write": e.Rates.CacheWrite,
		} {
			if v != nil && (*v < 0 || *v > maxSaneRate) {
				t.Fatalf("model %q has an out-of-range %s rate %v", name, label, *v)
			}
		}
	}
}

// Every alias claims two names bill identically. An alias whose target left the
// catalog is dead weight that will silently never fire.
func TestAliasTargetsExist(t *testing.T) {
	c, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for from, to := range aliases {
		if _, ok := c.Models[to]; !ok {
			t.Errorf("alias %q -> %q: target is not in the bundled catalog", from, to)
		}
	}
}

func TestResolveRules(t *testing.T) {
	r := newTestResolver(t, Options{})
	cases := []struct {
		observed  string
		wantKey   string
		wantRule  string
		wantBasis Basis
	}{
		{"gpt-5.6-sol", "gpt-5.6-sol", RuleExact, BasisCatalog},
		{"claude-opus-5", "claude-opus-5", RuleExact, BasisCatalog},
		{"xai/grok-4.6", "xai/grok-4.6", RuleExact, BasisCatalog},
		// Grok Build records the public model as "<id>-build" in modelUsage.
		{"grok-4.6-build", "xai/grok-4.6", RuleAlias, BasisCatalog},
		{"grok-4.5-build", "xai/grok-4.5", RuleAlias, BasisCatalog},
		// Gateway/routing prefixes describe how a call was routed, not what was
		// billed, so they are dropped one segment at a time.
		{"aigw_local/deepseek-v4-flash", "deepseek-v4-flash", RuleProviderPrefix, BasisCatalog},
		{"google-antigravity/gemini-3.6-flash", "gemini-3.6-flash", RuleProviderPrefix, BasisCatalog},
		{"openai-codex:gpt-5.5", "gpt-5.5", RuleProviderPrefix, BasisCatalog},
		// Anthropic's two spellings of one model.
		{"anthropic--claude-4.6-opus", "claude-opus-4-6", RuleVendorReorder, BasisCatalog},
		{"GPT-5.5", "gpt-5.5", RuleCaseInsensitive, BasisCatalog},
		// Only one provider publishes this one, so the bare name is unambiguous.
		{"grok-4.5", "xai/grok-4.5", RuleUniqueSuffix, BasisCatalog},
	}
	for _, tc := range cases {
		m := r.Resolve(tc.observed)
		if m.CanonicalModel != tc.wantKey || m.Rule != tc.wantRule || m.Basis != tc.wantBasis {
			t.Errorf("Resolve(%q) = key %q rule %q basis %q, want %q/%q/%q",
				tc.observed, m.CanonicalModel, m.Rule, m.Basis, tc.wantKey, tc.wantRule, tc.wantBasis)
		}
	}
}

// Grok Build's runtime id is the public model plus "-build". That is not the
// separately priced grok-build-0.1 product, and a future grok-N.N-build must
// stay unpriced until it has its own alias/catalog row.
func TestGrokBuildRuntimeIdIsNotGrokBuildProduct(t *testing.T) {
	r := newTestResolver(t, Options{})
	build := r.Resolve("grok-4.6-build")
	pub := r.Resolve("grok-4.6")
	if !build.Priced() || !pub.Priced() {
		t.Fatalf("expected grok-4.6-build and grok-4.6 priced, got %q and %q", build.Basis, pub.Basis)
	}
	if build.CanonicalModel != pub.CanonicalModel || !build.Rates.equal(pub.Rates) {
		t.Errorf("grok-4.6-build -> %q %+v, grok-4.6 -> %q %+v",
			build.CanonicalModel, build.Rates, pub.CanonicalModel, pub.Rates)
	}
	product := r.Resolve("grok-build-0.1")
	if !product.Priced() {
		t.Fatal("grok-build-0.1 should remain its own catalog product")
	}
	if product.CanonicalModel == build.CanonicalModel {
		t.Fatalf("grok-4.6-build collapsed onto grok-build-0.1 (%q)", product.CanonicalModel)
	}
	future := r.Resolve("grok-9.9-build")
	if future.Priced() {
		t.Errorf("unknown grok-9.9-build resolved to %q", future.CanonicalModel)
	}
}

func TestDateSuffixResolvesToTheSameModel(t *testing.T) {
	r := newTestResolver(t, Options{})
	dated := r.Resolve("claude-opus-4-6-20260205")
	base := r.Resolve("claude-opus-4-6")
	if !dated.Priced() || !base.Priced() {
		t.Fatalf("expected both spellings priced, got %q and %q", dated.Basis, base.Basis)
	}
	if !dated.Rates.equal(base.Rates) {
		t.Errorf("dated and undated spellings priced differently: %+v vs %+v", dated.Rates, base.Rates)
	}
	// A model name that only exists with a date must still resolve after the
	// date is stripped, which is the rule this covers.
	synthetic := newCatalog(SourceBundled, "test", time.Time{}, map[string]Entry{
		"some-model": {Rates: Rates{Input: rate(1e-6), Output: rate(2e-6)}},
	})
	key, _, rule, _ := synthetic.lookup("some-model-20260205")
	if key != "some-model" || rule != RuleDateSuffix {
		t.Errorf("lookup(some-model-20260205) = %q via %q, want some-model via %q", key, rule, RuleDateSuffix)
	}
}

// The whole point of conservative matching: two sibling variants of one release
// have separate published rates and must never collapse into each other.
func TestModelVariantsStayDistinct(t *testing.T) {
	r := newTestResolver(t, Options{})
	sol := r.Resolve("gpt-5.6-sol")
	luna := r.Resolve("gpt-5.6-luna")
	if sol.CanonicalModel == luna.CanonicalModel {
		t.Fatalf("sol and luna resolved to the same key %q", sol.CanonicalModel)
	}
	if sol.Rates.equal(luna.Rates) && sol.Rates.Input != nil && luna.Rates.Input != nil && *sol.Rates.Input == *luna.Rates.Input {
		// Identical published rates are possible, but the keys must differ,
		// which the check above already enforces.
		t.Log("sol and luna currently publish identical rates")
	}
	// A future variant of the same family must not inherit an existing price.
	future := r.Resolve("gpt-5.6-nova-does-not-exist")
	if future.Priced() {
		t.Errorf("unknown variant resolved to %q", future.CanonicalModel)
	}
}

func TestUnknownModelIsUnpricedNotFree(t *testing.T) {
	r := newTestResolver(t, Options{})
	m := r.Resolve("codex-auto-review")
	if m.Basis != BasisUnpriced {
		t.Fatalf("basis = %q, want unpriced", m.Basis)
	}
	if m.Reason == "" {
		t.Error("an unpriced model must say why")
	}
	c := r.Cost("codex-auto-review", Buckets{Input: 1000, Output: 500})
	if c.Total != 0 {
		t.Errorf("total = %v, want 0", c.Total)
	}
	if c.PricedTokens != 0 || c.UnpricedTokens != 1500 {
		t.Errorf("priced/unpriced = %d/%d, want 0/1500", c.PricedTokens, c.UnpricedTokens)
	}
}

func TestAmbiguousBareNameIsUnpriced(t *testing.T) {
	c := newCatalog(SourceBundled, "test", time.Time{}, map[string]Entry{
		"vendor-a/shared-model": {Rates: Rates{Input: rate(1e-6), Output: rate(2e-6)}},
		"vendor-b/shared-model": {Rates: Rates{Input: rate(5e-6), Output: rate(9e-6)}},
	})
	key, _, _, reason := c.lookup("shared-model")
	if key != "" {
		t.Fatalf("ambiguous bare name resolved to %q", key)
	}
	if reason != ReasonAmbiguous {
		t.Errorf("reason = %q, want %q", reason, ReasonAmbiguous)
	}
	// When every publisher agrees on the rate, the choice cannot change the
	// number, so the match is allowed and labelled.
	agreed := newCatalog(SourceBundled, "test", time.Time{}, map[string]Entry{
		"vendor-a/same-price": {Rates: Rates{Input: rate(1e-6), Output: rate(2e-6)}},
		"vendor-b/same-price": {Rates: Rates{Input: rate(1e-6), Output: rate(2e-6)}},
	})
	key, _, rule, _ := agreed.lookup("same-price")
	if key == "" || rule != RuleAgreedSuffix {
		t.Errorf("lookup(same-price) = %q via %q, want a key via %q", key, rule, RuleAgreedSuffix)
	}
}

func TestInvalidRatesAreRejected(t *testing.T) {
	raw := []byte(`{
	  "negative-input": {"mode":"chat","input_cost_per_token":-1e-6,"output_cost_per_token":2e-6},
	  "absurd-input":   {"mode":"chat","input_cost_per_token":5,"output_cost_per_token":2e-6},
	  "sane":           {"mode":"chat","input_cost_per_token":1e-6,"output_cost_per_token":2e-6}
	}`)
	// A tiny table is below the sanity floor, so parse it through the snapshot
	// form where the floor does not apply and the per-rate validation does.
	models := map[string]snapshotEntry{}
	var upstream map[string]upstreamEntry
	if err := json.Unmarshal(raw, &upstream); err != nil {
		t.Fatal(err)
	}
	for name, e := range upstream {
		models[name] = snapshotEntry{Input: e.Input, Output: e.Output}
	}
	data, err := json.Marshal(snapshot{Source: "test", Models: models})
	if err != nil {
		t.Fatal(err)
	}
	c, err := parseSnapshot(data, SourceBundled)
	if err != nil {
		t.Fatal(err)
	}
	if r := c.Models["negative-input"].Rates.Input; r != nil {
		t.Errorf("negative input rate survived as %v", *r)
	}
	if r := c.Models["absurd-input"].Rates.Input; r != nil {
		t.Errorf("out-of-range input rate survived as %v", *r)
	}
	if c.Models["sane"].Rates.Input == nil {
		t.Error("valid rate was dropped")
	}
}

func TestCostFormula(t *testing.T) {
	c := newCatalog(SourceBundled, "test", time.Time{}, map[string]Entry{
		"m": {Rates: Rates{
			Input: rate(1e-6), Output: rate(10e-6), CacheRead: rate(0.1e-6), CacheWrite: rate(1.25e-6),
		}},
	})
	r := newTestResolver(t, Options{})
	r.catalog = c

	cost := r.Cost("m", Buckets{Input: 1_000_000, CacheRead: 2_000_000, CacheWrite: 400_000, Output: 100_000})
	want := 1.0 + 0.2 + 0.5 + 1.0
	if !almost(cost.Total, want) {
		t.Errorf("total = %v, want %v", cost.Total, want)
	}
	if !almost(cost.Input, 1.0) || !almost(cost.CacheRead, 0.2) || !almost(cost.CacheWrite, 0.5) || !almost(cost.Output, 1.0) {
		t.Errorf("composition = %+v", cost)
	}
	if cost.UnpricedTokens != 0 || cost.PricedTokens != 3_500_000 {
		t.Errorf("priced/unpriced = %d/%d", cost.PricedTokens, cost.UnpricedTokens)
	}
}

// Reasoning tokens are a detail of the output counter in every verified source
// format. Buckets has no reasoning field, so the only way to double charge them
// would be to add one; this test pins that decision.
func TestReasoningIsNotChargedTwice(t *testing.T) {
	c := newCatalog(SourceBundled, "test", time.Time{}, map[string]Entry{
		"m": {Rates: Rates{Input: rate(1e-6), Output: rate(10e-6)}},
	})
	r := newTestResolver(t, Options{})
	r.catalog = c
	// 100k output of which 60k was reasoning: the price is the output price.
	cost := r.Cost("m", Buckets{Output: 100_000})
	if !almost(cost.Total, 1.0) {
		t.Errorf("total = %v, want 1.0 (output only)", cost.Total)
	}
}

func TestPartialCoverage(t *testing.T) {
	// A catalog that prices input and output but publishes no cache rates.
	c := newCatalog(SourceBundled, "test", time.Time{}, map[string]Entry{
		"m": {Rates: Rates{Input: rate(1e-6), Output: rate(10e-6)}},
	})
	r := newTestResolver(t, Options{})
	r.catalog = c
	cost := r.Cost("m", Buckets{Input: 100, CacheRead: 900, Output: 100})
	if cost.PricedTokens != 200 || cost.UnpricedTokens != 900 {
		t.Fatalf("priced/unpriced = %d/%d, want 200/900", cost.PricedTokens, cost.UnpricedTokens)
	}
	if cost.Heuristic {
		t.Error("no heuristic was enabled, so nothing may be flagged heuristic")
	}
	if cost.Match.CacheBasis != BasisUnpriced {
		t.Errorf("cache basis = %q, want unpriced", cost.Match.CacheBasis)
	}
}

func TestHeuristicCacheRatesAreLabelled(t *testing.T) {
	c := newCatalog(SourceBundled, "test", time.Time{}, map[string]Entry{
		"m": {Rates: Rates{Input: rate(1e-6), Output: rate(10e-6)}},
	})
	r := newTestResolver(t, Options{HeuristicCache: true})
	r.catalog = c
	cost := r.Cost("m", Buckets{CacheRead: 1_000_000, CacheWrite: 1_000_000})
	if !cost.Heuristic {
		t.Error("inferred cache rates must be reported as heuristic")
	}
	if cost.Match.CacheBasis != BasisHeuristic {
		t.Errorf("cache basis = %q, want heuristic", cost.Match.CacheBasis)
	}
	if !almost(cost.CacheRead, 0.1) || !almost(cost.CacheWrite, 1.25) {
		t.Errorf("heuristic cache cost = read %v write %v, want 0.1 and 1.25", cost.CacheRead, cost.CacheWrite)
	}
	if cost.UnpricedTokens != 0 {
		t.Errorf("unpriced = %d, want 0 once the heuristic applies", cost.UnpricedTokens)
	}
	// The input rate itself is still catalog priced.
	if cost.Match.Basis != BasisCatalog {
		t.Errorf("basis = %q, want catalog", cost.Match.Basis)
	}
}

func TestOverridesWinAndAreLabelled(t *testing.T) {
	r := newTestResolver(t, Options{
		Overrides:     map[string]Rates{"gpt-5.6-sol": {Input: rate(2e-6), Output: rate(4e-6)}},
		OverridesPath: "test-overrides.json",
	})
	m := r.Resolve("gpt-5.6-sol")
	if m.Basis != BasisOverride {
		t.Fatalf("basis = %q, want %q", m.Basis, BasisOverride)
	}
	if m.Rates.Input == nil || *m.Rates.Input != 2e-6 {
		t.Errorf("override rate was not applied: %+v", m.Rates)
	}
}

func TestLoadOverridesRejectsBadRates(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"models":{"m":{"input":1e-6,"output":2e-6}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOverrides(good); err != nil {
		t.Fatalf("valid overrides rejected: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"models":{"m":{"input":-1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOverrides(bad); err == nil {
		t.Error("a negative override rate must be an error, not a dropped field")
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"models":{"m":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOverrides(empty); err == nil {
		t.Error("an override with no rates must be an error")
	}
}

func almost(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-9
}

func TestRefreshFailureKeepsTheCurrentCatalog(t *testing.T) {
	r := newTestResolver(t, Options{
		Refresh: true,
		Transport: func(context.Context, string) ([]byte, error) {
			return nil, errors.New("offline")
		},
	})
	before := r.Catalog()
	err := r.Refresh(context.Background())
	if err == nil {
		t.Fatal("expected the refresh to fail")
	}
	if r.Catalog() != before {
		t.Error("a failed refresh replaced the working catalog")
	}
	st := r.Status()
	if st.LastRefreshError == "" {
		t.Error("a failed refresh must be visible in status")
	}
	if st.Source != SourceBundled {
		t.Errorf("source = %q, want the bundled snapshot to still be in use", st.Source)
	}
	// The dashboard must keep pricing from the bundled snapshot.
	if !r.Resolve("gpt-5.6-sol").Priced() {
		t.Error("offline pricing stopped working after a failed refresh")
	}
}

func TestMalformedRefreshIsRejected(t *testing.T) {
	r := newTestResolver(t, Options{
		Refresh: true,
		Transport: func(context.Context, string) ([]byte, error) {
			return []byte(`<!doctype html><title>404</title>`), nil
		},
	})
	if err := r.Refresh(context.Background()); err == nil {
		t.Fatal("malformed payload was accepted")
	}
	if r.Catalog().Source != SourceBundled {
		t.Error("malformed payload replaced the catalog")
	}
}

func TestTruncatedRefreshIsRejected(t *testing.T) {
	r := newTestResolver(t, Options{
		Refresh: true,
		Transport: func(context.Context, string) ([]byte, error) {
			return []byte(`{"only-one-model":{"mode":"chat","input_cost_per_token":1e-6}}`), nil
		},
	})
	if err := r.Refresh(context.Background()); err == nil {
		t.Fatal("a table below the sanity floor was accepted")
	}
	if r.Catalog().Source != SourceBundled {
		t.Error("a truncated table replaced the catalog")
	}
}

func TestRefreshCachesAndIsReusedWhileFresh(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "pricing-catalog.json")
	payload := upstreamFixture(t, 150)
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	calls := 0
	opts := Options{
		Refresh:   true,
		CachePath: cache,
		Now:       func() time.Time { return now },
		Transport: func(context.Context, string) ([]byte, error) {
			calls++
			return payload, nil
		},
	}
	r := newTestResolver(t, opts)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if r.Catalog().Source != SourceRemote {
		t.Fatalf("source = %q, want remote", r.Catalog().Source)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("refresh did not write the cache: %v", err)
	}

	// A new process inside the TTL loads the cache and does not go out again.
	fresh := newTestResolver(t, opts)
	if fresh.Catalog().Source != SourceCached {
		t.Errorf("source = %q, want the cached catalog", fresh.Catalog().Source)
	}
	if fresh.stale() {
		t.Error("a catalog fetched moments ago is not stale")
	}

	// Past the TTL it is stale again and a refresh is allowed.
	opts.Now = func() time.Time { return now.Add(48 * time.Hour) }
	old := newTestResolver(t, opts)
	if !old.stale() {
		t.Error("a catalog older than the TTL must be stale")
	}
	// With refresh disabled the stale cache is still used, never discarded.
	opts.Refresh = false
	offline := newTestResolver(t, opts)
	if offline.stale() {
		t.Error("refresh is disabled, so nothing should be scheduled")
	}
	if offline.Catalog().Source != SourceCached || !offline.Status().Stale {
		t.Error("a stale cache must stay in use and be reported as stale")
	}
}

func TestCorruptCacheFallsBackToBundled(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "pricing-catalog.json")
	if err := os.WriteFile(cache, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newTestResolver(t, Options{CachePath: cache})
	if r.Catalog().Source != SourceBundled {
		t.Errorf("source = %q, want bundled", r.Catalog().Source)
	}
	if !r.Resolve("gpt-5.6-sol").Priced() {
		t.Error("a corrupt cache broke offline pricing")
	}
}

// upstreamFixture builds a synthetic LiteLLM-shaped table large enough to clear
// the sanity floor.
func upstreamFixture(t *testing.T, n int) []byte {
	t.Helper()
	models := map[string]map[string]any{}
	for i := 0; i < n; i++ {
		models[fixtureName(i)] = map[string]any{
			"mode":                        "chat",
			"litellm_provider":            "test",
			"input_cost_per_token":        1e-6,
			"output_cost_per_token":       2e-6,
			"cache_read_input_token_cost": 1e-7,
		}
	}
	data, err := json.Marshal(models)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fixtureName(i int) string {
	return "fixture-model-" + strconv.Itoa(i)
}
