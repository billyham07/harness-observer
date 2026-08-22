package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/billyham07/harness-observer/internal/pricing"
	"github.com/billyham07/harness-observer/internal/query"
	"github.com/billyham07/harness-observer/internal/serve"
	"github.com/billyham07/harness-observer/internal/store"
)

// pricingFlags registers the knobs every command that prices tokens shares.
// The environment supplies the defaults (see pricing.OptionsFromEnv) and an
// explicitly typed flag wins over it.
func pricingFlags(fs *flag.FlagSet) func() (*pricing.Resolver, error) {
	refresh := fs.Bool("pricing-refresh", true, "allow a best-effort public pricing catalog download (bundled rates already work offline)")
	heuristic := fs.Bool("pricing-heuristic-cache", false, "infer missing cache rates from the input rate; every number derived this way is marked heuristic")
	overrides := fs.String("pricing-overrides", "", "JSON file of explicit per-model rate overrides")
	return func() (*pricing.Resolver, error) {
		opts, err := pricing.OptionsFromEnv()
		if err != nil {
			return nil, err
		}
		typed := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { typed[f.Name] = true })
		if typed["pricing-refresh"] {
			opts.Refresh = *refresh
		}
		if typed["pricing-heuristic-cache"] {
			opts.HeuristicCache = *heuristic
		}
		if typed["pricing-overrides"] {
			loaded, loadErr := pricing.LoadOverrides(*overrides)
			if loadErr != nil {
				return nil, loadErr
			}
			opts.Overrides, opts.OverridesPath = loaded, *overrides
		}
		return pricing.New(opts)
	}
}

// costColumns are the cost columns `query --cost` adds. They are opt-in
// because the default table is about token evidence, and a dollar figure is a
// derived comparison metric rather than something a session file recorded.
func costHeaders(enabled bool) []string {
	if !enabled {
		return nil
	}
	return []string{"est_usd", "coverage", "unpriced", "reported_usd"}
}

func costCells(r query.Row, enabled bool) []string {
	if !enabled {
		return nil
	}
	if r.CostMetrics == nil {
		return []string{"-", "-", "-", fmt.Sprintf("%.4f", r.ReportedCostUSD)}
	}
	return []string{
		fmt.Sprintf("%.4f", r.EstimatedCostUSD),
		fmt.Sprintf("%.1f%%", r.PricingCoverage*100),
		fmt.Sprint(r.UnpricedTokens),
		fmt.Sprintf("%.4f", r.ReportedCostUSD),
	}
}

// runPricing is the diagnostic surface for the cost estimate: which catalog is
// in use, how fresh it is, and which models it cannot price.
func runPricing(ctx context.Context, args []string) error {
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "status":
		return runPricingStatus(ctx, args)
	case "refresh":
		return runPricingRefresh(ctx, args)
	case "unpriced":
		return runPricingUnpriced(ctx, args)
	default:
		return fmt.Errorf("unknown pricing subcommand %q (try: status, refresh, unpriced)", sub)
	}
}

func runPricingStatus(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("pricing status", flag.ExitOnError)
	build := pricingFlags(fs)
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	model := fs.String("model", "", "explain how one observed model name resolves")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolver, err := build()
	if err != nil {
		return err
	}
	if *model != "" {
		match := resolver.Resolve(*model)
		if *asJSON {
			return encodeJSON(match)
		}
		printMatch(match)
		return nil
	}
	status := resolver.Status()
	if *asJSON {
		return encodeJSON(status)
	}
	printStatus(status)
	return nil
}

func printStatus(st pricing.Status) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "catalog\t%s (version %s, %d models)\n", st.Source, st.Version, st.Models)
	if st.FetchedAt != nil {
		fmt.Fprintf(tw, "fetched\t%s\n", st.FetchedAt.Local().Format(time.RFC3339))
	}
	fmt.Fprintf(tw, "stale\t%s (ttl %s)\n", yesNo(st.Stale), time.Duration(st.TTLSeconds)*time.Second)
	fmt.Fprintf(tw, "bundled\tversion %s, %d models (always available offline)\n", st.BundledVersion, st.BundledModels)
	fmt.Fprintf(tw, "refresh\t%s\n", enabledDisabled(st.RefreshEnabled))
	fmt.Fprintf(tw, "source url\t%s\n", st.URL)
	fmt.Fprintf(tw, "cache\t%s\n", st.CachePath)
	if st.LastRefreshAttempt != nil {
		fmt.Fprintf(tw, "last attempt\t%s\n", st.LastRefreshAttempt.Local().Format(time.RFC3339))
	} else {
		fmt.Fprintf(tw, "last attempt\t-\n")
	}
	if st.LastRefreshError != "" {
		fmt.Fprintf(tw, "last error\t%s\n", st.LastRefreshError)
	}
	overrides := fmt.Sprint(st.Overrides)
	if st.OverridesPath != "" {
		overrides += " from " + st.OverridesPath
	}
	fmt.Fprintf(tw, "overrides\t%s\n", overrides)
	if st.HeuristicCache {
		fmt.Fprintf(tw, "cache rates\theuristic fallback ON: missing cache rates are inferred from the input rate and reported as heuristic\n")
	} else {
		fmt.Fprintf(tw, "cache rates\tpublished only: a model with no published cache rate leaves those tokens unpriced\n")
	}
	_ = tw.Flush()
	fmt.Println()
	fmt.Println("Estimates are API-equivalent costs at published rates. They are not")
	fmt.Println("actual subscription spend, and a model with no published rate is")
	fmt.Println("reported as unpriced rather than as $0.")
}

func printMatch(m pricing.Match) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "observed\t%s\n", m.ObservedModel)
	fmt.Fprintf(tw, "basis\t%s\n", m.Basis)
	if m.CanonicalModel != "" {
		fmt.Fprintf(tw, "canonical\t%s\n", m.CanonicalModel)
		fmt.Fprintf(tw, "matched by\t%s\n", m.Rule)
	}
	if m.Provider != "" {
		fmt.Fprintf(tw, "provider\t%s\n", m.Provider)
	}
	if m.Source != "" {
		fmt.Fprintf(tw, "catalog\t%s %s\n", m.Source, m.SourceVersion)
	}
	if m.Reason != "" {
		fmt.Fprintf(tw, "reason\t%s\n", m.Reason)
	}
	if m.CacheBasis != "" {
		fmt.Fprintf(tw, "cache rates\t%s\n", m.CacheBasis)
	}
	for label, r := range map[string]*float64{
		"input": m.Rates.Input, "cache read": m.Rates.CacheRead,
		"cache write": m.Rates.CacheWrite, "output": m.Rates.Output,
	} {
		if r == nil {
			continue
		}
		fmt.Fprintf(tw, "%s\t$%.2f per 1M tokens\n", label, *r*1e6)
	}
	_ = tw.Flush()
}

func runPricingRefresh(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pricing refresh", flag.ExitOnError)
	build := pricingFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolver, err := build()
	if err != nil {
		return err
	}
	fmt.Println("downloading the public pricing catalog (a price list only: nothing about local usage is sent)")
	if err := resolver.Refresh(ctx); err != nil {
		// A failed refresh is not a failed command: the previous catalog is
		// still in use and still correct.
		fmt.Fprintln(os.Stderr, "warn: refresh failed:", err)
	}
	printStatus(resolver.Status())
	return nil
}

func runPricingUnpriced(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pricing unpriced", flag.ExitOnError)
	build := pricingFlags(fs)
	dbPath := fs.String("db", "", "DuckDB path (default: per-user data dir, or $HARNESS_OBSERVER_DB)")
	since, until := windowFlags(fs)
	harness := fs.String("harness", "", "filter, comma separated")
	modelFilter := fs.String("model", "", "filter, comma separated")
	project := fs.String("project", "", "filter, comma separated")
	limit := fs.Int("limit", 25, "max rows (0 = all)")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolver, err := build()
	if err != nil {
		return err
	}
	from, err := serve.ParseTime(*since)
	if err != nil {
		return err
	}
	to, err := serve.ParseTime(*until)
	if err != nil {
		return err
	}
	filter := query.Filter{
		From:    from,
		To:      to,
		Harness: splitCSV(*harness),
		Model:   splitCSV(*modelFilter),
		Project: splitCSV(*project),
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	models, err := query.UnpricedModels(ctx, db.SQL(), filter, resolver, *limit)
	if err != nil {
		return err
	}
	totals, err := query.AggregateCost(ctx, db.SQL(), query.Request{Filter: filter}, resolver)
	if err != nil {
		return err
	}
	if *asJSON {
		payload := map[string]any{"models": models}
		if len(totals) > 0 {
			payload["totals"] = totals[0]
		}
		return encodeJSON(payload)
	}

	if len(totals) > 0 && totals[0].CostMetrics != nil {
		t := totals[0]
		fmt.Printf("estimated API cost  $%.2f  (coverage %.1f%%, %d of %d observed tokens priced)\n",
			t.EstimatedCostUSD, t.PricingCoverage*100, t.PricedTokens, t.PricedTokens+t.UnpricedTokens)
		fmt.Printf("pricing basis       %s\n\n", strings.Join(t.PricingBasis, ", "))
	}
	if len(models) == 0 {
		fmt.Println("every observed model resolved to a published rate")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "OBSERVED TOKENS\tTURNS\tHARNESS\tPROVIDER\tMODEL\tREASON")
	for _, m := range models {
		name := m.Model
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\t%s\n", m.Tokens, m.Turns, dash(m.Harness), dash(m.Provider), name, m.Reason)
	}
	return tw.Flush()
}

func dash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func enabledDisabled(v bool) string {
	if v {
		return "enabled (best effort, at most once per TTL, never on a page load)"
	}
	return "disabled (bundled and cached rates only)"
}

func encodeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
