package pricing

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpstreamOverrideUsesOfficialCatalogAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pricing-overrides.json")
	r := newTestResolver(t, Options{OverridesPath: path})
	match, err := r.SetOverride(Override{Model: "kimi-k3-2", Upstream: "moonshot/kimi-k3"})
	if err != nil {
		t.Fatal(err)
	}
	if match.Basis != BasisOverride || match.Rule != RuleUpstreamOverride || match.CanonicalModel != "moonshot/kimi-k3" {
		t.Fatalf("unexpected alias match: %+v", match)
	}
	if !match.Rates.equal(r.Resolve("moonshot/kimi-k3").Rates) {
		t.Fatalf("alias rates differ from upstream: %+v", match.Rates)
	}
	// A new resolver reads the same local configuration, with no source-code
	// alias and no dependency on the catalog cache being writable.
	r2, err := New(Options{CachePath: filepath.Join(t.TempDir(), "cache.json"), OverridesPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := r2.Resolve("kimi-k3-2"); got.CanonicalModel != "moonshot/kimi-k3" || got.Rule != RuleUpstreamOverride {
		t.Fatalf("persisted override did not reload: %+v", got)
	}
	if err := r.DeleteOverride("kimi-k3-2"); err != nil {
		t.Fatal(err)
	}
	if got := r.Resolve("kimi-k3-2"); got.Basis != BasisUnpriced {
		t.Fatalf("deleted alias still priced: %+v", got)
	}
}

func TestConflictingOverrideIsNotPriced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pricing-overrides.json")
	r := newTestResolver(t, Options{OverridesPath: path})
	if _, err := r.SetOverride(Override{Model: "kimi-k3-2", Upstream: "moonshot/kimi-k3"}); err != nil {
		t.Fatal(err)
	}
	// Simulate the other half of a config from a separately supplied explicit
	// file: two claims for one model must fail closed instead of picking one.
	r.mu.Lock()
	r.opts.Overrides["kimi-k3-2"] = Rates{Input: rate(3e-6)}
	r.memo = map[string]Match{}
	r.mu.Unlock()
	got := r.Resolve("kimi-k3-2")
	if got.Priced() || got.Reason != "conflicting local pricing overrides" {
		t.Fatalf("conflicting local override was silently accepted: %+v", got)
	}
	if _, err := r.SetOverride(Override{Model: "other-model", Rates: Rates{Input: rate(1e-6)}}); err == nil {
		t.Fatal("conflicting configuration was silently rewritten")
	}
}

func TestExplicitOverrideFileStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pricing-overrides.json")
	data := []byte(`{"models":{"internal/model":{"input":0.000001,"output":0.000003}}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := New(Options{CachePath: filepath.Join(t.TempDir(), "cache.json"), OverridesPath: path})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Resolve("internal/model")
	if got.Basis != BasisOverride || got.Rates.Input == nil || *got.Rates.Input != 1e-6 {
		t.Fatalf("legacy explicit override was not loaded: %+v", got)
	}
}

func TestOverrideValidation(t *testing.T) {
	r := newTestResolver(t, Options{OverridesPath: filepath.Join(t.TempDir(), "overrides.json")})
	for _, override := range []Override{
		{},
		{Model: "x", Upstream: "x"},
		{Model: "x", Upstream: "moonshot/kimi-k3", Rates: Rates{Input: rate(1e-6)}},
		{Model: "x", Upstream: "kyma/kimi-k3"},
		{Model: "x", Upstream: "moonshot/not-a-model"},
		{Model: "x"},
		{Model: "x", Rates: Rates{Input: rate(-1)}},
	} {
		if _, err := r.SetOverride(override); err == nil {
			t.Errorf("SetOverride(%+v) unexpectedly succeeded", override)
		}
	}
}

func TestCatalogModelsOnlyOffersFirstPartyRates(t *testing.T) {
	r := newTestResolver(t, Options{})
	r.catalog = newCatalog(SourceCached, "test", time.Time{}, map[string]Entry{
		"moonshot/kimi-k3": {Provider: "moonshot", Rates: Rates{Input: rate(3e-6)}},
		"kyma/kimi-k3":     {Provider: "kyma", Rates: Rates{Input: rate(9e-6)}},
		"claude-fable-5-2": {Provider: "anthropic", Rates: Rates{Input: rate(8e-6)}},
	})
	models := r.CatalogModels("kimi", 10)
	if len(models) != 1 || models[0].Model != "moonshot/kimi-k3" {
		t.Fatalf("reseller leaked into upstream options: %+v", models)
	}
	if got := r.CatalogModels("fable", 10); len(got) != 1 || got[0].Model != "claude-fable-5-2" {
		t.Fatalf("bare first-party model was omitted: %+v", got)
	}
}
