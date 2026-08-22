package pricing

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Basis says where a rate came from. It is reported alongside every estimate so
// a catalog price and an inferred one are never presented as the same thing.
type Basis string

const (
	BasisCatalog   Basis = "catalog"
	BasisOverride  Basis = "explicit-override"
	BasisHeuristic Basis = "heuristic"
	BasisUnpriced  Basis = "unpriced"
)

// The resolution rules, in the order they are attempted. Each one is an exact
// lookup of a deterministically rewritten name; none of them is a prefix or
// substring match, so a new "gpt-5.7-sol" can never be priced as "gpt-5.6-sol".
const (
	RuleOverride        = "explicit-override"
	RuleExact           = "exact"
	RuleSeparator       = "separator-normalized"
	RuleProviderPrefix  = "provider-prefix"
	RuleAlias           = "canonical-alias"
	RuleVendorReorder   = "vendor-name-reorder"
	RuleDateSuffix      = "date-suffix"
	RuleCaseInsensitive = "case-insensitive"
	RuleUniqueSuffix    = "unique-provider-suffix"
	RuleAgreedSuffix    = "agreed-provider-suffix"
)

// Reasons an observed model stays unpriced. These are diagnostics, not errors:
// an unpriced model is a coverage gap to report, not a failure to hide.
const (
	ReasonNoModel   = "no model recorded"
	ReasonNoMatch   = "no catalog entry for this model"
	ReasonAmbiguous = "several providers publish this model at different rates"
)

// Match explains one observed model to rate resolution end to end:
// observed model -> canonical catalog key -> catalog source and version.
type Match struct {
	ObservedModel  string `json:"observed_model"`
	CanonicalModel string `json:"canonical_model,omitempty"`
	Provider       string `json:"provider,omitempty"`
	Source         string `json:"source,omitempty"`
	SourceVersion  string `json:"source_version,omitempty"`
	Rule           string `json:"rule,omitempty"`
	Basis          Basis  `json:"basis"`
	// CacheBasis is separate because cache rates can be inferred from the input
	// rate while input and output stay catalog priced. Folding the two together
	// would make a partly heuristic number look fully published.
	CacheBasis Basis  `json:"cache_basis,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Rates      Rates  `json:"rates"`
}

// Priced reports whether any bucket of this match can be priced.
func (m Match) Priced() bool { return m.Basis != BasisUnpriced && !m.Rates.Empty() }

// aliases are hand-maintained canonicalizations for names no mechanical rule
// can derive. Keys are lowercase. Keep this list small and specific: every
// entry is a claim that two names are the same billed model.
// A reasoning-effort suffix is deliberately not aliased away here. Effort does
// not appear in a published rate, and guessing that "some-model-high" bills as
// "some-model" is exactly the inference this package refuses to make.
//
// Grok Build's turn_completed.modelUsage key is the public model id plus
// "-build" (summary/signals keep "grok-4.6"). That is not xai/grok-build-0.1,
// a different catalog product.
var aliases = map[string]string{
	"gpt-4o-latest":  "gpt-4o",
	"chatgpt-4o":     "chatgpt-4o-latest",
	"grok-4.6-build": "xai/grok-4.6",
	"grok-4.5-build": "xai/grok-4.5",
}

var (
	// dateSuffix matches a trailing release date, the one suffix that is safe to
	// drop: "claude-opus-4-6-20260205" and "claude-opus-4-6" are the same model
	// at the same price. Nothing else is stripped.
	dateSuffix = regexp.MustCompile(`-(\d{4}-\d{2}-\d{2}|\d{8})$`)

	// vendorReorder covers Anthropic's two spellings of one model:
	// "claude-4.6-opus" (marketing order) and "claude-opus-4-6" (API order).
	// Both appear in real harness transcripts.
	vendorReorder = regexp.MustCompile(`^claude-(\d+)[.\-](\d+)-(opus|sonnet|haiku)((?:-.*)?)$`)
)

type candidate struct {
	name string
	rule string
}

// candidates rewrites an observed model name into the exact catalog keys worth
// trying, in priority order. Every rewrite is reversible and explainable; the
// caller only ever does exact lookups on the results.
func candidates(observed string) []candidate {
	var out []candidate
	seen := map[string]bool{observed: true}
	add := func(name, rule string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, candidate{name: name, rule: rule})
	}

	// A harness may spell the provider boundary as "/", "--" or ":".
	base := normalizeSeparators(observed)
	add(base, RuleSeparator)

	// Drop leading routing/gateway segments one at a time: an "aigw_local/" or
	// "google-antigravity/" prefix says how the call was routed, not which model
	// was billed.
	segments := strings.Split(base, "/")
	for i := 1; i < len(segments); i++ {
		add(strings.Join(segments[i:], "/"), RuleProviderPrefix)
	}

	// Expand every name found so far through the remaining rewrites. The list
	// is snapshotted first so an expansion cannot feed itself.
	expand := func(rewrite func(string) string, rule string) {
		for _, name := range append([]string{observed}, names(out)...) {
			add(rewrite(name), rule)
		}
	}
	expand(func(name string) string { return aliases[strings.ToLower(name)] }, RuleAlias)
	expand(reorderVendorName, RuleVendorReorder)
	expand(func(name string) string {
		stripped := dateSuffix.ReplaceAllString(name, "")
		if stripped == name {
			return ""
		}
		return stripped
	}, RuleDateSuffix)
	return out
}

func names(list []candidate) []string {
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.name)
	}
	return out
}

func normalizeSeparators(name string) string {
	replaced := strings.ReplaceAll(name, "--", "/")
	replaced = strings.ReplaceAll(replaced, "::", "/")
	replaced = strings.ReplaceAll(replaced, ":", "/")
	return strings.Trim(replaced, "/")
}

func reorderVendorName(name string) string {
	m := vendorReorder.FindStringSubmatch(strings.ToLower(name))
	if m == nil {
		return ""
	}
	return fmt.Sprintf("claude-%s-%s-%s%s", m[3], m[1], m[2], m[4])
}

// lookup finds the catalog entry for an observed model name.
//
// Order matters: the untouched name wins over any rewrite, and every rewrite is
// tried as an exact key before case folding, so a catalog that spells two
// models differently only by case still resolves each to itself.
func (c *Catalog) lookup(observed string) (string, Entry, string, string) {
	if c == nil {
		return "", Entry{}, "", ReasonNoMatch
	}
	name := strings.TrimSpace(observed)
	if name == "" {
		return "", Entry{}, "", ReasonNoModel
	}
	if e, ok := c.Models[name]; ok {
		return name, e, RuleExact, ""
	}
	list := candidates(name)
	for _, cand := range list {
		if e, ok := c.Models[cand.name]; ok {
			return cand.name, e, cand.rule, ""
		}
	}
	for _, cand := range append([]candidate{{name: name, rule: RuleExact}}, list...) {
		if key, ok := c.lower[strings.ToLower(cand.name)]; ok {
			return key, c.Models[key], RuleCaseInsensitive, ""
		}
	}
	// Last resort: a bare model name against provider-prefixed catalog keys.
	// This only fires when the answer cannot depend on which provider is meant,
	// either because exactly one publishes the model or because every publisher
	// lists identical rates.
	for _, cand := range append([]candidate{{name: name, rule: RuleExact}}, list...) {
		key, rule, ambiguous := c.bySuffix(cand.name)
		if ambiguous {
			return "", Entry{}, "", ReasonAmbiguous
		}
		if key != "" {
			return key, c.Models[key], rule, ""
		}
	}
	return "", Entry{}, "", ReasonNoMatch
}

func (c *Catalog) bySuffix(name string) (string, string, bool) {
	keys := c.suffix[name]
	switch len(keys) {
	case 0:
		return "", "", false
	case 1:
		return keys[0], RuleUniqueSuffix, false
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	first := c.Models[sorted[0]]
	for _, k := range sorted[1:] {
		if !c.Models[k].Rates.equal(first.Rates) {
			return "", "", true
		}
	}
	return sorted[0], RuleAgreedSuffix, false
}
