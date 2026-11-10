package pricing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultURL is LiteLLM's public model pricing table. It is the only network
// endpoint this package knows, it is fetched with GET and no request body, and
// it receives nothing about the local machine.
const DefaultURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

const (
	// DefaultTTL is how long a downloaded catalog is considered fresh. Public
	// rates move on the scale of weeks; a day is already generous.
	DefaultTTL = 24 * time.Hour
	// DefaultTimeout bounds the refresh. A slow network must never be able to
	// hold up a dashboard that already has a working offline catalog.
	DefaultTimeout = 10 * time.Second
)

// heuristicCacheWrite and heuristicCacheRead are the CodeBurn-style fallbacks
// for a catalog that prices input but publishes no cache rates. They are
// disabled by default and every number they produce is reported as heuristic:
// an inferred rate is a guess about somebody else's price list, and this
// collector does not present guesses as evidence.
const (
	heuristicCacheWrite = 1.25
	heuristicCacheRead  = 0.10
)

// Options configure a Resolver. The zero value is usable: it yields the bundled
// offline catalog with no network access and no inferred cache rates.
type Options struct {
	// Overrides are explicit rates that win over any catalog entry, keyed by
	// the observed model name.
	Overrides map[string]Rates
	// OverrideAliases maps an observed model to a published upstream model.
	// It is local configuration, so aliases such as an internal preview name
	// never have to be added to the resolver source code.
	OverrideAliases map[string]string
	// OverridesPath is where Overrides and OverrideAliases are persisted.
	OverridesPath string
	// HeuristicCache derives missing cache rates from the input rate. Off by
	// default; when on, affected matches carry CacheBasis=heuristic.
	HeuristicCache bool
	// Refresh allows the best-effort public catalog download. Off means fully
	// offline: the bundled snapshot and any previously cached catalog still work.
	Refresh bool
	URL     string
	// CachePath is the downloaded catalog's local copy. Empty uses the per-user
	// cache directory.
	CachePath string
	TTL       time.Duration
	Timeout   time.Duration

	// Now and Transport exist so tests can drive TTL and network behaviour
	// without a clock or a socket.
	Now       func() time.Time
	Transport func(ctx context.Context, url string) ([]byte, error)
}

func (o Options) withDefaults() Options {
	if o.URL == "" {
		o.URL = DefaultURL
	}
	if o.TTL <= 0 {
		o.TTL = DefaultTTL
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.CachePath == "" {
		o.CachePath = DefaultCachePath()
	}
	if o.OverridesPath == "" {
		o.OverridesPath = DefaultOverridesPath()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Transport == nil {
		o.Transport = httpTransport
	}
	return o
}

// DefaultCachePath is where a refreshed catalog is cached. It is a cache, not
// state: deleting it costs one download and nothing else.
func DefaultCachePath() string {
	if v := strings.TrimSpace(os.Getenv("HARNESS_OBSERVER_PRICING_CACHE")); v != "" {
		return v
	}
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, "harness-observer", "pricing-catalog.json")
}

// Resolver answers "what does this model cost per token" from the best catalog
// currently available, and explains every answer.
type Resolver struct {
	opts Options

	mu         sync.RWMutex
	overrideMu sync.Mutex
	catalog    *Catalog
	bundled    *Catalog
	memo       map[string]Match
	lastTry    time.Time
	lastErr    string
	refreshing bool
}

// New loads the bundled catalog and, if a fresh cached download exists, prefers
// it. It never blocks on the network.
func New(opts Options) (*Resolver, error) {
	opts = opts.withDefaults()
	bundled, err := Bundled()
	if err != nil {
		return nil, err
	}
	r := &Resolver{opts: opts, catalog: bundled, bundled: bundled, memo: map[string]Match{}}
	// Loading the explicit Options path directly also serves programmatic
	// callers and tests; OptionsFromEnv follows the same path for the CLI/UI.
	if rates, aliases, err := loadOverrideFile(opts.OverridesPath); err == nil {
		if len(rates) > 0 || len(aliases) > 0 || len(r.opts.Overrides) == 0 {
			r.opts.Overrides = rates
			r.opts.OverrideAliases = aliases
		}
	}
	if cached, err := loadCache(opts.CachePath); err == nil && cached != nil {
		// A stale cache still beats no cache: it was a real published table, and
		// the staleness is reported rather than silently corrected.
		r.catalog = cached
	}
	return r, nil
}

// Catalog is the table currently in use.
func (r *Resolver) Catalog() *Catalog {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.catalog
}

// Resolve maps an observed model name onto rates, memoized per resolver.
func (r *Resolver) Resolve(model string) Match {
	r.mu.RLock()
	if m, ok := r.memo[model]; ok {
		r.mu.RUnlock()
		return m
	}
	r.mu.RUnlock()

	m := r.resolve(model)
	r.mu.Lock()
	r.memo[model] = m
	r.mu.Unlock()
	return m
}

func (r *Resolver) resolve(model string) Match {
	r.mu.RLock()
	catalog, overrides := r.catalog, r.opts.Overrides
	r.mu.RUnlock()

	m := Match{ObservedModel: model, Basis: BasisUnpriced}
	model = strings.TrimSpace(model)
	if _, hasRates := overrides[model]; hasRates {
		r.mu.RLock()
		_, hasAlias := r.opts.OverrideAliases[model]
		r.mu.RUnlock()
		if hasAlias {
			m.Reason = "conflicting local pricing overrides"
			m.Source = RuleOverride
			m.SourceVersion = r.opts.OverridesPath
			return m
		}
	}
	if rates, ok := overrides[model]; ok {
		m.CanonicalModel = model
		m.Rule = RuleOverride
		m.Basis = BasisOverride
		m.Source = RuleOverride
		m.SourceVersion = r.opts.OverridesPath
		m.Rates = rates
		return r.applyCacheBasis(m)
	}
	r.mu.RLock()
	upstream := r.opts.OverrideAliases[model]
	r.mu.RUnlock()
	if upstream != "" {
		m = r.resolveCatalog(upstream)
		m.ObservedModel = model
		m.Rule = RuleUpstreamOverride
		m.Basis = BasisOverride
		return r.applyCacheBasis(m)
	}

	key, entry, rule, reason := catalog.lookup(model)
	if key == "" {
		m.Reason = reason
		m.Source = catalog.Source
		m.SourceVersion = catalog.Version
		return m
	}
	m.CanonicalModel = key
	m.Provider = entry.Provider
	m.Rule = rule
	m.Basis = BasisCatalog
	m.Source = catalog.Source
	m.SourceVersion = catalog.Version
	m.Rates = entry.Rates
	return r.applyCacheBasis(m)
}

// resolveCatalog bypasses local overrides. It is used to validate an upstream
// alias against published evidence and prevents alias chains or cycles.
func (r *Resolver) resolveCatalog(model string) Match {
	r.mu.RLock()
	catalog := r.catalog
	r.mu.RUnlock()
	m := Match{ObservedModel: model, Basis: BasisUnpriced}
	key, entry, rule, reason := catalog.lookup(model)
	if key == "" {
		m.Reason = reason
		m.Source = catalog.Source
		m.SourceVersion = catalog.Version
		return m
	}
	m.CanonicalModel = key
	m.Provider = entry.Provider
	m.Rule = rule
	m.Basis = BasisCatalog
	m.Source = catalog.Source
	m.SourceVersion = catalog.Version
	m.Rates = entry.Rates
	return r.applyCacheBasis(m)
}

// applyCacheBasis fills missing cache rates only when the operator asked for
// the heuristic, and always records that it did.
func (r *Resolver) applyCacheBasis(m Match) Match {
	if r.opts.HeuristicCache && m.Rates.Input != nil {
		inferred := false
		if m.Rates.CacheRead == nil {
			m.Rates.CacheRead = rate(*m.Rates.Input * heuristicCacheRead)
			inferred = true
		}
		if m.Rates.CacheWrite == nil {
			m.Rates.CacheWrite = rate(*m.Rates.Input * heuristicCacheWrite)
			inferred = true
		}
		if inferred {
			m.CacheBasis = BasisHeuristic
			return m
		}
	}
	// Nothing was inferred, so the cache buckets carry whatever basis produced
	// the rest of the rates, or none at all if the catalog published neither.
	if m.Rates.CacheRead == nil && m.Rates.CacheWrite == nil {
		m.CacheBasis = BasisUnpriced
		return m
	}
	m.CacheBasis = m.Basis
	return m
}

// Buckets are the canonical normalized token buckets a turn is priced from.
//
// Reasoning tokens are deliberately absent. In every source format verified so
// far reasoning is a subset of the output counter, so charging it again would
// double count it. If a provider ever publishes a separate, non-overlapping
// reasoning price, that becomes a new bucket here rather than an addition to
// output.
type Buckets struct {
	Input      int64
	CacheRead  int64
	CacheWrite int64
	Output     int64
}

// Cost is one priced set of buckets, carrying enough detail to show composition
// and to say honestly how much of the usage it actually covers.
type Cost struct {
	Match          Match
	Input          float64
	CacheRead      float64
	CacheWrite     float64
	Output         float64
	Total          float64
	PricedTokens   int64
	UnpricedTokens int64
	Heuristic      bool
}

// Cost prices one model's buckets. A bucket whose rate is unknown contributes
// nothing to the total and all of its tokens to UnpricedTokens, which is what
// keeps "we do not know" from rendering as "$0.00".
func (r *Resolver) Cost(model string, b Buckets) Cost {
	m := r.Resolve(model)
	c := Cost{Match: m}
	apply := func(tokens int64, price *float64, into *float64) {
		if tokens <= 0 {
			return
		}
		if price == nil {
			c.UnpricedTokens += tokens
			return
		}
		*into = float64(tokens) * *price
		c.Total += *into
		c.PricedTokens += tokens
	}
	apply(b.Input, m.Rates.Input, &c.Input)
	apply(b.CacheRead, m.Rates.CacheRead, &c.CacheRead)
	apply(b.CacheWrite, m.Rates.CacheWrite, &c.CacheWrite)
	apply(b.Output, m.Rates.Output, &c.Output)
	c.Heuristic = m.CacheBasis == BasisHeuristic && (b.CacheRead > 0 || b.CacheWrite > 0)
	return c
}

// Status is what "where do these prices come from" looks like from the CLI or
// the API.
type Status struct {
	Source             string     `json:"source"`
	Version            string     `json:"version"`
	Models             int        `json:"models"`
	FetchedAt          *time.Time `json:"fetched_at,omitempty"`
	Stale              bool       `json:"stale"`
	BundledVersion     string     `json:"bundled_version"`
	BundledModels      int        `json:"bundled_models"`
	RefreshEnabled     bool       `json:"refresh_enabled"`
	URL                string     `json:"url"`
	CachePath          string     `json:"cache_path"`
	TTLSeconds         float64    `json:"ttl_seconds"`
	LastRefreshAttempt *time.Time `json:"last_refresh_attempt,omitempty"`
	LastRefreshError   string     `json:"last_refresh_error,omitempty"`
	HeuristicCache     bool       `json:"heuristic_cache"`
	Overrides          int        `json:"overrides"`
	OverridesPath      string     `json:"overrides_path,omitempty"`
}

func (r *Resolver) Status() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	st := Status{
		Source:         r.catalog.Source,
		Version:        r.catalog.Version,
		Models:         r.catalog.Len(),
		BundledVersion: r.bundled.Version,
		BundledModels:  r.bundled.Len(),
		RefreshEnabled: r.opts.Refresh,
		URL:            r.opts.URL,
		CachePath:      r.opts.CachePath,
		TTLSeconds:     r.opts.TTL.Seconds(),
		HeuristicCache: r.opts.HeuristicCache,
		Overrides:      len(r.opts.Overrides),
		OverridesPath:  r.opts.OverridesPath,
	}
	if !r.catalog.FetchedAt.IsZero() {
		t := r.catalog.FetchedAt
		st.FetchedAt = &t
		st.Stale = r.opts.Now().Sub(t) > r.opts.TTL
	} else {
		st.Stale = r.catalog.Source == SourceBundled
	}
	if !r.lastTry.IsZero() {
		t := r.lastTry
		st.LastRefreshAttempt = &t
	}
	st.LastRefreshError = r.lastErr
	return st
}

// LoadOverrides reads explicit per-model rates from a local override file.
// Upstream aliases in the same file are loaded by OptionsFromEnv.
func LoadOverrides(path string) (map[string]Rates, error) {
	rates, _, err := loadOverrideFile(path)
	return rates, err
}

// OptionsFromEnv builds Options from the environment so the CLI, the API server
// and the dashboard all read the same knobs.
//
//	HARNESS_OBSERVER_PRICING_REFRESH    1/0 (default 1: refresh is best effort)
//	HARNESS_OBSERVER_PRICING_URL        public catalog URL
//	HARNESS_OBSERVER_PRICING_CACHE      cached catalog path
//	HARNESS_OBSERVER_PRICING_TTL        Go duration, e.g. 24h
//	HARNESS_OBSERVER_PRICING_OVERRIDES  path to a rate override file
//	HARNESS_OBSERVER_PRICING_HEURISTIC_CACHE  1 to infer missing cache rates
func OptionsFromEnv() (Options, error) {
	opts := Options{Refresh: true}
	opts.Refresh = envBool("HARNESS_OBSERVER_PRICING_REFRESH", true)
	opts.HeuristicCache = envBool("HARNESS_OBSERVER_PRICING_HEURISTIC_CACHE", false)
	opts.URL = strings.TrimSpace(os.Getenv("HARNESS_OBSERVER_PRICING_URL"))
	opts.CachePath = strings.TrimSpace(os.Getenv("HARNESS_OBSERVER_PRICING_CACHE"))
	opts.OverridesPath = strings.TrimSpace(os.Getenv("HARNESS_OBSERVER_PRICING_OVERRIDES"))
	if opts.OverridesPath == "" {
		opts.OverridesPath = DefaultOverridesPath()
	}
	if v := strings.TrimSpace(os.Getenv("HARNESS_OBSERVER_PRICING_TTL")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return opts, fmt.Errorf("HARNESS_OBSERVER_PRICING_TTL: %w", err)
		}
		opts.TTL = d
	}
	overrides, aliases, err := loadOverrideFile(opts.OverridesPath)
	if err != nil {
		return opts, err
	}
	opts.Overrides = overrides
	opts.OverrideAliases = aliases
	return opts, nil
}

func envBool(name string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
