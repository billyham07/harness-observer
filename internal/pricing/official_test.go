package pricing

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// Minimized from the local public catalog: adding Kyma's different rate must
// not make Google's own model unpriced for an API-equivalent comparison.
func TestGeminiOfficialDefault(t *testing.T) {
	data, err := os.ReadFile("testdata/gemini-4-argon.json")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := parseSnapshot(data, SourceCached)
	if err != nil {
		t.Fatal(err)
	}
	r := newTestResolver(t, Options{})
	r.catalog = catalog
	for _, name := range []string{
		"gemini-4-argon", "GEMINI-4-ARGON",
		"google-antigravity/gemini-4-argon", "agy/gemini-4-argon",
	} {
		t.Run(name, func(t *testing.T) {
			m := r.Resolve(name)
			if m.CanonicalModel != "gemini/gemini-4-argon" || m.Rule != RuleOfficialProvider || m.Basis != BasisCatalog {
				t.Fatalf("unexpected match: %+v", m)
			}
			if m.Provider != "gemini" || m.Source != SourceCached || m.SourceVersion != catalog.Version {
				t.Fatalf("missing official pricing provenance: %+v", m)
			}
			cost := r.Cost(name, Buckets{Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000})
			if math.Abs(cost.Total-12.1) > 1e-9 || cost.PricedTokens != 3_000_000 || cost.UnpricedTokens != 0 || cost.Heuristic {
				t.Fatalf("unexpected official API-equivalent cost: %+v", cost)
			}
		})
	}
	for _, name := range []string{"kyma/gemini-4-argon", "KYMA/GEMINI-4-ARGON", "gemini/gemini-4-argon", "vertex_ai/gemini-4-argon"} {
		m := r.Resolve(name)
		if m.CanonicalModel != strings.ToLower(name) || !m.Priced() {
			t.Errorf("explicit provider lost precedence: %+v", m)
		}
	}
	// Operator rates still take priority over the official default.
	r = newTestResolver(t, Options{Overrides: map[string]Rates{
		"gemini-4-argon": {Input: rate(7e-6)},
	}})
	r.catalog = catalog
	if m := r.Resolve("gemini-4-argon"); m.Basis != BasisOverride || *m.Rates.Input != 7e-6 {
		t.Fatalf("override lost precedence: %+v", m)
	}
}

func TestGeminiOfficialDefaultIsExactAndFailsClosed(t *testing.T) {
	catalog := newCatalog(SourceBundled, "test", time.Time{}, map[string]Entry{
		"gemini/gemini-4-argon": {Provider: "gemini", Rates: Rates{Input: rate(2e-6)}},
		// A reseller-only variant is not evidence of Google's published rate.
		"kyma/gemini-future": {Provider: "kyma", Rates: Rates{Input: rate(5.4e-6)}},
	})
	for _, name := range []string{"gemini-future", "gemini-4-argon-high", "gemini-4-argon-next", "gemini-4-argon-cyber-permissive"} {
		key, _, _, reason := catalog.lookup(name)
		if key != "" || reason != ReasonNoMatch {
			t.Errorf("lookup(%q) = %q (%q), want unpriced", name, key, reason)
		}
	}
	// An exact bare catalog entry is authoritative, as before.
	catalog = newCatalog(SourceBundled, "test", time.Time{}, map[string]Entry{
		"gemini-4-argon":        {Rates: Rates{Input: rate(3e-6)}},
		"gemini/gemini-4-argon": {Rates: Rates{Input: rate(2e-6)}},
	})
	if key, _, rule, _ := catalog.lookup("gemini-4-argon"); key != "gemini-4-argon" || rule != RuleExact {
		t.Fatalf("exact catalog entry lost precedence: %q via %q", key, rule)
	}
}

func TestOfficialNamespacesUseCompleteModelID(t *testing.T) {
	for _, tc := range []struct{ observed, official string }{
		{"gemini-new-model", "gemini/gemini-new-model"},
		{"grok-new-model", "xai/grok-new-model"},
		{"claude-new-model", "anthropic/claude-new-model"},
		{"gpt-new-model", "openai/gpt-new-model"},
		{"claude-4.6-opus", "anthropic/claude-opus-4-6"},
		{"claude-5.2-fable", "anthropic/claude-fable-5-2"},
		{"claude-fable-5.2", "anthropic/claude-fable-5-2"},
		{"kimi-k3.1-pro", "moonshot/kimi-k3.1-pro"},
		{"glm-5.3-flash", "zai/glm-5.3-flash"},
		{"qwen3.8-max", "dashscope/qwen3.8-max"},
		{"deepseek-new-model", "deepseek/deepseek-new-model"},
		{"minimax-new-model", "minimax/minimax-new-model"},
		{"mistral-new-model", "mistral/mistral-new-model"},
		{"claude-ocx-aigw_local--glm-5.3-flash", "zai/glm-5.3-flash"},
	} {
		t.Run(tc.observed, func(t *testing.T) {
			catalog := newCatalog(SourceBundled, "test", time.Time{}, map[string]Entry{
				tc.official:               {Rates: Rates{Input: rate(2e-6)}},
				"reseller/" + tc.observed: {Rates: Rates{Input: rate(5e-6)}},
			})
			key, _, rule, _ := catalog.lookup(tc.observed)
			if key != tc.official || rule != RuleOfficialProvider {
				t.Fatalf("lookup = %q via %q, want %q via official-provider", key, rule, tc.official)
			}
			// An unlisted sibling must not inherit this model's price.
			if key, _, _, _ := catalog.lookup(tc.observed + "-unknown"); key != "" {
				t.Fatalf("unlisted sibling inherited %q", key)
			}
		})
	}
}

func TestBundledGeminiArgonOfficialDefault(t *testing.T) {
	r := newTestResolver(t, Options{})
	m := r.Resolve("gemini-4-argon")
	if m.CanonicalModel != "gemini/gemini-4-argon" || !m.Priced() || m.Source != SourceBundled {
		t.Fatalf("offline official pricing unavailable: %+v", m)
	}
}
