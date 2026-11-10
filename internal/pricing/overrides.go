package pricing

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Override is one observed model's local pricing decision. Upstream borrows
// the published rates of another model; Rates sets them explicitly. Exactly
// one form is used for an entry. The observed model name is the map key in the
// file, so adding an alias never requires a parser or catalog change.
type Override struct {
	Model    string `json:"model,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Rates    Rates  `json:"rates,omitempty"`
	// These four fields keep the original flat {"models": {name: rates}}
	// file format source-compatible. The UI/API writes the nested rates form.
	Input      *float64 `json:"input,omitempty"`
	Output     *float64 `json:"output,omitempty"`
	CacheRead  *float64 `json:"cache_read,omitempty"`
	CacheWrite *float64 `json:"cache_write,omitempty"`
}

func (o Override) explicitRates() Rates {
	if !o.Rates.Empty() {
		return o.Rates
	}
	return Rates{Input: o.Input, Output: o.Output, CacheRead: o.CacheRead, CacheWrite: o.CacheWrite}
}

// overrideFile is deliberately a superset of the original {"models": {name:
// rates}} override format. Existing hand-written files keep working, while an
// entry may instead contain {"upstream": "official-model"}.
type overrideFile struct {
	Version int                 `json:"version,omitempty"`
	Models  map[string]Override `json:"models"`
}

// DefaultOverridesPath keeps editable pricing beside the downloaded catalog.
// It is configuration, not a cache: refreshing LiteLLM never rewrites it.
func DefaultOverridesPath() string {
	if v := strings.TrimSpace(os.Getenv("HARNESS_OBSERVER_PRICING_OVERRIDES")); v != "" {
		return v
	}
	return filepath.Join(filepath.Dir(DefaultCachePath()), "pricing-overrides.json")
}

func loadOverrideFile(path string) (map[string]Rates, map[string]string, error) {
	rates := map[string]Rates{}
	aliases := map[string]string{}
	if path == "" {
		return rates, aliases, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return rates, aliases, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var doc overrideFile
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("pricing overrides %s: %w", path, err)
	}
	for name, override := range doc.Models {
		model := strings.TrimSpace(name)
		if model == "" {
			return nil, nil, fmt.Errorf("pricing overrides %s: empty model name", path)
		}
		upstream := strings.TrimSpace(override.Upstream)
		explicit := override.explicitRates()
		if upstream != "" {
			if !explicit.Empty() {
				return nil, nil, fmt.Errorf("pricing overrides %s: model %q has both upstream and explicit rates", path, name)
			}
			aliases[model] = upstream
			continue
		}
		if err := validateRates(explicit); err != nil {
			return nil, nil, fmt.Errorf("pricing overrides %s: model %q: %w", path, name, err)
		}
		rates[model] = explicit
	}
	return rates, aliases, nil
}

func validateRates(rates Rates) error {
	if rates.Empty() {
		return errors.New("at least one rate is required")
	}
	for label, value := range map[string]*float64{
		"input": rates.Input, "output": rates.Output,
		"cache_read": rates.CacheRead, "cache_write": rates.CacheWrite,
	} {
		if value == nil {
			continue
		}
		if _, ok := validRate(*value); !ok {
			return fmt.Errorf("invalid %s rate %v", label, *value)
		}
	}
	return nil
}

func saveOverrideFile(path string, rates map[string]Rates, aliases map[string]string) error {
	if path == "" {
		return errors.New("pricing overrides path is not configured")
	}
	doc := overrideFile{Version: 1, Models: map[string]Override{}}
	for model, values := range rates {
		doc.Models[model] = Override{Rates: values}
	}
	for model, upstream := range aliases {
		if _, conflict := rates[model]; conflict {
			return fmt.Errorf("pricing overrides %s: model %q has both upstream and explicit rates", path, model)
		}
		doc.Models[model] = Override{Upstream: upstream}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pricing-overrides-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Overrides returns a sorted snapshot suitable for the local settings UI.
func (r *Resolver) Overrides() []Override {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Override, 0, len(r.opts.Overrides)+len(r.opts.OverrideAliases))
	for model, rates := range r.opts.Overrides {
		out = append(out, Override{Model: model, Rates: rates})
	}
	for model, upstream := range r.opts.OverrideAliases {
		out = append(out, Override{Model: model, Upstream: upstream})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

// SetOverride validates and atomically persists one local alias or explicit
// rate set, then makes it visible to subsequent queries immediately.
func (r *Resolver) SetOverride(override Override) (Match, error) {
	override.Model = strings.TrimSpace(override.Model)
	override.Upstream = strings.TrimSpace(override.Upstream)
	if override.Model == "" {
		return Match{}, errors.New("observed model is required")
	}
	if strings.ContainsAny(override.Model, "\r\n\x00") {
		return Match{}, errors.New("observed model contains a control character")
	}

	r.overrideMu.Lock()
	defer r.overrideMu.Unlock()

	var match Match
	if override.Upstream != "" {
		if !override.explicitRates().Empty() {
			return Match{}, errors.New("choose an upstream model or explicit rates, not both")
		}
		if override.Upstream == override.Model {
			return Match{}, errors.New("upstream model must differ from the observed model")
		}
		// Aliases deliberately resolve against the catalog, not another local
		// override. That keeps them acyclic and makes the upstream price visible.
		match = r.resolveCatalog(override.Upstream)
		if !match.Priced() {
			return Match{}, fmt.Errorf("upstream model %q has no published rate", override.Upstream)
		}
		if !isOfficialEntry(match.CanonicalModel, r.catalogEntry(match.CanonicalModel)) {
			return Match{}, fmt.Errorf("upstream model %q is not a first-party catalog entry", override.Upstream)
		}
	} else {
		override.Rates = override.explicitRates()
		if err := validateRates(override.Rates); err != nil {
			return Match{}, err
		}
	}

	r.mu.RLock()
	rates := cloneRates(r.opts.Overrides)
	aliases := cloneAliases(r.opts.OverrideAliases)
	path := r.opts.OverridesPath
	r.mu.RUnlock()
	if override.Upstream != "" {
		delete(rates, override.Model)
		aliases[override.Model] = override.Upstream
	} else {
		delete(aliases, override.Model)
		rates[override.Model] = override.Rates
	}
	if err := saveOverrideFile(path, rates, aliases); err != nil {
		return Match{}, err
	}
	r.mu.Lock()
	r.opts.Overrides = rates
	r.opts.OverrideAliases = aliases
	r.memo = map[string]Match{}
	r.mu.Unlock()
	return r.Resolve(override.Model), nil
}

// DeleteOverride removes only the local decision. Catalog resolution resumes
// immediately and the change survives the next catalog refresh.
func (r *Resolver) DeleteOverride(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("observed model is required")
	}
	r.overrideMu.Lock()
	defer r.overrideMu.Unlock()
	r.mu.RLock()
	rates := cloneRates(r.opts.Overrides)
	aliases := cloneAliases(r.opts.OverrideAliases)
	path := r.opts.OverridesPath
	r.mu.RUnlock()
	if _, ok := rates[model]; !ok {
		if _, ok := aliases[model]; !ok {
			return os.ErrNotExist
		}
	}
	delete(rates, model)
	delete(aliases, model)
	if err := saveOverrideFile(path, rates, aliases); err != nil {
		return err
	}
	r.mu.Lock()
	r.opts.Overrides = rates
	r.opts.OverrideAliases = aliases
	r.memo = map[string]Match{}
	r.mu.Unlock()
	return nil
}

func cloneRates(in map[string]Rates) map[string]Rates {
	out := make(map[string]Rates, len(in))
	for model, rates := range in {
		out[model] = rates
	}
	return out
}

func cloneAliases(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for model, upstream := range in {
		out[model] = upstream
	}
	return out
}

func (r *Resolver) catalogEntry(key string) Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.catalog.Models[key]
}

// firstPartyProviders are the model publishers used by the upstream selector.
// Cloud marketplaces, gateways and resellers are intentionally not options.
var firstPartyProviders = map[string]bool{
	"openai": true, "anthropic": true, "gemini": true,
	"vertex_ai-language-models": true, "xai": true, "deepseek": true,
	"moonshot": true, "zai": true, "dashscope": true, "minimax": true, "tencent": true,
	"mistral": true, "codestral": true, "cohere": true, "cohere_chat": true,
	"ai21": true, "amazon_nova": true, "volcengine": true,
	"bedrock": true, "bedrock_converse": true,
	"xiaomi_mimo": true, "inception": true, "cognition": true, "stepfun": true,
}

func isOfficialEntry(name string, entry Entry) bool {
	// Bare Bedrock keys are official cloud-hosted variants, but a reseller key
	// is excluded by its explicit gateway/provider prefix even without a slash.
	if entry.Provider == "bedrock" || entry.Provider == "bedrock_converse" {
		return !strings.Contains(name, "/")
	}
	// Some publishers have a bare LiteLLM key without a namespace but still
	// carry their own provider field (for example claude-fable-5-2).
	return firstPartyProviders[entry.Provider] || !strings.Contains(name, "/")
}

// CatalogModel is one selectable published upstream model. Rates remain
// per-token in the API, matching Rates everywhere else in the Go and JSON APIs.
type CatalogModel struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Rates    Rates  `json:"rates"`
}

// CatalogModels searches only first-party published models. It never scans
// session evidence; the query is just a substring of the public model id.
func (r *Resolver) CatalogModels(query string, limit int) []CatalogModel {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query = strings.ToLower(strings.TrimSpace(query))
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CatalogModel, 0, limit)
	for name, entry := range r.catalog.Models {
		if !isOfficialEntry(name, entry) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		out = append(out, CatalogModel{Model: name, Provider: entry.Provider, Rates: entry.Rates})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
