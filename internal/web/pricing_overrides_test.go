package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/billyham07/harness-observer/internal/pricing"
)

func TestPricingOverrideEndpoints(t *testing.T) {
	s := newTestServer(t, writeSlowSource(t, 0))
	resolver, err := pricing.New(pricing.Options{
		CachePath:     filepath.Join(t.TempDir(), "catalog.json"),
		OverridesPath: filepath.Join(t.TempDir(), "overrides.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Pricing = resolver
	handler := s.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pricing/catalog?q=kimi-k3", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "moonshot/kimi-k3") || strings.Contains(rec.Body.String(), "kyma/kimi-k3") {
		t.Fatalf("official catalog choices = %d %s", rec.Code, rec.Body.String())
	}

	body := `{"model":"kimi-k3-2","upstream":"moonshot/kimi-k3"}`
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/pricing/overrides", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT override = %d %s", rec.Code, rec.Body.String())
	}
	var saved struct {
		Match pricing.Match `json:"match"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Match.Rule != pricing.RuleUpstreamOverride || saved.Match.CanonicalModel != "moonshot/kimi-k3" {
		t.Fatalf("saved match = %+v", saved.Match)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/pricing/overrides?model=kimi-k3-2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE override = %d %s", rec.Code, rec.Body.String())
	}
	if resolver.Resolve("kimi-k3-2").Priced() {
		t.Fatal("deleted override remains active")
	}
}

func TestPricingOverrideRejectsReseller(t *testing.T) {
	s := newTestServer(t, writeSlowSource(t, 0))
	resolver, err := pricing.New(pricing.Options{
		CachePath:     filepath.Join(t.TempDir(), "catalog.json"),
		OverridesPath: filepath.Join(t.TempDir(), "overrides.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Pricing = resolver
	rec := httptest.NewRecorder()
	body := `{"model":"kimi-k3-2","upstream":"kyma/kimi-k3"}`
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/pricing/overrides", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reseller override = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
