// Command harness-observer is a local, read-only usage observer for coding
// agent harnesses.
//
//	harness-observer gpui      native desktop window + background incremental ingest
//	harness-observer web       local dashboard in a browser + background incremental ingest
//	harness-observer collect   stream normalized turns as NDJSON (no storage)
//	harness-observer ingest    persist normalized turns into the local DuckDB store
//	harness-observer doctor    report discovery/checkpoint state per source
//	harness-observer query     aggregate the local store over any time range
//	harness-observer logs      list individual normalized turn/events, paged
//	harness-observer serve     HTTP/JSON query API + Prometheus metrics
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/billyham07/harness-observer/internal/buildinfo"
	"github.com/billyham07/harness-observer/internal/collector"
	"github.com/billyham07/harness-observer/internal/ingest"
	"github.com/billyham07/harness-observer/internal/logx"
	"github.com/billyham07/harness-observer/internal/query"
	"github.com/billyham07/harness-observer/internal/serve"
	"github.com/billyham07/harness-observer/internal/source"
	"github.com/billyham07/harness-observer/internal/source/claude"
	"github.com/billyham07/harness-observer/internal/source/codex"
	"github.com/billyham07/harness-observer/internal/source/grok"
	"github.com/billyham07/harness-observer/internal/source/opencode"
	"github.com/billyham07/harness-observer/internal/source/pi"
	"github.com/billyham07/harness-observer/internal/store"
	"github.com/billyham07/harness-observer/internal/web"
)

const defaultSources = "codex,claude,omp,pi,opencode,grok"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	command, args := splitCommand(os.Args[1:])

	var err error
	switch command {
	case "collect":
		err = runCollect(ctx, args)
	case "ingest":
		err = runIngest(ctx, args)
	case "doctor":
		err = runDoctor(ctx, args)
	case "query":
		err = runQuery(ctx, args)
	case "logs":
		err = runLogs(ctx, args)
	case "serve":
		err = runServe(ctx, args)
	case "web":
		err = runWeb(ctx, args)
	case "gpui":
		err = runGPUI(ctx, args)
	case "pricing":
		err = runPricing(ctx, args)
	case "help", "-h", "-help", "--help":
		usage(os.Stdout)
	case "version", "-version", "--version", "-V":
		printVersion(os.Stdout)
	default:
		usage(os.Stderr)
		err = fmt.Errorf("unknown command %q (try: gpui, web, collect, ingest, doctor, query, logs, serve, pricing, version)", command)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// splitCommand picks the command out of the argument list. A leading flag
// belongs to collect, with one exception: a top level help or version flag is
// routed to that command. Otherwise it would reach the default command's flag
// set and never mention that "web" or "gpui" exist.
//
// A bare invocation (no args) is platform-specific: Windows opens the native
// window, everywhere else still streams NDJSON. Leading flags stay on collect
// so `harness-observer --since ...` does not break on Windows.
func splitCommand(args []string) (string, []string) {
	if len(args) > 0 && (!strings.HasPrefix(args[0], "-") || isHelpFlag(args[0]) || isVersionFlag(args[0])) {
		return args[0], args[1:]
	}
	if len(args) == 0 {
		return defaultCommand(), nil
	}
	return "collect", args
}

func defaultCommand() string {
	if runtime.GOOS == "windows" {
		return "gpui"
	}
	return "collect"
}

func isHelpFlag(arg string) bool {
	switch arg {
	case "-h", "-help", "--help":
		return true
	}
	return false
}

// isVersionFlag routes --version the same way --help is routed. Without it the
// flag reaches collect's flag set, which reports "flag provided but not
// defined" for the most conventional question a binary is asked.
func isVersionFlag(arg string) bool {
	switch arg {
	case "-V", "-version", "--version":
		return true
	}
	return false
}

// printVersion answers "which build is this, and what can it read". The store
// and parser versions belong next to the build: a binary that is newer than
// the database it opens, or older than the evidence it parsed, is exactly the
// situation this output exists to explain.
func printVersion(w io.Writer) {
	fmt.Fprintf(w, "harness-observer %s\n%s\nstore schema %d · parser %d\n",
		buildinfo.Version(), buildinfo.Platform(), store.SchemaVersion, ingest.ParserVersion)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `harness-observer - local read-only usage observability for coding agents

usage: harness-observer <command> [flags]

  gpui      native desktop window with background incremental ingest (Windows default)
  web       local dashboard on `+web.DefaultAddr+` in a browser
  collect   stream normalized turns as NDJSON (no storage)
  ingest    persist normalized turns into the local DuckDB store (incremental, idempotent)
  doctor    report roots, discovery and checkpoint state per source
  query     aggregate the local store over an arbitrary time range
  logs      list individual normalized events, filtered and paged
  serve     HTTP/JSON query API + Prometheus /metrics
  pricing   inspect the model pricing catalog behind the cost estimate
  version   print the build, Go toolchain and store schema versions

Start here:

  harness-observer                      Windows: native window; elsewhere: collect
  harness-observer gpui                 native window (WebView2 on Windows)
  harness-observer web                  dashboard, opens a browser window
  harness-observer web --no-open        dashboard without opening a browser
  harness-observer web --addr 127.0.0.1:9500 --ingest-interval 1m

A bare "harness-observer" on Windows opens the native window. Anywhere else,
and whenever the first argument is a flag, the default is still "collect".
Run "harness-observer <command> -h" for the flags of a command.
`)
}

// stringList collects a repeatable flag. --select is repeatable rather than
// comma separated because one value is a harness|model|effort tuple, and a
// model name is allowed to contain a comma.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, " ") }

func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// debugFlag wires --debug into the program's own diagnostics. It is about this
// process (scans, refreshes, failures), not about agent events: those are the
// "logs" command and they come from the store.
func debugFlag(fs *flag.FlagSet) func() {
	debug := fs.Bool("debug", false, "verbose program diagnostics on stderr (also: "+logx.EnvVar+"=debug)")
	return func() {
		if *debug {
			logx.SetLevel(logx.LevelDebug)
		}
	}
}

func sourcesFlag(fs *flag.FlagSet) *string {
	return fs.String("sources", defaultSources, "comma separated: codex,claude,omp,pi,opencode,grok")
}

func buildSources(spec string) ([]source.FileSource, error) {
	var out []source.FileSource
	for _, name := range strings.Split(spec, ",") {
		switch strings.TrimSpace(name) {
		case "":
		case "codex":
			out = append(out, codex.New(""))
		// claude covers both Claude frontends; the old per-frontend spellings
		// stay accepted so an existing script keeps working.
		case "claude", "claude-desktop", "claude-code":
			out = append(out, claude.New())
		case "omp":
			out = append(out, pi.New("omp", ""))
		case "pi":
			out = append(out, pi.New("pi", ""))
		case "opencode":
			out = append(out, opencode.New())
		case "grok", "grok-build":
			out = append(out, grok.New(""))
		default:
			return nil, fmt.Errorf("unknown source %q", name)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no sources selected")
	}
	return out, nil
}

func windowFlags(fs *flag.FlagSet) (*string, *string) {
	since := fs.String("since", "", "RFC3339 or YYYY-MM-DD[THH:MM[:SS]] (local time)")
	until := fs.String("until", "", "RFC3339 or YYYY-MM-DD[THH:MM[:SS]] (local time)")
	return since, until
}

func parseWindow(since, until string) (source.Window, error) {
	from, err := serve.ParseTime(since)
	if err != nil {
		return source.Window{}, err
	}
	to, err := serve.ParseTime(until)
	if err != nil {
		return source.Window{}, err
	}
	return source.Window{Since: from, Until: to}, nil
}

func runCollect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("collect", flag.ExitOnError)
	spec := sourcesFlag(fs)
	since, until := windowFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	window, err := parseWindow(*since, *until)
	if err != nil {
		return err
	}
	sources, err := buildSources(*spec)
	if err != nil {
		return err
	}
	generic := make([]source.Source, 0, len(sources))
	for _, s := range sources {
		generic = append(generic, s)
	}
	turns, err := (collector.Collector{Sources: generic}).Collect(ctx, window)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	for _, t := range turns {
		if err := enc.Encode(t); err != nil {
			return err
		}
	}
	return nil
}

func runIngest(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	spec := sourcesFlag(fs)
	since, until := windowFlags(fs)
	dbPath := fs.String("db", "", "DuckDB path (default: per-user data dir, or $HARNESS_OBSERVER_DB)")
	reset := fs.Bool("reset-checkpoints", false, "re-read every discovered file from the start")
	quiet := fs.Bool("quiet", false, "suppress the per-source summary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	window, err := parseWindow(*since, *until)
	if err != nil {
		return err
	}
	sources, err := buildSources(*spec)
	if err != nil {
		return err
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if *reset {
		for _, s := range sources {
			if err := db.ResetSource(ctx, s.Name()); err != nil {
				return err
			}
		}
	}

	stats, err := ingest.Ingestor{DB: db, Sources: sources, Window: window}.Run(ctx)
	if err != nil {
		return err
	}
	if *quiet {
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SOURCE\tDISCOVERED\tPARSED\tSKIPPED\tREWOUND\tTURNS\tNEW\tNEWEST")
	for _, st := range stats {
		newest := "-"
		if !st.NewestActivity.IsZero() {
			newest = st.NewestActivity.Local().Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
			st.Source, st.FilesDiscovered, st.FilesParsed, st.FilesSkipped, st.FilesRewound,
			st.TurnsParsed, st.TurnsIngested, newest)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, st := range stats {
		for _, warning := range st.Warnings {
			fmt.Fprintf(os.Stderr, "warn [%s] %s\n", st.Source, warning)
		}
	}
	fmt.Println("db:", db.Path())
	reportLegacyStore()
	return nil
}

// reportLegacyStore mentions a pre-DuckDB database once instead of leaving an
// orphan file unexplained. It is never opened, migrated or deleted: normalized
// data is rebuilt from the session evidence, which is both safer than a row
// level migration and the only path that stays correct when normalization has
// changed. Removing it is the user's call.
func reportLegacyStore() {
	if path, found := store.LegacySQLiteStore(); found {
		fmt.Printf("note: a pre-DuckDB SQLite store is still at %s.\n"+
			"      It is not read, migrated or deleted; this store is rebuilt from session files.\n"+
			"      Delete it yourself when you no longer want it.\n", path)
	}
}

// runDoctor reports evidence discovery and checkpoint state. It deliberately
// prints paths and counters only: never prompts, responses or credentials.
func runDoctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	spec := sourcesFlag(fs)
	dbPath := fs.String("db", "", "DuckDB path (default: per-user data dir, or $HARNESS_OBSERVER_DB)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sources, err := buildSources(*spec)
	if err != nil {
		return err
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	version, err := db.SchemaVersion()
	if err != nil {
		return err
	}
	fmt.Printf("database        %s\nschema version  %d (binary knows %d)\nparser version  %d\n",
		db.Path(), version, store.SchemaVersion, ingest.ParserVersion)
	if path, found := store.LegacySQLiteStore(); found {
		fmt.Printf("legacy store    %s (not read, not migrated, not deleted)\n", path)
	}
	fmt.Println()

	for _, s := range sources {
		fmt.Printf("source          %s\n", s.Name())
		for i, root := range s.Roots() {
			state := "missing"
			if info, statErr := os.Stat(root); statErr == nil && info.IsDir() {
				state = "present"
			}
			label := "roots searched"
			if i > 0 {
				label = ""
			}
			fmt.Printf("%-16s %s (%s)\n", label, root, state)
		}
		if err := s.Prepare(ctx); err != nil {
			fmt.Printf("warnings        prepare: %v\n\n", err)
			continue
		}
		files, listErr := s.Files(ctx)
		fmt.Printf("%-16s %d\n", "files discovered", len(files))

		checkpointed, resumable := 0, 0
		for _, path := range files {
			cp, found, cpErr := db.Checkpoint(ctx, s.Name(), path)
			if cpErr != nil {
				return cpErr
			}
			if !found {
				continue
			}
			checkpointed++
			if info, statErr := os.Stat(path); statErr == nil && info.Size() > cp.State.Offset {
				resumable++
			}
		}
		stats, statsErr := db.CheckpointStats(ctx, s.Name())
		if statsErr != nil {
			return statsErr
		}
		fmt.Printf("%-16s %d files, %d unread tails\n", "checkpoints", checkpointed, resumable)
		fmt.Printf("%-16s %d\n", "turns parsed", stats.Turns)
		stored, newest, newestErr := storedForSource(ctx, db, s.Name())
		if newestErr != nil {
			return newestErr
		}
		fmt.Printf("%-16s %d (after dedup)\n", "turns stored", stored)
		if newest.IsZero() {
			fmt.Printf("%-16s -\n", "newest activity")
		} else {
			fmt.Printf("%-16s %s\n", "newest activity", newest.Local().Format(time.RFC3339))
		}
		if listErr != nil {
			fmt.Printf("%-16s discovery: %v\n", "warnings", listErr)
		}
		if r, ok := s.(source.Reporter); ok {
			for i, note := range r.Report() {
				label := "notes"
				if i > 0 {
					label = ""
				}
				fmt.Printf("%-16s %s\n", label, note)
			}
		}
		fmt.Println()
	}
	return nil
}

func storedForSource(ctx context.Context, db *store.DB, sourceName string) (int64, time.Time, error) {
	// A source name is a collector identity ("omp"), while stored rows carry the
	// harness identity a source can classify per session ("codex-desktop"), so
	// match on the harness prefix rather than assuming they are equal.
	rows, err := query.Aggregate(ctx, db.SQL(), query.Request{GroupBy: []string{"harness"}})
	if err != nil {
		return 0, time.Time{}, err
	}
	var (
		stored int64
		newest time.Time
	)
	for _, r := range rows {
		harness := r.Group["harness"]
		if harness != sourceName && !strings.HasPrefix(harness, sourceName+"-") {
			continue
		}
		stored += r.Turns
		if r.LastSeen.After(newest) {
			newest = r.LastSeen
		}
	}
	return stored, newest, nil
}

func runQuery(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("query", flag.ExitOnError)
	dbPath := fs.String("db", "", "DuckDB path (default: per-user data dir, or $HARNESS_OBSERVER_DB)")
	since, until := windowFlags(fs)
	group := fs.String("group", "harness,model", "group dimensions: "+strings.Join(query.GroupDimensions(), ", "))
	bucket := fs.String("bucket", "", "time bucket: hour, day, week, month")
	harness := fs.String("harness", "", "filter, comma separated")
	client := fs.String("client", "", "filter, comma separated")
	provider := fs.String("provider", "", "filter, comma separated")
	modelFilter := fs.String("model", "", "filter, comma separated")
	project := fs.String("project", "", "filter, comma separated")
	basis := fs.String("basis", "", "filter: observed, derived-delta, estimated")
	eventKind := fs.String("event-kind", "", "filter: assistant, tool-llm, compaction, branch-summary")
	strict := fs.Bool("strict", false, "exclude usage_basis=estimated")
	limit := fs.Int("limit", 0, "max rows (0 = all)")
	order := fs.String("order", "", "set to \"tokens\" to sort by observed tokens instead of the group key")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	withCost := fs.Bool("cost", false, "add API-equivalent cost columns (an estimate at published rates, not billed spend)")
	buildPricing := pricingFlags(fs)
	effort := fs.String("effort", "", "filter by reasoning effort, comma separated")
	var selection stringList
	fs.Var(&selection, "select", "hierarchy selection harness[|model[|effort]], repeatable; levels stay correlated")
	if err := fs.Parse(args); err != nil {
		return err
	}

	values := map[string]string{
		"from": *since, "to": *until, "group": *group, "bucket": *bucket,
		"harness": *harness, "client": *client, "provider": *provider,
		"model": *modelFilter, "effort": *effort, "project": *project, "basis": *basis,
		"event_kind": *eventKind, "limit": fmt.Sprint(*limit), "order": *order,
	}
	req, err := serve.RequestFromValues(
		func(k string) string { return values[k] },
		func(k string) []string {
			if k == "sel" {
				return selection
			}
			return splitCSV(values[k])
		},
	)
	if err != nil {
		return err
	}
	if *strict && len(req.Filter.Basis) == 0 {
		req.Filter.Basis = []string{"observed", "derived-delta"}
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	// JSON always carries the estimate; the table only does when asked, so the
	// default output stays about token evidence.
	var pricer query.Pricer
	if *withCost || *asJSON {
		resolver, buildErr := buildPricing()
		if buildErr != nil {
			return buildErr
		}
		pricer = resolver
	}
	rows, err := query.AggregateCost(ctx, db.SQL(), req, pricer)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	header := append([]string{}, req.GroupBy...)
	if req.Bucket != query.BucketNone {
		header = append(header, "bucket")
	}
	header = append(header, "turns", "sessions", "input", "cache_read", "cache_write", "output", "reasoning", "observed", "cache_hit", "tools", "tool_err")
	header = append(header, costHeaders(*withCost)...)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range rows {
		var cells []string
		for _, g := range req.GroupBy {
			v := r.Group[strings.ToLower(strings.TrimSpace(g))]
			if v == "" {
				v = "-"
			}
			cells = append(cells, v)
		}
		if req.Bucket != query.BucketNone {
			cells = append(cells, r.Bucket)
		}
		cells = append(cells,
			fmt.Sprint(r.Turns), fmt.Sprint(r.Sessions), fmt.Sprint(r.InputTokens),
			fmt.Sprint(r.CacheRead), fmt.Sprint(r.CacheWrite), fmt.Sprint(r.OutputTokens),
			fmt.Sprint(r.Reasoning), fmt.Sprint(r.ObservedTokens),
			fmt.Sprintf("%.1f%%", r.CacheHitRatio*100),
			fmt.Sprint(r.ToolCalls), fmt.Sprint(r.ToolErrors))
		cells = append(cells, costCells(r, *withCost)...)
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	if *withCost {
		fmt.Fprintln(tw)
		fmt.Fprintln(tw, "est_usd is an API-equivalent estimate at published rates, not actual subscription spend.")
		fmt.Fprintln(tw, "coverage is the share of observed tokens a published rate was found for; the rest is unpriced, not free.")
	}
	return tw.Flush()
}

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dbPath := fs.String("db", "", "DuckDB path (default: per-user data dir, or $HARNESS_OBSERVER_DB)")
	addr := fs.String("addr", "127.0.0.1:9464", "listen address (loopback by default: this is local-only data)")
	buildPricing := pricingFlags(fs)
	applyDebug := debugFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	applyDebug()
	resolver, err := buildPricing()
	if err != nil {
		return err
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	fmt.Printf("serving %s on http://%s (endpoints: /api/health, /api/query, /api/logs, /api/pricing, /metrics)\n", db.Path(), *addr)
	return serve.Server{DB: db, Pricing: resolver}.ListenAndServe(ctx, *addr)
}

// runWeb starts the built-in dashboard in a browser. The same process owns
// background incremental ingestion, so a desktop user needs neither cron nor
// Task Scheduler to keep the numbers current.
func runWeb(ctx context.Context, args []string) error {
	return runDashboard(ctx, args, dashboardKindWeb)
}

// runGPUI starts the same dashboard as web, then hosts it in a native window
// (WebView2 on Windows). Closing the window stops the process.
func runGPUI(ctx context.Context, args []string) error {
	return runDashboard(ctx, args, dashboardKindGPUI)
}

func splitCSV(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
